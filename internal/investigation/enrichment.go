package investigation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/runbook"
)

// Enricher adds contextual information to an investigation prompt.
// Enrich returns a formatted text block to inject into the prompt,
// or an error if the enricher cannot retrieve its data.
type Enricher interface {
	Enrich(ctx context.Context, evt *alert.Event) (string, error)
}

// EnrichmentPipeline runs a set of Enrichers and concatenates their results.
type EnrichmentPipeline struct {
	enrichers []Enricher
	log       *slog.Logger
}

// NewEnrichmentPipeline constructs a pipeline from the provided enrichers.
func NewEnrichmentPipeline(enrichers ...Enricher) *EnrichmentPipeline {
	return &EnrichmentPipeline{
		enrichers: enrichers,
		log:       slog.Default(),
	}
}

// Enrich runs every enricher in order, collects successful results, and
// returns the concatenated text. Enrichers that return an error are skipped
// with a warning log; the pipeline never fails.
func (p *EnrichmentPipeline) Enrich(ctx context.Context, evt *alert.Event) string {
	var parts []string

	for _, e := range p.enrichers {
		text, err := e.Enrich(ctx, evt)
		if err != nil {
			p.log.Warn("enricher skipped due to error",
				"enricher", fmt.Sprintf("%T", e),
				"service", evt.Service,
				"error", err,
			)
			continue
		}
		if text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

// BuildEnrichedPrompt wraps buildInitialPrompt and appends the enrichment
// context block when it is non-empty.
func BuildEnrichedPrompt(evt *alert.Event, rb *runbook.Runbook, enrichment string) string {
	base := buildInitialPrompt(evt, rb)
	if enrichment == "" {
		return base
	}

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n## Enrichment Context\n")
	b.WriteString(enrichment)
	return b.String()
}

// ---------------------------------------------------------------------------
// RecentDeploymentsEnricher
// ---------------------------------------------------------------------------

// Deployment represents a single deployment event.
type Deployment struct {
	Version    string
	Author     string
	DeployedAt time.Time
	Status     string
}

// DeploymentSource retrieves recent deployments for a service.
type DeploymentSource interface {
	RecentDeployments(ctx context.Context, service string, since time.Duration) ([]Deployment, error)
}

// RecentDeploymentsEnricher fetches recent deployments for the alert's service.
type RecentDeploymentsEnricher struct {
	src    DeploymentSource
	window time.Duration
}

// NewRecentDeploymentsEnricher constructs the enricher with the given source
// and look-back window (e.g. 2*time.Hour).
func NewRecentDeploymentsEnricher(src DeploymentSource, window time.Duration) *RecentDeploymentsEnricher {
	return &RecentDeploymentsEnricher{
		src:    src,
		window: window,
	}
}

// Enrich queries the deployment source and formats a Markdown section.
func (e *RecentDeploymentsEnricher) Enrich(ctx context.Context, evt *alert.Event) (string, error) {
	deployments, err := e.src.RecentDeployments(ctx, evt.Service, e.window)
	if err != nil {
		return "", fmt.Errorf("RecentDeploymentsEnricher: %w", err)
	}

	if len(deployments) == 0 {
		return "## Recent Deployments\n- No recent deployments found.", nil
	}

	var b strings.Builder
	b.WriteString("## Recent Deployments\n")
	for _, d := range deployments {
		fmt.Fprintf(&b, "- [%s] %s by %s (%s)\n",
			d.DeployedAt.UTC().Format(time.RFC3339),
			d.Version,
			d.Author,
			d.Status,
		)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// StubDeploymentSource is a no-op DeploymentSource used when no real source
// is configured. It always returns an empty slice.
type StubDeploymentSource struct{}

// RecentDeployments always returns an empty list.
func (s *StubDeploymentSource) RecentDeployments(_ context.Context, _ string, _ time.Duration) ([]Deployment, error) {
	return []Deployment{}, nil
}

// ---------------------------------------------------------------------------
// ServiceInfoEnricher
// ---------------------------------------------------------------------------

// ServiceInfo holds metadata about a service from the service catalog.
type ServiceInfo struct {
	Team         string
	OnCall       string
	Dependencies []string
}

// ServiceCatalog looks up metadata for a named service.
type ServiceCatalog interface {
	GetService(ctx context.Context, name string) (*ServiceInfo, error)
}

// ServiceInfoEnricher looks up service catalog data for the alert's service.
type ServiceInfoEnricher struct {
	catalog ServiceCatalog
}

// NewServiceInfoEnricher constructs the enricher with the provided catalog.
func NewServiceInfoEnricher(catalog ServiceCatalog) *ServiceInfoEnricher {
	return &ServiceInfoEnricher{catalog: catalog}
}

// Enrich fetches service metadata and formats a Markdown section.
func (e *ServiceInfoEnricher) Enrich(ctx context.Context, evt *alert.Event) (string, error) {
	info, err := e.catalog.GetService(ctx, evt.Service)
	if err != nil {
		return "", fmt.Errorf("ServiceInfoEnricher: %w", err)
	}

	var b strings.Builder
	b.WriteString("## Service Info\n")

	team := info.Team
	if team == "" {
		team = "unknown"
	}
	fmt.Fprintf(&b, "- Team: %s\n", team)

	onCall := info.OnCall
	if onCall == "" {
		onCall = "unknown"
	}
	fmt.Fprintf(&b, "- On-call: %s\n", onCall)

	if len(info.Dependencies) > 0 {
		fmt.Fprintf(&b, "- Dependencies: %s\n", strings.Join(info.Dependencies, ", "))
	} else {
		b.WriteString("- Dependencies: none\n")
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// StubServiceCatalog is a no-op ServiceCatalog used when no real catalog is
// configured. It returns an empty ServiceInfo for every service.
type StubServiceCatalog struct{}

// GetService always returns an empty ServiceInfo.
func (s *StubServiceCatalog) GetService(_ context.Context, _ string) (*ServiceInfo, error) {
	return &ServiceInfo{}, nil
}
