package interceptors

import (
	"context"
	"net/http"

	"go.temporal.io/sdk/activity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// WorkflowHTTPRoundTripper adds workflow metadata to outgoing HTTP requests
type WorkflowHTTPRoundTripper struct {
	base http.RoundTripper
}

// NewWorkflowHTTPRoundTripper creates a new HTTP interceptor that adds workflow metadata
func NewWorkflowHTTPRoundTripper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &WorkflowHTTPRoundTripper{base: base}
}

// RoundTrip implements http.RoundTripper and injects workflow headers
func (w *WorkflowHTTPRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if we're running in an activity context
	// Handle panic gracefully if not in Temporal context (e.g., during tests)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Not in activity context, continue without headers
			}
		}()

		info := activity.GetInfo(req.Context())
		if info.WorkflowExecution.ID != "" {
			// Add workflow ID and run ID headers
			req.Header.Set("X-Workflow-ID", info.WorkflowExecution.ID)
			req.Header.Set("X-Run-ID", info.WorkflowExecution.RunID)
		}
	}()

	return w.base.RoundTrip(req)
}

// WorkflowUnaryClientInterceptor adds workflow metadata to outgoing gRPC requests
func WorkflowUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Add workflow metadata if available
		info := activity.GetInfo(ctx)
		if info.WorkflowExecution.ID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-workflow-id", info.WorkflowExecution.ID)
			ctx = metadata.AppendToOutgoingContext(ctx, "x-run-id", info.WorkflowExecution.RunID)
		}

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// WorkflowStreamClientInterceptor adds workflow metadata to outgoing gRPC streams
func WorkflowStreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		// Add workflow metadata if available
		info := activity.GetInfo(ctx)
		if info.WorkflowExecution.ID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-workflow-id", info.WorkflowExecution.ID)
			ctx = metadata.AppendToOutgoingContext(ctx, "x-run-id", info.WorkflowExecution.RunID)
		}

		return streamer(ctx, desc, cc, method, opts...)
	}
}
