package source

// grafana.go parses Grafana Alerting webhook payloads (Grafana 9+ unified alerting).
//
// Grafana contact point configuration:
//   Type: Webhook
//   URL:  http://sentinel:8080/api/v1/alerts/grafana
//   HTTP Method: POST

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

// grafanaPayload is the Grafana unified alerting webhook body.
type grafanaPayload struct {
	Version         string            `json:"version"`
	GroupKey        string            `json:"groupKey"`
	TruncatedAlerts int               `json:"truncatedAlerts"`
	Status          string            `json:"status"` // "firing" | "resolved"
	OrgID           int64             `json:"orgId"`
	Title           string            `json:"title"`
	State           string            `json:"state"`
	Message         string            `json:"message"`
	RuleURL         string            `json:"ruleUrl"`
	Alerts          []grafanaAlert    `json:"alerts"`
	GroupLabels     map[string]string `json:"groupLabels"`
	CommonLabels    map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
}

type grafanaAlert struct {
	Status       string            `json:"status"` // "firing" | "resolved"
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	Values       map[string]any    `json:"values"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	SilenceURL   string            `json:"silenceURL"`
	DashboardURL string            `json:"dashboardURL"`
	PanelURL     string            `json:"panelURL"`
	ImageURL     string            `json:"imageURL"`
}

// ParseGrafana converts a Grafana unified alerting webhook body into alert events.
// Only "firing" alerts are returned; resolved alerts are skipped.
func ParseGrafana(body []byte) ([]*alert.Event, error) {
	var p grafanaPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("grafana: invalid payload: %w", err)
	}

	// Grafana may send payloads without an alerts array (older format).
	// Fall back to a single event from top-level fields.
	if len(p.Alerts) == 0 {
		if p.Status != "firing" && p.State != "alerting" {
			return nil, nil
		}
		evt := grafanaTopLevelToEvent(p, body)
		return []*alert.Event{evt}, nil
	}

	var events []*alert.Event
	for _, a := range p.Alerts {
		if a.Status != "firing" {
			continue
		}
		events = append(events, grafanaAlertToEvent(a, p, body))
	}
	return events, nil
}

func grafanaAlertToEvent(a grafanaAlert, p grafanaPayload, raw []byte) *alert.Event {
	// Title: per-alert annotations.summary → group title → alertname label.
	title := a.Annotations["summary"]
	if title == "" {
		title = p.Title
	}
	if title == "" {
		title = a.Labels["alertname"]
	}
	if title == "" {
		title = "Grafana Alert"
	}

	desc := a.Annotations["description"]
	if desc == "" {
		desc = p.Message
	}

	labels := make(map[string]string, len(p.CommonLabels)+len(a.Labels))
	for k, v := range p.CommonLabels {
		labels[k] = v
	}
	for k, v := range a.Labels {
		labels[k] = v
	}
	if a.DashboardURL != "" {
		labels["dashboard_url"] = a.DashboardURL
	}
	if a.PanelURL != "" {
		labels["panel_url"] = a.PanelURL
	}
	if p.RuleURL != "" {
		labels["rule_url"] = p.RuleURL
	}

	return &alert.Event{
		Source:      alert.SourceWebhook,
		Severity:    grafanaSeverity(a.Labels),
		Title:       truncate(title, 200),
		Description: desc,
		Service:     firstNonEmpty(a.Labels["service"], a.Labels["job"], a.Labels["namespace"]),
		Labels:      labels,
		RawPayload:  raw,
	}
}

func grafanaTopLevelToEvent(p grafanaPayload, raw []byte) *alert.Event {
	title := p.Title
	if title == "" {
		title = "Grafana Alert"
	}
	return &alert.Event{
		Source:      alert.SourceWebhook,
		Severity:    grafanaSeverity(p.CommonLabels),
		Title:       truncate(title, 200),
		Description: p.Message,
		Service:     firstNonEmpty(p.CommonLabels["service"], p.CommonLabels["job"]),
		Labels:      p.CommonLabels,
		RawPayload:  raw,
	}
}

// grafanaSeverity derives severity from labels (severity or priority label).
func grafanaSeverity(labels map[string]string) alert.Severity {
	for _, key := range []string{"severity", "priority", "level"} {
		if v, ok := labels[key]; ok {
			switch strings.ToLower(v) {
			case "critical", "p0", "high":
				return alert.SeverityCritical
			case "warning", "warn", "p1", "p2", "medium":
				return alert.SeverityWarning
			case "info", "low", "none":
				return alert.SeverityInfo
			}
		}
	}
	return alert.SeverityWarning
}
