package workflows

import (
	"fmt"
	"sync"
	"time"

	"go.temporal.io/sdk/workflow"
)

// CircuitBreakerState represents the state of the circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern for workflows
type CircuitBreaker struct {
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenRequests int

	mu           sync.RWMutex
	state        CircuitBreakerState
	failures     int
	lastFailTime time.Time
	successCount int
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration, halfOpenRequests int) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		halfOpenRequests: halfOpenRequests,
		state:            StateClosed,
	}
}

// Call executes the function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit should transition from Open to HalfOpen
	if cb.state == StateOpen {
		if time.Since(cb.lastFailTime) > cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.onFailure()
		return err
	}

	cb.onSuccess()
	return nil
}

func (cb *CircuitBreaker) onSuccess() {
	cb.failures = 0

	switch cb.state {
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenRequests {
			cb.state = StateClosed
		}
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.maxFailures {
			cb.state = StateOpen
		}
	case StateHalfOpen:
		cb.state = StateOpen
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// WorkflowCircuitBreaker provides circuit breaker functionality for Temporal workflows
type WorkflowCircuitBreaker struct {
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenRequests int

	state        CircuitBreakerState
	failures     int
	lastFailTime time.Time
	successCount int
}

// NewWorkflowCircuitBreaker creates a circuit breaker for use in workflows
func NewWorkflowCircuitBreaker(maxFailures int, resetTimeout time.Duration) *WorkflowCircuitBreaker {
	return &WorkflowCircuitBreaker{
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		halfOpenRequests: 2, // Try 2 requests in half-open state
		state:            StateClosed,
	}
}

// Execute runs a function with circuit breaker protection in a workflow context
func (cb *WorkflowCircuitBreaker) Execute(ctx workflow.Context, name string, fn func() error) error {
	logger := workflow.GetLogger(ctx)

	// Check circuit state
	now := workflow.Now(ctx)

	if cb.state == StateOpen {
		if now.Sub(cb.lastFailTime) > cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			logger.Info("Circuit breaker transitioning to half-open", "name", name)
		} else {
			logger.Warn("Circuit breaker is open, rejecting request", "name", name)
			return fmt.Errorf("circuit breaker '%s' is open", name)
		}
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.failures++
		cb.lastFailTime = now

		if cb.state == StateClosed && cb.failures >= cb.maxFailures {
			cb.state = StateOpen
			logger.Error("Circuit breaker opened due to failures",
				"name", name,
				"failures", cb.failures,
				"max_failures", cb.maxFailures,
			)
		} else if cb.state == StateHalfOpen {
			cb.state = StateOpen
			logger.Warn("Circuit breaker reopened in half-open state", "name", name)
		}

		return err
	}

	// Success handling
	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.halfOpenRequests {
			cb.state = StateClosed
			cb.failures = 0
			logger.Info("Circuit breaker closed after successful recovery", "name", name)
		}
	} else {
		cb.failures = 0
	}

	return nil
}

// IsOpen returns true if the circuit breaker is open
func (cb *WorkflowCircuitBreaker) IsOpen() bool {
	return cb.state == StateOpen
}

// Reset resets the circuit breaker to closed state
func (cb *WorkflowCircuitBreaker) Reset() {
	cb.state = StateClosed
	cb.failures = 0
	cb.successCount = 0
}