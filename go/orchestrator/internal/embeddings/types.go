package embeddings

import "time"

// Config controls the embedding service behavior
type Config struct {
	// BaseURL points to the LLM service providing /embeddings
	BaseURL string
	// DefaultModel is the default embedding model (e.g., text-embedding-3-small)
	DefaultModel string
	// Timeout for outbound HTTP calls
	Timeout time.Duration
	// EnableRedis enables Redis-backed cache (optional)
	EnableRedis bool
	// RedisAddr in host:port form when EnableRedis is true
	RedisAddr string
	// CacheTTL sets TTL for embedding cache entries
	CacheTTL time.Duration
	// MaxLRU controls in-process LRU size
	MaxLRU int
	// Chunking configuration for long texts
	Chunking ChunkingConfig
}
