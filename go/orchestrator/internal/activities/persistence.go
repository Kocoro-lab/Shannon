package activities

import (
	"context"
	"database/sql"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// PersistenceActivities handles database persistence for agent and tool executions
type PersistenceActivities struct {
	dbClient *db.Client
	logger   *zap.Logger
}

// NewPersistenceActivities creates a new persistence activities instance
func NewPersistenceActivities(dbClient *db.Client, logger *zap.Logger) *PersistenceActivities {
	return &PersistenceActivities{
		dbClient: dbClient,
		logger:   logger,
	}
}

// PersistAgentExecutionInput contains data to persist for an agent execution
type PersistAgentExecutionInput struct {
	ID         string                 `json:"id,omitempty"` // Optional pre-generated ID for correlation with tool executions
	WorkflowID string                 `json:"workflow_id"`
	AgentID    string                 `json:"agent_id"`
	Input      string                 `json:"input"`
	Output     string                 `json:"output"`
	State      string                 `json:"state"`
	TokensUsed int                    `json:"tokens_used"`
	ModelUsed  string                 `json:"model_used"`
	DurationMs int64                  `json:"duration_ms"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// PersistAgentExecutionStandalone is a standalone function that persists agent execution
// It uses the global dbClient if available
func PersistAgentExecutionStandalone(ctx context.Context, input PersistAgentExecutionInput) error {
	zap.L().Info("PersistAgentExecutionStandalone called", zap.String("workflow_id", input.WorkflowID))
	dbClient := GetGlobalDBClient()
	if dbClient == nil {
		// Log when no dbClient available
		zap.L().Warn("No global dbClient available, skipping agent execution persistence")
		return nil
	}
	zap.L().Info("Got dbClient, proceeding with persistence")

	activities := &PersistenceActivities{
		dbClient: dbClient,
		logger:   zap.L(),
	}
	return activities.PersistAgentExecution(ctx, input)
}

// PersistToolExecutionStandalone is a standalone function that persists tool execution
// It uses the global dbClient if available
func PersistToolExecutionStandalone(ctx context.Context, input PersistToolExecutionInput) error {
	dbClient := GetGlobalDBClient()
	if dbClient == nil {
		// Log when no dbClient available
		zap.L().Warn("No global dbClient available, skipping tool execution persistence")
		return nil
	}

	activities := &PersistenceActivities{
		dbClient: dbClient,
		logger:   zap.L(),
	}
	return activities.PersistToolExecution(ctx, input)
}

// PersistAgentExecution persists an agent execution to the database
func (p *PersistenceActivities) PersistAgentExecution(ctx context.Context, input PersistAgentExecutionInput) error {
	// Use zap.L() for logging since this might be called from goroutines
	logger := zap.L()
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	logger.Info("Persisting agent execution",
		zap.String("workflow_id", input.WorkflowID),
		zap.String("agent_id", input.AgentID),
	)

	// Ensure metadata map exists and enrich with session/user if available via DB
	if input.Metadata == nil {
		input.Metadata = make(map[string]interface{})
	}

	// Attempt to look up session_id and user_id via task_executions for this workflow
	if p.dbClient != nil {
		if sqlDB := p.dbClient.GetDB(); sqlDB != nil {
			var sessID, userUUID sql.NullString
			_ = sqlDB.QueryRowContext(ctx,
				"SELECT session_id::text, user_id::text FROM task_executions WHERE workflow_id = $1 ORDER BY created_at DESC LIMIT 1",
				input.WorkflowID,
			).Scan(&sessID, &userUUID)
			if sessID.Valid && sessID.String != "" {
				if _, ok := input.Metadata["session_id"]; !ok {
					input.Metadata["session_id"] = sessID.String
				}
			}
			if userUUID.Valid && userUUID.String != "" {
				if _, ok := input.Metadata["user_id"]; !ok {
					input.Metadata["user_id"] = userUUID.String
				}
			}
		}
	}

	// Use pre-generated ID if provided, otherwise generate new one
	agentID := input.ID
	if agentID == "" {
		agentID = uuid.New().String()
	}

	agentExec := &db.AgentExecution{
		ID:           agentID,
		WorkflowID:   input.WorkflowID,
		TaskID:       input.WorkflowID, // Use workflow ID as task ID
		AgentID:      input.AgentID,
		Input:        input.Input,
		Output:       input.Output,
		State:        input.State,
		TokensUsed:   input.TokensUsed,
		ModelUsed:    input.ModelUsed,
		DurationMs:   input.DurationMs,
		ErrorMessage: input.Error,
		Metadata:     input.Metadata,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Queue the write
	err := p.dbClient.QueueWrite(db.WriteTypeAgentExecution, agentExec, func(writeErr error) {
		if writeErr != nil {
			p.logger.Error("Failed to persist agent execution",
				zap.String("workflow_id", input.WorkflowID),
				zap.String("agent_id", input.AgentID),
				zap.Error(writeErr),
			)
		} else {
			p.logger.Debug("Agent execution persisted",
				zap.String("workflow_id", input.WorkflowID),
				zap.String("agent_id", input.AgentID),
			)
		}
	})

	if err != nil {
		p.logger.Error("Failed to queue agent execution write",
			zap.String("workflow_id", input.WorkflowID),
			zap.String("agent_id", input.AgentID),
			zap.Error(err),
		)
		// Don't fail the activity - persistence is non-critical
	}

	return nil
}

// PersistToolExecutionInput contains data to persist for a tool execution
type PersistToolExecutionInput struct {
	WorkflowID       string                 `json:"workflow_id"`
	AgentID          string                 `json:"agent_id"`
	AgentExecutionID string                 `json:"agent_execution_id,omitempty"` // Links to agent_executions.id
	ToolName         string                 `json:"tool_name"`
	InputParams      map[string]interface{} `json:"input_params"`
	Output           string                 `json:"output"`
	Success          bool                   `json:"success"`
	TokensConsumed   int                    `json:"tokens_consumed"`
	DurationMs       int64                  `json:"duration_ms"`
	Error            string                 `json:"error,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// PersistToolExecution persists a tool execution to the database
func (p *PersistenceActivities) PersistToolExecution(ctx context.Context, input PersistToolExecutionInput) error {
	// Use zap.L() for logging since this might be called from goroutines
	logger := zap.L()
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	logger.Info("Persisting tool execution",
		zap.String("workflow_id", input.WorkflowID),
		zap.String("agent_id", input.AgentID),
		zap.String("tool_name", input.ToolName),
	)

	// Set AgentExecutionID if provided
	var agentExecID *string
	if input.AgentExecutionID != "" {
		agentExecID = &input.AgentExecutionID
	}

	toolExec := &db.ToolExecution{
		ID:               uuid.New().String(),
		WorkflowID:       input.WorkflowID,
		AgentID:          input.AgentID,
		AgentExecutionID: agentExecID,
		ToolName:         input.ToolName,
		InputParams:      input.InputParams,
		Output:           input.Output,
		Success:          input.Success,
		TokensConsumed:   input.TokensConsumed,
		DurationMs:       input.DurationMs,
		Error:            input.Error,
		Metadata:         input.Metadata,
		CreatedAt:        time.Now(),
	}

	// Queue the write
	err := p.dbClient.QueueWrite(db.WriteTypeToolExecution, toolExec, func(writeErr error) {
		if writeErr != nil {
			p.logger.Error("Failed to persist tool execution",
				zap.String("workflow_id", input.WorkflowID),
				zap.String("agent_id", input.AgentID),
				zap.String("tool_name", input.ToolName),
				zap.Error(writeErr),
			)
		} else {
			p.logger.Debug("Tool execution persisted",
				zap.String("workflow_id", input.WorkflowID),
				zap.String("agent_id", input.AgentID),
				zap.String("tool_name", input.ToolName),
			)
		}
	})

	if err != nil {
		p.logger.Error("Failed to queue tool execution write",
			zap.String("workflow_id", input.WorkflowID),
			zap.String("agent_id", input.AgentID),
			zap.String("tool_name", input.ToolName),
			zap.Error(err),
		)
		// Don't fail the activity - persistence is non-critical
	}

	return nil
}
