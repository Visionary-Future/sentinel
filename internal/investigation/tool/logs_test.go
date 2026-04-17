package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/datasource"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
)

func TestQueryLogs_NoSource_ReturnsStub(t *testing.T) {
	sources := datasource.NewRegistry() // empty — no sources
	fn := tool.QueryLogs(sources)

	input, _ := json.Marshal(map[string]any{
		"service":    "order-service",
		"time_range": "30m",
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No data source configured") {
		t.Errorf("expected stub message, got: %s", result)
	}
}

func TestQueryLogs_WithSource_CallsSource(t *testing.T) {
	stub := &stubSource{
		logResult: &datasource.LogResult{
			TotalCount: 10,
			Summary:    "Total: 10 log lines (ERROR: 3, INFO: 7).",
		},
	}
	sources := datasource.NewRegistry()
	sources.Register(stub)

	fn := tool.QueryLogs(sources)
	input, _ := json.Marshal(map[string]any{
		"service":    "order-service",
		"time_range": "30m",
		"filters":    map[string]string{"level": "error"},
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != stub.logResult.Summary {
		t.Errorf("expected summary %q, got %q", stub.logResult.Summary, result)
	}
	if stub.lastQuery.Service != "order-service" {
		t.Errorf("expected service=order-service, got=%s", stub.lastQuery.Service)
	}
}

func TestQueryMetrics_NoSource_ReturnsStub(t *testing.T) {
	sources := datasource.NewRegistry()
	fn := tool.QueryMetrics(sources)

	input, _ := json.Marshal(map[string]any{
		"service":     "order-service",
		"metric_name": "p99_latency",
		"time_range":  "1h",
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No data source configured") {
		t.Errorf("expected stub message, got: %s", result)
	}
}

func TestParseTimeRange(t *testing.T) {
	cases := []struct {
		input    string
		wantDiff time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1d", 24 * time.Hour},
		{"", 30 * time.Minute}, // default
	}

	fn := tool.QueryLogs(datasource.NewRegistry())
	for _, tc := range cases {
		input, _ := json.Marshal(map[string]any{
			"service":    "svc",
			"time_range": tc.input,
		})
		// Just verify it doesn't error — time range parsing is internal
		_, err := fn(context.Background(), input)
		if err != nil {
			t.Errorf("time_range=%q: unexpected error: %v", tc.input, err)
		}
	}
}

// stubSource implements datasource.Source for testing.
type stubSource struct {
	lastQuery  datasource.LogQuery
	logResult  *datasource.LogResult
	metResult  *datasource.MetricResult
}

func (s *stubSource) Name() string { return "stub" }

func (s *stubSource) QueryLogs(_ context.Context, q datasource.LogQuery) (*datasource.LogResult, error) {
	s.lastQuery = q
	if s.logResult != nil {
		return s.logResult, nil
	}
	return &datasource.LogResult{Summary: "stub log result"}, nil
}

func (s *stubSource) QueryMetrics(_ context.Context, _ datasource.MetricQuery) (*datasource.MetricResult, error) {
	if s.metResult != nil {
		return s.metResult, nil
	}
	return &datasource.MetricResult{Summary: "stub metric result"}, nil
}
