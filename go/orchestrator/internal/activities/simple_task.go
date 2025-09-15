package activities

import (
	"context"

	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// ExecuteSimpleTaskInput contains everything needed for a simple task
type ExecuteSimpleTaskInput struct {
	Query          string                 `json:"query"`
	UserID         string                 `json:"user_id"`
	SessionID      string                 `json:"session_id"`
	Context        map[string]interface{} `json:"context"`
	SessionCtx     map[string]interface{} `json:"session_ctx"`
	History        []string               `json:"history"`
	PersonaID      string                 `json:"persona_id"`
	SuggestedTools []string               `json:"suggested_tools,omitempty"`
	ToolParameters map[string]interface{} `json:"tool_parameters,omitempty"`
}

// ExecuteSimpleTaskResult contains the complete result
type ExecuteSimpleTaskResult struct {
	Response   string `json:"response"`
	TokensUsed int    `json:"tokens_used"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// ExecuteSimpleTask executes a simple query with minimal overhead
// This consolidated activity merges context and executes the agent in one step.
// Session updates and vector persistence are handled by the workflow after this activity completes.
func ExecuteSimpleTask(ctx context.Context, input ExecuteSimpleTaskInput) (ExecuteSimpleTaskResult, error) {
	// Use activity logger for proper Temporal correlation
	activity.GetLogger(ctx).Info("ExecuteSimpleTask activity started",
		"query", input.Query,
		"session_id", input.SessionID,
	)
	// Use zap logger for the core logic which needs *zap.Logger
	logger := zap.L()
	if logger == nil {
		// Fallback to creating a new logger if global logger is not initialized
		logger, _ = zap.NewProduction()
	}

	// Step 1: Merge context (no separate activity)
	mergedContext := make(map[string]interface{})
	for k, v := range input.Context {
		mergedContext[k] = v
	}
	for k, v := range input.SessionCtx {
		mergedContext[k] = v
	}

	// Step 2: Execute agent using shared helper (not calling activity directly)
	agentResult, err := executeAgentCore(ctx, AgentExecutionInput{
		Query:          input.Query,
		AgentID:        "simple-agent",
		Context:        mergedContext,
		Mode:           "simple",
		SessionID:      input.SessionID,
		History:        input.History,
		PersonaID:      input.PersonaID,
		SuggestedTools: input.SuggestedTools,
		ToolParameters: input.ToolParameters,
	}, logger)

	if err != nil {
		logger.Error("Agent execution failed", zap.Error(err))
		return ExecuteSimpleTaskResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Return the result - session update and vector persistence
	// are handled by the workflow to maintain separation of concerns
	return ExecuteSimpleTaskResult{
		Response:   agentResult.Response,
		TokensUsed: agentResult.TokensUsed,
		Success:    true,
	}, nil
}