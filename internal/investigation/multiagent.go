package investigation

import "strings"

// FocusAreas is the default set of investigation focus areas used in
// multi-agent collaboration.
var FocusAreas = []string{"logs", "metrics", "deployments"}

// SubInvestigation holds the findings from a single focused sub-agent.
type SubInvestigation struct {
	Focus      string // e.g. "logs", "metrics", "deployments"
	Steps      []Step
	RootCause  string
	Resolution string
	Confidence int // 0-100
}

// CollaborationResult is the merged output produced by MergeSubInvestigations.
type CollaborationResult struct {
	SubInvestigations []SubInvestigation
	MergedRootCause   string
	MergedResolution  string
	MergedSummary     string
	MergedConfidence  int // weighted average, 0-100
}

// MergeSubInvestigations combines results from multiple focused sub-agents into
// a single CollaborationResult. It does not mutate any of the input values.
//
// Merging rules:
//   - MergedRootCause: non-empty root causes concatenated with newlines, each
//     prefixed by its focus area.
//   - MergedResolution: non-empty resolutions concatenated with newlines.
//   - MergedSummary: one line per sub-investigation describing focus, root
//     cause, and resolution.
//   - MergedConfidence: weighted average of sub-investigation confidences where
//     the weight of each sub-investigation is its step count (minimum weight 1).
func MergeSubInvestigations(subs []SubInvestigation) CollaborationResult {
	if len(subs) == 0 {
		return CollaborationResult{}
	}

	var rootCauseParts []string
	var resolutionParts []string
	var summaryParts []string
	var allSteps []Step

	totalWeight := 0
	weightedConfidence := 0

	for _, sub := range subs {
		// Collect steps.
		allSteps = append(allSteps, sub.Steps...)

		// Root cause — prefix with focus area when non-empty.
		if sub.RootCause != "" {
			rootCauseParts = append(rootCauseParts,
				"["+sub.Focus+"] "+sub.RootCause)
		}

		// Resolution — append non-empty values.
		if sub.Resolution != "" {
			resolutionParts = append(resolutionParts, sub.Resolution)
		}

		// Summary line.
		line := "Focus: " + sub.Focus +
			" | Root cause: " + nonEmpty(sub.RootCause, "N/A") +
			" | Resolution: " + nonEmpty(sub.Resolution, "N/A")
		summaryParts = append(summaryParts, line)

		// Weighted confidence — weight is max(1, stepCount).
		weight := max(len(sub.Steps), 1)
		totalWeight += weight
		weightedConfidence += sub.Confidence * weight
	}

	mergedConfidence := 0
	if totalWeight > 0 {
		mergedConfidence = weightedConfidence / totalWeight
	}

	// Snapshot the input slice so the result owns its own copy.
	subsCopy := make([]SubInvestigation, len(subs))
	copy(subsCopy, subs)

	return CollaborationResult{
		SubInvestigations: subsCopy,
		MergedRootCause:   strings.Join(rootCauseParts, "\n"),
		MergedResolution:  strings.Join(resolutionParts, "\n"),
		MergedSummary:     strings.Join(summaryParts, "\n"),
		MergedConfidence:  mergedConfidence,
	}
}

// ShouldUseMultiAgent returns true when the alert warrants spawning multiple
// parallel sub-agents. Currently only critical-severity alerts qualify.
func ShouldUseMultiAgent(severity string, _ string) bool {
	return strings.EqualFold(severity, "critical")
}

// nonEmpty returns s when it is non-empty, otherwise fallback.
func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
