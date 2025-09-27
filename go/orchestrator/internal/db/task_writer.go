package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
)

func buildTaskMetricsPayload(task *TaskExecution) JSONB {
	if task == nil {
		return nil
	}

	metrics := make(JSONB)

	if task.TotalTokens > 0 {
		metrics["total_tokens"] = task.TotalTokens
	}
	if task.PromptTokens > 0 {
		metrics["prompt_tokens"] = task.PromptTokens
	}
	if task.CompletionTokens > 0 {
		metrics["completion_tokens"] = task.CompletionTokens
	}
	if task.TotalCostUSD > 0 {
		metrics["total_cost_usd"] = task.TotalCostUSD
	}
	if task.DurationMs != nil {
		metrics["duration_ms"] = *task.DurationMs
	}
	if task.AgentsUsed > 0 {
		metrics["agents_used"] = task.AgentsUsed
	}
	if task.ToolsInvoked > 0 {
		metrics["tools_invoked"] = task.ToolsInvoked
	}
	if task.CacheHits > 0 {
		metrics["cache_hits"] = task.CacheHits
	}
	if task.ComplexityScore > 0 {
		metrics["complexity_score"] = task.ComplexityScore
	}
	if task.Metadata != nil && len(task.Metadata) > 0 {
		metrics["metadata"] = map[string]interface{}(task.Metadata)
	}

	if len(metrics) == 0 {
		return JSONB{}
	}

	return metrics
}

// SaveTaskExecution saves or updates a task execution record (idempotent by workflow_id)
func (c *Client) SaveTaskExecution(ctx context.Context, task *TaskExecution) error {
	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	metricsPayload := buildTaskMetricsPayload(task)

	query := `
		INSERT INTO tasks (
			id, workflow_id, user_id, session_id, query, mode, status,
			started_at, completed_at, result, error, metrics,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		ON CONFLICT (workflow_id) DO UPDATE SET
			status = EXCLUDED.status,
			completed_at = EXCLUDED.completed_at,
			result = EXCLUDED.result,
			error = EXCLUDED.error,
			metrics = CASE
				WHEN EXCLUDED.metrics IS NULL OR EXCLUDED.metrics = '{}'::jsonb THEN tasks.metrics
				ELSE EXCLUDED.metrics
			END
		RETURNING id`

	// Handle empty UUID strings as NULL
	var userID, sessionID interface{}
	if task.UserID == nil {
		userID = nil
	} else {
		userID = task.UserID
	}
	// Convert SessionID string to UUID for database
	if task.SessionID == "" {
		sessionID = nil
	} else {
		sid, err := uuid.Parse(task.SessionID)
		if err != nil {
			sessionID = nil // Invalid UUID, store as NULL
		} else {
			sessionID = sid
		}
	}

	err := c.db.QueryRowContext(ctx, query,
		task.ID, task.WorkflowID, userID, sessionID,
		task.Query, task.Mode, task.Status,
		task.StartedAt, task.CompletedAt, task.Result, task.ErrorMessage, metricsPayload,
		task.CreatedAt,
	).Scan(&task.ID)

	if err != nil {
		return fmt.Errorf("failed to save task execution: %w", err)
	}

	c.logger.Debug("Task execution saved",
		zap.String("workflow_id", task.WorkflowID),
		zap.String("status", task.Status),
	)

	return nil
}

// BatchSaveTaskExecutions saves multiple task executions in a single transaction
func (c *Client) BatchSaveTaskExecutions(ctx context.Context, tasks []*TaskExecution) error {
	if len(tasks) == 0 {
		return nil
	}

	return c.WithTransactionCB(ctx, func(tx *circuitbreaker.TxWrapper) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO tasks (
				id, workflow_id, user_id, session_id, query, mode, status,
				started_at, completed_at, result, error, metrics,
				created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
			)
			ON CONFLICT (workflow_id) DO UPDATE SET
				status = EXCLUDED.status,
				completed_at = EXCLUDED.completed_at,
				result = EXCLUDED.result,
				error = EXCLUDED.error,
				metrics = CASE
					WHEN EXCLUDED.metrics IS NULL OR EXCLUDED.metrics = '{}'::jsonb THEN tasks.metrics
					ELSE EXCLUDED.metrics
				END
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, task := range tasks {
			if task.ID == uuid.Nil {
				task.ID = uuid.New()
			}
			if task.CreatedAt.IsZero() {
				task.CreatedAt = time.Now()
			}

			// Handle empty UUID strings as NULL
			var userID, sessionID interface{}
			if task.UserID == nil {
				userID = nil
			} else {
				userID = task.UserID
			}
			// Convert SessionID string to UUID for database
			if task.SessionID == "" {
				sessionID = nil
			} else {
				sid, err := uuid.Parse(task.SessionID)
				if err != nil {
					sessionID = nil // Invalid UUID, store as NULL
				} else {
					sessionID = sid
				}
			}

			metricsPayload := buildTaskMetricsPayload(task)

			_, err := stmt.ExecContext(ctx,
				task.ID, task.WorkflowID, userID, sessionID,
				task.Query, task.Mode, task.Status,
				task.StartedAt, task.CompletedAt, task.Result, task.ErrorMessage, metricsPayload,
				task.CreatedAt,
			)
			if err != nil {
				return fmt.Errorf("failed to insert task %s: %w", task.WorkflowID, err)
			}
		}

		return nil
	})
}

// SaveAgentExecution saves an agent execution record
func (c *Client) SaveAgentExecution(ctx context.Context, agent *AgentExecution) error {
	if agent.ID == "" {
		agent.ID = uuid.New().String()
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now()
	}
	if agent.UpdatedAt.IsZero() {
		agent.UpdatedAt = time.Now()
	}

	query := `
		INSERT INTO agent_executions (
			id, workflow_id, task_id, agent_id,
			input, output, state, error_message,
			tokens_used, model_used,
			duration_ms, metadata,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)`

	_, err := c.db.ExecContext(ctx, query,
		agent.ID, agent.WorkflowID, agent.TaskID, agent.AgentID,
		agent.Input, agent.Output, agent.State, agent.ErrorMessage,
		agent.TokensUsed, agent.ModelUsed,
		agent.DurationMs, agent.Metadata,
		agent.CreatedAt, agent.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save agent execution: %w", err)
	}

	return nil
}

// BatchSaveAgentExecutions saves multiple agent executions
func (c *Client) BatchSaveAgentExecutions(ctx context.Context, agents []*AgentExecution) error {
	if len(agents) == 0 {
		return nil
	}

	valueStrings := make([]string, 0, len(agents))
	valueArgs := make([]interface{}, 0, len(agents)*14)

	for i, agent := range agents {
		if agent.ID == "" {
			agent.ID = uuid.New().String()
		}
		if agent.CreatedAt.IsZero() {
			agent.CreatedAt = time.Now()
		}
		if agent.UpdatedAt.IsZero() {
			agent.UpdatedAt = time.Now()
		}

		valueStrings = append(valueStrings, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			i*14+1, i*14+2, i*14+3, i*14+4, i*14+5,
			i*14+6, i*14+7, i*14+8, i*14+9, i*14+10,
			i*14+11, i*14+12, i*14+13, i*14+14,
		))

		valueArgs = append(valueArgs,
			agent.ID, agent.WorkflowID, agent.TaskID, agent.AgentID,
			agent.Input, agent.Output, agent.State, agent.ErrorMessage,
			agent.TokensUsed, agent.ModelUsed,
			agent.DurationMs, agent.Metadata,
			agent.CreatedAt, agent.UpdatedAt,
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO agent_executions (
			id, workflow_id, task_id, agent_id,
			input, output, state, error_message,
			tokens_used, model_used,
			duration_ms, metadata,
			created_at, updated_at
		) VALUES %s`,
		strings.Join(valueStrings, ","),
	)

	_, err := c.db.ExecContext(ctx, query, valueArgs...)
	if err != nil {
		return fmt.Errorf("failed to batch save agent executions: %w", err)
	}

	return nil
}

// SaveToolExecution saves a tool execution record
func (c *Client) SaveToolExecution(ctx context.Context, tool *ToolExecution) error {
	if tool.ID == "" {
		tool.ID = uuid.New().String()
	}
	if tool.CreatedAt.IsZero() {
		tool.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO tool_executions (
			id, workflow_id, agent_id, agent_execution_id,
			tool_name,
			input_params, output, success, error,
			duration_ms, tokens_consumed,
			metadata,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)`

	_, err := c.db.ExecContext(ctx, query,
		tool.ID, tool.WorkflowID, tool.AgentID, tool.AgentExecutionID,
		tool.ToolName,
		tool.InputParams, tool.Output, tool.Success, tool.Error,
		tool.DurationMs, tool.TokensConsumed,
		tool.Metadata,
		tool.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save tool execution: %w", err)
	}

	return nil
}

// BatchSaveToolExecutions saves multiple tool executions
func (c *Client) BatchSaveToolExecutions(ctx context.Context, tools []*ToolExecution) error {
	if len(tools) == 0 {
		return nil
	}

	return c.WithTransactionCB(ctx, func(tx *circuitbreaker.TxWrapper) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO tool_executions (
				id, workflow_id, agent_id, agent_execution_id,
				tool_name,
				input_params, output, success, error,
				duration_ms, tokens_consumed,
				metadata,
				created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
			)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, tool := range tools {
			if tool.ID == "" {
				tool.ID = uuid.New().String()
			}
			if tool.CreatedAt.IsZero() {
				tool.CreatedAt = time.Now()
			}

			_, err := stmt.ExecContext(ctx,
				tool.ID, tool.WorkflowID, tool.AgentID, tool.AgentExecutionID,
				tool.ToolName,
				tool.InputParams, tool.Output, tool.Success, tool.Error,
				tool.DurationMs, tool.TokensConsumed,
				tool.Metadata,
				tool.CreatedAt,
			)
			if err != nil {
				return fmt.Errorf("failed to insert tool %s: %w", tool.ToolName, err)
			}
		}

		return nil
	})
}

// CreateSession creates a new session in the database (tenant-aware)
func (c *Client) CreateSession(ctx context.Context, sessionID string, userID string, tenantID string) error {
	// Parse user ID - if not a valid UUID, we'll need to look it up or create a user
	var uid *uuid.UUID
	if userID != "" {
		parsed, err := uuid.Parse(userID)
		if err == nil {
			// Valid UUID - but still need to ensure user exists
			uid = &parsed

			// Check if user exists, create if not
			var exists bool
			err := c.db.QueryRowContext(ctx,
				"SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)",
				parsed,
			).Scan(&exists)

			if err != nil || !exists {
				// User doesn't exist, create it with the UUID as both id and external_id
				_, err = c.db.ExecContext(ctx,
					"INSERT INTO users (id, external_id, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING",
					parsed, parsed.String(), time.Now(), time.Now(),
				)
				if err != nil {
					c.logger.Warn("Failed to create user",
						zap.String("user_id", userID),
						zap.Error(err))
					// Continue without user_id
					uid = nil
				}
			}
		} else {
			// User ID is not a UUID, try to find or create user by external_id
			var userUUID uuid.UUID
			err := c.db.QueryRowContext(ctx,
				"SELECT id FROM users WHERE external_id = $1",
				userID,
			).Scan(&userUUID)

			if err != nil {
				// User doesn't exist, create it
				userUUID = uuid.New()
				_, err = c.db.ExecContext(ctx,
					"INSERT INTO users (id, external_id, created_at, updated_at) VALUES ($1, $2, $3, $4)",
					userUUID, userID, time.Now(), time.Now(),
				)
				if err != nil {
					c.logger.Warn("Failed to create user",
						zap.String("external_id", userID),
						zap.Error(err))
					// Continue without user_id
					uid = nil
				} else {
					uid = &userUUID
				}
			} else {
				uid = &userUUID
			}
		}
	}

	query := `
        INSERT INTO sessions (id, user_id, tenant_id, context, token_budget, tokens_used, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (id) DO NOTHING
    `

	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	// Parse tenant ID
	var tid *uuid.UUID
	if tenantID != "" {
		if parsed, err := uuid.Parse(tenantID); err == nil {
			tid = &parsed
		}
	}

	now := time.Now()
	_, err = c.db.ExecContext(ctx, query,
		sessionUUID,
		uid,
		tid,
		JSONB(map[string]interface{}{"created_from": "orchestrator"}),
		10000, // default token budget
		0,     // tokens used
		now,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// SaveSessionArchive saves a session snapshot
func (c *Client) SaveSessionArchive(ctx context.Context, archive *SessionArchive) error {
	if archive.ID == uuid.Nil {
		archive.ID = uuid.New()
	}
	if archive.SnapshotTakenAt.IsZero() {
		archive.SnapshotTakenAt = time.Now()
	}

	query := `
		INSERT INTO session_archives (
			id, session_id, user_id,
			snapshot_data, message_count, total_tokens, total_cost_usd,
			session_started_at, snapshot_taken_at, ttl_expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)`

	_, err := c.db.ExecContext(ctx, query,
		archive.ID, archive.SessionID, archive.UserID,
		archive.SnapshotData, archive.MessageCount, archive.TotalTokens, archive.TotalCostUSD,
		archive.SessionStartedAt, archive.SnapshotTakenAt, archive.TTLExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save session archive: %w", err)
	}

	return nil
}

// SaveAuditLog saves an audit log entry
func (c *Client) SaveAuditLog(ctx context.Context, audit *AuditLog) error {
	if audit.ID == uuid.Nil {
		audit.ID = uuid.New()
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO audit_logs (
			id, user_id, action, entity_type, entity_id,
			ip_address, user_agent, request_id,
			old_value, new_value, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)`

	_, err := c.db.ExecContext(ctx, query,
		audit.ID, audit.UserID, audit.Action, audit.EntityType, audit.EntityID,
		audit.IPAddress, audit.UserAgent, audit.RequestID,
		audit.OldValue, audit.NewValue, audit.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save audit log: %w", err)
	}

	return nil
}

// GetTaskExecution retrieves a task execution by workflow ID
func (c *Client) GetTaskExecution(ctx context.Context, workflowID string) (*TaskExecution, error) {
	var task TaskExecution

	query := `
		SELECT id, workflow_id, user_id, session_id, query, mode, status,
			started_at, completed_at, result, error,
			created_at
		FROM tasks
		WHERE workflow_id = $1`

	row, err := c.db.QueryRowContextCB(ctx, query, workflowID)
	if err != nil {
		return &task, err
	}

	err = row.Scan(
		&task.ID, &task.WorkflowID, &task.UserID, &task.SessionID,
		&task.Query, &task.Mode, &task.Status,
		&task.StartedAt, &task.CompletedAt, &task.Result, &task.ErrorMessage,
		&task.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task execution: %w", err)
	}

	return &task, nil
}
