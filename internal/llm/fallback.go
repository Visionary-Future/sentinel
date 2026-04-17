package llm

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// FallbackProvider tries multiple LLM providers in order, falling back to the
// next if the current one fails. Each provider is retried up to maxRetries
// times with exponential backoff before moving to the next.
type FallbackProvider struct {
	providers  []Provider
	maxRetries int
	baseDelay  time.Duration
	log        *slog.Logger
	// tracks which provider last succeeded
	activeIdx int
}

func NewFallbackProvider(log *slog.Logger, providers ...Provider) *FallbackProvider {
	return &FallbackProvider{
		providers:  providers,
		maxRetries: 2,
		baseDelay:  500 * time.Millisecond,
		log:        log,
	}
}

func (f *FallbackProvider) Name() string {
	if len(f.providers) == 0 {
		return "fallback(empty)"
	}
	return f.providers[f.activeIdx].Name()
}

func (f *FallbackProvider) Model() string {
	if len(f.providers) == 0 {
		return ""
	}
	return f.providers[f.activeIdx].Model()
}

func (f *FallbackProvider) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	var lastErr error

	for i, p := range f.providers {
		for attempt := 0; attempt <= f.maxRetries; attempt++ {
			if attempt > 0 {
				delay := f.baseDelay * time.Duration(math.Pow(2, float64(attempt-1)))
				f.log.Warn("retrying LLM call",
					"provider", p.Name(),
					"attempt", attempt+1,
					"delay", delay,
				)

				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}

			resp, err := p.Chat(ctx, system, messages, tools)
			if err == nil {
				f.activeIdx = i
				return resp, nil
			}

			lastErr = err
			f.log.Warn("LLM call failed",
				"provider", p.Name(),
				"attempt", attempt+1,
				"error", err,
			)
		}

		f.log.Error("provider exhausted retries, falling back",
			"provider", p.Name(),
			"next_provider_available", i+1 < len(f.providers),
		)
	}

	return nil, fmt.Errorf("all LLM providers failed: %w", lastErr)
}
