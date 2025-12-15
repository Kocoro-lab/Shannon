package metadata

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/stretchr/testify/assert"
)

func TestAggregateAgentMetadata_EmptyResults(t *testing.T) {
	results := []activities.AgentExecutionResult{}

	meta := AggregateAgentMetadata(results, 0)

	assert.Empty(t, meta, "empty results should return empty metadata")
}

func TestAggregateAgentMetadata_SingleAgent_WithSplitTokens(t *testing.T) {
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-1",
			ModelUsed:    "gpt-5-nano-2025-08-07",
			Provider:     "openai",
			InputTokens:  100,
			OutputTokens: 200,
			TokensUsed:   0, // Split tokens take precedence
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	assert.Equal(t, "gpt-5-nano-2025-08-07", meta["model_used"], "should return the agent's model")
	assert.Equal(t, "openai", meta["provider"], "should return the agent's provider")
	assert.Equal(t, 100, meta["input_tokens"], "should have input tokens")
	assert.Equal(t, 200, meta["output_tokens"], "should have output tokens")
	assert.Equal(t, 300, meta["total_tokens"], "should sum input+output tokens")

	// Single agent now includes agent_usages for consistency (deprecated in favor of model_breakdown)
	agentUsages, hasAgentUsages := meta["agent_usages"].([]map[string]interface{})
	assert.True(t, hasAgentUsages, "should have agent_usages array")
	assert.Equal(t, 1, len(agentUsages), "should have single agent usage entry")
}

func TestAggregateAgentMetadata_SingleAgent_WithTotalTokens(t *testing.T) {
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-2",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "openai",
			InputTokens:  0,
			OutputTokens: 0,
			TokensUsed:   500, // Use total tokens when split not available
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	assert.Equal(t, "gpt-5-mini-2025-08-07", meta["model_used"])
	assert.Equal(t, "openai", meta["provider"])
	// Should estimate 60/40 split
	assert.Equal(t, 300, meta["input_tokens"], "should estimate 60% input")
	assert.Equal(t, 200, meta["output_tokens"], "should estimate 40% output")
	assert.Equal(t, 500, meta["total_tokens"])
}

func TestAggregateAgentMetadata_EmptyModel_UsesDetection(t *testing.T) {
	// Test that empty model uses provider detection
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-3",
			ModelUsed:    "", // Empty model
			Provider:     "", // Empty provider
			InputTokens:  100,
			OutputTokens: 200,
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	// Empty model means no model/provider in metadata
	_, hasModel := meta["model_used"]
	assert.False(t, hasModel, "empty model should not set model_used")

	// Should still aggregate tokens
	assert.Equal(t, 300, meta["total_tokens"])
}

func TestAggregateAgentMetadata_MultipleAgents_ModelFrequency(t *testing.T) {
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-1",
			ModelUsed:    "gpt-5-nano-2025-08-07",
			Provider:     "openai",
			InputTokens:  100,
			OutputTokens: 100,
		},
		{
			Success:      true,
			AgentID:      "agent-2",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "openai",
			InputTokens:  200,
			OutputTokens: 200,
		},
		{
			Success:      true,
			AgentID:      "agent-3",
			ModelUsed:    "gpt-5-mini-2025-08-07", // Most frequently used
			Provider:     "openai",
			InputTokens:  300,
			OutputTokens: 300,
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	assert.Equal(t, "gpt-5-mini-2025-08-07", meta["model_used"], "should return most frequently used model")
	assert.Equal(t, "openai", meta["provider"])
	assert.Equal(t, 1200, meta["total_tokens"], "should sum all tokens")

	// Multiple agents should have agent_usages array
	agentUsages, ok := meta["agent_usages"].([]map[string]interface{})
	assert.True(t, ok, "should have agent_usages for multiple agents")
	assert.Equal(t, 3, len(agentUsages), "should have three agent usage entries")
}

func TestAggregateAgentMetadata_ZeroTokens(t *testing.T) {
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-zero",
			ModelUsed:    "gpt-5-nano-2025-08-07",
			Provider:     "openai",
			InputTokens:  0,
			OutputTokens: 0,
			TokensUsed:   0,
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	assert.Equal(t, "gpt-5-nano-2025-08-07", meta["model_used"])
	// Zero tokens shouldn't set token fields
	_, hasTokens := meta["total_tokens"]
	assert.False(t, hasTokens, "zero tokens should not set total_tokens")
}

func TestAggregateAgentMetadata_MixedData(t *testing.T) {
	// Test realistic scenario with mixed data quality
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-complete",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "openai",
			InputTokens:  1000,
			OutputTokens: 2000,
		},
		{
			Success:      true,
			AgentID:      "agent-total-only",
			ModelUsed:    "gpt-5-nano-2025-08-07",
			Provider:     "openai",
			TokensUsed:   500,
		},
		{
			Success:      true,
			AgentID:      "agent-no-model",
			ModelUsed:    "",
			Provider:     "",
			InputTokens:  100,
			OutputTokens: 200,
		},
		{
			Success:     true,
			AgentID:     "agent-no-tokens",
			ModelUsed:   "gpt-5-mini-2025-08-07",
			Provider:    "openai",
			TokensUsed:  0,
		},
	}

	meta := AggregateAgentMetadata(results, 100) // Add synthesis tokens

	assert.Equal(t, "gpt-5-mini-2025-08-07", meta["model_used"], "should pick most frequent model")
	assert.Equal(t, "openai", meta["provider"])
	// Total: 1000+2000 (agent1) + 100+200 (agent3) + 100 (synthesis) = 3400
	assert.Equal(t, 3400, meta["total_tokens"], "should sum all tokens including synthesis")

	// Should have agent_usages for multiple successful agents
	agentUsages, ok := meta["agent_usages"].([]map[string]interface{})
	assert.True(t, ok, "should have agent_usages array")
	assert.Equal(t, 4, len(agentUsages), "should have all four successful agents")

	// Verify each successful agent has correct structure
	for _, usage := range agentUsages {
		assert.NotEmpty(t, usage["agent_id"], "all entries should have agent_id")
		assert.Contains(t, usage, "cost_usd", "all entries should have cost_usd")
	}
}

func TestAggregateAgentMetadata_ProviderDetection(t *testing.T) {
	// Test provider detection when agents don't provide it
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-1",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "", // No provider, should detect from model
			InputTokens:  100,
			OutputTokens: 200,
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	assert.Equal(t, "gpt-5-mini-2025-08-07", meta["model_used"])
	// Should detect provider from model name
	provider, ok := meta["provider"].(string)
	assert.True(t, ok, "should have provider")
	assert.NotEmpty(t, provider, "should detect provider from model name")
}

func TestAggregateAgentMetadata_FailedAgents(t *testing.T) {
	// Test that failed agents are excluded
	results := []activities.AgentExecutionResult{
		{
			Success:      true,
			AgentID:      "agent-success",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "openai",
			InputTokens:  100,
			OutputTokens: 200,
		},
		{
			Success:      false, // Failed agent
			AgentID:      "agent-failed",
			ModelUsed:    "gpt-5-mini-2025-08-07",
			Provider:     "openai",
			InputTokens:  500,
			OutputTokens: 500,
		},
	}

	meta := AggregateAgentMetadata(results, 0)

	// Failed agent tokens should still be counted in totals
	assert.Equal(t, 1300, meta["total_tokens"], "should include failed agent tokens in total")

	// agent_usages includes only successful agents (now includes even single agent for consistency)
	agentUsages, hasAgentUsages := meta["agent_usages"].([]map[string]interface{})
	assert.True(t, hasAgentUsages, "should have agent_usages array")
	assert.Equal(t, 1, len(agentUsages), "should have single successful agent entry")
	assert.Equal(t, "agent-success", agentUsages[0]["agent_id"], "should be the successful agent")
}
