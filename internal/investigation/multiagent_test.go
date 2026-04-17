package investigation

import (
	"strings"
	"testing"
)

func makeMultiStep(index int, desc string) Step {
	return Step{Index: index, Description: desc}
}

// ---------------------------------------------------------------------------
// MergeSubInvestigations
// ---------------------------------------------------------------------------

func TestMultiMergeEmpty(t *testing.T) {
	result := MergeSubInvestigations(nil)

	if len(result.SubInvestigations) != 0 {
		t.Errorf("expected 0 sub-investigations, got %d", len(result.SubInvestigations))
	}
	if result.MergedRootCause != "" {
		t.Errorf("expected empty MergedRootCause, got %q", result.MergedRootCause)
	}
	if result.MergedResolution != "" {
		t.Errorf("expected empty MergedResolution, got %q", result.MergedResolution)
	}
	if result.MergedSummary != "" {
		t.Errorf("expected empty MergedSummary, got %q", result.MergedSummary)
	}
	if result.MergedConfidence != 0 {
		t.Errorf("expected confidence 0, got %d", result.MergedConfidence)
	}
}

func TestMultiMergeSinglePassthrough(t *testing.T) {
	sub := SubInvestigation{
		Focus:      "logs",
		Steps:      []Step{makeMultiStep(1, "check logs")},
		RootCause:  "OOM",
		Resolution: "increase memory limit",
		Confidence: 80,
	}

	result := MergeSubInvestigations([]SubInvestigation{sub})

	if len(result.SubInvestigations) != 1 {
		t.Fatalf("expected 1 sub-investigation, got %d", len(result.SubInvestigations))
	}
	if !strings.Contains(result.MergedRootCause, "OOM") {
		t.Errorf("MergedRootCause should contain root cause text; got %q", result.MergedRootCause)
	}
	if !strings.Contains(result.MergedRootCause, "[logs]") {
		t.Errorf("MergedRootCause should contain focus prefix [logs]; got %q", result.MergedRootCause)
	}
	if !strings.Contains(result.MergedResolution, "increase memory limit") {
		t.Errorf("MergedResolution should contain resolution; got %q", result.MergedResolution)
	}
	if result.MergedConfidence != 80 {
		t.Errorf("expected confidence 80, got %d", result.MergedConfidence)
	}
}

func TestMultiMergeMultipleSubs(t *testing.T) {
	subs := []SubInvestigation{
		{
			Focus:      "logs",
			Steps:      []Step{makeMultiStep(1, "check logs"), makeMultiStep(2, "parse errors")},
			RootCause:  "repeated 5xx errors in auth service",
			Resolution: "redeploy auth service",
			Confidence: 70,
		},
		{
			Focus:      "metrics",
			Steps:      []Step{makeMultiStep(1, "check latency")},
			RootCause:  "latency spike to 2000ms",
			Resolution: "scale up replicas",
			Confidence: 90,
		},
		{
			Focus:      "deployments",
			Steps:      []Step{makeMultiStep(1, "check deploy history"), makeMultiStep(2, "compare configs"), makeMultiStep(3, "rollback candidate")},
			RootCause:  "bad config deployed at 10:00",
			Resolution: "rollback deployment v1.2.3",
			Confidence: 95,
		},
	}

	result := MergeSubInvestigations(subs)

	// All three sub-investigations preserved.
	if len(result.SubInvestigations) != 3 {
		t.Errorf("expected 3 sub-investigations, got %d", len(result.SubInvestigations))
	}

	// Root cause contains all three prefixed causes.
	for _, want := range []string{"[logs]", "[metrics]", "[deployments]"} {
		if !strings.Contains(result.MergedRootCause, want) {
			t.Errorf("MergedRootCause missing %q; full value: %q", want, result.MergedRootCause)
		}
	}

	// Resolution contains all three resolutions.
	for _, want := range []string{"redeploy auth service", "scale up replicas", "rollback deployment v1.2.3"} {
		if !strings.Contains(result.MergedResolution, want) {
			t.Errorf("MergedResolution missing %q; full value: %q", want, result.MergedResolution)
		}
	}

	// Summary contains one line per sub-investigation.
	lines := strings.Split(strings.TrimSpace(result.MergedSummary), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 summary lines, got %d; full value: %q", len(lines), result.MergedSummary)
	}
}

func TestMultiMergeSkipsEmptyRootCause(t *testing.T) {
	subs := []SubInvestigation{
		{Focus: "logs", RootCause: "", Resolution: "", Confidence: 50},
		{Focus: "metrics", RootCause: "high error rate", Resolution: "fix db pool", Confidence: 60},
	}

	result := MergeSubInvestigations(subs)

	// Only the non-empty root cause should appear.
	if strings.Contains(result.MergedRootCause, "[logs]") {
		t.Errorf("MergedRootCause should not contain [logs] prefix when root cause is empty; got %q", result.MergedRootCause)
	}
	if !strings.Contains(result.MergedRootCause, "[metrics]") {
		t.Errorf("MergedRootCause should contain [metrics]; got %q", result.MergedRootCause)
	}

	// Only the non-empty resolution should appear.
	if !strings.Contains(result.MergedResolution, "fix db pool") {
		t.Errorf("MergedResolution missing expected text; got %q", result.MergedResolution)
	}
}

// ---------------------------------------------------------------------------
// Weighted confidence
// ---------------------------------------------------------------------------

func TestMultiConfidenceWeightedAverage(t *testing.T) {
	// logs: 1 step, confidence 100  → weight 1, contribution 100
	// metrics: 3 steps, confidence 0 → weight 3, contribution 0
	// total weight = 4, expected = (100 + 0) / 4 = 25
	subs := []SubInvestigation{
		{
			Focus:      "logs",
			Steps:      []Step{makeMultiStep(1, "s1")},
			Confidence: 100,
		},
		{
			Focus:      "metrics",
			Steps:      []Step{makeMultiStep(1, "s1"), makeMultiStep(2, "s2"), makeMultiStep(3, "s3")},
			Confidence: 0,
		},
	}

	result := MergeSubInvestigations(subs)

	if result.MergedConfidence != 25 {
		t.Errorf("expected weighted confidence 25, got %d", result.MergedConfidence)
	}
}

func TestMultiConfidenceZeroStepsUsesWeightOne(t *testing.T) {
	// Two subs with no steps — each gets minimum weight 1.
	// confidence: 40 and 60 → (40*1 + 60*1) / 2 = 50
	subs := []SubInvestigation{
		{Focus: "logs", Confidence: 40},
		{Focus: "metrics", Confidence: 60},
	}

	result := MergeSubInvestigations(subs)

	if result.MergedConfidence != 50 {
		t.Errorf("expected confidence 50 for equal-weight subs, got %d", result.MergedConfidence)
	}
}

// ---------------------------------------------------------------------------
// ShouldUseMultiAgent
// ---------------------------------------------------------------------------

func TestMultiShouldUseMultiAgentCritical(t *testing.T) {
	cases := []struct {
		severity string
		want     bool
	}{
		{"critical", true},
		{"CRITICAL", true},
		{"Critical", true},
		{"warning", false},
		{"info", false},
		{"", false},
		{"high", false},
	}

	for _, tc := range cases {
		got := ShouldUseMultiAgent(tc.severity, "any-service")
		if got != tc.want {
			t.Errorf("ShouldUseMultiAgent(%q, ...) = %v, want %v", tc.severity, got, tc.want)
		}
	}
}

func TestMultiShouldUseMultiAgentServiceIgnored(t *testing.T) {
	// Service name must not affect the result — only severity matters.
	if ShouldUseMultiAgent("warning", "payment-service") {
		t.Error("expected false for warning severity regardless of service")
	}
	if !ShouldUseMultiAgent("critical", "payment-service") {
		t.Error("expected true for critical severity regardless of service")
	}
}

// ---------------------------------------------------------------------------
// FocusAreas
// ---------------------------------------------------------------------------

func TestMultiFocusAreasContainsDefaults(t *testing.T) {
	required := []string{"logs", "metrics", "deployments"}
	for _, r := range required {
		found := false
		for _, f := range FocusAreas {
			if f == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FocusAreas missing required entry %q; got %v", r, FocusAreas)
		}
	}
}

// ---------------------------------------------------------------------------
// Immutability: result must not share slice memory with input
// ---------------------------------------------------------------------------

func TestMultiMergeDoesNotMutateInput(t *testing.T) {
	original := SubInvestigation{
		Focus:      "logs",
		Steps:      []Step{makeMultiStep(1, "original")},
		RootCause:  "original cause",
		Resolution: "original resolution",
		Confidence: 55,
	}
	input := []SubInvestigation{original}

	result := MergeSubInvestigations(input)

	// Mutate the result's copy — the original must remain unchanged.
	result.SubInvestigations[0].RootCause = "mutated"

	if input[0].RootCause != "original cause" {
		t.Error("MergeSubInvestigations mutated the input slice")
	}
}
