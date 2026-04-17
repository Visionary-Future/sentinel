package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// EventType identifies the kind of investigation lifecycle event.
type EventType string

const (
	EventInvestigationStarted   EventType = "investigation.started"
	EventInvestigationCompleted EventType = "investigation.completed"
	EventInvestigationFailed    EventType = "investigation.failed"
)

// WebhookEvent is the JSON payload delivered to the configured webhook URL.
type WebhookEvent struct {
	Type             EventType `json:"type"`
	InvestigationID  string    `json:"investigation_id"`
	AlertID          string    `json:"alert_id"`
	Status           string    `json:"status"`
	RootCause        string    `json:"root_cause,omitempty"`
	Resolution       string    `json:"resolution,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	Confidence       int       `json:"confidence,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}

const (
	webhookTimeout     = 10 * time.Second
	webhookMaxAttempts = 3
)

// retryDelays holds the backoff wait between successive attempts.
var retryDelays = [webhookMaxAttempts - 1]time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
}

// WebhookSender delivers investigation lifecycle events to an external HTTP endpoint.
type WebhookSender struct {
	url    string
	client *http.Client
	log    *slog.Logger
}

// NewWebhookSender returns a configured WebhookSender.
// When url is empty the sender is effectively disabled: Send always returns nil.
func NewWebhookSender(url string, log *slog.Logger) *WebhookSender {
	return &WebhookSender{
		url: url,
		client: &http.Client{
			Timeout: webhookTimeout,
		},
		log: log,
	}
}

// Send marshals evt as JSON and POSTs it to the configured URL.
// It retries up to 3 times with exponential backoff (500 ms, 1 s, 2 s).
// Each attempt uses a 10-second per-attempt timeout derived from ctx.
// Returns nil immediately when the URL is empty (disabled).
func (s *WebhookSender) Send(ctx context.Context, evt WebhookEvent) error {
	if s.url == "" {
		return nil
	}

	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("webhook: marshal event: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < webhookMaxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelays[attempt-1]
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook: context cancelled before attempt %d: %w", attempt+1, ctx.Err())
			case <-time.After(delay):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, webhookTimeout)
		lastErr = s.doPost(attemptCtx, body)
		cancel()

		if lastErr == nil {
			return nil
		}

		s.log.Warn("webhook delivery failed",
			"attempt", attempt+1,
			"url", s.url,
			"error", lastErr,
		)
	}

	return fmt.Errorf("webhook: all %d attempts failed: %w", webhookMaxAttempts, lastErr)
}

// doPost performs a single HTTP POST with the encoded body.
func (s *WebhookSender) doPost(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// BuildEvent constructs a WebhookEvent from an Investigation and the given EventType.
func BuildEvent(inv *Investigation, eventType EventType) WebhookEvent {
	return WebhookEvent{
		Type:            eventType,
		InvestigationID: inv.ID,
		AlertID:         inv.AlertID,
		Status:          string(inv.Status),
		RootCause:       inv.RootCause,
		Resolution:      inv.Resolution,
		Summary:         inv.Summary,
		Confidence:      inv.Confidence,
		Timestamp:       time.Now().UTC(),
	}
}
