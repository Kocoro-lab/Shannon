package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ContextKey is the key type for context values
type ContextKey string

const (
	// UserContextKey is the context key for user information
	UserContextKey ContextKey = "user"
)

// Middleware provides authentication middleware for HTTP and gRPC
type Middleware struct {
	authService *Service
	jwtManager  *JWTManager
	skipAuth    bool // For development/testing
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(authService *Service, jwtManager *JWTManager, skipAuth bool) *Middleware {
	return &Middleware{
		authService: authService,
		jwtManager:  jwtManager,
		skipAuth:    skipAuth,
	}
}

// HTTPMiddleware provides HTTP authentication middleware
func (m *Middleware) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if configured (for development)
		if m.skipAuth {
			// Use default dev user context
			ctx := context.WithValue(r.Context(), UserContextKey, &UserContext{
				UserID:   uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				TenantID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Username: "dev",
				Email:    "dev@shannon.local",
				Role:     RoleOwner,
				Scopes:   []string{ScopeWorkflowsRead, ScopeWorkflowsWrite, ScopeAgentsExecute},
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Try API key header
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "" {
				userCtx, err := m.authService.ValidateAPIKey(r.Context(), apiKey)
				if err != nil {
					http.Error(w, "Invalid API key", http.StatusUnauthorized)
					return
				}
				ctx := context.WithValue(r.Context(), UserContextKey, userCtx)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			http.Error(w, "Missing authorization", http.StatusUnauthorized)
			return
		}

		// Extract bearer token
		token, err := ExtractBearerToken(authHeader)
		if err != nil {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		// Validate JWT token
		userCtx, err := m.jwtManager.ValidateAccessToken(token)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Add user context to request
		ctx := context.WithValue(r.Context(), UserContextKey, userCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UnaryServerInterceptor provides gRPC authentication interceptor
func (m *Middleware) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip auth for health check
		if strings.HasSuffix(info.FullMethod, "/Health") {
			return handler(ctx, req)
		}

		// Skip auth if configured (for development)
		if m.skipAuth {
			// Use default dev user context
			ctx = context.WithValue(ctx, UserContextKey, &UserContext{
				UserID:   uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				TenantID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Username: "dev",
				Email:    "dev@shannon.local",
				Role:     RoleOwner,
				Scopes:   []string{ScopeWorkflowsRead, ScopeWorkflowsWrite, ScopeAgentsExecute},
			})
			return handler(ctx, req)
		}

		// Extract metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Try to extract token from authorization header
		var userCtx *UserContext
		if authHeaders := md.Get("authorization"); len(authHeaders) > 0 {
			token, err := ExtractBearerToken(authHeaders[0])
			if err == nil {
				userCtx, err = m.jwtManager.ValidateAccessToken(token)
				if err != nil {
					return nil, status.Error(codes.Unauthenticated, "invalid token")
				}
			}
		}

		// Try API key if no JWT found
		if userCtx == nil {
			if apiKeys := md.Get("x-api-key"); len(apiKeys) > 0 {
				var err error
				userCtx, err = m.authService.ValidateAPIKey(ctx, apiKeys[0])
				if err != nil {
					return nil, status.Error(codes.Unauthenticated, "invalid API key")
				}
			}
		}

		// No valid authentication found
		if userCtx == nil {
			return nil, status.Error(codes.Unauthenticated, "missing authentication")
		}

		// Add user context and proceed
		ctx = context.WithValue(ctx, UserContextKey, userCtx)
		return handler(ctx, req)
	}
}

// RequireScopes checks if the user has the required scopes
func RequireScopes(ctx context.Context, requiredScopes ...string) error {
	userCtx, ok := ctx.Value(UserContextKey).(*UserContext)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing user context")
	}

	// Check if user has all required scopes
	for _, required := range requiredScopes {
		found := false
		for _, scope := range userCtx.Scopes {
			if scope == required {
				found = true
				break
			}
		}
		if !found {
			return status.Errorf(codes.PermissionDenied, "missing required scope: %s", required)
		}
	}

	return nil
}

// GetUserContext extracts user context from context
func GetUserContext(ctx context.Context) (*UserContext, error) {
	userCtx, ok := ctx.Value(UserContextKey).(*UserContext)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing user context")
	}
	return userCtx, nil
}
