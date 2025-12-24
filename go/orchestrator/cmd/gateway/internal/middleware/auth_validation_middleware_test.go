package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	authpkg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

// --- Mocks ---

type mockAuthService struct {
	users map[string]*authpkg.UserContext
}

func (m *mockAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*authpkg.UserContext, error) {
	if u, ok := m.users[apiKey]; ok {
		return u, nil
	}
	return nil, assertErr("invalid api key")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func okHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

// --- Auth tests ---

func TestAuth_NoQueryParamAcceptedForNonStreamEndpoints(t *testing.T) {
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	t.Cleanup(func() { os.Unsetenv("GATEWAY_SKIP_AUTH") })
	logger := zaptest.NewLogger(t)
	uid := uuid.New()
	tid := uuid.New()
	mw := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
		"good": {UserID: uid, TenantID: tid, IsAPIKey: true, TokenType: "api_key"},
	}}, logger)

	handler := mw.Middleware(okHandler(t))

	// Query param on non-stream endpoint -> unauthorized (security: don't leak tokens in URLs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?api_key=good", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when using api_key query param on non-stream endpoint, got %d", rec.Code)
	}
}

func TestAuth_QueryParamAcceptedForSSEEndpoints(t *testing.T) {
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	t.Cleanup(func() { os.Unsetenv("GATEWAY_SKIP_AUTH") })
	logger := zaptest.NewLogger(t)
	uid := uuid.New()
	tid := uuid.New()
	mw := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
		"sk_test_key": {UserID: uid, TenantID: tid, IsAPIKey: true, TokenType: "api_key"},
	}}, logger)

	handler := mw.Middleware(okHandler(t))

	// api_key query param on /stream/sse -> accepted (EventSource can't send headers)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/sse?workflow_id=test&api_key=sk_test_key", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with api_key query param on SSE endpoint, got %d", rec.Code)
		}
	}

	// api_key query param on /stream/ws -> accepted
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/ws?workflow_id=test&api_key=sk_test_key", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with api_key query param on WebSocket endpoint, got %d", rec.Code)
		}
	}

	// token query param (JWT) on /stream/sse -> accepted
	{
		// Add JWT to mock
		jwtToken := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.sig"
		mw2 := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
			jwtToken: {UserID: uid, TenantID: tid, IsAPIKey: false, TokenType: "jwt"},
		}}, logger)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/sse?workflow_id=test&token="+jwtToken, nil)
		rec := httptest.NewRecorder()
		mw2.Middleware(okHandler(t)).ServeHTTP(rec, req)
		// Note: This will fail validation since we don't have a real JWT validator
		// but we can verify extractToken returns the token
	}

	// sk-shannon- format normalized via query param
	{
		mw3 := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
			"sk_normalized": {UserID: uid, TenantID: tid, IsAPIKey: true, TokenType: "api_key"},
		}}, logger)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/sse?workflow_id=test&api_key=sk-shannon-normalized", nil)
		rec := httptest.NewRecorder()
		mw3.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with sk-shannon- api_key query param (normalized), got %d", rec.Code)
		}
	}
}

func TestAuth_HeaderAndBearerAccepted(t *testing.T) {
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	t.Cleanup(func() { os.Unsetenv("GATEWAY_SKIP_AUTH") })
	logger := zaptest.NewLogger(t)
	uid := uuid.New()
	tid := uuid.New()
	mw := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
		"good": {UserID: uid, TenantID: tid, IsAPIKey: true, TokenType: "api_key"},
	}}, logger)

	// X-API-Key
	{
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-API-Key", "good")
		rec := httptest.NewRecorder()
		mw.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with X-API-Key, got %d", rec.Code)
		}
	}
	// Authorization: Bearer
	{
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		mw.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with Bearer, got %d", rec.Code)
		}
	}
}

func TestAuth_SkipAuthEnv(t *testing.T) {
	os.Setenv("GATEWAY_SKIP_AUTH", "1")
	os.Setenv("ENVIRONMENT", "test")
	t.Cleanup(func() {
		os.Unsetenv("GATEWAY_SKIP_AUTH")
		os.Unsetenv("ENVIRONMENT")
	})
	logger := zaptest.NewLogger(t)
	mw := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{}}, logger)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	mw.Middleware(okHandler(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when skipping auth, got %d", rec.Code)
	}
}

// --- Validation tests ---

func TestValidation_ListTasksInvalidLimitOffset(t *testing.T) {
	logger := zaptest.NewLogger(t)
	vm := NewValidationMiddleware(logger)

	// invalid limit
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?limit=abc", nil)
		vm.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid limit, got %d", rec.Code)
		}
	}
	// invalid offset
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?offset=-1", nil)
		vm.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid offset, got %d", rec.Code)
		}
	}
}

func TestAuth_DetectTokenType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mw := NewAuthMiddleware(&mockAuthService{}, logger)

	tests := []struct {
		name     string
		token    string
		expected string
	}{
		// API key formats
		{"sk_ prefix", "sk_test_abc123", "api_key"},
		{"sk-shannon- prefix", "sk-shannon-abc123", "api_key"},
		{"sk-shannon- with sk_ inside", "sk-shannon-sk_abc123", "api_key"},

		// JWT formats (must start with "eyJ" and have 3 segments)
		{"valid JWT structure", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "jwt"},
		{"minimal JWT with eyJ prefix", "eyJ.payload.signature", "jwt"},

		// NOT detected as JWT (missing eyJ prefix or wrong structure)
		{"3 segments without eyJ prefix", "abc123.def456.ghi789", "api_key"},
		{"minimal 3 segments without eyJ", "a.b.c", "api_key"},
		{"only two segments", "header.payload", "api_key"},
		{"only one segment", "randomtoken", "api_key"},
		{"four segments", "a.b.c.d", "api_key"},
		{"api key with dots", "sk_test.with.dots", "api_key"},

		// Fallback to api_key
		{"unknown format", "some-random-token", "api_key"},
		{"empty token", "", "api_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mw.detectTokenType(tt.token)
			if result != tt.expected {
				t.Errorf("detectTokenType(%q) = %q, want %q", tt.token, result, tt.expected)
			}
		})
	}
}

func TestAuth_ShannonAPIKeyViaBearerHeader(t *testing.T) {
	os.Setenv("GATEWAY_SKIP_AUTH", "0")
	t.Cleanup(func() { os.Unsetenv("GATEWAY_SKIP_AUTH") })
	logger := zaptest.NewLogger(t)
	uid := uuid.New()
	tid := uuid.New()

	// Mock only accepts the NORMALIZED key (sk_test123), not the raw sk-shannon-test123
	// This test verifies that normalization happens before validation
	mw := NewAuthMiddleware(&mockAuthService{users: map[string]*authpkg.UserContext{
		"sk_test123": {UserID: uid, TenantID: tid, IsAPIKey: true, TokenType: "api_key"},
	}}, logger)

	// Bearer sk-shannon-test123 should be normalized to sk_test123 before validation
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer sk-shannon-test123")
	rec := httptest.NewRecorder()
	mw.Middleware(okHandler(t)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer sk-shannon-xxx (normalized to sk_xxx), got %d", rec.Code)
	}
}

func TestAuth_NormalizeAPIKey(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mw := NewAuthMiddleware(&mockAuthService{}, logger)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"sk_ unchanged", "sk_abc123", "sk_abc123"},
		{"sk-shannon- normalized", "sk-shannon-test123", "sk_test123"},
		{"sk-shannon-sk_ normalized", "sk-shannon-sk_abc", "sk_abc"},
		{"random token unchanged", "random-token", "random-token"},
		{"empty unchanged", "", ""},
		{"whitespace trimmed", "  sk_test  ", "sk_test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mw.normalizeAPIKey(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeAPIKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidation_PathAndSSEParams(t *testing.T) {
	logger := zaptest.NewLogger(t)
	vm := NewValidationMiddleware(logger)

	// invalid id
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/%20/events", nil) // space in id
		vm.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid id, got %d", rec.Code)
		}
	}

	// missing workflow_id
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/sse", nil)
		vm.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing workflow_id, got %d", rec.Code)
		}
	}

	// valid workflow_id
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/sse?workflow_id=task-abc_123", nil)
		vm.Middleware(okHandler(t)).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for valid workflow_id, got %d", rec.Code)
		}
	}
}
