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

	// Memory retrieval with gate precedence (hierarchical > simple session)
	hierarchicalVersion := workflow.GetVersion(ctx, "memory_retrieval_v1", workflow.DefaultVersion, 1)
	sessionVersion := workflow.GetVersion(ctx, "session_memory_v1", workflow.DefaultVersion, 1)

	if hierarchicalVersion >= 1 && input.SessionID != "" {
		// Use hierarchical memory (combines recent + semantic)
		var hierMemory activities.FetchHierarchicalMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchHierarchicalMemory,
			activities.FetchHierarchicalMemoryInput{
				Query:        input.Query,
				SessionID:    input.SessionID,
				TenantID:     input.TenantID,
				RecentTopK:   5,   // Fixed for determinism
				SemanticTopK: 5,   // Fixed for determinism
				Threshold:    0.75, // Fixed semantic threshold
			}).Get(ctx, &hierMemory)

		if len(hierMemory.Items) > 0 {
			baseContext["agent_memory"] = hierMemory.Items
			logger.Info("Injected hierarchical memory into React context",
				"session_id", input.SessionID,
				"memory_items", len(hierMemory.Items),
				"sources", hierMemory.Sources,
			)
		}
	} else if sessionVersion >= 1 && input.SessionID != "" {
		// Fallback to simple session memory if hierarchical not enabled
		var sessionMemory activities.FetchSessionMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchSessionMemory,
			activities.FetchSessionMemoryInput{
				SessionID: input.SessionID,
				TenantID:  input.TenantID,
				TopK:      20, // Fixed for determinism
			}).Get(ctx, &sessionMemory)

		if len(sessionMemory.Items) > 0 {
			baseContext["agent_memory"] = sessionMemory.Items
			logger.Info("Injected session memory into React context",
				"session_id", input.SessionID,
				"memory_items", len(sessionMemory.Items),
			)
		}
	}

	// Context compression (version-gated for determinism)
	compressionVersion := workflow.GetVersion(ctx, "context_compress_v1", workflow.DefaultVersion, 1)
	if compressionVersion >= 1 && input.SessionID != "" && len(input.History) > 20 {
		// Check if compression is needed with rate limiting
		estimatedTokens := activities.EstimateTokens(convertHistoryForAgent(input.History))
		modelTier := "medium" // Default for React tasks

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       modelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in React workflow",
				"session_id", input.SessionID,
				"reason", checkResult.Reason,
				"message_count", len(input.History),
			)

			// Compress context via activity
			var compressResult activities.CompressContextResult
			err = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext,
				activities.CompressContextInput{
					SessionID:    input.SessionID,
					History:      convertHistoryMapForCompression(input.History),
					TargetTokens: int(float64(activities.GetModelWindowSize(modelTier)) * 0.375),
					ParentWorkflowID: input.ParentWorkflowID,
				}).Get(ctx, &compressResult)

			if err == nil && compressResult.Summary != "" && compressResult.Stored {
				logger.Info("Context compressed and stored",
					"session_id", input.SessionID,
					"summary_length", len(compressResult.Summary),
				)

				// Update compression state in session
				var updateResult activities.UpdateCompressionStateResult
				_ = workflow.ExecuteActivity(ctx, "UpdateCompressionStateActivity",
					activities.UpdateCompressionStateInput{
						SessionID:    input.SessionID,
						MessageCount: len(input.History),
					}).Get(ctx, &updateResult)
			}
		}
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
					"tenant_id":     input.TenantID,
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

    // Aggregate tool errors from React agent results
    var toolErrors []map[string]string
    for _, ar := range reactResult.AgentResults {
        if len(ar.ToolExecutions) == 0 { continue }
        for _, te := range ar.ToolExecutions {
            if !te.Success || (te.Error != "") {
                toolErrors = append(toolErrors, map[string]string{
                    "agent_id": ar.AgentID,
                    "tool":     te.Tool,
                    "error":    te.Error,
                })
            }
        }
    }

    meta := map[string]interface{}{
        "version":       "v2",
        "iterations":    reactResult.Iterations,
        "quality_score": qualityScore,
        "thoughts":      len(reactResult.Thoughts),
        "actions":       len(reactResult.Actions),
        "observations":  len(reactResult.Observations),
    }
    if len(toolErrors) > 0 { meta["tool_errors"] = toolErrors }

    return TaskResult{
        Result:     finalResult,
        Success:    true,
        TokensUsed: totalTokens,
        Metadata:   meta,
    }, nil
}
