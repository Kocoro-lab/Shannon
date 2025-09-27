package workflows

import (
	"encoding/json"
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
		Message:    "Analyzing: " + truncatedQuery,
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
				RecentTopK:   5,   // Fixed for determinism
				SemanticTopK: 5,   // Fixed for determinism
				Threshold:    0.75, // Fixed semantic threshold
			}).Get(ctx, &hierMemory)

		if len(hierMemory.Items) > 0 {
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["agent_memory"] = hierMemory.Items
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
		modelTier := "medium" // Default for simple tasks
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
					SessionID:    input.SessionID,
					History:      convertHistoryMapForCompression(input.History),
					TargetTokens: int(float64(activities.GetModelWindowSize(modelTier)) * 0.375), // Compress to half of 75%
					ParentWorkflowID: workflowID,
				}).Get(ctx, &compressResult)

			if err == nil && compressResult.Summary != "" && compressResult.Stored {
				logger.Info("Context compressed and stored",
					"session_id", input.SessionID,
					"summary_length", len(compressResult.Summary),
				)

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

	// Execute the consolidated simple task activity
	// This single activity handles everything: agent execution, session update, etc.
	var result activities.ExecuteSimpleTaskResult
	err := workflow.ExecuteActivity(ctx, activities.ExecuteSimpleTask, activities.ExecuteSimpleTaskInput{
		Query:          input.Query,
		UserID:         input.UserID,
		SessionID:      input.SessionID,
		Context:        input.Context,
		SessionCtx:     input.SessionCtx,
		History:        convertHistoryForAgent(input.History),
		SuggestedTools: input.SuggestedTools,
		ToolParameters: input.ToolParameters,
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
                Query:        input.Query,
                AgentResults: agentResults,
                Context:      input.Context,
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

	logger.Info("SimpleTaskWorkflow completed successfully",
		"tokens_used", totalTokens,
	)

	// Emit completion event
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventAgentCompleted,
		AgentID:    "simple-agent",
		Message:    "Task completed successfully",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Emit workflow completed event for dashboards
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "simple-agent",
		Message:    "Workflow completed",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"mode":       "simple",
			"num_agents": 1,
		},
	}, nil
}
