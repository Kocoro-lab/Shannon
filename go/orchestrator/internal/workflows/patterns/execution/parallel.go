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
)

// ParallelConfig controls parallel execution behavior
type ParallelConfig struct {
	MaxConcurrency int                    // Maximum concurrent agents
	Semaphore      workflow.Semaphore     // Concurrency control (interface, not pointer)
	EmitEvents     bool                   // Whether to emit streaming events
	Context        map[string]interface{} // Base context for all agents
}

// ParallelTask represents a task to execute in parallel
type ParallelTask struct {
	ID             string
	Description    string
	SuggestedTools []string
	ToolParameters map[string]interface{}
	PersonaID      string
	Role           string
	Dependencies   []string // For hybrid parallel/sequential execution
}

// ParallelResult contains results from parallel execution
type ParallelResult struct {
	Results     []activities.AgentExecutionResult
	TotalTokens int
	Metadata    map[string]interface{}
}

// ExecuteParallel runs multiple tasks in parallel with concurrency control.
// It supports optional budget enforcement and streaming events.
func ExecuteParallel(
	ctx workflow.Context,
	tasks []ParallelTask,
	sessionID string,
	history []string,
	config ParallelConfig,
	budgetPerAgent int,
	userID string,
	modelTier string,
) (*ParallelResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting parallel execution",
		"task_count", len(tasks),
		"max_concurrency", config.MaxConcurrency,
	)

	// Create semaphore if not provided
	if config.Semaphore == nil {
		config.Semaphore = workflow.NewSemaphore(ctx, int64(config.MaxConcurrency))
	}

	// Channel for collecting in-flight futures with a release handshake
	futuresChan := workflow.NewChannel(ctx)

	// Track futures with their original index
	type futureWithIndex struct {
		Index   int
		Future  workflow.Future
		Release workflow.Channel // send a signal when it's safe to release the semaphore
	}

	// Activity options
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// Launch parallel executions
	for i, task := range tasks {
		i := i       // Capture for closure
		task := task // Capture for closure

		workflow.Go(ctx, func(ctx workflow.Context) {
			// Acquire semaphore
			if err := config.Semaphore.Acquire(ctx, 1); err != nil {
				logger.Error("Failed to acquire semaphore",
					"task_id", task.ID,
					"error", err,
				)
				futuresChan.Send(ctx, futureWithIndex{Index: i, Future: nil, Release: nil})
				return
			}
			// Create a release channel so the collector can signal when to release
			rel := workflow.NewChannel(ctx)

			// Prepare task context
			taskContext := make(map[string]interface{})
			for k, v := range config.Context {
				taskContext[k] = v
			}
			taskContext["role"] = task.Role
			taskContext["task_id"] = task.ID

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

			// Execute agent
			var future workflow.Future

			if budgetPerAgent > 0 {
				// Execute with budget
				wid := workflow.GetInfo(ctx).WorkflowExecution.ID
				// Extract base UUID from workflow ID (remove suffix like "_23")
				taskID := wid
				if idx := strings.LastIndex(wid, "_"); idx > 0 {
					taskID = wid[:idx]
				}
				future = workflow.ExecuteActivity(ctx,
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
					})
			} else {
				// Execute without budget
				future = workflow.ExecuteActivity(ctx,
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
					})
			}

			futuresChan.Send(ctx, futureWithIndex{Index: i, Future: future, Release: rel})

			// Hold the permit until the collector signals that it has finished processing the result
			var _sig struct{}
			rel.Receive(ctx, &_sig)
			// Now safe to release the semaphore
			config.Semaphore.Release(1)
		})
	}

	// Collect results
	results := make([]activities.AgentExecutionResult, len(tasks))
	totalTokens := 0
	successCount := 0
	errorCount := 0

	// Use a selector to receive futures and process completions in completion order
	sel := workflow.NewSelector(ctx)
	received := 0
	skippedNil := 0
	processed := 0

	var registerReceive func()
	registerReceive = func() {
		sel.AddReceive(futuresChan, func(c workflow.ReceiveChannel, more bool) {
			var fwi futureWithIndex
			c.Receive(ctx, &fwi)
			received++
			if fwi.Future == nil {
				// Failed to acquire or schedule; count as error and skip
				errorCount++
				skippedNil++
			} else {
				fwi := fwi // capture for closure
				sel.AddFuture(fwi.Future, func(f workflow.Future) {
					var result activities.AgentExecutionResult
					err := f.Get(ctx, &result)
					if err != nil {
						logger.Error("Agent execution failed",
							"task_id", tasks[fwi.Index].ID,
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
									AgentID:    fmt.Sprintf("agent-%s", tasks[fwi.Index].ID),
									Message:    err.Error(),
									Timestamp:  workflow.Now(ctx),
								}).Get(ctx, nil)
						}
					} else {
						results[fwi.Index] = result
						totalTokens += result.TokensUsed
						successCount++

						// Persist agent execution (fire-and-forget)
						workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
						persistAgentExecutionLocal(ctx, workflowID, fmt.Sprintf("agent-%s", tasks[fwi.Index].ID), tasks[fwi.Index].Description, result)

						// Emit completion event
						if config.EmitEvents {
							wid := workflow.GetInfo(ctx).WorkflowExecution.ID
							_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
								activities.EmitTaskUpdateInput{
									WorkflowID: wid,
									EventType:  activities.StreamEventAgentCompleted,
									AgentID:    fmt.Sprintf("agent-%s", tasks[fwi.Index].ID),
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
									Role:      tasks[fwi.Index].Role,
									Query:     tasks[fwi.Index].Description,
									Answer:    result.Response,
									Model:     result.ModelUsed,
									RedactPII: true,
									Extra: map[string]interface{}{
										"task_id": tasks[fwi.Index].ID,
									},
								})
						}
					}

					// Signal producer that we're done with this future (release semaphore)
					if fwi.Release != nil {
						var sig struct{}
						fwi.Release.Send(ctx, sig)
					}
					processed++
				})
			}

			// Continue receiving until we've seen all producer messages
			if received < len(tasks) {
				registerReceive()
			}
		})
	}

	// Prime the selector to start receiving
	if len(tasks) > 0 {
		registerReceive()
	}

	// Event loop: select until all non-nil futures are processed
	for processed < (len(tasks) - skippedNil) {
		sel.Select(ctx)
	}

	logger.Info("Parallel execution completed",
		"total_tasks", len(tasks),
		"successful", successCount,
		"failed", errorCount,
		"total_tokens", totalTokens,
	)

	return &ParallelResult{
		Results:     results,
		TotalTokens: totalTokens,
		Metadata: map[string]interface{}{
			"total_tasks": len(tasks),
			"successful":  successCount,
			"failed":      errorCount,
		},
	}, nil
}

// persistAgentExecutionLocal is a local helper to avoid circular imports
// It mirrors the logic from supervisor_workflow.go and sequential.go
func persistAgentExecutionLocal(ctx workflow.Context, workflowID, agentID, input string, result activities.AgentExecutionResult) {
	logger := workflow.GetLogger(ctx)

	// Use detached context for fire-and-forget persistence
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	detachedCtx = workflow.WithActivityOptions(detachedCtx, activityOpts)

	// Persist agent execution asynchronously
	workflow.ExecuteActivity(detachedCtx,
		activities.PersistAgentExecutionStandalone,
		activities.PersistAgentExecutionInput{
			WorkflowID: workflowID,
			AgentID:    agentID,
			Input:      input,
			Output:     result.Response,
			State:      "COMPLETED",
			TokensUsed: result.TokensUsed,
			ModelUsed:  result.ModelUsed,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Metadata: map[string]interface{}{
				"workflow": "parallel",
				"strategy": "parallel",
			},
		})

	// Persist tool executions if any
	for _, tool := range result.ToolExecutions {
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

		workflow.ExecuteActivity(detachedCtx,
			activities.PersistToolExecutionStandalone,
			activities.PersistToolExecutionInput{
				WorkflowID: workflowID,
				AgentID:    agentID,
				ToolName:   tool.Tool,
				Output:     outputStr,
				Success:    tool.Success,
				Error:      tool.Error,
			})
	}

	logger.Debug("Scheduled persistence for agent execution",
		"workflow_id", workflowID,
		"agent_id", agentID,
	)
}
