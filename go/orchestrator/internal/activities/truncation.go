package activities

// Centralized truncation limits for streaming messages.
// Keep these values consistent across agent and synthesis paths.
const (
	// Max length for synthesized final content in SSE.
	MaxSynthesisOutputChars = 10000

	// Max length for agent LLM final outputs in SSE.
	MaxLLMOutputChars = 10000

	// Max length for prompts displayed/logged in SSE (sanitized).
	MaxPromptChars = 5000

	// Max length for "thinking" status snippets.
	MaxThinkingChars = 200
)
