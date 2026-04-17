package investigation

import (
	"testing"
)

func TestScoreInvestigation(t *testing.T) {
	t.Parallel()

	makeToolCall := func(name string) ToolCall {
		return ToolCall{ID: name, Name: name}
	}

	makeStep := func(tools ...string) Step {
		calls := make([]ToolCall, 0, len(tools))
		for _, name := range tools {
			calls = append(calls, makeToolCall(name))
		}
		return Step{ToolCalls: calls}
	}

	tests := []struct {
		name            string
		inv             Investigation
		wantMinTotal    int
		wantMaxTotal    int
		wantNeedsReview bool
		wantBreakdown   map[string]int // optional spot checks; zero value = skip
	}{
		{
			name: "high quality — all fields, many steps, high confidence",
			inv: Investigation{
				RootCause:  "database connection pool exhausted",
				Resolution: "increased pool size to 200 and added idle timeout",
				Summary:    "connection pool saturation caused cascading timeouts",
				Confidence: 90,
				Steps: []Step{
					makeStep("query_logs"),
					makeStep("query_metrics"),
					makeStep("search_history"),
					makeStep("query_logs"),
					makeStep("query_metrics"),
					makeStep("search_history"),
				},
			},
			wantMinTotal:    80,
			wantMaxTotal:    100,
			wantNeedsReview: false,
		},
		{
			name: "minimal — only root cause set",
			inv: Investigation{
				RootCause:  "OOM kill",
				Confidence: 0,
			},
			wantMinTotal:    0,
			wantMaxTotal:    49, // must be < threshold → needs review
			wantNeedsReview: true,
		},
		{
			name:            "empty investigation",
			inv:             Investigation{},
			wantMinTotal:    0,
			wantMaxTotal:    0,
			wantNeedsReview: true,
		},
		{
			name: "diverse tools boost score",
			inv: Investigation{
				RootCause:  "spike in error rate",
				Confidence: 50,
				Steps: []Step{
					makeStep("query_logs"),
					makeStep("query_metrics"),
					makeStep("search_history"),
				},
			},
			// HasRootCause(25) + ConfidenceLevel(7) + DataSourcesDiversity(15) + StepCount(10) = 57
			wantMinTotal:    55,
			wantMaxTotal:    100,
			wantNeedsReview: false,
			wantBreakdown: map[string]int{
				"DataSourcesDiversity": 15,
				"HasRootCause":         25,
			},
		},
		{
			name: "confidence 0 vs 100 — lower confidence produces lower score",
			inv: Investigation{
				RootCause:  "disk full",
				Resolution: "cleared old logs",
				Summary:    "disk usage hit 100%",
				Confidence: 0,
				Steps:      []Step{makeStep("query_metrics")},
			},
			// HasRootCause(25)+HasResolution(20)+HasSummary(10)+Confidence(0)+Diversity(5)+Steps(5) = 65
			// NeedsReview because Confidence < 30
			wantMinTotal:    60,
			wantMaxTotal:    70,
			wantNeedsReview: true,
			wantBreakdown: map[string]int{
				"ConfidenceLevel": 0,
			},
		},
		{
			name: "confidence 100 — maximum confidence points",
			inv: Investigation{
				RootCause:  "disk full",
				Resolution: "cleared old logs",
				Summary:    "disk usage hit 100%",
				Confidence: 100,
				Steps:      []Step{makeStep("query_metrics")},
			},
			// HasRootCause(25)+HasResolution(20)+HasSummary(10)+Confidence(15)+Diversity(5)+Steps(5) = 80
			wantMinTotal:    80,
			wantMaxTotal:    100,
			wantNeedsReview: false,
			wantBreakdown: map[string]int{
				"ConfidenceLevel": 15,
			},
		},
		{
			name: "one unique tool — 5 pts diversity",
			inv: Investigation{
				Confidence: 80,
				Steps: []Step{
					makeStep("query_logs"),
					makeStep("query_logs"),
				},
			},
			wantBreakdown: map[string]int{
				"DataSourcesDiversity": 5,
			},
			wantMinTotal:    0,
			wantMaxTotal:    100,
			wantNeedsReview: true, // no root cause → total < 50
		},
		{
			name: "two unique tools — 10 pts diversity",
			inv: Investigation{
				Confidence: 80,
				Steps: []Step{
					makeStep("query_logs"),
					makeStep("query_metrics"),
				},
			},
			wantBreakdown: map[string]int{
				"DataSourcesDiversity": 10,
			},
			wantMinTotal:    0,
			wantMaxTotal:    100,
			wantNeedsReview: true,
		},
		{
			name: "step tiers — 1 step gives 5 pts",
			inv: Investigation{
				Confidence: 0,
				Steps:      []Step{makeStep()},
			},
			wantBreakdown: map[string]int{
				"StepCount": 5,
			},
			wantMinTotal:    0,
			wantMaxTotal:    100,
			wantNeedsReview: true,
		},
		{
			name: "step tiers — 3 steps gives 10 pts",
			inv: Investigation{
				Confidence: 0,
				Steps:      []Step{makeStep(), makeStep(), makeStep()},
			},
			wantBreakdown: map[string]int{
				"StepCount": 10,
			},
			wantMinTotal:    0,
			wantMaxTotal:    100,
			wantNeedsReview: true,
		},
		{
			name: "step tiers — 6 steps gives 15 pts",
			inv: Investigation{
				Confidence: 0,
				Steps:      []Step{makeStep(), makeStep(), makeStep(), makeStep(), makeStep(), makeStep()},
			},
			wantBreakdown: map[string]int{
				"StepCount": 15,
			},
			wantMinTotal:    0,
			wantMaxTotal:    100,
			wantNeedsReview: true,
		},
		{
			name: "total capped at 100",
			inv: Investigation{
				RootCause:  "x",
				Resolution: "y",
				Summary:    "z",
				Confidence: 100,
				Steps: []Step{
					makeStep("query_logs"),
					makeStep("query_metrics"),
					makeStep("search_history"),
					makeStep("query_logs"),
					makeStep("query_metrics"),
					makeStep("search_history"),
				},
			},
			// raw = 25+20+10+15+15+15 = 100; cap ensures it does not exceed 100
			wantMinTotal:    100,
			wantMaxTotal:    100,
			wantNeedsReview: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ScoreInvestigation(&tc.inv)

			if got.Total < tc.wantMinTotal || got.Total > tc.wantMaxTotal {
				t.Errorf("Total = %d, want [%d, %d]", got.Total, tc.wantMinTotal, tc.wantMaxTotal)
			}

			if got.NeedsReview != tc.wantNeedsReview {
				t.Errorf("NeedsReview = %v, want %v (Total=%d, Confidence=%d)",
					got.NeedsReview, tc.wantNeedsReview, got.Total, tc.inv.Confidence)
			}

			for component, wantPts := range tc.wantBreakdown {
				if got.Breakdown[component] != wantPts {
					t.Errorf("Breakdown[%q] = %d, want %d", component, got.Breakdown[component], wantPts)
				}
			}
		})
	}
}

func TestQualityThreshold(t *testing.T) {
	if QualityThreshold != 50 {
		t.Errorf("QualityThreshold = %d, want 50", QualityThreshold)
	}
}
