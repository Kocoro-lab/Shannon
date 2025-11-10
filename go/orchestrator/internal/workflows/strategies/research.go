package strategies

import (
    "fmt"
    "regexp"
    "strings"
    "time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/budget"
    pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

// FilterCitationsByEntity filters citations based on entity relevance when canonical name is detected.
//
// Scoring System (OR logic, not AND):
//   - Official domain match: +0.6 points (e.g., ptmind.com, jp.ptmind.com)
//   - Alias in URL: +0.4 points (broader domain matching)
//   - Title/snippet/source contains alias: +0.4 points
//   - Threshold: 0.3 (passes with any single match)
//
// Filtering Strategy:
//   1. Always keep ALL official domain citations (bypass threshold)
//   2. Keep non-official citations scoring >= threshold
//   3. Backfill to minKeep (8) using quality×credibility+entity_score
//
// Prevents Over-Filtering:
//   - Lower threshold (0.3 vs 0.5) for better recall
//   - Higher minKeep (8 vs 3) for deep research coverage
//   - Official sites guaranteed inclusion
//
// Search Engine Variance:
//   - If search doesn't return official sites, filter won't create them
//   - If official sites in results, they're always preserved
//
// Future Improvements (see long-term plan):
//   - Phase 2: Soft rerank (multiply scores vs hard filter)
//   - Phase 3: Verification entity coherence
//   - Phase 4: Adaptive retry if coverage < target
func FilterCitationsByEntity(citations []metadata.Citation, canonicalName string, aliases []string, officialDomains []string) []metadata.Citation {
	if canonicalName == "" || len(citations) == 0 {
		return citations
	}

	const (
		threshold = 0.3  // Minimum relevance score to pass (lowered for recall)
		minKeep   = 8    // Safety floor: keep at least this many for deep research
	)

	// Normalize canonical name and aliases for matching
	canonical := strings.ToLower(strings.TrimSpace(canonicalName))
	aliasSet := make(map[string]bool)
	aliasSet[canonical] = true
	for _, a := range aliases {
		normalized := strings.ToLower(strings.TrimSpace(a))
		if normalized != "" {
			aliasSet[normalized] = true
		}
	}

	// Normalize official domains (extract domain from URL or use as-is)
	domainSet := make(map[string]bool)
	for _, d := range officialDomains {
		normalized := strings.ToLower(strings.TrimSpace(d))
		// Remove protocol if present
		normalized = strings.TrimPrefix(normalized, "https://")
		normalized = strings.TrimPrefix(normalized, "http://")
		normalized = strings.TrimPrefix(normalized, "www.")
		if normalized != "" {
			domainSet[normalized] = true
		}
	}

	type scoredCitation struct {
		citation      metadata.Citation
		score         float64
		isOfficial    bool
		matchedDomain string
		matchedAlias  string
	}

	var scored []scoredCitation
	var officialSites []scoredCitation

	for _, c := range citations {
		score := 0.0
		isOfficial := false
		matchedDomain := ""
		matchedAlias := ""

		// Check 1: Domain match - stronger signal for official domains
		urlLower := strings.ToLower(c.URL)
		for domain := range domainSet {
			if strings.Contains(urlLower, domain) {
				score += 0.6  // Increased weight for domain match
				isOfficial = true
				matchedDomain = domain
				break
			}
		}

		// Also check if URL contains any alias (broader domain matching)
		if !isOfficial {
			for alias := range aliasSet {
				// Remove quotes from aliases for URL matching
				cleanAlias := strings.Trim(alias, "\"")
				if cleanAlias != "" && strings.Contains(urlLower, cleanAlias) {
					score += 0.4  // Partial credit for alias in URL
					matchedDomain = "alias-in-url:" + cleanAlias
					break
				}
			}
		}

		// Check 2: Title/snippet contains canonical name or aliases
		titleLower := strings.ToLower(c.Title)
		snippetLower := strings.ToLower(c.Snippet)
		sourceLower := strings.ToLower(c.Source)
		combined := titleLower + " " + snippetLower + " " + sourceLower

		for alias := range aliasSet {
			cleanAlias := strings.Trim(alias, "\"")
			if cleanAlias != "" && strings.Contains(combined, cleanAlias) {
				score += 0.4  // Title/snippet match
				matchedAlias = cleanAlias
				break
			}
		}

		sc := scoredCitation{
			citation:      c,
			score:         score,
			isOfficial:    isOfficial,
			matchedDomain: matchedDomain,
			matchedAlias:  matchedAlias,
		}
		scored = append(scored, sc)

		// Track official sites separately for backfill
		if isOfficial {
			officialSites = append(officialSites, sc)
		}
	}

	// Step 1: Always keep official domain citations (bypass threshold)
	var filtered []metadata.Citation
	officialKept := 0
	for _, sc := range officialSites {
		filtered = append(filtered, sc.citation)
		officialKept++
	}

	// Step 2: Add non-official citations that pass threshold
	for _, sc := range scored {
		if !sc.isOfficial && sc.score >= threshold {
			filtered = append(filtered, sc.citation)
		}
	}

	// Step 3: Safety floor with backfill
	if len(filtered) < minKeep {
		// Sort all citations by combined score (quality × credibility + entity relevance)
		for i := 0; i < len(scored); i++ {
			for j := i + 1; j < len(scored); j++ {
				scoreI := (scored[i].citation.QualityScore * scored[i].citation.CredibilityScore) + scored[i].score
				scoreJ := (scored[j].citation.QualityScore * scored[j].citation.CredibilityScore) + scored[j].score
				if scoreJ > scoreI {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}

		// Backfill from top-scored citations
		existingURLs := make(map[string]bool)
		for _, c := range filtered {
			existingURLs[c.URL] = true
		}

		for i := 0; i < len(scored) && len(filtered) < minKeep; i++ {
			if !existingURLs[scored[i].citation.URL] {
				filtered = append(filtered, scored[i].citation)
			}
		}
	}

	return filtered
}

// ResearchWorkflow demonstrates composed patterns for complex research tasks.
// It combines React loops, parallel research, and reflection patterns.
func ResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ResearchWorkflow with composed patterns",
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

	// Set up workflow ID and emit context for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

    // Prepare base context (merge input.Context + SessionCtx)
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
			logger.Info("Injected hierarchical memory into research context",
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
			logger.Info("Injected session memory into research context",
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
		modelTier := determineModelTier(baseContext, "medium")

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       modelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in research workflow",
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
					TargetTokens:     int(float64(activities.GetModelWindowSize(modelTier)) * 0.375),
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

    // Step 0: Refine/expand vague research queries
    // Emit refinement start event
    emitTaskUpdate(ctx, input, activities.StreamEventAgentThinking, "research-refiner", "Refining research query")

    var totalTokens int
    var refineResult activities.RefineResearchQueryResult
    refinedQuery := input.Query // Default to original query
err := workflow.ExecuteActivity(ctx, constants.RefineResearchQueryActivity,
    activities.RefineResearchQueryInput{
        Query:   input.Query,
        Context: baseContext,
    }).Get(ctx, &refineResult)

if err == nil && refineResult.RefinedQuery != "" {
        logger.Info("Query refined for research",
            "original", input.Query,
            "refined", refineResult.RefinedQuery,
            "areas", refineResult.ResearchAreas,
            "tokens_used", refineResult.TokensUsed,
        )
        refinedQuery = refineResult.RefinedQuery
        baseContext["research_areas"] = refineResult.ResearchAreas
        baseContext["original_query"] = input.Query
        baseContext["refinement_rationale"] = refineResult.Rationale
        baseContext["refined_query"] = refinedQuery
        if refineResult.CanonicalName != "" {
            baseContext["canonical_name"] = refineResult.CanonicalName
        }
        if len(refineResult.ExactQueries) > 0 {
            baseContext["exact_queries"] = refineResult.ExactQueries
        }
        if len(refineResult.OfficialDomains) > 0 {
            baseContext["official_domains"] = refineResult.OfficialDomains
        }
        if len(refineResult.DisambiguationTerms) > 0 {
            baseContext["disambiguation_terms"] = refineResult.DisambiguationTerms
        }
        // Account for refinement tokens in the workflow total
        totalTokens += refineResult.TokensUsed

        // Record refinement token usage for accurate cost tracking
        if refineResult.TokensUsed > 0 {
            inTok := 0
            // We only have a total for refinement; approximate split 60/40
            if refineResult.TokensUsed > 0 {
                inTok = int(float64(refineResult.TokensUsed) * 0.6)
            }
            outTok := refineResult.TokensUsed - inTok
            recCtx := opts.WithTokenRecordOptions(ctx)
            _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
                UserID:       input.UserID,
                SessionID:    input.SessionID,
                TaskID:       workflowID,
                AgentID:      "research-refiner",
                Model:        refineResult.ModelUsed,
                Provider:     refineResult.Provider,
                InputTokens:  inTok,
                OutputTokens: outTok,
                Metadata:     map[string]interface{}{"phase": "refine"},
            }).Get(recCtx, nil)
        }

        // Emit refinement complete event with details (include canonical/entity hints for diagnostics)
        emitTaskUpdatePayload(ctx, input, activities.StreamEventProgress, "research-refiner",
            fmt.Sprintf("Expanded query into %d research areas", len(refineResult.ResearchAreas)),
            map[string]interface{}{
                "original_query":     input.Query,
                "refined_query":      refineResult.RefinedQuery,
                "research_areas":     refineResult.ResearchAreas,
                "rationale":          refineResult.Rationale,
                "tokens_used":        refineResult.TokensUsed,
                "model_used":         refineResult.ModelUsed,
                "provider":           refineResult.Provider,
                "canonical_name":     refineResult.CanonicalName,
                "exact_queries":      refineResult.ExactQueries,
                "official_domains":   refineResult.OfficialDomains,
                "disambiguation_terms": refineResult.DisambiguationTerms,
            })
	} else if err != nil {
		logger.Warn("Query refinement failed, using original query", "error", err)
        // Emit warning but continue with original query
        emitTaskUpdate(ctx, input, activities.StreamEventProgress, "research-refiner", "Query refinement skipped, proceeding with original query")
	}

	// Step 1: Decompose the (now refined) research query
    var decomp activities.DecompositionResult
    err = workflow.ExecuteActivity(ctx,
        constants.DecomposeTaskActivity,
        activities.DecompositionInput{
            Query:          refinedQuery, // Use refined query here
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

		// Record decomposition token usage for accurate cost tracking (if provided)
		if decomp.TokensUsed > 0 || decomp.InputTokens > 0 || decomp.OutputTokens > 0 {
			inTok := decomp.InputTokens
			outTok := decomp.OutputTokens
			if inTok == 0 && outTok == 0 && decomp.TokensUsed > 0 {
				inTok = int(float64(decomp.TokensUsed) * 0.6)
				outTok = decomp.TokensUsed - inTok
			}
            recCtx := opts.WithTokenRecordOptions(ctx)
            _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
                UserID:       input.UserID,
                SessionID:    input.SessionID,
                TaskID:       workflowID,
                AgentID:      "decompose",
                Model:        decomp.ModelUsed,
                Provider:     decomp.Provider,
                InputTokens:  inTok,
                OutputTokens: outTok,
                Metadata:     map[string]interface{}{"phase": "decompose"},
            }).Get(recCtx, nil)
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
	var agentResults []activities.AgentExecutionResult

	// Step 2: Execute based on complexity
	if decomp.ComplexityScore < 0.5 || len(decomp.Subtasks) <= 1 {
		// Simple research - use React pattern for step-by-step exploration
		logger.Info("Using React pattern for simple research",
			"complexity", decomp.ComplexityScore,
		)

        // Allow tuning ReAct iterations via context with safe clamp (2..8)
        reactMaxIterations := 5
        if v, ok := baseContext["react_max_iterations"]; ok {
            switch t := v.(type) {
            case int:
                reactMaxIterations = t
            case float64:
                reactMaxIterations = int(t)
            }
            if reactMaxIterations < 2 { reactMaxIterations = 2 }
            if reactMaxIterations > 8 { reactMaxIterations = 8 }
        }

        reactConfig := patterns.ReactConfig{
            MaxIterations:     reactMaxIterations,
            ObservationWindow: 3,
            MaxObservations:   20,
            MaxThoughts:       10,
            MaxActions:        10,
        }

		reactOpts := patterns.Options{
			BudgetAgentMax: agentMaxTokens,
			SessionID:      input.SessionID,
			UserID:         input.UserID,
			EmitEvents:     true,
			ModelTier:      modelTier,
			Context:        baseContext,
		}

        reactResult, err := patterns.ReactLoop(
            ctx,
            refinedQuery,
            baseContext,
            input.SessionID,
            convertHistoryForAgent(input.History),
            reactConfig,
            reactOpts,
        )

		if err != nil {
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("React loop failed: %v", err),
			}, err
		}

		// Use the actual agent results from ReAct (includes tool executions for citation collection)
		agentResults = append(agentResults, reactResult.AgentResults...)
		totalTokens = reactResult.TotalTokens

	} else {
        // Complex research - check if we should use ReAct per task for deeper reasoning
        useReactPerTask := false
        if v, ok := baseContext["react_per_task"].(bool); ok && v {
            useReactPerTask = true
            logger.Info("react_per_task enabled via context flag")
        }
        // Auto-enable for high complexity only when strategy is deep/academic
        if !useReactPerTask && decomp.ComplexityScore > 0.7 {
            strategy := ""
            if sv, ok := baseContext["research_strategy"].(string); ok {
                strategy = strings.ToLower(strings.TrimSpace(sv))
            }
            if strategy == "deep" || strategy == "academic" {
                useReactPerTask = true
                logger.Info("Auto-enabling react_per_task due to high complexity",
                    "complexity", decomp.ComplexityScore,
                    "strategy", strategy,
                )
            }
        }

		if useReactPerTask {
			// Use ReAct loop per subtask for deeper reasoning
			logger.Info("Using ReAct per subtask for deep research",
				"complexity", decomp.ComplexityScore,
				"subtasks", len(decomp.Subtasks),
			)

            // Determine execution strategy
            hasDependencies := false
            for _, subtask := range decomp.Subtasks {
                if len(subtask.Dependencies) > 0 {
                    hasDependencies = true
                    break
                }
            }

            if hasDependencies {
                // Sequential execution with ReAct per subtask, respecting dependencies
                logger.Info("Using sequential ReAct execution due to dependencies")

				// Build execution order via topological sort
				executionOrder := topologicalSort(decomp.Subtasks)
				previousResults := make(map[string]string)

				for _, subtaskID := range executionOrder {
					// Find the subtask
					var subtask activities.Subtask
					for _, st := range decomp.Subtasks {
						if st.ID == subtaskID {
							subtask = st
							break
						}
					}

					// Build context with dependency results
					subtaskContext := make(map[string]interface{})
					for k, v := range baseContext {
						subtaskContext[k] = v
					}
					if len(subtask.Dependencies) > 0 {
						depResults := make(map[string]string)
						for _, depID := range subtask.Dependencies {
							if res, ok := previousResults[depID]; ok {
								depResults[depID] = res
							}
						}
						subtaskContext["previous_results"] = depResults
					}

                    // Execute this subtask with ReAct
                    // Allow tuning ReAct iterations via context with safe clamp (2..8)
                    reactMaxIterations := 5
                    if v, ok := baseContext["react_max_iterations"]; ok {
                        switch t := v.(type) {
                        case int:
                            reactMaxIterations = t
                        case float64:
                            reactMaxIterations = int(t)
                        }
                        if reactMaxIterations < 2 { reactMaxIterations = 2 }
                        if reactMaxIterations > 8 { reactMaxIterations = 8 }
                    }
                    reactConfig := patterns.ReactConfig{
                        MaxIterations:     reactMaxIterations,
                        ObservationWindow: 3,
                        MaxObservations:   20,
                        MaxThoughts:       10,
                        MaxActions:        10,
                    }

					reactOpts := patterns.Options{
						BudgetAgentMax: agentMaxTokens,
						SessionID:      input.SessionID,
						UserID:         input.UserID,
						EmitEvents:     true,
						ModelTier:      modelTier,
						Context:        subtaskContext,
					}

					reactResult, err := patterns.ReactLoop(
						ctx,
						subtask.Description,
						subtaskContext,
						input.SessionID,
						convertHistoryForAgent(input.History),
						reactConfig,
						reactOpts,
					)

					if err == nil {
						agentResults = append(agentResults, reactResult.AgentResults...)
						totalTokens += reactResult.TotalTokens
						previousResults[subtaskID] = reactResult.FinalResult
					} else {
						logger.Warn("ReAct loop failed for subtask, continuing", "subtask_id", subtaskID, "error", err)
					}
				}

            } else {
                // Parallel execution with ReAct per subtask
                logger.Info("Using parallel ReAct execution (no dependencies)")

				// Use channels to collect results from parallel executions
				resultsChan := workflow.NewChannel(ctx)

				for _, subtask := range decomp.Subtasks {
					st := subtask // Capture for goroutine

                    workflow.Go(ctx, func(gctx workflow.Context) {
                        // Allow tuning ReAct iterations via context with safe clamp (2..8)
                        reactMaxIterations := 5
                        if v, ok := baseContext["react_max_iterations"]; ok {
                            switch t := v.(type) {
                            case int:
                                reactMaxIterations = t
                            case float64:
                                reactMaxIterations = int(t)
                            }
                            if reactMaxIterations < 2 { reactMaxIterations = 2 }
                            if reactMaxIterations > 8 { reactMaxIterations = 8 }
                        }
                        reactConfig := patterns.ReactConfig{
                            MaxIterations:     reactMaxIterations,
                            ObservationWindow: 3,
                            MaxObservations:   20,
                            MaxThoughts:       10,
                            MaxActions:        10,
                        }

						reactOpts := patterns.Options{
							BudgetAgentMax: agentMaxTokens,
							SessionID:      input.SessionID,
							UserID:         input.UserID,
							EmitEvents:     true,
							ModelTier:      modelTier,
							Context:        baseContext,
						}

						reactResult, err := patterns.ReactLoop(
							gctx,
							st.Description,
							baseContext,
							input.SessionID,
							convertHistoryForAgent(input.History),
							reactConfig,
							reactOpts,
						)

						if err == nil {
							resultsChan.Send(gctx, reactResult)
						} else {
							logger.Warn("ReAct loop failed for parallel subtask", "subtask_id", st.ID, "error", err)
							// Send nil to indicate failure
							resultsChan.Send(gctx, (*patterns.ReactLoopResult)(nil))
						}
					})
				}

				// Collect all results
				for i := 0; i < len(decomp.Subtasks); i++ {
					var result *patterns.ReactLoopResult
					resultsChan.Receive(ctx, &result)
					if result != nil {
						agentResults = append(agentResults, result.AgentResults...)
						totalTokens += result.TotalTokens
					}
				}
			}

		} else {
			// Complex research - use parallel/hybrid execution with simple agents
			logger.Info("Using parallel execution for complex research",
				"complexity", decomp.ComplexityScore,
				"subtasks", len(decomp.Subtasks),
			)

			// Determine execution strategy
			hasDependencies := false
			for _, subtask := range decomp.Subtasks {
				if len(subtask.Dependencies) > 0 {
					hasDependencies = true
					break
				}
			}

			if hasDependencies {
			// Use hybrid execution for dependency management
			logger.Info("Using hybrid execution due to dependencies")

			hybridTasks := make([]execution.HybridTask, len(decomp.Subtasks))
			for i, subtask := range decomp.Subtasks {
				role := "researcher"
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
                ParentArea:     subtask.ParentArea,
                Dependencies:   subtask.Dependencies,
            }
			}

			// Force inject web_search tool for research workflows to ensure citation collection
			for i := range hybridTasks {
				hasWebSearch := false
				for _, tool := range hybridTasks[i].SuggestedTools {
					if tool == "web_search" {
						hasWebSearch = true
						break
					}
				}
				if !hasWebSearch {
					hybridTasks[i].SuggestedTools = append(hybridTasks[i].SuggestedTools, "web_search")
				}
			}

			hybridConfig := execution.HybridConfig{
				MaxConcurrency:           5,
				EmitEvents:               true,
				Context:                  baseContext,
				DependencyWaitTimeout:    6 * time.Minute,
				PassDependencyResults:    true,
				ClearDependentToolParams: true,
			}

			hybridResult, err := execution.ExecuteHybrid(
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
				return TaskResult{
					Success:      false,
					ErrorMessage: fmt.Sprintf("Hybrid execution failed: %v", err),
				}, err
			}

			// Convert results to agent results
			for _, result := range hybridResult.Results {
				agentResults = append(agentResults, result)
			}
			totalTokens = hybridResult.TotalTokens

		} else {
			// Use pure parallel execution
			logger.Info("Using pure parallel execution")

			parallelTasks := make([]execution.ParallelTask, len(decomp.Subtasks))
			for i, subtask := range decomp.Subtasks {
				role := "researcher"
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
                ParentArea:     subtask.ParentArea,
            }
			}

			// Force inject web_search tool for research workflows to ensure citation collection
			for i := range parallelTasks {
				hasWebSearch := false
				for _, tool := range parallelTasks[i].SuggestedTools {
					if tool == "web_search" {
						hasWebSearch = true
						break
					}
				}
				if !hasWebSearch {
					parallelTasks[i].SuggestedTools = append(parallelTasks[i].SuggestedTools, "web_search")
				}
			}

			parallelConfig := execution.ParallelConfig{
				MaxConcurrency: 5,
				EmitEvents:     true,
				Context:        baseContext,
			}

			parallelResult, err := execution.ExecuteParallel(
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
				return TaskResult{
					Success:      false,
					ErrorMessage: fmt.Sprintf("Parallel execution failed: %v", err),
				}, err
			}

			agentResults = parallelResult.Results
			totalTokens = parallelResult.TotalTokens
			}
		}
	}

    // Optional: filter out agent results that likely belong to the wrong entity
    if v, ok := baseContext["canonical_name"].(string); ok && strings.TrimSpace(v) != "" {
        aliases := []string{v}
        if eqv, ok := baseContext["exact_queries"]; ok {
            switch t := eqv.(type) {
            case []string:
                for _, q := range t { aliases = append(aliases, strings.Trim(q, "\"")) }
            case []interface{}:
                for _, it := range t {
                    if s, ok := it.(string); ok { aliases = append(aliases, strings.Trim(s, "\"")) }
                }
            }
        }
        // Use official_domains for additional positive matching
        var domains []string
        if dv, ok := baseContext["official_domains"]; ok {
            switch t := dv.(type) {
            case []string:
                domains = append(domains, t...)
            case []interface{}:
                for _, it := range t {
                    if s, ok := it.(string); ok { domains = append(domains, s) }
                }
            }
        }
        filtered := make([]activities.AgentExecutionResult, 0, len(agentResults))
        removed := 0
        for _, ar := range agentResults {
            txt := strings.ToLower(ar.Response)
            match := false
            for _, a := range aliases {
                if sa := strings.ToLower(strings.TrimSpace(a)); sa != "" && strings.Contains(txt, sa) {
                    match = true; break
                }
            }
            if !match && len(domains) > 0 {
                for _, d := range domains {
                    sd := strings.ToLower(strings.TrimSpace(d))
                    if sd != "" && strings.Contains(txt, sd) {
                        match = true; break
                    }
                }
            }
            // Keep non-search reasoning, drop obvious off-entity tool-driven results
            if match || len(ar.ToolsUsed) == 0 {
                filtered = append(filtered, ar)
            } else {
                removed++
            }
        }
        if len(filtered) > 0 {
            agentResults = filtered
        }
        if removed > 0 {
            logger.Info("Entity filter removed off-entity results",
                "removed", removed,
                "kept", len(agentResults),
                "aliases", aliases,
                "domains", domains,
            )
        }
    }

    // Step 3: Synthesize results
    logger.Info("Synthesizing research results",
        "agent_count", len(agentResults),
    )

    // Per-agent token usage is recorded inside execution patterns (ReactLoop/Parallel/Hybrid).
    // Avoid double-counting here to prevent duplicate token_usage rows.

    // Collect citations from agent tool outputs and inject into context for synthesis/formatting
    // Also retain them for metadata/verification.
    var collectedCitations []metadata.Citation
    // Build lightweight results array with tool_executions to feed metadata.CollectCitations
    {
        var resultsForCitations []interface{}
        for _, ar := range agentResults {
            // Build tool_executions payload compatible with citations extractor
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

        // Use workflow timestamp for determinism; let collector default max to 15
        now := workflow.Now(ctx)
        citations, _ := metadata.CollectCitations(resultsForCitations, now, 0)

        // Apply entity-based filtering if canonical name is present
        if len(citations) > 0 {
            canonicalName, _ := baseContext["canonical_name"].(string)
            if canonicalName != "" {
                // Extract domains and aliases for filtering
                var domains []string
                if d, ok := baseContext["official_domains"].([]string); ok {
                    domains = d
                }
                var aliases []string
                if eq, ok := baseContext["exact_queries"].([]string); ok {
                    aliases = eq
                }
                // Filter citations by entity relevance
                beforeCount := len(citations)
                logger.Info("Applying citation entity filter",
                    "pre_filter_count", beforeCount,
                    "canonical_name", canonicalName,
                    "official_domains", domains,
                    "alias_count", len(aliases),
                )
                citations = FilterCitationsByEntity(citations, canonicalName, aliases, domains)
                logger.Info("Citation filter completed",
                    "before", beforeCount,
                    "after", len(citations),
                    "removed", beforeCount-len(citations),
                    "retention_rate", float64(len(citations))/float64(beforeCount),
                )
            }
            collectedCitations = citations
            // Format into numbered list lines expected by FormatReportWithCitations
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
            baseContext["available_citations"] = strings.TrimRight(b.String(), "\n")
            baseContext["citation_count"] = len(citations)
        }
    }

    // Set synthesis style to comprehensive for research workflows
    baseContext["synthesis_style"] = "comprehensive"
    baseContext["research_areas_count"] = len(refineResult.ResearchAreas)
    // Synthesis tier: allow override via synthesis_model_tier; fallback to large
    synthTier := "large"
    if v, ok := baseContext["synthesis_model_tier"].(string); ok && strings.TrimSpace(v) != "" {
        synthTier = strings.ToLower(strings.TrimSpace(v))
    }
    baseContext["model_tier"] = synthTier

    var synthesis activities.SynthesisResult
    err = workflow.ExecuteActivity(ctx,
        activities.SynthesizeResultsLLM,
        activities.SynthesisInput{
            Query:        input.Query, // Use original query for language detection
            AgentResults: agentResults,
            // Ensure comprehensive report style for research synthesis unless already specified
            Context: func() map[string]interface{} {
                if baseContext == nil {
                    baseContext = map[string]interface{}{}
                }
                if _, ok := baseContext["synthesis_style"]; !ok {
                    baseContext["synthesis_style"] = "comprehensive"
                }
                return baseContext
            }(),
            ParentWorkflowID: input.ParentWorkflowID,
        }).Get(ctx, &synthesis)

	if err != nil {
		logger.Error("Synthesis failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to synthesize results: %v", err),
		}, err
	}

    totalTokens += synthesis.TokensUsed

    // Record synthesis token usage
    if synthesis.TokensUsed > 0 {
        inTok := synthesis.InputTokens
        outTok := synthesis.CompletionTokens
        if inTok == 0 && outTok > 0 {
            // Infer if needed
            est := synthesis.TokensUsed - outTok
            if est > 0 { inTok = est }
        }
        recCtx := opts.WithTokenRecordOptions(ctx)
        _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
            UserID:       input.UserID,
            SessionID:    input.SessionID,
            TaskID:       workflowID,
            AgentID:      "synthesis",
            Model:        synthesis.ModelUsed,
            Provider:     synthesis.Provider,
            InputTokens:  inTok,
            OutputTokens: outTok,
            Metadata: map[string]interface{}{
                "phase": "synthesis",
            },
        }).Get(recCtx, nil)
    }

	// Step 3.5: Gap-filling loop (iterative re-search for undercovered areas)
	// Version-gated for safe rollout and Temporal determinism
	gapFillingVersion := workflow.GetVersion(ctx, "gap_filling_v1", workflow.DefaultVersion, 1)
	if gapFillingVersion >= 1 {
		// Check iteration count from context (prevents infinite loops)
		iterationCount := 0
		if baseContext != nil {
			if v, ok := baseContext["gap_iteration"].(int); ok {
				iterationCount = v
			}
		}

		// Only attempt gap-filling if we haven't exceeded max iterations
		const maxGapIterations = 2
		if iterationCount < maxGapIterations {
			gapAnalysis := analyzeGaps(synthesis.FinalResult, refineResult.ResearchAreas)

			if len(gapAnalysis.UndercoveredAreas) > 0 {
				logger.Info("Detected coverage gaps; triggering targeted re-search",
					"gaps", gapAnalysis.UndercoveredAreas,
					"iteration", iterationCount,
				)

				// Build targeted search queries for gaps
				gapQueries := buildGapQueries(gapAnalysis.UndercoveredAreas, input.Query)

				// Execute targeted searches using ReAct pattern
				var allGapResults []activities.AgentExecutionResult
				for _, gapQuery := range gapQueries {
					gapContext := make(map[string]interface{})
					for k, v := range baseContext {
						gapContext[k] = v
					}
					gapContext["research_mode"] = "gap_fill"
					gapContext["target_area"] = gapQuery.TargetArea
					gapContext["gap_iteration"] = iterationCount + 1

				reactConfig := patterns.ReactConfig{
					MaxIterations:     3,  // Focused search
					ObservationWindow: 3,  // Keep last 3 observations in context
					MaxObservations:   20, // Prevent unbounded growth
					MaxThoughts:       10,
					MaxActions:        10,
				}
					reactOpts := patterns.Options{
						BudgetAgentMax: agentMaxTokens,
						SessionID:      input.SessionID,
						ModelTier:      modelTier,
						Context:        gapContext,
					}

					gapResult, err := patterns.ReactLoop(
						ctx,
						gapQuery.Query,
						gapContext,
						input.SessionID,
						[]string{}, // No history for gap queries
						reactConfig,
						reactOpts,
					)

					if err == nil && len(gapResult.AgentResults) > 0 {
						allGapResults = append(allGapResults, gapResult.AgentResults...)
						totalTokens += gapResult.TotalTokens
					}
				}

				// If we got new evidence, re-collect citations and re-synthesize
				if len(allGapResults) > 0 {
					logger.Info("Gap-filling search completed",
						"gap_results", len(allGapResults),
						"iteration", iterationCount+1,
					)

					// Combine all agent results (original + gap results) and recompute citations
					// This ensures global deduplication and consistent numbering
					combinedAgentResults := append(agentResults, allGapResults...)

					var resultsForCitations []interface{}
					for _, ar := range combinedAgentResults {
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
					allCitations, _ := metadata.CollectCitations(resultsForCitations, now, 0) // Use 0 for default max (15)

					if len(allCitations) > 0 {

						// Re-synthesize with augmented evidence
						var enhancedSynthesis activities.SynthesisResult

						// Build enhanced context with new citations
						enhancedContext := make(map[string]interface{})
						for k, v := range baseContext {
							enhancedContext[k] = v
						}
						enhancedContext["research_areas"] = refineResult.ResearchAreas
						enhancedContext["gap_iteration"] = iterationCount + 1
            // Ensure synthesis tier respects override; fallback to large
            synthTier2 := "large"
            if v, ok := enhancedContext["synthesis_model_tier"].(string); ok && strings.TrimSpace(v) != "" {
                synthTier2 = strings.ToLower(strings.TrimSpace(v))
            }
            enhancedContext["model_tier"] = synthTier2

						// Format citations for synthesis
						if len(allCitations) > 0 {
							var b strings.Builder
							for idx, c := range allCitations {
								title := c.Title
								if title == "" {
									title = c.Source
								}
								if c.PublishedDate != nil {
									fmt.Fprintf(&b, "[%d] %s (%s) - %s, %s\n", idx+1, title, c.URL, c.Source, c.PublishedDate.Format("2006-01-02"))
								} else {
									fmt.Fprintf(&b, "[%d] %s (%s) - %s\n", idx+1, title, c.URL, c.Source)
								}
							}
							enhancedContext["available_citations"] = strings.TrimRight(b.String(), "\n")
							enhancedContext["citation_count"] = len(allCitations)
						}

						err = workflow.ExecuteActivity(ctx,
							activities.SynthesizeResultsLLM,
							activities.SynthesisInput{
								Query:            input.Query,
								AgentResults:     combinedAgentResults, // Combined results with global dedup
								Context:          enhancedContext,
								ParentWorkflowID: input.ParentWorkflowID,
							}).Get(ctx, &enhancedSynthesis)

						if err == nil {
							synthesis = enhancedSynthesis
							collectedCitations = allCitations
							totalTokens += enhancedSynthesis.TokensUsed
							logger.Info("Gap-filling synthesis completed",
								"iteration", iterationCount+1,
								"total_citations", len(allCitations),
							)
						}
					}
				}
			}
		}
	}

	// Step 4: Apply reflection pattern for quality improvement
	reflectionConfig := patterns.ReflectionConfig{
		Enabled:             true,
		MaxRetries:          2,
		ConfidenceThreshold: 0.8,
		Criteria:            []string{"accuracy", "completeness", "clarity"},
		TimeoutMs:           30000,
	}

	reflectionOpts := patterns.Options{
		BudgetAgentMax: agentMaxTokens,
		SessionID:      input.SessionID,
		ModelTier:      modelTier,
	}

    finalResult, qualityScore, reflectionTokens, err := patterns.ReflectOnResult(
        ctx,
        refinedQuery,
        synthesis.FinalResult,
        agentResults,
        baseContext,
        reflectionConfig,
        reflectionOpts,
    )

	if err != nil {
		logger.Warn("Reflection failed, using original result", "error", err)
		finalResult = synthesis.FinalResult
		qualityScore = 0.5
	}

    totalTokens += reflectionTokens

    // Optional: verify claims if enabled and we have citations
    var verification activities.VerificationResult
    verifyEnabled := false
    if v, ok := baseContext["enable_verification"].(bool); ok {
        verifyEnabled = v
    }
    if verifyEnabled && len(collectedCitations) > 0 {
        // Convert citations to []interface{} of maps for VerifyClaimsActivity
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

	// Step 5: Update session and persist results
	if input.SessionID != "" {
		var updRes activities.SessionUpdateResult
		err = workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     finalResult,
				TokensUsed: totalTokens,
				AgentsUsed: len(agentResults),
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
				Model:     modelTier,
				Metadata: map[string]interface{}{
					"workflow":      "research_flow_v2",
					"complexity":    decomp.ComplexityScore,
					"quality_score": qualityScore,
					"patterns_used": []string{"react", "parallel", "reflection"},
					"tenant_id":     input.TenantID,
				},
				RedactPII: true,
			})
	}

	logger.Info("ResearchWorkflow completed successfully",
		"total_tokens", totalTokens,
		"quality_score", qualityScore,
		"agent_count", len(agentResults),
	)

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
        "version":       "v2",
        "complexity":    decomp.ComplexityScore,
        "quality_score": qualityScore,
        "agent_count":   len(agentResults),
        "patterns_used": []string{"react", "parallel", "reflection"},
    }
    if len(collectedCitations) > 0 {
        // Export a light citation struct to metadata
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
    if verification.TotalClaims > 0 || verification.OverallConfidence > 0 {
        meta["verification"] = verification
    }
	if len(toolErrors) > 0 {
		meta["tool_errors"] = toolErrors
	}

    // Aggregate agent metadata (model, provider, tokens)
    agentMeta := metadata.AggregateAgentMetadata(agentResults, synthesis.TokensUsed+reflectionTokens)
    for k, v := range agentMeta {
        meta[k] = v
    }

    // Compute cost estimate from per-phase tokens using centralized pricing
    // Sum per-agent usage using splits; then add synthesis using model from synthesis result
    var estCost float64
    for _, ar := range agentResults {
        if ar.InputTokens > 0 || ar.OutputTokens > 0 {
            estCost += pricing.CostForSplit(ar.ModelUsed, ar.InputTokens, ar.OutputTokens)
        } else if ar.TokensUsed > 0 {
            estCost += pricing.CostForTokens(ar.ModelUsed, ar.TokensUsed)
        }
    }
    if synthesis.TokensUsed > 0 {
        inTok := synthesis.InputTokens
        outTok := synthesis.CompletionTokens
        if inTok == 0 && outTok > 0 {
            est := synthesis.TokensUsed - outTok
            if est > 0 { inTok = est }
        }
        if synthesis.ModelUsed != "" {
            if inTok > 0 || outTok > 0 {
                estCost += pricing.CostForSplit(synthesis.ModelUsed, inTok, outTok)
            } else {
                estCost += pricing.CostForTokens(synthesis.ModelUsed, synthesis.TokensUsed)
            }
        } else {
            estCost += pricing.CostForTokens("", synthesis.TokensUsed)
        }
    }
    if estCost > 0 {
        meta["cost_usd"] = estCost
    }

    // Finalize accurate cost by aggregating recorded token usage for this task
    // Ensures task_executions.total_cost_usd reflects sum of per-agent and synthesis usage
    {
        var report *budget.UsageReport
        err := workflow.ExecuteActivity(ctx, constants.GenerateUsageReportActivity, activities.UsageReportInput{
            // Aggregate by task_id across all user_id records to avoid partial sums
            UserID:    "",
            SessionID: input.SessionID,
            TaskID:    workflowID,
            // Time range left empty; activity defaults to last 24h
        }).Get(ctx, &report)
        if err == nil && report != nil {
            if report.TotalCostUSD > 0 {
                meta["cost_usd"] = report.TotalCostUSD
            }
            if report.TotalTokens > 0 {
                if totalTokens < report.TotalTokens {
                    totalTokens = report.TotalTokens
                }
            }
        } else if err != nil {
            logger.Warn("Usage report aggregation failed", "error", err)
        }
    }

    // Include synthesis finish_reason and requested_max_tokens for observability/debugging
    if synthesis.FinishReason != "" {
        meta["finish_reason"] = synthesis.FinishReason
    }
    if synthesis.RequestedMaxTokens > 0 {
        meta["requested_max_tokens"] = synthesis.RequestedMaxTokens
    }
    if synthesis.CompletionTokens > 0 {
        meta["completion_tokens"] = synthesis.CompletionTokens
    }
    if synthesis.EffectiveMaxCompletion > 0 {
        meta["effective_max_completion"] = synthesis.EffectiveMaxCompletion
    }

	// Emit WORKFLOW_COMPLETED before returning
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "research",
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

// GapAnalysis holds information about undercovered research areas
type GapAnalysis struct {
	UndercoveredAreas []string
}

// GapQuery represents a targeted search query for a gap area
type GapQuery struct {
	Query      string
	TargetArea string
}

// analyzeGaps detects which research areas are undercovered in the synthesis
func analyzeGaps(synthesisText string, researchAreas []string) GapAnalysis {
	gaps := GapAnalysis{
		UndercoveredAreas: []string{},
	}

	for _, area := range researchAreas {
		areaHeading := "### " + area
		idx := strings.Index(synthesisText, areaHeading)
		if idx == -1 {
			// Missing section heading
			gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
			continue
		}

		// Extract section content
		content := synthesisText[idx+len(areaHeading):]
		nextSectionIdx := strings.Index(content, "\n### ") // Use ### for subsections
		if nextSectionIdx == -1 {
			nextSectionIdx = strings.Index(content, "\n## ") // Fallback to ## for main sections
		}
		if nextSectionIdx == -1 {
			nextSectionIdx = len(content)
		}
		sectionContent := strings.TrimSpace(content[:nextSectionIdx])

		// Check for gap indicator phrases
		gapPhrases := []string{
			"limited information available",
			"insufficient data",
			"not enough information",
			"no clear evidence",
			"data unavailable",
			"no information found",
			"未找到足够信息", // Chinese
			"情報が不足",     // Japanese
		}

		for _, phrase := range gapPhrases {
			if strings.Contains(strings.ToLower(sectionContent), phrase) {
				gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
				break
			}
		}

		// Also check citation density (if < 2 citations, might be weak)
		citationCount := countInlineCitationsInSection(sectionContent)
		if citationCount < 2 {
			gaps.UndercoveredAreas = append(gaps.UndercoveredAreas, area)
		}
	}

	return gaps
}

// buildGapQueries creates targeted queries for gap areas
func buildGapQueries(gaps []string, originalQuery string) []GapQuery {
	queries := make([]GapQuery, 0, len(gaps))
	for _, area := range gaps {
		queries = append(queries, GapQuery{
			Query:      fmt.Sprintf("Find detailed information about: %s (related to: %s)", area, originalQuery),
			TargetArea: area,
		})
	}
	return queries
}

// countInlineCitationsInSection counts unique inline citation references [n] in text
func countInlineCitationsInSection(text string) int {
	re := regexp.MustCompile(`\[\d+\]`)
	matches := re.FindAllString(text, -1)
	// Deduplicate (same citation can appear multiple times)
	seen := make(map[string]bool)
	for _, m := range matches {
		seen[m] = true
	}
	return len(seen)
}

// topologicalSort performs topological sort on subtasks based on dependencies
// Returns execution order (list of subtask IDs)
func topologicalSort(subtasks []activities.Subtask) []string {
	// Build adjacency list and in-degree map
	adjList := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize all subtasks
	for _, st := range subtasks {
		if _, ok := inDegree[st.ID]; !ok {
			inDegree[st.ID] = 0
		}
		for _, dep := range st.Dependencies {
			adjList[dep] = append(adjList[dep], st.ID)
			inDegree[st.ID]++
		}
	}

	// Find all nodes with in-degree 0
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Process queue
	result := make([]string, 0, len(subtasks))
	for len(queue) > 0 {
		// Pop first element
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Reduce in-degree of neighbors
		for _, neighbor := range adjList[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// If result doesn't contain all subtasks, there's a cycle
	// Fall back to original order
	if len(result) != len(subtasks) {
		result = make([]string, 0, len(subtasks))
		for _, st := range subtasks {
			result = append(result, st.ID)
		}
	}

	return result
}
