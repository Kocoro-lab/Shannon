package execution

import (
	"encoding/json"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	imodels "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
	pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
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
	ParentArea     string
	Dependencies   []string // For hybrid parallel/sequential execution
	// Optional per-task context overrides (e.g., task contracts)
	ContextOverrides map[string]interface{}
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
		StartToCloseTimeout: agentStartToCloseTimeout(config.Context, 10*time.Minute),
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
			if task.ContextOverrides != nil {
				for k, v := range task.ContextOverrides {
					taskContext[k] = v
				}
			}
			taskContext["role"] = task.Role
			taskContext["task_id"] = task.ID
			if task.ParentArea != "" {
				taskContext["parent_area"] = task.ParentArea
			}

			// Compute agent name using station names for deterministic, human-readable IDs
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			if config.Context != nil {
				if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
					wid = p
				}
			}
			agentName := agents.GetAgentName(wid, i)

			// Emit agent started event (publish under parent workflow when available)
			if config.EmitEvents {
				_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
					activities.EmitTaskUpdateInput{
						WorkflowID: wid,
						EventType:  activities.StreamEventAgentStarted,
						AgentID:    agentName,
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
			}

			// Execute agent
			var future workflow.Future

			if budgetPerAgent > 0 {
				// Execute with budget
				// Prefer parent workflow ID for budget tracking and persistence
				parentWid := ""
				if config.Context != nil {
					if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
						parentWid = p
					}
				}
				// Use parent workflow ID when available, otherwise fallback to child ID (trim suffix if present)
				taskID := wid
				if parentWid != "" {
					taskID = parentWid
				} else if idx := strings.LastIndex(wid, "_"); idx > 0 {
					taskID = wid[:idx]
				}
				future = workflow.ExecuteActivity(ctx,
					constants.ExecuteAgentWithBudgetActivity,
					activities.BudgetedAgentInput{
						AgentInput: activities.AgentExecutionInput{
							Query:            task.Description,
							AgentID:          agentName,
							Context:          taskContext,
							Mode:             "standard",
							SessionID:        sessionID,
							History:          history,
							SuggestedTools:   task.SuggestedTools,
							ToolParameters:   task.ToolParameters,
							PersonaID:        task.PersonaID,
							ParentWorkflowID: parentWid,
						},
						MaxTokens: budgetPerAgent,
						UserID:    userID,
						TaskID:    taskID,
						ModelTier: modelTier,
					})
			} else {
				// Execute without budget
				// Inject model_tier to taskContext to ensure consistent tier selection
				// (budget path injects via BudgetedAgentInput.ModelTier -> budget.go:298)
				if modelTier != "" {
					taskContext["model_tier"] = modelTier
				}

				// Determine parent workflow if available for streaming correlation
				parentWid := ""
				if config.Context != nil {
					if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
						parentWid = p
					}
				}
				future = workflow.ExecuteActivity(ctx,
					activities.ExecuteAgent,
					activities.AgentExecutionInput{
						Query:            task.Description,
						AgentID:          agentName,
						Context:          taskContext,
						Mode:             "standard",
						SessionID:        sessionID,
						History:          history,
						SuggestedTools:   task.SuggestedTools,
						ToolParameters:   task.ToolParameters,
						PersonaID:        task.PersonaID,
						ParentWorkflowID: parentWid,
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
						// Emit error event (parent workflow when available)
						if config.EmitEvents {
							wid := workflow.GetInfo(ctx).WorkflowExecution.ID
							if config.Context != nil {
								if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
									wid = p
								}
							}
							agentName := agents.GetAgentName(wid, fwi.Index)
							_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
								activities.EmitTaskUpdateInput{
									WorkflowID: wid,
									EventType:  activities.StreamEventErrorOccurred,
									AgentID:    agentName,
									Message:    err.Error(),
									Timestamp:  workflow.Now(ctx),
								}).Get(ctx, nil)
						}
					} else {
						results[fwi.Index] = result
						totalTokens += result.TokensUsed
						successCount++

						// Record token usage for this parallel task
						// Important: Avoid double-recording when running budgeted agents.
						// ExecuteAgentWithBudget already records usage inside the activity.
						if budgetPerAgent <= 0 {
							wid := workflow.GetInfo(ctx).WorkflowExecution.ID
							if config.Context != nil {
								if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
									wid = p
								}
							}
							inTok := result.InputTokens
							outTok := result.OutputTokens
							if inTok == 0 && outTok == 0 && result.TokensUsed > 0 {
								inTok = result.TokensUsed * 6 / 10
								outTok = result.TokensUsed - inTok
							}
							// Fallbacks for missing model/provider
							model := result.ModelUsed
							if strings.TrimSpace(model) == "" {
								if m := pricing.GetPriorityOneModel(modelTier); m != "" {
									model = m
								}
							}
							provider := result.Provider
							if strings.TrimSpace(provider) == "" {
								provider = imodels.DetectProvider(model)
							}
							// Standardized activity options
							recCtx := opts.WithTokenRecordOptions(ctx)

							// Zero-token observability flag via workflow context: record_zero_token=true
							recordZero := false
							if config.Context != nil {
								if v, ok := config.Context["record_zero_token"]; ok {
									switch t := v.(type) {
									case bool:
										recordZero = t
									case string:
										if strings.EqualFold(t, "true") {
											recordZero = true
										}
									}
								}
							}

							meta := map[string]interface{}{"phase": "parallel"}

							if (inTok + outTok) == 0 {
								if recordZero {
									meta["zero_tokens"] = true
									_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
										UserID:       userID,
										SessionID:    sessionID,
										TaskID:       wid,
										AgentID:      result.AgentID,
										Model:        model,
										Provider:     provider,
										InputTokens:  inTok,
										OutputTokens: outTok,
										Metadata:     meta,
									}).Get(recCtx, nil)
								} else {
									logger.Warn("Skipping token usage record: zero tokens",
										"agent_id", result.AgentID,
										"task_id", tasks[fwi.Index].ID,
									)
								}
							} else {
								_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
									UserID:       userID,
									SessionID:    sessionID,
									TaskID:       wid,
									AgentID:      result.AgentID,
									Model:        model,
									Provider:     provider,
									InputTokens:  inTok,
									OutputTokens: outTok,
									Metadata:     meta,
								}).Get(recCtx, nil)
							}
						}

						// Persist agent execution (fire-and-forget). Use parent workflow ID when available.
						workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
						if config.Context != nil {
							if p, ok := config.Context["parent_workflow_id"].(string); ok && p != "" {
								workflowID = p
							}
						}
						agentName := agents.GetAgentName(workflowID, fwi.Index)
						persistAgentExecutionLocal(ctx, workflowID, agentName, tasks[fwi.Index].Description, result)

						// Emit completion event (parent workflow when available)
						if config.EmitEvents {
							_ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate",
								activities.EmitTaskUpdateInput{
									WorkflowID: workflowID,
									EventType:  activities.StreamEventAgentCompleted,
									AgentID:    agentName,
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

	// Build metadata summary
	md := map[string]interface{}{
		"total_tasks": len(tasks),
		"successful":  successCount,
		"failed":      errorCount,
	}

	return &ParallelResult{
		Results:     results,
		TotalTokens: totalTokens,
		Metadata:    md,
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
