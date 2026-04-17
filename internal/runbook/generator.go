package runbook

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// InvestigationSummary is a minimal projection of a completed investigation
// used for runbook generation. Avoids importing the investigation package.
type InvestigationSummary struct {
	AlertTitle  string
	Service     string
	RootCause   string
	Resolution  string
	Steps       []string // human-readable step descriptions
	CompletedAt time.Time
}

// GeneratorStore abstracts the queries the Generator needs.
type GeneratorStore interface {
	// FindCompletedByService returns recent successful investigations
	// for a given service, ordered newest first.
	FindCompletedByService(ctx context.Context, service string, limit int) ([]InvestigationSummary, error)
}

// Generator analyses completed investigations and proposes new runbooks
// when recurring patterns are detected.
type Generator struct {
	store     GeneratorStore
	rbRepo    *Repository
	threshold int // minimum investigations with same pattern to propose a runbook
}

// NewGenerator creates a generator. threshold is the minimum number of
// similar investigations required before a runbook is proposed (default 3).
func NewGenerator(store GeneratorStore, rbRepo *Repository, threshold int) *Generator {
	if threshold == 0 {
		threshold = 3
	}
	return &Generator{store: store, rbRepo: rbRepo, threshold: threshold}
}

// ProposedRunbook is a runbook candidate generated from investigation patterns.
type ProposedRunbook struct {
	Name          string
	Description   string
	Service       string
	Steps         []string
	CommonCause   string
	SourceCount   int // how many investigations contributed
}

// ProposeForService analyses completed investigations for a service and
// returns a proposed runbook if a recurring pattern is detected.
func (g *Generator) ProposeForService(ctx context.Context, service string) (*ProposedRunbook, error) {
	summaries, err := g.store.FindCompletedByService(ctx, service, 20)
	if err != nil {
		return nil, fmt.Errorf("find investigations: %w", err)
	}

	if len(summaries) < g.threshold {
		return nil, nil // not enough data
	}

	// Group by similar root causes (simple substring matching)
	clusters := clusterByCause(summaries)

	for cause, group := range clusters {
		if len(group) < g.threshold {
			continue
		}

		steps := extractCommonSteps(group)
		if len(steps) == 0 {
			continue
		}

		return &ProposedRunbook{
			Name:        fmt.Sprintf("Auto: %s investigation", service),
			Description: fmt.Sprintf("Auto-generated from %d similar investigations for %s", len(group), service),
			Service:     service,
			Steps:       steps,
			CommonCause: cause,
			SourceCount: len(group),
		}, nil
	}

	return nil, nil
}

// Save persists a proposed runbook with auto-generated triggers.
func (g *Generator) Save(ctx context.Context, proposed *ProposedRunbook) (*Runbook, error) {
	content := buildRunbookContent(proposed)

	rb := &Runbook{
		Name:        proposed.Name,
		Description: proposed.Description,
		Content:     content,
		Triggers: []Trigger{{
			Field:    "alert.service",
			Operator: "equals",
			Value:    proposed.Service,
		}},
		Enabled: false, // disabled by default, requires human review
	}

	return g.rbRepo.Save(ctx, rb)
}

func clusterByCause(summaries []InvestigationSummary) map[string][]InvestigationSummary {
	clusters := make(map[string][]InvestigationSummary)
	for _, s := range summaries {
		if s.RootCause == "" {
			continue
		}
		// Normalize: lowercase, first 100 chars as cluster key
		key := strings.ToLower(s.RootCause)
		if len(key) > 100 {
			key = key[:100]
		}

		// Try to find an existing cluster with similar key
		matched := false
		for existing := range clusters {
			if stringSimilarity(existing, key) > 0.6 {
				clusters[existing] = append(clusters[existing], s)
				matched = true
				break
			}
		}
		if !matched {
			clusters[key] = append(clusters[key], s)
		}
	}
	return clusters
}

// stringSimilarity returns a simple Jaccard similarity of word sets.
func stringSimilarity(a, b string) float64 {
	wordsA := toWordSet(a)
	wordsB := toWordSet(b)

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA)
	for w := range wordsB {
		if !wordsA[w] {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func toWordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[strings.ToLower(w)] = true
	}
	return set
}

func extractCommonSteps(group []InvestigationSummary) []string {
	// Collect all step descriptions, find the most frequent ones
	stepFreq := make(map[string]int)
	for _, s := range group {
		for _, step := range s.Steps {
			normalized := strings.ToLower(strings.TrimSpace(step))
			if normalized != "" {
				stepFreq[normalized]++
			}
		}
	}

	// Keep steps that appear in at least half the investigations
	minFreq := len(group) / 2
	if minFreq < 1 {
		minFreq = 1
	}

	var common []string
	for step, freq := range stepFreq {
		if freq >= minFreq {
			common = append(common, step)
		}
	}
	return common
}

func buildRunbookContent(proposed *ProposedRunbook) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", proposed.Name)
	fmt.Fprintf(&b, "%s\n\n", proposed.Description)
	fmt.Fprintf(&b, "## Common Root Cause\n%s\n\n", proposed.CommonCause)
	b.WriteString("## Investigation Steps\n")
	for i, step := range proposed.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, step)
	}
	return b.String()
}
