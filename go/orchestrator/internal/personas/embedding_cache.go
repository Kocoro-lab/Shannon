package personas

import (
	"crypto/md5"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EmbeddingCacheEntry holds a cached embedding with metadata
type EmbeddingCacheEntry struct {
	Embedding   []float64 `json:"embedding"`
	Timestamp   time.Time `json:"timestamp"`
	AccessCount int       `json:"access_count"`
	LastAccess  time.Time `json:"last_access"`
}

// SimilarityCacheEntry holds a cached similarity result
type SimilarityCacheEntry struct {
	Similarity  float64   `json:"similarity"`
	Timestamp   time.Time `json:"timestamp"`
	AccessCount int       `json:"access_count"`
	LastAccess  time.Time `json:"last_access"`
}

// InMemoryEmbeddingCache implements EmbeddingCache using in-memory storage
type InMemoryEmbeddingCache struct {
	embeddings      map[string]*EmbeddingCacheEntry
	similarities    map[string]*SimilarityCacheEntry
	ttl             time.Duration
	maxSize         int
	cleanupInterval time.Duration
	mu              sync.RWMutex
	logger          *zap.Logger
	stopCleanup     chan struct{}
	wg              sync.WaitGroup
}

// NewEmbeddingCache creates a new embedding cache
func NewEmbeddingCache(ttl time.Duration, logger *zap.Logger) EmbeddingCache {
	cache := &InMemoryEmbeddingCache{
		embeddings:      make(map[string]*EmbeddingCacheEntry),
		similarities:    make(map[string]*SimilarityCacheEntry),
		ttl:             ttl,
		maxSize:         10000,    // Default max size
		cleanupInterval: ttl / 10, // Cleanup every 1/10 of TTL
		logger:          logger,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine
	cache.wg.Add(1)
	go cache.cleanupLoop()

	return cache
}

// NewEmbeddingCacheWithSize creates a new embedding cache with custom size
func NewEmbeddingCacheWithSize(ttl time.Duration, maxSize int, logger *zap.Logger) EmbeddingCache {
	cache := &InMemoryEmbeddingCache{
		embeddings:      make(map[string]*EmbeddingCacheEntry),
		similarities:    make(map[string]*SimilarityCacheEntry),
		ttl:             ttl,
		maxSize:         maxSize,
		cleanupInterval: ttl / 10,
		logger:          logger,
		stopCleanup:     make(chan struct{}),
	}

	cache.wg.Add(1)
	go cache.cleanupLoop()

	return cache
}

// Get retrieves an embedding from cache
func (c *InMemoryEmbeddingCache) Get(text string) ([]float64, bool) {
	key := c.generateKey(text)

	c.mu.RLock()
	entry, exists := c.embeddings[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.Timestamp) > c.ttl {
		c.mu.Lock()
		delete(c.embeddings, key)
		c.mu.Unlock()
		return nil, false
	}

	// Update access statistics
	c.mu.Lock()
	entry.AccessCount++
	entry.LastAccess = time.Now()
	c.mu.Unlock()

	// Return a copy to prevent modification
	result := make([]float64, len(entry.Embedding))
	copy(result, entry.Embedding)

	return result, true
}

// Set stores an embedding in cache
func (c *InMemoryEmbeddingCache) Set(text string, embedding []float64) {
	key := c.generateKey(text)

	// Create a copy to prevent external modification
	embeddingCopy := make([]float64, len(embedding))
	copy(embeddingCopy, embedding)

	entry := &EmbeddingCacheEntry{
		Embedding:   embeddingCopy,
		Timestamp:   time.Now(),
		AccessCount: 1,
		LastAccess:  time.Now(),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries to stay under max size
	if len(c.embeddings) >= c.maxSize {
		c.evictLRU()
	}

	c.embeddings[key] = entry
}

// GetSimilarity retrieves a similarity result from cache
func (c *InMemoryEmbeddingCache) GetSimilarity(text1, text2 string) (float64, bool) {
	key := c.generateSimilarityKey(text1, text2)

	c.mu.RLock()
	entry, exists := c.similarities[key]
	c.mu.RUnlock()

	if !exists {
		return 0, false
	}

	// Check if entry has expired
	if time.Since(entry.Timestamp) > c.ttl {
		c.mu.Lock()
		delete(c.similarities, key)
		c.mu.Unlock()
		return 0, false
	}

	// Update access statistics
	c.mu.Lock()
	entry.AccessCount++
	entry.LastAccess = time.Now()
	c.mu.Unlock()

	return entry.Similarity, true
}

// SetSimilarity stores a similarity result in cache
func (c *InMemoryEmbeddingCache) SetSimilarity(text1, text2 string, similarity float64) {
	key := c.generateSimilarityKey(text1, text2)

	entry := &SimilarityCacheEntry{
		Similarity:  similarity,
		Timestamp:   time.Now(),
		AccessCount: 1,
		LastAccess:  time.Now(),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries to stay under max size
	if len(c.similarities) >= c.maxSize {
		c.evictSimilarityLRU()
	}

	c.similarities[key] = entry
}

// Clear removes all entries from cache
func (c *InMemoryEmbeddingCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.embeddings = make(map[string]*EmbeddingCacheEntry)
	c.similarities = make(map[string]*SimilarityCacheEntry)

	c.logger.Info("Embedding cache cleared")
}

// Size returns the total number of cached items
func (c *InMemoryEmbeddingCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.embeddings) + len(c.similarities)
}

// GetStats returns cache statistics
func (c *InMemoryEmbeddingCache) GetStats() EmbeddingCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalHits, totalAccesses int
	var oldestEntry, newestEntry time.Time

	// Calculate embedding stats
	for _, entry := range c.embeddings {
		totalHits += entry.AccessCount
		totalAccesses++

		if oldestEntry.IsZero() || entry.Timestamp.Before(oldestEntry) {
			oldestEntry = entry.Timestamp
		}
		if newestEntry.IsZero() || entry.Timestamp.After(newestEntry) {
			newestEntry = entry.Timestamp
		}
	}

	// Calculate similarity stats
	for _, entry := range c.similarities {
		totalHits += entry.AccessCount
		totalAccesses++

		if oldestEntry.IsZero() || entry.Timestamp.Before(oldestEntry) {
			oldestEntry = entry.Timestamp
		}
		if newestEntry.IsZero() || entry.Timestamp.After(newestEntry) {
			newestEntry = entry.Timestamp
		}
	}

	var hitRate float64
	if totalAccesses > 0 {
		hitRate = float64(totalHits) / float64(totalAccesses)
	}

	return EmbeddingCacheStats{
		EmbeddingCount:  len(c.embeddings),
		SimilarityCount: len(c.similarities),
		TotalHits:       int64(totalHits),
		TotalAccesses:   totalAccesses,
		HitRate:         hitRate,
		OldestEntry:     oldestEntry,
		NewestEntry:     newestEntry,
		MaxSize:         c.maxSize,
		TTL:             c.ttl,
	}
}

// EmbeddingCacheStats holds embedding cache statistics
type EmbeddingCacheStats struct {
	EmbeddingCount  int           `json:"embedding_count"`
	SimilarityCount int           `json:"similarity_count"`
	TotalHits       int64         `json:"total_hits"`
	TotalAccesses   int           `json:"total_accesses"`
	HitRate         float64       `json:"hit_rate"`
	OldestEntry     time.Time     `json:"oldest_entry"`
	NewestEntry     time.Time     `json:"newest_entry"`
	MaxSize         int           `json:"max_size"`
	TTL             time.Duration `json:"ttl"`
}

// generateKey generates a cache key for text
func (c *InMemoryEmbeddingCache) generateKey(text string) string {
	hash := md5.Sum([]byte(text))
	return fmt.Sprintf("emb_%x", hash)
}

// generateSimilarityKey generates a cache key for similarity between two texts
func (c *InMemoryEmbeddingCache) generateSimilarityKey(text1, text2 string) string {
	// Ensure consistent ordering for bidirectional similarity
	if text1 > text2 {
		text1, text2 = text2, text1
	}

	combined := text1 + "|" + text2
	hash := md5.Sum([]byte(combined))
	return fmt.Sprintf("sim_%x", hash)
}

// evictLRU removes the least recently used embedding entry
func (c *InMemoryEmbeddingCache) evictLRU() {
	if len(c.embeddings) == 0 {
		return
	}

	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.embeddings {
		if oldestTime.IsZero() || entry.LastAccess.Before(oldestTime) {
			oldestTime = entry.LastAccess
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(c.embeddings, oldestKey)
		c.logger.Debug("Evicted LRU embedding entry", zap.String("key", oldestKey))
	}
}

// evictSimilarityLRU removes the least recently used similarity entry
func (c *InMemoryEmbeddingCache) evictSimilarityLRU() {
	if len(c.similarities) == 0 {
		return
	}

	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.similarities {
		if oldestTime.IsZero() || entry.LastAccess.Before(oldestTime) {
			oldestTime = entry.LastAccess
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(c.similarities, oldestKey)
		c.logger.Debug("Evicted LRU similarity entry", zap.String("key", oldestKey))
	}
}

// cleanupLoop periodically removes expired entries
func (c *InMemoryEmbeddingCache) cleanupLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupExpired()
		case <-c.stopCleanup:
			return
		}
	}
}

// cleanupExpired removes expired entries from cache
func (c *InMemoryEmbeddingCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	embeddingsCleaned := 0
	similaritiesCleaned := 0

	// Clean expired embeddings
	for key, entry := range c.embeddings {
		if now.Sub(entry.Timestamp) > c.ttl {
			delete(c.embeddings, key)
			embeddingsCleaned++
		}
	}

	// Clean expired similarities
	for key, entry := range c.similarities {
		if now.Sub(entry.Timestamp) > c.ttl {
			delete(c.similarities, key)
			similaritiesCleaned++
		}
	}

	if embeddingsCleaned > 0 || similaritiesCleaned > 0 {
		c.logger.Debug("Cleaned up expired cache entries",
			zap.Int("embeddings_cleaned", embeddingsCleaned),
			zap.Int("similarities_cleaned", similaritiesCleaned))
	}
}

// Close gracefully shuts down the cache
func (c *InMemoryEmbeddingCache) Close() error {
	close(c.stopCleanup)
	c.wg.Wait()

	c.logger.Info("Embedding cache closed gracefully")
	return nil
}

// Warmup pre-loads cache with common embeddings
func (c *InMemoryEmbeddingCache) Warmup(texts []string, embeddingFunc func(string) ([]float64, error)) error {
	c.logger.Info("Starting cache warmup", zap.Int("text_count", len(texts)))

	for i, text := range texts {
		// Check if already cached
		if _, exists := c.Get(text); exists {
			continue
		}

		// Generate and cache embedding
		embedding, err := embeddingFunc(text)
		if err != nil {
			c.logger.Warn("Failed to generate embedding during warmup",
				zap.Int("index", i),
				zap.String("text", text[:min(50, len(text))]),
				zap.Error(err))
			continue
		}

		c.Set(text, embedding)
	}

	c.logger.Info("Cache warmup completed", zap.Int("cache_size", c.Size()))
	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
