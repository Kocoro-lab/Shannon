package strategies

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
)

// ReactWorkflow uses the extracted React pattern for step-by-step problem solving.
// It leverages the Reason-Act-Observe loop with optional reflection.
func ReactWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ReactWorkflow with pattern",
		"query", input.Query,
		"session_id", input.SessionID,
		"version", "v2",
	)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Load configuration
	var config activities.WorkflowConfig
	configActivity := workflow.ExecuteActivity(ctx,
		activities.GetWorkflowConfig,
	)
	if err := configActivity.Get(ctx, &config); err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		config = activities.WorkflowConfig{
			ReactMaxIterations:     10,
			ReactObservationWindow: 3,
		}
	}

	// Prepare base context
	baseContext := make(map[string]interface{})
	for k, v := range input.Context {
		baseContext[k] = v
	}
	for k, v := range input.SessionCtx {
		baseContext[k] = v
	}
	if input.ParentWorkflowID != "" {
		baseContext["parent_workflow_id"] = input.ParentWorkflowID
	}

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
			baseContext["agent_memory"] = hierMemory.Items
			logger.Info("Injected hierarchical memory into React context",
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
			baseContext["agent_memory"] = sessionMemory.Items
			logger.Info("Injected session memory into React context",
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
		tierForCompression := "medium"

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       tierForCompression,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in React workflow",
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
					TargetTokens:     int(float64(activities.GetModelWindowSize(tierForCompression)) * 0.375),
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

	// Check for budget configuration
	agentMaxTokens := 0
	if v, ok := baseContext["budget_agent_max"].(int); ok {
		agentMaxTokens = v
	}
	if v, ok := baseContext["budget_agent_max"].(float64); ok && v > 0 {
		agentMaxTokens = int(v)
	}

	// Determine model tier based on query complexity
	modelTier := "medium" // Default for React tasks
	// Resolve an approximate concrete model for pricing/metadata; prefer provider override from context
	providerOverride := ""
	if v, ok := baseContext["provider_override"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := baseContext["provider"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := baseContext["llm_provider"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	}
	approxModel := ""
	if providerOverride != "" {
		approxModel = pricing.GetPriorityModelForProvider(modelTier, providerOverride)
	}
	if approxModel == "" {
		approxModel = pricing.GetPriorityOneModel(modelTier)
	}
	if approxModel == "" {
		approxModel = modelTier
	}

	// Configure React pattern
	reactConfig := patterns.ReactConfig{
		MaxIterations:     config.ReactMaxIterations,
		ObservationWindow: config.ReactObservationWindow,
		MaxObservations:   100, // Safety limit
		MaxThoughts:       50,  // Safety limit
		MaxActions:        50,  // Safety limit
	}

	reactOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		UserID:         input.UserID,
		EmitEvents:     true,
		ModelTier:      modelTier,
		Context:        baseContext,
	}

	// Execute React loop
	logger.Info("Executing React loop pattern",
		"max_iterations", reactConfig.MaxIterations,
		"observation_window", reactConfig.ObservationWindow,
	)

	reactResult, err := patterns.ReactLoop(
		ctx,
		input.Query,
		baseContext,
		input.SessionID,
		convertHistoryForAgent(input.History),
		reactConfig,
		reactOpts,
	)

	if err != nil {
		logger.Error("React loop failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("React loop failed: %v", err),
		}, err
	}

	// Optional: Apply reflection for quality improvement on complex results
	finalResult := reactResult.FinalResult
	qualityScore := 0.5
	totalTokens := reactResult.TotalTokens
	reflectionTokens := 0

	if reactResult.Iterations > 5 { // Complex task that needed many iterations
		logger.Info("Applying reflection for quality improvement",
			"iterations", reactResult.Iterations,
		)

		reflectionConfig := patterns.ReflectionConfig{
			Enabled:             true,
			MaxRetries:          1, // Single reflection pass for React
			ConfidenceThreshold: 0.7,
			Criteria:            []string{"completeness", "correctness", "clarity"},
			TimeoutMs:           30000,
		}

		reflectionOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			ModelTier:      modelTier,
		}

		// Convert React result to agent result for reflection
		approxModelRefl := pricing.GetPriorityOneModel(modelTier)
		if approxModelRefl == "" {
			approxModelRefl = modelTier
		}
		agentResults := []activities.AgentExecutionResult{
			{
				AgentID:    "react-agent",
				Response:   reactResult.FinalResult,
				TokensUsed: reactResult.TotalTokens,
				Success:    true,
				ModelUsed:  approxModelRefl,
			},
		}

		improvedResult, score, reflTokens, err := patterns.ReflectOnResult(
			ctx,
			input.Query,
			reactResult.FinalResult,
			agentResults,
			baseContext,
			reflectionConfig,
			reflectionOpts,
		)

		if err == nil {
			finalResult = improvedResult
			qualityScore = score
			reflectionTokens = reflTokens
			totalTokens += reflectionTokens
			logger.Info("Reflection improved quality",
				"score", qualityScore,
				"tokens", reflectionTokens,
			)
		} else {
			logger.Warn("Reflection failed, using original result", "error", err)
		}
	}

	// Optional: collect citations and append Sources section when enabled
	var collectedCitations []metadata.Citation
	if v, ok := baseContext["enable_citations"].(bool); ok && v {
		var resultsForCitations []interface{}
		for _, ar := range reactResult.AgentResults {
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
		now := workflow.Now(ctx)
		citations, _ := metadata.CollectCitations(resultsForCitations, now, 0)
		if len(citations) > 0 {
			collectedCitations = citations
			var b strings.Builder
			for i, c := range citations {
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
			citationList := strings.TrimRight(b.String(), "\n")
			finalResult = formatting.FormatReportWithCitations(finalResult, citationList)
		}
	}

	// Update session with results (include per-agent usage for accurate cost)
	if input.SessionID != "" {
		var updRes activities.SessionUpdateResult
		// Build per-agent usage from pattern loop results
		usages := make([]activities.AgentUsage, 0, len(reactResult.AgentResults))
		for _, ar := range reactResult.AgentResults {
			usages = append(usages, activities.AgentUsage{Model: ar.ModelUsed, Tokens: ar.TokensUsed, InputTokens: ar.InputTokens, OutputTokens: ar.OutputTokens})
		}
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: totalTokens,
				AgentsUsed: reactResult.Iterations * 2, // Reasoner + Actor per iteration
				AgentUsage: usages,
			}).Get(ctx, &updRes)

		if err != nil {
			logger.Error("Failed to update session", "error", err)
		}

		// Persist to vector store (fire-and-forget)
		detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
		workflow.ExecuteActivity(detachedCtx,
			activities.RecordQuery,
			activities.RecordQueryInput{
				SessionID: input.SessionID,
				UserID:    input.UserID,
				Query:     input.Query,
				Answer:    finalResult,
				Model:     approxModel,
				Metadata: map[string]interface{}{
					"workflow":      "react_v2",
					"iterations":    reactResult.Iterations,
					"quality_score": qualityScore,
					"thoughts":      len(reactResult.Thoughts),
					"actions":       len(reactResult.Actions),
					"observations":  len(reactResult.Observations),
					"tenant_id":     input.TenantID,
				},
				RedactPII: true,
			})
	}

	logger.Info("ReactWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"iterations", reactResult.Iterations,
	)

	// Record pattern metrics (fire-and-forget)
	metricsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})
	_ = workflow.ExecuteActivity(metricsCtx, "RecordPatternMetrics", activities.PatternMetricsInput{
		Pattern:      "react",
		Version:      "v2",
		AgentCount:   reactResult.Iterations * 2, // Reasoner + Actor per iteration
		TokensUsed:   totalTokens,
		WorkflowType: "react",
	}).Get(ctx, nil)

	// Aggregate tool errors from React agent results
	var toolErrors []map[string]string
	for _, ar := range reactResult.AgentResults {
		if len(ar.ToolExecutions) == 0 {
			continue
		}
		for _, te := range ar.ToolExecutions {
			if !te.Success || (te.Error != "") {
				toolErrors = append(toolErrors, map[string]string{
					"agent_id": ar.AgentID,
					"tool":     te.Tool,
					"error":    te.Error,
				})
			}
		}
	}

	meta := map[string]interface{}{
		"version":       "v2",
		"iterations":    reactResult.Iterations,
		"quality_score": qualityScore,
		"thoughts":      len(reactResult.Thoughts),
		"actions":       len(reactResult.Actions),
		"observations":  len(reactResult.Observations),
	}
	if len(collectedCitations) > 0 {
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
		meta["citations"] = out
	}
	if len(toolErrors) > 0 {
		meta["tool_errors"] = toolErrors
	}

	// Aggregate agent metadata (model, provider, tokens, cost)
	// Note: ReactResult doesn't track per-iteration models; use tier's priority-1 model
	agentResults := []activities.AgentExecutionResult{
		{
			AgentID:      "react-agent",
			ModelUsed:    approxModel,
			TokensUsed:   reactResult.TotalTokens,
			InputTokens:  reactResult.TotalTokens * 6 / 10, // Estimate 60/40 split
			OutputTokens: reactResult.TotalTokens * 4 / 10,
			Success:      true,
		},
	}
	agentMeta := metadata.AggregateAgentMetadata(agentResults, reflectionTokens)
	for k, v := range agentMeta {
		meta[k] = v
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
		AgentID:    "react",
		Message:    "All done",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}
