package strategies

import (
    "fmt"
    "time"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"
)

// ExploratoryWorkflow implements iterative discovery with hypothesis testing using patterns
// This workflow explores a problem space through tree-of-thoughts pattern for systematic exploration
func ExploratoryWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ExploratoryWorkflow with patterns",
		"query", input.Query,
		"user_id", input.UserID,
	)

	// Input validation
	if err := validateInput(input); err != nil {
		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Load configuration
	config := getWorkflowConfig(ctx)

	// Prepare pattern options
	opts := patterns.Options{
		UserID:         input.UserID,
		BudgetAgentMax: getBudgetMax(input.Context),
		ModelTier:      determineModelTier(input.Context, "medium"),
	}

	// Phase 1: Use Tree-of-Thoughts for systematic exploration
	totConfig := patterns.TreeOfThoughtsConfig{
		MaxDepth:          config.ExploratoryMaxIterations,
		BranchingFactor:   config.ExploratoryBranchFactor,
		EvaluationMethod:  "scoring",
		PruningThreshold:  1.0 - config.ExploratoryConfidenceThreshold, // Invert for pruning
		ExplorationBudget: config.ExploratoryMaxIterations * config.ExploratoryBranchFactor,
		BacktrackEnabled:  true,
		ModelTier:         opts.ModelTier,
	}

	logger.Info("Starting Tree-of-Thoughts exploration",
		"max_depth", totConfig.MaxDepth,
		"branching_factor", totConfig.BranchingFactor,
	)

	// Ensure parent workflow ID is available in context passed to patterns
	ctxMap := make(map[string]interface{})
	for k, v := range input.Context {
		ctxMap[k] = v
	}
	if input.ParentWorkflowID != "" {
		ctxMap["parent_workflow_id"] = input.ParentWorkflowID
	}

	// Memory retrieval with gate precedence (hierarchical > simple session)
	hierarchicalVersion := workflow.GetVersion(ctx, "memory_retrieval_v1", workflow.DefaultVersion, 1)
	sessionVersion := workflow.GetVersion(ctx, "session_memory_v1", workflow.DefaultVersion, 1)

	var memoryItems []map[string]interface{}
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
			memoryItems = hierMemory.Items
			ctxMap["agent_memory"] = memoryItems
			logger.Info("Injected hierarchical memory into exploratory ToT context",
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
			memoryItems = sessionMemory.Items
			ctxMap["agent_memory"] = memoryItems
			logger.Info("Injected session memory into exploratory ToT context",
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

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       opts.ModelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in exploratory workflow",
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
					TargetTokens:     int(float64(activities.GetModelWindowSize(opts.ModelTier)) * 0.375),
					ParentWorkflowID: input.ParentWorkflowID,
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
			}
		}
	}

	totResult, err := patterns.TreeOfThoughts(
		ctx,
		input.Query,
		ctxMap,
		input.SessionID,
		convertHistoryForAgent(input.History),
		totConfig,
		opts,
	)

	if err != nil {
		logger.Error("Tree-of-Thoughts exploration failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Exploration failed: %v", err),
		}, err
	}

	// Phase 2: If confidence is low, apply Debate pattern on top findings
	finalResult := totResult.BestSolution
	totalTokens := totResult.TotalTokens
	finalConfidence := totResult.Confidence

	if totResult.Confidence < config.ExploratoryConfidenceThreshold {
		logger.Info("Confidence below threshold, applying Debate pattern",
			"current_confidence", totResult.Confidence,
			"threshold", config.ExploratoryConfidenceThreshold,
		)

		// Extract top perspectives from tree exploration
		perspectives := []string{}
		if totResult.ExplorationTree != nil {
			for i := range totResult.ExplorationTree.Children {
				if i >= 3 {
					break // Limit to 3 perspectives
				}
				perspectives = append(perspectives, fmt.Sprintf("perspective_%d", i+1))
			}
		}

		debateConfig := patterns.DebateConfig{
			NumDebaters:      len(perspectives),
			MaxRounds:        2,
			Perspectives:     perspectives,
			RequireConsensus: false,
			ModeratorEnabled: true,
			VotingEnabled:    false,
			ModelTier:        opts.ModelTier,
		}

		// Prepare debate context with exploration findings
		debateContext := make(map[string]interface{})
		for k, v := range input.Context {
			debateContext[k] = v
		}
		if input.ParentWorkflowID != "" {
			debateContext["parent_workflow_id"] = input.ParentWorkflowID
		}
		debateContext["exploration_findings"] = totResult.BestPath

		// Inject memory into debate context (reuse from earlier fetch)
		if len(memoryItems) > 0 {
			debateContext["agent_memory"] = memoryItems
		}

		debateResult, err := patterns.Debate(
			ctx,
			fmt.Sprintf("Based on exploration findings, what is the best answer to: %s", input.Query),
			debateContext,
			input.SessionID,
			convertHistoryForAgent(input.History),
			debateConfig,
			opts,
		)

		if err == nil {
			finalResult = debateResult.FinalPosition
			totalTokens += debateResult.TotalTokens
			finalConfidence = 0.8 // Debate increases confidence
			logger.Info("Debate enhanced the exploration result")
		} else {
			logger.Warn("Debate pattern failed, using tree-of-thoughts result", "error", err)
		}
	}

	// Phase 3: Apply Reflection pattern for final quality check
	if finalConfidence < 0.9 {
		logger.Info("Applying reflection for final quality improvement")

		reflectionConfig := patterns.ReflectionConfig{
			Enabled:             true,
			MaxRetries:          2,
			ConfidenceThreshold: 0.9,
			Criteria:            []string{"clarity", "completeness", "accuracy"},
			TimeoutMs:           30000,
		}

		// Create mock agent results for reflection
		agentResults := []activities.AgentExecutionResult{
			{
				Response:   finalResult,
				Success:    true,
				TokensUsed: totalTokens,
			},
		}

        reflectedResult, reflectedConfidence, reflectionTokens, err := patterns.ReflectOnResult(
            ctx,
            input.Query,
            finalResult,
            agentResults,
            ctxMap,
            reflectionConfig,
            opts,
        )

		if err == nil {
			finalResult = reflectedResult
			finalConfidence = reflectedConfidence
			totalTokens += reflectionTokens
			logger.Info("Reflection improved final result", "new_confidence", finalConfidence)
		} else {
			logger.Warn("Reflection failed, using previous result", "error", err)
		}
	}

	// Optional: append Sources when citations provided in context
	if v, ok := input.Context["enable_citations"].(bool); ok && v {
		if citationList, ok2 := ctxMap["available_citations"].(string); ok2 && citationList != "" {
			finalResult = formatting.FormatReportWithCitations(finalResult, citationList)
		}
	}

	// Update session
	if input.SessionID != "" {
		if err := updateSession(ctx, input.SessionID, finalResult, totalTokens, totResult.TotalThoughts); err != nil {
			logger.Warn("Failed to update session",
				"error", err,
				"session_id", input.SessionID,
			)
		}
	}

	logger.Info("ExploratoryWorkflow completed",
		"total_tokens", totalTokens,
		"final_confidence", finalConfidence,
		"total_thoughts_explored", totResult.TotalThoughts,
		"tree_depth", totResult.TreeDepth,
	)

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
		AgentID:    "exploratory",
		Message:    activities.MsgWorkflowCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Build metadata with agent information
	meta := map[string]interface{}{
		"workflow_type":      "exploratory",
		"pattern_used":       "tree_of_thoughts",
		"total_thoughts":     totResult.TotalThoughts,
		"tree_depth":         totResult.TreeDepth,
		"final_confidence":   finalConfidence,
		"debate_applied":     totResult.Confidence < config.ExploratoryConfidenceThreshold,
		"reflection_applied": finalConfidence < 0.9,
	}

	// Aggregate agent metadata (model, provider, tokens, cost)
	// Resolve actual model from tier using config (pattern doesn't track model used)
	actualModel := getPriorityModelForTier(opts.ModelTier)
	agentResults := []activities.AgentExecutionResult{
		{
			AgentID:      "exploratory-agent",
			ModelUsed:    actualModel,
			TokensUsed:   totResult.TotalTokens,
			InputTokens:  totResult.TotalTokens * 6 / 10, // Estimate 60/40 split
			OutputTokens: totResult.TotalTokens * 4 / 10,
			Success:      true,
		},
	}
	reflectionTokensCount := totalTokens - totResult.TotalTokens // Approximate reflection tokens
	agentMeta := metadata.AggregateAgentMetadata(agentResults, reflectionTokensCount)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Align: compute and include estimated cost using centralized pricing
	if totalTokens > 0 {
		metaModel := ""
		if m, ok := meta["model"].(string); ok && m != "" {
			metaModel = m
		}
		meta["cost_usd"] = pricing.CostForTokens(metaModel, totalTokens)
	}

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}
