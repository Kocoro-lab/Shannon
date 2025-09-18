package patterns

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
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
			"REASON about the next step for: %s\nPrevious observations: %v\nWhat should I do next and why?",
			query,
			getRecentObservations(observations, config.ObservationWindow),
		)

		var reasonResult activities.AgentExecutionResult
		var err error

		// Execute reasoning with optional budget
		if opts.BudgetAgentMax > 0 {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
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
					},
					MaxTokens: opts.BudgetAgentMax,
					UserID:    opts.UserID,
					TaskID:    wid,
					ModelTier: opts.ModelTier,
				}).Get(ctx, &reasonResult)
		} else {
			err = workflow.ExecuteActivity(ctx,
				"ExecuteAgent",
				activities.AgentExecutionInput{
					Query:     reasonQuery,
					AgentID:   fmt.Sprintf("reasoner-%d", iteration),
					Context:   reasonContext,
					Mode:      "standard",
					SessionID: sessionID,
					History:   history,
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

		// Determine which tools to use based on reasoning
		suggestedTools := extractToolsFromReasoning(reasonResult.Response)

		actionQuery := fmt.Sprintf(
			"ACT on this plan: %s\nExecute the next step with available tools.",
			reasonResult.Response,
		)

		var actionResult activities.AgentExecutionResult

		// Execute action with optional budget
		if opts.BudgetAgentMax > 0 {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
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
					},
					MaxTokens: opts.BudgetAgentMax,
					UserID:    opts.UserID,
					TaskID:    wid,
					ModelTier: opts.ModelTier,
				}).Get(ctx, &actionResult)
		} else {
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
				}).Get(ctx, &actionResult)
		}

		if err != nil {
			logger.Error("Action execution failed", "error", err)
			observations = append(observations, fmt.Sprintf("Error: %v", err))
			// Continue to next iteration to try recovery
		} else {
			actions = append(actions, actionResult.Response)
			// Trim actions if exceeding limit
			if len(actions) > config.MaxActions {
				actions = actions[len(actions)-config.MaxActions:]
			}
			totalTokens += actionResult.TokensUsed

			// Phase 3: OBSERVE - Record and analyze the result
			observation := fmt.Sprintf("Action result: %s", actionResult.Response)
			observations = append(observations, observation)

			// Keep only recent observations to prevent memory growth
			if len(observations) > config.MaxObservations {
				// Create a summary of oldest observations
				oldCount := len(observations) - config.MaxObservations + 1
				summary := fmt.Sprintf("[%d older observations truncated]", oldCount)
				observations = append([]string{summary}, observations[len(observations)-config.MaxObservations+1:]...)
			}

			logger.Info("Observation recorded",
				"iteration", iteration+1,
				"observation_length", len(observation),
			)
		}

		iteration++

		// Check for early termination based on confidence
		if hasHighConfidenceSolution(observations, thoughts) {
			logger.Info("High confidence solution found",
				"iteration", iteration,
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
		err := workflow.ExecuteActivity(ctx,
			"ExecuteAgent",
			activities.AgentExecutionInput{
				Query:     synthesisQuery,
				AgentID:   "react-synthesizer",
				Context:   baseContext,
				Mode:      "standard",
				SessionID: sessionID,
				History:   history,
			}).Get(ctx, &finalResult)

		if err != nil {
			return nil, fmt.Errorf("final synthesis failed: %w", err)
		}
	}

	totalTokens += finalResult.TokensUsed
	agentResults = append(agentResults, finalResult)

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

func extractToolsFromReasoning(reasoning string) []string {
	// Simple keyword-based tool extraction
	// In production, this could use more sophisticated NLP
	var tools []string
	lowerReasoning := strings.ToLower(reasoning)

	toolKeywords := map[string][]string{
		"calculator":    {"calculate", "compute", "math", "arithmetic"},
		"web_search":    {"search", "look up", "find information"},
		"code_executor": {"run code", "execute", "python", "script"},
		"file_ops":      {"read file", "write file", "save", "load"},
	}

	for tool, keywords := range toolKeywords {
		for _, keyword := range keywords {
			if strings.Contains(lowerReasoning, keyword) {
				tools = append(tools, tool)
				break
			}
		}
	}

	return tools
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
			strings.Contains(lowerObs, "answer is") {
			return true
		}
	}

	return false
}
