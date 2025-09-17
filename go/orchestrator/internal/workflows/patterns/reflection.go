package patterns

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// ReflectOnResult evaluates and potentially improves a result through iterative reflection.
// It uses the EvaluateResult activity to score the response and re-synthesizes with feedback if needed.
// Returns: improved result, final quality score, total tokens used, and any error.
func ReflectOnResult(
	ctx workflow.Context,
	query string,
	initialResult string,
	agentResults []activities.AgentExecutionResult, // For re-synthesis
	baseContext map[string]interface{},
	config ReflectionConfig,
	opts Options,
) (string, float64, int, error) {

	logger := workflow.GetLogger(ctx)
	finalResult := initialResult
	var totalTokens int
	var retryCount int
	var lastScore float64 = 0.5 // Default score

	// Early exit if reflection is disabled
	if !config.Enabled {
		return finalResult, lastScore, totalTokens, nil
	}

	for retryCount < config.MaxRetries {
		// Evaluate current result quality
		var evalResult activities.EvaluateResultOutput
		evalCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(config.TimeoutMs) * time.Millisecond,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})

		err := workflow.ExecuteActivity(evalCtx, "EvaluateResult",
			activities.EvaluateResultInput{
				Query:    query,
				Response: finalResult,
				Criteria: config.Criteria,
			}).Get(ctx, &evalResult)

		if err != nil {
			logger.Warn("Reflection evaluation failed, using current result", "error", err)
			return finalResult, lastScore, totalTokens, nil
		}

		lastScore = evalResult.Score
		logger.Info("Reflection evaluation completed",
			"score", evalResult.Score,
			"threshold", config.ConfidenceThreshold,
			"retry_count", retryCount)

		// Check if meets quality threshold
		if evalResult.Score >= config.ConfidenceThreshold {
			logger.Info("Response meets quality threshold, no retry needed")
			return finalResult, evalResult.Score, totalTokens, nil
		}

		// Result doesn't meet threshold, check if we can retry
		retryCount++
		if retryCount >= config.MaxRetries {
			logger.Info("Max reflection retries reached, using best effort result")
			return finalResult, evalResult.Score, totalTokens, nil
		}

		logger.Info("Response below threshold, retrying with reflection feedback",
			"feedback", evalResult.Feedback,
			"retry", retryCount)

		// Build reflection context with feedback
		reflectionContext := make(map[string]interface{})
		for k, v := range baseContext {
			reflectionContext[k] = v
		}
		reflectionContext["reflection_feedback"] = evalResult.Feedback
		reflectionContext["previous_response"] = finalResult
		reflectionContext["improvement_needed"] = true

		// Re-synthesize with feedback
		var improvedSynthesis activities.SynthesisResult
		synthCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute, // Allow more time for synthesis
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
		})

		err = workflow.ExecuteActivity(synthCtx, "SynthesizeResultsLLM",
			activities.SynthesisInput{
				Query:        query,
				AgentResults: agentResults,
				Context:      reflectionContext,
			}).Get(ctx, &improvedSynthesis)

		if err != nil {
			logger.Warn("Reflection re-synthesis failed, keeping previous result", "error", err)
			return finalResult, evalResult.Score, totalTokens, nil
		}

		// Update result and track tokens
		finalResult = improvedSynthesis.FinalResult
		totalTokens += improvedSynthesis.TokensUsed

		logger.Info("Reflection iteration completed",
			"retry", retryCount,
			"tokens_used", improvedSynthesis.TokensUsed)
	}

	return finalResult, lastScore, totalTokens, nil
}
