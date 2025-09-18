package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// Service handles authentication operations
type Service struct {
	db         *sqlx.DB
	logger     *zap.Logger
	jwtManager *JWTManager
}

// NewService creates a new authentication service
func NewService(db *sqlx.DB, logger *zap.Logger, jwtSecret string) *Service {
	return &Service{
		db:     db,
		logger: logger,
		jwtManager: NewJWTManager(
			jwtSecret,
			30*time.Minute, // Access token expiry
			7*24*time.Hour, // Refresh token expiry
		),
	}
}

// Register creates a new user account
func (s *Service) Register(ctx context.Context, req *RegisterRequest) (*User, error) {
	// Check if email already exists
	var exists bool
	err := s.db.GetContext(ctx, &exists,
		"SELECT EXISTS(SELECT 1 FROM auth.users WHERE email = $1)", req.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to check email existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("email already registered")
	}

	// Check if username already exists
	err = s.db.GetContext(ctx, &exists,
		"SELECT EXISTS(SELECT 1 FROM auth.users WHERE username = $1)", req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to check username existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("username already taken")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Determine tenant ID
	var tenantID uuid.UUID
	if req.TenantID != "" {
		tenantID, err = uuid.Parse(req.TenantID)
		if err != nil {
			return nil, fmt.Errorf("invalid tenant ID: %w", err)
		}
	} else {
		// Create new tenant for the user
		tenantID, err = s.createTenant(ctx, req.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to create tenant: %w", err)
		}
	}

	// Create user
	user := &User{
		ID:           uuid.New(),
		Email:        req.Email,
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		FullName:     req.FullName,
		TenantID:     tenantID,
		Role:         RoleUser,
		IsActive:     true,
		IsVerified:   false,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	query := `
		INSERT INTO auth.users (id, email, username, password_hash, full_name, tenant_id, role, is_active, is_verified)
		VALUES (:id, :email, :username, :password_hash, :full_name, :tenant_id, :role, :is_active, :is_verified)
	`

	_, err = s.db.NamedExecContext(ctx, query, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Log audit event
	s.logAuditEvent(ctx, AuditEventAccountCreated, user.ID, tenantID, nil)

	s.logger.Info("User registered successfully",
		zap.String("user_id", user.ID.String()),
		zap.String("email", user.Email),
		zap.String("tenant_id", tenantID.String()))

	return user, nil
}

// Login authenticates a user and returns tokens
func (s *Service) Login(ctx context.Context, req *LoginRequest) (*TokenPair, error) {
	// Find user by email
	var user User
	query := `SELECT * FROM auth.users WHERE email = $1 AND is_active = true`
	err := s.db.GetContext(ctx, &user, query, req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			// Log failed attempt
			s.logAuditEvent(ctx, AuditEventLoginFailed, uuid.Nil, uuid.Nil,
				map[string]interface{}{"email": req.Email})
			return nil, fmt.Errorf("invalid email or password")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		// Log failed attempt
		s.logAuditEvent(ctx, AuditEventLoginFailed, user.ID, user.TenantID, nil)
		return nil, fmt.Errorf("invalid email or password")
	}

	// Generate token pair
	tokens, refreshTokenHash, err := s.jwtManager.GenerateTokenPair(&user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	// Store refresh token
	err = s.storeRefreshToken(ctx, &user, refreshTokenHash)
	if err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Update last login
	_, err = s.db.ExecContext(ctx,
		"UPDATE auth.users SET last_login = NOW() WHERE id = $1", user.ID)
	if err != nil {
		s.logger.Warn("Failed to update last login", zap.Error(err))
	}

	// Log audit event
	s.logAuditEvent(ctx, AuditEventLogin, user.ID, user.TenantID, nil)

	s.logger.Info("User logged in successfully",
		zap.String("user_id", user.ID.String()),
		zap.String("email", user.Email))

	return tokens, nil
}

// ValidateAPIKey validates an API key and returns user context
func (s *Service) ValidateAPIKey(ctx context.Context, apiKey string) (*UserContext, error) {
	// Extract key prefix (first 8 chars)
	if len(apiKey) < 8 {
		return nil, fmt.Errorf("invalid API key format")
	}
	keyPrefix := apiKey[:8]
	keyHash := hashToken(apiKey)

	// Find API key by prefix first
	var keys []APIKey
	query := `
		SELECT * FROM auth.api_keys 
		WHERE key_prefix = $1 AND is_active = true
	`
	err := s.db.SelectContext(ctx, &keys, query, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to query API keys: %w", err)
	}

	// Find matching key with constant-time comparison
	var key *APIKey
	for _, k := range keys {
		if compareTokenHash(k.KeyHash, keyHash) {
			key = &k
			break
		}
	}

	if key == nil {
		return nil, fmt.Errorf("invalid API key")
	}

	// Check expiration
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("API key expired")
	}

	// Update last used
	go func() {
		_, err := s.db.Exec(
			"UPDATE auth.api_keys SET last_used = NOW() WHERE id = $1", key.ID)
		if err != nil {
			s.logger.Warn("Failed to update API key last used", zap.Error(err))
		}
	}()

	// Get user details
	var user User
	err = s.db.GetContext(ctx, &user,
		"SELECT * FROM auth.users WHERE id = $1", key.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user for API key: %w", err)
	}

	// Log audit event
	s.logAuditEvent(ctx, AuditEventAPIKeyUsed, user.ID, user.TenantID,
		map[string]interface{}{"api_key_id": key.ID.String()})

	return &UserContext{
		UserID:    user.ID,
		TenantID:  user.TenantID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		Scopes:    key.Scopes,
		IsAPIKey:  true,
		TokenType: "api_key",
	}, nil
}

// CreateAPIKey creates a new API key for a user
func (s *Service) CreateAPIKey(ctx context.Context, userID uuid.UUID, req *CreateAPIKeyRequest) (string, *APIKey, error) {
	// Get user to verify they exist and get tenant ID
	var user User
	err := s.db.GetContext(ctx, &user,
		"SELECT * FROM auth.users WHERE id = $1", userID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Generate API key
	apiKey, keyHash, keyPrefix, err := generateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Set default scopes if not provided
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{
			ScopeWorkflowsRead, ScopeWorkflowsWrite,
			ScopeAgentsExecute,
			ScopeSessionsRead, ScopeSessionsWrite,
		}
	}

	// Create API key record
	key := &APIKey{
		ID:               uuid.New(),
		KeyHash:          keyHash,
		KeyPrefix:        keyPrefix,
		UserID:           userID,
		TenantID:         user.TenantID,
		Name:             req.Name,
		Description:      req.Description,
		Scopes:           scopes,
		RateLimitPerHour: 1000,
		ExpiresAt:        req.ExpiresAt,
		IsActive:         true,
		CreatedAt:        time.Now(),
	}

	query := `
		INSERT INTO auth.api_keys 
		(id, key_hash, key_prefix, user_id, tenant_id, name, description, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = s.db.ExecContext(ctx, query,
		key.ID, key.KeyHash, key.KeyPrefix, key.UserID, key.TenantID,
		key.Name, key.Description, key.Scopes, key.ExpiresAt)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create API key: %w", err)
	}

	// Log audit event
	s.logAuditEvent(ctx, AuditEventAPIKeyCreated, userID, user.TenantID,
		map[string]interface{}{"api_key_id": key.ID.String(), "name": key.Name})

	s.logger.Info("API key created successfully",
		zap.String("key_id", key.ID.String()),
		zap.String("user_id", userID.String()),
		zap.String("name", key.Name))

	// Return the actual API key (only shown once)
	return apiKey, key, nil
}

// Helper functions

func (s *Service) createTenant(ctx context.Context, username string) (uuid.UUID, error) {
	tenant := &Tenant{
		ID:               uuid.New(),
		Name:             fmt.Sprintf("%s's Workspace", username),
		Slug:             fmt.Sprintf("%s-%s", username, generateRandomString(6)),
		Plan:             PlanFree,
		TokenLimit:       10000,
		RateLimitPerHour: 100,
		IsActive:         true,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	query := `
		INSERT INTO auth.tenants (id, name, slug, plan, token_limit, rate_limit_per_hour)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.db.ExecContext(ctx, query,
		tenant.ID, tenant.Name, tenant.Slug, tenant.Plan, tenant.TokenLimit, tenant.RateLimitPerHour)
	if err != nil {
		return uuid.Nil, err
	}

	return tenant.ID, nil
}

func (s *Service) storeRefreshToken(ctx context.Context, user *User, tokenHash string) error {
	query := `
		INSERT INTO auth.refresh_tokens (user_id, tenant_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	_, err := s.db.ExecContext(ctx, query, user.ID, user.TenantID, tokenHash, expiresAt)
	return err
}

func (s *Service) logAuditEvent(ctx context.Context, eventType string, userID, tenantID uuid.UUID, details map[string]interface{}) {
	query := `
		INSERT INTO auth.audit_logs (event_type, user_id, tenant_id, details)
		VALUES ($1, $2, $3, $4)
	`

	// Convert nil UUIDs to NULL
	var userIDPtr, tenantIDPtr *uuid.UUID
	if userID != uuid.Nil {
		userIDPtr = &userID
	}
	if tenantID != uuid.Nil {
		tenantIDPtr = &tenantID
	}

	_, err := s.db.ExecContext(ctx, query, eventType, userIDPtr, tenantIDPtr, details)
	if err != nil {
		s.logger.Warn("Failed to log audit event",
			zap.String("event_type", eventType),
			zap.Error(err))
	}
}

func generateAPIKey() (key, hash, prefix string, err error) {
	// Generate 32 random bytes
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	key = "sk_" + hex.EncodeToString(b)
	hash = hashToken(key)
	prefix = key[:8]
	return key, hash, prefix, nil
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}
