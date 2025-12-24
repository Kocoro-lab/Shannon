package openai

import (
	"testing"
)

func TestNewDefaultRegistry(t *testing.T) {
	registry := newDefaultRegistry()

	if registry == nil {
		t.Fatal("newDefaultRegistry() returned nil")
	}

	// Check default model
	if registry.GetDefaultModel() != "shannon-chat" {
		t.Errorf("GetDefaultModel() = %q, want %q", registry.GetDefaultModel(), "shannon-chat")
	}
}

func TestRegistryGetModel(t *testing.T) {
	registry := newDefaultRegistry()

	tests := []struct {
		name        string
		modelName   string
		expectError bool
	}{
		{
			name:        "valid model",
			modelName:   "shannon-chat",
			expectError: false,
		},
		{
			name:        "research model",
			modelName:   "shannon-deep-research",
			expectError: false,
		},
		{
			name:        "invalid model",
			modelName:   "nonexistent-model",
			expectError: true,
		},
		{
			name:        "empty model uses default",
			modelName:   "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := registry.GetModel(tt.modelName)
			if tt.expectError {
				if err == nil {
					t.Error("GetModel() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("GetModel() unexpected error: %v", err)
				}
				if model == nil {
					t.Error("GetModel() returned nil model")
				}
			}
		})
	}
}

func TestRegistryIsValidModel(t *testing.T) {
	registry := newDefaultRegistry()

	tests := []struct {
		modelName string
		expected  bool
	}{
		{"shannon-chat", true},
		{"shannon-deep-research", true},
		{"shannon-standard-research", true},
		{"shannon-quick-research", true},
		{"shannon-ads-research", true},
		{"shannon-complex", true},
		{"invalid-model", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			result := registry.IsValidModel(tt.modelName)
			if result != tt.expected {
				t.Errorf("IsValidModel(%q) = %v, want %v", tt.modelName, result, tt.expected)
			}
		})
	}
}

func TestRegistryListModels(t *testing.T) {
	registry := newDefaultRegistry()

	models := registry.ListModels()

	if len(models) == 0 {
		t.Error("ListModels() returned empty list")
	}

	// Check that all models have required fields
	for _, model := range models {
		if model.ID == "" {
			t.Error("Model ID is empty")
		}
		if model.Object != "model" {
			t.Errorf("Model Object = %q, want %q", model.Object, "model")
		}
		if model.OwnedBy != "shannon" {
			t.Errorf("Model OwnedBy = %q, want %q", model.OwnedBy, "shannon")
		}
	}
}

func TestRegistryGetMaxTokensLimit(t *testing.T) {
	registry := newDefaultRegistry()

	limit := registry.GetMaxTokensLimit()
	if limit <= 0 {
		t.Errorf("GetMaxTokensLimit() = %d, want positive value", limit)
	}
	if limit != 16384 {
		t.Errorf("GetMaxTokensLimit() = %d, want %d", limit, 16384)
	}
}

func TestRegistryGetDefaultTemperature(t *testing.T) {
	registry := newDefaultRegistry()

	temp := registry.GetDefaultTemperature()
	if temp <= 0 || temp > 2 {
		t.Errorf("GetDefaultTemperature() = %f, want value between 0 and 2", temp)
	}
	if temp != 0.7 {
		t.Errorf("GetDefaultTemperature() = %f, want %f", temp, 0.7)
	}
}

func TestRegistryGetRateLimit(t *testing.T) {
	registry := newDefaultRegistry()

	tests := []struct {
		modelName            string
		minRequestsPerMinute int
		minTokensPerMinute   int
	}{
		{"shannon-chat", 1, 1},
		{"shannon-deep-research", 1, 1},
		{"", 1, 1},              // Default model
		{"invalid-model", 1, 1}, // Falls back to default
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			limit := registry.GetRateLimit(tt.modelName)
			if limit == nil {
				t.Fatal("GetRateLimit() returned nil")
			}
			if limit.RequestsPerMinute < tt.minRequestsPerMinute {
				t.Errorf("RequestsPerMinute = %d, want >= %d", limit.RequestsPerMinute, tt.minRequestsPerMinute)
			}
			if limit.TokensPerMinute < tt.minTokensPerMinute {
				t.Errorf("TokensPerMinute = %d, want >= %d", limit.TokensPerMinute, tt.minTokensPerMinute)
			}
		})
	}
}

func TestModelConfigWorkflowModes(t *testing.T) {
	registry := newDefaultRegistry()

	tests := []struct {
		modelName    string
		expectedMode string
	}{
		{"shannon-chat", "simple"},
		{"shannon-deep-research", "research"},
		{"shannon-complex", "supervisor"},
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			model, err := registry.GetModel(tt.modelName)
			if err != nil {
				t.Fatalf("GetModel() error: %v", err)
			}
			if model.WorkflowMode != tt.expectedMode {
				t.Errorf("WorkflowMode = %q, want %q", model.WorkflowMode, tt.expectedMode)
			}
		})
	}
}
