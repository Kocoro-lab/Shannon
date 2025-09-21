package workflows

import (
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// SimpleTaskWorkflow handles simple, single-agent tasks efficiently
// This workflow minimizes events by using a single consolidated activity
func SimpleTaskWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting SimpleTaskWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
		"session_id", input.SessionID,
	)

	// Determine workflow ID for event streaming
	// Use parent workflow ID if this is a child workflow, otherwise use own ID
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Emit workflow started event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "simple-agent",
		Message:    "Starting simple task workflow",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Emit thinking event
	truncatedQuery := input.Query
	if len(truncatedQuery) > 80 {
		truncatedQuery = truncatedQuery[:77] + "..."
	}
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentThinking,
		AgentID:    "simple-agent",
		Message:    "Analyzing: " + truncatedQuery,
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute, // Simple tasks should be fast
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2, // Fewer retries for simple tasks
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Emit agent started event
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentStarted,
		AgentID:    "simple-agent",
		Message:    "Processing query",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Execute the consolidated simple task activity
	// This single activity handles everything: agent execution, session update, etc.
	var result activities.ExecuteSimpleTaskResult
	err := workflow.ExecuteActivity(ctx, activities.ExecuteSimpleTask, activities.ExecuteSimpleTaskInput{
		Query:          input.Query,
		UserID:         input.UserID,
		SessionID:      input.SessionID,
		Context:        input.Context,
		SessionCtx:     input.SessionCtx,
		History:        convertHistoryForAgent(input.History),
		SuggestedTools: input.SuggestedTools,
		ToolParameters: input.ToolParameters,
	}).Get(ctx, &result)

	if err != nil {
		logger.Error("Simple task execution failed", "error", err)
		// Emit error event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventErrorOccurred,
			AgentID:    "simple-agent",
			Message:    "Task execution failed: " + err.Error(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Persist to vector store for future context retrieval (fire-and-forget)
	if input.SessionID != "" {
		ro := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3}}
		detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
		dctx := workflow.WithActivityOptions(detachedCtx, ro)
		// Schedule without waiting on result
		workflow.ExecuteActivity(dctx, activities.RecordQuery, activities.RecordQueryInput{
			SessionID: input.SessionID,
			UserID:    input.UserID,
			Query:     input.Query,
			Answer:    result.Response,
			Model:     "simple-agent",
			Metadata:  map[string]interface{}{"workflow": "simple", "mode": "simple", "tenant_id": input.TenantID},
			RedactPII: true,
		})
	}

	// Update session with token usage
	if input.SessionID != "" {
		var sessionUpdateResult activities.SessionUpdateResult
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     result.Response,
				TokensUsed: result.TokensUsed,
				AgentsUsed: 1,
				ModelUsed:  result.ModelUsed,
			},
		).Get(ctx, &sessionUpdateResult)
		if err != nil {
			logger.Warn("Failed to update session with tokens",
				"session_id", input.SessionID,
				"error", err,
			)
		}
	}

	// Check if we need synthesis for web_search or JSON results
	finalResult := result.Response
	totalTokens := result.TokensUsed

	// Determine if synthesis is needed
	needsSynthesis := false

	// Check if web_search was used
	if input.SuggestedTools != nil {
		for _, tool := range input.SuggestedTools {
			if strings.EqualFold(tool, "web_search") {
				needsSynthesis = true
				break
			}
		}
	}

	// Check if response looks like JSON
	if !needsSynthesis {
		trimmed := strings.TrimSpace(result.Response)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			needsSynthesis = true
		}
	}

	// Perform synthesis if needed
	if needsSynthesis && result.Success {
		logger.Info("Response appears to be web_search results or JSON, performing synthesis")

		// Convert to agent results format for synthesis
		agentResults := []activities.AgentExecutionResult{
			{
				AgentID:    "simple-agent",
				Response:   result.Response,
				Success:    true,
				TokensUsed: result.TokensUsed,
			},
		}

		var synthesis activities.SynthesisResult
		err = workflow.ExecuteActivity(ctx,
			activities.SynthesizeResultsLLM,
			activities.SynthesisInput{
				Query:        input.Query,
				AgentResults: agentResults,
				Context:      input.Context,
			},
		).Get(ctx, &synthesis)

		if err != nil {
			logger.Warn("Synthesis failed, using raw result", "error", err)
		} else {
			finalResult = synthesis.FinalResult
			totalTokens += synthesis.TokensUsed
			logger.Info("Synthesis completed", "additional_tokens", synthesis.TokensUsed)
		}
	}

	logger.Info("SimpleTaskWorkflow completed successfully",
		"tokens_used", totalTokens,
	)

	// Emit completion event
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentCompleted,
		AgentID:    "simple-agent",
		Message:    "Task completed successfully",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Emit workflow completed event for dashboards
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "simple-agent",
		Message:    "Workflow completed",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"mode":       "simple",
			"num_agents": 1,
		},
	}, nil
}
