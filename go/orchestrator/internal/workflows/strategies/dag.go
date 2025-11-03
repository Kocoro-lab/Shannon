package strategies

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

// DAGWorkflow uses extracted patterns for cleaner multi-agent orchestration.
// It composes parallel/sequential/hybrid execution patterns with optional reflection.
func DAGWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting DAGWorkflow with composed patterns",
		"query", input.Query,
		"session_id", input.SessionID,
		"version", "v2",
	)

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Load workflow configuration
	var config activities.WorkflowConfig
	configActivity := workflow.ExecuteActivity(ctx, activities.GetWorkflowConfig)
	if err := configActivity.Get(ctx, &config); err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		// Use defaults if config load fails
		config = activities.WorkflowConfig{
			SimpleThreshold:               0.3,
			MaxParallelAgents:             5,
			ReflectionEnabled:             true,
			ReflectionMaxRetries:          2,
			ReflectionConfidenceThreshold: 0.8,
			ParallelMaxConcurrency:        5,
			HybridDependencyTimeout:       360,
			SequentialPassResults:         true,
			SequentialExtractNumeric:      true,
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
	// Propagate parent workflow ID to downstream activities (pattern helpers)
	if input.ParentWorkflowID != "" {
		baseContext["parent_workflow_id"] = input.ParentWorkflowID
	}

	// Step 1: Decompose the task (use preplanned plan if provided)
	var decomp activities.DecompositionResult
	var err error
	if input.PreplannedDecomposition != nil {
		decomp = *input.PreplannedDecomposition
	} else {
		err = workflow.ExecuteActivity(ctx,
			constants.DecomposeTaskActivity,
			activities.DecompositionInput{
				Query:          input.Query,
				Context:        baseContext,
				AvailableTools: []string{},
			}).Get(ctx, &decomp)

		if err != nil {
			logger.Error("Task decomposition failed", "error", err)
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Failed to decompose task: %v", err),
			}, err
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

	modelTier := determineModelTier(baseContext, "medium")
	var totalTokens int
	var agentResults []activities.AgentExecutionResult

	// Step 2: Check if task needs tools or has dependencies
	needsTools := false
	for _, subtask := range decomp.Subtasks {
		if len(subtask.SuggestedTools) > 0 || len(subtask.Dependencies) > 0 || len(subtask.Produces) > 0 || len(subtask.Consumes) > 0 {
			needsTools = true
			break
		}
		if subtask.ToolParameters != nil && len(subtask.ToolParameters) > 0 {
			needsTools = true
			break
		}
	}

	// Simple detection:
	// - Fallback to simple if decomposition returned zero subtasks (LLM/schema hiccup)
	// - Otherwise, only treat as simple when no tools are needed AND it's trivial AND below threshold
	//   A single tool-based subtask should use the pattern path, not the simple activity
	simpleByShape := len(decomp.Subtasks) == 0 || (len(decomp.Subtasks) == 1 && !needsTools)
	isSimple := len(decomp.Subtasks) == 0 || (decomp.ComplexityScore < config.SimpleThreshold && simpleByShape)

	// Step 3: Handle simple tasks directly (no tools, trivial plan)
	if isSimple {
		logger.Info("Executing simple task",
			"complexity", decomp.ComplexityScore,
			"subtasks", len(decomp.Subtasks),
		)

		// Execute single agent
		var simpleResult activities.ExecuteSimpleTaskResult
		err = workflow.ExecuteActivity(ctx,
			activities.ExecuteSimpleTask,
			activities.ExecuteSimpleTaskInput{
				Query:            input.Query,
				SessionID:        input.SessionID,
				UserID:           input.UserID,
				Context:          baseContext,
				SessionCtx:       input.SessionCtx,
				ParentWorkflowID: input.ParentWorkflowID,
			}).Get(ctx, &simpleResult)

		if err != nil {
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Simple execution failed: %v", err),
			}, err
		}

		agentResults = append(agentResults, activities.AgentExecutionResult{
			AgentID:    "simple-agent",
			Response:   simpleResult.Response,
			TokensUsed: simpleResult.TokensUsed,
			Success:    simpleResult.Success,
		})
		totalTokens = simpleResult.TokensUsed

		// Update session
		if input.SessionID != "" {
			_ = updateSessionWithAgentUsage(ctx, input.SessionID, simpleResult.Response, totalTokens, 1, []activities.AgentExecutionResult{{AgentID: "simple-agent", TokensUsed: simpleResult.TokensUsed, ModelUsed: simpleResult.ModelUsed, Success: true, Response: simpleResult.Response}})
			_ = recordToVectorStore(ctx, input, simpleResult.Response, "simple", decomp.ComplexityScore)
		}

		return TaskResult{
			Result:     simpleResult.Response,
			Success:    true,
			TokensUsed: totalTokens,
			Metadata: map[string]interface{}{
				"complexity_score": decomp.ComplexityScore,
				"mode":             "simple",
				"num_agents":       1,
			},
		}, nil
	}

	// Step 3: Complex multi-agent execution
	logger.Info("Executing complex task with patterns",
		"complexity", decomp.ComplexityScore,
		"subtasks", len(decomp.Subtasks),
		"strategy", decomp.ExecutionStrategy,
	)

	// Emit workflow started event
	emitTaskUpdate(ctx, input, activities.StreamEventWorkflowStarted, "", "")

	// Determine execution strategy
	hasDependencies := false
	for _, subtask := range decomp.Subtasks {
		if len(subtask.Dependencies) > 0 {
			hasDependencies = true
			break
		}
	}

	// Choose execution pattern based on strategy and dependencies
	execStrategy := decomp.ExecutionStrategy
	if execStrategy == "" {
		execStrategy = "parallel"
	}

	if hasDependencies {
		// Use hybrid execution for dependency management
		logger.Info("Using hybrid execution pattern for dependencies")
		agentResults, totalTokens = executeHybridPattern(
			ctx, decomp, input, baseContext, agentMaxTokens, modelTier, config,
		)
	} else if execStrategy == "sequential" {
		// Use sequential execution
		logger.Info("Using sequential execution pattern")
		agentResults, totalTokens = executeSequentialPattern(
			ctx, decomp, input, baseContext, agentMaxTokens, modelTier, config,
		)
	} else {
		// Default to parallel execution
		logger.Info("Using parallel execution pattern")
		agentResults, totalTokens = executeParallelPattern(
			ctx, decomp, input, baseContext, agentMaxTokens, modelTier, config,
		)
	}

	// Step 4: Synthesize results
	logger.Info("Synthesizing agent results",
		"agent_count", len(agentResults),
	)

	var synthesis activities.SynthesisResult

	// Check if decomposition included a synthesis/summarization subtask
	// Prefer structured subtask type over brittle description matching
	hasSynthesisSubtask := false
	var synthesisTaskIdx int

	for i, subtask := range decomp.Subtasks {
		t := strings.ToLower(strings.TrimSpace(subtask.TaskType))
		if t == "synthesis" || t == "summarization" || t == "summary" || t == "synthesize" {
			hasSynthesisSubtask = true
			synthesisTaskIdx = i
			logger.Info("Detected synthesis subtask in decomposition",
				"task_id", subtask.ID,
				"task_type", subtask.TaskType,
				"index", i,
			)
		}
	}

	// Priority order for synthesis decision:
	// 1. BypassSingleResult (config-driven optimization)
	// 2. Synthesis subtask detection (respects user intent)
	// 3. Standard synthesis (default behavior)

	// Count successful results for bypass logic
	successfulCount := 0
	var singleSuccessResult activities.AgentExecutionResult
	for _, result := range agentResults {
		if result.Success {
			successfulCount++
			singleSuccessResult = result
		}
	}

	if input.BypassSingleResult && successfulCount == 1 {
		// Heuristic guard: if the single result likely needs synthesis (e.g., web_search JSON),
		// do not bypass — proceed to standard LLM synthesis for a user‑ready answer.
		shouldBypass := true
		// 1) If tools used include web_search, prefer synthesis for natural language output
		if len(singleSuccessResult.ToolsUsed) > 0 {
			for _, t := range singleSuccessResult.ToolsUsed {
				if strings.EqualFold(t, "web_search") {
					shouldBypass = false
					break
				}
			}
		}
		// 2) If response looks like raw JSON, avoid bypass
		if shouldBypass {
			trimmed := strings.TrimSpace(singleSuccessResult.Response)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				shouldBypass = false
			} else if strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
				// Handle JSON encoded as a quoted string
				var inner string
				if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
					innerTrim := strings.TrimSpace(inner)
					if strings.HasPrefix(innerTrim, "{") || strings.HasPrefix(innerTrim, "[") {
						shouldBypass = false
					}
				}
			}
		}
		// Enforce role-aware synthesis for data_analytics even with single result
		if shouldBypass && baseContext != nil {
			if role, ok := baseContext["role"].(string); ok && strings.EqualFold(role, "data_analytics") {
				shouldBypass = false
			}
		}

		if shouldBypass {
			// Single success bypass - skip synthesis entirely for efficiency
			// Works for both sequential (1 result) and parallel (1 success among N) modes
			synthesis = activities.SynthesisResult{
				FinalResult: singleSuccessResult.Response,
				TokensUsed:  0, // No synthesis performed here
			}
			logger.Info("Bypassing synthesis for single successful result",
				"agent_id", singleSuccessResult.AgentID,
				"total_agents", len(agentResults),
				"successful", successfulCount,
			)
		} else {
			// Fall through to standard synthesis below
			logger.Info("Single result requires synthesis (web_search/JSON detected)")
			err = workflow.ExecuteActivity(ctx,
				activities.SynthesizeResultsLLM,
				activities.SynthesisInput{
					Query:            input.Query,
					AgentResults:     agentResults,
					Context:          baseContext,
					ParentWorkflowID: input.ParentWorkflowID,
				},
			).Get(ctx, &synthesis)

			if err != nil {
				logger.Error("Synthesis failed", "error", err)
				return TaskResult{
					Success:      false,
					ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
				}, err
			}
			totalTokens += synthesis.TokensUsed
		}
	} else if hasSynthesisSubtask && synthesisTaskIdx >= 0 && synthesisTaskIdx < len(agentResults) && agentResults[synthesisTaskIdx].Success {
		// Use the synthesis subtask's result as final output
		synthesisResult := agentResults[synthesisTaskIdx]
		synthesis = activities.SynthesisResult{
			FinalResult: synthesisResult.Response,
			TokensUsed:  0, // Already counted in agent execution
		}
		logger.Info("Using synthesis subtask result as final output",
			"agent_id", synthesisResult.AgentID,
		)
	} else {
		// No bypass or synthesis subtask, perform standard synthesis
		logger.Info("Performing standard synthesis of agent results")
		err = workflow.ExecuteActivity(ctx,
			activities.SynthesizeResultsLLM,
			activities.SynthesisInput{
				Query:        input.Query,
				AgentResults: agentResults,
				Context:      baseContext,
			}).Get(ctx, &synthesis)

		if err != nil {
			logger.Error("Synthesis failed", "error", err)
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
			}, err
		}

		totalTokens += synthesis.TokensUsed
	}

	// Step 5: Optional reflection for quality improvement
	finalResult := synthesis.FinalResult
	qualityScore := 0.5
	reflectionTokens := 0

	if config.ReflectionEnabled && shouldReflect(decomp.ComplexityScore, &config) && !hasSynthesisSubtask {
		// Only reflect if we didn't detect a synthesis subtask
		// This preserves user-specified output formats (e.g., Chinese text)
		reflectionConfig := patterns.ReflectionConfig{
			Enabled:             true,
			MaxRetries:          config.ReflectionMaxRetries,
			ConfidenceThreshold: config.ReflectionConfidenceThreshold,
			Criteria:            config.ReflectionCriteria,
			TimeoutMs:           config.ReflectionTimeoutMs,
		}

		reflectionOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			ModelTier:      modelTier,
		}

		improvedResult, score, reflectionTokens, err := patterns.ReflectOnResult(
			ctx,
			input.Query,
			synthesis.FinalResult,
			agentResults,
			baseContext,
			reflectionConfig,
			reflectionOpts,
		)

		if err == nil {
			finalResult = improvedResult
			qualityScore = score
			totalTokens += reflectionTokens
			logger.Info("Reflection improved quality",
				"score", qualityScore,
				"tokens", reflectionTokens,
			)
		}
	} else if hasSynthesisSubtask && config.ReflectionEnabled {
		logger.Info("Skipping reflection to preserve synthesis subtask output format")
	}

	// Step 6: Update session and persist
	if input.SessionID != "" {
		_ = updateSessionWithAgentUsage(ctx, input.SessionID, finalResult, totalTokens, len(agentResults), agentResults)
		_ = recordToVectorStore(ctx, input, finalResult, decomp.Mode, decomp.ComplexityScore)
	}

	// Note: Workflow completion is handled by the orchestrator

	logger.Info("DAGWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"agent_count", len(agentResults),
	)

	// Record pattern metrics (fire-and-forget)
	metricsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})
	_ = workflow.ExecuteActivity(metricsCtx, "RecordPatternMetrics", activities.PatternMetricsInput{
		Pattern:      execStrategy,
		Version:      "v2",
		AgentCount:   len(agentResults),
		TokensUsed:   totalTokens,
		WorkflowType: "dag",
	}).Get(ctx, nil)

	if shouldReflect(decomp.ComplexityScore, &config) && qualityScore > 0.5 {
		_ = workflow.ExecuteActivity(metricsCtx, "RecordPatternMetrics", activities.PatternMetricsInput{
			Pattern:    "reflection",
			Version:    "v2",
			Improved:   qualityScore > 0.7,
			TokensUsed: reflectionTokens,
		}).Get(ctx, nil)
	}

	// Aggregate tool errors across agent results
	var toolErrors []map[string]string
	for _, ar := range agentResults {
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
		"version":        "v2",
		"complexity":     decomp.ComplexityScore,
		"quality_score":  qualityScore,
		"agent_count":    len(agentResults),
		"execution_mode": execStrategy,
		"had_reflection": shouldReflect(decomp.ComplexityScore, &config),
	}
	if len(toolErrors) > 0 {
		meta["tool_errors"] = toolErrors
	}

	// Aggregate agent metadata (model, provider, tokens, cost)
	agentMeta := metadata.AggregateAgentMetadata(agentResults, reflectionTokens+synthesis.TokensUsed)
	for k, v := range agentMeta {
		meta[k] = v
	}

	// Ensure total_tokens present in metadata (fallback to workflow total if missing/zero)
	if tv, ok := meta["total_tokens"]; !ok || (ok && ((func() int {
		switch x := tv.(type) {
		case int:
			return x
		case float64:
			return int(x)
		default:
			return 0
		}
	})() == 0)) {
		if totalTokens > 0 {
			meta["total_tokens"] = totalTokens
		}
	}

	// Fallback: if model/provider missing, prefer provider override from context, then derive from model/tier
	// Provider override from context/session
	providerOverride := ""
	if v, ok := baseContext["provider_override"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := baseContext["provider"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := baseContext["llm_provider"].(string); ok && strings.TrimSpace(v) != "" {
		providerOverride = strings.ToLower(strings.TrimSpace(v))
	}

	// Model fallback
	_, hasModel := meta["model"]
	_, hasModelUsed := meta["model_used"]
	if !hasModel && !hasModelUsed {
		chosen := ""
		if providerOverride != "" {
			chosen = pricing.GetPriorityModelForProvider(modelTier, providerOverride)
		}
		if chosen == "" {
			chosen = pricing.GetPriorityOneModel(modelTier)
		}
		if chosen != "" {
			meta["model"] = chosen
			meta["model_used"] = chosen
		}
	}

	// Provider fallback (prefer override, then detect from model, then tier default)
	if _, ok := meta["provider"]; !ok || meta["provider"] == "" {
		prov := providerOverride
		if prov == "" {
			if m, ok := meta["model"].(string); ok && m != "" {
				prov = detectProviderFromModel(m)
			}
		}
		if prov == "" || prov == "unknown" {
			prov = pricing.GetPriorityOneProvider(modelTier)
		}
		if prov != "" {
			meta["provider"] = prov
		}
	}

	// If cost is missing or zero but we now have model and tokens, compute cost using pricing
	if cv, ok := meta["cost_usd"]; !ok || (ok && (func() bool {
		switch x := cv.(type) {
		case int:
			return x == 0
		case float64:
			return x == 0.0
		default:
			return false
		}
	})()) {
		if m, okm := meta["model"].(string); okm && m != "" {
			// Try to get total tokens as int
			tokens := 0
			if tv, ok := meta["total_tokens"]; ok {
				switch x := tv.(type) {
				case int:
					tokens = x
				case float64:
					tokens = int(x)
				}
			}
			if tokens == 0 {
				tokens = totalTokens
			}
			if tokens > 0 {
				meta["cost_usd"] = pricing.CostForTokens(m, tokens)
			}
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
		AgentID:    "dag",
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

// executeParallelPattern uses the parallel execution pattern
func executeParallelPattern(
	ctx workflow.Context,
	decomp activities.DecompositionResult,
	input TaskInput,
	baseContext map[string]interface{},
	agentMaxTokens int,
	modelTier string,
	config activities.WorkflowConfig,
) ([]activities.AgentExecutionResult, int) {

	parallelTasks := make([]execution.ParallelTask, len(decomp.Subtasks))
	for i, subtask := range decomp.Subtasks {
		// Preserve incoming role from base context by default; allow LLM to override via agent_types
		baseRole := "agent"
		if v, ok := baseContext["role"].(string); ok && v != "" {
			baseRole = v
		}
		role := baseRole
		if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
			role = decomp.AgentTypes[i]
		}

		parallelTasks[i] = execution.ParallelTask{
			ID:             subtask.ID,
			Description:    subtask.Description,
			SuggestedTools: subtask.SuggestedTools,
			ToolParameters: subtask.ToolParameters,
			PersonaID:      subtask.SuggestedPersona,
			Role:           role,
		}
	}

	// Honor plan_schema_v2 concurrency_limit if specified
	maxConcurrency := config.ParallelMaxConcurrency
	if decomp.ConcurrencyLimit > 0 {
		maxConcurrency = decomp.ConcurrencyLimit
	}

	parallelConfig := execution.ParallelConfig{
		MaxConcurrency: maxConcurrency,
		EmitEvents:     true,
		Context:        baseContext,
	}

	result, err := execution.ExecuteParallel(
		ctx,
		parallelTasks,
		input.SessionID,
		convertHistoryForAgent(input.History),
		parallelConfig,
		agentMaxTokens,
		input.UserID,
		modelTier,
	)

	if err != nil {
		workflow.GetLogger(ctx).Error("Parallel execution failed", "error", err)
		return nil, 0
	}

	return result.Results, result.TotalTokens
}

// executeSequentialPattern uses the sequential execution pattern
func executeSequentialPattern(
	ctx workflow.Context,
	decomp activities.DecompositionResult,
	input TaskInput,
	baseContext map[string]interface{},
	agentMaxTokens int,
	modelTier string,
	config activities.WorkflowConfig,
) ([]activities.AgentExecutionResult, int) {

	sequentialTasks := make([]execution.SequentialTask, len(decomp.Subtasks))
	for i, subtask := range decomp.Subtasks {
		// Preserve incoming role from base context by default; allow LLM to override via agent_types
		baseRole := "agent"
		if v, ok := baseContext["role"].(string); ok && v != "" {
			baseRole = v
		}
		role := baseRole
		if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
			role = decomp.AgentTypes[i]
		}

		sequentialTasks[i] = execution.SequentialTask{
			ID:             subtask.ID,
			Description:    subtask.Description,
			SuggestedTools: subtask.SuggestedTools,
			ToolParameters: subtask.ToolParameters,
			PersonaID:      subtask.SuggestedPersona,
			Role:           role,
			Dependencies:   subtask.Dependencies,
		}
	}

	sequentialConfig := execution.SequentialConfig{
		EmitEvents:               true,
		Context:                  baseContext,
		PassPreviousResults:      config.SequentialPassResults,
		ExtractNumericValues:     config.SequentialExtractNumeric,
		ClearDependentToolParams: true,
	}

	result, err := execution.ExecuteSequential(
		ctx,
		sequentialTasks,
		input.SessionID,
		convertHistoryForAgent(input.History),
		sequentialConfig,
		agentMaxTokens,
		input.UserID,
		modelTier,
	)

	if err != nil {
		workflow.GetLogger(ctx).Error("Sequential execution failed", "error", err)
		return nil, 0
	}

	return result.Results, result.TotalTokens
}

// executeHybridPattern uses the hybrid execution pattern for dependencies
func executeHybridPattern(
	ctx workflow.Context,
	decomp activities.DecompositionResult,
	input TaskInput,
	baseContext map[string]interface{},
	agentMaxTokens int,
	modelTier string,
	config activities.WorkflowConfig,
) ([]activities.AgentExecutionResult, int) {

	hybridTasks := make([]execution.HybridTask, len(decomp.Subtasks))
	for i, subtask := range decomp.Subtasks {
		// Preserve incoming role from base context by default; allow LLM to override via agent_types
		baseRole := "agent"
		if v, ok := baseContext["role"].(string); ok && v != "" {
			baseRole = v
		}
		role := baseRole
		if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
			role = decomp.AgentTypes[i]
		}

		hybridTasks[i] = execution.HybridTask{
			ID:             subtask.ID,
			Description:    subtask.Description,
			SuggestedTools: subtask.SuggestedTools,
			ToolParameters: subtask.ToolParameters,
			PersonaID:      subtask.SuggestedPersona,
			Role:           role,
			Dependencies:   subtask.Dependencies,
		}
	}

	// Honor plan_schema_v2 concurrency_limit if specified
	maxConcurrency := config.ParallelMaxConcurrency
	if decomp.ConcurrencyLimit > 0 {
		maxConcurrency = decomp.ConcurrencyLimit
	}

	hybridConfig := execution.HybridConfig{
		MaxConcurrency:           maxConcurrency,
		EmitEvents:               true,
		Context:                  baseContext,
		DependencyWaitTimeout:    time.Duration(config.HybridDependencyTimeout) * time.Second,
		PassDependencyResults:    config.SequentialPassResults,
		ClearDependentToolParams: true,
	}

	result, err := execution.ExecuteHybrid(
		ctx,
		hybridTasks,
		input.SessionID,
		convertHistoryForAgent(input.History),
		hybridConfig,
		agentMaxTokens,
		input.UserID,
		modelTier,
	)

	if err != nil {
		workflow.GetLogger(ctx).Error("Hybrid execution failed", "error", err)
		return nil, 0
	}

	// Convert map results to slice
	var agentResults []activities.AgentExecutionResult
	for _, result := range result.Results {
		agentResults = append(agentResults, result)
	}

	return agentResults, result.TotalTokens
}

// Helper functions

func updateSession(ctx workflow.Context, sessionID, result string, tokens, agents int) error {
	var updRes activities.SessionUpdateResult
	return workflow.ExecuteActivity(ctx,
		constants.UpdateSessionResultActivity,
		activities.SessionUpdateInput{
			SessionID:  sessionID,
			Result:     result,
			TokensUsed: tokens,
			AgentsUsed: agents,
		}).Get(ctx, &updRes)
}

// updateSessionWithAgentUsage passes per-agent model/token usage for accurate cost
func updateSessionWithAgentUsage(ctx workflow.Context, sessionID, result string, tokens, agents int, results []activities.AgentExecutionResult) error {
	var usages []activities.AgentUsage
	for _, r := range results {
		usages = append(usages, activities.AgentUsage{Model: r.ModelUsed, Tokens: r.TokensUsed, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens})
	}
	var updRes activities.SessionUpdateResult
	return workflow.ExecuteActivity(ctx,
		constants.UpdateSessionResultActivity,
		activities.SessionUpdateInput{
			SessionID:  sessionID,
			Result:     result,
			TokensUsed: tokens,
			AgentsUsed: agents,
			AgentUsage: usages,
		}).Get(ctx, &updRes)
}

func recordToVectorStore(ctx workflow.Context, input TaskInput, answer, mode string, complexity float64) error {
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	workflow.ExecuteActivity(detachedCtx,
		activities.RecordQuery,
		activities.RecordQueryInput{
			SessionID: input.SessionID,
			UserID:    input.UserID,
			Query:     input.Query,
			Answer:    answer,
			Model:     mode,
			Metadata: map[string]interface{}{
				"workflow":   "dag_v2",
				"complexity": complexity,
				"tenant_id":  input.TenantID,
			},
			RedactPII: true,
		})
	return nil
}
