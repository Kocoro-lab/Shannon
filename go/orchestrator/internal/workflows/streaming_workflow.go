package workflows

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/state"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
)

// StreamingWorkflow executes tasks with streaming output and typed state management
func StreamingWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting StreamingWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
		"session_id", input.SessionID,
	)

	// Determine workflow ID for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Initialize control signal handler for pause/resume/cancel
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	controlHandler := &ControlSignalHandler{
		WorkflowID: workflowID,
		AgentID:    "streaming",
		Logger:     logger,
		EmitCtx:    emitCtx,
	}
	controlHandler.Setup(ctx)

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

	// Check pause/cancel before execution
	if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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

	// Build metadata and include model/provider + token breakdown (estimated 60/40 if missing)
	meta := map[string]interface{}{
		"execution_time_ms": agentState.GetExecutionDuration().Milliseconds(),
		"checkpoints":       stateChannel.ListCheckpoints(),
		"final_state":       agentState.ExecutionState.Status,
	}

	// Prepare a single-agent result for aggregation
	ar := activities.AgentExecutionResult{
		AgentID:      "streaming-agent",
		ModelUsed:    streamRes.ModelUsed,
		TokensUsed:   streamRes.TokensUsed,
		InputTokens:  streamRes.InputTokens,
		OutputTokens: streamRes.OutputTokens,
		Success:      true,
	}
	if ar.InputTokens == 0 && ar.OutputTokens == 0 && ar.TokensUsed > 0 {
		ar.InputTokens = ar.TokensUsed * 6 / 10
		ar.OutputTokens = ar.TokensUsed - ar.InputTokens
	}
	agentMeta := metadata.AggregateAgentMetadata([]activities.AgentExecutionResult{ar}, 0)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Add cost estimate if tokens available
	if streamRes.TokensUsed > 0 {
		model := ""
		if m, ok := meta["model"].(string); ok && m != "" {
			model = m
		}
		meta["cost_usd"] = pricing.CostForTokens(model, streamRes.TokensUsed)
	}

	// Check pause/cancel before completion
	if err := controlHandler.CheckPausePoint(ctx, "pre_completion"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	return TaskResult{
		Result:     streamRes.Response,
		Success:    true,
		TokensUsed: streamRes.TokensUsed,
		Metadata:   meta,
	}, nil
}

// ParallelStreamingWorkflow executes multiple streaming tasks in parallel
func ParallelStreamingWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ParallelStreamingWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
	)

	// Determine workflow ID for event streaming
	// Use parent workflow ID if this is a child workflow, otherwise use own ID
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Initialize control signal handler for pause/resume/cancel
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	controlHandler := &ControlSignalHandler{
		WorkflowID: workflowID,
		AgentID:    "parallel-streaming",
		Logger:     logger,
		EmitCtx:    emitCtx,
	}
	controlHandler.Setup(ctx)

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

	// Check pause/cancel before execution
	if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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

	// Collect citations from streaming agent results (best-effort)
	var collectedCitations []metadata.Citation
	if len(results) > 0 {
		var resultsForCitations []interface{}
		for _, ar := range results {
			var toolExecs []interface{}
			if len(ar.ToolExecutions) > 0 {
				for _, te := range ar.ToolExecutions {
					toolExecs = append(toolExecs, map[string]interface{}{
						"tool":    te.Tool,
						"success": te.Success,
						"output":  te.Output,
						"error":   te.Error,
					})
				}
			}
			resultsForCitations = append(resultsForCitations, map[string]interface{}{
				"agent_id":        ar.AgentID,
				"tool_executions": toolExecs,
				"response":        ar.Response,
			})
		}
		collectedCitations, _ = metadata.CollectCitations(resultsForCitations, workflow.Now(ctx), 0)
	}

	// Prepare synthesis context (copy input context, set defaults, inject citations)
	buildContext := func() map[string]interface{} {
		ctxMap := map[string]interface{}{}
		if input.Context != nil {
			for k, v := range input.Context {
				ctxMap[k] = v
			}
		}
		if _, ok := ctxMap["synthesis_style"]; !ok {
			if _, hasAreas := ctxMap["research_areas"]; hasAreas {
				ctxMap["synthesis_style"] = "comprehensive"
			}
		}
		if len(collectedCitations) > 0 {
			var b strings.Builder
			for i, c := range collectedCitations {
				idx := i + 1
				title := c.Title
				if title == "" {
					title = c.Source
				}
				if c.PublishedDate != nil {
					fmt.Fprintf(&b, "[%d] %s (%s) - %s, %s\n", idx, title, c.URL, c.Source, c.PublishedDate.Format("2006-01-02"))
				} else {
					fmt.Fprintf(&b, "[%d] %s (%s) - %s\n", idx, title, c.URL, c.Source)
				}
			}
			ctxMap["available_citations"] = strings.TrimRight(b.String(), "\n")
			ctxMap["citation_count"] = len(collectedCitations)

			out := make([]map[string]interface{}, 0, len(collectedCitations))
			for _, c := range collectedCitations {
				out = append(out, map[string]interface{}{
					"url":               c.URL,
					"title":             c.Title,
					"source":            c.Source,
					"credibility_score": c.CredibilityScore,
					"quality_score":     c.QualityScore,
				})
			}
			ctxMap["citations"] = out
		}
		return ctxMap
	}

	// Synthesize results (LLM-first)
	var synthesis activities.SynthesisResult
	didSynthesisLLM := false

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
				Query:              input.Query,
				AgentResults:       agentResults,
				Context:            buildContext(),
				CollectedCitations: collectedCitations,
				ParentWorkflowID:   workflowID,
			}).Get(ctx, &synthesis)
			if err != nil {
				logger.Error("Result synthesis failed", "error", err)
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}
			didSynthesisLLM = true
		}
	} else {
		var err error
		err = workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{
			Query:              input.Query,
			AgentResults:       agentResults,
			Context:            buildContext(),
			CollectedCitations: collectedCitations,
			ParentWorkflowID:   workflowID,
		}).Get(ctx, &synthesis)
		if err != nil {
			logger.Error("Result synthesis failed", "error", err)
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
		didSynthesisLLM = true
	}

	// Record synthesis token usage when LLM synthesis ran
	if didSynthesisLLM && synthesis.TokensUsed > 0 {
		inTok := synthesis.InputTokens
		outTok := synthesis.CompletionTokens
		if inTok == 0 && outTok > 0 {
			est := synthesis.TokensUsed - outTok
			if est > 0 {
				inTok = est
			}
		}
		recCtx := opts.WithTokenRecordOptions(ctx)
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       workflowID,
			AgentID:      "streaming_synthesis",
			Model:        synthesis.ModelUsed,
			Provider:     synthesis.Provider,
			InputTokens:  inTok,
			OutputTokens: outTok,
			Metadata: map[string]interface{}{
				"phase":    "synthesis",
				"workflow": "streaming",
			},
		}).Get(recCtx, nil)
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

	// Build metadata and include aggregate model/provider + token breakdown across streams
	meta := map[string]interface{}{
		"num_streams": len(results),
		"parallel":    true,
	}
	// Ensure each agent has input/output tokens (estimate 60/40 if missing)
	agentResultsForMeta := make([]activities.AgentExecutionResult, 0, len(agentResults))
	for _, r := range agentResults {
		ar := r
		if (ar.InputTokens == 0 && ar.OutputTokens == 0) && ar.TokensUsed > 0 {
			ar.InputTokens = ar.TokensUsed * 6 / 10
			ar.OutputTokens = ar.TokensUsed - ar.InputTokens
		}
		agentResultsForMeta = append(agentResultsForMeta, ar)
	}
	agentMeta := metadata.AggregateAgentMetadata(agentResultsForMeta, 0)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Add cost estimate if tokens available
	if totalTokens > 0 {
		model := ""
		if m, ok := meta["model"].(string); ok && m != "" {
			model = m
		}
		meta["cost_usd"] = pricing.CostForTokens(model, totalTokens)
	}

	// Check pause/cancel before completion
	if err := controlHandler.CheckPausePoint(ctx, "pre_completion"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Emit WORKFLOW_COMPLETED before returning
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "parallel-streaming",
		Message:    activities.MsgWorkflowCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     synthesis.FinalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}
