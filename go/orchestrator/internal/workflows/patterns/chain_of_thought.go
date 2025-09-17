package patterns

import (
	"fmt"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ChainOfThoughtConfig configures the chain-of-thought pattern
type ChainOfThoughtConfig struct {
	MaxSteps              int    // Maximum reasoning steps
	RequireExplanation    bool   // Require step-by-step explanation
	ShowIntermediateSteps bool   // Include intermediate reasoning in result
	PromptTemplate        string // Custom prompt template
	StepDelimiter         string // Delimiter between steps
	ModelTier             string // Model tier to use
}

// ChainOfThoughtResult contains the result of chain-of-thought reasoning
type ChainOfThoughtResult struct {
	FinalAnswer    string
	ReasoningSteps []string
	TotalTokens    int
	Confidence     float64
	StepDurations  []time.Duration
}

// ChainOfThought implements step-by-step reasoning pattern
// This pattern guides an agent through explicit reasoning steps before reaching a conclusion
func ChainOfThought(
	ctx workflow.Context,
	query string,
	context map[string]interface{},
	sessionID string,
	history []string,
	config ChainOfThoughtConfig,
	opts Options,
) (*ChainOfThoughtResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Chain-of-Thought reasoning",
		"query", query,
		"max_steps", config.MaxSteps,
	)

	// Set activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Set defaults
	if config.MaxSteps == 0 {
		config.MaxSteps = 5
	}
	if config.StepDelimiter == "" {
		config.StepDelimiter = "\n→ "
	}
	if config.ModelTier == "" {
		config.ModelTier = opts.ModelTier
		if config.ModelTier == "" {
			config.ModelTier = "medium"
		}
	}

	result := &ChainOfThoughtResult{
		ReasoningSteps: make([]string, 0, config.MaxSteps),
		StepDurations:  make([]time.Duration, 0, config.MaxSteps),
	}

	// Build the chain-of-thought prompt
	cotPrompt := buildChainOfThoughtPrompt(query, config)

	// Execute chain-of-thought reasoning
	startTime := workflow.Now(ctx)

	var cotResult activities.AgentExecutionResult
	if opts.BudgetAgentMax > 0 {
		wid := workflow.GetInfo(ctx).WorkflowExecution.ID
		err := workflow.ExecuteActivity(ctx,
			constants.ExecuteAgentWithBudgetActivity,
			activities.BudgetedAgentInput{
				AgentInput: activities.AgentExecutionInput{
					Query:     cotPrompt,
					AgentID:   "cot-reasoner",
					Context:   context,
					Mode:      "reasoning",
					SessionID: sessionID,
					History:   history,
				},
				MaxTokens: opts.BudgetAgentMax,
				UserID:    opts.UserID,
				TaskID:    wid,
				ModelTier: config.ModelTier,
			}).Get(ctx, &cotResult)
		if err != nil {
			return nil, fmt.Errorf("chain-of-thought reasoning failed: %w", err)
		}
	} else {
		err := workflow.ExecuteActivity(ctx,
			activities.ExecuteAgent,
			activities.AgentExecutionInput{
				Query:     cotPrompt,
				AgentID:   "cot-reasoner",
				Context:   context,
				Mode:      "reasoning",
				SessionID: sessionID,
				History:   history,
			}).Get(ctx, &cotResult)
		if err != nil {
			return nil, fmt.Errorf("chain-of-thought reasoning failed: %w", err)
		}
	}

	duration := workflow.Now(ctx).Sub(startTime)
	result.StepDurations = append(result.StepDurations, duration)
	result.TotalTokens = cotResult.TokensUsed

	// Parse reasoning steps from response
	steps := parseReasoningSteps(cotResult.Response, config.StepDelimiter)
	result.ReasoningSteps = steps

	// Extract final answer
	result.FinalAnswer = extractFinalAnswer(cotResult.Response, steps)

	// Calculate confidence based on reasoning clarity
	result.Confidence = calculateReasoningConfidence(steps, cotResult.Response)

	// If we need to validate the reasoning, do an additional check
	if config.RequireExplanation && result.Confidence < 0.7 {
		logger.Info("Low confidence reasoning, requesting clarification")

		clarificationPrompt := fmt.Sprintf(
			"The previous reasoning for '%s' had unclear steps. Please provide a clearer step-by-step explanation:\n%s",
			query,
			strings.Join(steps, config.StepDelimiter),
		)

		var clarifyResult activities.AgentExecutionResult
		if opts.BudgetAgentMax > 0 {
			wid := workflow.GetInfo(ctx).WorkflowExecution.ID
			err := workflow.ExecuteActivity(ctx,
				constants.ExecuteAgentWithBudgetActivity,
				activities.BudgetedAgentInput{
					AgentInput: activities.AgentExecutionInput{
						Query:     clarificationPrompt,
						AgentID:   "cot-clarifier",
						Context:   context,
						Mode:      "reasoning",
						SessionID: sessionID,
						History:   append(history, fmt.Sprintf("Previous: %s", cotResult.Response)),
					},
					MaxTokens: opts.BudgetAgentMax / 2, // Use less budget for clarification
					UserID:    opts.UserID,
					TaskID:    wid,
					ModelTier: config.ModelTier,
				}).Get(ctx, &clarifyResult)
			if err == nil {
				// Update with clarified reasoning
				clarifiedSteps := parseReasoningSteps(clarifyResult.Response, config.StepDelimiter)
				if len(clarifiedSteps) > 0 {
					result.ReasoningSteps = clarifiedSteps
					result.FinalAnswer = extractFinalAnswer(clarifyResult.Response, clarifiedSteps)
					result.Confidence = calculateReasoningConfidence(clarifiedSteps, clarifyResult.Response)
				}
				result.TotalTokens += clarifyResult.TokensUsed
			}
		}
	}

	// Format the result based on configuration
	if config.ShowIntermediateSteps {
		stepsText := strings.Join(result.ReasoningSteps, config.StepDelimiter)
		result.FinalAnswer = fmt.Sprintf(
			"Reasoning:\n%s\n\nFinal Answer: %s",
			stepsText,
			result.FinalAnswer,
		)
	}

	logger.Info("Chain-of-Thought completed",
		"steps", len(result.ReasoningSteps),
		"tokens", result.TotalTokens,
		"confidence", result.Confidence,
	)

	return result, nil
}

// buildChainOfThoughtPrompt creates the prompt for chain-of-thought reasoning
func buildChainOfThoughtPrompt(query string, config ChainOfThoughtConfig) string {
	if config.PromptTemplate != "" {
		return strings.ReplaceAll(config.PromptTemplate, "{query}", query)
	}

	// Default chain-of-thought prompt
	return fmt.Sprintf(`Please solve this step-by-step:

Question: %s

Think through this systematically:
1. First, identify what is being asked
2. Break down the problem into steps
3. Work through each step with clear reasoning
4. Show your work and explain your thinking
5. Arrive at the final answer

Use "→" to mark each reasoning step.
End with "Therefore:" followed by your final answer.`, query)
}

// parseReasoningSteps extracts reasoning steps from the response
func parseReasoningSteps(response, delimiter string) []string {
	// Look for step markers
	lines := strings.Split(response, "\n")
	steps := []string{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Check for step indicators
		if strings.HasPrefix(line, "→") ||
			strings.HasPrefix(line, "Step") ||
			strings.HasPrefix(line, "1.") ||
			strings.HasPrefix(line, "2.") ||
			strings.HasPrefix(line, "3.") ||
			strings.HasPrefix(line, "•") {
			steps = append(steps, line)
		}
	}

	// If no explicit steps found, try to extract logical segments
	if len(steps) == 0 {
		segments := strings.Split(response, ". ")
		for _, segment := range segments {
			if len(strings.TrimSpace(segment)) > 20 { // Meaningful segment
				steps = append(steps, segment)
				if len(steps) >= 5 { // Limit to reasonable number
					break
				}
			}
		}
	}

	return steps
}

// extractFinalAnswer gets the final answer from the reasoning
func extractFinalAnswer(response string, steps []string) string {
	// Look for explicit final answer markers
	markers := []string{
		"Therefore:",
		"Final Answer:",
		"The answer is:",
		"In conclusion:",
		"Result:",
	}

	lowerResponse := strings.ToLower(response)
	for _, marker := range markers {
		markerLower := strings.ToLower(marker)
		if idx := strings.Index(lowerResponse, markerLower); idx != -1 {
			answer := response[idx+len(marker):]
			// Take first paragraph/sentence as answer
			if endIdx := strings.Index(answer, "\n\n"); endIdx > 0 {
				answer = answer[:endIdx]
			}
			return strings.TrimSpace(answer)
		}
	}

	// If no explicit marker, use last step or last paragraph
	if len(steps) > 0 {
		return steps[len(steps)-1]
	}

	// Fallback: last paragraph
	paragraphs := strings.Split(response, "\n\n")
	if len(paragraphs) > 0 {
		return paragraphs[len(paragraphs)-1]
	}

	return response
}

// calculateReasoningConfidence estimates confidence based on reasoning quality
func calculateReasoningConfidence(steps []string, response string) float64 {
	confidence := 0.5 // Base confidence

	// More steps indicate thorough reasoning
	if len(steps) >= 3 {
		confidence += 0.2
	}

	// Check for logical connectors
	logicalTerms := []string{
		"therefore", "because", "since", "thus",
		"consequently", "hence", "so", "implies",
	}
	lowerResponse := strings.ToLower(response)
	logicalCount := 0
	for _, term := range logicalTerms {
		logicalCount += strings.Count(lowerResponse, term)
	}
	if logicalCount >= 3 {
		confidence += 0.15
	}

	// Check for structured reasoning
	if strings.Contains(response, "Step") || strings.Contains(response, "→") {
		confidence += 0.1
	}

	// Check for conclusion
	if strings.Contains(lowerResponse, "therefore") ||
		strings.Contains(lowerResponse, "final answer") {
		confidence += 0.05
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}
