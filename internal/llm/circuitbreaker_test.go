package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// cbErrBackend is a sentinel error returned by the inner provider in CB tests.
var cbErrBackend = errors.New("backend error")

// newCBProvider returns a mockProvider pre-loaded with n consecutive errors
// followed by unlimited successes.
func newCBProvider(name string, failFirst int) *mockProvider {
	p := newMockProvider(name, "m1")
	for i := 0; i < failFirst; i++ {
		p.withError(cbErrBackend)
	}
	return p
}

// newCB is a shorthand constructor for tests.
func newCB(inner Provider, maxFailures int, cooldown time.Duration) *CircuitBreaker {
	return NewCircuitBreaker(inner, maxFailures, cooldown)
}

func chatCB(cb *CircuitBreaker) error {
	_, err := cb.Chat(context.Background(), "", nil, nil)
	return err
}

// Tests

func TestCircuitBreaker_ClosedPassesThrough(t *testing.T) {
	p := newMockProvider("test", "m1")
	cb := newCB(p, 3, time.Minute)

	if got := cb.State(); got != "closed" {
		t.Fatalf("expected initial state closed, got %s", got)
	}

	if err := chatCB(cb); err != nil {
		t.Fatalf("expected no error in closed state, got %v", err)
	}
	if p.callCount != 1 {
		t.Fatalf("expected 1 call to inner provider, got %d", p.callCount)
	}
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	p := newCBProvider("test", 10) // plenty of errors
	cb := newCB(p, 3, time.Minute)

	for i := 0; i < 2; i++ {
		err := chatCB(cb)
		if !errors.Is(err, cbErrBackend) {
			t.Fatalf("call %d: expected backend error, got %v", i+1, err)
		}
		if cb.State() != "closed" {
			t.Fatalf("call %d: expected still closed, got %s", i+1, cb.State())
		}
	}

	// Third failure should open the circuit.
	err := chatCB(cb)
	if !errors.Is(err, cbErrBackend) {
		t.Fatalf("expected backend error on triggering failure, got %v", err)
	}
	if cb.State() != "open" {
		t.Fatalf("expected open state after max failures, got %s", cb.State())
	}
}

func TestCircuitBreaker_OpenRejectsWithErrCircuitOpen(t *testing.T) {
	p := newCBProvider("test", 10)
	cb := newCB(p, 2, time.Hour) // very long cooldown so it stays open

	// Trip the breaker.
	chatCB(cb) //nolint:errcheck
	chatCB(cb) //nolint:errcheck

	if cb.State() != "open" {
		t.Fatalf("expected open, got %s", cb.State())
	}

	callsBefore := p.callCount
	err := chatCB(cb)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	// Inner provider must NOT have been called.
	if p.callCount != callsBefore {
		t.Fatalf("inner provider was called while circuit is open (before=%d, after=%d)",
			callsBefore, p.callCount)
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterCooldown(t *testing.T) {
	cooldown := 50 * time.Millisecond
	p := newCBProvider("test", 10) // always fails
	cb := newCB(p, 2, cooldown)

	// Trip the breaker.
	chatCB(cb) //nolint:errcheck
	chatCB(cb) //nolint:errcheck

	if cb.State() != "open" {
		t.Fatalf("expected open, got %s", cb.State())
	}

	// Wait for cooldown to expire.
	time.Sleep(cooldown + 10*time.Millisecond)

	callsBefore := p.callCount
	err := chatCB(cb)
	// The probe call should reach the inner provider (not be rejected with ErrCircuitOpen).
	if errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected probe call to reach inner provider, got ErrCircuitOpen")
	}
	if p.callCount != callsBefore+1 {
		t.Fatalf("expected inner provider called once during probe (before=%d after=%d)",
			callsBefore, p.callCount)
	}
}

func TestCircuitBreaker_HalfOpenSuccessResetsToClose(t *testing.T) {
	cooldown := 50 * time.Millisecond
	// Fail twice to open the circuit, then succeed on the probe.
	p := newMockProvider("test", "m1")
	p.withErrors(cbErrBackend, cbErrBackend)
	p.withResponse(&Response{Content: "ok", StopReason: StopReasonEndTurn})

	cb := newCB(p, 2, cooldown)

	// Trip the breaker.
	chatCB(cb) //nolint:errcheck
	chatCB(cb) //nolint:errcheck

	if cb.State() != "open" {
		t.Fatalf("expected open, got %s", cb.State())
	}

	time.Sleep(cooldown + 10*time.Millisecond)

	// Probe call succeeds — circuit should reset to closed.
	if err := chatCB(cb); err != nil {
		t.Fatalf("expected probe call to succeed, got %v", err)
	}
	if cb.State() != "closed" {
		t.Fatalf("expected closed after successful probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReturnsToOpen(t *testing.T) {
	cooldown := 50 * time.Millisecond
	p := newCBProvider("test", 10) // always fails
	cb := newCB(p, 2, cooldown)

	// Trip the breaker.
	chatCB(cb) //nolint:errcheck
	chatCB(cb) //nolint:errcheck

	time.Sleep(cooldown + 10*time.Millisecond)

	// Probe call fails — circuit must return to open.
	err := chatCB(cb)
	if errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("probe call should have reached inner provider, got ErrCircuitOpen")
	}
	if !errors.Is(err, cbErrBackend) {
		t.Fatalf("expected backend error from failed probe, got %v", err)
	}
	if cb.State() != "open" {
		t.Fatalf("expected open after failed probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailureCounter(t *testing.T) {
	// Fail twice, succeed once (resets counter), then fail twice more — should NOT open (max=3).
	// The existing mockProvider treats a nil entry in the errors queue as "no error, fall through
	// to responses". We use that to interleave a success without touching the responses queue.
	p := newMockProvider("test", "m1")
	p.withErrors(cbErrBackend, cbErrBackend) // failures 1-2
	p.withError(nil)                          // success (nil error → falls through, responses empty → default ok)
	p.withErrors(cbErrBackend, cbErrBackend) // failures 1-2 again after reset

	cb := newCB(p, 3, time.Minute)

	chatCB(cb) //nolint:errcheck // failure 1
	chatCB(cb) //nolint:errcheck // failure 2
	if cb.State() != "closed" {
		t.Fatalf("expected closed before success, got %s", cb.State())
	}

	chatCB(cb) //nolint:errcheck // success — resets counter
	if cb.State() != "closed" {
		t.Fatalf("expected closed after success, got %s", cb.State())
	}

	chatCB(cb) //nolint:errcheck // failure 1 again (counter was reset)
	chatCB(cb) //nolint:errcheck // failure 2 again — still below max=3
	if cb.State() != "closed" {
		t.Fatalf("expected still closed (counter was reset by success), got %s", cb.State())
	}
}

func TestCircuitBreaker_NameAndModelDelegateToInner(t *testing.T) {
	p := newMockProvider("myprovider", "mymodel")
	cb := newCB(p, 3, time.Minute)

	if cb.Name() != "myprovider" {
		t.Errorf("Name() = %q, want %q", cb.Name(), "myprovider")
	}
	if cb.Model() != "mymodel" {
		t.Errorf("Model() = %q, want %q", cb.Model(), "mymodel")
	}
}

// alternatingCBProvider succeeds on even-numbered calls and fails on odd ones.
// It is safe for concurrent use.
type alternatingCBProvider struct {
	mu    sync.Mutex
	calls int
}

func (a *alternatingCBProvider) Name() string  { return "alternating" }
func (a *alternatingCBProvider) Model() string { return "alt-model" }
func (a *alternatingCBProvider) Chat(_ context.Context, _ string, _ []Message, _ []Tool) (*Response, error) {
	a.mu.Lock()
	n := a.calls
	a.calls++
	a.mu.Unlock()

	if n%2 == 0 {
		return &Response{Content: "ok", StopReason: StopReasonEndTurn}, nil
	}
	return nil, cbErrBackend
}

func TestCircuitBreaker_ThreadSafety(t *testing.T) {
	inner := &alternatingCBProvider{}
	cb := newCB(inner, 5, 10*time.Millisecond)

	var wg sync.WaitGroup
	const goroutines = 20
	const callsEach = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				cb.Chat(context.Background(), "", nil, nil) //nolint:errcheck
				cb.State()
			}
		}()
	}

	wg.Wait()

	// No panic or data race is the primary assertion; also verify state is valid.
	state := cb.State()
	if state != "closed" && state != "open" && state != "half_open" {
		t.Errorf("unexpected state after concurrent access: %s", state)
	}
}
