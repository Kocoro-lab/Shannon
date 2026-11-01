package pricing

import (
	"math"
	"testing"
)

func TestDefaultPerToken(t *testing.T) {
	// Reset to ensure fresh load
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	price := DefaultPerToken()
	if price <= 0 {
		t.Errorf("DefaultPerToken returned non-positive price: %f", price)
	}

	// defaults.combined_per_1k: 0.005 = 0.000005 per token
	expectedMin := 0.000004
	expectedMax := 0.000006
	if price < expectedMin || price > expectedMax {
		t.Errorf("DefaultPerToken returned unexpected price: %f, expected between %f and %f", price, expectedMin, expectedMax)
	}
}

func TestPricePerTokenForModel(t *testing.T) {
	// Reset to ensure fresh load
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	tests := []struct {
		model     string
		wantFound bool
		minPrice  float64
		maxPrice  float64
	}{
		// Price ranges based on config/models.yaml (per token, not per 1k)
		// gpt-5-nano-2025-08-07: 0.0001/0.0004 per 1k = 0.0000001/0.0000004 per token
		{"gpt-5-nano-2025-08-07", true, 0.0000001, 0.0000004},
		// gpt-5.1: 0.00125/0.01 per 1k = 0.00000125/0.00001 per token
		{"gpt-5.1", true, 0.00000125, 0.00001},
		// gpt-5-pro-2025-10-06: 0.02/0.08 per 1k = 0.00002/0.00008 per token
		{"gpt-5-pro-2025-10-06", true, 0.00002, 0.00008},
		// claude-sonnet-4-5-20250929: 0.003/0.015 per 1k = 0.000003/0.000015 per token
		{"claude-sonnet-4-5-20250929", true, 0.000003, 0.000015},
		// claude-haiku-4-5-20251001: 0.001/0.005 per 1k = 0.000001/0.000005 per token
		{"claude-haiku-4-5-20251001", true, 0.000001, 0.000005},
		// deepseek-chat: 0.00027/0.0011 per 1k = 0.00000027/0.0000011 per token
		{"deepseek-chat", true, 0.00000027, 0.0000011},
		// claude-sonnet-4-6: 0.003/0.015 per 1k = 0.000003/0.000015 per token
		{"claude-sonnet-4-6", true, 0.000003, 0.000015},
		// claude-opus-4-6: 0.005/0.025 per 1k = 0.000005/0.000025 per token
		{"claude-opus-4-6", true, 0.000005, 0.000025},
		{"unknown-model", false, 0, 0},
		{"", false, 0, 0},
	}

	for _, tt := range tests {
		price, found := PricePerTokenForModel(tt.model)
		if found != tt.wantFound {
			t.Errorf("PricePerTokenForModel(%q): found = %v, want %v", tt.model, found, tt.wantFound)
		}
		if found && (price < tt.minPrice || price > tt.maxPrice) {
			t.Errorf("PricePerTokenForModel(%q): price = %f, want between %f and %f", tt.model, price, tt.minPrice, tt.maxPrice)
		}
	}
}

func TestCostForTokens(t *testing.T) {
	// Reset to ensure fresh load
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	tests := []struct {
		model   string
		tokens  int
		minCost float64
		maxCost float64
	}{
		{"gpt-5-nano-2025-08-07", 1000, 0.0001, 0.0004},
		{"gpt-5.1", 1000, 0.00125, 0.01},
		{"gpt-5-pro-2025-10-06", 1000, 0.02, 0.08},
		// Unknown models should use default: 0.005 per 1k
		{"unknown-model", 1000, 0.005, 0.005},
		{"", 1000, 0.005, 0.005},
		{"gpt-5-nano-2025-08-07", 0, 0, 0},
	}

	for _, tt := range tests {
		cost := CostForTokens(tt.model, tt.tokens)
		if cost < tt.minCost || cost > tt.maxCost {
			t.Errorf("CostForTokens(%q, %d): cost = %f, want between %f and %f", tt.model, tt.tokens, cost, tt.minCost, tt.maxCost)
		}
	}
}

func TestModifiedTime(t *testing.T) {
	// Just ensure it doesn't panic
	_ = ModifiedTime()
}

func TestCostForSplit_SyntheticScraperModels(t *testing.T) {
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	tests := []struct {
		model        string
		outputTokens int
		wantCost     float64
		tolerance    float64
	}{
		// shannon_web_search: (7500/1000) × 0.002 = $0.015
		{"shannon_web_search", 7500, 0.015, 0.001},
		// shannon_firecrawl: (7500/1000) × 0.000133 ≈ $0.001
		{"shannon_firecrawl", 7500, 0.001, 0.001},
		// shannon_outcrawl: (7500/1000) × 0.00333 ≈ $0.025
		{"shannon_outcrawl", 7500, 0.025, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			// Synthetic tools use 0 input tokens, all output
			cost := CostForSplit(tt.model, 0, tt.outputTokens)
			if math.Abs(cost-tt.wantCost) > tt.tolerance {
				t.Errorf("CostForSplit(%s, 0, %d) = %f, want %f (±%f)",
					tt.model, tt.outputTokens, cost, tt.wantCost, tt.tolerance)
			}
		})
	}
}

func TestCostForSplitWithCache(t *testing.T) {
	mu.Lock()
	initialized = false
	loaded = nil
	mu.Unlock()

	tests := []struct {
		name                  string
		model                 string
		inputTokens           int
		outputTokens          int
		cacheReadTokens       int
		cacheCreationTokens   int
		cacheCreation1hTokens int
		provider              string
		wantMin               float64
		wantMax               float64
	}{
		{
			name:                "anthropic_no_cache",
			model:               "claude-sonnet-4-5-20250929",
			inputTokens:         1000,
			outputTokens:        500,
			cacheReadTokens:     0,
			cacheCreationTokens: 0,
			provider:            "anthropic",
			wantMin:             0.0104,
			wantMax:             0.0106,
		},
		{
			name:                "anthropic_with_cache_read",
			model:               "claude-sonnet-4-5-20250929",
			inputTokens:         1000,
			outputTokens:        500,
			cacheReadTokens:     5000,
			cacheCreationTokens: 0,
			provider:            "anthropic",
			wantMin:             0.0119,
			wantMax:             0.0121,
		},
		{
			name:                "anthropic_with_cache_creation",
			model:               "claude-sonnet-4-5-20250929",
			inputTokens:         1000,
			outputTokens:        500,
			cacheReadTokens:     0,
			cacheCreationTokens: 2000,
			provider:            "anthropic",
			wantMin:             0.0179,
			wantMax:             0.0181,
		},
		{
			name:                "openai_cache_discount",
			model:               "gpt-5-mini-2025-08-07",
			inputTokens:         5000,
			outputTokens:        1000,
			cacheReadTokens:     3000,
			cacheCreationTokens: 0,
			provider:            "openai",
			wantMin:             0.002874,
			wantMax:             0.002876,
		},
		{
			// grok-4-1-fast-non-reasoning: input 0.0002/1K, output 0.0005/1K
			// base = 5000/1000 * 0.0002 + 1000/1000 * 0.0005 = 0.001 + 0.0005 = 0.0015
			// discount = 3000/1000 * 0.0002 * 0.75 = 0.00045
			// expected = 0.0015 - 0.00045 = 0.00105
			name:                "xai_cache_discount_75pct",
			model:               "grok-4-1-fast-non-reasoning",
			inputTokens:         5000,
			outputTokens:        1000,
			cacheReadTokens:     3000,
			cacheCreationTokens: 0,
			provider:            "xai",
			wantMin:             0.001049,
			wantMax:             0.001051,
		},
		{
			// kimi-k2-turbo-preview: input 0.00115/1K, output 0.008/1K
			// base = 5000/1000 * 0.00115 + 1000/1000 * 0.008 = 0.00575 + 0.008 = 0.01375
			// discount = 3000/1000 * 0.00115 * 0.75 = 0.0025875
			// expected = 0.01375 - 0.0025875 = 0.0111625
			name:                "kimi_cache_discount_75pct",
			model:               "kimi-k2-turbo-preview",
			inputTokens:         5000,
			outputTokens:        1000,
			cacheReadTokens:     3000,
			cacheCreationTokens: 0,
			provider:            "kimi",
			wantMin:             0.011161,
			wantMax:             0.011164,
		},
		{
			// MiniMax-M2.7: input 0.00033/1K, output 0.00133/1K, cache separate (Anthropic-style).
			// base = 5000/1000 * 0.00033 + 1000/1000 * 0.00133 = 0.00165 + 0.00133 = 0.00298
			// cache_read at 10%: + 3000/1000 * 0.00033 * 0.1 = 0.000099
			// cache_creation at 125%: + 2000/1000 * 0.00033 * 1.25 = 0.000825
			// expected = 0.00298 + 0.000099 + 0.000825 = 0.003904
			name:                "minimax_cache_separate",
			model:               "MiniMax-M2.7",
			inputTokens:         5000,
			outputTokens:        1000,
			cacheReadTokens:     3000,
			cacheCreationTokens: 2000,
			provider:            "minimax",
			wantMin:             0.003903,
			wantMax:             0.003905,
		},
		{
			// claude-sonnet-4-5-20250929: input 0.003/1K, output 0.015/1K
			// base = 1000/1000 * 0.003 + 500/1000 * 0.015 = 0.003 + 0.0075 = 0.0105
			// cache_creation 3000 total, 2000 at 1h TTL, 1000 at 5m TTL
			// 5m: 1000/1000 * 0.003 * 1.25 = 0.00375
			// 1h: 2000/1000 * 0.003 * 2.0  = 0.012
			// expected = 0.0105 + 0.00375 + 0.012 = 0.02625
			name:                  "anthropic_1h_cache_creation",
			model:                 "claude-sonnet-4-5-20250929",
			inputTokens:           1000,
			outputTokens:          500,
			cacheReadTokens:       0,
			cacheCreationTokens:   3000,
			cacheCreation1hTokens: 2000,
			provider:              "anthropic",
			wantMin:               0.02625 - 0.0001,
			wantMax:               0.02625 + 0.0001,
		},
		{
			// claude-sonnet-4-5-20250929: input 0.003/1K, output 0.015/1K
			// base = 1000/1000 * 0.003 + 500/1000 * 0.015 = 0.003 + 0.0075 = 0.0105
			// cache_creation 2000 all at 5m TTL (no 1h)
			// 5m: 2000/1000 * 0.003 * 1.25 = 0.0075
			// expected = 0.0105 + 0.0075 = 0.018
			name:                  "anthropic_all_5m_no_1h",
			model:                 "claude-sonnet-4-5-20250929",
			inputTokens:           1000,
			outputTokens:          500,
			cacheReadTokens:       0,
			cacheCreationTokens:   2000,
			cacheCreation1hTokens: 0,
			provider:              "anthropic",
			wantMin:               0.018 - 0.0001,
			wantMax:               0.018 + 0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CostForSplitWithCache(
				tt.model, tt.inputTokens, tt.outputTokens,
				tt.cacheReadTokens, tt.cacheCreationTokens, tt.cacheCreation1hTokens, tt.provider,
			)
			if cost < tt.wantMin || cost > tt.wantMax {
				t.Errorf("CostForSplitWithCache(%s): got %f, want [%f, %f]",
					tt.name, cost, tt.wantMin, tt.wantMax)
			}
		})
	}
}
