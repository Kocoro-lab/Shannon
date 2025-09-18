package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	authpkg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AuthMiddleware provides authentication middleware
type AuthMiddleware struct {
	authService *authpkg.Service
	logger      *zap.Logger
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(authService *authpkg.Service, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authService: authService,
		logger:      logger,
	}
}

// Middleware returns the HTTP middleware function
func (m *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if auth should be skipped (development only)
		if os.Getenv("GATEWAY_SKIP_AUTH") == "1" {
			// Create a mock user context for development
			userCtx := &authpkg.UserContext{
				UserID:    uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				TenantID:  uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Username:  "admin",
				Email:     "admin@shannon.local",
				Role:      "admin",
				IsAPIKey:  true,
				TokenType: "api_key",
			}
			ctx := context.WithValue(r.Context(), "user", userCtx)
			m.logger.Debug("Auth skipped (GATEWAY_SKIP_AUTH=1)",
				zap.String("path", r.URL.Path),
			)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Extract API key from header or query parameter
		apiKey := m.extractAPIKey(r)
		if apiKey == "" {
			m.sendUnauthorized(w, "API key is required")
			return
		}

		// Validate API key using the auth service
		userCtx, err := m.authService.ValidateAPIKey(r.Context(), apiKey)
		if err != nil {
			m.logger.Debug("API key validation failed",
				zap.Error(err),
				zap.String("api_key_prefix", m.getKeyPrefix(apiKey)),
			)
			m.sendUnauthorized(w, "Invalid API key")
			return
		}

		// Add user context to request context
		ctx := context.WithValue(r.Context(), "user", userCtx)

		// Log successful authentication
		m.logger.Debug("Request authenticated",
			zap.String("user_id", userCtx.UserID.String()),
			zap.String("tenant_id", userCtx.TenantID.String()),
			zap.String("path", r.URL.Path),
		)

		// Continue with authenticated request
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractAPIKey extracts the API key from the request
func (m *AuthMiddleware) extractAPIKey(r *http.Request) string {
	// Check X-API-Key header
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	// Check Authorization header with Bearer token
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.Split(auth, " ")
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check api_key query parameter (less secure, but convenient for SSE)
	if apiKey := r.URL.Query().Get("api_key"); apiKey != "" {
		return apiKey
	}

	return ""
}

// getKeyPrefix returns the first few characters of the API key for logging
func (m *AuthMiddleware) getKeyPrefix(apiKey string) string {
	if len(apiKey) > 8 {
		return apiKey[:8] + "..."
	}
	return "***"
}

// sendUnauthorized sends an unauthorized response
func (m *AuthMiddleware) sendUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="Shannon API"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + message + `"}`))
}

// ServeHTTP implements http.Handler interface for convenience
func (m *AuthMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// This allows the middleware to be used directly as a handler
	// It will reject all requests since there's no next handler
	m.sendUnauthorized(w, "Direct access not allowed")
}