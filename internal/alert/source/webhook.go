package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/config"
)

// WebhookPayload is the expected JSON body for the generic webhook endpoint.
type WebhookPayload struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    string            `json:"severity"`
	Service     string            `json:"service"`
	Labels      map[string]string `json:"labels"`
}

// WebhookSource provides a channel that the HTTP handler pushes events onto.
// The HTTP handler is registered separately in the API layer.
type WebhookSource struct {
	cfg    config.WebhookSourceConfig
	events chan *alert.Event
	log    *slog.Logger
}

func NewWebhook(cfg config.WebhookSourceConfig, log *slog.Logger) *WebhookSource {
	return &WebhookSource{
		cfg:    cfg,
		events: make(chan *alert.Event, 256),
		log:    log.With("source", "webhook"),
	}
}

func (w *WebhookSource) Name() string { return "webhook" }

// Start forwards queued events from the HTTP handler to onAlert.
func (w *WebhookSource) Start(ctx context.Context, onAlert func(*alert.Event)) error {
	w.log.Info("webhook source started", "path", w.cfg.Path)
	for {
		select {
		case <-ctx.Done():
			return nil
		case evt := <-w.events:
			onAlert(evt)
		}
	}
}

// Enqueue directly adds a pre-parsed alert event to the processing queue.
// Used by format-specific parsers (Alertmanager, Grafana, etc.).
func (w *WebhookSource) Enqueue(evt *alert.Event) {
	w.events <- evt
}

// ParseAndEnqueue parses a raw webhook body and enqueues it for processing.
// Called by the HTTP handler.
func (w *WebhookSource) ParseAndEnqueue(body []byte) error {
	var p WebhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("invalid webhook payload: %w", err)
	}
	if p.Title == "" {
		return fmt.Errorf("webhook payload missing required field: title")
	}

	severity := alert.Severity(p.Severity)
	if severity == "" {
		severity = alert.SeverityWarning
	}

	w.events <- &alert.Event{
		Source:      alert.SourceWebhook,
		Severity:    severity,
		Title:       p.Title,
		Description: p.Description,
		Service:     p.Service,
		Labels:      p.Labels,
		RawPayload:  body,
	}
	return nil
}
