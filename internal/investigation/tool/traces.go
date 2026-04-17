package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentinelai/sentinel/internal/llm"
)

var QueryTracesTool = llm.Tool{
	Name:        "query_traces",
	Description: "Query distributed traces for a service. Returns slow or error traces to help identify performance bottlenecks and failure points in the request chain.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"service":      {"type": "string", "description": "The service name to query traces for"},
			"time_range":   {"type": "string", "description": "Time range to query, e.g. '30m', '1h'"},
			"min_duration": {"type": "string", "description": "Minimum trace duration to filter, e.g. '1s', '500ms'"},
			"status":       {"type": "string", "description": "Filter by status: 'error', 'ok', or empty for all"}
		},
		"required": ["service", "time_range"]
	}`),
}

type QueryTracesInput struct {
	Service     string `json:"service"`
	TimeRange   string `json:"time_range"`
	MinDuration string `json:"min_duration"`
	Status      string `json:"status"`
}

// TraceSource abstracts trace retrieval (ARMS, Jaeger, Tempo, etc.).
type TraceSource interface {
	QueryTraces(ctx context.Context, q TraceQuery) (*TraceResult, error)
}

type TraceQuery struct {
	Service     string
	From        time.Time
	To          time.Time
	MinDuration time.Duration
	StatusFilter string // "error", "ok", ""
	Limit       int
}

type Trace struct {
	TraceID    string        `json:"trace_id"`
	Operation  string        `json:"operation"`
	Duration   time.Duration `json:"duration"`
	Status     string        `json:"status"`
	SpanCount  int           `json:"span_count"`
	ErrorMsg   string        `json:"error_msg,omitempty"`
	StartTime  time.Time     `json:"start_time"`
}

type TraceResult struct {
	Traces  []Trace
	Summary string
}

// QueryTraces returns a handler. If source is nil, returns a stub response.
func QueryTraces(source TraceSource) Func {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in QueryTracesInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}

		if source == nil {
			return fmt.Sprintf(
				"[query_traces] service=%s time_range=%s\n"+
					"No trace source configured. Connect ARMS, Jaeger, or Tempo in config (data_sources.traces).",
				in.Service, in.TimeRange,
			), nil
		}

		from, to := parseTimeRange(in.TimeRange)
		minDur, _ := time.ParseDuration(in.MinDuration)

		result, err := source.QueryTraces(ctx, TraceQuery{
			Service:      in.Service,
			From:         from,
			To:           to,
			MinDuration:  minDur,
			StatusFilter: in.Status,
			Limit:        20,
		})
		if err != nil {
			return "", fmt.Errorf("query_traces: %w", err)
		}

		if result.Summary != "" {
			return result.Summary, nil
		}

		if len(result.Traces) == 0 {
			return fmt.Sprintf("No traces found for %s in the last %s.", in.Service, in.TimeRange), nil
		}

		var out string
		out = fmt.Sprintf("Found %d traces for %s:\n\n", len(result.Traces), in.Service)
		for _, tr := range result.Traces {
			line := fmt.Sprintf("- [%s] %s — %s (%d spans)",
				tr.StartTime.Format("15:04:05"),
				tr.Operation, tr.Duration, tr.SpanCount,
			)
			if tr.ErrorMsg != "" {
				line += fmt.Sprintf(" ERROR: %s", tr.ErrorMsg)
			}
			out += line + "\n"
		}
		return out, nil
	}
}
