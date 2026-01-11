//! Tool result caching.
//!
//! This module provides caching for tool execution results
//! to improve performance and reduce redundant API calls.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, SystemTime};
use tokio::sync::RwLock;

/// A cached tool result.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CachedResult {
    /// The tool result.
    pub result: serde_json::Value,
    /// When this result was cached.
    pub cached_at: SystemTime,
    /// Time-to-live in seconds.
    pub ttl_seconds: u64,
}

impl CachedResult {
    /// Check if this cached result has expired.
    #[must_use]
    pub fn is_expired(&self) -> bool {
        if let Ok(elapsed) = self.cached_at.elapsed() {
            elapsed.as_secs() > self.ttl_seconds
        } else {
            true
        }
    }
}

/// Cache key for tool results.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct CacheKey {
    /// Tool name.
    pub tool_name: String,
    /// Tool arguments (normalized JSON string).
    pub arguments: String,
}

impl CacheKey {
    /// Create a new cache key.
    pub fn new(tool_name: impl Into<String>, arguments: &serde_json::Value) -> Self {
        Self {
            tool_name: tool_name.into(),
            arguments: serde_json::to_string(arguments).unwrap_or_default(),
        }
    }
}

/// Tool result cache.
#[derive(Clone)]
pub struct ToolCache {
    /// Cached results.
    cache: Arc<RwLock<HashMap<CacheKey, CachedResult>>>,
    /// Default TTL in seconds.
    default_ttl: u64,
}

impl ToolCache {
    /// Create a new tool cache with default TTL of 1 hour.
    #[must_use]
    pub fn new() -> Self {
        Self::with_ttl(3600)
    }

    /// Create a new tool cache with custom default TTL.
    #[must_use]
    pub fn with_ttl(ttl_seconds: u64) -> Self {
        Self {
            cache: Arc::new(RwLock::new(HashMap::new())),
            default_ttl: ttl_seconds,
        }
    }

    /// Get a cached result if available and not expired.
    pub async fn get(
        &self,
        tool_name: &str,
        arguments: &serde_json::Value,
    ) -> Option<serde_json::Value> {
        let key = CacheKey::new(tool_name, arguments);
        let mut cache = self.cache.write().await;

        if let Some(cached) = cache.get(&key) {
            if cached.is_expired() {
                // Remove expired entry
                cache.remove(&key);
                None
            } else {
                Some(cached.result.clone())
            }
        } else {
            None
        }
    }

    /// Cache a tool result.
    pub async fn put(
        &self,
        tool_name: &str,
        arguments: &serde_json::Value,
        result: serde_json::Value,
    ) {
        self.put_with_ttl(tool_name, arguments, result, self.default_ttl)
            .await;
    }

    /// Cache a tool result with custom TTL.
    pub async fn put_with_ttl(
        &self,
        tool_name: &str,
        arguments: &serde_json::Value,
        result: serde_json::Value,
        ttl_seconds: u64,
    ) {
        let key = CacheKey::new(tool_name, arguments);
        let cached = CachedResult {
            result,
            cached_at: SystemTime::now(),
            ttl_seconds,
        };

        let mut cache = self.cache.write().await;
        cache.insert(key, cached);
    }

    /// Invalidate a specific cache entry.
    pub async fn invalidate(&self, tool_name: &str, arguments: &serde_json::Value) -> bool {
        let key = CacheKey::new(tool_name, arguments);
        let mut cache = self.cache.write().await;
        cache.remove(&key).is_some()
    }

    /// Invalidate all cache entries for a specific tool.
    pub async fn invalidate_tool(&self, tool_name: &str) -> usize {
        let mut cache = self.cache.write().await;
        let before = cache.len();
        cache.retain(|k, _| k.tool_name != tool_name);
        before - cache.len()
    }

    /// Clear all cached results.
    pub async fn clear(&self) {
        let mut cache = self.cache.write().await;
        cache.clear();
    }

    /// Remove all expired entries.
    pub async fn cleanup_expired(&self) -> usize {
        let mut cache = self.cache.write().await;
        let before = cache.len();
        cache.retain(|_, v| !v.is_expired());
        before - cache.len()
    }

    /// Get cache statistics.
    pub async fn stats(&self) -> CacheStats {
        let cache = self.cache.read().await;
        let total = cache.len();
        let expired = cache.values().filter(|v| v.is_expired()).count();

        CacheStats {
            total_entries: total,
            expired_entries: expired,
            valid_entries: total - expired,
        }
    }
}

impl Default for ToolCache {
    fn default() -> Self {
        Self::new()
    }
}

/// Cache statistics.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CacheStats {
    /// Total number of cached entries.
    pub total_entries: usize,
    /// Number of expired entries.
    pub expired_entries: usize,
    /// Number of valid entries.
    pub valid_entries: usize,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[tokio::test]
    async fn test_cache_put_and_get() {
        let cache = ToolCache::new();
        let args = json!({"query": "test"});
        let result = json!({"answer": "42"});

        cache.put("test_tool", &args, result.clone()).await;
        let cached = cache.get("test_tool", &args).await;

        assert_eq!(cached, Some(result));
    }

    #[tokio::test]
    async fn test_cache_expiration() {
        let cache = ToolCache::with_ttl(1); // 1 second TTL
        let args = json!({"query": "test"});
        let result = json!({"answer": "42"});

        cache.put("test_tool", &args, result.clone()).await;

        // Wait for expiration
        tokio::time::sleep(Duration::from_secs(2)).await;

        let cached = cache.get("test_tool", &args).await;
        assert!(cached.is_none());
    }

    #[tokio::test]
    async fn test_cache_invalidate() {
        let cache = ToolCache::new();
        let args = json!({"query": "test"});
        let result = json!({"answer": "42"});

        cache.put("test_tool", &args, result).await;
        assert!(cache.invalidate("test_tool", &args).await);

        let cached = cache.get("test_tool", &args).await;
        assert!(cached.is_none());
    }
}
