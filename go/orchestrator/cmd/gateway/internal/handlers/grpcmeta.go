package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"google.golang.org/grpc/metadata"
)

// withGRPCMetadata attaches authentication and tracing headers from the HTTP request
// to the outgoing gRPC context. It supports X-API-Key and Authorization (Bearer),
// as well as W3C traceparent for tracing propagation.
func withGRPCMetadata(ctx context.Context, r *http.Request) context.Context {
	md := metadata.MD{}

	if traceParent := strings.TrimSpace(r.Header.Get("traceparent")); traceParent != "" {
		md.Set("traceparent", traceParent)
	}

	// Forward auth headers.
	// IMPORTANT: orchestrator gRPC auth checks Authorization as JWT first and does not fall back to API key,
	// so we must map Bearer API keys to x-api-key metadata (not authorization).
	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		md.Set("x-api-key", normalizeAPIKey(apiKey))
	} else if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		raw := strings.TrimSpace(authHeader[len("bearer "):])
		if raw != "" {
			if isLikelyJWT(raw) {
				md.Set("authorization", authHeader)
			} else {
				md.Set("x-api-key", normalizeAPIKey(raw))
			}
		}
	} else if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		// Non-bearer auth (unlikely for OSS), forward as-is.
		md.Set("authorization", authHeader)
	}

	// Pass user/tenant IDs for ownership checks
	// First try to get from auth context (set by auth middleware).
	if userCtx, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext); ok {
		md.Set("x-user-id", userCtx.UserID.String())
		md.Set("x-tenant-id", userCtx.TenantID.String())
	} else {
		// Fallback to HTTP headers (dev mode support)
		if userID := strings.TrimSpace(r.Header.Get("x-user-id")); userID != "" {
			md.Set("x-user-id", userID)
		}
		if tenantID := strings.TrimSpace(r.Header.Get("x-tenant-id")); tenantID != "" {
			md.Set("x-tenant-id", tenantID)
		}
	}
	if len(md) > 0 {
		return metadata.NewOutgoingContext(ctx, md)
	}
	return ctx
}

func normalizeAPIKey(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "sk-shannon-") {
		token = strings.TrimPrefix(token, "sk-shannon-")
		if !strings.HasPrefix(token, "sk_") {
			token = "sk_" + token
		}
	}
	return token
}

func isLikelyJWT(token string) bool {
	return strings.Count(token, ".") == 2
}
