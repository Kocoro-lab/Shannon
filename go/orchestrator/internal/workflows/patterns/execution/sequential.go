package execution

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/util"
)

// SequentialConfig controls sequential execution behavior
type SequentialConfig struct {
	EmitEvents               bool                   // Whether to emit streaming events
	Context                  map[string]interface{} // Base context for all agents
	PassPreviousResults      bool                   // Whether to pass previous results to next agent
	ExtractNumericValues     bool                   // Whether to extract numeric values from responses
	ClearDependentToolParams bool                   // Clear tool params for dependent tasks
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
						if numVal, ok := util.ParseNumericValue(prevResult.Response); ok {
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
			task.ToolParameters = nil
		}

		// Emit agent started event
		if config.EmitEvents {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			if config.Context != nil {
				if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
					wid = p
				}
			}
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

			// Emit error event (parent workflow when available)
			if config.EmitEvents {
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				if config.Context != nil {
					if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
						wid = p
					}
				}
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

		// Persist agent execution (fire-and-forget)
		workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
		persistAgentExecution(ctx, workflowID, fmt.Sprintf("agent-%s", task.ID), task.Description, result)

		// Success
		results = append(results, result)
		totalTokens += result.TokensUsed
		successCount++

		// Emit completion event (parent workflow when available)
		if config.EmitEvents {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			if config.Context != nil {
				if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
					wid = p
				}
			}
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

// persistAgentExecution persists agent execution results (fire-and-forget)
func persistAgentExecution(ctx workflow.Context, workflowID string, agentID string, input string, result activities.AgentExecutionResult) {
	// Create a new context for persistence with no retries
	persistCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Determine state based on success
	state := "COMPLETED"
	if !result.Success {
		state = "FAILED"
	}

	// Fire and forget - don't wait for result
	workflow.ExecuteActivity(
		persistCtx,
		activities.PersistAgentExecutionStandalone,
		activities.PersistAgentExecutionInput{
			WorkflowID: workflowID,
			AgentID:    agentID,
			Input:      input,
			Output:     result.Response,
			State:      state,
			TokensUsed: result.TokensUsed,
			ModelUsed:  result.ModelUsed,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Metadata: map[string]interface{}{
				"workflow": "sequential",
				"strategy": "sequential",
			},
		},
	)

	// Persist tool executions if any
	if len(result.ToolExecutions) > 0 {
		for _, tool := range result.ToolExecutions {
			// Convert tool output to string
			outputStr := ""
			if tool.Output != nil {
				switch v := tool.Output.(type) {
				case string:
					outputStr = v
				default:
					// Properly serialize complex outputs to JSON
					if jsonBytes, err := json.Marshal(v); err == nil {
						outputStr = string(jsonBytes)
					} else {
						outputStr = "complex output"
					}
				}
			}

			workflow.ExecuteActivity(
				persistCtx,
				activities.PersistToolExecutionStandalone,
				activities.PersistToolExecutionInput{
					WorkflowID:     workflowID,
					AgentID:        agentID,
					ToolName:       tool.Tool,
					InputParams:    nil,
					Output:         outputStr,
					Success:        tool.Success,
					TokensConsumed: 0,
					DurationMs:     0,
					Error:          tool.Error,
				},
			)
		}
	}
}

// Helper function to parse numeric values from responses
// parseNumericValue wrapper removed in favor of util.ParseNumericValue at call sites
