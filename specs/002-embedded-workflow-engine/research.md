# Embedded Workflow Engine - Research Decisions

**Feature ID**: 002-embedded-workflow-engine  
**Version**: 1.0

---

## Decisions

### Decision 1: Use Durable Workflow Pattern with Event Sourcing

**Decision**: Implement embedded workflows using event sourcing with an append-only event log, enabling deterministic replay and crash recovery.

**Rationale**:
- **Crash Recovery**: Workflows can resume from last checkpoint after app crash
- **Deterministic Replay**: Same input events always produce same output state
- **Debuggability**: Full history available for time-travel debugging
- **Auditability**: Complete trace of workflow execution
- **Cloud Parity**: Matches Temporal's durability guarantees

**Alternatives Considered**:
1. **Stateless workflows** (rejected - cannot resume after crash)
2. **Snapshot-only checkpoints** (rejected - requires full state serialization, high memory)
3. **Database-backed state machine** (rejected - not deterministic, hard to debug)

**Implementation**:
- SQLite event log with WAL mode
- Bincode event serialization
- Checkpoint every 10 events or 5 minutes
- Event retention: 7 days for completed workflows

**Trade-offs**:
- ✅ Excellent crash recovery
- ✅ Complete audit trail
- ✅ Time-travel debugging possible
- ❌ Storage overhead (~100KB per workflow)
- ❌ Replay latency on recovery (~1s per 1000 events)

---

### Decision 2: SQLite for Local Persistence

**Decision**: Use SQLite as the embedded database for all workflow state, events, and sessions.

**Rationale**:
- **Self-Contained**: No external database service required
- **Cross-Platform**: Works on macOS, Windows, Linux, iOS, Android
- **Performance**: WAL mode enables concurrent reads during writes
- **Reliability**: ACID transactions, battle-tested
- **Zero Configuration**: Works out-of-the-box

**Alternatives Considered**:
1. **PostgreSQL** (rejected - requires external service, not portable)
2. **RocksDB** (rejected - LSM tree complexity, no SQL)
3. **In-Memory Only** (rejected - no persistence, data loss on crash)
4. **SurrealDB** (considered for future, but SQLite simpler for MVP)

**Implementation**:
- SQLite 3.35+ (for JSON functions)
- WAL mode (Write-Ahead Logging) for concurrency
- Page size: 4KB
- Connection pooling with r2d2
- Foreign keys enabled

**Trade-offs**:
- ✅ Zero external dependencies
- ✅ Simple deployment
- ✅ Good performance (<10ms queries)
- ❌ Limited concurrency (1 writer at a time)
- ❌ No horizontal scaling (single device)

---

### Decision 3: WASM for Workflow Modules

**Decision**: Compile cognitive patterns to WebAssembly modules for sandboxed execution using Wasmtime v40.

**Rationale**:
- **Security**: WASI capability-based sandboxing prevents malicious code
- **Isolation**: Memory-safe execution with resource limits
- **Portability**: Same WASM modules work across platforms
- **Performance**: Near-native speed after JIT compilation
- **Determinism**: Reproducible execution for replay

**Alternatives Considered**:
1. **Native Rust patterns** (used for MVP, WASM for v1.1+)
2. **Python sandboxing** (rejected - harder to sandbox, performance overhead)
3. **Docker containers** (rejected - too heavy for embedded use)

**Implementation**:
- Wasmtime v40 runtime
- WASI Preview 1 interface
- Fuel-based CPU limits (10M instructions)
- Memory limits (512MB default, 1GB max)
- Network allow-list per pattern

**Trade-offs**:
- ✅ Strong security isolation
- ✅ Deterministic execution
- ✅ Cross-platform compatibility
- ❌ Compilation complexity (requires wasm32-wasi target)
- ❌ ~10-20% performance overhead vs native
- ❌ Limited WASI Preview 1 features (no async I/O)

---

### Decision 4: Tokio Broadcast Channels for Event Streaming

**Decision**: Use Tokio's broadcast channels for real-time event distribution from workflows to UI subscribers.

**Rationale**:
- **Multi-Subscriber**: Multiple UI connections can subscribe to same workflow
- **Backpressure Handling**: Lagging subscribers don't block event emission
- **Memory Efficient**: Fixed-size ring buffer (256 events)
- **Native Tokio Integration**: Works seamlessly with async runtime

**Alternatives Considered**:
1. **Redis Pub/Sub** (rejected - requires external service, overkill for embedded)
2. **tokio::sync::watch** (rejected - only holds latest value, loses history)
3. **flume channels** (rejected - no broadcast support)
4. **Custom event bus** (considered, but tokio::broadcast sufficient)

**Implementation**:
```rust
pub struct EventBus {
    channels: Mutex<HashMap<String, broadcast::Sender<WorkflowEvent>>>,
    capacity: usize,  // 256 events per workflow
}
```

**Trade-offs**:
- ✅ Simple API
- ✅ Memory efficient (fixed buffer)
- ✅ Handles slow consumers gracefully
- ❌ Slow consumers may miss events (intentional design)
- ❌ Events not persisted in channel (must use event log)

---

### Decision 5: Native Patterns First, WASM Later

**Decision**: Implement patterns as native Rust code initially (Phase 1), compile to WASM in Phase 2.

**Rationale**:
- **Faster MVP**: Native implementation faster to develop and debug
- **Incremental Complexity**: Add WASM after core functionality proven
- **Same Interface**: CognitivePattern trait works for both native and WASM
- **Migration Path**: Can run hybrid (native + WASM) during transition

**Alternatives Considered**:
1. **WASM from Day 1** (rejected - adds unnecessary complexity to MVP)
2. **Never use WASM** (rejected - loses security and portability benefits)

**Implementation**:
- Phase 1: Native patterns (CoT, Research) via trait implementations
- Phase 2: WASM compilation for all patterns
- Pattern registry supports both: `execute_native()` and `execute_wasm()`

**Trade-offs**:
- ✅ Faster time to MVP
- ✅ Easier debugging during development
- ✅ Can benchmark native vs WASM performance
- ❌ Requires later migration effort
- ❌ Defers security benefits of sandboxing

---

### Decision 6: Deep Research 2.0 with Iterative Coverage Loop

**Decision**: Implement Manus.ai-inspired autonomous research with iterative coverage evaluation and gap identification.

**Rationale**:
- **Higher Quality**: Iterative refinement produces more comprehensive results
- **Autonomy**: System identifies gaps without human intervention
- **Measurable**: Coverage score (0-1) provides quality metric
- **Competitive**: Matches Manus.ai's deep research capabilities

**Alternatives Considered**:
1. **Single-pass research** (rejected - lower quality, no gap filling)
2. **Fixed iteration count** (rejected - may stop too early or late)
3. **User-driven refinement** (rejected - loses autonomy)

**Implementation**:
```rust
pub struct DeepResearch {
    pub max_iterations: usize,           // Default: 3
    pub sources_per_round: usize,        // Default: 6
    pub min_sources: usize,              // Default: 8
    pub coverage_threshold: f64,         // Default: 0.8
    pub enable_verification: bool,       // Default: false
    pub enable_fact_extraction: bool,    // Default: false
}
```

**Algorithm**:
1. Decompose query into sub-questions
2. Search sources for each sub-question
3. Evaluate coverage with LLM (score 0-1)
4. If coverage < threshold:
   - Identify gaps in coverage
   - Generate additional sub-questions
   - Repeat from step 2
5. Synthesize final report with citations
6. Optional: Extract facts and verify claims

**Trade-offs**:
- ✅ Higher quality research
- ✅ Autonomous gap identification
- ✅ Measurable quality (coverage score)
- ❌ Slower execution (3x iterations)
- ❌ Higher token cost (more LLM calls)
- ❌ More complex to debug

---

### Decision 7: In-Process Agent-Core for Python Tools

**Decision**: Run agent-core (WASI Python sandbox) in-process rather than as separate service.

**Rationale**:
- **Simplicity**: No IPC overhead, simpler deployment
- **Performance**: Direct function calls, no gRPC serialization
- **Shared Memory**: Can share state without copying
- **Easier Debugging**: Single process to debug

**Alternatives Considered**:
1. **Separate process** (cloud approach - rejected for embedded use)
2. **gRPC communication** (rejected - unnecessary IPC overhead)
3. **No Python support** (rejected - loses tool ecosystem)

**Implementation**:
- Agent-core as library crate (not binary)
- Direct Rust function calls to WASI sandbox
- Shared LLM client and tool registry

**Trade-offs**:
- ✅ Simpler architecture
- ✅ Better performance
- ✅ Easier deployment
- ❌ Crash in Python code crashes entire app (mitigated by WASI sandbox)
- ❌ Cannot scale Python execution separately

---

### Decision 8: Fixed Concurrent Workflow Limit

**Decision**: Limit concurrent workflows to `min(num_cpus, 10)` to prevent resource exhaustion.

**Rationale**:
- **Resource Protection**: Prevents OOM from too many concurrent workflows
- **Predictable Performance**: Each workflow gets fair CPU share
- **User Experience**: Better to queue than thrash

**Alternatives Considered**:
1. **Unlimited concurrency** (rejected - OOM risk, CPU thrashing)
2. **Fixed limit (e.g., 5)** (rejected - underutilizes high-core machines)
3. **Dynamic based on memory** (rejected - complex, unpredictable)

**Implementation**:
```rust
let max_concurrent = std::cmp::min(num_cpus::get(), 10);
let semaphore = Arc::new(Semaphore::new(max_concurrent));
```

**Trade-offs**:
- ✅ Prevents resource exhaustion
- ✅ Fair CPU allocation
- ✅ Simple implementation
- ❌ May underutilize on machines with >10 cores
- ❌ Queuing delay for workflows beyond limit

---

### Decision 9: zstd Compression for Checkpoints

**Decision**: Use zstd level 3 compression for workflow checkpoints.

**Rationale**:
- **Space Savings**: ~50-70% size reduction
- **Fast**: Level 3 provides good compression/speed balance
- **Battle-Tested**: Used by Facebook, Linux kernel
- **Streaming**: Can compress/decompress incrementally

**Alternatives Considered**:
1. **No compression** (rejected - checkpoint bloat)
2. **gzip** (rejected - slower, worse compression ratio)
3. **lz4** (rejected - faster but worse compression)
4. **zstd level 9+** (rejected - too slow for real-time checkpointing)

**Implementation**:
```rust
let compressed = zstd::encode_all(&bincode::serialize(&state)?, 3)?;
```

**Benchmarks**:
- Compression ratio: ~60% average
- Compression time: ~10ms for 100KB state
- Decompression time: ~5ms for 100KB state

**Trade-offs**:
- ✅ 50-70% space savings
- ✅ Fast compression/decompression
- ✅ Good balance
- ❌ Adds ~15ms latency to checkpointing
- ❌ Requires decompression to inspect checkpoints

---

### Decision 10: 7-Day Event Retention for Completed Workflows

**Decision**: Automatically delete events for completed workflows after 7 days.

**Rationale**:
- **Storage Management**: Prevents unbounded database growth
- **Privacy**: Limits data retention to reasonable period
- **Debugging Window**: 7 days sufficient for most debugging
- **Compliance**: Aligns with data minimization principles

**Alternatives Considered**:
1. **Unlimited retention** (rejected - storage bloat)
2. **1 day** (rejected - too short for debugging)
3. **30 days** (rejected - unnecessary for most users)
4. **User-configurable** (future enhancement)

**Implementation**:
- Background task runs daily
- Deletes workflows where `status = 'completed' AND completed_at < NOW() - 7 days`
- Cascades to events and checkpoints

**Trade-offs**:
- ✅ Predictable storage growth
- ✅ Privacy-friendly
- ✅ Sufficient debug window
- ❌ Cannot replay very old workflows
- ❌ Loses long-term analytics data

---

## Constraints

### Platform Constraints

- **Single Device Only**: No distributed execution in Phase 1
- **Desktop First**: Mobile support in Phase 2
- **Local Storage**: All data on device, no cloud sync
- **Internet Required**: For LLM API calls only

### Resource Constraints

- **Memory**: 150MB target, 500MB max per workflow
- **CPU**: Max concurrent workflows = min(num_cpus, 10)
- **Storage**: 100MB per 1000 workflows, 1GB total limit
- **Network**: Rate limits apply per LLM provider

### Technology Constraints

- **SQLite Version**: Requires 3.35+ for JSON functions
- **WASM Runtime**: Wasmtime v40 (v41+ breaks compatibility)
- **WASI**: Preview 1 only (Preview 2 not yet stable)
- **Rust Version**: 1.91.1+ (for async fn in traits)

### Feature Constraints

- **No Temporal UI**: Cannot use Temporal's workflow debugger
- **No Horizontal Scaling**: Single-device limit
- **No Scheduled Tasks**: Manual submission only (Phase 1)
- **API Key Storage**: Local only, not synced across devices

---

## Open Questions

### Question 1: WASM Module Distribution

**Question**: How should WASM workflow modules be packaged and updated?

**Options**:
a) Bundle in app binary (increases download size by ~50MB)
b) Download on-demand from CDN (requires network, version management)
c) User-provided (advanced users only, security risk)

**Recommendation**: Option (a) for Phase 1 (bundled), option (b) for Phase 2 (CDN with auto-updates)

### Question 2: Deep Research Configuration UI

**Question**: Should users configure deep research parameters in UI, or use defaults?

**Parameters**:
- `max_iterations` (default: 3, range: 1-10)
- `sources_per_round` (default: 6, range: 3-20)
- `coverage_threshold` (default: 0.8, range: 0.5-0.95)
- `enable_verification` (default: false)
- `enable_fact_extraction` (default: false)

**Recommendation**: Use sensible defaults, add advanced settings panel in Phase 2

### Question 3: Multi-Device Sync

**Question**: Should workflow state sync across devices in future versions?

**Considerations**:
- **Pros**: Work across desktop/mobile, cloud backup
- **Cons**: Requires cloud infrastructure, sync conflicts, privacy concerns

**Recommendation**: Local-only for Phase 1, evaluate cloud sync for Phase 3 based on user demand

---

## Research References

### Durable Workflow Pattern
- Temporal's workflow engine architecture
- Azure Durable Functions design docs
- Shannon's existing durable-shannon implementation

### Event Sourcing
- Martin Fowler's Event Sourcing pattern
- CQRS + Event Sourcing (Greg Young)
- Event Store design principles

### WebAssembly Sandboxing
- WASI specification (Preview 1)
- Wasmtime security model
- Bytecode Alliance standards

### Manus.ai Features
- Blog posts and demos
- Autonomous research capabilities
- Deep research with iterative coverage

### SQLite Optimization
- SQLite WAL mode documentation
- Performance tuning guide
- Concurrent access patterns
