package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

// InvestigationReport contains the fields an investigation produces that
// notification channels need. Defined here to avoid import cycles with
// the investigation package.
type InvestigationReport struct {
	ID          string
	Status      string
	RootCause   string
	Resolution  string
	Summary     string
	LLMProvider string
	LLMModel    string
	TokenUsage  int
	StepCount   int
	CompletedAt time.Time
}

// Payload bundles everything a notifier needs to compose a message.
type Payload struct {
	Alert         *alert.Event
	Investigation *InvestigationReport
}

// Channel is implemented by every notification backend.
type Channel interface {
	// Name returns a human-readable identifier (e.g. "wecom", "dingtalk").
	Name() string
	// Send delivers an investigation report. Implementations must be safe
	// to call from multiple goroutines.
	Send(ctx context.Context, p *Payload) error
}

const maxNotifyRetries = 2

// MultiChannel fans a notification out to all configured channels.
type MultiChannel struct {
	channels []Channel
	log      *slog.Logger
}

func NewMultiChannel(log *slog.Logger, channels ...Channel) *MultiChannel {
	return &MultiChannel{channels: channels, log: log}
}

func (m *MultiChannel) Send(ctx context.Context, p *Payload) {
	for _, ch := range m.channels {
		m.sendWithRetry(ctx, ch, p)
	}
}

func (m *MultiChannel) sendWithRetry(ctx context.Context, ch Channel, p *Payload) {
	var err error
	for attempt := 0; attempt <= maxNotifyRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				m.log.Error("notification cancelled during retry",
					"channel", ch.Name(), "error", ctx.Err())
				return
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		err = ch.Send(ctx, p)
		if err == nil {
			return
		}

		m.log.Warn("notification send failed",
			"channel", ch.Name(),
			"attempt", attempt+1,
			"error", err,
		)
	}

	m.log.Error("notification send exhausted retries",
		"channel", ch.Name(),
		"error", err,
	)
}
