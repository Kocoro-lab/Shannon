package workflows

import (
	"encoding/json"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// persistAgentExecution is a helper to persist agent execution results
// This is a fire-and-forget operation that won't fail the workflow
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
			Metadata:   map[string]interface{}{"role": result.Role},
		},
	)

	// Persist tool executions if any
	if len(result.ToolExecutions) > 0 {
		for _, tool := range result.ToolExecutions {
			// Convert tool output to string
			outputStr := ""
			if tool.Output != nil {
				// Handle different output types
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
					InputParams:    nil, // Tool execution from agent doesn't provide input params
					Output:         outputStr,
					Success:        tool.Success,
					TokensConsumed: 0,               // Not provided by agent
					DurationMs:     tool.DurationMs, // From agent-core proto
					Error:          tool.Error,
				},
			)
		}
	}
}
