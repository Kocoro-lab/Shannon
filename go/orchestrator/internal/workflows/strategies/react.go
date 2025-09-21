package strategies

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
)

// ReactWorkflow uses the extracted React pattern for step-by-step problem solving.
// It leverages the Reason-Act-Observe loop with optional reflection.
func ReactWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ReactWorkflow with pattern",
		"query", input.Query,
		"session_id", input.SessionID,
		"version", "v2",
	)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Load configuration
	var config activities.WorkflowConfig
	configActivity := workflow.ExecuteActivity(ctx,
		activities.GetWorkflowConfig,
	)
	if err := configActivity.Get(ctx, &config); err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		config = activities.WorkflowConfig{
			ReactMaxIterations:     10,
			ReactObservationWindow: 3,
		}
	}

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

	// Determine model tier based on query complexity
	modelTier := "medium" // Default for React tasks

	// Configure React pattern
	reactConfig := patterns.ReactConfig{
		MaxIterations:     config.ReactMaxIterations,
		ObservationWindow: config.ReactObservationWindow,
		MaxObservations:   100, // Safety limit
		MaxThoughts:       50,  // Safety limit
		MaxActions:        50,  // Safety limit
	}

	reactOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		UserID:         input.UserID,
		EmitEvents:     true,
		ModelTier:      modelTier,
		Context:        baseContext,
	}

	// Execute React loop
	logger.Info("Executing React loop pattern",
		"max_iterations", reactConfig.MaxIterations,
		"observation_window", reactConfig.ObservationWindow,
	)

	reactResult, err := patterns.ReactLoop(
		ctx,
		input.Query,
		baseContext,
		input.SessionID,
		convertHistoryForAgent(input.History),
		reactConfig,
		reactOpts,
	)

	if err != nil {
		logger.Error("React loop failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("React loop failed: %v", err),
		}, err
	}

	// Optional: Apply reflection for quality improvement on complex results
	finalResult := reactResult.FinalResult
	qualityScore := 0.5
	totalTokens := reactResult.TotalTokens

	if reactResult.Iterations > 5 { // Complex task that needed many iterations
		logger.Info("Applying reflection for quality improvement",
			"iterations", reactResult.Iterations,
		)

		reflectionConfig := patterns.ReflectionConfig{
			Enabled:             true,
			MaxRetries:          1, // Single reflection pass for React
			ConfidenceThreshold: 0.7,
			Criteria:            []string{"completeness", "correctness", "clarity"},
			TimeoutMs:           30000,
		}

		reflectionOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			ModelTier:      modelTier,
		}

		// Convert React result to agent result for reflection
		agentResults := []activities.AgentExecutionResult{
			{
				AgentID:    "react-agent",
				Response:   reactResult.FinalResult,
				TokensUsed: reactResult.TotalTokens,
				Success:    true,
				ModelUsed:  modelTier,
			},
		}

		improvedResult, score, reflectionTokens, err := patterns.ReflectOnResult(
			ctx,
			input.Query,
			reactResult.FinalResult,
			agentResults,
			baseContext,
			reflectionConfig,
			reflectionOpts,
		)

		if err == nil {
			finalResult = improvedResult
			qualityScore = score
			totalTokens += reflectionTokens
			logger.Info("Reflection improved quality",
				"score", qualityScore,
				"tokens", reflectionTokens,
			)
		} else {
			logger.Warn("Reflection failed, using original result", "error", err)
		}
	}

	// Update session with results (include per-agent usage for accurate cost)
	if input.SessionID != "" {
		var updRes activities.SessionUpdateResult
		// Build per-agent usage from pattern loop results
		usages := make([]activities.AgentUsage, 0, len(reactResult.AgentResults))
		for _, ar := range reactResult.AgentResults {
			usages = append(usages, activities.AgentUsage{Model: ar.ModelUsed, Tokens: ar.TokensUsed, InputTokens: ar.InputTokens, OutputTokens: ar.OutputTokens})
		}
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: totalTokens,
				AgentsUsed: reactResult.Iterations * 2, // Reasoner + Actor per iteration
				AgentUsage: usages,
			}).Get(ctx, &updRes)

		if err != nil {
			logger.Error("Failed to update session", "error", err)
		}

		// Persist to vector store (fire-and-forget)
		detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
		workflow.ExecuteActivity(detachedCtx,
			activities.RecordQuery,
			activities.RecordQueryInput{
				SessionID: input.SessionID,
				UserID:    input.UserID,
				Query:     input.Query,
				Answer:    finalResult,
				Model:     modelTier,
				Metadata: map[string]interface{}{
					"workflow":      "react_v2",
					"iterations":    reactResult.Iterations,
					"quality_score": qualityScore,
					"thoughts":      len(reactResult.Thoughts),
					"actions":       len(reactResult.Actions),
					"observations":  len(reactResult.Observations),
				},
				RedactPII: true,
			})
	}

	logger.Info("ReactWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"iterations", reactResult.Iterations,
	)

	// Record pattern metrics (fire-and-forget)
	metricsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})
	_ = workflow.ExecuteActivity(metricsCtx, "RecordPatternMetrics", activities.PatternMetricsInput{
		Pattern:      "react",
		Version:      "v2",
		AgentCount:   reactResult.Iterations * 2, // Reasoner + Actor per iteration
		TokensUsed:   totalTokens,
		WorkflowType: "react",
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"version":       "v2",
			"iterations":    reactResult.Iterations,
			"quality_score": qualityScore,
			"thoughts":      len(reactResult.Thoughts),
			"actions":       len(reactResult.Actions),
			"observations":  len(reactResult.Observations),
		},
	}, nil
}
