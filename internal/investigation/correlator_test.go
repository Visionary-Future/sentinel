package investigation

import (
	"sync"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

// makeEvent is a helper to build a minimal alert.Event for testing.
func makeEvent(id, service string, severity alert.Severity) *alert.Event {
	return &alert.Event{
		ID:       id,
		Service:  service,
		Severity: severity,
	}
}

// ---- severityRank ------------------------------------------------------------

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity alert.Severity
		want     int
	}{
		{alert.SeverityCritical, 3},
		{alert.SeverityWarning, 2},
		{alert.SeverityInfo, 1},
		{alert.Severity("unknown"), 0},
		{alert.Severity(""), 0},
	}
	for _, tc := range tests {
		got := severityRank(tc.severity)
		if got != tc.want {
			t.Errorf("severityRank(%q) = %d, want %d", tc.severity, got, tc.want)
		}
	}
}

// ---- buildAlertGroup ---------------------------------------------------------

func TestBuildAlertGroup_SingleAlert(t *testing.T) {
	evt := makeEvent("1", "svc-a", alert.SeverityInfo)
	pg := &pendingGroup{
		alerts:    []*alert.Event{evt},
		createdAt: time.Now(),
	}
	group := buildAlertGroup(pg, "svc-a")

	if group.PrimaryAlert != evt {
		t.Fatal("expected single alert to be primary")
	}
	if len(group.Related) != 0 {
		t.Fatalf("expected no related alerts, got %d", len(group.Related))
	}
	if group.Service != "svc-a" {
		t.Errorf("expected service svc-a, got %s", group.Service)
	}
}

func TestBuildAlertGroup_HighestSeverityIsPrimary(t *testing.T) {
	tests := []struct {
		name      string
		events    []*alert.Event
		primaryID string
	}{
		{
			name: "critical beats warning and info",
			events: []*alert.Event{
				makeEvent("info-1", "svc", alert.SeverityInfo),
				makeEvent("crit-1", "svc", alert.SeverityCritical),
				makeEvent("warn-1", "svc", alert.SeverityWarning),
			},
			primaryID: "crit-1",
		},
		{
			name: "warning beats info",
			events: []*alert.Event{
				makeEvent("info-1", "svc", alert.SeverityInfo),
				makeEvent("warn-1", "svc", alert.SeverityWarning),
			},
			primaryID: "warn-1",
		},
		{
			name: "all same severity — first alert wins",
			events: []*alert.Event{
				makeEvent("info-1", "svc", alert.SeverityInfo),
				makeEvent("info-2", "svc", alert.SeverityInfo),
			},
			primaryID: "info-1",
		},
		{
			name: "critical first in list still wins",
			events: []*alert.Event{
				makeEvent("crit-1", "svc", alert.SeverityCritical),
				makeEvent("info-1", "svc", alert.SeverityInfo),
			},
			primaryID: "crit-1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pg := &pendingGroup{alerts: tc.events, createdAt: time.Now()}
			group := buildAlertGroup(pg, "svc")

			if group.PrimaryAlert.ID != tc.primaryID {
				t.Errorf("primary ID = %q, want %q", group.PrimaryAlert.ID, tc.primaryID)
			}

			// Primary must not appear in Related
			for _, rel := range group.Related {
				if rel.ID == group.PrimaryAlert.ID {
					t.Errorf("primary alert %q found in Related list", group.PrimaryAlert.ID)
				}
			}

			// All non-primary events must appear in Related
			if len(group.Related) != len(tc.events)-1 {
				t.Errorf("expected %d related alerts, got %d", len(tc.events)-1, len(group.Related))
			}
		})
	}
}

// ---- NewCorrelator / Add / flushExpired / Stop --------------------------------

func TestNewCorrelator_DefaultWindow(t *testing.T) {
	called := make(chan *AlertGroup, 1)
	c := NewCorrelator(0, func(g *AlertGroup) { called <- g })
	defer c.Stop()

	if c.window != defaultCorrelationWindow {
		t.Errorf("expected default window %v, got %v", defaultCorrelationWindow, c.window)
	}
}

func TestCorrelator_Add_GroupsByService(t *testing.T) {
	c := NewCorrelator(time.Hour, nil) // long window so flush doesn't fire automatically
	defer c.Stop()

	c.Add(makeEvent("1", "svc-a", alert.SeverityInfo))
	c.Add(makeEvent("2", "svc-a", alert.SeverityWarning))
	c.Add(makeEvent("3", "svc-b", alert.SeverityCritical))

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.groups["svc-a"].alerts) != 2 {
		t.Errorf("expected 2 alerts for svc-a, got %d", len(c.groups["svc-a"].alerts))
	}
	if len(c.groups["svc-b"].alerts) != 1 {
		t.Errorf("expected 1 alert for svc-b, got %d", len(c.groups["svc-b"].alerts))
	}
}

func TestCorrelator_Add_EmptyServiceUsesUnknownKey(t *testing.T) {
	c := NewCorrelator(time.Hour, nil)
	defer c.Stop()

	c.Add(makeEvent("1", "", alert.SeverityInfo))

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.groups["__unknown__"]; !ok {
		t.Error("expected __unknown__ key for empty service")
	}
}

func TestCorrelator_FlushExpired_CallsOnFlush(t *testing.T) {
	var mu sync.Mutex
	var flushed []*AlertGroup

	window := 10 * time.Millisecond
	c := NewCorrelator(window, func(g *AlertGroup) {
		mu.Lock()
		flushed = append(flushed, g)
		mu.Unlock()
	})
	defer c.Stop()

	c.Add(makeEvent("1", "svc-a", alert.SeverityCritical))
	c.Add(makeEvent("2", "svc-a", alert.SeverityInfo))
	c.Add(makeEvent("3", "svc-b", alert.SeverityWarning))

	// Wait for window to expire, then trigger flush manually
	time.Sleep(window + 5*time.Millisecond)
	c.flushExpired()

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 2 {
		t.Fatalf("expected 2 flushed groups, got %d", len(flushed))
	}

	// Groups are removed from the map after flush
	c.mu.Lock()
	remaining := len(c.groups)
	c.mu.Unlock()

	if remaining != 0 {
		t.Errorf("expected groups map to be empty after flush, got %d entries", remaining)
	}
}

func TestCorrelator_FlushExpired_DoesNotFlushBeforeWindow(t *testing.T) {
	var mu sync.Mutex
	var flushCount int

	c := NewCorrelator(time.Hour, func(g *AlertGroup) {
		mu.Lock()
		flushCount++
		mu.Unlock()
	})
	defer c.Stop()

	c.Add(makeEvent("1", "svc-a", alert.SeverityInfo))
	c.flushExpired()

	mu.Lock()
	count := flushCount
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected no flush before window expires, got %d flushes", count)
	}
}

func TestCorrelator_Stop_TriggersFlush(t *testing.T) {
	flushed := make(chan *AlertGroup, 10)

	window := time.Hour // won't expire naturally
	c := NewCorrelator(window, func(g *AlertGroup) {
		flushed <- g
	})

	c.Add(makeEvent("1", "svc-a", alert.SeverityCritical))

	// Manually expire the group
	c.mu.Lock()
	c.groups["svc-a"].createdAt = time.Now().Add(-window - time.Second)
	c.mu.Unlock()

	c.Stop()

	// flushLoop does a final flushExpired on stop
	select {
	case g := <-flushed:
		if g.Service != "svc-a" {
			t.Errorf("expected flushed group for svc-a, got %s", g.Service)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for flush on Stop")
	}
}

func TestCorrelator_FlushExpired_CorrectPrimaryChosen(t *testing.T) {
	var flushed []*AlertGroup
	var mu sync.Mutex
	window := 5 * time.Millisecond

	c := NewCorrelator(window, func(g *AlertGroup) {
		mu.Lock()
		flushed = append(flushed, g)
		mu.Unlock()
	})
	defer c.Stop()

	c.Add(makeEvent("info-evt", "svc", alert.SeverityInfo))
	c.Add(makeEvent("crit-evt", "svc", alert.SeverityCritical))
	c.Add(makeEvent("warn-evt", "svc", alert.SeverityWarning))

	time.Sleep(window + 5*time.Millisecond)
	c.flushExpired()

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 1 {
		t.Fatalf("expected 1 flushed group, got %d", len(flushed))
	}
	g := flushed[0]
	if g.PrimaryAlert.ID != "crit-evt" {
		t.Errorf("expected crit-evt as primary, got %s", g.PrimaryAlert.ID)
	}
	if len(g.Related) != 2 {
		t.Errorf("expected 2 related alerts, got %d", len(g.Related))
	}
}
