package patterns

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	imodels "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
	pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	wopts "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
)

// BrowserConfig controls the browser automation loop behavior
type BrowserConfig struct {
	MaxIterations int // Maximum number of browser action loops
	ActionTimeout int // Timeout for each action in milliseconds
}

// CheckPauseFunc is a callback for checking pause/cancel signals during iteration
type CheckPauseFunc func(ctx workflow.Context, checkpoint string) error

// BrowserLoopResult contains the results of browser automation
type BrowserLoopResult struct {
	Actions      []string
	Observations []string
	FinalResult  string
	TotalTokens  int
	Iterations   int
	AgentResults []activities.AgentExecutionResult
}

// BrowserLoop executes a unified agent loop for browser automation.
// Unlike ReactLoop which splits REASON and ACT into separate agent calls,
// BrowserLoop uses a single agent that reasons and acts together.
//
// This matches modern browser automation approaches (Claude Code, Manus):
// 1. Agent receives: task + current page state + action history
// 2. Agent reasons about what to do AND executes tools in one call
// 3. Tool results become observations for next iteration
// 4. Repeat until task complete or max iterations
func BrowserLoop(
	ctx workflow.Context,
	query string,
	baseContext map[string]interface{},
	sessionID string,
	history []string,
	config BrowserConfig,
	opts Options,
	checkPause CheckPauseFunc,
) (*BrowserLoopResult, error) {
	logger := workflow.GetLogger(ctx)

	// Set activity options with longer timeout for browser actions
	activityTimeout := 3 * time.Minute
	if config.ActionTimeout > 0 {
		activityTimeout = time.Duration(config.ActionTimeout) * time.Millisecond
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: activityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Initialize state
	var observations []string
	var actions []string
	totalTokens := 0
	iteration := 0

	var agentResults []activities.AgentExecutionResult

	// Get workflow ID for SSE events
	wfID := workflow.GetInfo(ctx).WorkflowExecution.ID
	if baseContext != nil {
		if p, ok := baseContext["parent_workflow_id"].(string); ok && p != "" {
			wfID = p
		}
	}

	// Emit browser automation started
	streaming.Get().Publish(wfID, streaming.Event{
		WorkflowID: wfID,
		Type:       "PROGRESS",
		AgentID:    "browser",
		Message:    "Starting browser automation",
		Timestamp:  workflow.Now(ctx),
	})

	// Main unified agent loop
	for iteration < config.MaxIterations {
		// Check for pause/cancel at start of each iteration
		if checkPause != nil {
			if err := checkPause(ctx, fmt.Sprintf("pre_iteration_%d", iteration)); err != nil {
				return &BrowserLoopResult{
					Actions:      actions,
					Observations: observations,
					FinalResult:  "Browser automation paused/cancelled",
					TotalTokens:  totalTokens,
					Iterations:   iteration,
					AgentResults: agentResults,
				}, err
			}
		}

		logger.Info("Browser loop iteration",
			"iteration", iteration+1,
			"observations_count", len(observations),
		)

		agentID := agents.GetAgentName(wfID, iteration)

		// Emit iteration progress
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       "PROGRESS",
			AgentID:    "browser",
			Message:    fmt.Sprintf("Browser action %d/%d", iteration+1, config.MaxIterations),
			Timestamp:  workflow.Now(ctx),
		})

		// Emit agent started
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       "AGENT_STARTED",
			AgentID:    agentID,
			Message:    "Analyzing page and taking action",
			Timestamp:  workflow.Now(ctx),
		})

		// Build unified context for single agent call
		agentContext := make(map[string]interface{})
		for k, v := range baseContext {
			agentContext[k] = v
		}
		agentContext["query"] = query
		agentContext["iteration"] = iteration
		agentContext["previous_actions"] = actions

		// Include recent observations (truncated to prevent context overflow)
		recentObs := observations
		if len(recentObs) > 5 {
			recentObs = recentObs[len(recentObs)-5:]
		}
		agentContext["observations"] = recentObs

		// Build the unified prompt that combines reasoning and action
		agentQuery := buildBrowserAgentPrompt(query, actions, recentObs, iteration)

		var agentResult activities.AgentExecutionResult
		var err error

		// Execute unified agent (reason + act together)
		if opts.BudgetAgentMax > 0 {
			err = workflow.ExecuteActivity(ctx,
				constants.ExecuteAgentWithBudgetActivity,
				activities.BudgetedAgentInput{
					AgentInput: activities.AgentExecutionInput{
						Query:            agentQuery,
						AgentID:          agentID,
						Context:          agentContext,
						Mode:             "standard",
						SessionID:        sessionID,
						History:          history,
						ParentWorkflowID: wfID,
					},
					MaxTokens: opts.BudgetAgentMax,
					UserID:    opts.UserID,
					TaskID:    wfID,
					ModelTier: opts.ModelTier,
				}).Get(ctx, &agentResult)
		} else {
			err = workflow.ExecuteActivity(ctx,
				"ExecuteAgent",
				activities.AgentExecutionInput{
					Query:            agentQuery,
					AgentID:          agentID,
					Context:          agentContext,
					Mode:             "standard",
					SessionID:        sessionID,
					History:          history,
					ParentWorkflowID: wfID,
				}).Get(ctx, &agentResult)
		}

		if err != nil {
			logger.Error("Browser agent execution failed", "error", err)
			observations = append(observations, fmt.Sprintf("Error: %v", err))
			iteration++
			continue
		}

		// Emit agent completed
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       "AGENT_COMPLETED",
			AgentID:    agentID,
			Message:    "Action completed",
			Timestamp:  workflow.Now(ctx),
		})

		totalTokens += agentResult.TokensUsed
		agentResults = append(agentResults, agentResult)

		// Process response - remove base64 screenshot data to prevent context overflow
		if strings.TrimSpace(agentResult.Response) != "" {
			truncatedAction := truncateBase64Images(agentResult.Response)
			actions = append(actions, truncatedAction)

			// Build observation from tool executions (screenshots already emitted via TOOL_OBSERVATION)
			observation := buildObservationFromTools(agentResult.ToolExecutions)
			if observation != "" {
				observations = append(observations, observation)
			}
		}

		// Limit action/observation history to prevent memory growth
		if len(actions) > 20 {
			actions = actions[len(actions)-20:]
		}
		if len(observations) > 20 {
			observations = observations[len(observations)-20:]
		}

		// Record token usage
		if opts.BudgetAgentMax <= 0 {
			recordBrowserTokenUsage(ctx, wfID, sessionID, opts, agentID, agentResult)
		}

		iteration++

		// Check for task completion
		if isBrowserTaskComplete(agentResult) {
			logger.Info("Browser task completed",
				"iteration", iteration,
				"tool_count", len(agentResult.ToolExecutions),
			)
			break
		}

		// Check for no-progress (agent didn't take any action)
		// Use response pattern detection since tools execute in Python
		if !responseIndicatesToolUse(agentResult.Response) {
			logger.Warn("No tool execution detected in response, may be stuck",
				"iteration", iteration,
			)
			// Give agent 2 more chances before breaking
			if iteration > 2 && !hasRecentToolExecutions(agentResults, 2) {
				logger.Info("No recent tool executions, stopping loop")
				break
			}
		}
	}

	// Emit loop completed
	streaming.Get().Publish(wfID, streaming.Event{
		WorkflowID: wfID,
		Type:       "PROGRESS",
		AgentID:    "browser",
		Message:    "Browser automation completed",
		Timestamp:  workflow.Now(ctx),
	})

	// Build final result from last agent response
	finalResult := ""
	if len(agentResults) > 0 {
		lastResult := agentResults[len(agentResults)-1]
		finalResult = truncateBase64Images(lastResult.Response)
	}

	// If no useful final result, synthesize from observations
	if strings.TrimSpace(finalResult) == "" && len(observations) > 0 {
		finalResult = fmt.Sprintf("Browser automation completed after %d iterations. Final observations:\n%s",
			iteration, strings.Join(observations[max(0, len(observations)-3):], "\n"))
	}

	return &BrowserLoopResult{
		Actions:      actions,
		Observations: observations,
		FinalResult:  finalResult,
		TotalTokens:  totalTokens,
		Iterations:   iteration,
		AgentResults: agentResults,
	}, nil
}

// buildBrowserAgentPrompt creates a unified prompt for browser automation
func buildBrowserAgentPrompt(query string, actions []string, observations []string, iteration int) string {
	var sb strings.Builder

	sb.WriteString("You are a browser automation agent. Analyze the current page state and take the next action to complete the task.\n\n")
	sb.WriteString(fmt.Sprintf("TASK: %s\n\n", query))

	if len(actions) > 0 {
		sb.WriteString("PREVIOUS ACTIONS:\n")
		for i, a := range actions {
			// Only show last 5 actions to save context
			if i >= len(actions)-5 {
				sb.WriteString(fmt.Sprintf("- %s\n", truncateStringBrowser(a, 200)))
			}
		}
		sb.WriteString("\n")
	}

	if len(observations) > 0 {
		sb.WriteString("CURRENT STATE (from previous tool results):\n")
		for _, obs := range observations {
			sb.WriteString(fmt.Sprintf("- %s\n", truncateStringBrowser(obs, 300)))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("INSTRUCTIONS:\n")
	sb.WriteString("1. Decide what action to take next to progress toward the goal\n")
	sb.WriteString("2. Use the appropriate browser tool (browser_navigate, browser_click, browser_type, browser_screenshot, etc.)\n")
	sb.WriteString("3. If the task is complete, summarize what was accomplished\n")
	sb.WriteString("4. If you need to see the current page, use browser_screenshot\n")
	sb.WriteString("5. Keep your response concise - focus on the action, not lengthy explanations\n\n")

	if iteration == 0 {
		sb.WriteString("This is the first iteration. Start by navigating to the target page or taking a screenshot to understand the current state.\n")
	}

	return sb.String()
}

// buildObservationFromTools creates a compact observation string from tool executions
func buildObservationFromTools(toolExecs []activities.ToolExecution) string {
	if len(toolExecs) == 0 {
		return ""
	}

	var parts []string
	for _, te := range toolExecs {
		if te.Success {
			// Compact success message
			switch te.Tool {
			case "browser_navigate":
				if output, ok := te.Output.(map[string]interface{}); ok {
					title := output["title"]
					url := output["url"]
					parts = append(parts, fmt.Sprintf("Navigated to: %s (%s)", title, url))
				} else {
					parts = append(parts, fmt.Sprintf("%s: success", te.Tool))
				}
			case "browser_click":
				parts = append(parts, "Click action completed")
			case "browser_type":
				parts = append(parts, "Text input completed")
			case "browser_screenshot":
				// Don't include screenshot data - it's emitted separately
				parts = append(parts, "Screenshot captured (see UI)")
			case "browser_extract":
				if output, ok := te.Output.(map[string]interface{}); ok {
					if content, ok := output["content"].(string); ok {
						parts = append(parts, fmt.Sprintf("Extracted: %s", truncateStringBrowser(content, 500)))
					}
				}
			default:
				parts = append(parts, fmt.Sprintf("%s: success", te.Tool))
			}
		} else {
			parts = append(parts, fmt.Sprintf("%s failed: %s", te.Tool, truncateStringBrowser(te.Error, 100)))
		}
	}

	return strings.Join(parts, "; ")
}

// isBrowserTaskComplete checks if the browser task should be considered complete
func isBrowserTaskComplete(result activities.AgentExecutionResult) bool {
	response := strings.ToLower(result.Response)

	// Check for completion phrases - agent explicitly stating task is done
	completionPhrases := []string{
		"task complete",
		"task completed",
		"successfully completed",
		"all steps completed",
		"i have completed",
		"have been completed",
	}

	for _, phrase := range completionPhrases {
		if strings.Contains(response, phrase) {
			return true
		}
	}

	// NOTE: We do NOT check for hasToolExecution here because:
	// 1. Tools are executed in Python LLM service, not always tracked in Go struct
	// 2. The agent might just be reporting tool results, not signaling completion
	// 3. Stuck detection is handled separately via hasRecentToolExecutions

	return false
}

// hasToolExecutionBrowser checks if any tools were executed
// Note: This checks the Go struct, but tools executed in Python may not be tracked here.
// Use responseIndicatesToolUse for more reliable detection.
func hasToolExecutionBrowser(toolExecs []activities.ToolExecution) bool {
	return len(toolExecs) > 0
}

// responseIndicatesToolUse checks if the response contains patterns indicating a browser tool was used
func responseIndicatesToolUse(response string) bool {
	// Browser tool result patterns
	toolPatterns := []string{
		"browser_navigate result",
		"browser_click result",
		"browser_type result",
		"browser_screenshot result",
		"browser_extract result",
		"browser_scroll result",
		"navigated to",
		"clicked on",
		"screenshot captured",
		"url\":",
		"title\":",
	}

	responseLower := strings.ToLower(response)
	for _, pattern := range toolPatterns {
		if strings.Contains(responseLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// hasRecentToolExecutions checks if tools were executed in recent iterations
func hasRecentToolExecutions(results []activities.AgentExecutionResult, lookback int) bool {
	start := len(results) - lookback
	if start < 0 {
		start = 0
	}
	for i := start; i < len(results); i++ {
		// Check Go struct first
		if hasToolExecutionBrowser(results[i].ToolExecutions) {
			return true
		}
		// Also check response for tool execution patterns (for Python-executed tools)
		if responseIndicatesToolUse(results[i].Response) {
			return true
		}
	}
	return false
}

// truncateStringBrowser truncates a string to maxLen chars with ellipsis
func truncateStringBrowser(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// recordBrowserTokenUsage records token usage for browser automation
func recordBrowserTokenUsage(ctx workflow.Context, wfID, sessionID string, opts Options, agentID string, result activities.AgentExecutionResult) {
	inTok := result.InputTokens
	outTok := result.OutputTokens
	if inTok == 0 && outTok == 0 && result.TokensUsed > 0 {
		inTok = result.TokensUsed * 6 / 10
		outTok = result.TokensUsed - inTok
	}

	model := result.ModelUsed
	if strings.TrimSpace(model) == "" {
		if m := pricing.GetPriorityOneModel(opts.ModelTier); m != "" {
			model = m
		}
	}
	provider := result.Provider
	if strings.TrimSpace(provider) == "" {
		provider = imodels.DetectProvider(model)
	}

	recCtx := wopts.WithTokenRecordOptions(ctx)
	_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
		UserID:       opts.UserID,
		SessionID:    sessionID,
		TaskID:       wfID,
		AgentID:      agentID,
		Model:        model,
		Provider:     provider,
		InputTokens:  inTok,
		OutputTokens: outTok,
		Metadata:     map[string]interface{}{"phase": "browser_action"},
	}).Get(recCtx, nil)
}
