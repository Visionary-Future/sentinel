package source

// prometheus.go parses Prometheus Alertmanager webhook payloads (v4 format).
//
// Alertmanager webhook config example:
//
//	receivers:
//	  - name: sentinel
//	    webhook_configs:
//	      - url: http://sentinel:8080/api/v1/alerts/alertmanager
//	        send_resolved: false

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

// alertmanagerPayload is the v4 Alertmanager webhook body.
type alertmanagerPayload struct {
	Version           string            `json:"version"`
	Status            string            `json:"status"` // "firing" | "resolved"
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []amAlert         `json:"alerts"`
}

type amAlert struct {
	Status      string            `json:"status"` // "firing" | "resolved"
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	GeneratorURL string           `json:"generatorURL"`
}

// ParseAlertmanager converts an Alertmanager webhook body into alert events.
// Only "firing" alerts are returned by default; resolved alerts are skipped.
func ParseAlertmanager(body []byte) ([]*alert.Event, error) {
	var p alertmanagerPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("alertmanager: invalid payload: %w", err)
	}

	var events []*alert.Event
	for _, a := range p.Alerts {
		if a.Status != "firing" {
			continue
		}
		events = append(events, amAlertToEvent(a, p, body))
	}
	return events, nil
}

func amAlertToEvent(a amAlert, p alertmanagerPayload, raw []byte) *alert.Event {
	// Title: prefer annotations.summary, fallback to alertname label.
	title := a.Annotations["summary"]
	if title == "" {
		title = a.Labels["alertname"]
	}
	if title == "" {
		title = "Prometheus Alert"
	}

	// Description: prefer annotations.description, fallback to summary.
	desc := a.Annotations["description"]
	if desc == "" {
		desc = a.Annotations["summary"]
	}

	// Merge labels: per-alert labels take precedence over common labels.
	labels := make(map[string]string, len(p.CommonLabels)+len(a.Labels))
	for k, v := range p.CommonLabels {
		labels[k] = v
	}
	for k, v := range a.Labels {
		labels[k] = v
	}
	if p.ExternalURL != "" {
		labels["alertmanager_url"] = p.ExternalURL
	}
	if a.GeneratorURL != "" {
		labels["generator_url"] = a.GeneratorURL
	}

	return &alert.Event{
		Source:      alert.SourceWebhook,
		Severity:    amSeverity(a.Labels["severity"]),
		Title:       truncate(title, 200),
		Description: desc,
		Service:     firstNonEmpty(a.Labels["service"], a.Labels["job"], a.Labels["namespace"]),
		Labels:      labels,
		RawPayload:  raw,
	}
}

// amSeverity maps Prometheus severity labels to internal severity levels.
func amSeverity(s string) alert.Severity {
	switch strings.ToLower(s) {
	case "critical", "p0":
		return alert.SeverityCritical
	case "warning", "warn", "p1", "p2":
		return alert.SeverityWarning
	case "info", "none":
		return alert.SeverityInfo
	default:
		return alert.SeverityWarning
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
