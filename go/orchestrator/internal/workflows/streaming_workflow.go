package workflows

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/state"
)

// StreamingWorkflow executes tasks with streaming output and typed state management
func StreamingWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting StreamingWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
		"session_id", input.SessionID,
	)

	// Initialize typed state channel
	stateChannel := state.NewStateChannel("streaming-workflow")

	// Set initial state
	agentState := &state.AgentState{
		Query:   input.Query,
		Context: input.Context,
		PlanningState: state.PlanningState{
			CurrentStep: 0,
			TotalSteps:  1,
			Plan:        []string{"Analyze and respond to query"},
			Completed:   []bool{false},
		},
		ExecutionState: state.ExecutionState{
			Status:    "pending",
			StartTime: workflow.Now(ctx),
		},
		BeliefState: state.BeliefState{
			Confidence: 1.0,
		},
	}

	// Add state validation
	stateChannel.AddValidator(func(data interface{}) error {
		s, ok := data.(*state.AgentState)
		if !ok {
			return fmt.Errorf("invalid state type")
		}
		return s.Validate()
	})

	if err := stateChannel.Set(agentState); err != nil {
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Invalid initial state: %v", err),
		}, err
	}

	// Create checkpoint before execution
	checkpointID, _ := stateChannel.Checkpoint(map[string]interface{}{
		"phase": "pre-execution",
	})
	logger.Info("State checkpoint created", "checkpoint_id", checkpointID)

	// Configure activity options with longer timeout for streaming
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,  // Longer timeout for streaming
		HeartbeatTimeout:    30 * time.Second, // Heartbeat to track progress
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Update state to running
	agentState.ExecutionState.Status = "running"
	if err := stateChannel.Set(agentState); err != nil {
		logger.Error("Failed to update state", "error", err)
	}

	// Execute with streaming
	streamingActivities := activities.NewStreamingActivities()
	streamInput := activities.StreamExecuteInput{
		Query:     input.Query,
		Context:   input.Context,
		SessionID: input.SessionID,
		AgentID:   "streaming-agent",
		Mode:      input.Mode,
	}

	// Start streaming execution
	logger.Info("Starting streaming execution")

	var streamRes activities.AgentExecutionResult
	err := workflow.ExecuteActivity(ctx, streamingActivities.StreamExecute, streamInput).Get(ctx, &streamRes)

	if err != nil {
		logger.Error("Streaming execution failed", "error", err)

		// Update state with error
		agentState.ExecutionState.Status = "failed"
		agentState.AddError(state.ErrorRecord{
			Timestamp:    workflow.Now(ctx),
			ErrorType:    "streaming_error",
			ErrorMessage: err.Error(),
			Recoverable:  false,
		})
		stateChannel.Set(agentState)

		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Update state with result
	agentState.IntermediateResults = append(agentState.IntermediateResults, streamRes.Response)
	agentState.ExecutionState.Status = "completed"
	agentState.PlanningState.CurrentStep = 1
	agentState.PlanningState.Completed[0] = true

	// Add tool result
	agentState.AddToolResult(state.ToolResult{
		ToolName:      "streaming_llm",
		Input:         input.Query,
		Output:        streamRes.Response,
		Success:       true,
		ExecutionTime: int64(agentState.GetExecutionDuration().Milliseconds()),
		TokensUsed:    streamRes.TokensUsed,
		Timestamp:     workflow.Now(ctx),
	})

	if err := stateChannel.Set(agentState); err != nil {
		logger.Warn("Failed to update final state", "error", err)
	}

	// Create final checkpoint
	finalCheckpointID, _ := stateChannel.Checkpoint(map[string]interface{}{
		"phase":  "completed",
		"result": streamRes.Response,
	})

	logger.Info("StreamingWorkflow completed successfully",
		"result_length", len(streamRes.Response),
		"tokens_used", streamRes.TokensUsed,
		"duration_ms", agentState.GetExecutionDuration().Milliseconds(),
		"final_checkpoint", finalCheckpointID,
	)

	// Update session with token usage
	if input.SessionID != "" {
		var sessionUpdateResult activities.SessionUpdateResult
		err := workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     streamRes.Response,
				TokensUsed: streamRes.TokensUsed,
				AgentsUsed: 1,
				ModelUsed:  streamRes.ModelUsed,
			},
		).Get(ctx, &sessionUpdateResult)
		if err != nil {
			logger.Warn("Failed to update session with tokens",
				"session_id", input.SessionID,
				"error", err,
			)
		}
	}

	return TaskResult{
		Result:     streamRes.Response,
		Success:    true,
		TokensUsed: streamRes.TokensUsed,
		Metadata: map[string]interface{}{
			"execution_time_ms": agentState.GetExecutionDuration().Milliseconds(),
			"checkpoints":       stateChannel.ListCheckpoints(),
			"final_state":       agentState.ExecutionState.Status,
		},
	}, nil
}

// ParallelStreamingWorkflow executes multiple streaming tasks in parallel
func ParallelStreamingWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ParallelStreamingWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
	)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Create multiple streaming inputs for parallel execution
	streamingActivities := activities.NewStreamingActivities()
	inputs := []activities.StreamExecuteInput{
		{
			Query:     input.Query + " (perspective 1)",
			Context:   input.Context,
			SessionID: input.SessionID,
			AgentID:   "agent-1",
			Mode:      input.Mode,
		},
		{
			Query:     input.Query + " (perspective 2)",
			Context:   input.Context,
			SessionID: input.SessionID,
			AgentID:   "agent-2",
			Mode:      input.Mode,
		},
		{
			Query:     input.Query + " (perspective 3)",
			Context:   input.Context,
			SessionID: input.SessionID,
			AgentID:   "agent-3",
			Mode:      input.Mode,
		},
	}

	// Execute streams in parallel
	var futures []workflow.Future
	for _, streamInput := range inputs {
		future := workflow.ExecuteActivity(ctx, streamingActivities.StreamExecute, streamInput)
		futures = append(futures, future)
	}

	// Collect results
	var results []activities.AgentExecutionResult
	totalTokens := 0

	for i, future := range futures {
		var result activities.AgentExecutionResult
		err := future.Get(ctx, &result)
		if err != nil {
			logger.Error("Stream execution failed",
				"agent_id", inputs[i].AgentID,
				"error", err,
			)
			// Continue with other streams
			continue
		}
		results = append(results, result)
		totalTokens += result.TokensUsed
	}

	// Synthesize results (LLM-first)
	var synthesis activities.SynthesisResult

	// Already have AgentExecutionResult from streaming
	agentResults := make([]activities.AgentExecutionResult, len(results))
	copy(agentResults, results)

	if input.BypassSingleResult && len(agentResults) == 1 && agentResults[0].Success {
		// Avoid bypass if the single result looks like raw JSON or comes from web_search
		shouldBypass := true
		if len(agentResults[0].ToolsUsed) > 0 {
			for _, t := range agentResults[0].ToolsUsed {
				if strings.EqualFold(t, "web_search") {
					shouldBypass = false
					break
				}
			}
		}
		if shouldBypass {
			trimmed := strings.TrimSpace(agentResults[0].Response)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				shouldBypass = false
			}
		}

		if shouldBypass {
			synthesis = activities.SynthesisResult{FinalResult: agentResults[0].Response, TokensUsed: agentResults[0].TokensUsed}
		} else {
			var err error
        err = workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{
            Query:        input.Query,
            AgentResults: agentResults,
            Context:      input.Context, // Pass role/prompt_params for role-aware synthesis
        }).Get(ctx, &synthesis)
			if err != nil {
				logger.Error("Result synthesis failed", "error", err)
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}
		}
	} else {
		var err error
        err = workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{
            Query:        input.Query,
            AgentResults: agentResults,
            Context:      input.Context, // Pass role/prompt_params for role-aware synthesis
        }).Get(ctx, &synthesis)
		if err != nil {
			logger.Error("Result synthesis failed", "error", err)
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
	}

	logger.Info("ParallelStreamingWorkflow completed",
		"num_streams", len(results),
		"total_tokens", totalTokens,
	)

	// Update session with token usage
	if input.SessionID != "" {
		var sessionUpdateResult activities.SessionUpdateResult
		err := workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     synthesis.FinalResult,
				TokensUsed: totalTokens,
				AgentsUsed: len(results),
				AgentUsage: func() []activities.AgentUsage {
					u := make([]activities.AgentUsage, 0, len(agentResults))
					for _, r := range agentResults {
						u = append(u, activities.AgentUsage{Model: r.ModelUsed, Tokens: r.TokensUsed, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens})
					}
					return u
				}(),
			},
		).Get(ctx, &sessionUpdateResult)
		if err != nil {
			logger.Warn("Failed to update session with tokens",
				"session_id", input.SessionID,
				"error", err,
			)
		}
	}

	// Emit WORKFLOW_COMPLETED before returning
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "streaming",
		Message:    "Workflow completed",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     synthesis.FinalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"num_streams": len(results),
			"parallel":    true,
		},
	}, nil
}
