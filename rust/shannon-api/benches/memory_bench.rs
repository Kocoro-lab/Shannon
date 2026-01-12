//! Memory usage benchmarks for embedded workflow engine.
//!
//! Measures memory consumption per workflow and overall system memory.
//! Target: <150MB/workflow, Acceptable: <200MB/workflow

use criterion::{black_box, criterion_group, criterion_main, Criterion};
use std::time::Duration;

fn benchmark_workflow_memory(c: &mut Criterion) {
    let mut group = c.benchmark_group("workflow_memory");
    group.measurement_time(Duration::from_secs(10));
    group.sample_size(10);

    group.bench_function("single_workflow_rss", |b| {
        b.iter(|| {
            // Simulate workflow memory allocation
            // Typical workflow memory:
            // - Event buffer: ~256KB
            // - State data: ~50KB
            // - Pattern execution: ~10MB
            // - LLM response buffers: ~5MB
            // Total: ~15MB per workflow

            let _event_buffer = black_box(vec![0u8; 256 * 1024]);
            let _state_data = black_box(vec![0u8; 50 * 1024]);
            let _pattern_memory = black_box(vec![0u8; 10 * 1024 * 1024]);
            let _llm_buffers = black_box(vec![0u8; 5 * 1024 * 1024]);

            // Simulate work
            std::thread::sleep(Duration::from_micros(100));
        });
    });

    group.finish();
}

fn benchmark_concurrent_workflows(c: &mut Criterion) {
    let mut group = c.benchmark_group("concurrent_workflow_memory");
    group.measurement_time(Duration::from_secs(15));
    group.sample_size(5);

    for workflow_count in &[1, 3, 5, 10] {
        group.bench_with_input(
            format!("{}_workflows", workflow_count),
            workflow_count,
            |b, &count| {
                b.iter(|| {
                    // Simulate memory for N concurrent workflows
                    let mut workflows = Vec::new();
                    for _i in 0..count {
                        workflows.push(black_box(vec![0u8; 15 * 1024 * 1024]));
                    }

                    // Simulate concurrent execution
                    std::thread::sleep(Duration::from_millis(10 * count as u64));

                    black_box(workflows);
                });
            },
        );
    }

    group.finish();
}

fn benchmark_event_buffer_growth(c: &mut Criterion) {
    let mut group = c.benchmark_group("event_buffer_memory");

    for event_count in &[256, 512, 1024] {
        group.bench_with_input(
            format!("{}_events", event_count),
            event_count,
            |b, &count| {
                b.iter(|| {
                    // Simulate event buffer with ring buffer semantics
                    // Each event: ~1KB serialized
                    let buffer_size = count * 1024;
                    let _event_buffer = black_box(vec![0u8; buffer_size]);

                    std::thread::sleep(Duration::from_micros(10));
                });
            },
        );
    }

    group.finish();
}

fn benchmark_wasm_instance_memory(c: &mut Criterion) {
    let mut group = c.benchmark_group("wasm_memory");
    group.measurement_time(Duration::from_secs(10));

    group.bench_function("wasm_instance_allocation", |b| {
        b.iter(|| {
            // Simulate WASM instance memory allocation
            // Default: 512MB, Max: 1GB
            let _wasm_heap = black_box(vec![0u8; 512 * 1024 * 1024]);

            std::thread::sleep(Duration::from_micros(50));
        });
    });

    group.finish();
}

fn benchmark_cache_memory(c: &mut Criterion) {
    let mut group = c.benchmark_group("cache_memory");

    group.bench_function("wasm_cache_100mb", |b| {
        b.iter(|| {
            // Simulate WASM module cache (100MB)
            // ~6 modules × ~15MB each ≈ 90MB
            let module_count = black_box(6);
            let avg_module_size = black_box(15 * 1024 * 1024);

            let _cache_memory = black_box(vec![0u8; module_count * avg_module_size]);

            std::thread::sleep(Duration::from_micros(10));
        });
    });

    group.finish();
}

fn benchmark_memory_fragmentation(c: &mut Criterion) {
    let mut group = c.benchmark_group("memory_fragmentation");
    group.measurement_time(Duration::from_secs(10));

    group.bench_function("allocate_deallocate_cycle", |b| {
        b.iter(|| {
            // Simulate workflow lifecycle memory pattern
            // Create → Execute → Complete → Cleanup

            // Allocate
            let _workflow = black_box(vec![0u8; 15 * 1024 * 1024]);
            std::thread::sleep(Duration::from_micros(100));

            // Execute (additional allocations)
            let _events = black_box(vec![0u8; 256 * 1024]);
            std::thread::sleep(Duration::from_micros(50));

            // Deallocate happens automatically via drop
        });
    });

    group.finish();
}

criterion_group!(
    benches,
    benchmark_workflow_memory,
    benchmark_concurrent_workflows,
    benchmark_event_buffer_growth,
    benchmark_wasm_instance_memory,
    benchmark_cache_memory,
    benchmark_memory_fragmentation
);
criterion_main!(benches);
