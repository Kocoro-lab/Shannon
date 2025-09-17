package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestCircuitBreakerStates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.FailureThreshold = 3
	config.SuccessThreshold = 2
	config.MaxRequests = 5
	config.Timeout = 100 * time.Millisecond
	config.Interval = 200 * time.Millisecond

	cb := NewCircuitBreaker("test", config, logger)
	ctx := context.Background()

	// Initially should be closed
	if cb.State() != StateClosed {
		t.Errorf("Expected initial state to be closed, got %s", cb.State())
	}

	// Test successful calls don't change state
	for i := 0; i < 3; i++ {
		err := cb.Execute(ctx, func() error { return nil })
		if err != nil {
			t.Errorf("Expected success, got error: %v", err)
		}
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state to remain closed, got %s", cb.State())
	}

	// Test failure threshold triggers open state
	for i := 0; i < 3; i++ {
		err := cb.Execute(ctx, func() error { return errors.New("test error") })
		if err == nil {
			t.Error("Expected error, got nil")
		}
	}
	if cb.State() != StateOpen {
		t.Errorf("Expected state to be open, got %s", cb.State())
	}

	// Test circuit breaker rejects requests when open
	err := cb.Execute(ctx, func() error { return nil })
	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Trigger state check by attempting a call
	cb.beforeRequest()

	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state to be half-open, got %s", cb.State())
	}

	// Test success threshold in half-open transitions to closed
	for i := 0; i < 2; i++ {
		err := cb.Execute(ctx, func() error { return nil })
		if err != nil {
			t.Errorf("Expected success, got error: %v", err)
		}
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be closed, got %s", cb.State())
	}
}

func TestCircuitBreakerMaxRequests(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.MaxRequests = 2
	config.Timeout = 100 * time.Millisecond
	config.SuccessThreshold = 5 // Make sure it won't transition to closed

	cb := NewCircuitBreaker("test", config, logger)
	ctx := context.Background()

	// Force to half-open state
	cb.mutex.Lock()
	cb.state = StateHalfOpen
	cb.generation++
	cb.counts = Counts{}
	cb.mutex.Unlock()

	// First two requests should succeed
	for i := 0; i < 2; i++ {
		err := cb.Execute(ctx, func() error { return nil })
		if err != nil {
			t.Errorf("Expected success, got error: %v", err)
		}
	}

	// Third request should be rejected
	err := cb.Execute(ctx, func() error { return nil })
	if err != ErrTooManyRequests {
		t.Errorf("Expected too many requests error, got %v", err)
	}
}

func TestCircuitBreakerCounts(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	cb := NewCircuitBreaker("test", config, logger)
	ctx := context.Background()

	// Execute some successful and failed requests
	cb.Execute(ctx, func() error { return nil })
	cb.Execute(ctx, func() error { return errors.New("error") })
	cb.Execute(ctx, func() error { return nil })

	counts := cb.Counts()
	if counts.Requests != 3 {
		t.Errorf("Expected 3 requests, got %d", counts.Requests)
	}
	if counts.TotalSuccesses != 2 {
		t.Errorf("Expected 2 successes, got %d", counts.TotalSuccesses)
	}
	if counts.TotalFailures != 1 {
		t.Errorf("Expected 1 failure, got %d", counts.TotalFailures)
	}
}

func TestStateChangeCallback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.FailureThreshold = 2

	var callbackCalled bool
	var fromState, toState State
	config.OnStateChange = func(name string, from State, to State) {
		callbackCalled = true
		fromState = from
		toState = to
	}

	cb := NewCircuitBreaker("test", config, logger)
	ctx := context.Background()

	// Trigger state change to open
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func() error { return errors.New("error") })
	}

	if !callbackCalled {
		t.Error("Expected state change callback to be called")
	}
	if fromState != StateClosed || toState != StateOpen {
		t.Errorf("Expected transition from closed to open, got %s to %s", fromState, toState)
	}
}
