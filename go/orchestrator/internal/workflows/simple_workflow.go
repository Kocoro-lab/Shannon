package workflows

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

// SimpleTaskWorkflow handles simple, single-agent tasks efficiently
// This workflow minimizes events by using a single consolidated activity
func SimpleTaskWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting SimpleTaskWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
		"session_id", input.SessionID,
	)

	// Determine workflow ID for event streaming
	// Use parent workflow ID if this is a child workflow, otherwise use own ID
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Emit workflow started event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "simple-agent",
		Message:    "Starting simple task workflow",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Emit thinking event
	truncatedQuery := input.Query
	if len(truncatedQuery) > 80 {
		truncatedQuery = truncatedQuery[:77] + "..."
	}
    _ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
        WorkflowID: workflowID,
        EventType:  activities.StreamEventAgentThinking,
        AgentID:    "simple-agent",
        Message:    "Thinking: " + truncatedQuery,
        Timestamp:  workflow.Now(ctx),
    }).Get(ctx, nil)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute, // Simple tasks should be fast
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2, // Fewer retries for simple tasks
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Emit agent started event
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentStarted,
		AgentID:    "simple-agent",
		Message:    "Processing query",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

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
				RecentTopK:   5,    // Fixed for determinism
				SemanticTopK: 5,    // Fixed for determinism
				Threshold:    0.75, // Fixed semantic threshold
			}).Get(ctx, &hierMemory)

		if len(hierMemory.Items) > 0 {
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["agent_memory"] = hierMemory.Items
			// Emit memory recall metadata (no content)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventDataProcessing,
				AgentID:    "simple-agent",
				Message:    activities.MsgMemoryRecalled(len(hierMemory.Items)),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
			logger.Info("Injected hierarchical memory into context",
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
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["agent_memory"] = sessionMemory.Items
			// Emit memory recall metadata (no content)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventDataProcessing,
				AgentID:    "simple-agent",
				Message:    activities.MsgMemoryRecalled(len(sessionMemory.Items)),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
			logger.Info("Injected session memory into context",
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
		// Determine model tier from context or per-agent budget
		modelTier := deriveModelTier(input.Context)
		if tier, ok := input.Context["model_tier"].(string); ok {
			modelTier = tier
		}

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       modelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression",
				"session_id", input.SessionID,
				"reason", checkResult.Reason,
				"message_count", len(input.History),
			)

			// Compress context via activity
			var compressResult activities.CompressContextResult
			err = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext,
				activities.CompressContextInput{
					SessionID:        input.SessionID,
					History:          convertHistoryMapForCompression(input.History),
					TargetTokens:     int(float64(activities.GetModelWindowSize(modelTier)) * 0.375), // Compress to half of 75%
					ParentWorkflowID: workflowID,
				}).Get(ctx, &compressResult)

			if err == nil && compressResult.Summary != "" && compressResult.Stored {
				logger.Info("Context compressed and stored",
					"session_id", input.SessionID,
					"summary_length", len(compressResult.Summary),
				)

				// Emit compression applied (metadata only)
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: workflowID,
					EventType:  activities.StreamEventDataProcessing,
					AgentID:    "simple-agent",
					Message:    activities.MsgCompressionApplied(),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)

				// Update compression state in session
				var updateResult activities.UpdateCompressionStateResult
				_ = workflow.ExecuteActivity(ctx, "UpdateCompressionStateActivity",
					activities.UpdateCompressionStateInput{
						SessionID:    input.SessionID,
						MessageCount: len(input.History),
					}).Get(ctx, &updateResult)

				if updateResult.Updated {
					logger.Info("Compression state updated in session",
						"session_id", input.SessionID,
					)
				}
			}
		} else if err == nil {
			logger.Debug("Compression not needed",
				"session_id", input.SessionID,
				"reason", checkResult.Reason,
			)
		}
	}

	// Prepare history for agent; optionally inject summary and use sliding window when compressed
	historyForAgent := convertHistoryForAgent(input.History)

	// If compression was performed earlier in this workflow and produced a summary,
	// add it to context and shape history to primers+recents
	// Note: This piggybacks on the compression block above; we also re-check here in case
	// no prior compression happened but history is still large.
	if input.SessionID != "" && compressionVersion >= 1 {
		// Estimate tokens and, if needed, perform on-the-fly compression for the middle section
		estimatedTokens := activities.EstimateTokens(historyForAgent)
		// Use the same model tier determination for consistency
		modelTier := deriveModelTier(input.Context)
		if tier, ok := input.Context["model_tier"].(string); ok {
			modelTier = tier
		}
		window := activities.GetModelWindowSize(modelTier)
		trig, tgt := getCompressionRatios(input.Context, 0.75, 0.375)
		if estimatedTokens >= int(float64(window)*trig) {
			// Compress and store context to get a summary
			var compressResult activities.CompressContextResult
			_ = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext,
				activities.CompressContextInput{
					SessionID:        input.SessionID,
					History:          convertHistoryMapForCompression(input.History),
					TargetTokens:     int(float64(window) * tgt),
					ParentWorkflowID: workflowID,
				},
			).Get(ctx, &compressResult)
			if compressResult.Summary != "" {
				if input.Context == nil {
					input.Context = make(map[string]interface{})
				}
				input.Context["context_summary"] = fmt.Sprintf("Previous context summary: %s", compressResult.Summary)
				prim, rec := getPrimersRecents(input.Context, 3, 20)
				shaped := shapeHistory(input.History, prim, rec)
				historyForAgent = convertHistoryForAgent(shaped)
				// Emit summary injected (metadata only)
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: workflowID,
					EventType:  activities.StreamEventDataProcessing,
					AgentID:    "simple-agent",
					Message:    activities.MsgSummaryAdded(),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
			}
		}
	}

	// Execute the consolidated simple task activity
	// This single activity handles everything: agent execution, session update, etc.
	var result activities.ExecuteSimpleTaskResult
	err := workflow.ExecuteActivity(ctx, activities.ExecuteSimpleTask, activities.ExecuteSimpleTaskInput{
		Query:            input.Query,
		UserID:           input.UserID,
		SessionID:        input.SessionID,
		Context:          input.Context,
		SessionCtx:       input.SessionCtx,
		History:          historyForAgent,
		SuggestedTools:   input.SuggestedTools,
		ToolParameters:   input.ToolParameters,
		ParentWorkflowID: workflowID,
	}).Get(ctx, &result)

	if err != nil {
		logger.Error("Simple task execution failed", "error", err)
		// Emit error event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventErrorOccurred,
			AgentID:    "simple-agent",
			Message:    "Task execution failed: " + err.Error(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Persist agent execution using fire-and-forget Temporal activity
	if result.Success {
		detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
		persistOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
		}
		detachedCtx = workflow.WithActivityOptions(detachedCtx, persistOpts)

		// Persist agent execution
		workflow.ExecuteActivity(detachedCtx,
			activities.PersistAgentExecutionStandalone,
			activities.PersistAgentExecutionInput{
				WorkflowID: workflowID,
				AgentID:    "simple-agent",
				Input:      input.Query,
				Output:     result.Response,
				State:      "COMPLETED",
				TokensUsed: result.TokensUsed,
				ModelUsed:  result.ModelUsed,
				DurationMs: result.DurationMs,
				Error:      result.Error,
				Metadata: map[string]interface{}{
					"workflow": "simple",
					"strategy": "simple",
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
					AgentID:    "simple-agent",
					ToolName:   tool.Tool,
					Output:     outputStr,
					Success:    tool.Success,
					Error:      tool.Error,
				})
		}
	}

	// Persist to vector store for future context retrieval (fire-and-forget)
	if input.SessionID != "" {
		ro := workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3}}
		detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
		dctx := workflow.WithActivityOptions(detachedCtx, ro)
		// Schedule without waiting on result
		workflow.ExecuteActivity(dctx, activities.RecordQuery, activities.RecordQueryInput{
			SessionID: input.SessionID,
			UserID:    input.UserID,
			Query:     input.Query,
			Answer:    result.Response,
			Model:     "simple-agent",
			Metadata:  map[string]interface{}{"workflow": "simple", "mode": "simple", "tenant_id": input.TenantID},
			RedactPII: true,
		})
	}

	// Update session with token usage
	if input.SessionID != "" {
		var sessionUpdateResult activities.SessionUpdateResult
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     result.Response,
				TokensUsed: result.TokensUsed,
				AgentsUsed: 1,
				ModelUsed:  result.ModelUsed,
			},
		).Get(ctx, &sessionUpdateResult)
		if err != nil {
			logger.Warn("Failed to update session with tokens",
				"session_id", input.SessionID,
				"error", err,
			)
		}

		// Session title generation is now handled centrally in OrchestratorWorkflow
		// Keep version gate for replay determinism (no-op for new executions)
		_ = workflow.GetVersion(ctx, "session_title_v1", workflow.DefaultVersion, 1)
	}

	// Check if we need synthesis for web_search or JSON results
	finalResult := result.Response
	totalTokens := result.TokensUsed

	// Determine if synthesis is needed
	needsSynthesis := false

	// Check if web_search was used
	if input.SuggestedTools != nil {
		for _, tool := range input.SuggestedTools {
			if strings.EqualFold(tool, "web_search") {
				needsSynthesis = true
				break
			}
		}
	}

	// Check if response looks like JSON
	if !needsSynthesis {
		trimmed := strings.TrimSpace(result.Response)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			needsSynthesis = true
		}
	}

	// Perform synthesis if needed
	if needsSynthesis && result.Success {
		logger.Info("Response appears to be web_search results or JSON, performing synthesis")

		// Convert to agent results format for synthesis
		agentResults := []activities.AgentExecutionResult{
			{
				AgentID:    "simple-agent",
				Response:   result.Response,
				Success:    true,
				TokensUsed: result.TokensUsed,
			},
		}

		var synthesis activities.SynthesisResult
		err = workflow.ExecuteActivity(ctx,
			activities.SynthesizeResultsLLM,
			activities.SynthesisInput{
				Query:            input.Query,
				AgentResults:     agentResults,
				Context:          input.Context,
				ParentWorkflowID: workflowID,
			},
		).Get(ctx, &synthesis)

		if err != nil {
			logger.Warn("Synthesis failed, using raw result", "error", err)
		} else {
			finalResult = synthesis.FinalResult
			totalTokens += synthesis.TokensUsed
			logger.Info("Synthesis completed", "additional_tokens", synthesis.TokensUsed)
		}
	}

	// Aggregate tool errors for user-facing metadata
	var toolErrors []map[string]string
	if len(result.ToolExecutions) > 0 {
		for _, te := range result.ToolExecutions {
			if !te.Success || (te.Error != "") {
				toolErrors = append(toolErrors, map[string]string{
					"agent_id": "simple-agent",
					"tool":     te.Tool,
					"error":    te.Error,
				})
			}
		}
	}

	// Record token usage for SimpleTaskWorkflow (best-effort)
	// Use a simple 60/40 split when detailed counts are unavailable
	if input.UserID != "" && totalTokens > 0 {
		recOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		}
		recCtx := workflow.WithActivityOptions(ctx, recOpts)
		inTok := totalTokens * 6 / 10
		outTok := totalTokens - inTok
		provider := detectProviderFromModel(result.ModelUsed)
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       workflowID, // may not be UUID; DB layer resolves via workflow_id when possible
			AgentID:      "simple-agent",
			Model:        result.ModelUsed,
			Provider:     provider,
			InputTokens:  inTok,
			OutputTokens: outTok,
			Metadata:     map[string]interface{}{"workflow": "simple"},
		}).Get(ctx, nil)
	}

	logger.Info("SimpleTaskWorkflow completed successfully",
		"tokens_used", totalTokens,
	)

	// Emit completion event
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentCompleted,
		AgentID:    "simple-agent",
    Message:    "Task done",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Emit workflow completed event for dashboards
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "simple-agent",
    Message:    "All done",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	meta := map[string]interface{}{
		"mode":       "simple",
		"num_agents": 1,
	}
	if len(toolErrors) > 0 {
		meta["tool_errors"] = toolErrors
	}

	// Add model and provider information for task persistence
	if result.ModelUsed != "" {
		meta["model"] = result.ModelUsed
		meta["model_used"] = result.ModelUsed
		meta["provider"] = detectProviderFromModel(result.ModelUsed)
	}

	// Add token breakdown (60/40 split for prompt/completion)
	if totalTokens > 0 {
		inputTokens := totalTokens * 6 / 10
		outputTokens := totalTokens - inputTokens
		meta["input_tokens"] = inputTokens
		meta["output_tokens"] = outputTokens
		meta["total_tokens"] = totalTokens

		// Calculate cost (rough estimate, actual cost calculated by service layer)
		if result.ModelUsed != "" {
			cost := float64(totalTokens) * 0.0000005 // Default fallback rate
			meta["cost_usd"] = cost
		}
	}

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}

// detectProviderFromModel determines the provider based on the model name
// deriveModelTier intelligently determines the model tier based on context
// Priority: explicit model_tier > per-agent budget > default "medium"
func deriveModelTier(ctx map[string]interface{}) string {
	if ctx == nil {
		return "medium"
	}

	// First check for explicit model_tier
	if tier, ok := ctx["model_tier"].(string); ok && tier != "" {
		return tier
	}

	// Derive from per-agent budget if available
	if budget, ok := ctx["token_budget_per_agent"].(int); ok {
		return modelTierFromBudget(budget)
	}
	if budget, ok := ctx["token_budget_per_agent"].(float64); ok {
		return modelTierFromBudget(int(budget))
	}

	// Check budget_agent_max (set by orchestrator router)
	if budget, ok := ctx["budget_agent_max"].(int); ok {
		return modelTierFromBudget(budget)
	}
	if budget, ok := ctx["budget_agent_max"].(float64); ok {
		return modelTierFromBudget(int(budget))
	}

	// Default to medium for simple tasks
	return "medium"
}

// modelTierFromBudget maps token budget to appropriate model tier
func modelTierFromBudget(budget int) string {
	switch {
	case budget <= 8000:
		return "small" // 8k window models
	case budget <= 32000:
		return "medium" // 32k window models
	case budget <= 128000:
		return "large" // 128k window models
	default:
		return "xlarge" // 200k+ window models
	}
}
