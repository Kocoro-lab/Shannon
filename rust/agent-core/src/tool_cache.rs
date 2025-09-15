use crate::tools::{ToolCall, ToolResult};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};
use tracing::{debug, info, instrument, warn};

/// Cache key for tool results
#[derive(Debug, Clone, Hash, Eq, PartialEq)]
struct CacheKey {
    tool_name: String,
    parameters_hash: u64,
}

impl CacheKey {
    fn from_tool_call(call: &ToolCall) -> Self {
        use std::collections::hash_map::DefaultHasher;
        use std::hash::{Hash, Hasher};

        let mut hasher = DefaultHasher::new();

        // Hash the parameters in a deterministic way
        let params_string =
            serde_json::to_string(&call.parameters).unwrap_or_else(|_| String::new());
        params_string.hash(&mut hasher);

        Self {
            tool_name: call.tool_name.clone(),
            parameters_hash: hasher.finish(),
        }
    }
}

/// Cached tool result with metadata
#[derive(Debug, Clone)]
struct CachedResult {
    result: ToolResult,
    cached_at: Instant,
    last_accessed: Instant, // For true LRU eviction
    ttl: Duration,
    hit_count: u32,
}

impl CachedResult {
    fn is_expired(&self) -> bool {
        self.cached_at.elapsed() > self.ttl
    }
}

/// Statistics for cache performance
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct CacheStats {
    pub total_requests: u64,
    pub cache_hits: u64,
    pub cache_misses: u64,
    pub evictions: u64,
    pub average_ttl_seconds: f64,
}

impl CacheStats {
    pub fn hit_rate(&self) -> f64 {
        if self.total_requests == 0 {
            0.0
        } else {
            self.cache_hits as f64 / self.total_requests as f64
        }
    }
}

/// Tool execution result cache
pub struct ToolCache {
    cache: Arc<RwLock<HashMap<CacheKey, CachedResult>>>,
    stats: Arc<RwLock<CacheStats>>,
    max_size: usize,
    default_ttl: Duration,
}

impl ToolCache {
    pub fn new(max_size: usize, default_ttl_seconds: u64) -> Self {
        Self {
            cache: Arc::new(RwLock::new(HashMap::new())),
            stats: Arc::new(RwLock::new(CacheStats::default())),
            max_size,
            default_ttl: Duration::from_secs(default_ttl_seconds),
        }
    }

    /// Get a cached result if available and not expired
    #[instrument(skip(self, call), fields(tool = %call.tool_name))]
    pub fn get(&self, call: &ToolCall) -> Option<ToolResult> {
        let key = CacheKey::from_tool_call(call);

        let mut cache = self.cache.write().unwrap();
        let mut stats = self.stats.write().unwrap();

        stats.total_requests += 1;

        if let Some(cached) = cache.get_mut(&key) {
            if !cached.is_expired() {
                cached.hit_count += 1;
                cached.last_accessed = Instant::now(); // Update for LRU
                stats.cache_hits += 1;
                debug!(
                    "Cache hit for tool '{}' (hits: {}, age: {:?})",
                    call.tool_name,
                    cached.hit_count,
                    cached.cached_at.elapsed()
                );
                return Some(cached.result.clone());
            } else {
                // Remove expired entry
                debug!("Cache entry expired for tool '{}'", call.tool_name);
                cache.remove(&key);
                stats.evictions += 1;
            }
        }

        stats.cache_misses += 1;
        debug!("Cache miss for tool '{}'", call.tool_name);
        None
    }

    /// Store a tool result in the cache
    #[instrument(skip(self, call, result), fields(tool = %call.tool_name))]
    pub fn put(&self, call: &ToolCall, result: ToolResult, ttl_override: Option<Duration>) {
        // Don't cache failed results
        if !result.success {
            debug!("Not caching failed result for tool '{}'", call.tool_name);
            return;
        }

        let key = CacheKey::from_tool_call(call);
        let ttl = ttl_override.unwrap_or(self.default_ttl);

        let mut cache = self.cache.write().unwrap();
        let mut stats = self.stats.write().unwrap();

        // Evict old entries if cache is full
        if cache.len() >= self.max_size {
            self.evict_oldest(&mut cache, &mut stats);
        }

        let now = Instant::now();
        let cached = CachedResult {
            result,
            cached_at: now,
            last_accessed: now, // Initialize last_accessed
            ttl,
            hit_count: 0,
        };

        // Update average TTL
        let total_ttl = stats.average_ttl_seconds * cache.len() as f64 + ttl.as_secs_f64();
        stats.average_ttl_seconds = total_ttl / (cache.len() + 1) as f64;

        cache.insert(key, cached);
        info!(
            "Cached result for tool '{}' with TTL {:?}",
            call.tool_name, ttl
        );
    }

    /// Evict the least recently used entry from the cache (true LRU)
    fn evict_oldest(&self, cache: &mut HashMap<CacheKey, CachedResult>, stats: &mut CacheStats) {
        if let Some((lru_key, _)) = cache
            .iter()
            .min_by_key(|(_, v)| v.last_accessed) // Use last_accessed for true LRU
            .map(|(k, v)| (k.clone(), v.clone()))
        {
            cache.remove(&lru_key);
            stats.evictions += 1;
            debug!("Evicted LRU cache entry for tool '{}'", lru_key.tool_name);
        }
    }

    /// Clear the entire cache
    pub fn clear(&self) {
        let mut cache = self.cache.write().unwrap();
        let count = cache.len();
        cache.clear();
        info!("Cleared {} cache entries", count);
    }

    /// Get cache statistics
    pub fn get_stats(&self) -> CacheStats {
        self.stats.read().unwrap().clone()
    }

    /// Invalidate cache entries for a specific tool
    pub fn invalidate_tool(&self, tool_name: &str) {
        let mut cache = self.cache.write().unwrap();
        let keys_to_remove: Vec<_> = cache
            .keys()
            .filter(|k| k.tool_name == tool_name)
            .cloned()
            .collect();

        let count = keys_to_remove.len();
        for key in keys_to_remove {
            cache.remove(&key);
        }

        if count > 0 {
            info!(
                "Invalidated {} cache entries for tool '{}'",
                count, tool_name
            );
        }
    }

    /// Remove expired entries from the cache
    #[instrument(skip(self))]
    pub fn sweep_expired(&self) {
        let mut cache = self.cache.write().unwrap();
        let mut stats = self.stats.write().unwrap();

        let expired_keys: Vec<_> = cache
            .iter()
            .filter(|(_, v)| v.is_expired())
            .map(|(k, _)| k.clone())
            .collect();

        for key in &expired_keys {
            cache.remove(key);
            stats.evictions += 1;
        }

        if !expired_keys.is_empty() {
            debug!("Swept {} expired cache entries", expired_keys.len());
        }
    }
}

impl Default for ToolCache {
    fn default() -> Self {
        Self::new(1000, 300) // 1000 entries, 5 minutes default TTL
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cache_basic_operations() {
        let cache = ToolCache::new(10, 60);

        let call = ToolCall {
            tool_name: "test_tool".to_string(),
            parameters: HashMap::from([("param1".to_string(), serde_json::json!("value1"))]),
            call_id: Some("test_1".to_string()),
        };

        let result = ToolResult {
            tool: "test_tool".to_string(),
            success: true,
            output: serde_json::json!({"result": "test"}),
            error: None,
        };

        // Cache miss initially
        assert!(cache.get(&call).is_none());

        // Store result
        cache.put(&call, result.clone(), None);

        // Cache hit
        let cached = cache.get(&call);
        assert!(cached.is_some());
        assert_eq!(cached.unwrap().output, result.output);

        // Check stats
        let stats = cache.get_stats();
        assert_eq!(stats.cache_hits, 1);
        assert_eq!(stats.cache_misses, 1);
        assert_eq!(stats.hit_rate(), 0.5);
    }

    #[test]
    fn test_cache_expiration() {
        let cache = ToolCache::new(10, 0); // 0 second TTL for immediate expiration

        let call = ToolCall {
            tool_name: "test_tool".to_string(),
            parameters: HashMap::new(),
            call_id: None,
        };

        let result = ToolResult {
            tool: "test_tool".to_string(),
            success: true,
            output: serde_json::json!({}),
            error: None,
        };

        // Store with immediate expiration
        cache.put(&call, result, Some(Duration::from_millis(1)));

        // Sleep briefly to ensure expiration
        std::thread::sleep(Duration::from_millis(10));

        // Should be expired
        assert!(cache.get(&call).is_none());

        let stats = cache.get_stats();
        assert_eq!(stats.evictions, 1);
    }

    #[test]
    fn test_failed_results_not_cached() {
        let cache = ToolCache::new(10, 60);

        let call = ToolCall {
            tool_name: "test_tool".to_string(),
            parameters: HashMap::new(),
            call_id: None,
        };

        let failed_result = ToolResult {
            tool: "test_tool".to_string(),
            success: false,
            output: serde_json::json!(null),
            error: Some("Error".to_string()),
        };

        // Try to cache failed result
        cache.put(&call, failed_result, None);

        // Should not be cached
        assert!(cache.get(&call).is_none());
    }

    #[test]
    fn test_cache_invalidation() {
        let cache = ToolCache::new(10, 60);

        // Add multiple entries for same tool
        for i in 0..3 {
            let call = ToolCall {
                tool_name: "test_tool".to_string(),
                parameters: HashMap::from([("id".to_string(), serde_json::json!(i))]),
                call_id: None,
            };

            let result = ToolResult {
                tool: "test_tool".to_string(),
                success: true,
                output: serde_json::json!(i),
                error: None,
            };

            cache.put(&call, result, None);
        }

        // Add entry for different tool
        let other_call = ToolCall {
            tool_name: "other_tool".to_string(),
            parameters: HashMap::new(),
            call_id: None,
        };

        let other_result = ToolResult {
            tool: "other_tool".to_string(),
            success: true,
            output: serde_json::json!("other"),
            error: None,
        };

        cache.put(&other_call, other_result, None);

        // Invalidate test_tool entries
        cache.invalidate_tool("test_tool");

        // test_tool entries should be gone
        for i in 0..3 {
            let call = ToolCall {
                tool_name: "test_tool".to_string(),
                parameters: HashMap::from([("id".to_string(), serde_json::json!(i))]),
                call_id: None,
            };
            assert!(cache.get(&call).is_none());
        }

        // other_tool entry should remain
        assert!(cache.get(&other_call).is_some());
    }
}
