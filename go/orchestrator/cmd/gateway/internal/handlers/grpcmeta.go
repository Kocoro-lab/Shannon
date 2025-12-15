package handlers

import (
	"context"
	"net/http"

	"google.golang.org/grpc/metadata"
)

// withGRPCMetadata attaches authentication and tracing headers from the HTTP request
// to the outgoing gRPC context. It supports X-API-Key and Authorization (Bearer),
// as well as W3C traceparent for tracing propagation.
func withGRPCMetadata(ctx context.Context, r *http.Request) context.Context {
	md := metadata.MD{}
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		md.Set("x-api-key", apiKey)
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		md.Set("authorization", auth)
	}
	if traceParent := r.Header.Get("traceparent"); traceParent != "" {
		md.Set("traceparent", traceParent)
	}
	// Pass user/tenant IDs for ownership checks (dev mode support)
	if userID := r.Header.Get("x-user-id"); userID != "" {
		md.Set("x-user-id", userID)
	}
	if tenantID := r.Header.Get("x-tenant-id"); tenantID != "" {
		md.Set("x-tenant-id", tenantID)
	}
	if len(md) > 0 {
		return metadata.NewOutgoingContext(ctx, md)
	}
	return ctx
}
