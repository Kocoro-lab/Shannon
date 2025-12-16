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

	// Insert or update canonical record in task_executions
	var userID interface{}
	if task.UserID != nil {
		userID = task.UserID
	} else {
		userID = nil
	}
	var tenantID interface{}
	if task.TenantID != nil {
		tenantID = task.TenantID
	} else {
		tenantID = nil
	}
	sessionID := task.SessionID // VARCHAR in task_executions

	// Ensure metadata JSONB is not nil
	var metadata JSONB
	if task.Metadata != nil {
		metadata = task.Metadata
	} else {
		metadata = JSONB{}
	}

	// Default trigger_type to 'api' if not specified
	triggerType := task.TriggerType
	if triggerType == "" {
		triggerType = "api"
	}

	teQuery := `
        INSERT INTO task_executions (
            id, workflow_id, user_id, session_id, tenant_id,
            query, mode, status,
            started_at, completed_at,
            result, error_message,
            model_used, provider,
            total_tokens, prompt_tokens, completion_tokens, total_cost_usd,
            duration_ms, agents_used, tools_invoked, cache_hits,
            complexity_score, metadata, created_at,
            trigger_type, schedule_id
        ) VALUES (
            $1, $2, $3, $4, $5,
            $6, $7, $8,
            $9, $10,
            $11, $12,
            $13, $14,
            $15, $16, $17, $18,
            $19, $20, $21, $22,
            $23, $24, $25,
            $26, $27
        )
        ON CONFLICT (workflow_id) DO UPDATE SET
            status = EXCLUDED.status,
            completed_at = EXCLUDED.completed_at,
            result = EXCLUDED.result,
            error_message = EXCLUDED.error_message,
            model_used = EXCLUDED.model_used,
            provider = EXCLUDED.provider,
            total_tokens = EXCLUDED.total_tokens,
            prompt_tokens = EXCLUDED.prompt_tokens,
            completion_tokens = EXCLUDED.completion_tokens,
            total_cost_usd = EXCLUDED.total_cost_usd,
            duration_ms = EXCLUDED.duration_ms,
            agents_used = EXCLUDED.agents_used,
            tools_invoked = EXCLUDED.tools_invoked,
            cache_hits = EXCLUDED.cache_hits,
            complexity_score = EXCLUDED.complexity_score,
            metadata = EXCLUDED.metadata,
            tenant_id = COALESCE(EXCLUDED.tenant_id, task_executions.tenant_id)
        RETURNING id`

	err := c.db.QueryRowContext(ctx, teQuery,
		task.ID, task.WorkflowID, userID, sessionID, tenantID,
		task.Query, task.Mode, task.Status,
		task.StartedAt, task.CompletedAt,
		task.Result, task.ErrorMessage,
		task.ModelUsed, task.Provider,
		task.TotalTokens, task.PromptTokens, task.CompletionTokens, task.TotalCostUSD,
		task.DurationMs, task.AgentsUsed, task.ToolsInvoked, task.CacheHits,
		task.ComplexityScore, metadata, task.CreatedAt,
		triggerType, task.ScheduleID,
	).Scan(&task.ID)
	if err != nil {
		return fmt.Errorf("failed to save task execution: %w", err)
	}

	c.logger.Debug("Task execution saved (task_executions)",
		zap.String("workflow_id", task.WorkflowID),
		zap.String("status", task.Status))
	return nil
}

// BatchSaveTaskExecutions saves multiple task executions in a single transaction
func (c *Client) BatchSaveTaskExecutions(ctx context.Context, tasks []*TaskExecution) error {
	if len(tasks) == 0 {
		return nil
	}

	return c.WithTransactionCB(ctx, func(tx *circuitbreaker.TxWrapper) error {
		stmt, err := tx.PrepareContext(ctx, `
            INSERT INTO task_executions (
                id, workflow_id, user_id, session_id, tenant_id,
                query, mode, status,
                started_at, completed_at,
                result, error_message,
                model_used, provider,
                total_tokens, prompt_tokens, completion_tokens, total_cost_usd,
                duration_ms, agents_used, tools_invoked, cache_hits,
                complexity_score, metadata, created_at,
                trigger_type, schedule_id
            ) VALUES (
                $1, $2, $3, $4, $5,
                $6, $7, $8,
                $9, $10,
                $11, $12,
                $13, $14,
                $15, $16, $17, $18,
                $19, $20, $21, $22,
                $23, $24, $25,
                $26, $27
            )
            ON CONFLICT (workflow_id) DO UPDATE SET
                status = EXCLUDED.status,
                completed_at = EXCLUDED.completed_at,
                result = EXCLUDED.result,
                error_message = EXCLUDED.error_message,
                model_used = EXCLUDED.model_used,
                provider = EXCLUDED.provider,
                total_tokens = EXCLUDED.total_tokens,
                prompt_tokens = EXCLUDED.prompt_tokens,
                completion_tokens = EXCLUDED.completion_tokens,
                total_cost_usd = EXCLUDED.total_cost_usd,
                duration_ms = EXCLUDED.duration_ms,
                agents_used = EXCLUDED.agents_used,
                tools_invoked = EXCLUDED.tools_invoked,
                cache_hits = EXCLUDED.cache_hits,
                complexity_score = EXCLUDED.complexity_score,
                metadata = EXCLUDED.metadata,
                tenant_id = COALESCE(EXCLUDED.tenant_id, task_executions.tenant_id)
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

			// Prepare args for task_executions
			var userID interface{}
			if task.UserID != nil {
				userID = task.UserID
			} else {
				userID = nil
			}
			sessionID := task.SessionID
			var tenantID interface{}
			if task.TenantID != nil {
				tenantID = task.TenantID
			} else {
				tenantID = nil
			}
			var metadata JSONB
			if task.Metadata != nil {
				metadata = task.Metadata
			} else {
				metadata = JSONB{}
			}

			// Default trigger_type to 'api' if not specified
			triggerType := task.TriggerType
			if triggerType == "" {
				triggerType = "api"
			}

			_, err := stmt.ExecContext(ctx,
				task.ID, task.WorkflowID, userID, sessionID, tenantID,
				task.Query, task.Mode, task.Status,
				task.StartedAt, task.CompletedAt,
				task.Result, task.ErrorMessage,
				task.ModelUsed, task.Provider,
				task.TotalTokens, task.PromptTokens, task.CompletionTokens, task.TotalCostUSD,
				task.DurationMs, task.AgentsUsed, task.ToolsInvoked, task.CacheHits,
				task.ComplexityScore, metadata, task.CreatedAt,
				triggerType, task.ScheduleID,
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
	// Parse tenant ID once for reuse
	var tid *uuid.UUID
	if tenantID != "" {
		if parsed, err := uuid.Parse(tenantID); err == nil {
			tid = &parsed
		}
	}

	// Parse user ID - if not a valid UUID, we'll need to look it up or create a user
	var uid *uuid.UUID
	if userID != "" {
		parsed, err := uuid.Parse(userID)
		if err == nil {
			uid = &parsed
			_, execErr := c.db.ExecContext(ctx,
				"INSERT INTO users (id, external_id, tenant_id, auth_user_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (id) DO UPDATE SET tenant_id = COALESCE(users.tenant_id, EXCLUDED.tenant_id), auth_user_id = COALESCE(users.auth_user_id, EXCLUDED.auth_user_id), updated_at = GREATEST(users.updated_at, EXCLUDED.updated_at)",
				parsed, parsed.String(), tid, parsed, time.Now(), time.Now(),
			)
			if execErr != nil {
				c.logger.Warn("Failed to upsert user by id",
					zap.String("user_id", userID),
					zap.Error(execErr))
				uid = nil
			}
		} else {
			// User ID is not a UUID, try to find or create user by external_id
			userUUID := uuid.New()
			if err := c.db.QueryRowContext(ctx,
				"INSERT INTO users (id, external_id, tenant_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (external_id) DO UPDATE SET tenant_id = COALESCE(users.tenant_id, EXCLUDED.tenant_id), updated_at = GREATEST(users.updated_at, EXCLUDED.updated_at) RETURNING id",
				userUUID, userID, tid, time.Now(), time.Now(),
			).Scan(&userUUID); err != nil {
				c.logger.Warn("Failed to upsert user by external_id",
					zap.String("external_id", userID),
					zap.Error(err))
				uid = nil
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

	// Support non-UUID external session IDs by mapping them to an internal UUID
	var sessionUUID uuid.UUID
	var contextMap = map[string]interface{}{"created_from": "orchestrator"}
	if parsed, err := uuid.Parse(sessionID); err == nil {
		sessionUUID = parsed
	} else {
		// Generate an internal UUID and store external_id in context for lookup
		sessionUUID = uuid.New()
		contextMap["external_id"] = sessionID
	}

	now := time.Now()
	_, err := c.db.ExecContext(ctx, query,
		sessionUUID,
		uid,
		tid,
		JSONB(contextMap),
		10000,
		0,
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
        SELECT id, workflow_id, user_id, session_id, tenant_id, query, mode, status,
            started_at, completed_at, result, error_message,
            created_at, trigger_type, schedule_id
        FROM task_executions
        WHERE workflow_id = $1`

	row, err := c.db.QueryRowContextCB(ctx, query, workflowID)
	if err != nil {
		return &task, err
	}

	err = row.Scan(
		&task.ID, &task.WorkflowID, &task.UserID, &task.SessionID, &task.TenantID,
		&task.Query, &task.Mode, &task.Status,
		&task.StartedAt, &task.CompletedAt, &task.Result, &task.ErrorMessage,
		&task.CreatedAt, &task.TriggerType, &task.ScheduleID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task execution: %w", err)
	}

	return &task, nil
}

// UpdateTaskStatus updates the status of a task execution
func (c *Client) UpdateTaskStatus(ctx context.Context, workflowID string, status string) error {
	_, err := c.db.ExecContext(ctx,
		`UPDATE task_executions SET status = $1 WHERE workflow_id = $2`,
		status, workflowID)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	return nil
}
