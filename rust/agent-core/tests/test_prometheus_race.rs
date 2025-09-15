// Test to identify the exact Prometheus error in concurrent initialization
use prometheus::register_counter_vec;
use std::thread;

#[test]
fn test_prometheus_global_registry_race() {
    // This test will reveal the actual Prometheus error
    let mut handles = vec![];

    for i in 0..5 {
        let handle = thread::spawn(move || {
            // Try to register the same metric multiple times
            match register_counter_vec!("test_metric", "Test metric for race condition", &["label"])
            {
                Ok(_) => println!("Thread {} registered metric successfully", i),
                Err(e) => println!("Thread {} got error: {}", i, e),
            }
        });
        handles.push(handle);
    }

    for handle in handles {
        handle.join().unwrap();
    }
}
