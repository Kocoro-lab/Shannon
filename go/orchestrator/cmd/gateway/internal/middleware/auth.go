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

// APIKeyValidator interface for API key validation
type APIKeyValidator interface {
	ValidateAPIKey(ctx context.Context, apiKey string) (*authpkg.UserContext, error)
}

// JWTValidator interface for JWT token validation
type JWTValidator interface {
	ValidateAccessToken(tokenString string) (*authpkg.UserContext, error)
}

// AuthMiddleware provides authentication middleware supporting both API keys and JWTs
type AuthMiddleware struct {
	authService  APIKeyValidator
	jwtValidator JWTValidator // Optional: if nil, JWT auth is disabled
	logger       *zap.Logger
}

// NewAuthMiddleware creates a new authentication middleware (API key only)
func NewAuthMiddleware(authService APIKeyValidator, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authService: authService,
		logger:      logger,
	}
}

// NewAuthMiddlewareWithJWT creates a new authentication middleware with JWT support
func NewAuthMiddlewareWithJWT(authService APIKeyValidator, jwtValidator JWTValidator, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authService:  authService,
		jwtValidator: jwtValidator,
		logger:       logger,
	}
}

// Middleware returns the HTTP middleware function
func (m *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if auth should be skipped (DEVELOPMENT ONLY - NEVER USE IN PRODUCTION)
		env := os.Getenv("ENVIRONMENT")
		skipAuth := os.Getenv("GATEWAY_SKIP_AUTH")

		if skipAuth == "1" {
			// Only allow auth skip in development environment
			if env != "development" && env != "dev" && env != "test" {
				m.logger.Error("SECURITY WARNING: GATEWAY_SKIP_AUTH enabled in non-development environment",
					zap.String("environment", env),
					zap.String("path", r.URL.Path),
				)
				m.sendUnauthorized(w, "Authentication required")
				return
			}

			m.logger.Warn("Authentication bypassed (DEVELOPMENT MODE ONLY)",
				zap.String("environment", env),
				zap.String("path", r.URL.Path),
			)

			// In dev mode, respect x-user-id and x-tenant-id headers if provided
			// This allows testing ownership/tenancy isolation without real auth
			userID := uuid.MustParse("00000000-0000-0000-0000-000000000002")   // default
			tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001") // default

			if headerUserID := r.Header.Get("x-user-id"); headerUserID != "" {
				if parsed, err := uuid.Parse(headerUserID); err == nil {
					userID = parsed
				}
			}
			if headerTenantID := r.Header.Get("x-tenant-id"); headerTenantID != "" {
				if parsed, err := uuid.Parse(headerTenantID); err == nil {
					tenantID = parsed
				}
			}

			userCtx := &authpkg.UserContext{
				UserID:    userID,
				TenantID:  tenantID,
				Username:  "admin",
				Email:     "admin@shannon.local",
				Role:      "admin",
				IsAPIKey:  true,
				TokenType: "api_key",
			}
			ctx := context.WithValue(r.Context(), authpkg.UserContextKey, userCtx)
			ctx = context.WithValue(ctx, "user", userCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Extract token from headers (API key or JWT)
		token, tokenType := m.extractToken(r)
		if token == "" {
			m.sendUnauthorized(w, "Authentication required")
			return
		}

		var userCtx *authpkg.UserContext
		var err error

		// Validate based on token type
		switch tokenType {
		case "api_key":
			// Normalize API key format before validation (sk-shannon-xxx → sk_xxx)
			normalizedToken := m.normalizeAPIKey(token)
			userCtx, err = m.authService.ValidateAPIKey(r.Context(), normalizedToken)
			if err != nil {
				m.logger.Debug("API key validation failed",
					zap.Error(err),
					zap.String("api_key_prefix", m.getKeyPrefix(token)),
				)
				m.sendUnauthorized(w, "Invalid API key")
				return
			}
		case "jwt":
			if m.jwtValidator == nil {
				m.logger.Debug("JWT validation not configured, rejecting JWT token")
				m.sendUnauthorized(w, "JWT authentication not supported")
				return
			}
			userCtx, err = m.jwtValidator.ValidateAccessToken(token)
			if err != nil {
				m.logger.Debug("JWT validation failed", zap.Error(err))
				m.sendUnauthorized(w, "Invalid or expired token")
				return
			}
		default:
			m.sendUnauthorized(w, "Invalid authentication token")
			return
		}

		// Add user context to request context
		ctx := context.WithValue(r.Context(), authpkg.UserContextKey, userCtx)
		ctx = context.WithValue(ctx, "user", userCtx)

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

// extractToken extracts the authentication token and its type from the request.
// Returns (token, type) where type is "api_key" or "jwt".
func (m *AuthMiddleware) extractToken(r *http.Request) (string, string) {
	// Check X-API-Key header (always an API key)
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey, "api_key"
	}

	// Check Authorization header with Bearer token
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.Split(auth, " ")
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			token := parts[1]
			tokenType := m.detectTokenType(token)
			return token, tokenType
		}
	}

	// For SSE/WebSocket endpoints, check query parameters
	// Browser's EventSource API cannot send custom headers, so we support query params
	if strings.Contains(r.URL.Path, "/stream/") {
		if apiKey := r.URL.Query().Get("api_key"); apiKey != "" {
			return apiKey, "api_key"
		}
		if token := r.URL.Query().Get("token"); token != "" {
			tokenType := m.detectTokenType(token)
			return token, tokenType
		}
	}

	return "", ""
}

// detectTokenType determines if a token is an API key or JWT.
// API keys start with "sk_" or "sk-shannon-" prefix.
// JWTs must start with "eyJ" (base64 for '{"') and have 3 dot-separated segments.
func (m *AuthMiddleware) detectTokenType(token string) string {
	// Check for API key prefixes first (both internal sk_ and external sk-shannon- formats)
	if strings.HasPrefix(token, "sk_") || strings.HasPrefix(token, "sk-shannon-") {
		return "api_key"
	}

	// JWT detection: must have 3 segments and start with "eyJ" (base64-encoded JSON object)
	// The "eyJ" prefix indicates the header starts with '{"' which is standard for JWTs
	// This avoids misclassifying API keys that happen to contain two dots
	if strings.HasPrefix(token, "eyJ") && strings.Count(token, ".") == 2 {
		return "jwt"
	}

	// Default to API key for backwards compatibility
	return "api_key"
}

// normalizeAPIKey converts external API key formats to internal format.
// sk-shannon-xxx → sk_xxx (strips "sk-shannon-" prefix and ensures "sk_" prefix)
func (m *AuthMiddleware) normalizeAPIKey(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "sk-shannon-") {
		token = strings.TrimPrefix(token, "sk-shannon-")
		if !strings.HasPrefix(token, "sk_") {
			token = "sk_" + token
		}
	}
	return token
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
