package aliyun

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/sentinelai/sentinel/internal/config"
	"github.com/sentinelai/sentinel/internal/datasource"
)

// SLSSource implements datasource.Source backed by Aliyun Log Service.
type SLSSource struct {
	client   sls.ClientInterface
	project  string
	logstore string // default logstore
}

// NewSLS creates an SLS data source from config.
func NewSLS(cfg config.AliyunSLSConfig) (*SLSSource, error) {
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("aliyun SLS: access_key_id and access_key_secret are required")
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("aliyun SLS: project is required")
	}

	client := sls.CreateNormalInterface(
		cfg.Endpoint,
		cfg.AccessKeyID,
		cfg.AccessKeySecret,
		"", // security token — empty for AK/SK auth
	)

	return &SLSSource{
		client:   client,
		project:  cfg.Project,
		logstore: cfg.DefaultLogstore,
	}, nil
}

func (s *SLSSource) Name() string { return "aliyun_sls" }

// QueryLogs executes a log search against Aliyun SLS.
func (s *SLSSource) QueryLogs(ctx context.Context, q datasource.LogQuery) (*datasource.LogResult, error) {
	logstore := q.Logstore
	if logstore == "" {
		logstore = s.logstore
	}
	if logstore == "" {
		return nil, fmt.Errorf("SLS: no logstore specified")
	}
	if q.Limit == 0 {
		q.Limit = 100
	}

	// Build SLS query string
	query := buildSLSQuery(q)

	from := q.From.Unix()
	to := q.To.Unix()
	if from == 0 {
		from = time.Now().Add(-30 * time.Minute).Unix()
	}
	if to == 0 {
		to = time.Now().Unix()
	}

	resp, err := s.client.GetLogs(
		s.project,
		logstore,
		"",    // topic — empty matches all topics
		from,
		to,
		query,
		int64(q.Limit),
		0,     // offset
		false, // reverse (newest first = false)
	)
	if err != nil {
		return nil, fmt.Errorf("SLS GetLogs: %w", err)
	}

	return convertSLSLogs(resp, q.Limit), nil
}

// QueryMetrics uses SLS aggregation queries to derive metric-like data.
// For true time-series metrics, use AliyunCMS instead.
func (s *SLSSource) QueryMetrics(ctx context.Context, q datasource.MetricQuery) (*datasource.MetricResult, error) {
	logstore := s.logstore
	if logstore == "" {
		return nil, fmt.Errorf("SLS: no default logstore for metric queries")
	}

	interval := q.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	// Build a time-series aggregation query
	slsQuery := fmt.Sprintf(
		`* | SELECT __time__ - __time__ %% %d AS t, avg(%s) AS value FROM log GROUP BY t ORDER BY t`,
		int(interval.Seconds()),
		sanitiseMetricName(q.MetricName),
	)

	from := q.From.Unix()
	to := q.To.Unix()

	resp, err := s.client.GetLogs(s.project, logstore, "", from, to, slsQuery, 1000, 0, false)
	if err != nil {
		return nil, fmt.Errorf("SLS metric query: %w", err)
	}

	return convertSLSMetrics(resp), nil
}

// buildSLSQuery constructs an SLS query string from LogQuery fields.
func buildSLSQuery(q datasource.LogQuery) string {
	var parts []string

	if q.Query != "" {
		parts = append(parts, q.Query)
	}
	if q.Service != "" {
		parts = append(parts, fmt.Sprintf(`service: "%s"`, q.Service))
	}
	for k, v := range q.Filters {
		parts = append(parts, fmt.Sprintf(`%s: "%s"`, k, v))
	}

	return strings.Join(parts, " AND ")
}

// convertSLSLogs maps the SLS GetLogsResponse to our internal LogResult.
func convertSLSLogs(resp *sls.GetLogsResponse, limit int) *datasource.LogResult {
	result := &datasource.LogResult{
		TotalCount: resp.Count,
	}

	for _, log := range resp.Logs {
		line := datasource.LogLine{
			Fields: make(map[string]string),
		}

		for k, v := range log {
			switch k {
			case "__time__":
				if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
					line.Timestamp = time.Unix(ts, 0)
				}
			case "level", "__level__":
				line.Level = v
			case "message", "content", "__content__", "msg":
				line.Message = v
			default:
				line.Fields[k] = v
			}
		}

		// Fall back: use any field as message if message is empty
		if line.Message == "" {
			for _, key := range []string{"log", "body", "text"} {
				if v, ok := log[key]; ok {
					line.Message = v
					break
				}
			}
		}

		result.Lines = append(result.Lines, line)
	}

	result.Summary = buildLogSummary(result)
	return result
}

// convertSLSMetrics converts aggregation query results to MetricResult.
func convertSLSMetrics(resp *sls.GetLogsResponse) *datasource.MetricResult {
	result := &datasource.MetricResult{}

	for _, row := range resp.Logs {
		val, err := strconv.ParseFloat(row["value"], 64)
		if err != nil {
			continue
		}

		var ts time.Time
		if t, err := strconv.ParseInt(row["t"], 10, 64); err == nil {
			ts = time.Unix(t, 0)
		}

		result.Points = append(result.Points, datasource.MetricPoint{
			Timestamp: ts,
			Value:     val,
		})

		if val < result.Min || result.Min == 0 {
			result.Min = val
		}
		if val > result.Max {
			result.Max = val
		}
		result.Avg += val
	}

	if len(result.Points) > 0 {
		result.Avg /= float64(len(result.Points))
	}

	result.Summary = buildMetricSummary(result)
	return result
}

// buildLogSummary produces a compact summary for LLM consumption.
func buildLogSummary(r *datasource.LogResult) string {
	if len(r.Lines) == 0 {
		return "No logs found."
	}

	// Count by level
	levels := make(map[string]int)
	for _, l := range r.Lines {
		lvl := strings.ToUpper(l.Level)
		if lvl == "" {
			lvl = "UNKNOWN"
		}
		levels[lvl]++
	}

	var levelParts []string
	for lvl, count := range levels {
		levelParts = append(levelParts, fmt.Sprintf("%s: %d", lvl, count))
	}

	summary := fmt.Sprintf("Total: %d log lines (%s).\n", r.TotalCount, strings.Join(levelParts, ", "))

	// Include first 10 lines as sample
	sample := r.Lines
	if len(sample) > 10 {
		sample = sample[:10]
	}
	summary += "Sample entries:\n"
	for _, l := range sample {
		ts := ""
		if !l.Timestamp.IsZero() {
			ts = l.Timestamp.Format("15:04:05") + " "
		}
		summary += fmt.Sprintf("  [%s%s] %s\n", ts, l.Level, l.Message)
	}

	return summary
}

// buildMetricSummary produces a compact metric summary for the LLM.
func buildMetricSummary(r *datasource.MetricResult) string {
	if len(r.Points) == 0 {
		return "No metric data found."
	}
	return fmt.Sprintf(
		"%d data points. Min: %.2f, Max: %.2f, Avg: %.2f",
		len(r.Points), r.Min, r.Max, r.Avg,
	)
}

// sanitiseMetricName prevents SQL injection in metric field names.
func sanitiseMetricName(name string) string {
	allowed := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	var b strings.Builder
	for _, ch := range name {
		if strings.ContainsRune(allowed, ch) {
			b.WriteRune(ch)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
