# Workflow Debugging Guide

**Version**: 1.0  
**Audience**: Developers

## Overview

The embedded workflow engine includes powerful debugging tools for investigating failures, understanding execution, and optimizing performance.

## Time-Travel Debugging

### Export Workflow for Debugging

```bash
# Using CLI tool
cargo run --bin workflow_replay -- \
  --history workflow.json \
  --mode full

# Or from TypeScript
import { workflowAPI } from '@/lib/shannon/workflows';
const json = await workflowAPI.exportWorkflow('wf-123');
```

### Replay with Breakpoints

```bash
# Step-through mode
cargo run --bin workflow_replay -- \
  --history workflow.json \
  --mode step

# Breakpoint at specific event
cargo run --bin workflow_replay -- \
  --history workflow.json \
  --mode breakpoint \
  --breakpoint 10

# Inspect state at sequence
cargo run --bin workflow_replay -- \
  --history workflow.json \
  --inspect 15

# Verify determinism
cargo run --bin workflow_replay -- \
  --history workflow.json \
  --verify
```

### Breakpoint Examples

```rust
use shannon_api::workflow::embedded::ReplayManager;

let replay = ReplayManager::new(store, event_log_path).await?;

// Add breakpoint at sequence 10
replay.add_breakpoint(10, None);

// Add breakpoint for specific event type
replay.add_breakpoint(20, Some("WORKFLOW_COMPLETED".to_string()));

// Set step-through mode
replay.set_replay_mode(ReplayMode::StepThrough);

// Replay workflow
let result = replay.replay_from_file("workflow.json").await?;
```

## Logging and Observability

### Enable Debug Logging

```bash
# In terminal
export RUST_LOG=debug
npm run tauri:dev

# Or in code
RUST_LOG=shannon_api::workflow=debug cargo run
```

### Key Log Messages

```
INFO  shannon_api::workflow: Workflow started workflow_id=wf-123
DEBUG shannon_api::workflow: Pattern selected pattern=chain_of_thought complexity=0.4
INFO  durable_shannon::worker: Checkpoint created sequence=10
WARN  shannon_api::workflow: Circuit breaker opening failures=5
ERROR shannon_api::workflow: Workflow failed error="LLM timeout"
```

### Structured Logging

All workflow operations emit structured logs:

```rust
tracing::info!(
    workflow_id = %workflow_id,
    pattern = %pattern_type,
    progress = progress_percent,
    "Workflow progress"
);
```

## Common Issues

### Workflow Stuck

**Symptoms**: Progress stops, no events emitted

**Debugging Steps**:
1. Check circuit breaker state
2. Review last event in SQLite
3. Check LLM API rate limits
4. Verify network connectivity

```bash
# Check last event
sqlite3 shannon.db "SELECT * FROM workflow_events WHERE workflow_id='wf-123' ORDER BY sequence DESC LIMIT 1"

# Check circuit breaker
# (exposed in engine health endpoint)
```

### Checkpoint Corruption

**Symptoms**: Recovery fails with checksum error

**Debugging Steps**:
1. Engine automatically falls back to full replay
2. Check disk space
3. Verify zstd installation
4. Review checkpoint creation logs

```bash
# Verify checkpoints
sqlite3 shannon.db "SELECT workflow_id, sequence, length(state_data), checksum FROM workflow_checkpoints"
```

### Events Not Streaming

**Embedded Mode**:
- Verify Tauri event listener registered
- Check event channel capacity (256 default)
- Look for backpressure warnings

**Cloud Mode**:
- Verify SSE connection established
- Check network connectivity
- Review keep-alive heartbeats (every 15s)

### High Memory Usage

**Symptoms**: Memory >200MB per workflow

**Debugging Steps**:
1. Check event buffer size (default 256)
2. Review checkpoint frequency
3. Verify WASM memory limits (512MB default)
4. Check for event accumulation

```rust
// Check engine health
let health = engine.health().await?;
println!("Active workflows: {}", health.active_workflows);
println!("Memory usage: {}MB", health.memory_mb);
```

## Performance Debugging

### Benchmarking

```bash
# Run all benchmarks
cargo bench

# Specific benchmark
cargo bench --bench cold_start_bench
cargo bench --bench task_latency_bench
```

### Profiling

```bash
# Install profiling tools
cargo install cargo-flamegraph

# Profile workflow execution
cargo flamegraph --bin shannon-api

# Memory profiling
valgrind --tool=massif target/debug/shannon-api
```

### Event Throughput

Check event log write performance:

```bash
# Count events per workflow
sqlite3 shannon.db "SELECT workflow_id, COUNT(*) as event_count FROM workflow_events GROUP BY workflow_id"

# Check event rate
sqlite3 shannon.db "SELECT COUNT(*) / ((MAX(timestamp) - MIN(timestamp)) / 1000.0) as events_per_sec FROM workflow_events WHERE workflow_id='wf-123'"
```

## Error Recovery Debugging

### Circuit Breaker State

```rust
let recovery = RecoveryManager::new(store, event_log_path).await?;
let state = recovery.circuit_breaker_state();

match state {
    CircuitBreakerState::Closed => println!("Normal operation"),
    CircuitBreakerState::Open => println!("Failing fast"),
    CircuitBreakerState::HalfOpen => println!("Testing recovery"),
}
```

### Retry History

Review retry attempts in logs:

```
INFO Recovery attempt 1/3 delay=1s
WARN Operation failed error=Network attempt=1
INFO Recovery attempt 2/3 delay=2s
INFO Operation succeeded after retry attempt=2
```

## Deterministic Replay

### Verify Replay Determinism

```bash
# Export workflow
cargo run --bin workflow_replay -- \
  --history wf-123.json \
  --verify
```

Expected output:
```
✓ Replay is deterministic
  First run:  42 events, status=Completed
  Second run: 42 events, status=Completed
```

### Non-Deterministic Issues

If replay is not deterministic:

1. Check for system randomness (use seeded RNG)
2. Verify timestamps are from events, not system
3. Review external API calls (mock for replay)
4. Check concurrent execution order

## Development Tips

### Quick Iteration

```bash
# Watch mode
cargo watch -x 'test --lib workflow'

# Fast compile
cargo build --profile dev

# Skip slow tests
cargo test --lib -- --skip slow
```

### Test-Driven Debugging

1. Export failing workflow
2. Create test case with exported data
3. Fix issue
4. Verify with replay
5. Add regression test

### Useful SQL Queries

```sql
-- Recent workflows
SELECT workflow_id, status, created_at 
FROM workflows 
ORDER BY created_at DESC 
LIMIT 10;

-- Failed workflows
SELECT workflow_id, status, output 
FROM workflows 
WHERE status = 'Failed';

-- Event count by type
SELECT event_type, COUNT(*) 
FROM workflow_events 
GROUP BY event_type 
ORDER BY COUNT(*) DESC;

-- Checkpoint stats
SELECT 
  workflow_id,
  sequence,
  length(state_data) as size_bytes,
  checksum
FROM workflow_checkpoints;
```

## References

- [Main Documentation](embedded-workflow-engine.md)
- [Pattern Guide](workflow-patterns.md)
- [Configuration](workflow-configuration.md)
- [Rust Architecture](rust-architecture.md)
