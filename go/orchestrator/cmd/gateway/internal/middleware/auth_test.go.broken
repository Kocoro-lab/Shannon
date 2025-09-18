package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"go.uber.org/zap/zaptest"
)

// MockAuthService implements a mock authentication service for testing
type MockAuthService struct {
	ValidAPIKeys map[string]*auth.UserContext
	ShouldFail   bool
}

func (m *MockAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*auth.UserContext, error) {
	if m.ShouldFail {
		return nil, fmt.Errorf("mock auth failure")
	}
	if userCtx, ok := m.ValidAPIKeys[apiKey]; ok {
		return userCtx, nil
	}
	return nil, fmt.Errorf("invalid API key")
}

func TestAuthMiddleware_SkipAuth(t *testing.T) {
	// Set environment variable to skip auth
	os.Setenv("GATEWAY_SKIP_AUTH", "1")
	defer os.Unsetenv("GATEWAY_SKIP_AUTH")

	logger := zaptest.NewLogger(t)
	authService := &MockAuthService{
		ShouldFail: true, // Should not be called when auth is skipped
	}

	middleware := NewAuthMiddleware(authService, logger)

	// Create test handler
	handler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that user context is set
		userCtx := r.Context().Value("user")
		if userCtx == nil {
			t.Error("Expected user context to be set when auth is skipped")
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Create test request without API key
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rec, req)

	// Should succeed without authentication
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_ValidAPIKey(t *testing.T) {
	// Ensure auth is not skipped
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	defer os.Unsetenv("GATEWAY_SKIP_AUTH")

	logger := zaptest.NewLogger(t)

	testUserID := uuid.New()
	testTenantID := uuid.New()

	authService := &MockAuthService{
		ValidAPIKeys: map[string]*auth.UserContext{
			"sk_test_123456": {
				UserID:    testUserID,
				TenantID:  testTenantID,
				Username:  "testuser",
				Email:     "test@example.com",
				Role:      "user",
				IsAPIKey:  true,
				TokenType: "api_key",
			},
		},
	}

	middleware := NewAuthMiddleware(authService, logger)

	// Create test handler
	handler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that user context is set correctly
		userCtx := r.Context().Value("user").(*auth.UserContext)
		if userCtx.UserID != testUserID {
			t.Error("User ID mismatch")
		}
		if userCtx.Email != "test@example.com" {
			t.Error("Email mismatch")
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Test with X-API-Key header
	t.Run("Header Auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "sk_test_123456")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	// Test with Bearer token
	t.Run("Bearer Auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer sk_test_123456")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})

	// Test with query parameter
	t.Run("Query Param Auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?api_key=sk_test_123456", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestAuthMiddleware_InvalidAPIKey(t *testing.T) {
	// Ensure auth is not skipped
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	defer os.Unsetenv("GATEWAY_SKIP_AUTH")

	logger := zaptest.NewLogger(t)
	authService := &MockAuthService{
		ValidAPIKeys: make(map[string]*auth.UserContext),
	}

	middleware := NewAuthMiddleware(authService, logger)

	handler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid API key")
		w.WriteHeader(http.StatusOK)
	}))

	// Test with invalid API key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "sk_invalid_key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingAPIKey(t *testing.T) {
	// Ensure auth is not skipped
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	defer os.Unsetenv("GATEWAY_SKIP_AUTH")

	logger := zaptest.NewLogger(t)
	authService := &MockAuthService{}

	middleware := NewAuthMiddleware(authService, logger)

	handler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without API key")
		w.WriteHeader(http.StatusOK)
	}))

	// Test without API key
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header
	if rec.Header().Get("WWW-Authenticate") != `Bearer realm="Shannon API"` {
		t.Error("Expected WWW-Authenticate header")
	}
}

// Integration test with real database (requires running postgres with test data)
func TestAuthMiddleware_DatabaseIntegration(t *testing.T) {
	// Skip if not in integration test mode
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test (set RUN_INTEGRATION_TESTS=1 to run)")
	}

	// Connect to test database
	db, err := sqlx.Connect("postgres", "host=localhost port=5432 user=shannon password=shannon dbname=shannon sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Seed test API key
	_, err = db.Exec(`
		DELETE FROM auth.api_keys WHERE key_prefix = 'sk_test_';
		INSERT INTO auth.api_keys (
			key_hash, key_prefix, user_id, tenant_id, name, scopes, is_active
		) VALUES (
			'58a5e18e57cb4cf83cf8e4e1d420958e9297c3502468d2e33b5052b0f46cb640',
			'sk_test_',
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000001',
			'Integration Test Key',
			$1,
			true
		)
	`, pq.StringArray{"workflows:read", "workflows:write"})
	if err != nil {
		t.Fatalf("Failed to seed API key: %v", err)
	}

	// Create real auth service
	logger := zaptest.NewLogger(t)
	authService := auth.NewService(db, logger, "test-secret")

	// Test ValidateAPIKey directly
	userCtx, err := authService.ValidateAPIKey(context.Background(), "sk_test_123456")
	if err != nil {
		t.Fatalf("Failed to validate API key: %v", err)
	}

	if userCtx.UserID.String() != "00000000-0000-0000-0000-000000000002" {
		t.Errorf("Unexpected user ID: %s", userCtx.UserID)
	}

	// Test through middleware
	middleware := NewAuthMiddleware(authService, logger)

	handler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userCtx := r.Context().Value("user").(*auth.UserContext)
		if len(userCtx.Scopes) != 2 {
			t.Errorf("Expected 2 scopes, got %d", len(userCtx.Scopes))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/tasks", nil)
	req.Header.Set("X-API-Key", "sk_test_123456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Clean up
	_, err = db.Exec(`DELETE FROM auth.api_keys WHERE key_prefix = 'sk_test_'`)
	if err != nil {
		t.Logf("Failed to clean up test key: %v", err)
	}
}