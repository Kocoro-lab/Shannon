package strategies

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/formatting"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/validation"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/control"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
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

	// Determine workflow ID for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Initialize control signal handler for pause/resume/cancel
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	// Skip SSE emissions when running as child workflow (parent already emits)
	controlHandler := &control.SignalHandler{
		WorkflowID:  workflowID,
		AgentID:     "dag",
		Logger:      logger,
		EmitCtx:     emitCtx,
		SkipSSEEmit: input.ParentWorkflowID != "",
	}
	controlHandler.Setup(ctx)

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

	// Check pause/cancel before decomposition
	if err := controlHandler.CheckPausePoint(ctx, "pre_decomposition"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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

	// Validate DAG dependencies for cycles (prevents infinite waits)
	if len(decomp.Subtasks) > 1 {
		subtaskInfos := make([]validation.SubtaskInfo, len(decomp.Subtasks))
		for i, st := range decomp.Subtasks {
			subtaskInfos[i] = validation.SubtaskInfo{
				ID:           st.ID,
				Dependencies: st.Dependencies,
			}
		}
		if cycleErr := validation.ValidateDAGDependencies(subtaskInfos); cycleErr != nil {
			logger.Error("Cyclic dependency detected in task plan", "error", cycleErr)
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Invalid task plan: %v", cycleErr),
			}, cycleErr
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
	var collectedCitations []metadata.Citation
	// Option trigger: allow disabling citation collection via context flag (default on)
	enableCitations := true
	if v, ok := baseContext["enable_citations"].(bool); ok {
		enableCitations = v
	}

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

		simpleAgentName := agents.GetAgentName(workflowID, 0)

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
			AgentID:    simpleAgentName,
			Response:   simpleResult.Response,
			TokensUsed: simpleResult.TokensUsed,
			Success:    simpleResult.Success,
		})
		totalTokens = simpleResult.TokensUsed

		// Update session
		if input.SessionID != "" {
			_ = updateSessionWithAgentUsage(ctx, input.SessionID, simpleResult.Response, totalTokens, 1, []activities.AgentExecutionResult{{AgentID: simpleAgentName, TokensUsed: simpleResult.TokensUsed, ModelUsed: simpleResult.ModelUsed, Success: true, Response: simpleResult.Response}})
			_ = recordToVectorStore(ctx, input, simpleResult.Response, "simple", decomp.ComplexityScore)
		}

		// Build metadata aligned with other workflows (model/provider/tokens/cost)
		meta := map[string]interface{}{
			"complexity_score": decomp.ComplexityScore,
			"mode":             "simple",
			"num_agents":       1,
		}

		// Aggregate agent metadata from the single result to populate model/provider/tokens
		ar := []activities.AgentExecutionResult{
			{
				AgentID:    simpleAgentName,
				Response:   simpleResult.Response,
				TokensUsed: simpleResult.TokensUsed,
				Success:    simpleResult.Success,
				ModelUsed:  simpleResult.ModelUsed,
			},
		}
		agMeta := metadata.AggregateAgentMetadata(ar, 0)
		for k, v := range agMeta {
			meta[k] = v
		}

		// Compute cost with centralized pricing when tokens are available
		if totalTokens > 0 {
			modelForCost := ""
			if m, ok := meta["model"].(string); ok && m != "" {
				modelForCost = m
			} else {
				modelForCost = pricing.GetPriorityOneModel(modelTier)
			}
			meta["cost_usd"] = pricing.CostForTokens(modelForCost, totalTokens)
		}

		return TaskResult{
			Result:     simpleResult.Response,
			Success:    true,
			TokensUsed: totalTokens,
			Metadata:   meta,
		}, nil
	}

	// Check pause/cancel before complex execution
	if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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

	// Check pause/cancel before synthesis
	if err := controlHandler.CheckPausePoint(ctx, "pre_synthesis"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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

		// 3) If citations exist across agent results, avoid bypass so we can inject inline citations
		if shouldBypass && enableCitations {
			citations, formatted := collectAndFormatCitations(ctx, agentResults)
			if len(citations) > 0 {
				shouldBypass = false
				collectedCitations = citations
				baseContext["available_citations"] = formatted
				baseContext["citation_count"] = len(citations)
				baseContext["citations"] = citationsToStructuredMap(citations)
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
			// Collect citations for synthesis and inject into context (when enabled)
			if enableCitations {
				citations, formatted := collectAndFormatCitations(ctx, agentResults)
				if len(citations) > 0 {
					collectedCitations = citations
					baseContext["available_citations"] = formatted
					baseContext["citation_count"] = len(citations)
					baseContext["citations"] = citationsToStructuredMap(citations)
				}
			}
			err = workflow.ExecuteActivity(ctx,
				activities.SynthesizeResultsLLM,
				activities.SynthesisInput{
					Query:              input.Query,
					AgentResults:       agentResults,
					Context:            baseContext,
					CollectedCitations: collectedCitations,
					ParentWorkflowID:   input.ParentWorkflowID,
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
			if synthesis.TokensUsed > 0 {
				inTok := synthesis.InputTokens
				outTok := synthesis.CompletionTokens
				if inTok == 0 && outTok > 0 {
					est := synthesis.TokensUsed - outTok
					if est > 0 {
						inTok = est
					}
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				wid := input.ParentWorkflowID
				if wid == "" {
					wid = workflow.GetInfo(ctx).WorkflowExecution.ID
				}
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       wid,
					AgentID:      "dag_synthesis",
					Model:        synthesis.ModelUsed,
					Provider:     synthesis.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata: map[string]interface{}{
						"phase":    "synthesis",
						"workflow": "dag",
					},
				}).Get(recCtx, nil)
			}
		}
	} else if hasSynthesisSubtask && synthesisTaskIdx >= 0 && synthesisTaskIdx < len(agentResults) && agentResults[synthesisTaskIdx].Success {
		// If citations exist, re-run synthesis to inject inline citation numbers.
		// Otherwise, use the synthesis subtask's result directly.
		if enableCitations {
			citations, formatted := collectAndFormatCitations(ctx, agentResults)
			if len(citations) > 0 {
				collectedCitations = citations
				baseContext["available_citations"] = formatted
				baseContext["citation_count"] = len(citations)
				baseContext["citations"] = citationsToStructuredMap(citations)
			}
		}

		if len(collectedCitations) == 0 {
			// No citations: use the synthesis subtask's result as-is
			synthesisResult := agentResults[synthesisTaskIdx]
			const minSynthesisResponseLen = 100
			if len(strings.TrimSpace(synthesisResult.Response)) >= minSynthesisResponseLen && synthesisResult.TokensUsed > 0 {
				synthesis = activities.SynthesisResult{
					FinalResult: synthesisResult.Response,
					TokensUsed:  0, // Already counted in agent execution
				}
				logger.Info("Using synthesis subtask result as final output",
					"agent_id", synthesisResult.AgentID,
					"note", "no citations found",
					"response_length", len(synthesisResult.Response),
					"tokens_used", synthesisResult.TokensUsed,
				)
			} else {
				logger.Warn("Synthesis subtask result too short or zero tokens; falling back to LLM synthesis",
					"agent_id", synthesisResult.AgentID,
					"response_length", len(synthesisResult.Response),
					"tokens_used", synthesisResult.TokensUsed,
					"min_required_length", minSynthesisResponseLen,
				)
				err = workflow.ExecuteActivity(ctx,
					activities.SynthesizeResultsLLM,
					activities.SynthesisInput{
						Query:              input.Query,
						AgentResults:       agentResults,
						Context:            baseContext,
						CollectedCitations: collectedCitations,
						ParentWorkflowID:   input.ParentWorkflowID,
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
				if synthesis.TokensUsed > 0 {
					inTok := synthesis.InputTokens
					outTok := synthesis.CompletionTokens
					if inTok == 0 && outTok > 0 {
						est := synthesis.TokensUsed - outTok
						if est > 0 {
							inTok = est
						}
					}
					recCtx := opts.WithTokenRecordOptions(ctx)
					wid := input.ParentWorkflowID
					if wid == "" {
						wid = workflow.GetInfo(ctx).WorkflowExecution.ID
					}
					_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
						UserID:       input.UserID,
						SessionID:    input.SessionID,
						TaskID:       wid,
						AgentID:      "dag_synthesis",
						Model:        synthesis.ModelUsed,
						Provider:     synthesis.Provider,
						InputTokens:  inTok,
						OutputTokens: outTok,
						Metadata: map[string]interface{}{
							"phase":    "synthesis",
							"workflow": "dag",
						},
					}).Get(recCtx, nil)
				}
			}
		} else {
			// Citations present: re-run synthesis to inject inline citations and sources
			logger.Info("Re-running synthesis to inject inline citations",
				"citation_count", len(collectedCitations),
			)
			err = workflow.ExecuteActivity(ctx,
				activities.SynthesizeResultsLLM,
				activities.SynthesisInput{
					Query:              input.Query,
					AgentResults:       agentResults,
					Context:            baseContext,
					CollectedCitations: collectedCitations,
					ParentWorkflowID:   input.ParentWorkflowID,
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
			if synthesis.TokensUsed > 0 {
				inTok := synthesis.InputTokens
				outTok := synthesis.CompletionTokens
				if inTok == 0 && outTok > 0 {
					est := synthesis.TokensUsed - outTok
					if est > 0 {
						inTok = est
					}
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				wid := input.ParentWorkflowID
				if wid == "" {
					wid = workflow.GetInfo(ctx).WorkflowExecution.ID
				}
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       wid,
					AgentID:      "dag_synthesis",
					Model:        synthesis.ModelUsed,
					Provider:     synthesis.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata: map[string]interface{}{
						"phase":    "synthesis",
						"workflow": "dag",
					},
				}).Get(recCtx, nil)
			}
		}
	} else {
		// No bypass or synthesis subtask, perform standard synthesis
		logger.Info("Performing standard synthesis of agent results")
		// Collect citations and inject into context for synthesis
		if enableCitations {
			citations, formatted := collectAndFormatCitations(ctx, agentResults)
			if len(citations) > 0 {
				collectedCitations = citations
				// formatted citations prepared by helper
				baseContext["available_citations"] = formatted
				baseContext["citation_count"] = len(citations)
				baseContext["citations"] = citationsToStructuredMap(citations)
			}
		}
		err = workflow.ExecuteActivity(ctx,
			activities.SynthesizeResultsLLM,
			activities.SynthesisInput{
				Query:              input.Query,
				AgentResults:       agentResults,
				Context:            baseContext,
				CollectedCitations: collectedCitations,
				ParentWorkflowID:   input.ParentWorkflowID,
			}).Get(ctx, &synthesis)

		if err != nil {
			logger.Error("Synthesis failed", "error", err)
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
			}, err
		}

		totalTokens += synthesis.TokensUsed
		if synthesis.TokensUsed > 0 {
			inTok := synthesis.InputTokens
			outTok := synthesis.CompletionTokens
			if inTok == 0 && outTok > 0 {
				est := synthesis.TokensUsed - outTok
				if est > 0 {
					inTok = est
				}
			}
			recCtx := opts.WithTokenRecordOptions(ctx)
			wid := input.ParentWorkflowID
			if wid == "" {
				wid = workflow.GetInfo(ctx).WorkflowExecution.ID
			}
			_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
				UserID:       input.UserID,
				SessionID:    input.SessionID,
				TaskID:       wid,
				AgentID:      "dag_synthesis",
				Model:        synthesis.ModelUsed,
				Provider:     synthesis.Provider,
				InputTokens:  inTok,
				OutputTokens: outTok,
				Metadata: map[string]interface{}{
					"phase":    "synthesis",
					"workflow": "dag",
				},
			}).Get(recCtx, nil)
		}
	}

	// Step 5: Citation Agent - add citations to synthesis result
	finalResult := synthesis.FinalResult
	if len(collectedCitations) > 0 && synthesis.FinalResult != "" {
		// Step 5.1: Remove ## Sources section before passing to Citation Agent
		reportForCitation := synthesis.FinalResult
		var extractedSources string
		if idx := strings.LastIndex(strings.ToLower(reportForCitation), "## sources"); idx != -1 {
			extractedSources = strings.TrimSpace(reportForCitation[idx:])
			reportForCitation = strings.TrimSpace(reportForCitation[:idx])
			logger.Info("CitationAgent: stripped Sources section before processing",
				"sources_length", len(extractedSources),
				"report_length", len(reportForCitation),
			)
		}

		// Convert metadata.Citation to CitationForAgent
		citationsForAgent := make([]activities.CitationForAgent, 0, len(collectedCitations))
		for _, c := range collectedCitations {
			citationsForAgent = append(citationsForAgent, activities.CitationForAgent{
				URL:              c.URL,
				Title:            c.Title,
				Source:           c.Source,
				Snippet:          c.Snippet,
				CredibilityScore: c.CredibilityScore,
				QualityScore:     c.QualityScore,
			})
		}

		var citationResult activities.CitationAgentResult
		citationCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 180 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    time.Second,
				BackoffCoefficient: 2.0,
				MaximumAttempts:    2,
			},
		})

		wid := input.ParentWorkflowID
		if wid == "" {
			wid = workflow.GetInfo(ctx).WorkflowExecution.ID
		}

		// Dynamic model tier: use medium for longer reports (better instruction following)
		citationModelTier := "small"
		if len(reportForCitation) > 8000 {
			citationModelTier = "medium"
		}

		cerr := workflow.ExecuteActivity(citationCtx, "AddCitations", activities.CitationAgentInput{
			Report:           reportForCitation, // Clean report without Sources
			Citations:        citationsForAgent,
			ParentWorkflowID: wid,
			ModelTier:        citationModelTier,
		}).Get(citationCtx, &citationResult)

		if cerr != nil {
			logger.Warn("CitationAgent failed, using original synthesis", "error", cerr)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventProgress,
				AgentID:    "citation_agent",
				Message:    "Citation injection skipped due to service error",
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
		} else if citationResult.ValidationPassed {
			// Use cited report and rebuild Sources with correct Used inline/Additional labels
			// The new report has [n] markers, so FormatReportWithCitations will correctly
			// identify which citations were actually used inline
			citationsList := ""
			if v, ok := baseContext["available_citations"].(string); ok {
				citationsList = v
			}
			if citationsList != "" {
				finalResult = formatting.FormatReportWithCitations(citationResult.CitedReport, citationsList)
			} else {
				// Fallback: just append the extracted sources
				finalResult = citationResult.CitedReport
				if extractedSources != "" {
					finalResult = strings.TrimSpace(finalResult) + "\n\n" + extractedSources
				}
			}
			totalTokens += citationResult.TokensUsed
			logger.Info("CitationAgent: citations added and Sources rebuilt",
				"citations_used", len(citationResult.CitationsUsed),
			)
		} else {
			logger.Warn("CitationAgent validation failed, keeping original synthesis",
				"error", citationResult.ValidationError,
			)
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventProgress,
				AgentID:    "citation_agent",
				Message:    "Citation injection skipped due to validation failure",
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
			// Keep original finalResult (which already has Sources)
		}
	}

	// Step 6: Optional reflection for quality improvement
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

	// Optional: verify claims if enabled and we have citations
	var verification activities.VerificationResult
	verifyEnabled := false
	if v, ok := baseContext["enable_verification"].(bool); ok {
		verifyEnabled = v
	}
	if verifyEnabled && len(collectedCitations) > 0 {
		var verCitations []interface{}
		for _, c := range collectedCitations {
			m := map[string]interface{}{
				"url":               c.URL,
				"title":             c.Title,
				"source":            c.Source,
				"content":           c.Snippet,
				"credibility_score": c.CredibilityScore,
				"quality_score":     c.QualityScore,
			}
			verCitations = append(verCitations, m)
		}
		verr := workflow.ExecuteActivity(ctx, "VerifyClaimsActivity", activities.VerifyClaimsInput{
			Answer:    finalResult,
			Citations: verCitations,
		}).Get(ctx, &verification)
		if verr != nil {
			logger.Warn("Claim verification failed, skipping verification metadata", "error", verr)
		}
	}

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
	if verification.TotalClaims > 0 || verification.OverallConfidence > 0 {
		meta["verification"] = verification
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

	// Check pause/cancel before completion
	if err := controlHandler.CheckPausePoint(ctx, "pre_completion"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}

	// Emit final clean LLM_OUTPUT for OpenAI-compatible streaming.
	// Agent ID "final_output" signals the streamer to always show this content.
	if finalResult != "" {
		payload := map[string]interface{}{
			"tokens_used": totalTokens,
		}
		if model, ok := meta["model_used"].(string); ok && model != "" {
			payload["model_used"] = model
		} else if model, ok := meta["model"].(string); ok && model != "" {
			payload["model_used"] = model
		}

		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventLLMOutput,
			AgentID:    "final_output",
			Message:    finalResult,
			Timestamp:  workflow.Now(ctx),
			Payload:    payload,
		}).Get(ctx, nil)
	}

	// Emit WORKFLOW_COMPLETED before returning
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "dag",
		Message:    activities.MsgWorkflowCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata:   meta,
	}, nil
}

// collectAndFormatCitations extracts citations from agentResults and returns the
// citations along with a formatted list suitable for synthesis prompts.
func collectAndFormatCitations(ctx workflow.Context, agentResults []activities.AgentExecutionResult) ([]metadata.Citation, string) {
	var resultsForCitations []interface{}
	for _, ar := range agentResults {
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
	if len(citations) == 0 {
		return citations, ""
	}
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
	return citations, strings.TrimRight(b.String(), "\n")
}

// citationsToStructuredMap converts citations to a structured map format for SSE emission
func citationsToStructuredMap(citations []metadata.Citation) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(citations))
	for _, c := range citations {
		out = append(out, map[string]interface{}{
			"url":               c.URL,
			"title":             c.Title,
			"source":            c.Source,
			"credibility_score": c.CredibilityScore,
			"quality_score":     c.QualityScore,
		})
	}
	return out
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

	// Convert map results to slice in original task order (deterministic)
	// Map iteration is non-deterministic in Go, so we must iterate by original task order
	var agentResults []activities.AgentExecutionResult
	for _, task := range hybridTasks {
		if taskResult, found := result.Results[task.ID]; found {
			agentResults = append(agentResults, taskResult)
		}
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
	// Await result to prevent race condition where workflow closes before persistence
	return workflow.ExecuteActivity(ctx,
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
		}).Get(ctx, nil)
}
