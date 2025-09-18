package execution

import (
    "fmt"
    "strings"
    "time"

    "go.temporal.io/sdk/workflow"
    "go.temporal.io/sdk/temporal"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// SequentialConfig controls sequential execution behavior
type SequentialConfig struct {
	EmitEvents              bool                   // Whether to emit streaming events
	Context                 map[string]interface{} // Base context for all agents
	PassPreviousResults     bool                   // Whether to pass previous results to next agent
	ExtractNumericValues    bool                   // Whether to extract numeric values from responses
	ClearDependentToolParams bool                  // Clear tool params for dependent tasks
}

// SequentialTask represents a task to execute sequentially
type SequentialTask struct {
	ID             string
	Description    string
	SuggestedTools []string
	ToolParameters map[string]interface{}
	PersonaID      string
	Role           string
	Dependencies   []string // Tasks this depends on
}

// SequentialResult contains results from sequential execution
type SequentialResult struct {
	Results     []activities.AgentExecutionResult
	TotalTokens int
	Metadata    map[string]interface{}
}

// ExecuteSequential runs tasks one after another, optionally passing results between them.
// Each task can access results from all previous tasks in the sequence.
func ExecuteSequential(
    ctx workflow.Context,
    tasks []SequentialTask,
    sessionID string,
    history []string,
    config SequentialConfig,
    budgetPerAgent int,
    userID string,
    modelTier string,
) (*SequentialResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting sequential execution",
		"task_count", len(tasks),
		"pass_results", config.PassPreviousResults,
	)

	// Activity options
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// Execute tasks sequentially
	var results []activities.AgentExecutionResult
	totalTokens := 0
	successCount := 0
	errorCount := 0

	for i, task := range tasks {
		// Prepare task context
		taskContext := make(map[string]interface{})
		for k, v := range config.Context {
			taskContext[k] = v
		}
		taskContext["role"] = task.Role
		taskContext["task_id"] = task.ID

		// Fetch agent-specific memory if session exists
		if sessionID != "" {
			var am activities.FetchAgentMemoryResult
			_ = workflow.ExecuteActivity(ctx,
				activities.FetchAgentMemory,
				activities.FetchAgentMemoryInput{
					SessionID: sessionID,
					AgentID:   fmt.Sprintf("agent-%s", task.ID),
					TopK:      5,
				}).Get(ctx, &am)
			if len(am.Items) > 0 {
				taskContext["agent_memory"] = am.Items
			}
		}

		// Add previous results to context if configured
		if config.PassPreviousResults && len(results) > 0 {
			previousResults := make(map[string]interface{})
			for j, prevResult := range results {
				if j < i && j < len(tasks) {
					resultMap := map[string]interface{}{
						"response": prevResult.Response,
						"tokens":   prevResult.TokensUsed,
						"success":  prevResult.Success,
					}

					// Extract numeric value if configured
					if config.ExtractNumericValues {
						if numVal, ok := parseNumericValue(prevResult.Response); ok {
							resultMap["numeric_value"] = numVal
						}
					}

					// Extract tool results if available
					if len(prevResult.ToolExecutions) > 0 {
						for _, te := range prevResult.ToolExecutions {
							switch te.Tool {
							case "calculator":
								if te.Output != nil {
									if calcResult, ok := te.Output.(map[string]interface{}); ok {
										if res, ok := calcResult["result"]; ok {
											resultMap["calculator"] = map[string]interface{}{"result": res}
										}
									}
								}
							case "code_executor":
								if te.Output != nil {
									resultMap["code_executor"] = map[string]interface{}{"output": te.Output}
								}
							default:
								if te.Output != nil {
									resultMap[te.Tool] = te.Output
								}
							}
						}
					}

					previousResults[tasks[j].ID] = resultMap
				}
			}
			taskContext["previous_results"] = previousResults
		}

		// Clear tool parameters for dependent tasks if configured
		if config.ClearDependentToolParams && len(task.Dependencies) > 0 && task.ToolParameters != nil {
			logger.Info("Clearing tool_parameters for dependent task",
				"task_id", task.ID,
				"dependencies", task.Dependencies,
			)
			task.ToolParameters = nil
		}

		// Emit agent started event
		if config.EmitEvents {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
				activities.EmitTaskUpdateInput{
					WorkflowID: wid,
					EventType:  activities.StreamEventAgentStarted,
					AgentID:    fmt.Sprintf("agent-%s", task.ID),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
		}

		logger.Debug("Executing agent for sequential task",
			"task_index", i,
			"task_id", task.ID,
			"suggested_tools", task.SuggestedTools,
		)

		// Execute agent
		var result activities.AgentExecutionResult
		var err error

		if budgetPerAgent > 0 {
			// Execute with budget
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			// Extract base UUID from workflow ID (remove suffix like "_23")
			taskID := wid
			if idx := strings.LastIndex(wid, "_"); idx > 0 {
				taskID = wid[:idx]
			}
            err = workflow.ExecuteActivity(ctx,
                constants.ExecuteAgentWithBudgetActivity,
                activities.BudgetedAgentInput{
                    AgentInput: activities.AgentExecutionInput{
                        Query:          task.Description,
                        AgentID:        fmt.Sprintf("agent-%s", task.ID),
                        Context:        taskContext,
                        Mode:           "standard",
                        SessionID:      sessionID,
                        History:        history,
                        SuggestedTools: task.SuggestedTools,
                        ToolParameters: task.ToolParameters,
                        PersonaID:      task.PersonaID,
                    },
                    MaxTokens: budgetPerAgent,
                    UserID:    userID,
                    TaskID:    taskID,
                    ModelTier: modelTier,
                }).Get(ctx, &result)
		} else {
			// Execute without budget
            err = workflow.ExecuteActivity(ctx,
                activities.ExecuteAgent,
                activities.AgentExecutionInput{
                    Query:          task.Description,
                    AgentID:        fmt.Sprintf("agent-%s", task.ID),
                    Context:        taskContext,
                    Mode:           "standard",
                    SessionID:      sessionID,
                    History:        history,
                    SuggestedTools: task.SuggestedTools,
                    ToolParameters: task.ToolParameters,
                    PersonaID:      task.PersonaID,
                }).Get(ctx, &result)
		}

		if err != nil {
			logger.Error("Agent execution failed",
				"task_id", task.ID,
				"error", err,
			)
			errorCount++

			// Emit error event
			if config.EmitEvents {
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
					activities.EmitTaskUpdateInput{
						WorkflowID: wid,
						EventType:  activities.StreamEventErrorOccurred,
						AgentID:    fmt.Sprintf("agent-%s", task.ID),
						Message:    err.Error(),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
			}

			// Continue to next task even on failure
			continue
		}

		// Success
		results = append(results, result)
		totalTokens += result.TokensUsed
		successCount++

		// Emit completion event
		if config.EmitEvents {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
				activities.EmitTaskUpdateInput{
					WorkflowID: wid,
					EventType:  activities.StreamEventAgentCompleted,
					AgentID:    fmt.Sprintf("agent-%s", task.ID),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
		}

		// Record agent memory if session exists
		if sessionID != "" {
			detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
			workflow.ExecuteActivity(detachedCtx,
				activities.RecordAgentMemory,
				activities.RecordAgentMemoryInput{
					SessionID: sessionID,
					UserID:    userID,
					AgentID:   result.AgentID,
					Role:      task.Role,
					Query:     task.Description,
					Answer:    result.Response,
					Model:     result.ModelUsed,
					RedactPII: true,
					Extra: map[string]interface{}{
						"task_id": task.ID,
					},
				})
		}
	}

	logger.Info("Sequential execution completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", errorCount,
		"total_tokens", totalTokens,
	)

	return &SequentialResult{
		Results:     results,
		TotalTokens: totalTokens,
		Metadata: map[string]interface{}{
			"total_tasks": len(tasks),
			"successful":  successCount,
			"failed":      errorCount,
		},
	}, nil
}

// Helper function to parse numeric values from responses
func parseNumericValue(response string) (float64, bool) {
	// Simple numeric extraction - in production use more sophisticated parsing
	var value float64
	if _, err := fmt.Sscanf(response, "%f", &value); err == nil {
		return value, true
	}
	// Try to find a number in the response
	if _, err := fmt.Sscanf(response, "The answer is %f", &value); err == nil {
		return value, true
	}
	if _, err := fmt.Sscanf(response, "Result: %f", &value); err == nil {
		return value, true
	}
	return 0, false
}
