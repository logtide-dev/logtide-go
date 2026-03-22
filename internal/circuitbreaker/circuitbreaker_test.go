package circuitbreaker_test

import (
	"errors"
	"testing"
	"time"

	"github.com/logtide-dev/logtide-sdk-go/internal/circuitbreaker"
)

func TestInitiallyClosed(t *testing.T) {
	cb := circuitbreaker.New(3, 100*time.Millisecond)
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("initial state = %v, want Closed", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() = %v, want nil", err)
	}
}

func TestOpensAfterThreshold(t *testing.T) {
	cb := circuitbreaker.New(3, 100*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("state after 2 failures = %v, want Closed", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("state after 3 failures = %v, want Open", cb.State())
	}
	if err := cb.Allow(); !errors.Is(err, circuitbreaker.ErrOpen) {
		t.Errorf("Allow() = %v, want ErrOpen", err)
	}
}

func TestTransitionsToHalfOpen(t *testing.T) {
	cb := circuitbreaker.New(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()

	time.Sleep(60 * time.Millisecond)

	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() after timeout = %v, want nil", err)
	}
	if cb.State() != circuitbreaker.StateHalfOpen {
		t.Errorf("state = %v, want HalfOpen", cb.State())
	}
}

func TestHalfOpenSuccess(t *testing.T) {
	cb := circuitbreaker.New(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	cb.RecordSuccess()

	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("state after success = %v, want Closed", cb.State())
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	cb := circuitbreaker.New(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("state after half-open failure = %v, want Open", cb.State())
	}
}

func TestReset(t *testing.T) {
	cb := circuitbreaker.New(2, 100*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.Reset()

	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("state after reset = %v, want Closed", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() after reset = %v, want nil", err)
	}
}

func TestDisabledCircuitBreaker(t *testing.T) {
	// threshold=0 means disabled: Allow always returns nil, RecordFailure is no-op.
	cb := circuitbreaker.New(0, 100*time.Millisecond)
	for i := 0; i < 100; i++ {
		cb.RecordFailure()
	}
	if err := cb.Allow(); err != nil {
		t.Errorf("Allow() on disabled CB = %v, want nil", err)
	}
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("state = %v, want Closed (disabled CB should never open)", cb.State())
	}
}

func TestHalfOpenFailureResetsCounter(t *testing.T) {
	// After HalfOpen→Open, the failure counter should be 0.
	// Then if the CB closes again and receives failures, it should need
	// threshold failures to reopen (not fewer because of leftover state).
	cb := circuitbreaker.New(3, 50*time.Millisecond)
	// Open the circuit.
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateOpen {
		t.Fatalf("expected Open, got %v", cb.State())
	}
	// Wait for timeout → HalfOpen.
	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	if cb.State() != circuitbreaker.StateHalfOpen {
		t.Fatalf("expected HalfOpen, got %v", cb.State())
	}
	// One failure in HalfOpen reopens immediately.
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("expected Open after HalfOpen failure, got %v", cb.State())
	}
	// Now recover again → HalfOpen → Closed.
	time.Sleep(60 * time.Millisecond)
	cb.Allow()
	cb.RecordSuccess()
	if cb.State() != circuitbreaker.StateClosed {
		t.Fatalf("expected Closed, got %v", cb.State())
	}
	// Counter was reset on HalfOpen→Open, so we need full threshold again.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("should still be closed after 2 of 3 failures, got %v", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("expected Open after 3 failures, got %v", cb.State())
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	cb := circuitbreaker.New(3, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	// 2 failures — not yet open. RecordSuccess should reset them.
	cb.RecordSuccess()
	if cb.State() != circuitbreaker.StateClosed {
		t.Fatalf("expected Closed, got %v", cb.State())
	}
	// Now it takes another full threshold to open.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("should still be closed after 2 failures (counter reset), got %v", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("expected Open after 3 failures, got %v", cb.State())
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		s    circuitbreaker.State
		want string
	}{
		{circuitbreaker.StateClosed, "closed"},
		{circuitbreaker.StateOpen, "open"},
		{circuitbreaker.StateHalfOpen, "half-open"},
		{circuitbreaker.State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
