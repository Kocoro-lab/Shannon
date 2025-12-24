package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// rateLimitEntry tracks request timestamps for sliding window rate limiting
type rateLimitEntry struct {
	requests []time.Time
}

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	authService     *auth.Service
	db              *sqlx.DB
	logger          *zap.Logger
	rateLimiter     map[string]*rateLimitEntry // IP -> request timestamps (sliding window)
	rateLimiterLock sync.RWMutex
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *auth.Service, db *sqlx.DB, logger *zap.Logger) *AuthHandler {
	h := &AuthHandler{
		authService: authService,
		db:          db,
		logger:      logger,
		rateLimiter: make(map[string]*rateLimitEntry),
	}
	// Start background cleanup goroutine (runs once, not per-request)
	go h.cleanupRateLimiter()
	return h
}

// cleanupRateLimiter periodically removes stale rate limit entries
func (h *AuthHandler) cleanupRateLimiter() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		h.rateLimiterLock.Lock()
		now := time.Now()
		for ip, entry := range h.rateLimiter {
			// Remove entries with no recent requests
			if len(entry.requests) == 0 || now.Sub(entry.requests[len(entry.requests)-1]) > 5*time.Minute {
				delete(h.rateLimiter, ip)
			}
		}
		h.rateLimiterLock.Unlock()
	}
}

// RegisterRequest represents the email/password registration request
type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	FullName string `json:"full_name,omitempty"`
}

// LoginRequest represents the login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterResponse represents the successful registration/login response
type RegisterResponse struct {
	UserID       string                 `json:"user_id"`
	TenantID     string                 `json:"tenant_id"`
	AccessToken  string                 `json:"access_token"`
	RefreshToken string                 `json:"refresh_token"`
	ExpiresIn    int                    `json:"expires_in"`
	APIKey       string                 `json:"api_key"` // Only for NEW registrations
	Tier         string                 `json:"tier"`
	IsNewUser    bool                   `json:"is_new_user"`
	Quotas       map[string]interface{} `json:"quotas"`
	User         UserInfo               `json:"user"`
}

// UserInfo represents user profile information
type UserInfo struct {
	Email    string  `json:"email"`
	Username string  `json:"username"`
	Name     *string `json:"name,omitempty"`
	Picture  string  `json:"picture,omitempty"`
}

// MeResponse represents the /auth/me response
type MeResponse struct {
	UserID     string                 `json:"user_id"`
	TenantID   string                 `json:"tenant_id"`
	Email      string                 `json:"email"`
	Username   string                 `json:"username"`
	Name       *string                `json:"name,omitempty"`
	Picture    string                 `json:"picture,omitempty"`
	Tier       string                 `json:"tier"`
	Quotas     map[string]interface{} `json:"quotas"`
	RateLimits map[string]interface{} `json:"rate_limits"`
}

// RefreshRequest represents the token refresh request
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshResponse represents the token refresh response
type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// RefreshKeyResponse represents the API key refresh response
type RefreshKeyResponse struct {
	APIKey             string `json:"api_key"`
	PreviousKeyRevoked bool   `json:"previous_key_revoked"`
}

// APIKeyInfo represents an API key's metadata (not the full key for security)
type APIKeyInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	KeyPrefix   string  `json:"key_prefix"` // First 8 chars for identification
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
	LastUsedAt  *string `json:"last_used_at,omitempty"`
	IsActive    bool    `json:"is_active"`
}

// ListAPIKeysResponse represents the list of API keys
type ListAPIKeysResponse struct {
	Keys  []APIKeyInfo `json:"keys"`
	Total int          `json:"total"`
}

// CreateAPIKeyRequest represents the request to create a new API key
type CreateAPIKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// CreateAPIKeyResponse represents the response after creating an API key
type CreateAPIKeyResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	APIKey    string `json:"api_key"` // Full key - only returned once!
	KeyPrefix string `json:"key_prefix"`
	CreatedAt string `json:"created_at"`
	Warning   string `json:"warning"` // Remind user to save the key
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Register handles POST /api/v1/auth/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Rate limiting: 30 requests per minute per IP
	if !h.checkRateLimit(getClientIP(r), 30) {
		h.sendError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many registration attempts. Please try again later.")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate input
	if req.Email == "" || req.Username == "" || req.Password == "" {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Email, username, and password are required")
		return
	}
	if len(req.Password) < 8 {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Password must be at least 8 characters")
		return
	}

	// Register user
	user, err := h.authService.Register(ctx, &auth.RegisterRequest{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
		FullName: req.FullName,
	})
	if err != nil {
		h.logger.Error("Failed to register user",
			zap.Error(err),
			zap.String("email", req.Email))
		if strings.Contains(err.Error(), "already registered") || strings.Contains(err.Error(), "already exists") {
			h.sendError(w, http.StatusConflict, "email_exists", "Email already registered")
		} else {
			h.sendError(w, http.StatusInternalServerError, "registration_failed", "Failed to complete registration")
		}
		return
	}

	// Generate JWT tokens
	tokens, err := h.authService.Login(ctx, &auth.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		h.logger.Error("Failed to generate tokens after registration", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "token_generation_failed", "User created but failed to generate tokens")
		return
	}

	// Create default API key for new users
	apiKeyStr, _, err := h.authService.CreateAPIKey(ctx, user.ID, &auth.CreateAPIKeyRequest{
		Name:        "Default API Key",
		Description: "Auto-generated on registration",
	})
	if err != nil {
		h.logger.Warn("Failed to create default API key", zap.Error(err), zap.String("user_id", user.ID.String()))
		// Non-fatal - continue without API key
		apiKeyStr = ""
	}

	// Get tenant info
	var tenant auth.Tenant
	err = h.db.GetContext(ctx, &tenant,
		"SELECT * FROM auth.tenants WHERE id = $1", user.TenantID)
	if err != nil {
		h.logger.Error("Failed to get tenant", zap.Error(err), zap.String("tenant_id", user.TenantID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to retrieve account information")
		return
	}

	// Build response
	fullName := user.FullName
	resp := RegisterResponse{
		UserID:       user.ID.String(),
		TenantID:     user.TenantID.String(),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		APIKey:       apiKeyStr,
		Tier:         tenant.Plan,
		IsNewUser:    true,
		Quotas: map[string]interface{}{
			"monthly_tokens":    tenant.TokenLimit,
			"rate_limit_minute": 60,
			"rate_limit_hour":   tenant.RateLimitPerHour,
		},
		User: UserInfo{
			Email:    user.Email,
			Username: user.Username,
			Name:     &fullName,
			Picture:  getPicture(user.Metadata),
		},
	}

	h.logger.Info("User registration successful",
		zap.String("user_id", user.ID.String()),
		zap.String("email", user.Email))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Rate limiting: 30 requests per minute per IP
	if !h.checkRateLimit(getClientIP(r), 30) {
		h.sendError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many login attempts. Please try again later.")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Email and password are required")
		return
	}

	// Authenticate user
	tokens, err := h.authService.Login(ctx, &auth.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		h.logger.Warn("Login failed", zap.Error(err), zap.String("email", req.Email))
		h.sendError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	// Get user info
	var user auth.User
	err = h.db.GetContext(ctx, &user,
		"SELECT * FROM auth.users WHERE email = $1", req.Email)
	if err != nil {
		h.logger.Error("Failed to get user after login", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Login succeeded but failed to retrieve user information")
		return
	}

	// Get tenant info
	var tenant auth.Tenant
	err = h.db.GetContext(ctx, &tenant,
		"SELECT * FROM auth.tenants WHERE id = $1", user.TenantID)
	if err != nil {
		h.logger.Error("Failed to get tenant", zap.Error(err), zap.String("tenant_id", user.TenantID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to retrieve account information")
		return
	}

	// Build response
	fullName := user.FullName
	resp := RegisterResponse{
		UserID:       user.ID.String(),
		TenantID:     user.TenantID.String(),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		APIKey:       "", // Not returned on login
		Tier:         tenant.Plan,
		IsNewUser:    false,
		Quotas: map[string]interface{}{
			"monthly_tokens":    tenant.TokenLimit,
			"rate_limit_minute": 60,
			"rate_limit_hour":   tenant.RateLimitPerHour,
		},
		User: UserInfo{
			Email:    user.Email,
			Username: user.Username,
			Name:     &fullName,
			Picture:  getPicture(user.Metadata),
		},
	}

	h.logger.Info("Login successful", zap.String("user_id", user.ID.String()), zap.String("email", user.Email))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Refresh handles POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Rate limiting: 60 requests per minute per IP (token refresh can be frequent)
	if !h.checkRateLimit(getClientIP(r), 60) {
		h.sendError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many refresh attempts. Please try again later.")
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	if req.RefreshToken == "" {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Missing refresh_token")
		return
	}

	tokens, err := h.authService.Refresh(ctx, req.RefreshToken)
	if err != nil {
		h.logger.Warn("Token refresh failed", zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "invalid_refresh_token", "Invalid refresh token")
		return
	}

	resp := RefreshResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Get full user info
	var user auth.User
	err := h.db.GetContext(ctx, &user,
		"SELECT * FROM auth.users WHERE id = $1", userCtx.UserID)
	if err != nil {
		h.logger.Error("Failed to get user", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to retrieve user information")
		return
	}

	// Get tenant info
	var tenant auth.Tenant
	err = h.db.GetContext(ctx, &tenant,
		"SELECT * FROM auth.tenants WHERE id = $1", user.TenantID)
	if err != nil {
		h.logger.Error("Failed to get tenant", zap.Error(err), zap.String("tenant_id", user.TenantID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to retrieve account information")
		return
	}

	// Build response
	fullName := user.FullName
	resp := MeResponse{
		UserID:   user.ID.String(),
		TenantID: user.TenantID.String(),
		Email:    user.Email,
		Username: user.Username,
		Name:     &fullName,
		Picture:  getPicture(user.Metadata),
		Tier:     tenant.Plan,
		Quotas: map[string]interface{}{
			"monthly_tokens":    tenant.TokenLimit,
			"monthly_usage":     tenant.MonthlyTokenUsage,
			"rate_limit_minute": 60,
			"rate_limit_hour":   tenant.RateLimitPerHour,
		},
		RateLimits: map[string]interface{}{
			"minute": map[string]interface{}{
				"limit":     60,
				"remaining": 60, // TODO: Get actual values from Redis
			},
			"hour": map[string]interface{}{
				"limit":     tenant.RateLimitPerHour,
				"remaining": tenant.RateLimitPerHour,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RefreshKey handles POST /api/v1/auth/refresh-key
func (h *AuthHandler) RefreshKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !userCtx.IsAPIKey || userCtx.APIKeyID == uuid.Nil {
		h.sendError(w, http.StatusBadRequest, "api_key_required", "API key authentication required")
		return
	}

	// Revoke old API key
	previousKeyRevoked := false
	result, err := h.db.ExecContext(ctx,
		"UPDATE auth.api_keys SET is_active = false WHERE id = $1 AND is_active = true",
		userCtx.APIKeyID)
	if err != nil {
		h.logger.Error("Failed to revoke old API key", zap.Error(err))
	} else {
		if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
			previousKeyRevoked = true
		}
	}

	// Create new API key
	newAPIKey, _, err := h.authService.CreateAPIKey(ctx, userCtx.UserID, &auth.CreateAPIKeyRequest{
		Name:        "Refreshed API Key",
		Description: fmt.Sprintf("Regenerated on %s", time.Now().Format("2006-01-02 15:04:05")),
	})
	if err != nil {
		h.logger.Error("Failed to create new API key", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "key_generation_failed", "Failed to generate new API key")
		return
	}

	h.logger.Info("API key refreshed",
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("old_key_id", userCtx.APIKeyID.String()))

	resp := RefreshKeyResponse{
		APIKey:             newAPIKey,
		PreviousKeyRevoked: previousKeyRevoked,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ListAPIKeys handles GET /api/v1/auth/api-keys
func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	// Query API keys for this user (only metadata, not the hashed key)
	type apiKeyRow struct {
		ID          uuid.UUID  `db:"id"`
		Name        string     `db:"name"`
		KeyPrefix   string     `db:"key_prefix"`
		Description *string    `db:"description"`
		CreatedAt   time.Time  `db:"created_at"`
		LastUsed    *time.Time `db:"last_used"`
		IsActive    bool       `db:"is_active"`
	}

	var keys []apiKeyRow
	err := h.db.SelectContext(ctx, &keys, `
		SELECT id, name, key_prefix, description, created_at, last_used, is_active
		FROM auth.api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userCtx.UserID)
	if err != nil {
		h.logger.Error("Failed to list API keys", zap.Error(err), zap.String("user_id", userCtx.UserID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to retrieve API keys")
		return
	}

	// Convert to response format
	keyInfos := make([]APIKeyInfo, len(keys))
	for i, k := range keys {
		keyInfos[i] = APIKeyInfo{
			ID:          k.ID.String(),
			Name:        k.Name,
			KeyPrefix:   k.KeyPrefix,
			Description: k.Description,
			CreatedAt:   k.CreatedAt.Format(time.RFC3339),
			IsActive:    k.IsActive,
		}
		if k.LastUsed != nil {
			lastUsed := k.LastUsed.Format(time.RFC3339)
			keyInfos[i].LastUsedAt = &lastUsed
		}
	}

	resp := ListAPIKeysResponse{
		Keys:  keyInfos,
		Total: len(keyInfos),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// CreateKey handles POST /api/v1/auth/api-keys
func (h *AuthHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate name
	if req.Name == "" {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Name is required")
		return
	}
	if len(req.Name) > 100 {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Name must be 100 characters or less")
		return
	}

	// Create the API key
	var desc string
	if req.Description != nil {
		desc = *req.Description
	}
	apiKey, keyRecord, err := h.authService.CreateAPIKey(ctx, userCtx.UserID, &auth.CreateAPIKeyRequest{
		Name:        req.Name,
		Description: desc,
	})
	if err != nil {
		h.logger.Error("Failed to create API key", zap.Error(err), zap.String("user_id", userCtx.UserID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key")
		return
	}

	h.logger.Info("API key created",
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("key_id", keyRecord.ID.String()),
		zap.String("name", req.Name))

	resp := CreateAPIKeyResponse{
		ID:        keyRecord.ID.String(),
		Name:      req.Name,
		APIKey:    apiKey,
		KeyPrefix: keyRecord.KeyPrefix,
		CreatedAt: keyRecord.CreatedAt.Format(time.RFC3339),
		Warning:   "Store this API key securely. It will not be shown again.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// RevokeKey handles DELETE /api/v1/auth/api-keys/{id}
func (h *AuthHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	keyIDStr := r.PathValue("id")
	if keyIDStr == "" {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Missing API key ID")
		return
	}

	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid API key ID format")
		return
	}

	// Verify the key belongs to this user and deactivate it
	result, err := h.db.ExecContext(ctx, `
		UPDATE auth.api_keys
		SET is_active = false
		WHERE id = $1 AND user_id = $2 AND is_active = true
	`, keyID, userCtx.UserID)
	if err != nil {
		h.logger.Error("Failed to revoke API key", zap.Error(err), zap.String("key_id", keyID.String()))
		h.sendError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke API key")
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		h.sendError(w, http.StatusNotFound, "not_found", "API key not found or already revoked")
		return
	}

	h.logger.Info("API key revoked",
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("key_id", keyID.String()))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "API key revoked successfully",
	})
}

// Helper functions

func (h *AuthHandler) sendError(w http.ResponseWriter, statusCode int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// getClientIP extracts the real client IP from request headers.
// Behind AWS ALB, X-Forwarded-For format is: "spoofable, ..., client_ip_from_alb"
// ALB appends the real client IP, so the rightmost IP is trustworthy.
// This prevents spoofing via client-injected X-Forwarded-For headers.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by ALB/proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the rightmost IP (added by ALB, not spoofable)
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}

	// Fallback to RemoteAddr (strip port if present)
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func (h *AuthHandler) checkRateLimit(ip string, maxRequests int) bool {
	// Sliding window rate limiter (production should use Redis)
	h.rateLimiterLock.Lock()
	defer h.rateLimiterLock.Unlock()

	now := time.Now()
	windowStart := now.Add(-60 * time.Second)

	entry, exists := h.rateLimiter[ip]
	if !exists {
		entry = &rateLimitEntry{requests: make([]time.Time, 0, maxRequests)}
		h.rateLimiter[ip] = entry
	}

	// Remove requests outside the window
	validRequests := make([]time.Time, 0, len(entry.requests))
	for _, t := range entry.requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}
	entry.requests = validRequests

	// Check if limit exceeded
	if len(entry.requests) >= maxRequests {
		return false
	}

	// Add current request
	entry.requests = append(entry.requests, now)
	return true
}

func getPicture(metadata auth.JSONMap) string {
	if pic, ok := metadata["picture"].(string); ok {
		return pic
	}
	return ""
}
