package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
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
	SessionID   string                 `json:"session_id"`
	UserID      string                 `json:"user_id"`
	Context     map[string]interface{} `json:"context,omitempty"`
	TokenBudget int                    `json:"token_budget,omitempty"`
	TokensUsed  int                    `json:"tokens_used"`
	TaskCount   int                    `json:"task_count"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   *time.Time             `json:"updated_at,omitempty"`
	ExpiresAt   *time.Time             `json:"expires_at,omitempty"`
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
	TaskID       string                 `json:"task_id"`
	WorkflowID   string                 `json:"workflow_id"`
	Query        string                 `json:"query"`
	Status       string                 `json:"status"`
	Mode         string                 `json:"mode,omitempty"`
	Result       string                 `json:"result,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	TotalTokens  int                    `json:"total_tokens"`
	TotalCostUSD float64                `json:"total_cost_usd"`
	DurationMs   int                    `json:"duration_ms,omitempty"`
	AgentsUsed   int                    `json:"agents_used"`
	ToolsInvoked int                    `json:"tools_invoked"`
	StartedAt    time.Time              `json:"started_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
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
		ID          string        `db:"id"`
		UserID      string        `db:"user_id"`
		Context     []byte        `db:"context"`
		TokenBudget sql.NullInt32 `db:"token_budget"`
		TokensUsed  sql.NullInt32 `db:"tokens_used"`
		CreatedAt   time.Time     `db:"created_at"`
		UpdatedAt   sql.NullTime  `db:"updated_at"`
		ExpiresAt   sql.NullTime  `db:"expires_at"`
	}

	err := h.db.GetContext(ctx, &session, `
            SELECT id, user_id, context, token_budget, tokens_used, created_at, updated_at, expires_at
            FROM sessions
            WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
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

	// Parse context JSON first to get external_id if present
	var contextData map[string]interface{}
	if len(session.Context) > 0 {
		json.Unmarshal(session.Context, &contextData)
	}

	// Get task count for this session from task_executions (check both UUID and external_id)
	var taskCount int
	if extID, ok := contextData["external_id"].(string); ok && extID != "" {
		// Session has external_id, check both session.ID and external_id
		err = h.db.GetContext(ctx, &taskCount, `
			SELECT COUNT(*) FROM task_executions
			WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
		`, session.ID, extID, userCtx.UserID.String())
	} else {
		// No external_id, just check by UUID
		err = h.db.GetContext(ctx, &taskCount, `
			SELECT COUNT(*) FROM task_executions
			WHERE session_id = $1 AND user_id = $2
		`, session.ID, userCtx.UserID.String())
	}
	if err != nil {
		h.logger.Warn("Failed to get task count", zap.Error(err))
		taskCount = 0
	}

	// Try to get real-time token usage from Redis (if available)
	// Session manager stores sessions as JSON values with SET, not as Redis hashes
	// Note: Don't use session.TokensUsed - it's not reliably updated. Always get from Redis or task_executions.
	tokensUsed := 0
	if h.redis != nil {
		// Try both possible Redis keys: the input sessionID and any external_id
		keysToTry := []string{fmt.Sprintf("session:%s", sessionID)}
		if extID, ok := contextData["external_id"].(string); ok && extID != "" {
			keysToTry = append(keysToTry, fmt.Sprintf("session:%s", extID))
		}

		for _, redisKey := range keysToTry {
			if data, err := h.redis.Get(ctx, redisKey).Result(); err == nil {
				var sessionData map[string]interface{}
				if err := json.Unmarshal([]byte(data), &sessionData); err == nil {
					if tokens, ok := sessionData["total_tokens_used"].(float64); ok {
						tokensUsed = int(tokens)
						break
					}
				}
			}
		}
	}

	// If still 0, aggregate from task_executions as fallback (most accurate source)
	if tokensUsed == 0 {
		var aggregatedTokens int
		if extID, ok := contextData["external_id"].(string); ok && extID != "" {
			err = h.db.GetContext(ctx, &aggregatedTokens, `
				SELECT COALESCE(SUM(total_tokens), 0)::int FROM task_executions
				WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
			`, session.ID, extID, userCtx.UserID.String())
		} else {
			err = h.db.GetContext(ctx, &aggregatedTokens, `
				SELECT COALESCE(SUM(total_tokens), 0)::int FROM task_executions
				WHERE session_id = $1 AND user_id = $2
			`, session.ID, userCtx.UserID.String())
		}
		if err == nil && aggregatedTokens > 0 {
			tokensUsed = aggregatedTokens
		}
	}

	// Build response
	resp := SessionResponse{
		SessionID:  session.ID,
		UserID:     session.UserID,
		Context:    contextData,
		TokensUsed: tokensUsed,
		TaskCount:  taskCount,
		CreatedAt:  session.CreatedAt,
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

	// Verify session exists and user has access (also get id and external_id for task query)
	var sessionData struct {
		ID         string  `db:"id"`
		UserID     string  `db:"user_id"`
		ExternalID *string `db:"external_id"`
	}
	err := h.db.GetContext(ctx, &sessionData, `
        SELECT id, user_id, context->>'external_id' as external_id
        FROM sessions
        WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
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
	if sessionData.UserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Query all tasks for this session (check both UUID and external_id with user_id filter)
	var rows *sqlx.Rows
	if sessionData.ExternalID != nil && *sessionData.ExternalID != "" {
		// Session has external_id, check both session.ID and external_id
		rows, err = h.db.QueryxContext(ctx, `
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
			WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
			ORDER BY started_at ASC
		`, sessionData.ID, *sessionData.ExternalID, sessionData.UserID)
	} else {
		// No external_id, just check by UUID
		rows, err = h.db.QueryxContext(ctx, `
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
			WHERE session_id = $1 AND user_id = $2
			ORDER BY started_at ASC
		`, sessionData.ID, sessionData.UserID)
	}

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
			ID           string       `db:"id"`
			WorkflowID   string       `db:"workflow_id"`
			Query        string       `db:"query"`
			Status       string       `db:"status"`
			Mode         string       `db:"mode"`
			Result       string       `db:"result"`
			ErrorMessage string       `db:"error_message"`
			TotalTokens  int          `db:"total_tokens"`
			TotalCostUSD float64      `db:"total_cost_usd"`
			DurationMs   int          `db:"duration_ms"`
			AgentsUsed   int          `db:"agents_used"`
			ToolsInvoked int          `db:"tools_invoked"`
			StartedAt    time.Time    `db:"started_at"`
			CompletedAt  sql.NullTime `db:"completed_at"`
			Metadata     []byte       `db:"metadata"`
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
// Returns grouped turns (one per task) including full events per turn.
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

	// Verify session exists and user has access (also get id and external_id)
	var sessionData struct {
		ID         string  `db:"id"`
		UserID     string  `db:"user_id"`
		ExternalID *string `db:"external_id"`
	}
	err := h.db.GetContext(ctx, &sessionData, `
        SELECT id, user_id, context->>'external_id' as external_id
        FROM sessions
        WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
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
	if sessionData.UserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Pagination params (turns)
	q := r.URL.Query()
	limit := 10
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
	includePayload := q.Get("include_payload") == "true"

	// Shapes
	type Event struct {
		WorkflowID string    `json:"workflow_id"`
		Type       string    `json:"type"`
		AgentID    string    `json:"agent_id,omitempty"`
		Message    string    `json:"message,omitempty"`
		Timestamp  time.Time `json:"timestamp"`
		Seq        uint64    `json:"seq"`
		StreamID   string    `json:"stream_id,omitempty"`
		Payload    string    `json:"payload,omitempty"`
	}
	type Turn struct {
		Turn        int       `json:"turn"`
		TaskID      string    `json:"task_id"`
		UserQuery   string    `json:"user_query"`
		FinalOutput string    `json:"final_output"`
		Timestamp   time.Time `json:"timestamp"`
		Events      []Event   `json:"events"`
		Metadata    struct {
			TokensUsed      int      `json:"tokens_used"`
			ExecutionTimeMs int      `json:"execution_time_ms"`
			AgentsInvolved  []string `json:"agents_involved"`
		} `json:"metadata"`
	}

	// Count total turns (tasks)
	var total int
	if sessionData.ExternalID != nil && *sessionData.ExternalID != "" {
		err = h.db.GetContext(ctx, &total, `
            SELECT COUNT(*) FROM task_executions
            WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
        `, sessionData.ID, *sessionData.ExternalID, sessionData.UserID)
	} else {
		err = h.db.GetContext(ctx, &total, `
            SELECT COUNT(*) FROM task_executions
            WHERE session_id = $1 AND user_id = $2
        `, sessionData.ID, sessionData.UserID)
	}
	if err != nil {
		h.logger.Error("Failed to count session turns", zap.Error(err))
		h.sendError(w, "Failed to retrieve session events", http.StatusInternalServerError)
		return
	}

	// Fetch turns ordered by started_at
	type turnRow struct {
		TaskID      string         `db:"id"`
		WorkflowID  string         `db:"workflow_id"`
		Query       string         `db:"query"`
		Result      sql.NullString `db:"result"`
		StartedAt   time.Time      `db:"started_at"`
		CompletedAt sql.NullTime   `db:"completed_at"`
		TotalTokens sql.NullInt32  `db:"total_tokens"`
		DurationMs  sql.NullInt32  `db:"duration_ms"`
	}

	var turnRows []turnRow
	if sessionData.ExternalID != nil && *sessionData.ExternalID != "" {
		err = h.db.SelectContext(ctx, &turnRows, `
            SELECT id, workflow_id, query, result, started_at, completed_at,
                   COALESCE(total_tokens,0) as total_tokens,
                   COALESCE(duration_ms,0) as duration_ms
            FROM task_executions
            WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
            ORDER BY started_at ASC
            LIMIT $4 OFFSET $5
        `, sessionData.ID, *sessionData.ExternalID, sessionData.UserID, limit, offset)
	} else {
		err = h.db.SelectContext(ctx, &turnRows, `
            SELECT id, workflow_id, query, result, started_at, completed_at,
                   COALESCE(total_tokens,0) as total_tokens,
                   COALESCE(duration_ms,0) as duration_ms
            FROM task_executions
            WHERE session_id = $1 AND user_id = $2
            ORDER BY started_at ASC
            LIMIT $3 OFFSET $4
        `, sessionData.ID, sessionData.UserID, limit, offset)
	}
	if err != nil {
		h.logger.Error("Failed to query session turns", zap.Error(err))
		h.sendError(w, "Failed to retrieve session events", http.StatusInternalServerError)
		return
	}

	// Build IN clause for workflows
	wfIDs := make([]string, 0, len(turnRows))
	for _, t := range turnRows {
		wfIDs = append(wfIDs, t.WorkflowID)
	}

	// Map of workflow_id -> []Event
	eventsByWF := make(map[string][]Event, len(wfIDs))
	if len(wfIDs) > 0 {
		// Safe IN expansion using sqlx.In and rebind for Postgres
		// Conditionally include payload based on query parameter
		baseQuery := `
            SELECT workflow_id, type, COALESCE(agent_id,''), COALESCE(message,''), timestamp, COALESCE(seq,0), COALESCE(stream_id,'')`
		if includePayload {
			baseQuery += `, COALESCE(payload::text,'')`
		}
		baseQuery += `
            FROM event_logs
            WHERE workflow_id IN (?) AND type <> 'LLM_PARTIAL'
            ORDER BY timestamp ASC`
		inQuery, args, inErr := sqlx.In(baseQuery, wfIDs)
		if inErr != nil {
			h.logger.Error("Failed to build IN query for workflows", zap.Error(inErr))
			h.sendError(w, "Failed to retrieve session events", http.StatusInternalServerError)
			return
		}
		// Force PostgreSQL-style bindvars for predictable queries
		inQuery = sqlx.Rebind(sqlx.DOLLAR, inQuery)

		rows, qerr := h.db.QueryxContext(ctx, inQuery, args...)
		if qerr != nil {
			h.logger.Error("Failed to query events for workflows", zap.Error(qerr))
			h.sendError(w, "Failed to retrieve session events", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		// Safety: cap total events to avoid excessive memory usage per request
		const maxEventsPerRequest = 5000
		totalEvents := 0
		for rows.Next() {
			if totalEvents >= maxEventsPerRequest {
				h.logger.Error("Session exceeds event limit",
					zap.Int("max", maxEventsPerRequest),
					zap.Strings("workflow_ids", wfIDs))
				h.sendError(w, "Session has too many events. Please use pagination with smaller turn limits.", http.StatusRequestEntityTooLarge)
				return
			}
			var e Event
			if includePayload {
				if err := rows.Scan(&e.WorkflowID, &e.Type, &e.AgentID, &e.Message, &e.Timestamp, &e.Seq, &e.StreamID, &e.Payload); err != nil {
					h.logger.Error("Failed to scan event row with payload", zap.Error(err), zap.Strings("workflow_ids", wfIDs))
					h.sendError(w, "Failed to read events", http.StatusInternalServerError)
					return
				}
			} else {
				if err := rows.Scan(&e.WorkflowID, &e.Type, &e.AgentID, &e.Message, &e.Timestamp, &e.Seq, &e.StreamID); err != nil {
					h.logger.Error("Failed to scan event row", zap.Error(err), zap.Strings("workflow_ids", wfIDs))
					h.sendError(w, "Failed to read events", http.StatusInternalServerError)
					return
				}
			}
			eventsByWF[e.WorkflowID] = append(eventsByWF[e.WorkflowID], e)
			totalEvents++
		}
		if err := rows.Err(); err != nil {
			h.sendError(w, "Failed to read events", http.StatusInternalServerError)
			return
		}
	}

	// Build turns with metadata and fallback logic
	turns := make([]Turn, 0, len(turnRows))
	globalStart := offset + 1
	for i, t := range turnRows {
		evs := eventsByWF[t.WorkflowID]
		// Final output fallback: result -> first LLM_OUTPUT message -> ""
		final := strings.TrimSpace(t.Result.String)
		if final == "" {
			for _, e := range evs {
				if e.Type == "LLM_OUTPUT" && strings.TrimSpace(e.Message) != "" {
					final = e.Message
					break
				}
			}
		}
		// Execution time
		execMs := 0
		if t.DurationMs.Valid && t.DurationMs.Int32 > 0 {
			execMs = int(t.DurationMs.Int32)
		} else if t.CompletedAt.Valid {
			ms := int(t.CompletedAt.Time.Sub(t.StartedAt).Milliseconds())
			if ms > 0 {
				execMs = ms
			}
		} else {
			// Still running: compute duration up to now
			ms := int(time.Since(t.StartedAt).Milliseconds())
			if ms > 0 {
				execMs = ms
			}
		}
		// Agents involved (distinct agent_id from events)
		agentSet := map[string]struct{}{}
		for _, e := range evs {
			if e.AgentID != "" {
				agentSet[e.AgentID] = struct{}{}
			}
		}
		agents := make([]string, 0, len(agentSet))
		for a := range agentSet {
			agents = append(agents, a)
		}
		sort.Strings(agents) // Sort for deterministic output

		var turn Turn
		turn.Turn = globalStart + i
		turn.TaskID = t.TaskID
		turn.UserQuery = t.Query
		turn.FinalOutput = final
		turn.Timestamp = t.StartedAt
		turn.Events = evs
		turn.Metadata.TokensUsed = int(t.TotalTokens.Int32)
		turn.Metadata.ExecutionTimeMs = execMs
		turn.Metadata.AgentsInvolved = agents
		turns = append(turns, turn)
	}

	h.logger.Debug("Session turns retrieved",
		zap.String("session_id", sessionID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.Int("turns", len(turns)),
		zap.Int("total", total),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sessionID,
		"count":      total,
		"turns":      turns,
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
	// Note: task_executions.session_id is VARCHAR, sessions.id is UUID - match by id or external_id
	// Aggregate tokens_used from task_executions (more accurate than sessions.tokens_used which may not be updated)
	rows, err := h.db.QueryxContext(ctx, `
        SELECT
            s.id,
            s.user_id,
            COALESCE(s.context->>'title', '') as title,
            COALESCE(s.token_budget, 0) as token_budget,
            COALESCE(SUM(t.total_tokens), 0)::int as tokens_used,
            s.created_at,
            s.updated_at,
            s.expires_at,
            COUNT(t.id) as task_count
        FROM sessions s
        LEFT JOIN task_executions t ON (t.session_id = s.id::text OR t.session_id = s.context->>'external_id')
            AND t.user_id = s.user_id
        WHERE s.user_id = $1 AND s.deleted_at IS NULL
        GROUP BY s.id, s.user_id, s.context, s.token_budget, s.created_at, s.updated_at, s.expires_at
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
		// Don't fail the request, just return 0 total
		totalCount = len(sessions)
	}

	// Enrich with real-time token usage from Redis if available
	if h.redis != nil && len(sessions) > 0 {
		// Prepare keys for MGET
		// We need to check both session ID and external ID (if present)
		// Since MGET is simple key-value, we'll fetch both possible keys for each session
		// and take the max value found.
		var keys []string
		for _, s := range sessions {
			keys = append(keys, fmt.Sprintf("session:%s", s.SessionID))
			// We don't have external_id in SessionSummary easily available without parsing context
			// For list view, we'll stick to session ID for now to keep it efficient.
			// If external_id is critical for token counts in list view, we'd need to fetch it.
		}

		// Execute MGET
		if len(keys) > 0 {
			values, err := h.redis.MGet(ctx, keys...).Result()
			if err != nil {
				h.logger.Warn("Failed to fetch session data from Redis", zap.Error(err))
			} else {
				// Update sessions with Redis data
				for i, val := range values {
					if val == nil {
						continue
					}

					// Redis returns interface{}, needs type assertion
					if strVal, ok := val.(string); ok {
						var sessionData map[string]interface{}
						if err := json.Unmarshal([]byte(strVal), &sessionData); err == nil {
							if tokens, ok := sessionData["total_tokens_used"].(float64); ok {
								// Update if Redis has more current data
								if int(tokens) > sessions[i].TokensUsed {
									sessions[i].TokensUsed = int(tokens)
								}
							}
						}
					}
				}
			}
		}
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

	// Verify session exists and user has access (fetch ID for Redis cache)
	var session struct {
		ID      string `db:"id"`
		UserID  string `db:"user_id"`
		Context []byte `db:"context"`
	}
	err := h.db.GetContext(ctx, &session, `
		SELECT id, user_id, context FROM sessions WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
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
		WHERE (id::text = $2 OR context->>'external_id' = $2) AND deleted_at IS NULL
	`, updatedContext, sessionID)
	if err != nil {
		h.logger.Error("Failed to update session title", zap.Error(err))
		h.sendError(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	// Also update Redis cache if available (best-effort)
	// Session manager may store sessions under different keys depending on how they were created
	if h.redis != nil {
		// Try multiple possible Redis keys: input sessionID, DB UUID, and external_id
		keysToTry := []string{
			fmt.Sprintf("session:%s", sessionID),  // Original input (could be UUID or external_id)
			fmt.Sprintf("session:%s", session.ID), // Database UUID
		}
		// Also add external_id if present
		if extID, ok := contextData["external_id"].(string); ok && extID != "" {
			keysToTry = append(keysToTry, fmt.Sprintf("session:%s", extID))
		}

		// Try each possible key
		for _, redisKey := range keysToTry {
			sessionData, err := h.redis.Get(ctx, redisKey).Result()
			if err != nil {
				continue // Try next key
			}

			var redisSession map[string]interface{}
			if err := json.Unmarshal([]byte(sessionData), &redisSession); err != nil {
				h.logger.Warn("Failed to unmarshal Redis session data",
					zap.String("redis_key", redisKey),
					zap.Error(err),
				)
				continue
			}

			// Update context in Redis session (lowercase "context" to match Session struct)
			if redisCtx, ok := redisSession["context"].(map[string]interface{}); ok {
				redisCtx[SessionContextKeyTitle] = title
			} else {
				redisSession["context"] = map[string]interface{}{SessionContextKeyTitle: title}
			}

			// Write back to Redis with KeepTTL to preserve expiration
			if updatedData, err := json.Marshal(redisSession); err != nil {
				h.logger.Warn("Failed to marshal updated Redis session",
					zap.String("redis_key", redisKey),
					zap.Error(err),
				)
			} else {
				if err := h.redis.SetArgs(ctx, redisKey, updatedData, redis.SetArgs{KeepTTL: true}).Err(); err != nil {
					h.logger.Warn("Failed to update Redis cache with new title",
						zap.String("redis_key", redisKey),
						zap.Error(err),
					)
				} else {
					h.logger.Debug("Updated Redis session title",
						zap.String("redis_key", redisKey),
						zap.String("new_title", title),
					)
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

	// Extract session ID (accepts both UUID and external_id for consistency)
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		h.sendError(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Ownership check and fetch canonical/id mapping (do not filter on deleted_at to keep idempotency)
	var sessMeta struct {
		ID         string  `db:"id"`
		UserID     string  `db:"user_id"`
		ExternalID *string `db:"external_id"`
	}
	if err := h.db.GetContext(ctx, &sessMeta, `
        SELECT id, user_id, context->>'external_id' as external_id
        FROM sessions WHERE (id::text = $1 OR context->>'external_id' = $1)
    `, sessionID); err != nil {
		if err == sql.ErrNoRows {
			h.sendError(w, "Session not found", http.StatusNotFound)
			return
		}
		h.logger.Error("Failed to query session for delete", zap.Error(err), zap.String("session_id", sessionID))
		h.sendError(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}
	if sessMeta.UserID != userCtx.UserID.String() {
		h.sendError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Soft delete (idempotent)
	if _, err := h.db.ExecContext(ctx, `
        UPDATE sessions
        SET deleted_at = NOW(), deleted_by = $2, updated_at = NOW()
        WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
    `, sessionID, userCtx.UserID.String()); err != nil {
		h.logger.Error("Failed to soft delete session", zap.Error(err), zap.String("session_id", sessionID))
		h.sendError(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}

	// Clear Redis cache for this session (best-effort)
	if h.redis != nil {
		// Try all possible keys: original input, DB UUID, and external_id (if present)
		keys := []string{fmt.Sprintf("session:%s", sessionID), fmt.Sprintf("session:%s", sessMeta.ID)}
		if sessMeta.ExternalID != nil && *sessMeta.ExternalID != "" {
			keys = append(keys, fmt.Sprintf("session:%s", *sessMeta.ExternalID))
		}
		for _, key := range keys {
			if err := h.redis.Del(ctx, key).Err(); err != nil {
				h.logger.Warn("Failed to clear session cache", zap.Error(err), zap.String("redis_key", key))
			}
		}
	}

	// No content, idempotent
	w.WriteHeader(http.StatusNoContent)
}
