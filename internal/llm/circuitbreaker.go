package llm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is open and rejects calls.
var ErrCircuitOpen = fmt.Errorf("circuit breaker is open")

// circuitState represents the current state of the circuit breaker.
type circuitState int

const (
	stateClosed   circuitState = iota // normal operation, calls pass through
	stateOpen                         // rejecting all calls
	stateHalfOpen                     // allowing one test call
)

func (s circuitState) String() string {
	switch s {
	case stateClosed:
		return "closed"
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

const (
	defaultMaxFailures = 5
	defaultCooldown    = 5 * time.Minute
)

// CircuitBreaker wraps a Provider and implements the Provider interface,
// tracking failures and short-circuiting calls when the inner provider is
// consistently failing.
type CircuitBreaker struct {
	inner       Provider
	maxFailures int
	cooldown    time.Duration

	mu           sync.Mutex
	state        circuitState
	failureCount int
	openedAt     time.Time
}

// NewCircuitBreaker creates a CircuitBreaker wrapping inner. Zero values for
// maxFailures or cooldown fall back to defaults (5 failures, 5 minutes).
func NewCircuitBreaker(inner Provider, maxFailures int, cooldown time.Duration) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = defaultMaxFailures
	}
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	return &CircuitBreaker{
		inner:       inner,
		maxFailures: maxFailures,
		cooldown:    cooldown,
		state:       stateClosed,
	}
}

// Name delegates to the inner provider.
func (cb *CircuitBreaker) Name() string {
	return cb.inner.Name()
}

// Model delegates to the inner provider.
func (cb *CircuitBreaker) Model() string {
	return cb.inner.Model()
}

// State returns the current circuit breaker state as a string for observability.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state.String()
}

// Chat forwards the call to the inner provider if the circuit allows it,
// updating state based on the outcome.
func (cb *CircuitBreaker) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	if err := cb.beforeCall(); err != nil {
		return nil, err
	}

	resp, err := cb.inner.Chat(ctx, system, messages, tools)
	cb.afterCall(err)
	return resp, err
}

// beforeCall checks and potentially transitions state before a call is made.
// It returns ErrCircuitOpen if the call should be rejected.
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateClosed, stateHalfOpen:
		return nil
	case stateOpen:
		if time.Since(cb.openedAt) >= cb.cooldown {
			cb.state = stateHalfOpen
			return nil
		}
		return ErrCircuitOpen
	default:
		return nil
	}
}

// afterCall updates circuit state based on whether the call succeeded or failed.
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		cb.failureCount = 0
		cb.state = stateClosed
		return
	}

	switch cb.state {
	case stateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.maxFailures {
			cb.state = stateOpen
			cb.openedAt = time.Now()
		}
	case stateHalfOpen:
		cb.state = stateOpen
		cb.openedAt = time.Now()
	}
}
