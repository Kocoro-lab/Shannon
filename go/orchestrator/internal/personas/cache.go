package personas

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CacheEntry represents a single cache entry
type CacheEntry struct {
	PersonaID string
	Timestamp time.Time
	Hits      int64
	mu        sync.RWMutex
}

// SafeCache provides thread-safe caching with automatic cleanup
type SafeCache struct {
	data    map[string]*CacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	cleanup chan struct{}
	wg      sync.WaitGroup
	logger  *zap.Logger
	metrics *Metrics
	closed  bool
	closeMu sync.RWMutex
}

// NewSafeCache creates a new thread-safe cache
func NewSafeCache(ttl time.Duration, logger *zap.Logger, metrics *Metrics) *SafeCache {
	c := &SafeCache{
		data:    make(map[string]*CacheEntry),
		ttl:     ttl,
		cleanup: make(chan struct{}),
		logger:  logger,
		metrics: metrics,
	}

	// Start cleanup goroutine
	c.wg.Add(1)
	go c.cleanupExpired()

	return c
}

// Get retrieves a value from the cache
func (c *SafeCache) Get(key string) (string, bool) {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return "", false
	}
	c.closeMu.RUnlock()

	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	if !exists {
		if c.metrics != nil {
			c.metrics.CacheMisses.Inc()
		}
		return "", false
	}

	entry.mu.RLock()
	if time.Since(entry.Timestamp) > c.ttl {
		entry.mu.RUnlock()
		c.invalidate(key) // Asynchronously delete expired entry
		if c.metrics != nil {
			c.metrics.CacheMisses.Inc()
		}
		return "", false
	}

	personaID := entry.PersonaID
	entry.mu.RUnlock()

	// Atomically update hit count
	entry.mu.Lock()
	entry.Hits++
	entry.mu.Unlock()

	if c.metrics != nil {
		c.metrics.CacheHits.Inc()
	}

	return personaID, true
}

// Set stores a value in the cache
func (c *SafeCache) Set(key, personaID string) {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return
	}
	c.closeMu.RUnlock()

	c.mu.Lock()
	c.data[key] = &CacheEntry{
		PersonaID: personaID,
		Timestamp: time.Now(),
		Hits:      0,
	}
	c.mu.Unlock()

	if c.metrics != nil {
		c.metrics.CacheSize.Set(float64(c.Size()))
	}
}

// invalidate asynchronously removes an expired entry
func (c *SafeCache) invalidate(key string) {
	go func() {
		c.mu.Lock()
		delete(c.data, key)
		c.mu.Unlock()

		if c.metrics != nil {
			c.metrics.CacheSize.Set(float64(c.Size()))
		}
	}()
}

// cleanupExpired runs the periodic cleanup process
func (c *SafeCache) cleanupExpired() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.ttl / 2) // Clean every half TTL
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.performCleanup()
		case <-c.cleanup:
			return
		}
	}
}

// performCleanup removes expired entries in batch
func (c *SafeCache) performCleanup() {
	now := time.Now()
	toDelete := make([]string, 0)

	// Collect expired keys
	c.mu.RLock()
	for key, entry := range c.data {
		entry.mu.RLock()
		if now.Sub(entry.Timestamp) > c.ttl {
			toDelete = append(toDelete, key)
		}
		entry.mu.RUnlock()
	}
	c.mu.RUnlock()

	// Delete expired entries if any
	if len(toDelete) > 0 {
		c.mu.Lock()
		for _, key := range toDelete {
			delete(c.data, key)
		}
		c.mu.Unlock()

		if c.logger != nil {
			c.logger.Debug("Cleaned up expired cache entries",
				zap.Int("count", len(toDelete)))
		}

		if c.metrics != nil {
			c.metrics.CacheSize.Set(float64(c.Size()))
		}
	}
}

// Size returns the current cache size
func (c *SafeCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Clear removes all entries from the cache
func (c *SafeCache) Clear() {
	c.mu.Lock()
	c.data = make(map[string]*CacheEntry)
	c.mu.Unlock()

	if c.metrics != nil {
		c.metrics.CacheSize.Set(0)
	}
}

// Stats returns cache statistics
func (c *SafeCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStats{
		Size: len(c.data),
		TTL:  c.ttl,
	}

	totalHits := int64(0)
	oldestEntry := time.Now()
	newestEntry := time.Time{}

	for _, entry := range c.data {
		entry.mu.RLock()
		totalHits += entry.Hits
		if entry.Timestamp.Before(oldestEntry) {
			oldestEntry = entry.Timestamp
		}
		if entry.Timestamp.After(newestEntry) {
			newestEntry = entry.Timestamp
		}
		entry.mu.RUnlock()
	}

	stats.TotalHits = totalHits
	if len(c.data) > 0 {
		stats.OldestEntry = oldestEntry
		stats.NewestEntry = newestEntry
	}

	return stats
}

// Close gracefully shuts down the cache
func (c *SafeCache) Close() error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	c.closeMu.Unlock()

	// Signal cleanup goroutine to stop
	close(c.cleanup)

	// Wait for cleanup goroutine with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if c.logger != nil {
			c.logger.Info("Cache closed gracefully")
		}
	case <-time.After(5 * time.Second):
		if c.logger != nil {
			c.logger.Warn("Cache close timeout reached")
		}
		return ErrCacheUnavailable
	}

	// Clear all data
	c.Clear()

	return nil
}

// CacheStats holds cache statistics
type CacheStats struct {
	Size        int           `json:"size"`
	TotalHits   int64         `json:"total_hits"`
	TTL         time.Duration `json:"ttl"`
	OldestEntry time.Time     `json:"oldest_entry,omitempty"`
	NewestEntry time.Time     `json:"newest_entry,omitempty"`
}

// generateCacheKey generates a cache key from request parameters
func generateCacheKey(req *SelectionRequest) string {
	// Simple implementation - in production might want more sophisticated hashing
	base := req.Description + "|" + req.TaskType
	if req.UserID != "" {
		base += "|" + req.UserID
	}
	if req.ComplexityScore > 0 {
		base += "|" + fmt.Sprintf("%.2f", req.ComplexityScore)
	}
	return base
}
