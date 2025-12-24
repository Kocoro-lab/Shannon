package openai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// SessionPrefix is the Redis key prefix for OpenAI sessions
	SessionPrefix = "openai:session:"

	// SessionTTL is the default session time-to-live
	SessionTTL = 24 * time.Hour

	// HeaderSessionID is the header name for session ID
	HeaderSessionID = "X-Session-ID"
)

// SessionInfo contains metadata about a session
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	UserID       string    `json:"user_id"`
	TenantID     string    `json:"tenant_id"`
	ShannonSID   string    `json:"shannon_session_id"` // Mapped Shannon session
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	MessageCount int       `json:"message_count"`
}

// SessionManager handles OpenAI session management
type SessionManager struct {
	redis  *redis.Client
	logger *zap.Logger
	ttl    time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(redisClient *redis.Client, logger *zap.Logger) *SessionManager {
	return &SessionManager{
		redis:  redisClient,
		logger: logger,
		ttl:    SessionTTL,
	}
}

// SessionResult contains the resolved session and whether it was newly created
type SessionResult struct {
	SessionID      string // The OpenAI session ID to use
	ShannonSession string // The mapped Shannon session ID
	IsNew          bool   // True if a new session was created
	WasCollision   bool   // True if the provided session was rejected due to ownership
}

// ResolveSession resolves or creates a session for a request
// - If sessionID is empty, derives one from the request
// - If sessionID is provided but owned by different user, creates new and flags collision
// - Returns the session to use and whether headers should be echoed
func (sm *SessionManager) ResolveSession(
	ctx context.Context,
	providedSessionID string,
	userID, tenantID string,
	req *ChatCompletionRequest,
) (*SessionResult, error) {
	result := &SessionResult{}

	// Case 1: No session provided - derive from request
	if providedSessionID == "" {
		sessionID := sm.deriveSessionID(req, userID)
		shannonSID, isNew, err := sm.getOrCreateSession(ctx, sessionID, userID, tenantID)
		if err != nil {
			return nil, err
		}
		result.SessionID = sessionID
		result.ShannonSession = shannonSID
		result.IsNew = isNew
		return result, nil
	}

	// Case 2: Session provided - check ownership
	info, err := sm.getSessionInfo(ctx, providedSessionID)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to check session: %w", err)
	}

	// Session exists - verify ownership
	if info != nil {
		if info.UserID != userID || info.TenantID != tenantID {
			// Collision: session belongs to different user/tenant
			// Generate new session and flag collision
			sm.logger.Info("Session collision detected, generating new session",
				zap.String("provided_session", providedSessionID),
				zap.String("user_id", userID),
			)
			newSessionID := sm.generateSessionID()
			shannonSID, _, err := sm.getOrCreateSession(ctx, newSessionID, userID, tenantID)
			if err != nil {
				return nil, err
			}
			result.SessionID = newSessionID
			result.ShannonSession = shannonSID
			result.IsNew = true
			result.WasCollision = true
			return result, nil
		}

		// Session owned by this user - use it
		result.SessionID = providedSessionID
		result.ShannonSession = info.ShannonSID
		result.IsNew = false

		// Update last used
		go sm.touchSession(context.Background(), providedSessionID)
		return result, nil
	}

	// Session doesn't exist - create with provided ID
	shannonSID, _, err := sm.getOrCreateSession(ctx, providedSessionID, userID, tenantID)
	if err != nil {
		return nil, err
	}
	result.SessionID = providedSessionID
	result.ShannonSession = shannonSID
	result.IsNew = true
	return result, nil
}

// deriveSessionID creates a deterministic session ID from request content
func (sm *SessionManager) deriveSessionID(req *ChatCompletionRequest, userID string) string {
	var parts []string

	// Include user ID for isolation
	parts = append(parts, userID)

	// Include system message if present
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			parts = append(parts, msg.Content[:min(200, len(msg.Content))])
			break
		}
	}

	// Include first user message
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			parts = append(parts, msg.Content[:min(100, len(msg.Content))])
			break
		}
	}

	// Hash to create session ID
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "|")))
	hash := hex.EncodeToString(h.Sum(nil))[:16]
	return "oai-" + hash
}

// generateSessionID creates a new unique session ID
func (sm *SessionManager) generateSessionID() string {
	return "oai-" + uuid.New().String()[:8]
}

// getOrCreateSession gets an existing session or creates a new one
func (sm *SessionManager) getOrCreateSession(
	ctx context.Context,
	sessionID, userID, tenantID string,
) (shannonSID string, isNew bool, err error) {
	key := SessionPrefix + sessionID

	// Try to get existing
	info, err := sm.getSessionInfo(ctx, sessionID)
	if err != nil && err != redis.Nil {
		return "", false, err
	}

	if info != nil {
		return info.ShannonSID, false, nil
	}

	// Create new session - include full user ID for isolation even if orchestrator
	// doesn't strictly require it (defense in depth against session_id reuse after TTL)
	shannonSID = fmt.Sprintf("shannon-%s-%s", userID, sessionID)
	info = &SessionInfo{
		SessionID:    sessionID,
		UserID:       userID,
		TenantID:     tenantID,
		ShannonSID:   shannonSID,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
		MessageCount: 0,
	}

	// Store in Redis
	data := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%d",
		info.SessionID,
		info.UserID,
		info.TenantID,
		info.ShannonSID,
		info.CreatedAt.Unix(),
		info.LastUsedAt.Unix(),
		info.MessageCount,
	)

	if err := sm.redis.Set(ctx, key, data, sm.ttl).Err(); err != nil {
		return "", false, fmt.Errorf("failed to store session: %w", err)
	}

	sm.logger.Debug("Created new OpenAI session",
		zap.String("session_id", sessionID),
		zap.String("shannon_sid", shannonSID),
		zap.String("user_id", userID),
	)

	return shannonSID, true, nil
}

// getSessionInfo retrieves session info from Redis
func (sm *SessionManager) getSessionInfo(ctx context.Context, sessionID string) (*SessionInfo, error) {
	key := SessionPrefix + sessionID
	data, err := sm.redis.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	// Parse stored data
	parts := strings.Split(data, "|")
	if len(parts) < 7 {
		return nil, fmt.Errorf("invalid session data format")
	}

	var createdAt, lastUsedAt int64
	var messageCount int
	fmt.Sscanf(parts[4], "%d", &createdAt)
	fmt.Sscanf(parts[5], "%d", &lastUsedAt)
	fmt.Sscanf(parts[6], "%d", &messageCount)

	return &SessionInfo{
		SessionID:    parts[0],
		UserID:       parts[1],
		TenantID:     parts[2],
		ShannonSID:   parts[3],
		CreatedAt:    time.Unix(createdAt, 0),
		LastUsedAt:   time.Unix(lastUsedAt, 0),
		MessageCount: messageCount,
	}, nil
}

// touchSession updates the last used time and increments message count
func (sm *SessionManager) touchSession(ctx context.Context, sessionID string) {
	info, err := sm.getSessionInfo(ctx, sessionID)
	if err != nil {
		return
	}

	info.LastUsedAt = time.Now()
	info.MessageCount++

	key := SessionPrefix + sessionID
	data := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%d",
		info.SessionID,
		info.UserID,
		info.TenantID,
		info.ShannonSID,
		info.CreatedAt.Unix(),
		info.LastUsedAt.Unix(),
		info.MessageCount,
	)

	sm.redis.Set(ctx, key, data, sm.ttl)
}

// IncrementMessageCount increments the message count for a session
func (sm *SessionManager) IncrementMessageCount(ctx context.Context, sessionID string) {
	sm.touchSession(ctx, sessionID)
}
