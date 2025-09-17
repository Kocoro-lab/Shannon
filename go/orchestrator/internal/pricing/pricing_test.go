package pricing

import (
	"math"
	"sync"
	"testing"
)

func TestDefaultPerToken(t *testing.T) {
	// Reset the once to ensure fresh load
	once.Do(func() {})
	once = sync.Once{}

	price := DefaultPerToken()
	if price <= 0 {
		t.Errorf("DefaultPerToken returned non-positive price: %f", price)
	}

	// Should be 0.002/1000 based on config or fallback of 0.000002
	expectedMin := 0.000001
	expectedMax := 0.000003
	if price < expectedMin || price > expectedMax {
		t.Errorf("DefaultPerToken returned unexpected price: %f, expected between %f and %f", price, expectedMin, expectedMax)
	}
}

func TestPricePerTokenForModel(t *testing.T) {
	// Reset the once to ensure fresh load
	once.Do(func() {})
	once = sync.Once{}

	tests := []struct {
		model     string
		wantFound bool
		minPrice  float64
		maxPrice  float64
	}{
		{"gpt-3.5-turbo", true, 0.0000005, 0.000002},
		{"gpt-4-turbo", true, 0.00001, 0.00003},
		{"claude-3-sonnet", true, 0.000003, 0.00002},
		{"claude-3-haiku", true, 0.0000002, 0.000002},
		{"deepseek-chat", true, 0.0000001, 0.0000003},
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
	// Reset the once to ensure fresh load
	once.Do(func() {})
	once = sync.Once{}

	tests := []struct {
		model   string
		tokens  int
		minCost float64
		maxCost float64
	}{
		{"gpt-3.5-turbo", 1000, 0.0005, 0.002},
		{"gpt-4-turbo", 1000, 0.01, 0.03},
		{"unknown-model", 1000, 0.001, 0.003},
		{"", 1000, 0.001, 0.003},
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
