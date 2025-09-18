package tracing

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.27.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var tracer oteltrace.Tracer

// Config holds tracing configuration
type Config struct {
	Enabled      bool   `mapstructure:"enabled"`
	ServiceName  string `mapstructure:"service_name"`
	OTLPEndpoint string `mapstructure:"otlp_endpoint"`
}

// Initialize sets up minimal OTLP tracing
func Initialize(cfg Config, logger *zap.Logger) error {
	// Always initialize a tracer handle, even if provider is disabled.
	// This ensures Start* helpers never panic when tracing is disabled.
	if cfg.ServiceName == "" {
		cfg.ServiceName = "shannon-orchestrator"
	}
	tracer = otel.Tracer(cfg.ServiceName)

	if !cfg.Enabled {
		logger.Info("Tracing disabled")
		return nil
	}

	// Default values
	if cfg.ServiceName == "" {
		cfg.ServiceName = "shannon-orchestrator"
	}
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = "localhost:4317"
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(
		context.Background(),
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	// Set global tracer provider and keep tracer handle
	otel.SetTracerProvider(tp)
	tracer = otel.Tracer(cfg.ServiceName)

	logger.Info("Tracing initialized", zap.String("endpoint", cfg.OTLPEndpoint))
	return nil
}

// W3CTraceparent generates a W3C traceparent header value
func W3CTraceparent(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}

	sc := span.SpanContext()
	return fmt.Sprintf("00-%s-%s-%02x",
		sc.TraceID().String(),
		sc.SpanID().String(),
		sc.TraceFlags(),
	)
}

// InjectTraceparent adds W3C traceparent header to HTTP request
func InjectTraceparent(ctx context.Context, req *http.Request) {
	if traceparent := W3CTraceparent(ctx); traceparent != "" {
		req.Header.Set("traceparent", traceparent)
	}
}

// StartSpan creates a new span with the given name
func StartSpan(ctx context.Context, spanName string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, spanName)
}

// StartHTTPSpan creates a span for HTTP operations with method and URL
func StartHTTPSpan(ctx context.Context, method, url string) (context.Context, oteltrace.Span) {
	if tracer == nil {
		tracer = otel.Tracer("shannon-orchestrator")
	}
	spanName := fmt.Sprintf("HTTP %s", method)
	ctx, span := tracer.Start(ctx, spanName)
	span.SetAttributes(
		semconv.HTTPRequestMethodKey.String(method),
		semconv.URLFull(url),
	)
	return ctx, span
}

// ParseTraceparent parses W3C traceparent header
func ParseTraceparent(traceparent string) (traceID, spanID string, flags byte, valid bool) {
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return "", "", 0, false
	}

	version := parts[0]
	if version != "00" {
		return "", "", 0, false
	}

	traceID = parts[1]
	spanID = parts[2]

	var flagsInt int
	if _, err := fmt.Sscanf(parts[3], "%02x", &flagsInt); err != nil {
		return "", "", 0, false
	}
	flags = byte(flagsInt)

	return traceID, spanID, flags, true
}
