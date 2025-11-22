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
		// claude-sonnet-4-5-20250929: 0.0003/0.0015 per 1k = 0.0000003/0.0000015 per token
		{"claude-sonnet-4-5-20250929", true, 0.0000003, 0.0000015},
		// claude-haiku-4-5-20251001: 0.0001/0.0005 per 1k = 0.0000001/0.0000005 per token
		{"claude-haiku-4-5-20251001", true, 0.0000001, 0.0000005},
		// deepseek-chat: 0.00027/0.0011 per 1k = 0.00000027/0.0000011 per token
		{"deepseek-chat", true, 0.00000027, 0.0000011},
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

// Helper function to check if floats are approximately equal
func floatEquals(a, b float64) bool {
	const epsilon = 1e-9
	return math.Abs(a-b) < epsilon
}
