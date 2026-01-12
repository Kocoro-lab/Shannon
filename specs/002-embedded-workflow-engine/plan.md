# Embedded Workflow Engine - Technical Plan

**Version**: 1.0  
**Feature ID**: 002-embedded-workflow-engine  
**Status**: Ready for Implementation

---

## Technology Stack

### Core Technologies
- **Language**: Rust 1.91.1+ (stable)
- **Async Runtime**: Tokio 1.x
- **WASM Runtime**: Wasmtime v40 (WASI Preview 1)
- **Database**: SQLite 3.35+ with WAL mode
- **Event Bus**: Tokio broadcast channels
- **HTTP Server**: Axum (integrated in shannon-api)
- **Serialization**: serde, bincode, serde_json

### Key Crates
- `durable-shannon` - Durable workflow execution engine
- `workflow-patterns` - Cognitive pattern implementations  
- `shannon-api` - API gateway and orchestration
- `agent-core` - WASI Python sandbox
- `parking_lot` - Fast synchronization primitives
- `wasmtime` - WebAssembly runtime
- `rusqlite` - SQLite bindings
- `tokio` - Async runtime
- `axum` - HTTP framework
- `tower` - Middleware
- `async-stream` - Async stream utilities
- `futures` - Futures utilities

---

## Architecture Overview

### Component Hierarchy

```
Tauri Desktop App
└── Shannon API (Rust)
    ├── Embedded Workflow Engine (NEW)
    │   ├── Workflow Routing & Submission
    │   ├── Pattern Selection & Execution
    │   ├── Event Streaming Coordination
    │   └── Session & State Management
    ├── Durable Worker (durable-shannon)
    │   ├── WASM Module Execution
    │   ├── Event Sourcing & Replay
    │   ├── Checkpoint Management
    │   └── Concurrency Control
    ├── Pattern Registry (workflow-patterns)
    │   ├── Chain of Thought
    │   ├── Tree of Thoughts
    │   ├── Research (Deep 2.0)
    │   ├── ReAct
    │   ├── Debate
    │   └── Reflection
    ├── Activity Layer
    │   ├── LLM Activity (OpenAI, Anthropic, etc.)
    │   └── Tool Activity (web_search, calculator, Python)
    └── Storage Layer
        ├── SQLite Event Log
        ├── Workflow State DB
        └── Session Storage
```

### Data Flow Architecture

```
Task Submission → Complexity Analysis → Pattern Selection
                                            ↓
                              WASM Module OR Native Pattern
                                            ↓
                              Activity Calls (LLM, Tools)
                                            ↓
                              Event Emission → Event Bus
                                            ↓
                              SQLite Persistence + SSE Streaming
```

---

## File Structure

### New Files to Create

```
rust/shannon-api/src/workflow/embedded/
├── mod.rs                  # Module exports
├── engine.rs               # EmbeddedWorkflowEngine
├── router.rs               # Complexity analysis & routing
├── session.rs              # Session management
├── recovery.rs             # Workflow recovery & replay
├── event_bus.rs            # Event streaming coordination
└── export.rs               # Export/import workflows

rust/shannon-api/src/workflow/patterns/
├── mod.rs                  # Pattern registry
├── base.rs                 # CognitivePattern trait
├── chain_of_thought.rs     # CoT implementation
├── tree_of_thoughts.rs     # ToT implementation
├── research.rs             # Research + Deep 2.0
├── react.rs                # ReAct loops
├── debate.rs               # Multi-agent debate
└── reflection.rs           # Self-critique

rust/durable-shannon/src/activities/
├── llm.rs                  # LLM activity implementation
└── tools.rs                # Tool execution activity

rust/shannon-api/src/database/
├── workflow_store.rs       # Workflow CRUD operations
└── checkpoint.rs           # Checkpoint save/load

rust/shannon-api/src/gateway/
├── streaming.rs            # SSE/WebSocket endpoints
└── control.rs              # Pause/resume/cancel handlers

desktop/src-tauri/src/
└── workflow.rs             # Tauri command bindings

desktop/app/(app)/workflows/
└── history/
    └── page.tsx            # Workflow history browser

desktop/components/
├── workflow-progress.tsx   # Real-time progress
├── reasoning-timeline.tsx  # CoT step display
└── source-citations.tsx    # Research sources
```

### Modified Files

```
rust/shannon-api/src/workflow/mod.rs
rust/shannon-api/src/gateway/routes.rs
rust/shannon-api/Cargo.toml
rust/durable-shannon/Cargo.toml
desktop/src-tauri/src/main.rs
desktop/src-tauri/Cargo.toml
```

---

## Core Components

### 1. Embedded Workflow Engine

**Location**: `rust/shannon-api/src/workflow/embedded/engine.rs`

**Responsibilities**:
- Workflow submission and routing
- Pattern execution coordination
- Activity scheduling (LLM, Tools)
- Event streaming to UI
- State persistence and recovery
- Concurrency control

**Key Structs**:
```rust
pub struct EmbeddedWorkflowEngine {
    worker: Arc<durable_shannon::EmbeddedWorker<SqliteEventLog>>,
    patterns: Arc<PatternRegistry>,
    llm_client: Arc<LlmOrchestrator>,
    event_bus: Arc<EventBus>,
    database: Arc<Database>,
}
```

### 2. Durable Worker

**Location**: `rust/durable-shannon/src/worker/`

**Responsibilities**:
- WASM module instantiation and execution
- Event sourcing (append-only log)
- Deterministic replay
- Checkpoint creation and loading
- Concurrent workflow execution limits

**Event Log Interface**:
```rust
#[async_trait]
pub trait EventLog: Send + Sync {
    async fn append(&self, workflow_id: &str, event: Event) -> Result<()>;
    async fn replay(&self, workflow_id: &str) -> Result<Vec<Event>>;
    async fn replay_since(&self, workflow_id: &str, sequence: u64) -> Result<Vec<Event>>;
}
```

### 3. Pattern Registry

**Location**: `rust/shannon-api/src/workflow/patterns/`

**Responsibilities**:
- Register cognitive patterns
- Execute patterns (native or WASM)
- Cache WASM modules (LRU)
- Provide unified pattern interface

**Pattern Trait**:
```rust
#[async_trait]
pub trait CognitivePattern: Send + Sync {
    async fn execute(&self, input: PatternInput) -> Result<PatternOutput>;
    fn name(&self) -> &str;
    fn description(&self) -> &str;
}
```

### 4. Event Bus

**Location**: `rust/shannon-api/src/workflow/embedded/event_bus.rs`

**Responsibilities**:
- Real-time event broadcasting
- Multi-subscriber support
- Backpressure handling
- Event persistence coordination

**Architecture**:
```rust
pub struct EventBus {
    channels: Mutex<HashMap<String, broadcast::Sender<WorkflowEvent>>>,
    capacity: usize,  // Default: 256 events
}
```

### 5. Activity Layer

**Location**: `rust/durable-shannon/src/activities/`

**Responsibilities**:
- LLM API calls with streaming
- Tool execution (web_search, calculator, Python)
- Retry logic with exponential backoff
- Timeout enforcement

**Activity Context**:
```rust
pub struct ActivityContext {
    dependencies: HashMap<String, Arc<dyn Any + Send + Sync>>,
}

impl ActivityContext {
    pub fn get<T: 'static>(&self, key: &str) -> Result<&Arc<T>>;
}
```

---

## Database Schema

### SQLite Tables

**workflows**:
```sql
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    workflow_type TEXT NOT NULL,
    status TEXT NOT NULL,
    input TEXT NOT NULL,
    output TEXT,
    session_id TEXT,
    user_id TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER
);
CREATE INDEX idx_workflows_status ON workflows(status);
CREATE INDEX idx_workflows_session ON workflows(session_id);
```

**workflow_events**:
```sql
CREATE TABLE workflow_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_data BLOB NOT NULL,
    sequence INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id),
    UNIQUE (workflow_id, sequence)
);
CREATE INDEX idx_workflow_events_lookup ON workflow_events(workflow_id, sequence);
```

**workflow_checkpoints**:
```sql
CREATE TABLE workflow_checkpoints (
    workflow_id TEXT PRIMARY KEY,
    state_data BLOB NOT NULL,
    last_event_sequence INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);
```

**sessions**:
```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    active_workflow_id TEXT,
    context TEXT,
    created_at INTEGER NOT NULL,
    last_activity INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

---

## API Endpoints

### Task Management

- `POST /api/v1/tasks` - Submit workflow
- `GET /api/v1/tasks/{id}` - Get workflow status
- `GET /api/v1/tasks` - List workflows with pagination
- `GET /api/v1/tasks/{id}/output` - Get workflow result
- `GET /api/v1/tasks/{id}/progress` - Get progress percentage

### Control Signals

- `POST /api/v1/tasks/{id}/pause` - Pause workflow
- `POST /api/v1/tasks/{id}/resume` - Resume workflow
- `POST /api/v1/tasks/{id}/cancel` - Cancel workflow
- `GET /api/v1/tasks/{id}/control-state` - Get control state

### Event Streaming

- `GET /api/v1/tasks/{id}/stream` - SSE event stream
- `GET /api/v1/tasks/{id}/stream/ws` - WebSocket stream

### Session Management

- `POST /api/v1/sessions` - Create session
- `GET /api/v1/sessions/{id}` - Get session info
- `GET /api/v1/sessions/{id}/history` - Get conversation history
- `GET /api/v1/sessions/{id}/events` - Get session events

---

## Performance Targets

### Metrics

| Metric | Target | Acceptable | Measurement |
|--------|--------|-----------|-------------|
| Cold Start | <200ms | <250ms | Engine init time |
| Simple Task | <5s | <7s | CoT end-to-end |
| Research Task | <15min | <20min | Deep Research 2.0 |
| Event Latency | <100ms | <150ms | Emit → UI receive |
| Event Throughput | >5K/s | >3K/s | SQLite write rate |
| Memory/Workflow | <150MB | <200MB | RSS measurement |
| Concurrent Workflows | 5-10 | 3-5 | Based on cores |

### Resource Limits

**CPU**:
- Max concurrent workflows: `min(num_cpus::get(), 10)`
- WASM fuel limit: 10M instructions per activity
- Pattern execution: Multi-threaded with rayon

**Memory**:
- WASM instance: 512MB default, 1GB max
- Event buffer: 256 events per workflow (ring buffer)
- WASM cache: 100MB LRU

**Storage**:
- Event log retention: 7 days for completed workflows
- Checkpoint compression: zstd level 3
- WAL mode enabled for concurrent access

---

## Testing Strategy

### Coverage Targets
- **Core Engine**: 100% coverage (no exceptions)
- **Patterns**: 95% coverage
- **Activities**: 100% coverage
- **Overall**: ≥90% coverage

### Test Categories

**Unit Tests**: All components
- Pure logic functions
- State transitions
- Error handling
- Edge cases

**Integration Tests**: Cross-component
- Workflow submission → completion
- Event persistence → replay
- Pattern execution with activities
- Control signal propagation

**Property Tests**: Invariants
- Event ordering consistency
- Deterministic replay
- Checkpoint recovery equivalence

**Benchmarks**: Performance
- Cold start time
- Task latency (simple, research)
- Event throughput
- Memory usage

**End-to-End Tests**: Full workflows
- All 6 patterns
- Pause/resume/cancel
- Crash recovery
- Concurrent execution

---

## Security Model

### WASM Sandbox

**Capabilities**:
```rust
pub struct SandboxCapabilities {
    pub timeout_ms: u64,
    pub max_memory_mb: usize,
    pub allow_network: bool,
    pub allow_filesystem: bool,
    pub allowed_hosts: Vec<String>,
}
```

**Restrictions**:
- Virtualized filesystem (no host access)
- Network allow-list per workflow type
- Fuel-based CPU limits
- Memory caps (default 512MB)
- Deterministic execution (no system randomness)

### Data Protection

- API keys encrypted with AES-256-GCM
- Sensitive data sanitized in exports
- Local-only storage (no cloud sync)
- Event log contains no raw API keys

---

## Dependencies

### Required Crates (to add)

```toml
[dependencies]
# Existing shannon-api deps...
async-stream = "0.3"
lru = "0.12"
zstd = "0.13"
crc32fast = "1.4"

[dependencies.durable-shannon]
path = "../../durable-shannon"

[dependencies.workflow-patterns]
path = "../../workflow-patterns"
```

### Durable Shannon Updates

```toml
[dependencies]
wasmtime = "40"
wasmtime-wasi = "40"
bincode = "1.3"
parking_lot = "0.12"
```

---

## Configuration

### Feature Flags

```rust
// Cargo.toml
[features]
default = ["embedded-workflow"]
embedded-workflow = ["durable-shannon", "workflow-patterns"]
wasm-patterns = ["wasmtime"]
```

### Runtime Configuration

```yaml
# config/shannon.yaml
embedded_workflow:
  max_concurrent: 10
  wasm_cache_size_mb: 100
  event_buffer_size: 256
  checkpoint_interval_events: 10
  checkpoint_max_age_minutes: 5
  event_retention_days: 7
  
  patterns:
    chain_of_thought:
      max_iterations: 5
      model: "claude-sonnet-4-20250514"
    
    research:
      max_iterations: 3
      sources_per_round: 6
      min_sources: 8
      coverage_threshold: 0.8
      enable_verification: false
      enable_fact_extraction: false
```

---

## Migration Strategy

### Phase 1: Foundation (Weeks 1-6)
- SQLite event log backend
- Workflow database schema
- Event bus infrastructure
- Engine skeleton
- Pattern registry
- CoT and Research patterns
- LLM activity
- SSE streaming
- Control signals
- Session continuity
- Recovery & replay

### Phase 2: Advanced Features (Weeks 7-12)
- ToT, ReAct, Debate, Reflection patterns
- Tool activity
- Deep Research 2.0
- Complexity routing
- WASM module compilation
- WASM caching
- Performance benchmarks
- Optimization pass

### Phase 3: Production Ready (Weeks 13-16)
- Error recovery & retry
- Replay debugging
- Checkpoint optimization
- Tauri workflow commands
- Workflow history UI
- Progress visualization
- API compatibility tests
- Export/import
- E2E test suite
- Documentation
- Quality gates

---

## Success Criteria

✅ Execute Chain of Thought workflows  
✅ Execute Research workflows with citations  
✅ Event streaming to Tauri UI  
✅ State persistence in SQLite  
✅ Workflow pause/resume/cancel  
✅ Session continuity across app restarts  
✅ LLM provider integration (OpenAI, Anthropic)  
✅ Basic tool execution (web_search, calculator)  
✅ All tests passing with ≥90% coverage  
✅ Zero compilation warnings  
✅ Performance targets met  
✅ API compatibility with cloud verified

---

## References

- Rust Coding Standards: `docs/coding-standards/RUST.md`
- Shannon Architecture: `docs/rust-architecture.md`
- Durable Shannon: `rust/durable-shannon/README.md`
- Workflow Patterns: `rust/workflow-patterns/README.md`
