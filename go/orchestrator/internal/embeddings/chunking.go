package embeddings

import (
	"strings"

	"github.com/google/uuid"
)

// ChunkingConfig controls text chunking behavior
type ChunkingConfig struct {
	Enabled       bool   `yaml:"Enabled"`
	MaxTokens     int    `yaml:"MaxTokens"`
	OverlapTokens int    `yaml:"OverlapTokens"`
	TokenizerMode string `yaml:"TokenizerMode"` // "simple" | "tiktoken"
}

// DefaultChunkingConfig returns sensible defaults
func DefaultChunkingConfig() ChunkingConfig {
	return ChunkingConfig{
		Enabled:       true,
		MaxTokens:     1800,     // Safe for most models
		OverlapTokens: 200,      // ~11% overlap
		TokenizerMode: "simple", // Start with simple word-based
	}
}

// Chunk represents a text chunk with metadata
type Chunk struct {
	QAID       string // UUID for the Q&A pair
	Text       string // The chunk text
	Index      int    // 0-based chunk position
	TotalCount int    // Total number of chunks
}

// Chunker handles text chunking with overlap
type Chunker struct {
	maxTokens     int
	overlapTokens int
	tokenizerMode string
}

// NewChunker creates a new chunker with the given configuration
func NewChunker(config ChunkingConfig) *Chunker {
	if config.MaxTokens <= 0 {
		config.MaxTokens = 1800
	}
	if config.OverlapTokens <= 0 {
		config.OverlapTokens = 200
	}
	if config.TokenizerMode == "" {
		config.TokenizerMode = "simple"
	}

	return &Chunker{
		maxTokens:     config.MaxTokens,
		overlapTokens: config.OverlapTokens,
		tokenizerMode: config.TokenizerMode,
	}
}

// ChunkText splits text into overlapping chunks if needed
// Returns nil if text fits within maxTokens (no chunking needed)
func (c *Chunker) ChunkText(text string) []Chunk {
	tokens := c.tokenize(text)

	// No chunking needed if text fits
	if len(tokens) <= c.maxTokens {
		return nil
	}

	// Generate a unique ID for this Q&A pair
	qaID := uuid.New().String()
	chunks := []Chunk{}

	// Calculate step size (how many tokens to advance for each chunk)
	step := c.maxTokens - c.overlapTokens
	if step <= 0 {
		step = c.maxTokens / 2 // Fallback to 50% overlap
	}

	for i := 0; i < len(tokens); i += step {
		end := i + c.maxTokens
		if end > len(tokens) {
			end = len(tokens)
		}

		chunkTokens := tokens[i:end]
		chunks = append(chunks, Chunk{
			QAID:  qaID,
			Text:  c.detokenize(chunkTokens),
			Index: len(chunks),
		})

		// Stop if we've reached the end
		if end == len(tokens) {
			break
		}
	}

	// Set total count for all chunks
	for i := range chunks {
		chunks[i].TotalCount = len(chunks)
	}

	return chunks
}

// CountTokens estimates the token count for a given text
func (c *Chunker) CountTokens(text string) int {
	return len(c.tokenize(text))
}

// tokenize splits text into tokens based on the tokenizer mode
func (c *Chunker) tokenize(text string) []string {
	switch c.tokenizerMode {
	case "tiktoken":
		// TODO: Implement proper tiktoken tokenization
		// For now, fall back to simple
		return c.simpleTokenize(text)
	default:
		return c.simpleTokenize(text)
	}
}

// detokenize joins tokens back into text
func (c *Chunker) detokenize(tokens []string) string {
	switch c.tokenizerMode {
	case "tiktoken":
		// TODO: Implement proper tiktoken detokenization
		// For now, fall back to simple
		return strings.Join(tokens, " ")
	default:
		return strings.Join(tokens, " ")
	}
}

// simpleTokenize provides word-based tokenization
// Approximation: ~1.3 tokens per word for English text
func (c *Chunker) simpleTokenize(text string) []string {
	// Split on whitespace to get words
	words := strings.Fields(text)

	// For simple estimation, we treat each word as roughly 1.3 tokens
	// This means we should actually return more granular units
	// For now, we'll use words directly and adjust maxTokens accordingly
	tokens := []string{}
	for _, word := range words {
		// For longer words, we might want to split them
		// But for simplicity, treat each word as a token
		tokens = append(tokens, word)
	}

	return tokens
}

// EstimateTokensForModel estimates tokens based on the model
// This can be expanded to handle model-specific tokenization
func EstimateTokensForModel(text string, model string) int {
	// Model-specific adjustments
	switch {
	case strings.Contains(model, "gpt-4"):
		// GPT-4 tends to have slightly different tokenization
		return len(strings.Fields(text)) * 13 / 10 // 1.3 tokens per word
	case strings.Contains(model, "embedding"):
		// Embedding models often have similar tokenization to GPT-3
		return len(strings.Fields(text)) * 13 / 10
	default:
		// Default estimation
		return len(strings.Fields(text)) * 13 / 10
	}
}