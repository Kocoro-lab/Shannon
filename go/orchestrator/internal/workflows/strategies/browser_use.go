package strategies

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/control"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
)

// BrowserUseWorkflow implements browser automation using a unified agent loop.
// Unlike ReactWorkflow which separates REASON and ACT into two agent calls,
// BrowserUseWorkflow uses a single agent that reasons and acts together.
//
// This matches modern browser automation approaches (Claude Code, Manus):
// - Single agent per iteration that decides AND executes actions
// - No separate reasoner/actor split that causes duplicate tool calls
// - Screenshots are truncated in context but emitted to UI for visibility
func BrowserUseWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting BrowserUseWorkflow",
		"query", input.Query,
		"session_id", input.SessionID,
	)

	// Determine workflow ID for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Initialize control signal handler
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	controlHandler := &control.SignalHandler{
		WorkflowID:  workflowID,
		AgentID:     "browser_use",
		Logger:      logger,
		EmitCtx:     emitCtx,
		SkipSSEEmit: input.ParentWorkflowID != "",
	}
	controlHandler.Setup(ctx)

	// Prepare base context
	baseContext := make(map[string]interface{})
	for k, v := range input.Context {
		baseContext[k] = v
	}
	for k, v := range input.SessionCtx {
		baseContext[k] = v
	}
	if input.ParentWorkflowID != "" {
		baseContext["parent_workflow_id"] = input.ParentWorkflowID
	}

	// Check for budget configuration
	agentMaxTokens := 0
	if v, ok := baseContext["budget_agent_max"].(int); ok {
		agentMaxTokens = v
	}
	if v, ok := baseContext["budget_agent_max"].(float64); ok && v > 0 {
		agentMaxTokens = int(v)
	}

	// Use medium tier for browser tasks (needs good reasoning + tool use)
	modelTier := "medium"
	approxModel := pricing.GetPriorityOneModel(modelTier)
	if approxModel == "" {
		approxModel = modelTier
	}

	// Configure browser loop
	browserConfig := patterns.BrowserConfig{
		MaxIterations: 15,     // Browser tasks typically need more iterations
		ActionTimeout: 180000, // 3 minutes per action
	}

	browserOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		UserID:         input.UserID,
		EmitEvents:     true,
		ModelTier:      modelTier,
		Context:        baseContext,
	}

	// Check pause/cancel before execution
	if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Execute browser loop
	logger.Info("Executing browser automation loop",
		"max_iterations", browserConfig.MaxIterations,
	)

	// Create checkpoint callback that delegates to control handler
	checkPause := func(wfCtx workflow.Context, checkpoint string) error {
		return controlHandler.CheckPausePoint(wfCtx, checkpoint)
	}

	browserResult, err := patterns.BrowserLoop(
		ctx,
		input.Query,
		baseContext,
		input.SessionID,
		convertHistoryForAgent(input.History),
		browserConfig,
		browserOpts,
		checkPause,
	)

	if err != nil {
		logger.Error("Browser loop failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Browser automation failed: %v", err),
		}, err
	}

	finalResult := browserResult.FinalResult
	totalTokens := browserResult.TotalTokens

	// Update session with results
	if input.SessionID != "" {
		var updRes activities.SessionUpdateResult
		usages := make([]activities.AgentUsage, 0, len(browserResult.AgentResults))
		for _, ar := range browserResult.AgentResults {
			usages = append(usages, activities.AgentUsage{
				Model:        ar.ModelUsed,
				Tokens:       ar.TokensUsed,
				InputTokens:  ar.InputTokens,
				OutputTokens: ar.OutputTokens,
			})
		}
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: totalTokens,
				AgentsUsed: browserResult.Iterations,
				AgentUsage: usages,
			}).Get(ctx, &updRes)

		if err != nil {
			logger.Error("Failed to update session", "error", err)
		}

		// Persist to vector store
		_ = workflow.ExecuteActivity(ctx,
			activities.RecordQuery,
			activities.RecordQueryInput{
				SessionID: input.SessionID,
				UserID:    input.UserID,
				Query:     input.Query,
				Answer:    finalResult,
				Model:     approxModel,
				Metadata: map[string]interface{}{
					"workflow":     "browser_use",
					"iterations":   browserResult.Iterations,
					"actions":      len(browserResult.Actions),
					"observations": len(browserResult.Observations),
					"tenant_id":    input.TenantID,
				},
				RedactPII: true,
			}).Get(ctx, nil)
	}

	logger.Info("BrowserUseWorkflow completed",
		"total_tokens", totalTokens,
		"iterations", browserResult.Iterations,
	)

	// Build metadata
	meta := map[string]interface{}{
		"workflow":     "browser_use",
		"iterations":   browserResult.Iterations,
		"actions":      len(browserResult.Actions),
		"observations": len(browserResult.Observations),
	}

	// Aggregate agent metadata
	agentResults := []activities.AgentExecutionResult{
		{
			AgentID:      "browser-agent",
			ModelUsed:    approxModel,
			TokensUsed:   totalTokens,
			InputTokens:  totalTokens * 6 / 10,
			OutputTokens: totalTokens * 4 / 10,
			Success:      true,
		},
	}
	agentMeta := metadata.AggregateAgentMetadata(agentResults, 0)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Compute estimated cost
	if totalTokens > 0 {
		meta["cost_usd"] = pricing.CostForTokens(approxModel, totalTokens)
	}

	// Emit final output
	if finalResult != "" {
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventLLMOutput,
			AgentID:    "final_output",
			Message:    finalResult,
			Timestamp:  workflow.Now(ctx),
			Payload: map[string]interface{}{
				"tokens_used": totalTokens,
				"model_used":  approxModel,
			},
		}).Get(ctx, nil)
	}

	// Emit workflow completed
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "browser_use",
		Message:    activities.MsgWorkflowCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}
