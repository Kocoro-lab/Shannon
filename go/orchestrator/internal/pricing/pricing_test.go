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
		// gpt-3.5-turbo: 0.0005/0.002 per 1k = 0.0000005/0.000002 per token
		{"gpt-3.5-turbo", true, 0.0000005, 0.000002},
		// gpt-4-turbo: 0.01/0.03 per 1k = 0.00001/0.00003 per token
		{"gpt-4-turbo", true, 0.00001, 0.00003},
		// claude-3-sonnet: 0.003/0.02 per 1k = 0.000003/0.00002 per token
		{"claude-3-sonnet", true, 0.000003, 0.00002},
		// claude-3-haiku: 0.0002/0.002 per 1k = 0.0000002/0.000002 per token
		{"claude-3-haiku", true, 0.0000002, 0.000002},
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
		{"gpt-3.5-turbo", 1000, 0.0005, 0.002},
		{"gpt-4-turbo", 1000, 0.01, 0.03},
		// Unknown models should use default: 0.005 per 1k
		{"unknown-model", 1000, 0.005, 0.005},
		{"", 1000, 0.005, 0.005},
		{"gpt-3.5-turbo", 0, 0, 0},
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
