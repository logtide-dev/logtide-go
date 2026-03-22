// Package circuitbreaker implements the circuit-breaker pattern to prevent
// cascading failures when a downstream service is unavailable.
package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// ErrOpen is returned by Allow when the circuit is in the open state.
var ErrOpen = errors.New("circuit breaker is open")

// State represents the state of the circuit breaker.
type State int

const (
	StateClosed   State = iota // requests allowed
	StateOpen                  // requests blocked
	StateHalfOpen              // one probe request allowed
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker is a three-state (closed/open/half-open) circuit breaker.
// It is safe for concurrent use.
type CircuitBreaker struct {
	mu               sync.Mutex
	failureThreshold int
	timeout          time.Duration
	state            State
	failures         int
	lastStateChange  time.Time
}

// New creates a CircuitBreaker with the given failure threshold and open timeout.
// Passing threshold=0 or a negative value disables the circuit breaker (always allows).
func New(failureThreshold int, timeout time.Duration) *CircuitBreaker {
	if failureThreshold < 0 {
		failureThreshold = 0
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		timeout:          timeout,
		state:            StateClosed,
		lastStateChange:  time.Now(),
	}
}

// Allow checks whether a request is permitted.
// Returns ErrOpen when the circuit is open and the recovery timeout has not elapsed.
// When failureThreshold is 0 (disabled), Allow always returns nil.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.failureThreshold == 0 {
		return nil
	}
	if cb.state == StateOpen {
		if time.Since(cb.lastStateChange) >= cb.timeout {
			cb.state = StateHalfOpen
			cb.lastStateChange = time.Now()
		} else {
			return ErrOpen
		}
	}
	return nil
}

// RecordSuccess records a successful request.
// Transitions from HalfOpen → Closed and resets the failure counter.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		cb.lastStateChange = time.Now()
	}
}

// RecordFailure records a failed request.
// A single failure in HalfOpen reopens the circuit immediately.
// In Closed state, the circuit opens once failures reaches the threshold.
// When failureThreshold is 0 (disabled), RecordFailure is a no-op.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.failureThreshold == 0 {
		return
	}
	if cb.state == StateHalfOpen {
		// A single failure in HalfOpen reopens the circuit.
		// Reset the counter so it starts clean for the next Closed phase.
		cb.failures = 0
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
		return
	}
	cb.failures++
	if cb.failures >= cb.failureThreshold {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
	}
}

// State returns the current state.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset forces the circuit breaker back to the Closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failures = 0
	cb.lastStateChange = time.Now()
}
