package strategies

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

// ResearchWorkflow demonstrates composed patterns for complex research tasks.
// It combines React loops, parallel research, and reflection patterns.
func ResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ResearchWorkflow with composed patterns",
		"query", input.Query,
		"session_id", input.SessionID,
		"version", "v2",
	)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Prepare base context (merge input.Context + SessionCtx)
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

	// Step 1: Decompose the research query
	var decomp activities.DecompositionResult
	err := workflow.ExecuteActivity(ctx,
		constants.DecomposeTaskActivity,
		activities.DecompositionInput{
			Query:          input.Query,
			Context:        baseContext,
			AvailableTools: []string{},
		}).Get(ctx, &decomp)

	if err != nil {
		logger.Error("Task decomposition failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to decompose task: %v", err),
		}, err
	}

	// Check for budget configuration
	agentMaxTokens := 0
	if v, ok := baseContext["budget_agent_max"].(int); ok {
		agentMaxTokens = v
	}
	if v, ok := baseContext["budget_agent_max"].(float64); ok && v > 0 {
		agentMaxTokens = int(v)
	}

	modelTier := determineModelTier(baseContext, "medium")
	var totalTokens int
	var agentResults []activities.AgentExecutionResult

	// Step 2: Execute based on complexity
	if decomp.ComplexityScore < 0.5 || len(decomp.Subtasks) <= 1 {
		// Simple research - use React pattern for step-by-step exploration
		logger.Info("Using React pattern for simple research",
			"complexity", decomp.ComplexityScore,
		)

		reactConfig := patterns.ReactConfig{
			MaxIterations:     5,
			ObservationWindow: 3,
			MaxObservations:   20,
			MaxThoughts:       10,
			MaxActions:        10,
		}

		reactOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			EmitEvents:     true,
			ModelTier:      modelTier,
			Context:        baseContext,
		}

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
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("React loop failed: %v", err),
			}, err
		}

		// Convert React result to agent result for synthesis
		agentResults = append(agentResults, activities.AgentExecutionResult{
			AgentID:    "react-researcher",
			Response:   reactResult.FinalResult,
			TokensUsed: reactResult.TotalTokens,
			Success:    true,
			ModelUsed:  modelTier,
		})
		totalTokens = reactResult.TotalTokens

	} else {
		// Complex research - use parallel/hybrid execution
		logger.Info("Using parallel execution for complex research",
			"complexity", decomp.ComplexityScore,
			"subtasks", len(decomp.Subtasks),
		)

		// Determine execution strategy
		hasDepencies := false
		for _, subtask := range decomp.Subtasks {
			if len(subtask.Dependencies) > 0 {
				hasDepencies = true
				break
			}
		}

		if hasDepencies {
			// Use hybrid execution for dependency management
			logger.Info("Using hybrid execution due to dependencies")

			hybridTasks := make([]execution.HybridTask, len(decomp.Subtasks))
			for i, subtask := range decomp.Subtasks {
				role := "researcher"
				if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
					role = decomp.AgentTypes[i]
				}

				hybridTasks[i] = execution.HybridTask{
					ID:             subtask.ID,
					Description:    subtask.Description,
					SuggestedTools: subtask.SuggestedTools,
					ToolParameters: subtask.ToolParameters,
					PersonaID:      subtask.SuggestedPersona,
					Role:           role,
					Dependencies:   subtask.Dependencies,
				}
			}

			hybridConfig := execution.HybridConfig{
				MaxConcurrency:           5,
				EmitEvents:               true,
				Context:                  baseContext,
				DependencyWaitTimeout:    6 * time.Minute,
				PassDependencyResults:    true,
				ClearDependentToolParams: true,
			}

			hybridResult, err := execution.ExecuteHybrid(
				ctx,
				hybridTasks,
				input.SessionID,
				convertHistoryForAgent(input.History),
				hybridConfig,
				agentMaxTokens,
				input.UserID,
				modelTier,
			)

			if err != nil {
				return TaskResult{
					Success:      false,
					ErrorMessage: fmt.Sprintf("Hybrid execution failed: %v", err),
				}, err
			}

			// Convert results to agent results
			for _, result := range hybridResult.Results {
				agentResults = append(agentResults, result)
			}
			totalTokens = hybridResult.TotalTokens

		} else {
			// Use pure parallel execution
			logger.Info("Using pure parallel execution")

			parallelTasks := make([]execution.ParallelTask, len(decomp.Subtasks))
			for i, subtask := range decomp.Subtasks {
				role := "researcher"
				if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
					role = decomp.AgentTypes[i]
				}

				parallelTasks[i] = execution.ParallelTask{
					ID:             subtask.ID,
					Description:    subtask.Description,
					SuggestedTools: subtask.SuggestedTools,
					ToolParameters: subtask.ToolParameters,
					PersonaID:      subtask.SuggestedPersona,
					Role:           role,
				}
			}

			parallelConfig := execution.ParallelConfig{
				MaxConcurrency: 5,
				EmitEvents:     true,
				Context:        baseContext,
			}

			parallelResult, err := execution.ExecuteParallel(
				ctx,
				parallelTasks,
				input.SessionID,
				convertHistoryForAgent(input.History),
				parallelConfig,
				agentMaxTokens,
				input.UserID,
				modelTier,
			)

			if err != nil {
				return TaskResult{
					Success:      false,
					ErrorMessage: fmt.Sprintf("Parallel execution failed: %v", err),
				}, err
			}

			agentResults = parallelResult.Results
			totalTokens = parallelResult.TotalTokens
		}
	}

	// Step 3: Synthesize results
	logger.Info("Synthesizing research results",
		"agent_count", len(agentResults),
	)

	var synthesis activities.SynthesisResult
	err = workflow.ExecuteActivity(ctx,
		activities.SynthesizeResultsLLM,
		activities.SynthesisInput{
			Query:        input.Query,
			AgentResults: agentResults,
			Context:      baseContext,
		}).Get(ctx, &synthesis)

	if err != nil {
		logger.Error("Synthesis failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
		}, err
	}

	totalTokens += synthesis.TokensUsed

	// Step 4: Apply reflection pattern for quality improvement
	reflectionConfig := patterns.ReflectionConfig{
		Enabled:             true,
		MaxRetries:          2,
		ConfidenceThreshold: 0.8,
		Criteria:            []string{"accuracy", "completeness", "clarity"},
		TimeoutMs:           30000,
	}

	reflectionOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		ModelTier:      modelTier,
	}

	finalResult, qualityScore, reflectionTokens, err := patterns.ReflectOnResult(
		ctx,
		input.Query,
		synthesis.FinalResult,
		agentResults,
		baseContext,
		reflectionConfig,
		reflectionOpts,
	)

	if err != nil {
		logger.Warn("Reflection failed, using original result", "error", err)
		finalResult = synthesis.FinalResult
		qualityScore = 0.5
	}

	totalTokens += reflectionTokens

	// Step 5: Update session and persist results
	if input.SessionID != "" {
		var updRes activities.SessionUpdateResult
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: totalTokens,
				AgentsUsed: len(agentResults),
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
					"workflow":      "research_flow_v2",
					"complexity":    decomp.ComplexityScore,
					"quality_score": qualityScore,
					"patterns_used": []string{"react", "parallel", "reflection"},
				},
				RedactPII: true,
			})
	}

	logger.Info("ResearchWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"agent_count", len(agentResults),
	)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"version":       "v2",
			"complexity":    decomp.ComplexityScore,
			"quality_score": qualityScore,
			"agent_count":   len(agentResults),
			"patterns_used": []string{"react", "parallel", "reflection"},
		},
	}, nil
}
