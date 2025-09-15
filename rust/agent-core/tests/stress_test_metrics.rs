// Stress test for OnceLock-based metrics initialization
// Tests for race conditions and concurrent initialization

use shannon_agent_core::metrics;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

#[test]
fn test_concurrent_metrics_initialization() {
    // Test that multiple threads can safely initialize metrics
    let init_count = Arc::new(AtomicUsize::new(0));
    let error_count = Arc::new(AtomicUsize::new(0));

    let mut handles = vec![];

    for i in 0..10 {
        let init_count = init_count.clone();
        let error_count = error_count.clone();

        let handle = thread::spawn(move || {
            // Each thread tries to initialize metrics
            match metrics::init_metrics() {
                Ok(_) => {
                    init_count.fetch_add(1, Ordering::SeqCst);
                    println!("Thread {} initialized metrics (idempotent OK)", i);
                }
                Err(e) => {
                    // No error is expected in idempotent init
                    error_count.fetch_add(1, Ordering::SeqCst);
                    eprintln!("Thread {} unexpected init error: {}", i, e);
                }
            }

            // Try to use metrics after initialization
            let timer = metrics::TaskTimer::new("test");
            thread::sleep(Duration::from_millis(10));
            timer.complete("success", Some(100));
        });

        handles.push(handle);
    }

    // Wait for all threads
    for handle in handles {
        handle.join().unwrap();
    }

    // All threads should see Ok(()) due to idempotent initialization
    assert_eq!(
        init_count.load(Ordering::SeqCst),
        10,
        "All threads should observe successful initialization"
    );
    assert_eq!(
        error_count.load(Ordering::SeqCst),
        0,
        "No errors should occur during initialization"
    );

    // Verify metrics are accessible
    let metrics_output = metrics::get_metrics();
    assert!(
        !metrics_output.is_empty(),
        "Metrics should be available after initialization"
    );
}

#[test]
fn test_metrics_usage_without_initialization() {
    // Test that metrics can be safely accessed even if not initialized
    // This simulates a scenario where metrics initialization fails

    // Try to use a timer without initialization
    let timer = metrics::TaskTimer::new("test_mode");
    timer.complete("success", Some(42));

    // This should not panic, just silently skip recording
    let metrics_output = metrics::get_metrics();
    // Output might be empty or contain some metrics depending on test order
    println!("Metrics output length: {}", metrics_output.len());
}

#[test]
fn test_memory_pool_concurrent_access() {
    use bytes::Bytes;
    use shannon_agent_core::memory::MemoryPool;

    // Create a shared memory pool
    let pool = Arc::new(MemoryPool::new(10)); // 10MB pool

    let mut handles = vec![];

    // Spawn multiple threads that allocate and deallocate memory
    for i in 0..20 {
        let pool = pool.clone();

        let handle = thread::spawn(move || {
            // Use tokio runtime for async operations
            let rt = tokio::runtime::Runtime::new().unwrap();

            rt.block_on(async {
                let key = format!("thread_{}", i);
                let data = Bytes::from(vec![i as u8; 100_000]); // 100KB per thread

                // Try to allocate
                match pool.allocate(key.clone(), data.clone(), 60).await {
                    Ok(_) => {
                        println!("Thread {} allocated 100KB", i);

                        // Retrieve the data
                        if let Some(retrieved) = pool.retrieve(&key).await {
                            assert_eq!(retrieved, data, "Retrieved data should match");
                        }

                        // Deallocate
                        let _ = pool.deallocate(&key).await;
                        println!("Thread {} deallocated", i);
                    }
                    Err(e) => {
                        // Expected for some threads due to memory limit
                        println!("Thread {} allocation failed (expected): {}", i, e);
                    }
                }
            });
        });

        handles.push(handle);
    }

    // Wait for all threads
    for handle in handles {
        handle.join().unwrap();
    }

    // Check final pool state
    let rt = tokio::runtime::Runtime::new().unwrap();
    rt.block_on(async {
        let (used, max) = pool.get_usage_stats().await;
        println!("Final pool state: {}/{} bytes used", used, max);
        assert!(used <= max, "Used memory should not exceed max");
    });
}

#[test]
fn test_oncelock_double_initialization() {
    // Test that OnceLock properly prevents double initialization
    use std::sync::OnceLock;

    static TEST_METRIC: OnceLock<String> = OnceLock::new();

    // First initialization should succeed
    let result1 = TEST_METRIC.set("first".to_string());
    assert!(result1.is_ok(), "First set should succeed");

    // Second initialization should fail
    let result2 = TEST_METRIC.set("second".to_string());
    assert!(result2.is_err(), "Second set should fail");

    // Value should be the first one
    assert_eq!(TEST_METRIC.get().unwrap(), "first");
}
