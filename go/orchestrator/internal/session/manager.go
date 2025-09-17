package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"go.uber.org/zap"

	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
)

// Manager handles session management with Redis backend
type Manager struct {
	client      *circuitbreaker.RedisWrapper
	logger      *zap.Logger
	ttl         time.Duration
	mu          sync.RWMutex
	localCache  map[string]*Session  // Local cache for performance
	cacheAccess map[string]time.Time // Track last access time for LRU
	maxSessions int
}

// NewManager creates a new session manager
func NewManager(redisAddr string, logger *zap.Logger) (*Manager, error) {
	// Get Redis password from environment variable
	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword, // Use environment variable
		DB:           0,             // Default DB
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// Create circuit breaker wrapped client
	client := circuitbreaker.NewRedisWrapper(redisClient, logger)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Manager{
		client:      client,
		logger:      logger,
		ttl:         24 * time.Hour, // Default session TTL
		localCache:  make(map[string]*Session),
		cacheAccess: make(map[string]time.Time),
		maxSessions: 10000, // Max sessions to keep in local cache
	}, nil
}

// CreateSession creates a new session
func (m *Manager) CreateSession(ctx context.Context, userID string, tenantID string, metadata map[string]interface{}) (*Session, error) {
	sessionID := uuid.New().String()

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(m.ttl),
		Metadata:  metadata,
		Context:   make(map[string]interface{}),
		History:   make([]Message, 0),
	}

	// Store in Redis
	if err := m.saveSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Cache locally
	m.mu.Lock()
	m.localCache[sessionID] = session
	m.cleanupLocalCache()
	metrics.SessionCacheSize.Set(float64(len(m.localCache)))
	m.mu.Unlock()

	m.logger.Info("Created new session",
		zap.String("session_id", sessionID),
		zap.String("user_id", userID),
	)
	metrics.SessionsCreated.Inc()

	return session, nil
}

// CreateSessionWithID creates a new session with a specific ID
func (m *Manager) CreateSessionWithID(ctx context.Context, sessionID string, userID string, tenantID string, metadata map[string]interface{}) (*Session, error) {
	// IMPORTANT: Check if session already exists to prevent hijacking
	existing, _ := m.GetSession(ctx, sessionID)
	if existing != nil {
		if existing.UserID != userID {
			// Session exists but belongs to different user - security violation
			// Generate a new session ID instead
			m.logger.Warn("Attempted to reuse session ID from different user, generating new ID",
				zap.String("requested_session_id", sessionID),
				zap.String("requesting_user", userID),
				zap.String("existing_owner", existing.UserID),
			)
			// Create new session with generated ID
			return m.CreateSession(ctx, userID, tenantID, metadata)
		}
		// Session exists and belongs to same user - return existing
		return existing, nil
	}

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(m.ttl),
		Metadata:  metadata,
		Context:   make(map[string]interface{}),
		History:   make([]Message, 0),
	}

	// Store in Redis
	if err := m.saveSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Cache locally
	m.mu.Lock()
	m.localCache[sessionID] = session
	m.cleanupLocalCache()
	metrics.SessionCacheSize.Set(float64(len(m.localCache)))
	m.mu.Unlock()

	m.logger.Info("Created new session with specific ID",
		zap.String("session_id", sessionID),
		zap.String("user_id", userID),
	)
	metrics.SessionsCreated.Inc()
	return session, nil
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	// Check local cache first
	m.mu.RLock()
	if session, ok := m.localCache[sessionID]; ok {
		m.mu.RUnlock()
		metrics.SessionCacheHits.Inc()
		if session.IsExpired() {
			m.DeleteSession(ctx, sessionID)
			return nil, ErrSessionExpired
		}
		// Update access time for LRU
		m.mu.Lock()
		m.cacheAccess[sessionID] = time.Now()
		m.mu.Unlock()
		return session, nil
	}
	m.mu.RUnlock()
	metrics.SessionCacheMisses.Inc()

	// Load from Redis
	key := m.sessionKey(sessionID)
	data, err := m.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrSessionNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Check expiration
	if session.IsExpired() {
		m.DeleteSession(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	// Enforce tenant isolation if auth context is present
	if userCtx, err := authFromContext(ctx); err == nil && userCtx.TenantID != "" {
		if session.TenantID != "" && session.TenantID != userCtx.TenantID {
			// Do not leak existence
			return nil, ErrSessionNotFound
		}
	}

	// Update local cache and track access time
	m.mu.Lock()
	m.localCache[sessionID] = &session
	m.cacheAccess[sessionID] = time.Now()
	m.cleanupLocalCache()
	metrics.SessionCacheSize.Set(float64(len(m.localCache)))
	m.mu.Unlock()

	return &session, nil
}

// UpdateSession updates an existing session
func (m *Manager) UpdateSession(ctx context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}

	session.UpdatedAt = time.Now()

	// Save to Redis
	if err := m.saveSession(ctx, session); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Update local cache
	m.mu.Lock()
	m.localCache[session.ID] = session
	m.mu.Unlock()

	return nil
}

// DeleteSession deletes a session
func (m *Manager) DeleteSession(ctx context.Context, sessionID string) error {
	// Remove from Redis
	key := m.sessionKey(sessionID)
	if err := m.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	// Remove from local cache
	m.mu.Lock()
	delete(m.localCache, sessionID)
	// Update cache size metric while holding the lock to avoid races
	metrics.SessionCacheSize.Set(float64(len(m.localCache)))
	m.mu.Unlock()

	m.logger.Info("Deleted session", zap.String("session_id", sessionID))
	return nil
}

// ExtendSession extends the TTL of a session
func (m *Manager) ExtendSession(ctx context.Context, sessionID string, duration time.Duration) error {
	session, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	session.ExpiresAt = time.Now().Add(duration)
	return m.UpdateSession(ctx, session)
}

// AddMessage adds a message to session history
func (m *Manager) AddMessage(ctx context.Context, sessionID string, msg Message) error {
	session, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	session.History = append(session.History, msg)

	// Limit history size
	maxHistory := 100
	if len(session.History) > maxHistory {
		session.History = session.History[len(session.History)-maxHistory:]
	}

	return m.UpdateSession(ctx, session)
}

// UpdateContext updates session context
func (m *Manager) UpdateContext(ctx context.Context, sessionID string, key string, value interface{}) error {
	session, err := m.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Context == nil {
		session.Context = make(map[string]interface{})
	}
	session.Context[key] = value

	return m.UpdateSession(ctx, session)
}

// GetUserSessions gets all sessions for a user
func (m *Manager) GetUserSessions(ctx context.Context, userID string) ([]*Session, error) {
	pattern := fmt.Sprintf("session:*")
	keys, err := m.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var sessions []*Session
	for _, key := range keys {
		data, err := m.client.Get(ctx, key).Bytes()
		if err != nil {
			continue // Skip failed sessions
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		// Enforce tenant isolation if available
		if userCtx, err := authFromContext(ctx); err == nil && userCtx.TenantID != "" {
			if session.TenantID != "" && session.TenantID != userCtx.TenantID {
				continue
			}
		}

		if session.UserID == userID && !session.IsExpired() {
			sessions = append(sessions, &session)
		}
	}

	return sessions, nil
}

// CleanupExpired removes expired sessions
func (m *Manager) CleanupExpired(ctx context.Context) (int, error) {
	pattern := fmt.Sprintf("session:*")
	keys, err := m.client.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to list sessions: %w", err)
	}

	cleaned := 0
	for _, key := range keys {
		data, err := m.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		if session.IsExpired() {
			if err := m.client.Del(ctx, key).Err(); err == nil {
				cleaned++
			}
		}
	}

	m.logger.Info("Cleaned up expired sessions", zap.Int("count", cleaned))
	return cleaned, nil
}

// Private methods

func (m *Manager) sessionKey(sessionID string) string {
	return fmt.Sprintf("session:%s", sessionID)
}

func (m *Manager) saveSession(ctx context.Context, session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	key := m.sessionKey(session.ID)
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		ttl = m.ttl
	}

	return m.client.Set(ctx, key, data, ttl).Err()
}

func (m *Manager) cleanupLocalCache() {
	// Remove oldest entries if cache is too large using LRU
	if len(m.localCache) > m.maxSessions {
		// Find the oldest accessed sessions
		type accessEntry struct {
			id   string
			time time.Time
		}

		entries := make([]accessEntry, 0, len(m.localCache))
		for id := range m.localCache {
			accessTime, exists := m.cacheAccess[id]
			if !exists {
				// If no access time tracked, consider it very old
				accessTime = time.Time{}
			}
			entries = append(entries, accessEntry{id: id, time: accessTime})
		}

		// Sort by access time (oldest first)
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].time.Before(entries[i].time) {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}

		// Remove the oldest half
		toRemove := m.maxSessions / 2
		for i := 0; i < toRemove && i < len(entries); i++ {
			delete(m.localCache, entries[i].id)
			delete(m.cacheAccess, entries[i].id)
			metrics.SessionCacheEvictions.Inc()
		}
	}
}

// Close closes the session manager
func (m *Manager) Close() error {
	return m.client.Close()
}

// RedisWrapper returns the underlying Redis circuit breaker wrapper for health checks and monitoring
func (m *Manager) RedisWrapper() *circuitbreaker.RedisWrapper {
	return m.client
}

// authFromContext extracts a minimal subset of auth context without hard coupling
// to the entire auth package surface in this file.
type minimalUserCtx struct {
	TenantID string
}

func authFromContext(ctx context.Context) (*minimalUserCtx, error) {
	if uc, err := auth.GetUserContext(ctx); err == nil && uc != nil {
		return &minimalUserCtx{TenantID: uc.TenantID.String()}, nil
	}
	return nil, fmt.Errorf("no auth context")
}
