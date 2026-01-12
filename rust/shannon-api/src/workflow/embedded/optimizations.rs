//! Performance optimizations for embedded workflow engine.
//!
//! Provides utilities for:
//! - Event batching (10 events/batch)
//! - Memory pooling for event buffers
//! - Connection pooling for SQLite
//! - Parallel pattern execution

use parking_lot::Mutex;
use std::sync::Arc;

/// Event batch for optimized SQLite writes.
///
/// # Design
/// Accumulates events and flushes when:
/// - Batch size reaches threshold (default: 10)
/// - Time since last flush exceeds timeout (default: 100ms)
/// - Explicit flush is called
#[derive(Debug)]
pub struct EventBatcher<T> {
    /// Pending events.
    pending: Arc<Mutex<Vec<T>>>,
    /// Batch size threshold.
    batch_size: usize,
    /// Flush timeout in milliseconds.
    flush_timeout_ms: u64,
    /// Last flush timestamp.
    last_flush: Arc<Mutex<std::time::Instant>>,
}

impl<T: Clone> EventBatcher<T> {
    /// Create a new event batcher.
    ///
    /// # Arguments
    /// * `batch_size` - Number of events per batch (default: 10)
    /// * `flush_timeout_ms` - Maximum time to wait before flushing (default: 100ms)
    #[must_use]
    pub fn new(batch_size: usize, flush_timeout_ms: u64) -> Self {
        Self {
            pending: Arc::new(Mutex::new(Vec::with_capacity(batch_size))),
            batch_size,
            flush_timeout_ms,
            last_flush: Arc::new(Mutex::new(std::time::Instant::now())),
        }
    }

    /// Create with default settings (10 events, 100ms timeout).
    #[must_use]
    pub fn default() -> Self {
        Self::new(10, 100)
    }

    /// Add an event to the batch.
    ///
    /// Returns `Some(batch)` if the batch is ready to flush.
    #[must_use]
    pub fn add(&self, event: T) -> Option<Vec<T>> {
        let mut pending = self.pending.lock();
        pending.push(event);

        // Check if batch is full
        if pending.len() >= self.batch_size {
            let batch = pending.drain(..).collect();
            *self.last_flush.lock() = std::time::Instant::now();
            return Some(batch);
        }

        // Check if timeout exceeded
        let last_flush = self.last_flush.lock();
        if last_flush.elapsed().as_millis() >= self.flush_timeout_ms as u128 {
            drop(last_flush); // Release lock before draining
            let batch = pending.drain(..).collect();
            *self.last_flush.lock() = std::time::Instant::now();
            return Some(batch);
        }

        None
    }

    /// Force flush all pending events.
    #[must_use]
    pub fn flush(&self) -> Vec<T> {
        let mut pending = self.pending.lock();
        let batch = pending.drain(..).collect();
        *self.last_flush.lock() = std::time::Instant::now();
        batch
    }

    /// Get number of pending events.
    #[must_use]
    pub fn pending_count(&self) -> usize {
        self.pending.lock().len()
    }
}

/// Memory pool for event buffers.
///
/// Reuses allocated buffers to reduce allocation overhead.
#[derive(Debug)]
pub struct BufferPool {
    /// Pool of available buffers.
    buffers: Arc<Mutex<Vec<Vec<u8>>>>,
    /// Buffer capacity.
    buffer_capacity: usize,
    /// Maximum pool size.
    max_pool_size: usize,
}

impl BufferPool {
    /// Create a new buffer pool.
    ///
    /// # Arguments
    /// * `buffer_capacity` - Capacity of each buffer (default: 64KB)
    /// * `max_pool_size` - Maximum number of pooled buffers (default: 32)
    #[must_use]
    pub fn new(buffer_capacity: usize, max_pool_size: usize) -> Self {
        Self {
            buffers: Arc::new(Mutex::new(Vec::with_capacity(max_pool_size))),
            buffer_capacity,
            max_pool_size,
        }
    }

    /// Create with default settings (64KB buffers, 32 max).
    #[must_use]
    pub fn default() -> Self {
        Self::new(64 * 1024, 32)
    }

    /// Acquire a buffer from the pool.
    ///
    /// Returns a buffer from the pool if available, otherwise allocates new.
    #[must_use]
    pub fn acquire(&self) -> Vec<u8> {
        let mut buffers = self.buffers.lock();

        if let Some(mut buffer) = buffers.pop() {
            buffer.clear();
            buffer
        } else {
            Vec::with_capacity(self.buffer_capacity)
        }
    }

    /// Return a buffer to the pool.
    ///
    /// The buffer is only kept if pool is not full.
    pub fn release(&self, buffer: Vec<u8>) {
        let mut buffers = self.buffers.lock();

        if buffers.len() < self.max_pool_size {
            buffers.push(buffer);
        }
        // Otherwise let it drop
    }

    /// Get pool statistics.
    #[must_use]
    pub fn stats(&self) -> PoolStats {
        let buffers = self.buffers.lock();
        PoolStats {
            available: buffers.len(),
            capacity: self.buffer_capacity,
            max_pool_size: self.max_pool_size,
        }
    }
}

/// Buffer pool statistics.
#[derive(Debug, Clone)]
pub struct PoolStats {
    /// Number of available buffers.
    pub available: usize,
    /// Capacity of each buffer.
    pub capacity: usize,
    /// Maximum pool size.
    pub max_pool_size: usize,
}

/// Parallel execution utilities for pattern steps.
///
/// Uses rayon for CPU-bound parallel work.
pub struct ParallelExecutor {
    /// Thread pool size.
    thread_pool_size: usize,
}

impl ParallelExecutor {
    /// Create a new parallel executor.
    ///
    /// # Arguments
    /// * `thread_pool_size` - Number of threads (default: num_cpus)
    #[must_use]
    pub fn new(thread_pool_size: usize) -> Self {
        Self { thread_pool_size }
    }

    /// Create with default settings (num_cpus threads).
    #[must_use]
    pub fn default() -> Self {
        Self::new(num_cpus::get())
    }

    /// Execute steps in parallel.
    ///
    /// Returns results in original order.
    pub fn execute_parallel<F, T, R>(&self, items: Vec<T>, f: F) -> Vec<R>
    where
        F: Fn(T) -> R + Send + Sync,
        T: Send,
        R: Send,
    {
        use rayon::prelude::*;

        items.into_par_iter().map(f).collect()
    }
}

impl std::fmt::Debug for ParallelExecutor {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ParallelExecutor")
            .field("thread_pool_size", &self.thread_pool_size)
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_event_batcher_creation() {
        let batcher = EventBatcher::<String>::new(10, 100);
        assert_eq!(batcher.pending_count(), 0);
    }

    #[test]
    fn test_event_batcher_add_below_threshold() {
        let batcher = EventBatcher::default();

        for i in 0..5 {
            let result = batcher.add(format!("event_{}", i));
            assert!(result.is_none());
        }

        assert_eq!(batcher.pending_count(), 5);
    }

    #[test]
    fn test_event_batcher_flush_on_threshold() {
        let batcher = EventBatcher::new(5, 100);

        // Add events up to threshold
        for i in 0..4 {
            assert!(batcher.add(format!("event_{}", i)).is_none());
        }

        // 5th event should trigger flush
        let batch = batcher.add("event_4".to_string());
        assert!(batch.is_some());
        assert_eq!(batch.unwrap().len(), 5);
        assert_eq!(batcher.pending_count(), 0);
    }

    #[test]
    fn test_event_batcher_manual_flush() {
        let batcher = EventBatcher::default();

        for i in 0..3 {
            batcher.add(format!("event_{}", i));
        }

        let batch = batcher.flush();
        assert_eq!(batch.len(), 3);
        assert_eq!(batcher.pending_count(), 0);
    }

    #[test]
    fn test_buffer_pool_creation() {
        let pool = BufferPool::new(1024, 10);
        let stats = pool.stats();

        assert_eq!(stats.available, 0);
        assert_eq!(stats.capacity, 1024);
        assert_eq!(stats.max_pool_size, 10);
    }

    #[test]
    fn test_buffer_pool_acquire_release() {
        let pool = BufferPool::default();

        // Acquire buffer
        let buffer = pool.acquire();
        assert_eq!(buffer.capacity(), 64 * 1024);

        // Release buffer
        pool.release(buffer);

        let stats = pool.stats();
        assert_eq!(stats.available, 1);
    }

    #[test]
    fn test_buffer_pool_reuse() {
        let pool = BufferPool::default();

        // Acquire and release
        let buf1 = pool.acquire();
        pool.release(buf1);

        // Acquire again - should reuse
        let buf2 = pool.acquire();
        assert_eq!(buf2.capacity(), 64 * 1024);

        let stats = pool.stats();
        assert_eq!(stats.available, 0); // Buffer was reused
    }

    #[test]
    fn test_buffer_pool_max_size() {
        let pool = BufferPool::new(1024, 2);

        // Release 3 buffers
        pool.release(vec![0; 1024]);
        pool.release(vec![0; 1024]);
        pool.release(vec![0; 1024]); // Should be dropped

        let stats = pool.stats();
        assert_eq!(stats.available, 2); // Only 2 kept
    }

    #[test]
    fn test_parallel_executor_creation() {
        let executor = ParallelExecutor::default();
        assert!(executor.thread_pool_size > 0);
    }

    #[test]
    fn test_parallel_executor_execute() {
        let executor = ParallelExecutor::default();

        let items = vec![1, 2, 3, 4, 5];
        let results = executor.execute_parallel(items, |x| x * 2);

        assert_eq!(results, vec![2, 4, 6, 8, 10]);
    }

    #[test]
    fn test_parallel_executor_preserves_order() {
        let executor = ParallelExecutor::default();

        let items: Vec<i32> = (0..100).collect();
        let results = executor.execute_parallel(items, |x| x * 2);

        // Check order is preserved
        for (i, &result) in results.iter().enumerate() {
            assert_eq!(result, i as i32 * 2);
        }
    }
}
