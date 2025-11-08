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
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

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

		reactConfig := patterns.ReactConfig{
			MaxIterations:     5,
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
		// Complex research - use parallel/hybrid execution
		logger.Info("Using parallel execution for complex research",
			"complexity", decomp.ComplexityScore,
			"subtasks", len(decomp.Subtasks),
		)

		// Determine execution strategy
		hasDepencies := false
		for _, subtask := range decomp.Subtasks {
			if len(subtask.Dependencies) > 0 {
				hasDepencies = true
				break
			}
		}

		if hasDepencies {
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
        if len(citations) > 0 {
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
                "credibility_score": c.CredibilityScore,
                "quality_score":     c.QualityScore,
            }
            verCitations = append(verCitations, m)
        }
        _ = workflow.ExecuteActivity(ctx, "VerifyClaimsActivity", activities.VerifyClaimsInput{
            Answer:    finalResult,
            Citations: verCitations,
        }).Get(ctx, &verification)
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

    // Aggregate agent metadata (model, provider, tokens, cost)
    agentMeta := metadata.AggregateAgentMetadata(agentResults, synthesis.TokensUsed+reflectionTokens)
    for k, v := range agentMeta {
        meta[k] = v
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
