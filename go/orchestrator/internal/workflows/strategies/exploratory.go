package strategies

import (
	"fmt"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"go.temporal.io/sdk/workflow"
)

// ExploratoryWorkflow implements iterative discovery with hypothesis testing using patterns
// This workflow explores a problem space through tree-of-thoughts pattern for systematic exploration
func ExploratoryWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ExploratoryWorkflow with patterns",
		"query", input.Query,
		"user_id", input.UserID,
	)

	// Input validation
	if err := validateInput(input); err != nil {
		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Load configuration
	config := getWorkflowConfig(ctx)

	// Prepare pattern options
	opts := patterns.Options{
		UserID:         input.UserID,
		BudgetAgentMax: getBudgetMax(input.Context),
		ModelTier:      determineModelTier(input.Context, "medium"),
	}

	// Phase 1: Use Tree-of-Thoughts for systematic exploration
	totConfig := patterns.TreeOfThoughtsConfig{
		MaxDepth:          config.ExploratoryMaxIterations,
		BranchingFactor:   config.ExploratoryBranchFactor,
		EvaluationMethod:  "scoring",
		PruningThreshold:  1.0 - config.ExploratoryConfidenceThreshold, // Invert for pruning
		ExplorationBudget: config.ExploratoryMaxIterations * config.ExploratoryBranchFactor,
		BacktrackEnabled:  true,
		ModelTier:         opts.ModelTier,
	}

	logger.Info("Starting Tree-of-Thoughts exploration",
		"max_depth", totConfig.MaxDepth,
		"branching_factor", totConfig.BranchingFactor,
	)

	totResult, err := patterns.TreeOfThoughts(
		ctx,
		input.Query,
		input.Context,
		input.SessionID,
		convertHistoryForAgent(input.History),
		totConfig,
		opts,
	)

	if err != nil {
		logger.Error("Tree-of-Thoughts exploration failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Exploration failed: %v", err),
		}, err
	}

	// Phase 2: If confidence is low, apply Debate pattern on top findings
	finalResult := totResult.BestSolution
	totalTokens := totResult.TotalTokens
	finalConfidence := totResult.Confidence

	if totResult.Confidence < config.ExploratoryConfidenceThreshold {
		logger.Info("Confidence below threshold, applying Debate pattern",
			"current_confidence", totResult.Confidence,
			"threshold", config.ExploratoryConfidenceThreshold,
		)

		// Extract top perspectives from tree exploration
		perspectives := []string{}
		if totResult.ExplorationTree != nil {
			for i := range totResult.ExplorationTree.Children {
				if i >= 3 {
					break // Limit to 3 perspectives
				}
				perspectives = append(perspectives, fmt.Sprintf("perspective_%d", i+1))
			}
		}

		debateConfig := patterns.DebateConfig{
			NumDebaters:      len(perspectives),
			MaxRounds:        2,
			Perspectives:     perspectives,
			RequireConsensus: false,
			ModeratorEnabled: true,
			VotingEnabled:    false,
			ModelTier:        opts.ModelTier,
		}

		// Prepare debate context with exploration findings
		debateContext := make(map[string]interface{})
		for k, v := range input.Context {
			debateContext[k] = v
		}
		debateContext["exploration_findings"] = totResult.BestPath

		debateResult, err := patterns.Debate(
			ctx,
			fmt.Sprintf("Based on exploration findings, what is the best answer to: %s", input.Query),
			debateContext,
			input.SessionID,
			convertHistoryForAgent(input.History),
			debateConfig,
			opts,
		)

		if err == nil {
			finalResult = debateResult.FinalPosition
			totalTokens += debateResult.TotalTokens
			finalConfidence = 0.8 // Debate increases confidence
			logger.Info("Debate enhanced the exploration result")
		} else {
			logger.Warn("Debate pattern failed, using tree-of-thoughts result", "error", err)
		}
	}

	// Phase 3: Apply Reflection pattern for final quality check
	if finalConfidence < 0.9 {
		logger.Info("Applying reflection for final quality improvement")

		reflectionConfig := patterns.ReflectionConfig{
			Enabled:             true,
			MaxRetries:          2,
			ConfidenceThreshold: 0.9,
			Criteria:            []string{"clarity", "completeness", "accuracy"},
			TimeoutMs:           30000,
		}

		// Create mock agent results for reflection
		agentResults := []activities.AgentExecutionResult{
			{
				Response:   finalResult,
				Success:    true,
				TokensUsed: totalTokens,
			},
		}

		reflectedResult, reflectedConfidence, reflectionTokens, err := patterns.ReflectOnResult(
			ctx,
			input.Query,
			finalResult,
			agentResults,
			input.Context,
			reflectionConfig,
			opts,
		)

		if err == nil {
			finalResult = reflectedResult
			finalConfidence = reflectedConfidence
			totalTokens += reflectionTokens
			logger.Info("Reflection improved final result", "new_confidence", finalConfidence)
		} else {
			logger.Warn("Reflection failed, using previous result", "error", err)
		}
	}

	// Update session
	if input.SessionID != "" {
		if err := updateSession(ctx, input.SessionID, finalResult, totalTokens, totResult.TotalThoughts); err != nil {
			logger.Warn("Failed to update session",
				"error", err,
				"session_id", input.SessionID,
			)
		}
	}

	logger.Info("ExploratoryWorkflow completed",
		"total_tokens", totalTokens,
		"final_confidence", finalConfidence,
		"total_thoughts_explored", totResult.TotalThoughts,
		"tree_depth", totResult.TreeDepth,
	)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"workflow_type":      "exploratory",
			"pattern_used":       "tree_of_thoughts",
			"total_thoughts":     totResult.TotalThoughts,
			"tree_depth":         totResult.TreeDepth,
			"final_confidence":   finalConfidence,
			"debate_applied":     totResult.Confidence < config.ExploratoryConfidenceThreshold,
			"reflection_applied": finalConfidence < 0.9,
		},
	}, nil
}
