package patterns

import (
    "fmt"
    "strings"
    "time"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
    pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
    imodels "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
    wopts "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
)

// ReactConfig controls the Reason-Act-Observe loop behavior
type ReactConfig struct {
	MaxIterations     int // Maximum number of ReAct loops
	ObservationWindow int // How many recent observations to consider
	MaxObservations   int // Maximum observations to keep
	MaxThoughts       int // Maximum thoughts to track
	MaxActions        int // Maximum actions to track
}

// ReactLoopResult contains the results of a ReAct execution
type ReactLoopResult struct {
	Thoughts     []string
	Actions      []string
	Observations []string
	FinalResult  string
	TotalTokens  int
	Iterations   int
	AgentResults []activities.AgentExecutionResult
}

// ReactLoop executes a Reason-Act-Observe loop for step-by-step problem solving.
// It alternates between reasoning about what to do next, taking actions, and observing results.
func ReactLoop(
	ctx workflow.Context,
	query string,
	baseContext map[string]interface{},
	sessionID string,
	history []string,
	config ReactConfig,
	opts Options,
) (*ReactLoopResult, error) {

	logger := workflow.GetLogger(ctx)

	// Set activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Initialize state
	var observations []string
	var thoughts []string
	var actions []string
	totalTokens := 0
	iteration := 0

	// Main Reason-Act-Observe loop
	var agentResults []activities.AgentExecutionResult
	for iteration < config.MaxIterations {
		logger.Info("ReAct iteration",
			"iteration", iteration+1,
			"observations_count", len(observations),
		)

		// Phase 1: REASON - Think about what to do next
		reasonContext := make(map[string]interface{})
		for k, v := range baseContext {
			reasonContext[k] = v
		}
		reasonContext["query"] = query
		reasonContext["observations"] = getRecentObservations(observations, config.ObservationWindow)
		reasonContext["thoughts"] = thoughts
		reasonContext["actions"] = actions
		reasonContext["iteration"] = iteration

        reasonQuery := fmt.Sprintf(
            "REASON (1–2 sentences) about the single next action for: %s\nConstraints:\n- Keep response in the same language as the user's query.\n- State exactly what to do, why it's needed, and expected outcome.\n- If external information is required, say 'search'.\nContext: Previous observations: %v",
            query,
            getRecentObservations(observations, config.ObservationWindow),
        )

		var reasonResult activities.AgentExecutionResult
		var err error

		// Execute reasoning with optional budget
        if opts.BudgetAgentMax > 0 {
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if reasonContext != nil {
                if p, ok := reasonContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            err = workflow.ExecuteActivity(ctx,
                constants.ExecuteAgentWithBudgetActivity,
                activities.BudgetedAgentInput{
                    AgentInput: activities.AgentExecutionInput{
                        Query:     reasonQuery,
                        AgentID:   fmt.Sprintf("reasoner-%d", iteration),
                        Context:   reasonContext,
                        Mode:      "standard",
                        SessionID: sessionID,
                        History:   history,
                        ParentWorkflowID: wid,
                    },
                    MaxTokens: opts.BudgetAgentMax,
                    UserID:    opts.UserID,
                    TaskID:    wid,
                    ModelTier: opts.ModelTier,
                }).Get(ctx, &reasonResult)
        } else {
            // Determine parent workflow for streaming correlation
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if reasonContext != nil {
                if p, ok := reasonContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            err = workflow.ExecuteActivity(ctx,
                "ExecuteAgent",
                activities.AgentExecutionInput{
                    Query:     reasonQuery,
                    AgentID:   fmt.Sprintf("reasoner-%d", iteration),
                    Context:   reasonContext,
                    Mode:      "standard",
                    SessionID: sessionID,
                    History:   history,
                    ParentWorkflowID: wid,
                }).Get(ctx, &reasonResult)
        }

		if err != nil {
			logger.Error("Reasoning failed", "error", err)
			break
		}

		thoughts = append(thoughts, reasonResult.Response)
		// Trim thoughts if exceeding limit
		if len(thoughts) > config.MaxThoughts {
			thoughts = thoughts[len(thoughts)-config.MaxThoughts:]
		}
    totalTokens += reasonResult.TokensUsed
    // Record token usage for the reasoner step
    // Avoid double-recording when budgeted execution already recorded usage inside the activity
    if opts.BudgetAgentMax <= 0 {
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if baseContext != nil {
            if p, ok := baseContext["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        inTok := reasonResult.InputTokens
        outTok := reasonResult.OutputTokens
        if inTok == 0 && outTok == 0 && reasonResult.TokensUsed > 0 {
            inTok = reasonResult.TokensUsed * 6 / 10
            outTok = reasonResult.TokensUsed - inTok
        }
        // Fallbacks for missing model/provider
        model := reasonResult.ModelUsed
        if strings.TrimSpace(model) == "" {
            if m := pricing.GetPriorityOneModel(opts.ModelTier); m != "" {
                model = m
            }
        }
        provider := reasonResult.Provider
        if strings.TrimSpace(provider) == "" {
            provider = imodels.DetectProvider(model)
        }
        recCtx := wopts.WithTokenRecordOptions(ctx)
        _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
            UserID:       opts.UserID,
            SessionID:    sessionID,
            TaskID:       wid,
            AgentID:      fmt.Sprintf("reasoner-%d", iteration),
            Model:        model,
            Provider:     provider,
            InputTokens:  inTok,
            OutputTokens: outTok,
            Metadata:     map[string]interface{}{"phase": "react_reason"},
        }).Get(recCtx, nil)
    }

		// Check if reasoning indicates completion
		if isTaskComplete(reasonResult.Response) {
			logger.Info("Task marked complete by reasoning",
				"iteration", iteration+1,
			)
			break
		}

		// Phase 2: ACT - Execute the planned action
		actionContext := make(map[string]interface{})
		for k, v := range baseContext {
			actionContext[k] = v
		}
		actionContext["query"] = query
		actionContext["current_thought"] = reasonResult.Response
		actionContext["observations"] = getRecentObservations(observations, config.ObservationWindow)

        // Let LLM decide tools - no pattern matching
        // The agent will select appropriate tools based on the action query
        var suggestedTools []string
        // Bias toward web_search when running in research mode to ensure citations
        if baseContext != nil {
            if fr, ok := baseContext["force_research"].(bool); ok && fr {
                suggestedTools = []string{"web_search"}
            } else if rs, ok := baseContext["research_strategy"].(string); ok && strings.TrimSpace(rs) != "" {
                suggestedTools = []string{"web_search"}
            }
        }

        actionQuery := ""
        if len(suggestedTools) > 0 {
            // Research mode: be explicit about tool usage and reporting
            // Build quoted queries and site filters from context when available
            quoted := ""
            if actionContext != nil {
                if eq, ok := actionContext["exact_queries"]; ok {
                    switch t := eq.(type) {
                    case []string:
                        if len(t) > 0 { quoted = strings.Join(t, " OR ") }
                    case []interface{}:
                        parts := make([]string, 0, len(t))
                        for _, it := range t { if s, ok := it.(string); ok && s != "" { parts = append(parts, s) } }
                        if len(parts) > 0 { quoted = strings.Join(parts, " OR ") }
                    }
                }
            }
            domains := ""
            if actionContext != nil {
                if od, ok := actionContext["official_domains"]; ok {
                    switch t := od.(type) {
                    case []string:
                        if len(t) > 0 { for i, d := range t { t[i] = fmt.Sprintf("site:%s", d) }; domains = strings.Join(t, " OR ") }
                    case []interface{}:
                        parts := []string{}
                        for _, it := range t { if s, ok := it.(string); ok && s != "" { parts = append(parts, fmt.Sprintf("site:%s", s)) } }
                        if len(parts) > 0 { domains = strings.Join(parts, " OR ") }
                    }
                }
            }
            disamb := ""
            if actionContext != nil {
                if dt, ok := actionContext["disambiguation_terms"]; ok {
                    switch t := dt.(type) {
                    case []string:
                        if len(t) > 0 { disamb = strings.Join(t, ", ") }
                    case []interface{}:
                        parts := []string{}
                        for _, it := range t { if s, ok := it.(string); ok && s != "" { parts = append(parts, s) } }
                        if len(parts) > 0 { disamb = strings.Join(parts, ", ") }
                    }
                }
            }

            searchLine := fmt.Sprintf("Use web_search with queries: %s", quoted)
            if quoted == "" { searchLine = fmt.Sprintf("Use web_search to find authoritative information about: %s", query) }
            if domains != "" { searchLine += fmt.Sprintf("; prefer domains (%s)", domains) }
            if disamb != "" { searchLine += fmt.Sprintf("; add disambiguation: %s", disamb) }

            actionQuery = fmt.Sprintf(
                "ACT on this plan: %s\n\nIMPORTANT:\n- %s\n- For web_search parameters: search_type ∈ {neural|keyword|auto} (default 'auto' if unsure); category ∈ {company|research paper|news|pdf|github|tweet|personal site|linkedin|financial report}; if unsure, omit category.\n- Execute NOW and return findings in the SAME language as the user's query.\n- For each source used, include a 1–2 sentence summary plus Title and URL inline.\n- Keep the action atomic.\n- Do NOT include a '## Sources' section (the system will append Sources).",
                reasonResult.Response,
                searchLine,
            )
        } else {
            actionQuery = fmt.Sprintf(
                "ACT on this plan: %s\nConstraints:\n- Execute the next step with available tools.\n- Keep response in the SAME language as the user's query.\n- Keep actions atomic. If you used a tool that fetched information, add a brief 1–2 sentence summary of the key finding.",
                reasonResult.Response,
            )
        }

		var actionResult activities.AgentExecutionResult

		// Execute action with optional budget
        if opts.BudgetAgentMax > 0 {
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if actionContext != nil {
                if p, ok := actionContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            err = workflow.ExecuteActivity(ctx,
                constants.ExecuteAgentWithBudgetActivity,
                activities.BudgetedAgentInput{
                    AgentInput: activities.AgentExecutionInput{
                        Query:          actionQuery,
                        AgentID:        fmt.Sprintf("actor-%d", iteration),
                        Context:        actionContext,
                        Mode:           "standard",
                        SessionID:      sessionID,
                        History:        history,
                        SuggestedTools: suggestedTools,
                        ParentWorkflowID: wid,
                    },
                    MaxTokens: opts.BudgetAgentMax,
                    UserID:    opts.UserID,
                    TaskID:    wid,
                    ModelTier: opts.ModelTier,
                }).Get(ctx, &actionResult)
        } else {
            // Determine parent workflow for streaming correlation
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if actionContext != nil {
                if p, ok := actionContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            err = workflow.ExecuteActivity(ctx,
                "ExecuteAgent",
                activities.AgentExecutionInput{
                    Query:          actionQuery,
                    AgentID:        fmt.Sprintf("actor-%d", iteration),
                    Context:        actionContext,
                    Mode:           "standard",
                    SessionID:      sessionID,
                    History:        history,
                    SuggestedTools: suggestedTools,
                    ParentWorkflowID: wid,
                }).Get(ctx, &actionResult)
        }

		if err != nil {
			logger.Error("Action execution failed", "error", err)
			observations = append(observations, fmt.Sprintf("Error: %v", err))
			// Continue to next iteration to try recovery
        } else {
            // Always track tokens and tool executions for citations
            totalTokens += actionResult.TokensUsed
            agentResults = append(agentResults, actionResult)

            // Skip recording empty action responses (treat as no-op)
            if strings.TrimSpace(actionResult.Response) != "" {
                actions = append(actions, actionResult.Response)
                // Trim actions if exceeding limit
                if len(actions) > config.MaxActions {
                    actions = actions[len(actions)-config.MaxActions:]
                }
                // Phase 3: OBSERVE - Record and analyze the result
                observation := fmt.Sprintf("Action result: %s", actionResult.Response)
                observations = append(observations, observation)
            } else {
                logger.Warn("Empty action response; skipping record",
                    "iteration", iteration+1,
                    "agent_id", fmt.Sprintf("actor-%d", iteration),
                )
            }

			// Keep only recent observations to prevent memory growth
			if len(observations) > config.MaxObservations {
				// Create a summary of oldest observations
				oldCount := len(observations) - config.MaxObservations + 1
				summary := fmt.Sprintf("[%d older observations truncated]", oldCount)
				observations = append([]string{summary}, observations[len(observations)-config.MaxObservations+1:]...)
			}

            // Record token usage for the action step
            // Avoid double-recording when budgeted execution already recorded usage inside the activity
            if opts.BudgetAgentMax <= 0 {
                wid := workflow.GetInfo(ctx).WorkflowExecution.ID
                if actionContext != nil {
                    if p, ok := actionContext["parent_workflow_id"].(string); ok && p != "" {
                        wid = p
                    }
                }
                inTok := actionResult.InputTokens
                outTok := actionResult.OutputTokens
                if inTok == 0 && outTok == 0 && actionResult.TokensUsed > 0 {
                    inTok = actionResult.TokensUsed * 6 / 10
                    outTok = actionResult.TokensUsed - inTok
                }
                // Fallbacks for missing model/provider
                model := actionResult.ModelUsed
                if strings.TrimSpace(model) == "" {
                    if m := pricing.GetPriorityOneModel(opts.ModelTier); m != "" {
                        model = m
                    }
                }
                provider := actionResult.Provider
                if strings.TrimSpace(provider) == "" {
                    provider = imodels.DetectProvider(model)
                }
                recCtx := wopts.WithTokenRecordOptions(ctx)
                _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
                    UserID:       opts.UserID,
                    SessionID:    sessionID,
                    TaskID:       wid,
                    AgentID:      fmt.Sprintf("actor-%d", iteration),
                    Model:        model,
                    Provider:     provider,
                    InputTokens:  inTok,
                    OutputTokens: outTok,
                    Metadata:     map[string]interface{}{"phase": "react_action"},
                }).Get(recCtx, nil)
            }

            logger.Info("Observation recorded",
                "iteration", iteration+1,
                "total_observations", len(observations),
            )
		}

		iteration++

		// Check for early termination based on multiple criteria
		if shouldStopReactLoop(observations, thoughts, agentResults, iteration) {
			logger.Info("Early termination criteria met",
				"iteration", iteration,
				"total_agent_results", len(agentResults),
			)
			break
		}
	}

	// Final synthesis of all observations and actions
	logger.Info("Synthesizing final result from ReAct loops")

	synthesisQuery := fmt.Sprintf(
		"Synthesize the final answer for: %s\nThoughts: %v\nActions: %v\nObservations: %v",
		query,
		thoughts,
		actions,
		observations,
	)

	var finalResult activities.AgentExecutionResult

	// Execute final synthesis with optional budget
    if opts.BudgetAgentMax > 0 {
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if baseContext != nil {
            if p, ok := baseContext["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        err := workflow.ExecuteActivity(ctx,
            constants.ExecuteAgentWithBudgetActivity,
            activities.BudgetedAgentInput{
                AgentInput: activities.AgentExecutionInput{
                    Query:     synthesisQuery,
                    AgentID:   "react-synthesizer",
                    Context:   baseContext,
                    Mode:      "standard",
                    SessionID: sessionID,
                    History:   history,
                    ParentWorkflowID: wid,
                },
                MaxTokens: opts.BudgetAgentMax,
                UserID:    opts.UserID,
                TaskID:    wid,
                ModelTier: opts.ModelTier,
            }).Get(ctx, &finalResult)

		if err != nil {
			return nil, fmt.Errorf("final synthesis failed: %w", err)
		}
    } else {
        // Determine parent workflow for streaming correlation
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if baseContext != nil {
            if p, ok := baseContext["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        err := workflow.ExecuteActivity(ctx,
            "ExecuteAgent",
            activities.AgentExecutionInput{
                Query:     synthesisQuery,
                AgentID:   "react-synthesizer",
                Context:   baseContext,
                Mode:      "standard",
                SessionID: sessionID,
                History:   history,
                ParentWorkflowID: wid,
            }).Get(ctx, &finalResult)

		if err != nil {
			return nil, fmt.Errorf("final synthesis failed: %w", err)
		}
	}

totalTokens += finalResult.TokensUsed
agentResults = append(agentResults, finalResult)

    // Record token usage for the react-synthesizer step
    // Avoid double-recording when budgeted execution already recorded usage inside the activity
    if opts.BudgetAgentMax <= 0 {
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if baseContext != nil {
            if p, ok := baseContext["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        inTok := finalResult.InputTokens
        outTok := finalResult.OutputTokens
        if inTok == 0 && outTok == 0 && finalResult.TokensUsed > 0 {
            inTok = finalResult.TokensUsed * 6 / 10
            outTok = finalResult.TokensUsed - inTok
        }
        // Fallbacks for missing model/provider
        model := finalResult.ModelUsed
        if strings.TrimSpace(model) == "" {
            if m := pricing.GetPriorityOneModel(opts.ModelTier); m != "" {
                model = m
            }
        }
        provider := finalResult.Provider
        if strings.TrimSpace(provider) == "" {
            provider = imodels.DetectProvider(model)
        }
        recCtx := wopts.WithTokenRecordOptions(ctx)
        _ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
            UserID:       opts.UserID,
            SessionID:    sessionID,
            TaskID:       wid,
            AgentID:      "react-synthesizer",
            Model:        model,
            Provider:     provider,
            InputTokens:  inTok,
            OutputTokens: outTok,
            Metadata:     map[string]interface{}{"phase": "react_synth"},
        }).Get(recCtx, nil)
    }

	return &ReactLoopResult{
		Thoughts:     thoughts,
		Actions:      actions,
		Observations: observations,
		FinalResult:  finalResult.Response,
		TotalTokens:  totalTokens,
		Iterations:   iteration,
		AgentResults: agentResults,
	}, nil
}

// Helper functions

func getRecentObservations(observations []string, window int) []string {
	if len(observations) <= window {
		return observations
	}
	return observations[len(observations)-window:]
}

func isTaskComplete(reasoning string) bool {
	// Simple heuristic - in production would use structured output or LLM
	lowerReasoning := strings.ToLower(reasoning)
	completionPhrases := []string{
		"task complete",
		"problem solved",
		"found the answer",
		"successfully completed",
		"objective achieved",
		"goal reached",
		"finished",
		"done",
	}

	for _, phrase := range completionPhrases {
		if strings.Contains(lowerReasoning, phrase) {
			return true
		}
	}
	return false
}

// Removed extractToolsFromReasoning - all tool selection is now LLM-driven
// to maintain consistency with the LLM-native architecture

// shouldStopReactLoop checks multiple criteria for early termination
func shouldStopReactLoop(observations []string, thoughts []string, agentResults []activities.AgentExecutionResult, iteration int) bool {
	// Need at least 2 iterations to compare
	if iteration < 2 {
		return false
	}

	// Criterion 1: High confidence solution found
	if hasHighConfidenceSolution(observations, thoughts) {
		return true
	}

	// Criterion 2: Last two iterations returned similar results (convergence)
	var lastObs, prevObs string
	if len(observations) >= 2 {
		lastObs = observations[len(observations)-1]
		prevObs = observations[len(observations)-2]
		if areSimilar(lastObs, prevObs) {
			return true
		}
	}

	// Criterion 3: No new citations/information in last 2 iterations
	if len(agentResults) >= 2 && len(observations) >= 2 {
		lastCitationCount := countCitations(agentResults[len(agentResults)-1])
		prevCitationCount := countCitations(agentResults[len(agentResults)-2])

		// If both iterations have citations and counts are the same, likely no new info
		if lastCitationCount > 0 && prevCitationCount > 0 && lastCitationCount == prevCitationCount {
			// Also check if observation length is similar (not significantly more content)
			if len(lastObs) > 0 && len(prevObs) > 0 {
				lengthRatio := float64(len(lastObs)) / float64(len(prevObs))
				if lengthRatio > 0.8 && lengthRatio < 1.2 {
					return true
				}
			}
		}
	}

	return false
}

func hasHighConfidenceSolution(observations []string, thoughts []string) bool {
	// Check if we have strong indicators of a solution
	if len(observations) == 0 || len(thoughts) == 0 {
		return false
	}

	// Look for success indicators in recent observations
	recentObs := getRecentObservations(observations, 3)
	for _, obs := range recentObs {
		lowerObs := strings.ToLower(obs)
		if strings.Contains(lowerObs, "success") ||
			strings.Contains(lowerObs, "correct") ||
			strings.Contains(lowerObs, "solved") ||
			strings.Contains(lowerObs, "answer is") ||
			strings.Contains(lowerObs, "found") ||
			strings.Contains(lowerObs, "comprehensive") {
			return true
		}
	}

	return false
}

// areSimilar checks if two strings are similar (simple overlap check)
func areSimilar(s1, s2 string) bool {
	if s1 == s2 {
		return true
	}

	// Check length similarity first
	len1, len2 := len(s1), len(s2)
	if len1 == 0 || len2 == 0 {
		return false
	}

	lengthRatio := float64(len1) / float64(len2)
	if lengthRatio < 0.5 || lengthRatio > 2.0 {
		return false
	}

	// Check for high word overlap
	words1 := strings.Fields(strings.ToLower(s1))
	words2 := strings.Fields(strings.ToLower(s2))

	if len(words1) == 0 || len(words2) == 0 {
		return false
	}

	wordSet := make(map[string]bool)
	for _, w := range words1 {
		if len(w) > 3 { // Only consider words longer than 3 chars
			wordSet[w] = true
		}
	}

	overlap := 0
	for _, w := range words2 {
		if len(w) > 3 && wordSet[w] {
			overlap++
		}
	}

	// If >70% of significant words overlap, consider similar
	overlapRatio := float64(overlap) / float64(len(words2))
	return overlapRatio > 0.7
}

// countCitations counts how many citations are in an agent result
func countCitations(result activities.AgentExecutionResult) int {
	count := 0
	for _, toolExec := range result.ToolExecutions {
		if toolExec.Tool == "web_search" && toolExec.Success {
			// Rough estimate: count URLs in output
			if output, ok := toolExec.Output.(string); ok {
				count += strings.Count(output, "http")
			}
		}
	}
	return count
}
