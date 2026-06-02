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
		{"XAI Grok 4.1 Fast Non-Reasoning", "grok-4-1-fast-non-reasoning", "xai"},
		{"XAI Grok 4.1 Fast Reasoning", "grok-4-1-fast-reasoning", "xai"},
		{"XAI Grok 4.20 Reasoning", "grok-4.20-0309-reasoning", "xai"},

		// Llama/Meta models - should map to "ollama" (local deployment)
		{"Llama 3.2", "llama-3.2-3b", "ollama"},
		{"Llama 4 Scout", "llama-4-scout", "ollama"},
		{"Llama 3.1", "llama-3.1-405b", "ollama"},
		{"Code Llama", "codellama-34b", "ollama"},

		// Kimi / Moonshot models
		{"Kimi K2.5", "kimi-k2.5", "kimi"},
		{"Kimi K2 Thinking", "kimi-k2-thinking", "kimi"},
		{"Kimi K2 Turbo", "kimi-k2-turbo-preview", "kimi"},
		{"Moonshot V1", "moonshot-v1-128k", "kimi"},

		// MiniMax models
		{"MiniMax M3", "MiniMax-M3", "minimax"},
		{"MiniMax M2.7", "MiniMax-M2.7", "minimax"},
		{"MiniMax M2.7-HS", "MiniMax-M2.7-highspeed", "minimax"},

		// ZhipuAI models (updated)
		{"ZhipuAI GLM Flash", "glm-4.7-flash", "zai"},
		{"ZhipuAI GLM", "glm-4.7", "zai"},
		{"ZhipuAI GLM 5", "glm-5", "zai"},

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

