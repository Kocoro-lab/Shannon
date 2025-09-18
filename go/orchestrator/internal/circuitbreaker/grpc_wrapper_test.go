package circuitbreaker

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCWrapper_ErrorClassification(t *testing.T) {
	logger := zaptest.NewLogger(t)
	wrapper := NewGRPCWrapper("test-grpc", "test-service", logger)
	ctx := context.Background()

	// Test client errors don't trip circuit breaker
	clientErrors := []codes.Code{
		codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.Unauthenticated,
	}

	for _, code := range clientErrors {
		err := wrapper.Execute(ctx, func() error {
			return status.Error(code, "client error")
		})

		// Original error should be returned
		if status.Code(err) != code {
			t.Errorf("Expected original error code %v, got %v", code, status.Code(err))
		}

		// Circuit breaker should remain closed
		if wrapper.GetState() != StateClosed {
			t.Errorf("Circuit breaker should remain closed for client error %v, got %s", code, wrapper.GetState())
		}
	}

	// Test server errors DO trip circuit breaker
	serverErrors := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Internal,
	}

	// Reset circuit breaker for server error tests
	wrapper = NewGRPCWrapper("test-grpc-server", "test-service", logger)

	failureCount := 0
	for _, code := range serverErrors {
		err := wrapper.Execute(ctx, func() error {
			return status.Error(code, "server error")
		})

		failureCount++

		// Original error should be returned (unless circuit is open)
		if wrapper.GetState() != StateOpen && status.Code(err) != code {
			t.Errorf("Expected original error code %v, got %v", code, status.Code(err))
		}

		// After enough failures, circuit should open
		if failureCount >= 3 && wrapper.GetState() != StateOpen {
			t.Errorf("Circuit breaker should be open after %d server errors, got %s", failureCount, wrapper.GetState())
		}
	}
}

func TestGRPCWrapper_CircuitBreakerOpen(t *testing.T) {
	logger := zaptest.NewLogger(t)
	wrapper := NewGRPCWrapper("test-grpc-open", "test-service", logger)
	ctx := context.Background()

	// Force circuit breaker to open by causing failures
	for i := 0; i < 3; i++ {
		wrapper.Execute(ctx, func() error {
			return status.Error(codes.Unavailable, "service unavailable")
		})
	}

	// Verify circuit is open
	if wrapper.GetState() != StateOpen {
		t.Errorf("Expected circuit breaker to be open, got %s", wrapper.GetState())
	}

	// Test that circuit breaker error is returned
	err := wrapper.Execute(ctx, func() error {
		return nil // This should not be called
	})

	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}
}

func TestGRPCWrapper_Recovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Use short timeouts for testing
	config := Config{
		MaxRequests:      2,
		Interval:         100 * time.Millisecond,
		Timeout:          50 * time.Millisecond,
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OnStateChange:    nil,
	}

	cb := NewCircuitBreaker("test-recovery", config, logger)
	wrapper := &GRPCWrapper{
		cb:      cb,
		logger:  logger,
		name:    "test-recovery",
		service: "test-service",
	}

	ctx := context.Background()

	// Trip the circuit breaker
	for i := 0; i < 2; i++ {
		wrapper.Execute(ctx, func() error {
			return status.Error(codes.Unavailable, "service unavailable")
		})
	}

	// Verify it's open
	if wrapper.GetState() != StateOpen {
		t.Errorf("Expected circuit breaker to be open, got %s", wrapper.GetState())
	}

	// Wait for transition to half-open
	time.Sleep(70 * time.Millisecond)

	// First successful call should close the circuit
	err := wrapper.Execute(ctx, func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected successful call, got %v", err)
	}

	// Circuit should be closed now
	if wrapper.GetState() != StateClosed {
		t.Errorf("Expected circuit breaker to be closed after successful recovery, got %s", wrapper.GetState())
	}
}
