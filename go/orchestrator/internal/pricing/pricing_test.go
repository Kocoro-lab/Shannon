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
		// shannon_google_ads: (7500/1000) × 0.002 = $0.015
		{"shannon_google_ads", 7500, 0.015, 0.001},
		// shannon_yahoo: (7500/1000) × 0.000533 ≈ $0.004
		{"shannon_yahoo", 7500, 0.004, 0.001},
		// shannon_meta: (7500/1000) × 0.000533 ≈ $0.004
		{"shannon_meta", 7500, 0.004, 0.001},
		// shannon_page_screenshot: (500/1000) × 0.002 = $0.001
		{"shannon_page_screenshot", 500, 0.001, 0.001},
		// shannon_firecrawl: (7500/1000) × 0.000133 ≈ $0.001
		{"shannon_firecrawl", 7500, 0.001, 0.001},
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
		name                string
		model               string
		inputTokens         int
		outputTokens        int
		cacheReadTokens     int
		cacheCreationTokens int
		provider            string
		wantMin             float64
		wantMax             float64
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CostForSplitWithCache(
				tt.model, tt.inputTokens, tt.outputTokens,
				tt.cacheReadTokens, tt.cacheCreationTokens, tt.provider,
			)
			if cost < tt.wantMin || cost > tt.wantMax {
				t.Errorf("CostForSplitWithCache(%s): got %f, want [%f, %f]",
					tt.name, cost, tt.wantMin, tt.wantMax)
			}
		})
	}
}
