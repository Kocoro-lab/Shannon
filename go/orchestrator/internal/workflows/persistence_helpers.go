package workflows

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
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

	// Pre-generate agent execution ID using SideEffect for replay safety
	// This allows correlating tool executions with their parent agent execution
	var agentExecutionID string
	workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return uuid.New().String()
	}).Get(&agentExecutionID)

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
			ID:         agentExecutionID, // Use pre-generated ID
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

			// Extract input params from tool execution (from HTTP path)
			inputParamsMap, _ := tool.InputParams.(map[string]interface{})

			workflow.ExecuteActivity(
				persistCtx,
				activities.PersistToolExecutionStandalone,
				activities.PersistToolExecutionInput{
					WorkflowID:       workflowID,
					AgentID:          agentID,
					AgentExecutionID: agentExecutionID, // Link to parent agent execution
					ToolName:         tool.Tool,
					InputParams:      inputParamsMap,
					Output:           outputStr,
					Success:          tool.Success,
					TokensConsumed:   0,               // Not provided by agent
					DurationMs:       tool.DurationMs, // From agent-core proto
					Error:            tool.Error,
				},
			)
		}
	}
}
