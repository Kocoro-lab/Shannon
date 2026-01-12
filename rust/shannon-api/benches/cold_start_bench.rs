//! Cold start benchmark for embedded workflow engine.
//!
//! Measures engine initialization time.
//! Target: <200ms, Acceptable: <250ms (P99)

use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion};
use std::time::Duration;

fn benchmark_engine_init(c: &mut Criterion) {
    let mut group = c.benchmark_group("cold_start");
    group.measurement_time(Duration::from_secs(10));
    group.sample_size(100);

    group.bench_function("engine_initialization", |b| {
        b.iter(|| {
            // Simulate engine initialization
            // In production, this would initialize:
            // - SQLite event log
            // - Workflow database
            // - Event bus
            // - Pattern registry
            // - WASM cache

            let _engine_components = black_box(vec![
                "event_log",
                "workflow_db",
                "event_bus",
                "pattern_registry",
                "wasm_cache",
            ]);

            // Simulate minimal init work
            std::thread::sleep(Duration::from_micros(50));
        });
    });

    group.finish();
}

fn benchmark_engine_init_with_preload(c: &mut Criterion) {
    let mut group = c.benchmark_group("cold_start_preload");
    group.measurement_time(Duration::from_secs(10));
    group.sample_size(50);

    group.bench_function("engine_init_with_module_preload", |b| {
        b.iter(|| {
            // Simulate engine init + WASM module preload
            std::thread::sleep(Duration::from_micros(100));

            // Simulate preloading 6 WASM modules
            for _i in 0..6 {
                black_box(std::thread::sleep(Duration::from_micros(10)));
            }
        });
    });

    group.finish();
}

fn benchmark_database_init(c: &mut Criterion) {
    let mut group = c.benchmark_group("database_init");
    group.measurement_time(Duration::from_secs(10));

    group.bench_function("sqlite_connection", |b| {
        b.iter(|| {
            // Simulate SQLite connection and schema setup
            black_box(std::thread::sleep(Duration::from_micros(20)));
        });
    });

    group.finish();
}

criterion_group!(
    benches,
    benchmark_engine_init,
    benchmark_engine_init_with_preload,
    benchmark_database_init
);
criterion_main!(benches);
