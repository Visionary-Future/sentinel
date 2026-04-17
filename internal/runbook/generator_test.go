package runbook

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockStore implements GeneratorStore for testing.
type mockStore struct {
	summaries []InvestigationSummary
	err       error
}

func (m *mockStore) FindCompletedByService(_ context.Context, _ string, _ int) ([]InvestigationSummary, error) {
	return m.summaries, m.err
}

// newSummary is a convenience constructor for InvestigationSummary.
func newSummary(rootCause string, steps ...string) InvestigationSummary {
	return InvestigationSummary{
		AlertTitle:  "test alert",
		Service:     "svc",
		RootCause:   rootCause,
		Resolution:  "resolved",
		Steps:       steps,
		CompletedAt: time.Now(),
	}
}

// ---------------------------------------------------------------------------
// stringSimilarity / Jaccard tests
// ---------------------------------------------------------------------------

func TestStringSimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    string
		wantGTE float64 // minimum expected similarity
		wantLTE float64 // maximum expected similarity
	}{
		{
			name:    "identical strings",
			a:       "database connection timeout",
			b:       "database connection timeout",
			wantGTE: 1.0,
			wantLTE: 1.0,
		},
		{
			name:    "similar strings share most words",
			a:       "database connection timeout error",
			b:       "database connection timeout failure",
			wantGTE: 0.5,
			wantLTE: 1.0,
		},
		{
			name:    "completely different strings",
			a:       "cpu spike detected",
			b:       "disk full error",
			wantGTE: 0.0,
			wantLTE: 0.2,
		},
		{
			name:    "empty a returns zero",
			a:       "",
			b:       "something here",
			wantGTE: 0.0,
			wantLTE: 0.0,
		},
		{
			name:    "empty b returns zero",
			a:       "something here",
			b:       "",
			wantGTE: 0.0,
			wantLTE: 0.0,
		},
		{
			name:    "both empty returns zero",
			a:       "",
			b:       "",
			wantGTE: 0.0,
			wantLTE: 0.0,
		},
		{
			name:    "single shared word",
			a:       "timeout",
			b:       "timeout",
			wantGTE: 1.0,
			wantLTE: 1.0,
		},
		{
			name:    "case insensitive comparison",
			a:       "Database Connection Timeout",
			b:       "database connection timeout",
			wantGTE: 1.0,
			wantLTE: 1.0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stringSimilarity(tc.a, tc.b)
			if got < tc.wantGTE || got > tc.wantLTE {
				t.Errorf("stringSimilarity(%q, %q) = %.4f; want [%.4f, %.4f]",
					tc.a, tc.b, got, tc.wantGTE, tc.wantLTE)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// clusterByCause tests
// ---------------------------------------------------------------------------

func TestClusterByCause(t *testing.T) {
	t.Parallel()

	t.Run("similar root causes land in same cluster", func(t *testing.T) {
		t.Parallel()
		// All three strings share "database connection timeout" (3 words) with at
		// most one differing word, giving Jaccard similarity of 3/4 = 0.75 > 0.6.
		// The first summary is inserted as the cluster key; subsequent summaries
		// are compared against it and must exceed the 0.6 threshold.
		summaries := []InvestigationSummary{
			newSummary("database connection timeout exhausted"),
			newSummary("database connection timeout exhausted pool"),
			newSummary("database connection timeout exhausted resource"),
		}

		clusters := clusterByCause(summaries)

		if len(clusters) != 1 {
			t.Errorf("expected 1 cluster, got %d: %v", len(clusters), clusterKeys(clusters))
		}
		for _, group := range clusters {
			if len(group) != 3 {
				t.Errorf("expected 3 investigations in cluster, got %d", len(group))
			}
		}
	})

	t.Run("distinct root causes form separate clusters", func(t *testing.T) {
		t.Parallel()
		summaries := []InvestigationSummary{
			newSummary("database connection timeout"),
			newSummary("cpu spike high load"),
		}

		clusters := clusterByCause(summaries)

		if len(clusters) != 2 {
			t.Errorf("expected 2 clusters, got %d", len(clusters))
		}
	})

	t.Run("empty root cause is skipped", func(t *testing.T) {
		t.Parallel()
		summaries := []InvestigationSummary{
			newSummary(""),
			newSummary(""),
		}

		clusters := clusterByCause(summaries)

		if len(clusters) != 0 {
			t.Errorf("expected 0 clusters for empty root causes, got %d", len(clusters))
		}
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		t.Parallel()
		clusters := clusterByCause(nil)
		if len(clusters) != 0 {
			t.Errorf("expected empty map, got %d clusters", len(clusters))
		}
	})

	t.Run("root cause longer than 100 chars is truncated for key", func(t *testing.T) {
		t.Parallel()
		long := "database connection timeout error occurred because the pool was exhausted and could not acquire a new connection from the pool"
		summaries := []InvestigationSummary{
			newSummary(long),
			newSummary(long),
		}
		clusters := clusterByCause(summaries)
		// Both should land in one cluster (same long key, truncated identically).
		if len(clusters) != 1 {
			t.Errorf("expected 1 cluster, got %d", len(clusters))
		}
	})
}

// ---------------------------------------------------------------------------
// extractCommonSteps tests
// ---------------------------------------------------------------------------

func TestExtractCommonSteps(t *testing.T) {
	t.Parallel()

	t.Run("steps appearing in majority of investigations are returned", func(t *testing.T) {
		t.Parallel()
		group := []InvestigationSummary{
			newSummary("cause", "check logs", "restart service", "verify health"),
			newSummary("cause", "check logs", "restart service", "notify team"),
			newSummary("cause", "check logs", "restart service", "scale pods"),
		}

		common := extractCommonSteps(group)
		commonSet := toSet(common)

		if !commonSet["check logs"] {
			t.Error("expected 'check logs' in common steps")
		}
		if !commonSet["restart service"] {
			t.Error("expected 'restart service' in common steps")
		}
	})

	t.Run("steps appearing in only one investigation are excluded", func(t *testing.T) {
		t.Parallel()
		// With 4 investigations minFreq = 4/2 = 2.
		// "unique step only here" appears once → excluded.
		// "check logs" appears in all 4 → included.
		group := []InvestigationSummary{
			newSummary("cause", "check logs", "unique step only here"),
			newSummary("cause", "check logs"),
			newSummary("cause", "check logs"),
			newSummary("cause", "check logs"),
		}

		common := extractCommonSteps(group)
		commonSet := toSet(common)

		if commonSet["unique step only here"] {
			t.Error("unique step should not appear in common steps")
		}
		if !commonSet["check logs"] {
			t.Error("expected 'check logs' in common steps")
		}
	})

	t.Run("steps are normalised (trimmed, lowercased)", func(t *testing.T) {
		t.Parallel()
		group := []InvestigationSummary{
			newSummary("cause", "  Check Logs  "),
			newSummary("cause", "check logs"),
		}

		common := extractCommonSteps(group)
		commonSet := toSet(common)

		if !commonSet["check logs"] {
			t.Errorf("expected normalised 'check logs' in common steps; got %v", common)
		}
	})

	t.Run("empty steps list returns empty result", func(t *testing.T) {
		t.Parallel()
		group := []InvestigationSummary{
			newSummary("cause"),
			newSummary("cause"),
		}

		common := extractCommonSteps(group)
		if len(common) != 0 {
			t.Errorf("expected empty common steps, got %v", common)
		}
	})

	t.Run("single investigation with steps returns all steps", func(t *testing.T) {
		t.Parallel()
		group := []InvestigationSummary{
			newSummary("cause", "step one", "step two"),
		}

		common := extractCommonSteps(group)
		if len(common) == 0 {
			t.Error("expected steps to be returned for single investigation")
		}
	})
}

// ---------------------------------------------------------------------------
// ProposeForService tests
// ---------------------------------------------------------------------------

func TestProposeForService(t *testing.T) {
	t.Parallel()

	// Helper to build a generator with no real repository (Save is not called).
	newGen := func(summaries []InvestigationSummary, threshold int) *Generator {
		store := &mockStore{summaries: summaries}
		return NewGenerator(store, nil, threshold)
	}

	t.Run("returns nil when fewer summaries than threshold", func(t *testing.T) {
		t.Parallel()
		gen := newGen([]InvestigationSummary{
			newSummary("db timeout", "check logs"),
			newSummary("db timeout", "check logs"),
		}, 3)

		got, err := gen.ProposeForService(context.Background(), "svc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil proposal, got %+v", got)
		}
	})

	t.Run("returns nil for empty investigations list", func(t *testing.T) {
		t.Parallel()
		gen := newGen(nil, 2)

		got, err := gen.ProposeForService(context.Background(), "svc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil proposal, got %+v", got)
		}
	})

	t.Run("returns nil for single investigation", func(t *testing.T) {
		t.Parallel()
		gen := newGen([]InvestigationSummary{
			newSummary("db timeout", "check logs"),
		}, 2)

		got, err := gen.ProposeForService(context.Background(), "svc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil proposal, got %+v", got)
		}
	})

	t.Run("returns nil when cluster has no common steps", func(t *testing.T) {
		t.Parallel()
		// Four summaries share a similar root cause but each has a unique step.
		// minFreq = 4/2 = 2, so no step (frequency 1) meets the threshold.
		// Cause words are 4 common + 1 unique → Jaccard ≥ 4/5 = 0.8 > 0.6, so
		// all land in the same cluster and meet the threshold of 4.
		gen := newGen([]InvestigationSummary{
			newSummary("database connection timeout exhausted pool", "unique step a"),
			newSummary("database connection timeout exhausted queue", "unique step b"),
			newSummary("database connection timeout exhausted resource", "unique step c"),
			newSummary("database connection timeout exhausted socket", "unique step d"),
		}, 4)

		got, err := gen.ProposeForService(context.Background(), "svc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil proposal when no common steps, got %+v", got)
		}
	})

	t.Run("proposed runbook has correct fields", func(t *testing.T) {
		t.Parallel()
		service := "payment-service"
		// Root causes share 4 words; the 5th differs → Jaccard = 4/5 = 0.8 > 0.6.
		gen := NewGenerator(&mockStore{
			summaries: []InvestigationSummary{
				{Service: service, RootCause: "database connection timeout exhausted pool", Steps: []string{"check logs", "restart db"}},
				{Service: service, RootCause: "database connection timeout exhausted queue", Steps: []string{"check logs", "restart db"}},
				{Service: service, RootCause: "database connection timeout exhausted resource", Steps: []string{"check logs", "restart db"}},
			},
		}, nil, 3)

		got, err := gen.ProposeForService(context.Background(), service)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected a proposed runbook, got nil")
		}

		if got.Service != service {
			t.Errorf("Service = %q; want %q", got.Service, service)
		}
		if got.SourceCount < 3 {
			t.Errorf("SourceCount = %d; want >= 3", got.SourceCount)
		}
		if got.Name == "" {
			t.Error("Name must not be empty")
		}
		if got.Description == "" {
			t.Error("Description must not be empty")
		}
		if got.CommonCause == "" {
			t.Error("CommonCause must not be empty")
		}
		if len(got.Steps) == 0 {
			t.Error("Steps must not be empty")
		}
	})

	t.Run("name and description mention the service", func(t *testing.T) {
		t.Parallel()
		service := "order-svc"
		// 4 shared words; 5th differs → Jaccard = 4/5 = 0.8 > 0.6.
		gen := NewGenerator(&mockStore{
			summaries: []InvestigationSummary{
				{Service: service, RootCause: "cpu spike high load persistent", Steps: []string{"check metrics", "scale out"}},
				{Service: service, RootCause: "cpu spike high load elevated", Steps: []string{"check metrics", "scale out"}},
				{Service: service, RootCause: "cpu spike high load sustained", Steps: []string{"check metrics", "scale out"}},
			},
		}, nil, 3)

		got, err := gen.ProposeForService(context.Background(), service)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected a proposed runbook, got nil")
		}

		if !contains(got.Name, service) {
			t.Errorf("Name %q does not mention service %q", got.Name, service)
		}
		if !contains(got.Description, service) {
			t.Errorf("Description %q does not mention service %q", got.Description, service)
		}
	})

	t.Run("store error is propagated", func(t *testing.T) {
		t.Parallel()
		storeErr := errors.New("db unavailable")
		gen := NewGenerator(&mockStore{err: storeErr}, nil, 1)

		_, err := gen.ProposeForService(context.Background(), "svc")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, storeErr) {
			t.Errorf("error = %v; want to wrap %v", err, storeErr)
		}
	})

	t.Run("threshold default applied when zero given", func(t *testing.T) {
		t.Parallel()
		gen := NewGenerator(&mockStore{
			summaries: []InvestigationSummary{
				newSummary("db timeout error", "check logs"),
				newSummary("db timeout failure", "check logs"),
			},
		}, nil, 0) // 0 should default to 3

		got, err := gen.ProposeForService(context.Background(), "svc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Only 2 summaries, default threshold is 3 → should return nil.
		if got != nil {
			t.Errorf("expected nil when below default threshold, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// buildRunbookContent tests
// ---------------------------------------------------------------------------

func TestBuildRunbookContent(t *testing.T) {
	t.Parallel()

	proposed := &ProposedRunbook{
		Name:        "Auto: svc investigation",
		Description: "Auto-generated from 3 similar investigations for svc",
		Service:     "svc",
		Steps:       []string{"check logs", "restart service"},
		CommonCause: "database connection timeout",
		SourceCount: 3,
	}

	content := buildRunbookContent(proposed)

	for _, want := range []string{
		proposed.Name,
		proposed.Description,
		proposed.CommonCause,
		"check logs",
		"restart service",
		"## Investigation Steps",
		"## Common Root Cause",
	} {
		if !contains(content, want) {
			t.Errorf("buildRunbookContent output missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func clusterKeys(m map[string][]InvestigationSummary) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func toSet(ss []string) map[string]bool {
	s := make(map[string]bool, len(ss))
	for _, v := range ss {
		s[v] = true
	}
	return s
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
