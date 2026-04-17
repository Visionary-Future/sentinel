package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	cms "github.com/aliyun/alibaba-cloud-sdk-go/services/cms"
	"github.com/sentinelai/sentinel/internal/config"
	"github.com/sentinelai/sentinel/internal/datasource"
)

// CMSSource implements datasource.Source backed by Aliyun CloudMonitor Service.
// It supports metric queries via DescribeMetricList.
// Log queries are not supported (use SLSSource for logs).
type CMSSource struct {
	client *cms.Client
	region string
}

// NewCMS creates a CMS data source from config.
func NewCMS(cfg config.AliyunCMSConfig) (*CMSSource, error) {
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("aliyun CMS: access_key_id and access_key_secret are required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("aliyun CMS: region is required")
	}

	client, err := cms.NewClientWithAccessKey(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("aliyun CMS: create client: %w", err)
	}

	return &CMSSource{client: client, region: cfg.Region}, nil
}

func (c *CMSSource) Name() string { return "aliyun_cms" }

// QueryLogs is not supported by CMS.
func (c *CMSSource) QueryLogs(_ context.Context, _ datasource.LogQuery) (*datasource.LogResult, error) {
	return &datasource.LogResult{
		Summary: "aliyun_cms does not support log queries. Use aliyun_sls for logs.",
	}, nil
}

// QueryMetrics fetches time-series data from Aliyun CloudMonitor DescribeMetricList.
//
// MetricQuery.MetricName format: "Namespace/MetricName"
// e.g. "acs_ecs_dashboard/CPUUtilization"
// If no slash, defaults to namespace "acs_ecs_dashboard".
func (c *CMSSource) QueryMetrics(ctx context.Context, q datasource.MetricQuery) (*datasource.MetricResult, error) {
	namespace, metricName := splitNamespaceMetric(q.MetricName)

	period := int(q.Interval.Seconds())
	if period == 0 {
		period = 60
	}

	from := q.From
	to := q.To
	if from.IsZero() {
		from = time.Now().Add(-30 * time.Minute)
	}
	if to.IsZero() {
		to = time.Now()
	}

	req := cms.CreateDescribeMetricListRequest()
	req.Namespace = namespace
	req.MetricName = metricName
	req.Period = strconv.Itoa(period)
	req.StartTime = strconv.FormatInt(from.UnixMilli(), 10)
	req.EndTime = strconv.FormatInt(to.UnixMilli(), 10)

	resp, err := c.client.DescribeMetricList(req)
	if err != nil {
		return nil, fmt.Errorf("CMS DescribeMetricList: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("CMS error: %s (code: %s)", resp.Message, resp.Code)
	}

	return convertCMSDatapoints(resp.Datapoints, namespace+"/"+metricName), nil
}

// cmsDatapoint mirrors one entry from the Datapoints JSON array CMS returns.
type cmsDatapoint struct {
	Timestamp   int64   `json:"timestamp"`
	Average     float64 `json:"Average"`
	Maximum     float64 `json:"Maximum"`
	Minimum     float64 `json:"Minimum"`
	Sum         float64 `json:"Sum"`
	Value       float64 `json:"Value"`
	SampleCount float64 `json:"SampleCount"`
}

func (d cmsDatapoint) value() float64 {
	if d.Average != 0 {
		return d.Average
	}
	if d.Value != 0 {
		return d.Value
	}
	return d.Sum
}

// convertCMSDatapoints parses the CMS Datapoints JSON string into MetricResult.
func convertCMSDatapoints(datapoints, metricName string) *datasource.MetricResult {
	result := &datasource.MetricResult{}

	if datapoints == "" || datapoints == "[]" {
		result.Summary = fmt.Sprintf("No metric data for %s.", metricName)
		return result
	}

	var points []cmsDatapoint
	if err := json.Unmarshal([]byte(datapoints), &points); err != nil {
		result.Summary = fmt.Sprintf("Could not parse CMS datapoints for %s: %v", metricName, err)
		return result
	}

	for _, p := range points {
		val := p.value()
		ts := time.UnixMilli(p.Timestamp)

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

	if n := len(result.Points); n > 0 {
		result.Avg /= float64(n)
	}
	result.Summary = buildMetricSummary(result)
	return result
}

// splitNamespaceMetric splits "acs_ecs_dashboard/CPUUtilization" → namespace, metric.
func splitNamespaceMetric(name string) (namespace, metric string) {
	for i, ch := range name {
		if ch == '/' {
			return name[:i], name[i+1:]
		}
	}
	return "acs_ecs_dashboard", name
}
