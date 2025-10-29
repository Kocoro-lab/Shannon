package handlers

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"
    "unicode"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
    "github.com/google/uuid"
    "github.com/jmoiron/sqlx"
    "github.com/redis/go-redis/v9"
    "go.uber.org/zap"
)

const (
	// SessionContextKeyTitle is the key for session title in the context JSONB field
	SessionContextKeyTitle = "title"
)

// SessionHandler handles session-related HTTP requests
type SessionHandler struct {
	db     *sqlx.DB
	redis  *redis.Client
	logger *zap.Logger
}

// NewSessionHandler creates a new session handler
func NewSessionHandler(
	db *sqlx.DB,
	redis *redis.Client,
	logger *zap.Logger,
) *SessionHandler {
	return &SessionHandler{
		db:     db,
		redis:  redis,
		logger: logger,
	}
}

// SessionResponse represents a session metadata response
type SessionResponse struct {
	SessionID     string                 `json:"session_id"`
	UserID        string                 `json:"user_id"`
	Context       map[string]interface{} `json:"context,omitempty"`
	TokenBudget   int                    `json:"token_budget,omitempty"`
	TokensUsed    int                    `json:"tokens_used"`
	TaskCount     int                    `json:"task_count"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     *time.Time             `json:"updated_at,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
}

// SessionHistoryResponse represents session task history
type SessionHistoryResponse struct {
    SessionID string        `json:"session_id"`
    Tasks     []TaskHistory `json:"tasks"`
    Total     int           `json:"total"`
}

// ListSessionsResponse represents the list sessions response
type ListSessionsResponse struct {
	Sessions   []SessionSummary `json:"sessions"`
	TotalCount int              `json:"total_count"`
}

// SessionSummary represents a single session in listing
type SessionSummary struct {
	SessionID   string     `json:"session_id"`
	UserID      string     `json:"user_id"`
	Title       string     `json:"title,omitempty"`
	TaskCount   int        `json:"task_count"`
	TokensUsed  int        `json:"tokens_used"`
	TokenBudget int        `json:"token_budget,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// TaskHistory represents a task in session history
type TaskHistory struct {
	TaskID          string                 `json:"task_id"`
	WorkflowID      string                 `json:"workflow_id"`
	Query           string                 `json:"query"`
	Status          string                 `json:"status"`
	Mode            string                 `json:"mode,omitempty"`
	Result          string                 `json:"result,omitempty"`
	ErrorMessage    string                 `json:"error_message,omitempty"`
	TotalTokens     int                    `json:"total_tokens"`
	TotalCostUSD    float64                `json:"total_cost_usd"`
	DurationMs      int                    `json:"duration_ms,omitempty"`
	AgentsUsed      int                    `json:"agents_used"`
	ToolsInvoked    int                    `json:"tools_invoked"`
	StartedAt       time.Time              `json:"started_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// GetSession handles GET /api/v1/sessions/{sessionId}
func (h *SessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context from auth middleware
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from path
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		h.sendError(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Get session metadata from database
	var session struct {
		ID          string         `db:"id"`
		UserID      string         `db:"user_id"`
		Context     []byte         `db:"context"`
		TokenBudget sql.NullInt32  `db:"token_budget"`
		TokensUsed  sql.NullInt32  `db:"tokens_used"`
		CreatedAt   time.Time      `db:"created_at"`
		UpdatedAt   sql.NullTime   `db:"updated_at"`
		ExpiresAt   sql.NullTime   `db:"expires_at"`
	}

    err := h.db.GetContext(ctx, &session, `
            SELECT id, user_id, context, token_budget, tokens_used, created_at, updated_at, expires_at
            FROM sessions
            WHERE id::text = $1 AND deleted_at IS NULL
        `, sessionID)

	if err == sql.ErrNoRows {
		h.sendError(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("Failed to query session", zap.Error(err), zap.String("session_id", sessionID))
		h.sendError(w, "Failed to retrieve session", http.StatusInternalServerError)
		return
	}

	// Verify user has access to this session
	if session.UserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get task count for this session from task_executions
	var taskCount int
	err = h.db.GetContext(ctx, &taskCount, `
		SELECT COUNT(*) FROM task_executions WHERE session_id = $1
	`, sessionID)
	if err != nil {
		h.logger.Warn("Failed to get task count", zap.Error(err))
		taskCount = 0
	}

	// Parse context JSON
	var contextData map[string]interface{}
	if len(session.Context) > 0 {
		json.Unmarshal(session.Context, &contextData)
	}

	// Try to get real-time token usage from Redis (if available)
	tokensUsed := int(session.TokensUsed.Int32)
	if redisKey := fmt.Sprintf("session:%s", sessionID); h.redis != nil {
		if val, err := h.redis.HGet(ctx, redisKey, "total_tokens_used").Int(); err == nil {
			tokensUsed = val
		}
	}

	// Build response
	resp := SessionResponse{
		SessionID:   session.ID,
		UserID:      session.UserID,
		Context:     contextData,
		TokensUsed:  tokensUsed,
		TaskCount:   taskCount,
		CreatedAt:   session.CreatedAt,
	}

	if session.TokenBudget.Valid {
		resp.TokenBudget = int(session.TokenBudget.Int32)
	}
	if session.UpdatedAt.Valid {
		resp.UpdatedAt = &session.UpdatedAt.Time
	}
	if session.ExpiresAt.Valid {
		resp.ExpiresAt = &session.ExpiresAt.Time
	}

	h.logger.Debug("Session retrieved",
		zap.String("session_id", sessionID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.Int("task_count", taskCount),
	)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// GetSessionHistory handles GET /api/v1/sessions/{sessionId}/history
func (h *SessionHandler) GetSessionHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context from auth middleware
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from path
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		h.sendError(w, "Session ID is required", http.StatusBadRequest)
		return
	}

    // Verify session exists and user has access
    var sessionUserID string
    err := h.db.GetContext(ctx, &sessionUserID, `
        SELECT user_id FROM sessions WHERE id::text = $1 AND deleted_at IS NULL
    `, sessionID)

	if err == sql.ErrNoRows {
		h.sendError(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("Failed to query session", zap.Error(err))
		h.sendError(w, "Failed to retrieve session", http.StatusInternalServerError)
		return
	}

	// Verify user has access
	if sessionUserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Query all tasks for this session
	rows, err := h.db.QueryxContext(ctx, `
		SELECT
			id,
			workflow_id,
			query,
			status,
			COALESCE(mode, '') as mode,
			COALESCE(result, '') as result,
			COALESCE(error_message, '') as error_message,
			COALESCE(total_tokens, 0) as total_tokens,
			COALESCE(total_cost_usd, 0) as total_cost_usd,
			COALESCE(duration_ms, 0) as duration_ms,
			COALESCE(agents_used, 0) as agents_used,
			COALESCE(tools_invoked, 0) as tools_invoked,
			started_at,
			completed_at,
			metadata
		FROM task_executions
		WHERE session_id = $1
		ORDER BY started_at ASC
	`, sessionID)

	if err != nil {
		h.logger.Error("Failed to query task history", zap.Error(err))
		h.sendError(w, "Failed to retrieve session history", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Parse results
	tasks := make([]TaskHistory, 0)
	for rows.Next() {
		var task struct {
			ID           string         `db:"id"`
			WorkflowID   string         `db:"workflow_id"`
			Query        string         `db:"query"`
			Status       string         `db:"status"`
			Mode         string         `db:"mode"`
			Result       string         `db:"result"`
			ErrorMessage string         `db:"error_message"`
			TotalTokens  int            `db:"total_tokens"`
			TotalCostUSD float64        `db:"total_cost_usd"`
			DurationMs   int            `db:"duration_ms"`
			AgentsUsed   int            `db:"agents_used"`
			ToolsInvoked int            `db:"tools_invoked"`
			StartedAt    time.Time      `db:"started_at"`
			CompletedAt  sql.NullTime   `db:"completed_at"`
			Metadata     []byte         `db:"metadata"`
		}

		if err := rows.StructScan(&task); err != nil {
			h.logger.Error("Failed to scan task", zap.Error(err))
			continue
		}

		// Parse metadata JSON
		var metadata map[string]interface{}
		if len(task.Metadata) > 0 {
			json.Unmarshal(task.Metadata, &metadata)
		}

		history := TaskHistory{
			TaskID:       task.ID,
			WorkflowID:   task.WorkflowID,
			Query:        task.Query,
			Status:       task.Status,
			Mode:         task.Mode,
			Result:       task.Result,
			ErrorMessage: task.ErrorMessage,
			TotalTokens:  task.TotalTokens,
			TotalCostUSD: task.TotalCostUSD,
			DurationMs:   task.DurationMs,
			AgentsUsed:   task.AgentsUsed,
			ToolsInvoked: task.ToolsInvoked,
			StartedAt:    task.StartedAt,
			Metadata:     metadata,
		}

		if task.CompletedAt.Valid {
			history.CompletedAt = &task.CompletedAt.Time
		}

		tasks = append(tasks, history)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("Failed to iterate task rows", zap.Error(err))
		h.sendError(w, "Failed to retrieve session history", http.StatusInternalServerError)
		return
	}

	h.logger.Debug("Session history retrieved",
		zap.String("session_id", sessionID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.Int("task_count", len(tasks)),
	)

	// Build response
	resp := SessionHistoryResponse{
		SessionID: sessionID,
		Tasks:     tasks,
		Total:     len(tasks),
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// GetSessionEvents handles GET /api/v1/sessions/{sessionId}/events
// Returns SSE-like events for all tasks in a session, excluding LLM_PARTIAL.
func (h *SessionHandler) GetSessionEvents(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Get user context from auth middleware
    userCtx, ok := ctx.Value("user").(*auth.UserContext)
    if !ok {
        h.sendError(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    // Extract session ID from path
    sessionID := r.PathValue("sessionId")
    if sessionID == "" {
        h.sendError(w, "Session ID is required", http.StatusBadRequest)
        return
    }

    // Verify session exists and user has access
    var sessionUserID string
    err := h.db.GetContext(ctx, &sessionUserID, `
        SELECT user_id FROM sessions WHERE id::text = $1 AND deleted_at IS NULL
    `, sessionID)
    if err == sql.ErrNoRows {
        h.sendError(w, "Session not found", http.StatusNotFound)
        return
    }
    if err != nil {
        h.logger.Error("Failed to query session", zap.Error(err))
        h.sendError(w, "Failed to retrieve session", http.StatusInternalServerError)
        return
    }
    if sessionUserID != userCtx.UserID.String() {
        h.sendError(w, "Forbidden", http.StatusForbidden)
        return
    }

    // Pagination params
    q := r.URL.Query()
    limit := 200
    if v := q.Get("limit"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
            limit = n
        }
    }
    offset := 0
    if v := q.Get("offset"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n >= 0 {
            offset = n
        }
    }

    // Event shape (aligned with task events endpoint)
    type Event struct {
        WorkflowID string    `json:"workflow_id"`
        Type       string    `json:"type"`
        AgentID    string    `json:"agent_id,omitempty"`
        Message    string    `json:"message,omitempty"`
        Timestamp  time.Time `json:"timestamp"`
        Seq        uint64    `json:"seq"`
        StreamID   string    `json:"stream_id,omitempty"`
    }

    // Fetch events across all tasks in this session, excluding LLM_PARTIAL
    rows, err := h.db.QueryxContext(ctx, `
        SELECT e.workflow_id, e.type, COALESCE(e.agent_id,''), COALESCE(e.message,''), e.timestamp, COALESCE(e.seq,0), COALESCE(e.stream_id,'')
        FROM event_logs e
        JOIN task_executions t ON t.workflow_id = e.workflow_id
        WHERE t.session_id = $1 AND t.user_id = $2 AND e.type <> 'LLM_PARTIAL'
        ORDER BY e.timestamp ASC
        LIMIT $3 OFFSET $4
    `, sessionID, userCtx.UserID.String(), limit, offset)
    if err != nil {
        h.logger.Error("Failed to query session events", zap.Error(err))
        h.sendError(w, "Failed to retrieve session events", http.StatusInternalServerError)
        return
    }
    defer rows.Close()

    events := []Event{}
    for rows.Next() {
        var e Event
        if err := rows.Scan(&e.WorkflowID, &e.Type, &e.AgentID, &e.Message, &e.Timestamp, &e.Seq, &e.StreamID); err != nil {
            h.sendError(w, "Failed to scan event row", http.StatusInternalServerError)
            return
        }
        events = append(events, e)
    }
    if err := rows.Err(); err != nil {
        h.sendError(w, "Failed to read session events", http.StatusInternalServerError)
        return
    }

    h.logger.Debug("Session events retrieved",
        zap.String("session_id", sessionID),
        zap.String("user_id", userCtx.UserID.String()),
        zap.Int("count", len(events)),
    )

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "session_id": sessionID,
        "events":     events,
        "count":      len(events),
    })
}

// ListSessions handles GET /api/v1/sessions
func (h *SessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context from auth middleware
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse pagination params
	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Query sessions for this user
	// Note: task_executions.session_id is VARCHAR, sessions.id is UUID - cast for JOIN
    rows, err := h.db.QueryxContext(ctx, `
        SELECT
            s.id,
            s.user_id,
            COALESCE(s.context->>'title', '') as title,
            COALESCE(s.token_budget, 0) as token_budget,
            COALESCE(s.tokens_used, 0) as tokens_used,
            s.created_at,
            s.updated_at,
            s.expires_at,
            COUNT(t.id) as task_count
        FROM sessions s
        LEFT JOIN task_executions t ON t.session_id = s.id::text
        WHERE s.user_id = $1 AND s.deleted_at IS NULL
        GROUP BY s.id, s.user_id, s.context, s.token_budget, s.tokens_used, s.created_at, s.updated_at, s.expires_at
        ORDER BY s.created_at DESC
        LIMIT $2 OFFSET $3
    `, userCtx.UserID.String(), limit, offset)

	if err != nil {
		h.logger.Error("Failed to query sessions", zap.Error(err))
		h.sendError(w, "Failed to retrieve sessions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Parse results
	sessions := make([]SessionSummary, 0)
	for rows.Next() {
		var sess struct {
			ID          string       `db:"id"`
			UserID      string       `db:"user_id"`
			Title       string       `db:"title"`
			TokenBudget int          `db:"token_budget"`
			TokensUsed  int          `db:"tokens_used"`
			CreatedAt   time.Time    `db:"created_at"`
			UpdatedAt   sql.NullTime `db:"updated_at"`
			ExpiresAt   sql.NullTime `db:"expires_at"`
			TaskCount   int          `db:"task_count"`
		}

		if err := rows.StructScan(&sess); err != nil {
			h.logger.Error("Failed to scan session", zap.Error(err))
			continue
		}

		summary := SessionSummary{
			SessionID:  sess.ID,
			UserID:     sess.UserID,
			Title:      sess.Title,
			TaskCount:  sess.TaskCount,
			TokensUsed: sess.TokensUsed,
			CreatedAt:  sess.CreatedAt,
		}

		if sess.TokenBudget > 0 {
			summary.TokenBudget = sess.TokenBudget
		}
		if sess.UpdatedAt.Valid {
			summary.UpdatedAt = &sess.UpdatedAt.Time
		}
		if sess.ExpiresAt.Valid {
			summary.ExpiresAt = &sess.ExpiresAt.Time
		}

		sessions = append(sessions, summary)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("Failed to iterate session rows", zap.Error(err))
		h.sendError(w, "Failed to retrieve sessions", http.StatusInternalServerError)
		return
	}

	// Get total count for pagination
	var totalCount int
    err = h.db.GetContext(ctx, &totalCount, `
        SELECT COUNT(*) FROM sessions WHERE user_id = $1 AND deleted_at IS NULL
    `, userCtx.UserID.String())
	if err != nil {
		h.logger.Warn("Failed to get total session count", zap.Error(err))
		totalCount = len(sessions)
	}

	h.logger.Debug("Sessions retrieved",
		zap.String("user_id", userCtx.UserID.String()),
		zap.Int("count", len(sessions)),
		zap.Int("total", totalCount),
	)

	// Build response
	resp := ListSessionsResponse{
		Sessions:   sessions,
		TotalCount: totalCount,
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// sendError sends an error response
func (h *SessionHandler) sendError(w http.ResponseWriter, message string, code int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    json.NewEncoder(w).Encode(map[string]string{
        "error": message,
    })
}

// UpdateSessionTitle handles PATCH /api/v1/sessions/{sessionId}
func (h *SessionHandler) UpdateSessionTitle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from path
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		h.sendError(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var reqBody struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate title
	title := strings.TrimSpace(reqBody.Title)
	if title == "" {
		h.sendError(w, "Title cannot be empty", http.StatusBadRequest)
		return
	}

	// Sanitize title: strip control characters (newlines, tabs, zero-width chars, etc.)
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u200B' || r == '\uFEFF' {
			return -1 // Remove control characters, zero-width space, and BOM
		}
		return r
	}, title)

	// Re-check after sanitization
	if strings.TrimSpace(title) == "" {
		h.sendError(w, "Title cannot contain only control characters", http.StatusBadRequest)
		return
	}

	// Check character count (runes) not byte count to match truncation logic
	if len([]rune(title)) > 60 {
		h.sendError(w, "Title must be 60 characters or less", http.StatusBadRequest)
		return
	}

	// Verify session exists and user has access
	var session struct {
		UserID  string `db:"user_id"`
		Context []byte `db:"context"`
	}
	err := h.db.GetContext(ctx, &session, `
		SELECT user_id, context FROM sessions WHERE id::text = $1 AND deleted_at IS NULL
	`, sessionID)
	if err == sql.ErrNoRows {
		h.sendError(w, "Session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("Failed to query session", zap.Error(err))
		h.sendError(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	// Verify ownership
	if session.UserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Parse existing context
	var contextData map[string]interface{}
	if len(session.Context) > 0 {
		if err := json.Unmarshal(session.Context, &contextData); err != nil {
			h.logger.Error("Failed to parse session context", zap.Error(err))
			contextData = make(map[string]interface{})
		}
	} else {
		contextData = make(map[string]interface{})
	}

	// Update title in context
	contextData[SessionContextKeyTitle] = title

	// Serialize updated context
	updatedContext, err := json.Marshal(contextData)
	if err != nil {
		h.logger.Error("Failed to serialize context", zap.Error(err))
		h.sendError(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	// Update database (with deleted_at guard for race hardening)
	_, err = h.db.ExecContext(ctx, `
		UPDATE sessions
		SET context = $1, updated_at = NOW()
		WHERE id::text = $2 AND deleted_at IS NULL
	`, updatedContext, sessionID)
	if err != nil {
		h.logger.Error("Failed to update session title", zap.Error(err))
		h.sendError(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	// Also update Redis cache if available (best-effort)
	if h.redis != nil {
		redisKey := fmt.Sprintf("session:%s", sessionID)
		// Try to get current session data from Redis
		sessionData, err := h.redis.Get(ctx, redisKey).Result()
		if err != nil {
			h.logger.Debug("Session not found in Redis cache or Redis unavailable",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
		} else {
			var redisSession map[string]interface{}
			if err := json.Unmarshal([]byte(sessionData), &redisSession); err != nil {
				h.logger.Warn("Failed to unmarshal Redis session data",
					zap.String("session_id", sessionID),
					zap.Error(err),
				)
			} else {
				// Update context in Redis session (lowercase "context" to match Session struct)
				if redisCtx, ok := redisSession["context"].(map[string]interface{}); ok {
					redisCtx[SessionContextKeyTitle] = title
				} else {
					redisSession["context"] = map[string]interface{}{SessionContextKeyTitle: title}
				}
				// Write back to Redis with KeepTTL to preserve expiration
				if updatedData, err := json.Marshal(redisSession); err != nil {
					h.logger.Warn("Failed to marshal updated Redis session",
						zap.String("session_id", sessionID),
						zap.Error(err),
					)
				} else {
					if err := h.redis.SetArgs(ctx, redisKey, updatedData, redis.SetArgs{KeepTTL: true}).Err(); err != nil {
						h.logger.Warn("Failed to update Redis cache with new title",
							zap.String("session_id", sessionID),
							zap.Error(err),
						)
					}
				}
			}
		}
	}

	h.logger.Info("Session title updated",
		zap.String("session_id", sessionID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("new_title", title),
	)

	// Return success with updated title
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"title":      title,
	})
}

// DeleteSession handles DELETE /api/v1/sessions/{sessionId}
func (h *SessionHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Get user context
    userCtx, ok := ctx.Value("user").(*auth.UserContext)
    if !ok {
        h.sendError(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    // Extract and validate session ID
    sessionID := r.PathValue("sessionId")
    if sessionID == "" {
        h.sendError(w, "Session ID is required", http.StatusBadRequest)
        return
    }
    if _, err := uuid.Parse(sessionID); err != nil {
        h.sendError(w, "Invalid session ID format (uuid required)", http.StatusBadRequest)
        return
    }

    // Ownership check (do not filter on deleted_at to keep idempotency)
    var owner string
    if err := h.db.GetContext(ctx, &owner, `
        SELECT user_id FROM sessions WHERE id::text = $1
    `, sessionID); err != nil {
        if err == sql.ErrNoRows {
            h.sendError(w, "Session not found", http.StatusNotFound)
            return
        }
        h.logger.Error("Failed to query session for delete", zap.Error(err), zap.String("session_id", sessionID))
        h.sendError(w, "Failed to delete session", http.StatusInternalServerError)
        return
    }
    if owner != userCtx.UserID.String() {
        h.sendError(w, "Forbidden", http.StatusForbidden)
        return
    }

    // Soft delete (idempotent)
    if _, err := h.db.ExecContext(ctx, `
        UPDATE sessions
        SET deleted_at = NOW(), deleted_by = $2, updated_at = NOW()
        WHERE id::text = $1 AND deleted_at IS NULL
    `, sessionID, userCtx.UserID.String()); err != nil {
        h.logger.Error("Failed to soft delete session", zap.Error(err), zap.String("session_id", sessionID))
        h.sendError(w, "Failed to delete session", http.StatusInternalServerError)
        return
    }

    // Clear Redis cache for this session (best-effort)
    if h.redis != nil {
        key := fmt.Sprintf("session:%s", sessionID)
        if err := h.redis.Del(ctx, key).Err(); err != nil {
            h.logger.Warn("Failed to clear session cache", zap.Error(err), zap.String("key", key))
        }
    }

    // No content, idempotent
    w.WriteHeader(http.StatusNoContent)
}
