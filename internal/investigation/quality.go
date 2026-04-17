package investigation

const QualityThreshold = 50

// QualityScore holds the result of scoring an investigation.
type QualityScore struct {
	Total       int            // 0-100
	Breakdown   map[string]int // component name → points awarded
	NeedsReview bool
}

// ScoreInvestigation scores an investigation across six dimensions and
// returns a QualityScore. It never mutates the supplied Investigation.
func ScoreInvestigation(inv *Investigation) QualityScore {
	breakdown := make(map[string]int, 6)

	// HasRootCause — 25 pts
	if inv.RootCause != "" {
		breakdown["HasRootCause"] = 25
	}

	// HasResolution — 20 pts
	if inv.Resolution != "" {
		breakdown["HasResolution"] = 20
	}

	// HasSummary — 10 pts
	if inv.Summary != "" {
		breakdown["HasSummary"] = 10
	}

	// ConfidenceLevel — 0-15 pts, proportional to Confidence (0-100)
	breakdown["ConfidenceLevel"] = (inv.Confidence * 15) / 100

	// DataSourcesDiversity — count unique tool names across all steps
	breakdown["DataSourcesDiversity"] = scoreToolDiversity(inv.Steps)

	// StepCount — 15 pts tiered
	breakdown["StepCount"] = scoreStepCount(len(inv.Steps))

	total := 0
	for _, v := range breakdown {
		total += v
	}
	if total > 100 {
		total = 100
	}

	needsReview := total < QualityThreshold || inv.Confidence < 30

	return QualityScore{
		Total:       total,
		Breakdown:   breakdown,
		NeedsReview: needsReview,
	}
}

// scoreToolDiversity returns the DataSourcesDiversity component score.
func scoreToolDiversity(steps []Step) int {
	seen := make(map[string]struct{})
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			seen[tc.Name] = struct{}{}
		}
	}
	switch {
	case len(seen) >= 3:
		return 15
	case len(seen) == 2:
		return 10
	case len(seen) == 1:
		return 5
	default:
		return 0
	}
}

// scoreStepCount returns the StepCount component score.
func scoreStepCount(n int) int {
	switch {
	case n >= 6:
		return 15
	case n >= 3:
		return 10
	case n >= 1:
		return 5
	default:
		return 0
	}
}
