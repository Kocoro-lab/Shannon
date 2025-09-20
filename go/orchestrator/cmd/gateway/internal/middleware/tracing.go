package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TracingMiddleware provides distributed tracing support
type TracingMiddleware struct {
	logger *zap.Logger
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(logger *zap.Logger) *TracingMiddleware {
	return &TracingMiddleware{
		logger: logger,
	}
}

// Middleware returns the HTTP middleware function
func (tm *TracingMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract or generate trace ID
		traceID := tm.extractTraceID(r)
		if traceID == "" {
			traceID = tm.generateTraceID()
		}

		// Extract or generate span ID
		spanID := tm.generateSpanID()

		// Add trace context to request context
		ctx = context.WithValue(ctx, "trace_id", traceID)
		ctx = context.WithValue(ctx, "span_id", spanID)

		// Add trace headers to response
		w.Header().Set("X-Trace-ID", traceID)
		w.Header().Set("X-Span-ID", spanID)

		// Log request with trace context
		tm.logger.Debug("Request received",
			zap.String("trace_id", traceID),
			zap.String("span_id", spanID),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)

		// Continue with traced request
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractTraceID extracts trace ID from request headers
func (tm *TracingMiddleware) extractTraceID(r *http.Request) string {
	// Check traceparent header (W3C Trace Context)
	if traceparent := r.Header.Get("traceparent"); traceparent != "" {
		parts := strings.Split(traceparent, "-")
		if len(parts) >= 2 {
			return parts[1] // trace-id is the second part
		}
	}

	// Check X-Trace-ID header (custom)
	if traceID := r.Header.Get("X-Trace-ID"); traceID != "" {
		return traceID
	}

	// Check X-Request-ID header (common alternative)
	if requestID := r.Header.Get("X-Request-ID"); requestID != "" {
		return requestID
	}

	return ""
}

// generateTraceID generates a new trace ID
func (tm *TracingMiddleware) generateTraceID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// generateSpanID generates a new span ID
func (tm *TracingMiddleware) generateSpanID() string {
	// Use a shorter ID for spans
	id := uuid.New()
	return strings.ReplaceAll(id.String()[:16], "-", "")
}

// ServeHTTP implements http.Handler interface
func (tm *TracingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"Direct access not allowed"}`))
}
