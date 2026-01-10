use crate::config::Config;
use crate::metrics::{MEMORY_POOL_TOTAL_BYTES, MEMORY_POOL_USED_BYTES};
use anyhow::{Context, Result};
use bytes::Bytes;
use chrono::{DateTime, Utc};
use prometheus::{register_histogram_vec, register_int_counter_vec, HistogramVec, IntCounterVec};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, OnceLock};
use tokio::sync::RwLock;
use tracing::{debug, info, warn};

static ALLOCATIONS_TOTAL: OnceLock<IntCounterVec> = OnceLock::new();
static EVICTIONS_TOTAL: OnceLock<IntCounterVec> = OnceLock::new();
static ALLOCATION_SIZE: OnceLock<HistogramVec> = OnceLock::new();

// Thread-safe initialization result for memory metrics
static MEMORY_INIT_RESULT: OnceLock<Result<()>> = OnceLock::new();

// Thread-safe memory metrics initialization
fn init_memory_metrics() -> Result<()> {
    match MEMORY_INIT_RESULT.get_or_init(init_memory_metrics_internal) {
        Ok(()) => Ok(()),
        Err(e) => Err(anyhow::anyhow!(
            "Memory metrics initialization failed: {}",
            e
        )),
    }
}

// Internal initialization - only called once
fn init_memory_metrics_internal() -> Result<()> {
    // Check if already initialized (defensive)
    if ALLOCATIONS_TOTAL.get().is_some() {
        return Ok(());
    }

    ALLOCATIONS_TOTAL
        .set(
            register_int_counter_vec!(
                "agent_core_memory_allocations_total",
                "Total memory allocations",
                &["status"]
            )
            .context("Failed to register ALLOCATIONS_TOTAL metric")?,
        )
        .map_err(|_| anyhow::anyhow!("ALLOCATIONS_TOTAL already initialized"))?;

    EVICTIONS_TOTAL
        .set(
            register_int_counter_vec!(
                "agent_core_memory_evictions_total",
                "Total memory evictions",
                &["reason"]
            )
            .context("Failed to register EVICTIONS_TOTAL metric")?,
        )
        .map_err(|_| anyhow::anyhow!("EVICTIONS_TOTAL already initialized"))?;

    ALLOCATION_SIZE
        .set(
            register_histogram_vec!(
                "agent_core_memory_allocation_size_bytes",
                "Memory allocation size distribution",
                &["type"],
                vec![1024.0, 10240.0, 102400.0, 1048576.0, 10485760.0] // 1KB, 10KB, 100KB, 1MB, 10MB
            )
            .context("Failed to register ALLOCATION_SIZE metric")?,
        )
        .map_err(|_| anyhow::anyhow!("ALLOCATION_SIZE already initialized"))?;

    Ok(())
}

/// Runtime memory pool for agent execution
///
/// Policy overview:
/// - Scope: manages in-memory data during task execution (not persistent storage).
/// - TTL semantics: `ttl_seconds == 0` means the entry is considered expired immediately and
///   will be removed on the next access or cleanup sweep. Positive TTLs are enforced against
///   `created_at` both on access and during periodic cleanup.
/// - Eviction: before rejecting an allocation that would exceed the pool size, the pool
///   attempts to free space by evicting least‑recently‑used (LRU) entries. When two entries
///   share the same `last_accessed` timestamp, eviction falls back to `created_at` to
///   deterministically remove the older entry.
#[derive(Debug)]
pub struct MemoryPool {
    pools: Arc<RwLock<HashMap<String, MemorySlot>>>,
    max_total_size: usize,
    current_size: Arc<RwLock<usize>>,
    high_water_mark: Arc<RwLock<usize>>,
    allocation_count: Arc<RwLock<u64>>,
    sweeper_handle: Option<tokio::task::JoinHandle<()>>,
    shutdown_tx: Option<tokio::sync::oneshot::Sender<()>>,
    // Pressure thresholds
    warn_threshold: f64,     // Default 75%
    critical_threshold: f64, // Default 90%
}

#[derive(Clone, Debug)]
struct MemorySlot {
    data: Bytes,
    created_at: std::time::Instant,
    ttl_seconds: u64,
    access_count: u32,
    last_accessed: std::time::Instant,
}

impl MemoryPool {
    pub fn new(max_size_mb: usize) -> Self {
        let config = Config::global().unwrap_or_default();
        let pressure_threshold = config.memory.pressure_threshold;

        // Initialize memory-specific metrics if not already done (thread-safe)
        // This uses std::sync::Once internally so it's safe to call multiple times
        if let Err(e) = init_memory_metrics() {
            tracing::warn!("Failed to initialize memory metrics: {}", e);
        }

        // Initialize pool metrics
        if let Some(total_bytes) = MEMORY_POOL_TOTAL_BYTES.get() {
            total_bytes.set((max_size_mb * 1024 * 1024) as f64);
        }
        if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
            used_bytes.set(0.0);
        }

        Self {
            pools: Arc::new(RwLock::new(HashMap::new())),
            max_total_size: max_size_mb * 1024 * 1024,
            current_size: Arc::new(RwLock::new(0)),
            high_water_mark: Arc::new(RwLock::new(0)),
            allocation_count: Arc::new(RwLock::new(0)),
            sweeper_handle: None,
            shutdown_tx: None,
            warn_threshold: pressure_threshold * 0.8, // 80% of pressure threshold
            critical_threshold: pressure_threshold * 0.95, // 95% of pressure threshold
        }
    }

    /// Start background sweeper task
    pub fn start_sweeper(mut self, interval_ms: u64) -> Self {
        let (shutdown_tx, mut shutdown_rx) = tokio::sync::oneshot::channel();
        let pools = self.pools.clone();
        let current_size = self.current_size.clone();
        let max_size = self.max_total_size;

        let handle = tokio::spawn(async move {
            let mut interval =
                tokio::time::interval(tokio::time::Duration::from_millis(interval_ms));

            loop {
                tokio::select! {
                    _ = interval.tick() => {
                        // Perform cleanup
                        let mut pools_guard = pools.write().await;
                        let mut expired_keys = Vec::new();

                        for (key, slot) in pools_guard.iter() {
                            if slot.ttl_seconds > 0 && slot.created_at.elapsed().as_secs() > slot.ttl_seconds {
                                expired_keys.push(key.clone());
                            }
                        }

                        let mut total_freed = 0;
                        let num_expired = expired_keys.len();
                        for key in expired_keys {
                            if let Some(slot) = pools_guard.remove(&key) {
                                total_freed += slot.data.len();
                                if let Some(evictions) = EVICTIONS_TOTAL.get() {
                                    evictions.with_label_values(&["expired"]).inc();
                                }
                            }
                        }

                        if total_freed > 0 {
                            let mut size_guard = current_size.write().await;
                            *size_guard -= total_freed;
                            if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
                                used_bytes.set(*size_guard as f64);
                            }
                            info!("Sweeper: freed {} bytes from {} expired entries", total_freed, num_expired);
                        }

                        drop(pools_guard);

                        // Check pressure and log warnings
                        let current = *current_size.read().await;
                        let usage_pct = (current as f64 / max_size as f64) * 100.0;

                        if usage_pct > 90.0 {
                            warn!("Memory pool critical: {:.1}% used", usage_pct);
                        } else if usage_pct > 75.0 {
                            debug!("Memory pool warning: {:.1}% used", usage_pct);
                        }
                    }
                    _ = &mut shutdown_rx => {
                        info!("Memory pool sweeper shutting down");
                        break;
                    }
                }
            }
        });

        self.sweeper_handle = Some(handle);
        self.shutdown_tx = Some(shutdown_tx);
        self
    }

    pub async fn allocate(&self, key: String, data: Bytes, ttl_seconds: u64) -> Result<()> {
        let data_size = data.len();

        // First, cleanup expired entries to free space
        self.cleanup_expired().await;

        // If allocation would exceed limits, try LRU eviction first
        let mut current = *self.current_size.read().await;
        if current + data_size > self.max_total_size {
            let needed = current + data_size - self.max_total_size;
            let freed = self.evict_lru(needed).await;
            current = *self.current_size.read().await;
            if current + data_size > self.max_total_size {
                warn!("Memory allocation denied: would exceed limit even after LRU eviction");
                if let Some(allocations) = ALLOCATIONS_TOTAL.get() {
                    allocations.with_label_values(&["rejected"]).inc();
                }
                anyhow::bail!(
                    "Memory pool exhausted: need {} bytes, have {} available after freeing {}",
                    data_size,
                    self.max_total_size.saturating_sub(current),
                    freed
                );
            }
        }

        // Store the data
        let mut pools = self.pools.write().await;

        // Check if key already exists and deallocate old data
        if let Some(old_slot) = pools.get(&key) {
            *self.current_size.write().await -= old_slot.data.len();
        }

        pools.insert(
            key.clone(),
            MemorySlot {
                data,
                created_at: std::time::Instant::now(),
                ttl_seconds,
                access_count: 0,
                last_accessed: std::time::Instant::now(),
            },
        );

        // Update statistics
        let mut current_size = self.current_size.write().await;
        *current_size += data_size;

        // Update metrics
        if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
            used_bytes.set(*current_size as f64);
        }
        if let Some(allocations) = ALLOCATIONS_TOTAL.get() {
            allocations.with_label_values(&["success"]).inc();
        }
        if let Some(alloc_size) = ALLOCATION_SIZE.get() {
            alloc_size
                .with_label_values(&["normal"])
                .observe(data_size as f64);
        }

        // Update high water mark
        let mut hwm = self.high_water_mark.write().await;
        if *current_size > *hwm {
            *hwm = *current_size;
        }

        *self.allocation_count.write().await += 1;

        // Pressure logging (non-fatal)
        let usage_pct = *current_size as f64 / self.max_total_size as f64;
        if usage_pct > self.critical_threshold {
            warn!(
                "Memory pool critical: {:.1}% used after allocation",
                usage_pct * 100.0
            );
        } else if usage_pct > self.warn_threshold {
            debug!(
                "Memory pool high: {:.1}% used after allocation",
                usage_pct * 100.0
            );
        }

        debug!(
            "Allocated {} bytes for key: {} (total: {} bytes)",
            data_size, key, *current_size
        );
        Ok(())
    }

    // TTL semantics: ttl_seconds == 0 means "expire immediately".
    // Non-zero TTLs are checked against created_at on access and cleanup.
    pub async fn retrieve(&self, key: &str) -> Option<Bytes> {
        let mut pools = self.pools.write().await;

        if let Some(slot) = pools.get_mut(key) {
            // Check if TTL has expired
            // TTL semantics: 0 means immediate expiry
            if slot.ttl_seconds == 0
                || (slot.ttl_seconds > 0 && slot.created_at.elapsed().as_secs() > slot.ttl_seconds)
            {
                debug!("Memory slot expired for key: {}", key);
                // Remove expired slot
                let data_size = slot.data.len();
                pools.remove(key);
                *self.current_size.write().await -= data_size;
                return None;
            }

            // Update access statistics
            slot.access_count += 1;
            slot.last_accessed = std::time::Instant::now();

            Some(slot.data.clone())
        } else {
            None
        }
    }

    #[allow(dead_code)]
    pub async fn deallocate(&self, key: &str) -> Result<()> {
        let mut pools = self.pools.write().await;

        if let Some(slot) = pools.remove(key) {
            let size = slot.data.len();
            let mut current = self.current_size.write().await;
            *current -= size;
            if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
                used_bytes.set(*current as f64);
            }
            debug!("Deallocated {} bytes for key: {}", size, key);
        }

        Ok(())
    }

    pub async fn cleanup_expired(&self) {
        let mut pools = self.pools.write().await;
        let mut expired_keys = Vec::new();

        for (key, slot) in pools.iter() {
            if slot.ttl_seconds == 0
                || (slot.ttl_seconds > 0 && slot.created_at.elapsed().as_secs() > slot.ttl_seconds)
            {
                expired_keys.push(key.clone());
            }
        }

        let mut total_freed = 0;
        let num_expired = expired_keys.len();
        for key in expired_keys {
            if let Some(slot) = pools.remove(&key) {
                total_freed += slot.data.len();
            }
        }

        if total_freed > 0 {
            let mut current = self.current_size.write().await;
            *current -= total_freed;
            if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
                used_bytes.set(*current as f64);
            }
            if let Some(evictions) = EVICTIONS_TOTAL.get() {
                evictions
                    .with_label_values(&["expired"])
                    .inc_by(num_expired as u64);
            }
            debug!(
                "Cleaned up {} bytes of expired memory from {} entries",
                total_freed, num_expired
            );
        }
    }

    /// Evict least recently used entries to free up space
    // Evict least-recently-used entries until at least needed_bytes are freed.
    // Tie-breaker: if last_accessed is equal, fall back to created_at to stabilize ordering.
    async fn evict_lru(&self, needed_bytes: usize) -> usize {
        let mut pools = self.pools.write().await;

        // Sort entries by last access time; tie-break on created_at to stabilize ordering
        let mut entries: Vec<(String, std::time::Instant, std::time::Instant, usize)> = pools
            .iter()
            .map(|(k, v)| (k.clone(), v.last_accessed, v.created_at, v.data.len()))
            .collect();
        entries.sort_by(|a, b| {
            let ord = a.1.cmp(&b.1);
            if ord == std::cmp::Ordering::Equal {
                a.2.cmp(&b.2)
            } else {
                ord
            }
        });

        let mut freed = 0;
        let mut evicted_count = 0;
        for (key, _, _, size) in entries {
            if freed >= needed_bytes {
                break;
            }

            pools.remove(&key);
            freed += size;
            evicted_count += 1;
            debug!("Evicted LRU entry: {} ({} bytes)", key, size);
        }

        if freed > 0 {
            let mut current = self.current_size.write().await;
            *current -= freed;
            if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
                used_bytes.set(*current as f64);
            }
            if let Some(evictions) = EVICTIONS_TOTAL.get() {
                evictions
                    .with_label_values(&["lru"])
                    .inc_by(evicted_count as u64);
            }
            info!(
                "Evicted {} bytes via LRU from {} entries",
                freed, evicted_count
            );
        }

        freed
    }

    #[allow(dead_code)]
    pub async fn clear(&self) {
        let mut pools = self.pools.write().await;
        pools.clear();
        *self.current_size.write().await = 0;
        if let Some(used_bytes) = MEMORY_POOL_USED_BYTES.get() {
            used_bytes.set(0.0);
        }
        info!("Cleared all memory pools");
    }

    pub async fn get_usage_stats(&self) -> (usize, usize) {
        let current = *self.current_size.read().await;
        (current, self.max_total_size)
    }

    #[allow(dead_code)]
    pub async fn get_detailed_stats(&self) -> MemoryStats {
        let pools = self.pools.read().await;
        let current_size = *self.current_size.read().await;
        let high_water_mark = *self.high_water_mark.read().await;
        let allocation_count = *self.allocation_count.read().await;

        MemoryStats {
            current_size,
            max_size: self.max_total_size,
            high_water_mark,
            allocation_count,
            entry_count: pools.len(),
            fragmentation: self.calculate_fragmentation(&pools),
        }
    }

    #[allow(dead_code)]
    fn calculate_fragmentation(&self, pools: &HashMap<String, MemorySlot>) -> f64 {
        if pools.is_empty() {
            return 0.0;
        }

        let sizes: Vec<usize> = pools.values().map(|s| s.data.len()).collect();
        let avg_size = sizes.iter().sum::<usize>() as f64 / sizes.len() as f64;
        let variance: f64 = sizes
            .iter()
            .map(|&s| {
                let diff = s as f64 - avg_size;
                diff * diff
            })
            .sum::<f64>()
            / sizes.len() as f64;

        // Normalize fragmentation score between 0 and 1
        (variance.sqrt() / avg_size).min(1.0)
    }

    /// Check memory health and return warnings if any
    #[allow(dead_code)]
    pub async fn health_check(&self) -> Vec<String> {
        let mut warnings = Vec::new();
        let (current, max) = self.get_usage_stats().await;

        let usage_percent = (current as f64 / max as f64) * 100.0;

        if usage_percent > 90.0 {
            warnings.push(format!("Critical: Memory usage at {:.1}%", usage_percent));
        } else if usage_percent > 75.0 {
            warnings.push(format!("Warning: Memory usage at {:.1}%", usage_percent));
        }

        let pools = self.pools.read().await;
        let expired_count = pools
            .values()
            .filter(|s| {
                s.ttl_seconds == 0
                    || (s.ttl_seconds > 0 && s.created_at.elapsed().as_secs() > s.ttl_seconds)
            })
            .count();

        if expired_count > 10 {
            warnings.push(format!(
                "Warning: {} expired entries not cleaned up",
                expired_count
            ));
        }

        warnings
    }
}

impl Drop for MemoryPool {
    fn drop(&mut self) {
        // Signal shutdown to sweeper if it's running
        if let Some(tx) = self.shutdown_tx.take() {
            let _ = tx.send(()); // Ignore error if already shut down
        }

        // Wait for sweeper task with timeout
        if let Some(handle) = self.sweeper_handle.take() {
            // We need to use block_in_place or spawn_blocking since we're in a sync context
            // But since this is Drop, we can't use async, so we'll just detach the task
            // and let tokio runtime handle cleanup
            handle.abort(); // Abort the task instead of waiting
        }
    }
}

#[derive(Debug, Clone)]
pub struct MemoryStats {
    pub current_size: usize,
    pub max_size: usize,
    pub high_water_mark: usize,
    pub allocation_count: u64,
    pub entry_count: usize,
    pub fragmentation: f64,
}

// ---------------------------------------------------------------------------------
// Lightweight memory manager used by FSM (wrapper around MemoryPool + in-memory log)

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryEntry {
    pub id: String,
    pub query: String,
    pub response: String,
    pub metadata: HashMap<String, serde_json::Value>,
    pub timestamp: DateTime<Utc>,
}

#[derive(Debug)]
pub struct MemoryManager {
    pool: MemoryPool,
    entries: Arc<RwLock<Vec<MemoryEntry>>>,
    capacity: usize,
}

impl MemoryManager {
    pub fn new(capacity: usize) -> Self {
        Self {
            pool: MemoryPool::new(64), // 64MB pool by default
            entries: Arc::new(RwLock::new(Vec::with_capacity(capacity.min(4096)))),
            capacity,
        }
    }

    pub async fn store(&self, entry: MemoryEntry) {
        // Keep a rolling buffer up to capacity
        let mut guard = self.entries.write().await;
        if guard.len() >= self.capacity {
            guard.remove(0);
        }
        // Also cache response bytes in pool (best-effort, 1h TTL)
        let _ = self
            .pool
            .allocate(
                format!("memory:{}", entry.id),
                Bytes::from(entry.response.clone()),
                3600,
            )
            .await;
        guard.push(entry);
    }

    pub async fn search(&self, query: &str, limit: usize) -> Vec<MemoryEntry> {
        let q = query.to_lowercase();
        let guard = self.entries.read().await;
        let mut out = Vec::new();
        for e in guard.iter().rev() {
            // most recent first
            if e.query.to_lowercase().contains(&q) || e.response.to_lowercase().contains(&q) {
                out.push(e.clone());
                if out.len() >= limit {
                    break;
                }
            }
        }
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_basic_allocation() {
        let pool = MemoryPool::new(1); // 1MB
        let data = Bytes::from("test data");

        assert!(pool
            .allocate("key1".to_string(), data.clone(), 60)
            .await
            .is_ok());

        let retrieved = pool.retrieve("key1").await;
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap(), data);
    }

    #[tokio::test]
    async fn test_ttl_expiration() {
        let pool = MemoryPool::new(1);
        let data = Bytes::from("test data");

        pool.allocate("key1".to_string(), data, 0).await.unwrap(); // Instant expiry

        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        let retrieved = pool.retrieve("key1").await;
        assert!(retrieved.is_none());
    }

    #[tokio::test]
    async fn test_memory_limit() {
        let pool = MemoryPool::new(1); // 1MB
        let large_data = Bytes::from(vec![0u8; 1024 * 1024 + 1]); // Just over 1MB

        let result = pool.allocate("key1".to_string(), large_data, 60).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_lru_eviction() {
        let pool = MemoryPool::new(1); // 1MB

        // Allocate half the pool
        let data1 = Bytes::from(vec![1u8; 500 * 1024]);
        pool.allocate("key1".to_string(), data1, 60).await.unwrap();

        // Allocate another half
        let data2 = Bytes::from(vec![2u8; 500 * 1024]);
        pool.allocate("key2".to_string(), data2, 60).await.unwrap();

        // Access key2 to make it more recently used
        pool.retrieve("key2").await;

        // Try to allocate more - should evict key1
        let data3 = Bytes::from(vec![3u8; 500 * 1024]);
        pool.allocate("key3".to_string(), data3, 60).await.unwrap();

        // key1 should be evicted, key2 and key3 should exist
        assert!(pool.retrieve("key1").await.is_none());
        assert!(pool.retrieve("key2").await.is_some());
        assert!(pool.retrieve("key3").await.is_some());
    }
}
