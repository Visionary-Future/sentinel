package investigation

import (
	"sync"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

const (
	defaultCorrelationWindow = 2 * time.Minute
	defaultFlushInterval     = 30 * time.Second
)

// AlertGroup represents a set of correlated alerts that should be
// investigated together.
type AlertGroup struct {
	PrimaryAlert *alert.Event   // highest severity alert in the group
	Related      []*alert.Event // other correlated alerts
	Service      string
	CreatedAt    time.Time
}

// Correlator buffers incoming alerts and groups them by service within
// a time window. When the window expires, it flushes the group as a
// single investigation.
type Correlator struct {
	window   time.Duration
	mu       sync.Mutex
	groups   map[string]*pendingGroup // key: service name
	onFlush  func(*AlertGroup)
	stopCh   chan struct{}
}

type pendingGroup struct {
	alerts    []*alert.Event
	createdAt time.Time
}

func NewCorrelator(window time.Duration, onFlush func(*AlertGroup)) *Correlator {
	if window == 0 {
		window = defaultCorrelationWindow
	}
	c := &Correlator{
		window:  window,
		groups:  make(map[string]*pendingGroup),
		onFlush: onFlush,
		stopCh:  make(chan struct{}),
	}
	go c.flushLoop()
	return c
}

// Add buffers an alert for correlation. If the alert's service already
// has a pending group, the alert joins it. Otherwise a new group starts.
func (c *Correlator) Add(evt *alert.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := evt.Service
	if key == "" {
		key = "__unknown__"
	}

	pg, exists := c.groups[key]
	if !exists {
		c.groups[key] = &pendingGroup{
			alerts:    []*alert.Event{evt},
			createdAt: time.Now(),
		}
		return
	}

	pg.alerts = append(pg.alerts, evt)
}

// flushLoop periodically checks for groups that have exceeded the
// correlation window and flushes them.
func (c *Correlator) flushLoop() {
	ticker := time.NewTicker(defaultFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.flushExpired()
		case <-c.stopCh:
			// Final flush on shutdown
			c.flushExpired()
			return
		}
	}
}

func (c *Correlator) flushExpired() {
	c.mu.Lock()
	now := time.Now()
	var toFlush []string
	for key, pg := range c.groups {
		if now.Sub(pg.createdAt) >= c.window {
			toFlush = append(toFlush, key)
		}
	}

	groups := make([]*pendingGroup, 0, len(toFlush))
	keys := make([]string, 0, len(toFlush))
	for _, key := range toFlush {
		groups = append(groups, c.groups[key])
		keys = append(keys, key)
		delete(c.groups, key)
	}
	c.mu.Unlock()

	for i, pg := range groups {
		group := buildAlertGroup(pg, keys[i])
		if c.onFlush != nil {
			c.onFlush(group)
		}
	}
}

// Stop shuts down the correlator's flush loop.
func (c *Correlator) Stop() {
	close(c.stopCh)
}

// buildAlertGroup picks the highest-severity alert as primary.
func buildAlertGroup(pg *pendingGroup, service string) *AlertGroup {
	primary := pg.alerts[0]
	for _, evt := range pg.alerts[1:] {
		if severityRank(evt.Severity) > severityRank(primary.Severity) {
			primary = evt
		}
	}

	var related []*alert.Event
	for _, evt := range pg.alerts {
		if evt.ID != primary.ID {
			related = append(related, evt)
		}
	}

	return &AlertGroup{
		PrimaryAlert: primary,
		Related:      related,
		Service:      service,
		CreatedAt:    pg.createdAt,
	}
}

func severityRank(s alert.Severity) int {
	switch s {
	case alert.SeverityCritical:
		return 3
	case alert.SeverityWarning:
		return 2
	case alert.SeverityInfo:
		return 1
	default:
		return 0
	}
}
