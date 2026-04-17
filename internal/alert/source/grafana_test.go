package source

import (
	"testing"

	"github.com/sentinelai/sentinel/internal/alert"
)

func TestParseGrafana_FiringAlert(t *testing.T) {
	body := []byte(`{
		"version": "1",
		"status": "firing",
		"orgId": 1,
		"title": "[FIRING:1] High CPU (prod)",
		"state": "alerting",
		"message": "CPU utilization exceeded 90%",
		"ruleUrl": "http://grafana/alerting/1",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "HighCPU", "severity": "critical", "service": "api-server"},
			"annotations": {"summary": "CPU > 90%", "description": "CPU utilization is at 95%"},
			"startsAt": "2024-01-01T00:00:00Z",
			"endsAt": "0001-01-01T00:00:00Z",
			"dashboardURL": "http://grafana/d/abc/dashboard",
			"panelURL": "http://grafana/d/abc/dashboard?panelId=1"
		}]
	}`)

	events, err := ParseGrafana(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.Title != "CPU > 90%" {
		t.Errorf("expected annotation summary as title, got %q", evt.Title)
	}
	if evt.Severity != alert.SeverityCritical {
		t.Errorf("expected critical, got %q", evt.Severity)
	}
	if evt.Service != "api-server" {
		t.Errorf("expected service api-server, got %q", evt.Service)
	}
	if evt.Labels["dashboard_url"] == "" {
		t.Error("expected dashboard_url in labels")
	}
}

func TestParseGrafana_SkipsResolved(t *testing.T) {
	body := []byte(`{
		"version": "1",
		"status": "resolved",
		"title": "Resolved",
		"alerts": [{
			"status": "resolved",
			"labels": {"alertname": "X"},
			"annotations": {},
			"startsAt": "2024-01-01T00:00:00Z"
		}]
	}`)

	events, err := ParseGrafana(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected resolved to be skipped, got %d events", len(events))
	}
}

func TestParseGrafana_NoAlertsFallback(t *testing.T) {
	// Older Grafana format with no alerts array.
	body := []byte(`{
		"version": "1",
		"status": "firing",
		"state": "alerting",
		"title": "Disk almost full",
		"message": "Disk usage > 90%",
		"commonLabels": {"severity": "warning", "service": "storage"}
	}`)

	events, err := ParseGrafana(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected fallback to produce 1 event, got %d", len(events))
	}
	if events[0].Title != "Disk almost full" {
		t.Errorf("expected top-level title, got %q", events[0].Title)
	}
	if events[0].Severity != alert.SeverityWarning {
		t.Errorf("expected warning severity, got %q", events[0].Severity)
	}
}

func TestParseGrafana_TitleFallback(t *testing.T) {
	// No summary annotation — should fall back to group title, then alertname.
	body := []byte(`{
		"version": "1",
		"status": "firing",
		"title": "[FIRING:1] DB Down",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "DBDown"},
			"annotations": {},
			"startsAt": "2024-01-01T00:00:00Z"
		}]
	}`)

	events, err := ParseGrafana(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falls back to group title from top level.
	if events[0].Title != "[FIRING:1] DB Down" {
		t.Errorf("expected group title, got %q", events[0].Title)
	}
}

func TestParseGrafana_MixedStatuses(t *testing.T) {
	body := []byte(`{
		"version": "1",
		"status": "firing",
		"title": "Multi",
		"alerts": [
			{"status": "firing", "labels": {"alertname": "A"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"},
			{"status": "resolved", "labels": {"alertname": "B"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"},
			{"status": "firing", "labels": {"alertname": "C"}, "annotations": {}, "startsAt": "2024-01-01T00:00:00Z"}
		]
	}`)

	events, err := ParseGrafana(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 firing events, got %d", len(events))
	}
}

func TestParseGrafana_InvalidJSON(t *testing.T) {
	_, err := ParseGrafana([]byte("{bad json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGrafanaSeverity(t *testing.T) {
	cases := []struct {
		labels map[string]string
		want   alert.Severity
	}{
		{map[string]string{"severity": "critical"}, alert.SeverityCritical},
		{map[string]string{"severity": "high"}, alert.SeverityCritical},
		{map[string]string{"priority": "p0"}, alert.SeverityCritical},
		{map[string]string{"severity": "warning"}, alert.SeverityWarning},
		{map[string]string{"level": "medium"}, alert.SeverityWarning},
		{map[string]string{"severity": "info"}, alert.SeverityInfo},
		{map[string]string{"severity": "low"}, alert.SeverityInfo},
		{map[string]string{}, alert.SeverityWarning},
	}
	for _, tc := range cases {
		got := grafanaSeverity(tc.labels)
		if got != tc.want {
			t.Errorf("grafanaSeverity(%v) = %q, want %q", tc.labels, got, tc.want)
		}
	}
}
