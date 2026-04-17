package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentinelai/sentinel/internal/datasource"
	"github.com/sentinelai/sentinel/internal/llm"
)

// QueryLogsInput is the argument schema for the query_logs tool.
type QueryLogsInput struct {
	Service   string            `json:"service"`
	TimeRange string            `json:"time_range"` // e.g. "30m", "2h", "1d"
	Filters   map[string]string `json:"filters"`    // e.g. {"level": "error"}
	Limit     int               `json:"limit"`
}

var QueryLogsTool = llm.Tool{
	Name:        "query_logs",
	Description: "Query application logs for a service within a time range. Use this to find error patterns, exception stacks, and log-level distributions.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"service":    {"type": "string", "description": "The service name to query logs for"},
			"time_range": {"type": "string", "description": "Time range to query, e.g. '30m', '2h', '1d'"},
			"filters":    {"type": "object", "description": "Key-value filters, e.g. {\"level\": \"error\", \"status_code\": \"500\"}"},
			"limit":      {"type": "integer", "description": "Max number of log lines to return (default 100)"}
		},
		"required": ["service", "time_range"]
	}`),
}

// QueryLogs returns a handler that queries the configured data source registry.
// Falls back to a stub response when no source is registered.
func QueryLogs(sources *datasource.Registry) func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in QueryLogsInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
		if in.Limit == 0 {
			in.Limit = 100
		}

		src := sources.Default()
		if src == nil {
			return fmt.Sprintf(
				"[query_logs] service=%s time_range=%s filters=%v\n"+
					"No data source configured. Connect Aliyun SLS in config (data_sources.aliyun_sls).",
				in.Service, in.TimeRange, in.Filters,
			), nil
		}

		from, to := parseTimeRange(in.TimeRange)
		result, err := src.QueryLogs(ctx, datasource.LogQuery{
			Service: in.Service,
			From:    from,
			To:      to,
			Filters: in.Filters,
			Limit:   in.Limit,
		})
		if err != nil {
			return "", fmt.Errorf("query_logs [%s]: %w", src.Name(), err)
		}

		return result.Summary, nil
	}
}

var QueryMetricsTool = llm.Tool{
	Name:        "query_metrics",
	Description: "Query time-series metrics for a service. Use this to check latency percentiles, error rates, throughput, CPU/memory usage, and other numerical indicators.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"service":     {"type": "string", "description": "The service name"},
			"metric_name": {"type": "string", "description": "Metric to query, e.g. 'p99_latency', 'error_rate', 'throughput', 'cpu_usage'"},
			"time_range":  {"type": "string", "description": "Time range, e.g. '30m', '2h'"},
			"interval":    {"type": "string", "description": "Aggregation interval, e.g. '1m', '5m'"}
		},
		"required": ["service", "metric_name", "time_range"]
	}`),
}

// QueryMetricsInput is the argument schema for the query_metrics tool.
type QueryMetricsInput struct {
	Service    string `json:"service"`
	MetricName string `json:"metric_name"`
	TimeRange  string `json:"time_range"`
	Interval   string `json:"interval"`
}

// QueryMetrics returns a handler backed by the datasource registry.
func QueryMetrics(sources *datasource.Registry) func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in QueryMetricsInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}

		src := sources.Default()
		if src == nil {
			now := time.Now()
			return fmt.Sprintf(
				"[query_metrics] service=%s metric=%s time_range=%s\n"+
					"Sample data points (stub):\n"+
					"  %s: value=120\n"+
					"  %s: value=850 (spike)\n"+
					"  %s: value=920 (elevated)\n"+
					"No data source configured.",
				in.Service, in.MetricName, in.TimeRange,
				now.Add(-30*time.Minute).Format(time.RFC3339),
				now.Add(-15*time.Minute).Format(time.RFC3339),
				now.Add(-5*time.Minute).Format(time.RFC3339),
			), nil
		}

		from, to := parseTimeRange(in.TimeRange)
		interval := parseInterval(in.Interval)
		result, err := src.QueryMetrics(ctx, datasource.MetricQuery{
			Service:    in.Service,
			MetricName: in.MetricName,
			From:       from,
			To:         to,
			Interval:   interval,
		})
		if err != nil {
			return "", fmt.Errorf("query_metrics [%s]: %w", src.Name(), err)
		}

		return result.Summary, nil
	}
}

// parseTimeRange converts a human-readable range like "30m", "2h", "1d"
// into absolute from/to times (to = now).
func parseTimeRange(r string) (from, to time.Time) {
	to = time.Now()
	if r == "" {
		return to.Add(-30 * time.Minute), to
	}

	d, err := time.ParseDuration(r)
	if err != nil {
		// Try mapping shorthand "1d" → 24h
		if len(r) >= 2 && r[len(r)-1] == 'd' {
			var days int
			fmt.Sscanf(r[:len(r)-1], "%d", &days)
			d = time.Duration(days) * 24 * time.Hour
		} else {
			d = 30 * time.Minute
		}
	}

	return to.Add(-d), to
}

// parseInterval converts e.g. "5m" to a duration; defaults to 5 minutes.
func parseInterval(s string) time.Duration {
	if s == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
