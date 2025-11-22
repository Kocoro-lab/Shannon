package models

import (
	"testing"
)

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		// OpenAI models
		{"OpenAI GPT-5", "gpt-5-nano-2025-08-07", "openai"},
		{"OpenAI GPT-4", "gpt-4.1-2025-04-14", "openai"},
		{"OpenAI Davinci", "text-davinci-003", "openai"},
		{"OpenAI Turbo", "gpt-3.5-turbo", "openai"},

		// Anthropic models
		{"Anthropic Claude", "claude-sonnet-4-5-20250929", "anthropic"},
		{"Anthropic Opus", "claude-opus-4-1-20250805", "anthropic"},
		{"Anthropic Haiku", "claude-haiku-4-5-20251001", "anthropic"},
		{"Anthropic Sonnet", "sonnet-4-20250514", "anthropic"},

		// Google models
		{"Google Gemini", "gemini-2.5-pro", "google"},
		{"Google Gemini Flash", "gemini-2.5-flash", "google"},
		{"Google Palm", "palm-2", "google"},

		// DeepSeek models
		{"DeepSeek Chat", "deepseek-chat", "deepseek"},
		{"DeepSeek R1", "deepseek-r1", "deepseek"},
		{"DeepSeek V3", "deepseek-v3.2", "deepseek"},

		// Qwen models
		{"Qwen 3", "qwen3-8b", "qwen"},
		{"Qwen Instruct", "qwen3-4b-instruct-2507", "qwen"},

		// X.AI models
		{"XAI Grok 3 Mini", "grok-3-mini", "xai"},
		{"XAI Grok Beta", "grok-beta", "xai"},
		{"XAI Grok 4 Fast Non-Reasoning", "grok-4-fast-non-reasoning", "xai"},
		{"XAI Grok 4 Fast Reasoning", "grok-4-fast-reasoning", "xai"},

		// Mistral models (should be detected before llama check)
		{"Mistral Small", "mistral-small-3.2-24b-instruct-2506", "mistral"},
		{"Mistral Nemo", "mistral-nemo-instruct-2407", "mistral"},
		{"Mistral Codestral", "codestral-22b-v0.1", "mistral"},
		{"Mixtral", "mixtral-8x7b", "mistral"},

		// Llama/Meta models - should map to "ollama" (local deployment)
		{"Llama 3.2", "llama-3.2-3b", "ollama"},
		{"Llama 4 Scout", "llama-4-scout", "ollama"},
		{"Llama 3.1", "llama-3.1-405b", "ollama"},
		{"Code Llama", "codellama-34b", "ollama"},

		// Cohere models
		{"Cohere Command", "command-r-plus-08-2024", "cohere"},
		{"Cohere Command R", "command-r-08-2024", "cohere"},

		// ZhipuAI models
		{"ZhipuAI GLM", "glm-4.5-flash", "zai"},
		{"ZhipuAI GLM Air", "glm-4.5-air", "zai"},
		{"ZhipuAI GLM 4.6", "glm-4.6", "zai"},

		// Groq models
		{"Groq Llama", "groq-llama-70b", "groq"},

		// Unknown/empty
		{"Empty model", "", "unknown"},
		{"Unknown model", "some-random-model", "unknown"},

		// Case insensitivity
		{"Uppercase GPT", "GPT-5-NANO-2025", "openai"},
		{"Mixed case Claude", "Claude-Sonnet-4-5", "anthropic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectProvider(tt.model)
			if result != tt.expected {
				t.Errorf("DetectProvider(%q) = %q, want %q", tt.model, result, tt.expected)
			}
		})
	}
}

func TestDetectProvider_LlamaConsistency(t *testing.T) {
	// Critical test: all llama models should return "ollama" (not "meta")
	// This ensures consistency across the codebase
	llamaModels := []string{
		"llama-3.2-3b",
		"llama-4-scout",
		"llama-3.1-405b",
		"codellama-34b",
		"LLAMA-3.3-70B",
	}

	for _, model := range llamaModels {
		t.Run(model, func(t *testing.T) {
			result := DetectProvider(model)
			if result != "ollama" {
				t.Errorf("DetectProvider(%q) = %q, want %q (llama models should map to ollama)", model, result, "ollama")
			}
		})
	}
}

func TestDetectProvider_MistralBeforeLlama(t *testing.T) {
	// Mistral models should be detected before llama check
	// This tests the order of pattern matching
	mistralModels := []string{
		"mistral-7b",
		"mistral-small-3.2-24b",
		"mixtral-8x7b",
		"codestral-22b",
	}

	for _, model := range mistralModels {
		t.Run(model, func(t *testing.T) {
			result := DetectProvider(model)
			if result != "mistral" {
				t.Errorf("DetectProvider(%q) = %q, want %q", model, result, "mistral")
			}
		})
	}
}
