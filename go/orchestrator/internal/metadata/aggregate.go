package metadata

import (
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
)

// AggregateAgentMetadata extracts model, provider, and token information from agent results.
// Returns metadata map with model_used, provider, input_tokens, output_tokens, total_tokens, and cost estimate.
func AggregateAgentMetadata(agentResults []activities.AgentExecutionResult, synthesisTokens int) map[string]interface{} {
	meta := make(map[string]interface{})

	if len(agentResults) == 0 {
		return meta
	}

	// Find the primary model (from first successful agent or most used model)
	var primaryModel string
	totalInputTokens := 0
	totalOutputTokens := 0
	totalTokensUsed := 0
	modelCounts := make(map[string]int)

	// Per-agent usage details for visibility
	agentUsages := make([]map[string]interface{}, 0, len(agentResults))

	for _, result := range agentResults {
		if result.Success && result.ModelUsed != "" {
			modelCounts[result.ModelUsed]++
			if primaryModel == "" {
				primaryModel = result.ModelUsed
			}
		}
		totalInputTokens += result.InputTokens
		totalOutputTokens += result.OutputTokens
		totalTokensUsed += result.TokensUsed

		// Record per-agent usage
		if result.Success {
			agentUsage := map[string]interface{}{
				"agent_id": result.AgentID,
			}
			if result.ModelUsed != "" {
				agentUsage["model"] = result.ModelUsed
			}
			if result.InputTokens > 0 || result.OutputTokens > 0 {
				agentUsage["input_tokens"] = result.InputTokens
				agentUsage["output_tokens"] = result.OutputTokens
				agentUsage["total_tokens"] = result.InputTokens + result.OutputTokens
			} else if result.TokensUsed > 0 {
				agentUsage["total_tokens"] = result.TokensUsed
			}
			agentUsages = append(agentUsages, agentUsage)
		}
	}

	// Use the most frequently used model if available
	maxCount := 0
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			primaryModel = model
		}
	}

	// Populate metadata
	if primaryModel != "" {
		meta["model"] = primaryModel
		meta["model_used"] = primaryModel
		meta["provider"] = models.DetectProvider(primaryModel)
	}

	// Add token breakdown
	// Prefer split tokens when available, fallback to TokensUsed sum
	if totalInputTokens > 0 || totalOutputTokens > 0 {
		meta["input_tokens"] = totalInputTokens
		meta["output_tokens"] = totalOutputTokens
		totalTokens := totalInputTokens + totalOutputTokens + synthesisTokens
		meta["total_tokens"] = totalTokens
	} else if totalTokensUsed > 0 {
		// Fallback: use TokensUsed when splits unavailable
		// Estimate 60/40 split for input/output
		totalTokens := totalTokensUsed + synthesisTokens
		meta["input_tokens"] = int(float64(totalTokensUsed) * 0.6)
		meta["output_tokens"] = int(float64(totalTokensUsed) * 0.4)
		meta["total_tokens"] = totalTokens
	}

	// Add cost estimate if we have tokens
	if totalTokens, ok := meta["total_tokens"].(int); ok && totalTokens > 0 && primaryModel != "" {
		// Rough cost estimate (will be calculated properly by service layer)
		// Using a conservative average rate
		meta["cost_usd"] = float64(totalTokens) * 0.0000005
	}

	// Include per-agent usage details if we have multiple agents
	if len(agentUsages) > 1 {
		meta["agent_usages"] = agentUsages
	}

	return meta
}
