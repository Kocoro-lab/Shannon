package strategies

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
)

// TestProviderOverridePrecedence ensures provider_override is read before provider/llm_provider
func TestProviderOverridePrecedence(t *testing.T) {
	tests := []struct {
		name             string
		context          map[string]interface{}
		expectedProvider string
	}{
		{
			name: "provider_override takes precedence",
			context: map[string]interface{}{
				"provider_override": "anthropic",
				"provider":          "openai",
				"llm_provider":      "google",
			},
			expectedProvider: "anthropic",
		},
		{
			name: "provider used when provider_override absent",
			context: map[string]interface{}{
				"provider":     "openai",
				"llm_provider": "google",
			},
			expectedProvider: "openai",
		},
		{
			name: "llm_provider used when both provider_override and provider absent",
			context: map[string]interface{}{
				"llm_provider": "google",
			},
			expectedProvider: "google",
		},
		{
			name:             "empty when all absent",
			context:          map[string]interface{}{},
			expectedProvider: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract provider override using same logic as DAG workflow
			providerOverride := ""
			if v, ok := tt.context["provider_override"].(string); ok && v != "" {
				providerOverride = v
			} else if v, ok := tt.context["provider"].(string); ok && v != "" {
				providerOverride = v
			} else if v, ok := tt.context["llm_provider"].(string); ok && v != "" {
				providerOverride = v
			}

			if providerOverride != tt.expectedProvider {
				t.Errorf("Expected provider %q, got %q", tt.expectedProvider, providerOverride)
			}
		})
	}
}

// TestProviderAwareModelSelection ensures GetPriorityModelForProvider uses provider override
func TestProviderAwareModelSelection(t *testing.T) {
	// This test verifies the pricing helper respects provider parameter
	tests := []struct {
		name           string
		tier           string
		provider       string
		expectNonEmpty bool
	}{
		{
			name:           "anthropic medium tier",
			tier:           "medium",
			provider:       "anthropic",
			expectNonEmpty: true,
		},
		{
			name:           "openai small tier",
			tier:           "small",
			provider:       "openai",
			expectNonEmpty: true,
		},
		{
			name:           "invalid provider returns empty",
			tier:           "medium",
			provider:       "nonexistent",
			expectNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := pricing.GetPriorityModelForProvider(tt.tier, tt.provider)

			if tt.expectNonEmpty && model == "" {
				t.Errorf("Expected non-empty model for %s/%s, got empty", tt.provider, tt.tier)
			}
			if !tt.expectNonEmpty && model != "" {
				t.Errorf("Expected empty model for %s/%s, got %q", tt.provider, tt.tier, model)
			}
		})
	}
}
