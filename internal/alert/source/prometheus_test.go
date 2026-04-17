package source

import (
	"encoding/json"
	"testing"

	"github.com/sentinelai/sentinel/internal/alert"
)

func TestParseAlertmanager_FiringAlert(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"commonLabels": {"severity": "critical", "service": "order-service"},
		"commonAnnotations": {},
		"externalURL": "http://alertmanager:9093",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "HighLatency", "severity": "critical", "service": "order-service"},
			"annotations": {"summary": "P99 latency > 2s", "description": "Latency exceeded threshold"},
			"startsAt": "2024-01-01T00:00:00Z",
			"generatorURL": "http://prometheus:9090/graph"
		}]
	}`)

	events, err := ParseAlertmanager(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.Title != "P99 latency > 2s" {
		t.Errorf("expected summary as title, got %q", evt.Title)
	}
	if evt.Severity != alert.SeverityCritical {
		t.Errorf("expected critical severity, got %q", evt.Severity)
	}
	if evt.Service != "order-service" {
		t.Errorf("expected service order-service, got %q", evt.Service)
	}
	if evt.Description != "Latency exceeded threshold" {
		t.Errorf("unexpected description: %q", evt.Description)
	}
}

func TestParseAlertmanager_SkipsResolved(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "resolved",
		"commonLabels": {},
		"commonAnnotations": {},
		"alerts": [{
			"status": "resolved",
			"labels": {"alertname": "HighLatency"},
			"annotations": {"summary": "Resolved"},
			"startsAt": "2024-01-01T00:00:00Z"
		}]
	}`)

	events, err := ParseAlertmanager(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected resolved alerts to be skipped, got %d events", len(events))
	}
}

func TestParseAlertmanager_MultipleAlerts(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"commonLabels": {},
		"commonAnnotations": {},
		"alerts": [
			{"status": "firing", "labels": {"alertname": "A1"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"},
			{"status": "resolved", "labels": {"alertname": "A2"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"},
			{"status": "firing", "labels": {"alertname": "A3"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"}
		]
	}`)

	events, err := ParseAlertmanager(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 firing events, got %d", len(events))
	}
}

func TestParseAlertmanager_FallsBackToAlertname(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"commonLabels": {},
		"commonAnnotations": {},
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "DatabaseDown"},
			"annotations": {},
			"startsAt": "2024-01-01T00:00:00Z"
		}]
	}`)

	events, err := ParseAlertmanager(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Title != "DatabaseDown" {
		t.Errorf("expected alertname as fallback title, got %q", events[0].Title)
	}
}

func TestParseAlertmanager_LabelsMerged(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"commonLabels": {"env": "prod", "region": "cn-hangzhou"},
		"commonAnnotations": {},
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "X", "env": "prod", "instance": "10.0.0.1"},
			"annotations": {"summary": "test"},
			"startsAt": "2024-01-01T00:00:00Z"
		}]
	}`)

	events, err := ParseAlertmanager(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Labels["region"] != "cn-hangzhou" {
		t.Errorf("expected common label region to be merged, got %q", events[0].Labels["region"])
	}
	if events[0].Labels["instance"] != "10.0.0.1" {
		t.Errorf("expected per-alert label instance, got %q", events[0].Labels["instance"])
	}
}

func TestParseAlertmanager_InvalidJSON(t *testing.T) {
	_, err := ParseAlertmanager([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAmSeverity(t *testing.T) {
	cases := []struct {
		input string
		want  alert.Severity
	}{
		{"critical", alert.SeverityCritical},
		{"CRITICAL", alert.SeverityCritical},
		{"p0", alert.SeverityCritical},
		{"warning", alert.SeverityWarning},
		{"warn", alert.SeverityWarning},
		{"p1", alert.SeverityWarning},
		{"info", alert.SeverityInfo},
		{"unknown", alert.SeverityWarning},
		{"", alert.SeverityWarning},
	}
	for _, tc := range cases {
		got := amSeverity(tc.input)
		if got != tc.want {
			t.Errorf("amSeverity(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// Verify the raw payload is preserved.
func TestParseAlertmanager_RawPayloadPreserved(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"commonLabels": {},
		"commonAnnotations": {},
		"alerts": [{"status": "firing", "labels": {"alertname": "X"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"}]
	}`)

	events, _ := ParseAlertmanager(body)
	if len(events) == 0 {
		t.Fatal("expected 1 event")
	}

	var raw map[string]any
	if err := json.Unmarshal(events[0].RawPayload, &raw); err != nil {
		t.Errorf("raw payload not valid JSON: %v", err)
	}
	if raw["version"] != "4" {
		t.Errorf("expected version=4 in raw payload")
	}
}
