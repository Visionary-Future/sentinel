package llm

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"
)

// mockProvider is a test double for the Provider interface.
type mockProvider struct {
	name      string
	model     string
	responses []*Response
	errors    []error
	callCount int
}

func newMockProvider(name, model string) *mockProvider {
	return &mockProvider{name: name, model: model}
}

func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) Model() string { return m.model }

func (m *mockProvider) Chat(_ context.Context, _ string, _ []Message, _ []Tool) (*Response, error) {
	m.callCount++

	// Consume errors queue first; once exhausted, fall through to responses.
	if len(m.errors) > 0 {
		err := m.errors[0]
		m.errors = m.errors[1:]
		if err != nil {
			return nil, err
		}
	}

	if len(m.responses) > 0 {
		resp := m.responses[0]
		m.responses = m.responses[1:]
		return resp, nil
	}

	return &Response{Content: "ok", StopReason: StopReasonEndTurn}, nil
}

func (m *mockProvider) withError(err error) *mockProvider {
	m.errors = append(m.errors, err)
	return m
}

func (m *mockProvider) withErrors(errs ...error) *mockProvider {
	m.errors = append(m.errors, errs...)
	return m
}

func (m *mockProvider) withResponse(r *Response) *mockProvider {
	m.responses = append(m.responses, r)
	return m
}

// silentLogger returns a logger that discards all output, keeping test output clean.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestFallbackProvider_FirstProviderSucceeds(t *testing.T) {
	want := &Response{Content: "hello", StopReason: StopReasonEndTurn, TokensUsed: 42}

	primary := newMockProvider("primary", "model-a")
	primary.withResponse(want)

	secondary := newMockProvider("secondary", "model-b")

	fp := NewFallbackProvider(silentLogger(), primary, secondary)

	got, err := fp.Chat(context.Background(), "system", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if secondary.callCount != 0 {
		t.Errorf("secondary provider should not have been called, got %d calls", secondary.callCount)
	}
	if fp.Name() != "primary" {
		t.Errorf("Name() = %q, want %q", fp.Name(), "primary")
	}
	if fp.Model() != "model-a" {
		t.Errorf("Model() = %q, want %q", fp.Model(), "model-a")
	}
}

func TestFallbackProvider_FirstFailsThenSecondSucceeds(t *testing.T) {
	callErr := errors.New("primary unavailable")

	// Primary fails on every attempt (maxRetries=2 means 3 attempts total).
	primary := newMockProvider("primary", "model-a")
	primary.withErrors(callErr, callErr, callErr)

	want := &Response{Content: "fallback response", StopReason: StopReasonEndTurn}
	secondary := newMockProvider("secondary", "model-b")
	secondary.withResponse(want)

	// Use zero base delay so the test doesn't actually wait.
	fp := &FallbackProvider{
		providers:  []Provider{primary, secondary},
		maxRetries: 2,
		baseDelay:  0,
		log:        silentLogger(),
	}

	got, err := fp.Chat(context.Background(), "system", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// primary should have been tried maxRetries+1 = 3 times.
	if primary.callCount != 3 {
		t.Errorf("primary callCount = %d, want 3", primary.callCount)
	}
	// secondary succeeds on first attempt.
	if secondary.callCount != 1 {
		t.Errorf("secondary callCount = %d, want 1", secondary.callCount)
	}
	// activeIdx should point to secondary (index 1).
	if fp.Name() != "secondary" {
		t.Errorf("Name() after fallback = %q, want %q", fp.Name(), "secondary")
	}
}

func TestFallbackProvider_AllProvidersFail(t *testing.T) {
	callErr := errors.New("service down")

	p1 := newMockProvider("p1", "m1")
	p1.withErrors(callErr, callErr, callErr)

	p2 := newMockProvider("p2", "m2")
	p2.withErrors(callErr, callErr, callErr)

	fp := &FallbackProvider{
		providers:  []Provider{p1, p2},
		maxRetries: 2,
		baseDelay:  0,
		log:        silentLogger(),
	}

	_, err := fp.Chat(context.Background(), "system", nil, nil)
	if err == nil {
		t.Fatal("expected error when all providers fail, got nil")
	}
	if !errors.Is(err, callErr) {
		t.Errorf("error chain should wrap original error; got %v", err)
	}
}

func TestFallbackProvider_RetryWithBackoff(t *testing.T) {
	callErr := errors.New("transient error")

	// Fail twice, succeed on third attempt (attempt index 2).
	primary := newMockProvider("primary", "model-a")
	primary.withErrors(callErr, callErr)
	primary.withResponse(&Response{Content: "recovered", StopReason: StopReasonEndTurn})

	baseDelay := 1 * time.Millisecond
	fp := &FallbackProvider{
		providers:  []Provider{primary},
		maxRetries: 2,
		baseDelay:  baseDelay,
		log:        silentLogger(),
	}

	start := time.Now()
	got, err := fp.Chat(context.Background(), "system", nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "recovered" {
		t.Errorf("content = %q, want %q", got.Content, "recovered")
	}
	// Two retries: delay 1ms (2^0) + 2ms (2^1) = at least 3ms total.
	minExpected := baseDelay + 2*baseDelay
	if elapsed < minExpected {
		t.Errorf("elapsed %v is less than expected minimum backoff %v", elapsed, minExpected)
	}
	if primary.callCount != 3 {
		t.Errorf("primary callCount = %d, want 3", primary.callCount)
	}
}

func TestFallbackProvider_ContextCancelledDuringBackoff(t *testing.T) {
	callErr := errors.New("error")

	primary := newMockProvider("primary", "model-a")
	// Fail once so a retry is attempted with a delay.
	primary.withErrors(callErr)

	ctx, cancel := context.WithCancel(context.Background())

	fp := &FallbackProvider{
		providers:  []Provider{primary},
		maxRetries: 2,
		baseDelay:  100 * time.Millisecond, // long delay so cancel wins
		log:        silentLogger(),
	}

	// Cancel immediately after starting.
	cancel()

	_, err := fp.Chat(ctx, "system", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestFallbackProvider_EmptyProviders(t *testing.T) {
	fp := NewFallbackProvider(silentLogger())

	if fp.Name() != "fallback(empty)" {
		t.Errorf("Name() = %q, want %q", fp.Name(), "fallback(empty)")
	}
	if fp.Model() != "" {
		t.Errorf("Model() = %q, want %q", fp.Model(), "")
	}

	_, err := fp.Chat(context.Background(), "system", nil, nil)
	if err == nil {
		t.Fatal("expected error with empty providers, got nil")
	}
}
