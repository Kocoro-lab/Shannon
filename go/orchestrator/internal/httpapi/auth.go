package httpapi

import (
	"encoding/json"
	"net/http"

	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"go.uber.org/zap"
)

// AuthHTTPHandler provides minimal HTTP endpoints for authentication.
// Endpoints:
//
//	POST /api/auth/register
//	POST /api/auth/login
type AuthHTTPHandler struct {
	svc    *auth.Service
	logger *zap.Logger
}

// NewAuthHTTPHandler constructs a new handler.
func NewAuthHTTPHandler(svc *auth.Service, logger *zap.Logger) *AuthHTTPHandler {
	return &AuthHTTPHandler{svc: svc, logger: logger}
}

// RegisterRoutes registers auth endpoints on the given mux.
func (h *AuthHTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/register", h.handleRegister)
	mux.HandleFunc("/api/auth/login", h.handleLogin)
}

func (h *AuthHTTPHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req auth.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	// Minimal validation
	if req.Email == "" || req.Username == "" || req.Password == "" || req.FullName == "" {
		http.Error(w, `{"error":"missing required fields"}`, http.StatusBadRequest)
		return
	}

	user, err := h.svc.Register(r.Context(), &req)
	if err != nil {
		h.logger.Warn("Register failed", zap.Error(err))
		http.Error(w, `{"error":"`+sanitizeErr(err.Error())+`"}`, http.StatusBadRequest)
		return
	}

	// Respond with a safe user view
	resp := map[string]interface{}{
		"user": map[string]interface{}{
			"id":        user.ID,
			"email":     user.Email,
			"username":  user.Username,
			"tenant_id": user.TenantID,
			"role":      user.Role,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AuthHTTPHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req auth.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, `{"error":"missing email or password"}`, http.StatusBadRequest)
		return
	}

	tokens, err := h.svc.Login(r.Context(), &req)
	if err != nil {
		h.logger.Warn("Login failed", zap.Error(err))
		http.Error(w, `{"error":"invalid email or password"}`, http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

// writeJSON writes a JSON response with status and content-type.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sanitizeErr trims error messages for safe client output (UTF-8 safe).
func sanitizeErr(s string) string {
	// Keep it simple: don't leak internals
	runes := []rune(s)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return s
}
