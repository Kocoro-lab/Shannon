package circuitbreaker

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCWrapper provides circuit breaker functionality for gRPC calls
type GRPCWrapper struct {
	cb      *CircuitBreaker
	logger  *zap.Logger
	name    string
	service string
}

// NewGRPCWrapper creates a gRPC wrapper with circuit breaker
func NewGRPCWrapper(name, service string, logger *zap.Logger) *GRPCWrapper {
	config := GetGRPCConfig().ToConfig()
	cb := NewCircuitBreaker(name, config, logger)

	// Register with metrics collector
	GlobalMetricsCollector.RegisterCircuitBreaker(name, service, cb)

	return &GRPCWrapper{
		cb:      cb,
		logger:  logger,
		name:    name,
		service: service,
	}
}

// Execute executes a gRPC call with circuit breaker protection
func (gw *GRPCWrapper) Execute(ctx context.Context, fn func() error) error {
	var originalErr error

	cbErr := gw.cb.Execute(ctx, func() error {
		originalErr = fn()
		// Only count server/transient errors toward circuit breaker state
		if originalErr != nil && isCircuitBreakerError(originalErr) {
			return originalErr
		}
		// Client errors don't count toward circuit breaker failure
		return nil
	})

	// Record metrics based on actual error, not circuit breaker decision
	state := gw.cb.State()
	var success bool
	var err error

	if cbErr == ErrCircuitBreakerOpen || cbErr == ErrTooManyRequests {
		// Circuit breaker is open/throttling
		success = false
		err = cbErr
	} else {
		// Return original error (could be client error that doesn't trip breaker)
		success = originalErr == nil
		err = originalErr
	}

	GlobalMetricsCollector.RecordRequest(gw.name, gw.service, state, success)

	return err
}

// UnaryClientInterceptor returns a gRPC unary client interceptor with circuit breaker
func (gw *GRPCWrapper) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return gw.Execute(ctx, func() error {
			return invoker(ctx, method, req, reply, cc, opts...)
		})
	}
}

// StreamClientInterceptor returns a gRPC stream client interceptor with circuit breaker
func (gw *GRPCWrapper) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		var stream grpc.ClientStream
		err := gw.Execute(ctx, func() error {
			var streamErr error
			stream, streamErr = streamer(ctx, desc, cc, method, opts...)
			return streamErr
		})
		return stream, err
	}
}

// IsCircuitBreakerOpen returns true if the circuit breaker is open
func (gw *GRPCWrapper) IsCircuitBreakerOpen() bool {
	return gw.cb.State() == StateOpen
}

// GetState returns the current circuit breaker state
func (gw *GRPCWrapper) GetState() State {
	return gw.cb.State()
}

// isCircuitBreakerError determines if an error should trigger the circuit breaker
func isCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}

	// Circuit breaker specific errors
	if err == ErrCircuitBreakerOpen || err == ErrTooManyRequests {
		return true
	}

	// Check gRPC status codes
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Internal:
			return true
		case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.Unauthenticated:
			// These are client errors, don't trigger circuit breaker
			return false
		default:
			return true
		}
	}

	return true
}

// GRPCConnectionWrapper wraps gRPC dial with circuit breaker
type GRPCConnectionWrapper struct {
	cb      *CircuitBreaker
	logger  *zap.Logger
	target  string
	service string
}

// NewGRPCConnectionWrapper creates a connection wrapper with circuit breaker
func NewGRPCConnectionWrapper(target, service string, logger *zap.Logger) *GRPCConnectionWrapper {
	config := GetGRPCConnectionConfig().ToConfig()
	cb := NewCircuitBreaker("grpc-connection", config, logger)

	// Register with metrics collector
	GlobalMetricsCollector.RegisterCircuitBreaker("grpc-connection", service, cb)

	return &GRPCConnectionWrapper{
		cb:      cb,
		logger:  logger,
		target:  target,
		service: service,
	}
}

// DialContext dials a gRPC connection with circuit breaker protection
func (gcw *GRPCConnectionWrapper) DialContext(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn
	var err error

	cbErr := gcw.cb.Execute(ctx, func() error {
		conn, err = grpc.DialContext(ctx, gcw.target, opts...)
		return err
	})

	// Record metrics
	state := gcw.cb.State()
	success := cbErr == nil && err == nil
	GlobalMetricsCollector.RecordRequest("grpc-connection", gcw.service, state, success)

	if cbErr != nil {
		return nil, cbErr
	}
	return conn, err
}

// IsCircuitBreakerOpen returns true if the circuit breaker is open
func (gcw *GRPCConnectionWrapper) IsCircuitBreakerOpen() bool {
	return gcw.cb.State() == StateOpen
}
