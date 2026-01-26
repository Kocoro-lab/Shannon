package auth

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// JSONMap handles JSON database columns
type JSONMap map[string]interface{}

// Scan implements sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(map[string]interface{})
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// Value implements driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	return json.Marshal(j)
}

// User represents an authenticated user
type User struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	Email           string     `json:"email" db:"email"`
	Username        string     `json:"username" db:"username"`
	PasswordHash    string     `json:"-" db:"password_hash"`
	FullName        string     `json:"full_name" db:"full_name"`
	TenantID        uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Role            string     `json:"role" db:"role"` // user, admin, owner
	IsActive        bool       `json:"is_active" db:"is_active"`
	IsVerified      bool       `json:"is_verified" db:"is_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty" db:"email_verified_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	LastLogin       *time.Time `json:"last_login,omitempty" db:"last_login"`
	Metadata        JSONMap    `json:"metadata,omitempty" db:"metadata"`
}

// Tenant represents an organization/workspace
type Tenant struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	Name              string     `json:"name" db:"name"`
	Slug              string     `json:"slug" db:"slug"`
	Plan              string     `json:"plan" db:"plan"` // free, pro, enterprise
	TokenLimit        int        `json:"token_limit" db:"token_limit"`
	MonthlyTokenUsage int        `json:"monthly_token_usage" db:"monthly_token_usage"`
	DailyTokenLimit   *int       `json:"daily_token_limit,omitempty" db:"daily_token_limit"`
	DailyTokenUsage   int        `json:"daily_token_usage" db:"daily_token_usage"`
	RateLimitPerHour  int        `json:"rate_limit_per_hour" db:"rate_limit_per_hour"`
	OverageTokens     int64      `json:"overage_tokens" db:"overage_tokens"`
	UsageResetAt      *time.Time `json:"usage_reset_at,omitempty" db:"usage_reset_at"`
	IsActive          bool       `json:"is_active" db:"is_active"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
	Metadata          JSONMap    `json:"metadata,omitempty" db:"metadata"`
}

// APIKey represents an API key for programmatic access
type APIKey struct {
	ID               uuid.UUID      `json:"id" db:"id"`
	KeyHash          string         `json:"-" db:"key_hash"`
	KeyPrefix        string         `json:"key_prefix" db:"key_prefix"`
	UserID           uuid.UUID      `json:"user_id" db:"user_id"`
	TenantID         uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	Name             string         `json:"name" db:"name"`
	Description      string         `json:"description" db:"description"`
	Scopes           pq.StringArray `json:"scopes" db:"scopes"`
	RateLimitPerHour int            `json:"rate_limit_per_hour" db:"rate_limit_per_hour"`
	LastUsed         *time.Time     `json:"last_used,omitempty" db:"last_used"`
	ExpiresAt        *time.Time     `json:"expires_at,omitempty" db:"expires_at"`
	IsActive         bool           `json:"is_active" db:"is_active"`
	CreatedAt        time.Time      `json:"created_at" db:"created_at"`
}

// RefreshToken represents a JWT refresh token
type RefreshToken struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	TokenHash string     `json:"-" db:"token_hash"`
	UserID    uuid.UUID  `json:"user_id" db:"user_id"`
	TenantID  uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	Revoked   bool       `json:"revoked" db:"revoked"`
	RevokedAt *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	IPAddress string     `json:"ip_address" db:"ip_address"`
	UserAgent string     `json:"user_agent" db:"user_agent"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// UserContext represents the authenticated context for a request
type UserContext struct {
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Scopes    []string  `json:"scopes"`
	IsAPIKey  bool      `json:"is_api_key"`
	TokenType string    `json:"token_type"` // jwt or api_key

	// API key details for rate limiting
	APIKeyID uuid.UUID `json:"api_key_id,omitempty"` // ID of the API key (if IsAPIKey)
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // seconds
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Username string `json:"username" validate:"required,min=3,max=50"`
	Password string `json:"password" validate:"required,min=8"`
	FullName string `json:"full_name" validate:"required"`
	TenantID string `json:"tenant_id,omitempty"` // Optional, create new if not provided
}

// CreateAPIKeyRequest represents a request to create an API key
type CreateAPIKeyRequest struct {
	Name        string     `json:"name" validate:"required"`
	Description string     `json:"description"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// AuditEvent types
const (
	AuditEventLogin            = "login"
	AuditEventLogout           = "logout"
	AuditEventLoginFailed      = "login_failed"
	AuditEventTokenRefresh     = "token_refresh"
	AuditEventAPIKeyCreated    = "api_key_created"
	AuditEventAPIKeyDeleted    = "api_key_deleted"
	AuditEventAPIKeyUsed       = "api_key_used"
	AuditEventPermissionChange = "permission_changed"
	AuditEventPasswordChange   = "password_changed"
	AuditEventAccountCreated   = "account_created"
	AuditEventAccountDeleted   = "account_deleted"
)

// Scopes for authorization
const (
	ScopeWorkflowsRead    = "workflows:read"
	ScopeWorkflowsWrite   = "workflows:write"
	ScopeAgentsExecute    = "agents:execute"
	ScopeSessionsRead     = "sessions:read"
	ScopeSessionsWrite    = "sessions:write"
	ScopeAPIKeysManage    = "api_keys:manage"
	ScopeUsersManage      = "users:manage"
	ScopeTenantManage     = "tenant:manage"
	ScopeSkillsDangerous  = "skills:dangerous" // Required for invoking dangerous skills
)

// User roles
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
	RoleOwner = "owner"
)

// Tenant plans
const (
	PlanFree       = "free"
	PlanPro        = "pro"
	PlanEnterprise = "enterprise"
)
