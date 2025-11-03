package models

import (
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
)

// DetectProvider determines the provider from a model name.
// It first checks models.yaml for explicit provider mapping via model_catalog,
// then falls back to pattern matching for common model naming conventions.
//
// This function consolidates provider detection logic to avoid inconsistencies
// across the codebase. All provider detection should use this function.
//
// Note: Llama models are mapped to "ollama" (local deployment convention) even
// though models.yaml may list them under "meta" provider (Together AI).
func DetectProvider(model string) string {
	if model == "" {
		return "unknown"
	}

	// Special case: Check for Groq provider first (before llama pattern matching)
	// Groq-specific models should be identified as "groq" not "ollama"
	ml := strings.ToLower(model)
	if strings.Contains(ml, "groq") && !strings.Contains(ml, "llama") {
		return "groq"
	}
	if strings.Contains(ml, "groq-llama") {
		return "groq"
	}

	// Strategy 1: Check if model exists in models.yaml model_catalog
	// The pricing package loads models.yaml, and we can infer provider from
	// the model_catalog structure (provider.model_name)
	provider := detectProviderFromCatalog(model)
	if provider != "" {
		// Override: map "meta" provider to "ollama" for llama models
		// This maintains the codebase convention of using "ollama" for llama models
		if provider == "meta" && strings.Contains(ml, "llama") {
			return "ollama"
		}
		return provider
	}

	// Strategy 2: Fall back to pattern matching for common model names
	return detectProviderFromPattern(model)
}

// detectProviderFromCatalog checks models.yaml model_catalog for explicit provider mapping.
// The config structure has: model_catalog -> provider -> model_name
// We search through all providers to find which one has this model.
func detectProviderFromCatalog(model string) string {
	// Try to infer provider by checking which tier contains this model
	// If model exists in model_tiers, we can get its provider
	tiers := []string{"small", "medium", "large"}
	for _, tier := range tiers {
		provider := pricing.GetProviderForModel(tier, model)
		if provider != "" {
			return provider
		}
	}
	return ""
}

// detectProviderFromPattern uses pattern matching to detect provider from model name.
// This is a fallback when the model is not found in models.yaml.
func detectProviderFromPattern(model string) string {
	ml := strings.ToLower(model)

	// OpenAI models
	if strings.Contains(ml, "gpt-") || strings.Contains(ml, "davinci") ||
		strings.Contains(ml, "turbo") || strings.Contains(ml, "text-") {
		return "openai"
	}

	// Anthropic models
	if strings.Contains(ml, "claude") || strings.Contains(ml, "opus") ||
		strings.Contains(ml, "sonnet") || strings.Contains(ml, "haiku") {
		return "anthropic"
	}

	// Google models
	if strings.Contains(ml, "gemini") || strings.Contains(ml, "palm") ||
		strings.Contains(ml, "bard") {
		return "google"
	}

	// DeepSeek models
	if strings.Contains(ml, "deepseek") {
		return "deepseek"
	}

	// Qwen models
	if strings.Contains(ml, "qwen") {
		return "qwen"
	}

	// X.AI models
	if strings.Contains(ml, "grok") {
		return "xai"
	}

	// Mistral models (check before llama since some names might overlap)
	if strings.Contains(ml, "mistral") || strings.Contains(ml, "mixtral") ||
		strings.Contains(ml, "codestral") {
		return "mistral"
	}

	// Llama/Meta models - map to "ollama" (local deployment convention)
	// This matches the majority convention in the codebase
	if strings.Contains(ml, "llama") || strings.Contains(ml, "codellama") {
		return "ollama"
	}

	// Cohere models
	if strings.Contains(ml, "command") || strings.Contains(ml, "cohere") {
		return "cohere"
	}

	// ZhipuAI models
	if strings.Contains(ml, "glm") {
		return "zai"
	}

	// Groq models
	if strings.Contains(ml, "groq") {
		return "groq"
	}

	return "unknown"
}
