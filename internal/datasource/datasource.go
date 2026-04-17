package datasource

import (
	"context"
	"time"
)

// LogQuery describes a log search request.
type LogQuery struct {
	Service   string
	Logstore  string            // optional override; uses default if empty
	From      time.Time
	To        time.Time
	Query     string            // SLS SQL / ES query string
	Filters   map[string]string // key=value filters (ANDed)
	Limit     int               // max lines returned (default 100)
}

// LogLine is a single parsed log entry.
type LogLine struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]string
}

// LogResult holds query output.
type LogResult struct {
	TotalCount int64
	Lines      []LogLine
	// Summary is a short textual digest suitable for injecting into the LLM.
	Summary string
}

// MetricQuery describes a time-series metric request.
type MetricQuery struct {
	Service    string
	MetricName string
	From       time.Time
	To         time.Time
	Interval   time.Duration // aggregation window (e.g. 1m, 5m)
}

// MetricPoint is one data point in a time-series.
type MetricPoint struct {
	Timestamp time.Time
	Value     float64
}

// MetricResult holds query output.
type MetricResult struct {
	Points  []MetricPoint
	Min     float64
	Max     float64
	Avg     float64
	// Summary is a short textual digest suitable for injecting into the LLM.
	Summary string
}

// Source is implemented by every observability backend.
type Source interface {
	// Name returns a human-readable identifier (e.g. "aliyun_sls").
	Name() string
	// QueryLogs executes a log search.
	QueryLogs(ctx context.Context, q LogQuery) (*LogResult, error)
	// QueryMetrics fetches time-series metric data.
	QueryMetrics(ctx context.Context, q MetricQuery) (*MetricResult, error)
}

// Registry holds all configured data sources.
type Registry struct {
	sources []Source
	byName  map[string]Source
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Source)}
}

// Register adds a data source to the registry.
func (r *Registry) Register(s Source) {
	r.sources = append(r.sources, s)
	r.byName[s.Name()] = s
}

// Get returns a source by name, or nil if not found.
func (r *Registry) Get(name string) Source {
	return r.byName[name]
}

// Default returns the first registered source, or nil.
func (r *Registry) Default() Source {
	if len(r.sources) == 0 {
		return nil
	}
	return r.sources[0]
}

// Len returns the number of registered sources.
func (r *Registry) Len() int { return len(r.sources) }
