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

// DebateConfig configures the debate pattern
type DebateConfig struct {
	NumDebaters      int      // Number of agents in debate (2-5)
	MaxRounds        int      // Maximum debate rounds
	Perspectives     []string // Different perspectives to represent
	RequireConsensus bool     // Whether to require consensus
	ModeratorEnabled bool     // Use a moderator agent
	VotingEnabled    bool     // Enable voting mechanism
	ModelTier        string   // Model tier for debaters
}

// DebateResult contains the outcome of a debate
type DebateResult struct {
	FinalPosition    string
	Positions        []DebatePosition
	ConsensusReached bool
	TotalTokens      int
	Rounds           int
	WinningArgument  string
	Votes            map[string]int
}

// DebatePosition represents one agent's position
type DebatePosition struct {
	AgentID    string
	Position   string
	Arguments  []string
	Confidence float64
	TokensUsed int
}

// Debate implements multi-agent debate pattern for exploring different perspectives
func Debate(
	ctx workflow.Context,
	query string,
	context map[string]interface{},
	sessionID string,
	history []string,
	config DebateConfig,
	opts Options,
) (*DebateResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Debate pattern",
		"query", query,
		"debaters", config.NumDebaters,
		"max_rounds", config.MaxRounds,
	)

	// Set defaults
	if config.NumDebaters == 0 {
		config.NumDebaters = 3
	}
	if config.NumDebaters > 5 {
		config.NumDebaters = 5 // Cap at 5 for manageability
	}
	if config.MaxRounds == 0 {
		config.MaxRounds = 3
	}
	if config.ModelTier == "" {
		config.ModelTier = opts.ModelTier
		if config.ModelTier == "" {
			config.ModelTier = "medium"
		}
	}

	// Initialize perspectives if not provided
	if len(config.Perspectives) == 0 {
		config.Perspectives = generateDefaultPerspectives(config.NumDebaters)
	}

	result := &DebateResult{
		Positions: make([]DebatePosition, 0, config.NumDebaters),
		Votes:     make(map[string]int),
	}

	// Track debate history for context
	debateHistory := []string{}

	// Phase 1: Initial positions
	logger.Info("Phase 1: Gathering initial positions")

	var initialPositions []DebatePosition
	futures := make([]workflow.Future, config.NumDebaters)

	for i := 0; i < config.NumDebaters; i++ {
		perspective := ""
		if i < len(config.Perspectives) {
			perspective = config.Perspectives[i]
		}

		agentID := fmt.Sprintf("debater-%d-%s", i+1, perspective)

		// Create debate context
		debateContext := make(map[string]interface{})
		for k, v := range context {
			debateContext[k] = v
		}
		debateContext["perspective"] = perspective
		debateContext["role"] = "debater"
		debateContext["agent_number"] = i + 1

		initialPrompt := fmt.Sprintf(
			"As a %s perspective, provide your position on: %s\n\nBe specific and provide strong arguments.",
			perspective,
			query,
		)

        if opts.BudgetAgentMax > 0 {
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if debateContext != nil {
                if p, ok := debateContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            } else if context != nil {
                if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            futures[i] = workflow.ExecuteActivity(ctx,
                constants.ExecuteAgentWithBudgetActivity,
                activities.BudgetedAgentInput{
					AgentInput: activities.AgentExecutionInput{
						Query:             initialPrompt,
						AgentID:           agentID,
						Context:           debateContext,
						Mode:              "debate",
						SessionID:         sessionID,
						History:           history,
                            ParentWorkflowID: wid,
					},
					MaxTokens: opts.BudgetAgentMax / config.NumDebaters,
					UserID:    opts.UserID,
					TaskID:    wid,
					ModelTier: config.ModelTier,
				})
        } else {
            // Determine parent workflow for streaming correlation
            wid := workflow.GetInfo(ctx).WorkflowExecution.ID
            if debateContext != nil {
                if p, ok := debateContext["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            } else if context != nil {
                if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                    wid = p
                }
            }
            futures[i] = workflow.ExecuteActivity(ctx,
                activities.ExecuteAgent,
                activities.AgentExecutionInput{
                    Query:             initialPrompt,
                    AgentID:           agentID,
                    Context:           debateContext,
                    Mode:              "debate",
                    SessionID:         sessionID,
                    History:           history,
                    ParentWorkflowID:  wid,
                })
        }
	}

	// Collect initial positions
	for i, future := range futures {
		var agentResult activities.AgentExecutionResult
		if err := future.Get(ctx, &agentResult); err != nil {
			logger.Warn("Debater failed to provide initial position",
				"agent", i,
				"error", err,
			)
			continue
		}

		position := DebatePosition{
			AgentID:    fmt.Sprintf("debater-%d", i+1),
			Position:   agentResult.Response,
			Arguments:  extractArguments(agentResult.Response),
			Confidence: 0.5, // Initial confidence
			TokensUsed: agentResult.TokensUsed,
		}
		initialPositions = append(initialPositions, position)
		result.TotalTokens += agentResult.TokensUsed
		debateHistory = append(debateHistory, fmt.Sprintf("%s: %s", position.AgentID, position.Position))
	}

	result.Positions = initialPositions

	// Phase 2: Debate rounds
	for round := 1; round <= config.MaxRounds; round++ {
		logger.Info("Debate round", "round", round)

		roundPositions := make([]DebatePosition, 0, len(initialPositions))
		roundFutures := make([]workflow.Future, len(initialPositions))

		// Each debater responds to others
		for i, debater := range initialPositions {
			// Build context with other positions
			othersPositions := []string{}
			for j, other := range initialPositions {
				if i != j {
					othersPositions = append(othersPositions,
						fmt.Sprintf("%s argues: %s", other.AgentID, other.Position))
				}
			}

			responsePrompt := fmt.Sprintf(
				"Round %d: Consider these other perspectives:\n%s\n\n"+
					"As %s, respond with:\n"+
					"1. Counter-arguments to opposing views\n"+
					"2. Strengthen your position\n"+
					"3. Find any common ground\n",
				round,
				strings.Join(othersPositions, "\n"),
				debater.AgentID,
			)

			debateContext := map[string]interface{}{
				"round":           round,
				"debate_history":  debateHistory,
				"other_positions": othersPositions,
			}

            if opts.BudgetAgentMax > 0 {
                wid := workflow.GetInfo(ctx).WorkflowExecution.ID
                if debateContext != nil {
                    if p, ok := debateContext["parent_workflow_id"].(string); ok && p != "" {
                        wid = p
                    }
                } else if context != nil {
                    if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                        wid = p
                    }
                }
                roundFutures[i] = workflow.ExecuteActivity(ctx,
                    constants.ExecuteAgentWithBudgetActivity,
                    activities.BudgetedAgentInput{
						AgentInput: activities.AgentExecutionInput{
							Query:             responsePrompt,
							AgentID:           debater.AgentID,
							Context:           debateContext,
							Mode:              "debate",
							SessionID:         sessionID,
							History:           append(history, debateHistory...),
                                ParentWorkflowID: wid,
						},
						MaxTokens: opts.BudgetAgentMax / (config.NumDebaters * config.MaxRounds),
						UserID:    opts.UserID,
						TaskID:    wid,
						ModelTier: config.ModelTier,
					})
            } else {
                // Determine parent workflow for streaming correlation
                wid := workflow.GetInfo(ctx).WorkflowExecution.ID
                if debateContext != nil {
                    if p, ok := debateContext["parent_workflow_id"].(string); ok && p != "" {
                        wid = p
                    }
                } else if context != nil {
                    if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                        wid = p
                    }
                }
                roundFutures[i] = workflow.ExecuteActivity(ctx,
                    activities.ExecuteAgent,
                    activities.AgentExecutionInput{
                        Query:             responsePrompt,
                        AgentID:           debater.AgentID,
                        Context:           debateContext,
                        Mode:              "debate",
                        SessionID:         sessionID,
                        History:           append(history, debateHistory...),
                        ParentWorkflowID:  wid,
                    })
            }
		}

		// Collect round responses
		for i, future := range roundFutures {
			var agentResult activities.AgentExecutionResult
			if err := future.Get(ctx, &agentResult); err != nil {
				logger.Warn("Debater failed in round",
					"round", round,
					"agent", i,
					"error", err,
				)
				continue
			}

			position := DebatePosition{
				AgentID:    initialPositions[i].AgentID,
				Position:   agentResult.Response,
				Arguments:  extractArguments(agentResult.Response),
				Confidence: calculateArgumentStrength(agentResult.Response),
				TokensUsed: agentResult.TokensUsed,
			}
			roundPositions = append(roundPositions, position)
			result.TotalTokens += agentResult.TokensUsed
			debateHistory = append(debateHistory,
				fmt.Sprintf("Round %d - %s: %s", round, position.AgentID, position.Position))
		}

		// Update positions with latest round
		initialPositions = roundPositions
		result.Positions = roundPositions
		result.Rounds = round

		// Check for consensus if required
		if config.RequireConsensus && checkConsensus(roundPositions) {
			result.ConsensusReached = true
			logger.Info("Consensus reached", "round", round)
			break
		}
	}

	// Phase 3: Resolution (Moderator or Voting)
	if config.ModeratorEnabled {
		result.FinalPosition = moderateDebate(ctx, result.Positions, query, debateHistory, opts)
	} else if config.VotingEnabled {
		result.FinalPosition, result.Votes = conductVoting(result.Positions)
	} else {
		// Synthesize positions
		result.FinalPosition = synthesizePositions(result.Positions, query)
	}

	// Determine winning argument
	bestConfidence := 0.0
	for _, pos := range result.Positions {
		if pos.Confidence > bestConfidence {
			bestConfidence = pos.Confidence
			result.WinningArgument = pos.Position
		}
	}

	logger.Info("Debate completed",
		"rounds", result.Rounds,
		"consensus", result.ConsensusReached,
		"total_tokens", result.TotalTokens,
	)

	// Persist debate consensus for learning (fire-and-forget)
	consensusVersion := workflow.GetVersion(ctx, "persist_debate_consensus_v1", workflow.DefaultVersion, 1)
	if consensusVersion >= 1 {
		persistCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})

		// Extract all position texts for storage
		positionTexts := make([]string, len(result.Positions))
		for i, pos := range result.Positions {
			positionTexts[i] = pos.Position
		}

		// Calculate consensus confidence
		consensusConfidence := bestConfidence
		if result.ConsensusReached {
			consensusConfidence = 0.9 // High confidence when consensus reached
		}

		workflow.ExecuteActivity(
			persistCtx,
			activities.PersistDebateConsensus,
			activities.PersistDebateConsensusInput{
				SessionID:        sessionID,
				Topic:            query,
				WinningPosition:  result.FinalPosition,
				ConsensusReached: result.ConsensusReached,
				Confidence:       consensusConfidence,
				Positions:        positionTexts,
				Metadata: map[string]interface{}{
					"rounds":       result.Rounds,
					"total_tokens": result.TotalTokens,
					"num_debaters": config.NumDebaters,
					"voting":       config.VotingEnabled,
				},
			},
		)
	}

	return result, nil
}

// generateDefaultPerspectives creates default debate perspectives
func generateDefaultPerspectives(num int) []string {
	perspectives := []string{
		"optimistic",
		"skeptical",
		"practical",
		"innovative",
		"conservative",
	}

	if num <= len(perspectives) {
		return perspectives[:num]
	}
	return perspectives
}

// extractArguments parses key arguments from a position
func extractArguments(position string) []string {
	arguments := []string{}

	// Look for numbered points
	lines := strings.Split(position, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "1.") ||
			strings.HasPrefix(line, "2.") ||
			strings.HasPrefix(line, "3.") ||
			strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "•") {
			arguments = append(arguments, line)
		}
	}

	// If no structured arguments, extract sentences
	if len(arguments) == 0 {
		sentences := strings.Split(position, ". ")
		for i, sent := range sentences {
			if i >= 3 { // Limit to key points
				break
			}
			if len(sent) > 20 {
				arguments = append(arguments, sent)
			}
		}
	}

	return arguments
}

// calculateArgumentStrength estimates the strength of arguments
func calculateArgumentStrength(response string) float64 {
	strength := 0.5

	// Evidence indicators
	if strings.Contains(strings.ToLower(response), "evidence") ||
		strings.Contains(strings.ToLower(response), "study") ||
		strings.Contains(strings.ToLower(response), "data") {
		strength += 0.15
	}

	// Logical structure
	if strings.Contains(response, "therefore") ||
		strings.Contains(response, "because") ||
		strings.Contains(response, "consequently") {
		strength += 0.1
	}

	// Counter-arguments addressed
	if strings.Contains(strings.ToLower(response), "however") ||
		strings.Contains(strings.ToLower(response), "although") ||
		strings.Contains(strings.ToLower(response), "counter") {
		strength += 0.15
	}

	// Specific examples
	if strings.Contains(strings.ToLower(response), "for example") ||
		strings.Contains(strings.ToLower(response), "such as") ||
		strings.Contains(strings.ToLower(response), "instance") {
		strength += 0.1
	}

	if strength > 1.0 {
		strength = 1.0
	}

	return strength
}

// checkConsensus determines if positions have converged
func checkConsensus(positions []DebatePosition) bool {
	if len(positions) < 2 {
		return true
	}

	// Simple consensus check: look for agreement keywords
	agreementCount := 0
	for _, pos := range positions {
		lower := strings.ToLower(pos.Position)
		if strings.Contains(lower, "agree") ||
			strings.Contains(lower, "consensus") ||
			strings.Contains(lower, "common ground") ||
			strings.Contains(lower, "alignment") {
			agreementCount++
		}
	}

	// If majority show agreement, consider consensus reached
	return agreementCount > len(positions)/2
}

// moderateDebate uses a moderator to synthesize the debate
func moderateDebate(ctx workflow.Context, positions []DebatePosition, query string, history []string, opts Options) string {
	// Simplified moderation - in practice would use another agent
	return synthesizePositions(positions, query)
}

// conductVoting tallies votes from positions
func conductVoting(positions []DebatePosition) (string, map[string]int) {
	votes := make(map[string]int)

	// Simple voting based on confidence
	winner := positions[0]
	for _, pos := range positions {
		votes[pos.AgentID] = int(pos.Confidence * 100)
		if pos.Confidence > winner.Confidence {
			winner = pos
		}
	}

	return winner.Position, votes
}

// synthesizePositions combines multiple debate positions
func synthesizePositions(positions []DebatePosition, query string) string {
	if len(positions) == 0 {
		return "No positions available"
	}

	// Find strongest arguments across all positions
	allArguments := []string{}
	for _, pos := range positions {
		allArguments = append(allArguments, pos.Arguments...)
	}

	// Build synthesis
	synthesis := fmt.Sprintf("After debate on '%s':\n\n", query)

	// Add strongest position
	strongest := positions[0]
	for _, pos := range positions {
		if pos.Confidence > strongest.Confidence {
			strongest = pos
		}
	}
	synthesis += fmt.Sprintf("Strongest Position: %s\n\n", strongest.Position)

	// Add key arguments
	if len(allArguments) > 0 {
		synthesis += "Key Arguments:\n"
		for i, arg := range allArguments {
			if i >= 5 { // Limit to top arguments
				break
			}
			synthesis += fmt.Sprintf("- %s\n", arg)
		}
	}

	return synthesis
}
