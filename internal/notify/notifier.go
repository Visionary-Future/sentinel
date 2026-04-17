package notify

import (
	"context"
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

// MultiChannel fans a notification out to all configured channels.
type MultiChannel struct {
	channels []Channel
}

func NewMultiChannel(channels ...Channel) *MultiChannel {
	return &MultiChannel{channels: channels}
}

func (m *MultiChannel) Send(ctx context.Context, p *Payload) {
	for _, ch := range m.channels {
		if err := ch.Send(ctx, p); err != nil {
			// Notification failures are non-fatal; log at call site.
			_ = err
		}
	}
}
