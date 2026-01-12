# Embedded Workflow Engine - Implementation Plan

**Version**: 1.0  
**Date**: January 12, 2026  
**Status**: Ready for Implementation  
**Specification**: [`specs/embedded-workflow-engine-spec.md`](../specs/embedded-workflow-engine-spec.md)

---

## Executive Summary

This plan details the implementation of an embedded workflow engine for Shannon's Tauri application, enabling autonomous AI agent execution with deep research capabilities while maintaining 100% API compatibility with the cloud-based Temporal implementation.

**Key Deliverables**:
- Embedded workflow engine integrated into shannon-api
- Native Rust implementations of CoT, ToT, ReAct, Research, Debate, Reflection patterns
- SQLite-based durable state with event sourcing
- Real-time event streaming to Tauri UI
- Session continuity across app restarts
- 100% test coverage for core engine components

**Timeline**: 16 weeks (4 months) across 3 phases

**Team Size**: 2-3 engineers

---

## Technical Stack

### Core Technologies

| Component | Technology | Version | Purpose |
|-----------|-----------|---------|---------|
| **Language** | Rust | 1.91.1 (stable) | Primary implementation language |
| **Async Runtime** | tokio | 1.x | Asynchronous execution |
| **WASM Runtime** | Wasmtime | 0.40 | WASI workflow execution |
| **Database** | rusqlite | 0.32+ | SQLite embedded database |
| **HTTP Framework** | axum | 0.8+ | REST API endpoints |
| **Event Streaming** | tokio broadcast | 1.x | Real-time UI updates |
| **Serialization** | serde/bincode | 1.x | Data serialization |
| **Sync Primitives** | parking_lot | 0.12+ | High-performance locks |

### Testing & Quality

| Component | Technology | Version | Purpose |
|-----------|-----------|---------|---------|
| **Unit Testing** | cargo test | - | Standard Rust tests |
| **Property Testing** | proptest | 1.x | Generative testing |
| **Benchmarking** | criterion | 0.5+ | Performance measurement |
| **Coverage** | cargo-tarpaulin | 0.31+ | Code coverage analysis |
| **Mocking** | mockall | 0.13+ | Test doubles |

### Dependencies (from Cargo.toml)

```toml
[dependencies]
# Existing dependencies from shannon-api
axum = { workspace = true }
tokio = { workspace = true }
serde = { workspace = true }
serde_json = { workspace = true }
bincode = { workspace = true }
rusqlite = { workspace = true }
parking_lot = { workspace = true }
async-trait = { workspace = true }
tracing = { workspace = true }
chrono = { workspace = true }
uuid = { workspace = true }
anyhow = { workspace = true }
thiserror = { workspace = true }

# Workflow engine dependencies
durable-shannon = { workspace = true }
workflow-patterns = { workspace = true }
wasmtime = { workspace = true }
wasmtime-wasi = { workspace = true }

# Performance
mimalloc = { workspace = true }

[dev-dependencies]
proptest = "1.5"
criterion = "0.5"
mockall = "0.13"
tokio-test = { workspace = true }
tempfile = { workspace = true }
```

---

## Architecture Overview

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                       Tauri Frontend (Next.js)                   │
│                     desktop/app/(app)/chat/                      │
└────────────────────────────┬────────────────────────────────────┘
                             │ Tauri IPC / HTTP (port 8765)
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│              Shannon API - Gateway Layer                         │
│          rust/shannon-api/src/gateway/                           │
│  ┌────────────────────────────────────────────────────────┐     │
│  │ • POST /api/v1/tasks (task submission)                 │     │
│  │ • GET /api/v1/tasks/{id}/stream (SSE streaming)        │     │
│  │ • POST /api/v1/tasks/{id}/pause|resume|cancel          │     │
│  │ • Embedded auth (no JWT required)                      │     │
│  └────────────────────────────────────────────────────────┘     │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│         Embedded Workflow Engine (NEW)                           │
│    rust/shannon-api/src/workflow/embedded/                       │
│  ┌────────────────────────────────────────────────────────┐     │
│  │ EmbeddedWorkflowEngine                                  │     │
│  │ • Task routing & complexity analysis                    │     │
│  │ • Pattern selection (CoT, ToT, Research, etc.)          │     │
│  │ • Workflow orchestration                                │     │
│  │ • Event streaming coordination                          │     │
│  │ • Session management                                    │     │
│  └──────────────┬──────────────────────────┬───────────────┘     │
│                 │                          │                     │
│                 ▼                          ▼                     │
│  ┌──────────────────────────┐  ┌─────────────────────────┐     │
│  │   Pattern Registry        │  │    Event Bus            │     │
│  │  (patterns/)              │  │  (embedded/event_bus.rs)│     │
│  │ • CoT, ToT, Research      │  │ • broadcast channels    │     │
│  │ • ReAct, Debate           │  │ • Event persistence     │     │
│  │ • Reflection              │  │ • SSE/WS streaming      │     │
│  └──────────────┬────────────┘  └─────────────────────────┘     │
└─────────────────┼──────────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────────┐
│         Durable Shannon Worker                                   │
│    rust/durable-shannon/src/worker/                              │
│  ┌────────────────────────────────────────────────────────┐     │
│  │ EmbeddedWorker                                          │     │
│  │ • WASM module loading & execution                       │     │
│  │ • Event sourcing & replay                               │     │
│  │ • Checkpoint management                                 │     │
│  │ • Concurrency control (max N workflows)                 │     │
│  └──────────────┬──────────────────────────┬───────────────┘     │
│                 │                          │                     │
│                 ▼                          ▼                     │
│  ┌──────────────────────────┐  ┌─────────────────────────┐     │
│  │   SQLite Event Log        │  │  MicroSandbox (WASM)    │     │
│  │  (backends/)              │  │  (microsandbox/)        │     │
│  │ • workflow_events table   │  │ • Wasmtime v40          │     │
│  │ • workflow_checkpoints    │  │ • WASI Preview 1        │     │
│  │ • Event replay            │  │ • Capability security   │     │
│  └───────────────────────────┘  └─────────────────────────┘     │
└─────────────────┬──────────────────────────┬────────────────────┘
                  │                          │
                  ▼                          ▼
┌──────────────────────────┐    ┌─────────────────────────────────┐
│   Activity Execution      │    │    LLM Orchestrator             │
│  (activities/)            │    │  rust/shannon-api/src/llm/      │
│ • call_llm()              │◄───┤ • Multi-provider routing        │
│ • execute_tool()          │    │ • OpenAI, Anthropic, etc.       │
│ • web_search()            │    │ • Streaming support             │
└──────────┬────────────────┘    │ • Retry & fallback              │
           │                     └─────────────────────────────────┘
           ▼
┌─────────────────────────────────────────────────────────────────┐
│              Agent Core (In-Process)                             │
│         rust/agent-core/src/                                     │
│  • WASI Python sandbox                                           │
│  • Python tool execution                                         │
│  • Memory management                                             │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

**Task Submission**:
```
1. UI → POST /api/v1/tasks → Gateway
2. Gateway → EmbeddedWorkflowEngine.submit_task()
3. Engine → analyze_complexity() → route_workflow()
4. Engine → PatternRegistry.get(pattern_name)
5. Engine → DurableWorker.submit(pattern, input)
6. Worker → MicroSandbox.instantiate(wasm_module)
7. WASM → call_activity("llm", request) → LLM Orchestrator
8. WASM → call_activity("tool", request) → Agent Core
9. Worker → emit Event → SQLite EventLog.append()
10. Worker → EventBus.broadcast() → SSE → UI
11. Worker → return WorkflowHandle → Engine → Gateway → UI
```

**Event Streaming**:
```
1. UI → GET /api/v1/tasks/{id}/stream → Gateway
2. Gateway → EmbeddedWorkflowEngine.stream_events(task_id)
3. Engine → EventBus.subscribe(workflow_id)
4. EventBus → broadcast::Receiver<WorkflowEvent>
5. Gateway → map WorkflowEvent → SSE format
6. Gateway → send SSE event → UI
7. UI → EventSource.onmessage → update UI state
```

**State Recovery**:
```
1. App startup → EmbeddedWorkflowEngine.recover_all()
2. Engine → Database.list_incomplete_workflows()
3. For each workflow:
   a. SqliteEventLog.replay(workflow_id) → Vec<Event>
   b. Worker.reconstruct_state(events)
   c. Worker.resume_execution()
4. Engine → emit recovery events → UI
```

---

## File Structure

### New Files to Create

```
rust/shannon-api/src/
├── workflow/
│   ├── embedded/                         # NEW - Embedded engine core
│   │   ├── mod.rs                        # Module exports
│   │   ├── engine.rs                     # EmbeddedWorkflowEngine implementation
│   │   ├── router.rs                     # Complexity analysis & routing logic
│   │   ├── event_bus.rs                  # Real-time event streaming
│   │   ├── session.rs                    # Session continuity management
│   │   └── recovery.rs                   # Crash recovery & replay
│   ├── patterns/                         # NEW - Pattern implementations
│   │   ├── mod.rs                        # Pattern registry
│   │   ├── chain_of_thought.rs           # CoT pattern with LLM integration
│   │   ├── tree_of_thoughts.rs           # ToT pattern with branching
│   │   ├── react.rs                      # ReAct loop implementation
│   │   ├── research.rs                   # Deep Research 2.0
│   │   ├── debate.rs                     # Multi-agent debate
│   │   └── reflection.rs                 # Self-critique pattern
│   └── ...existing files...

rust/durable-shannon/src/
├── backends/
│   ├── mod.rs                            # MODIFY - Add sqlite backend
│   ├── sqlite.rs                         # NEW - SQLite EventLog impl
│   └── memory.rs                         # NEW - In-memory EventLog (testing)
├── activities/
│   ├── llm.rs                            # ENHANCE - Full LLM activity impl
│   └── tools.rs                          # ENHANCE - Full tool activity impl
└── ...existing files...

rust/shannon-api/src/database/
├── workflow_store.rs                     # NEW - SQLite schema & queries
└── ...existing files...

desktop/src-tauri/src/
├── workflow.rs                           # NEW - Tauri commands for workflow ops
└── ...existing files...

tests/
├── workflow/                             # NEW - Comprehensive test suite
│   ├── integration/
│   │   ├── end_to_end_test.rs           # Full workflow execution
│   │   ├── pattern_test.rs              # All patterns (CoT, ToT, etc.)
│   │   ├── recovery_test.rs             # Crash recovery scenarios
│   │   ├── streaming_test.rs            # Event streaming validation
│   │   └── session_test.rs              # Session continuity tests
│   ├── unit/
│   │   ├── event_log_test.rs            # SQLite event log
│   │   ├── pattern_registry_test.rs     # Pattern routing
│   │   ├── router_test.rs               # Complexity analysis
│   │   └── event_bus_test.rs            # Event streaming
│   ├── property/
│   │   ├── event_log_properties.rs      # Proptest: event log consistency
│   │   └── state_machine_properties.rs  # Proptest: workflow state correctness
│   └── benchmarks/
│       ├── cold_start_bench.rs          # Criterion: cold start latency
│       ├── task_latency_bench.rs        # Criterion: end-to-end latency
│       └── throughput_bench.rs          # Criterion: event throughput
```

### Modified Files

```
rust/shannon-api/src/
├── workflow/
│   ├── mod.rs                            # MODIFY - Export embedded module
│   └── engine.rs                         # MODIFY - Integrate EmbeddedWorkflowEngine
├── gateway/
│   └── routes.rs                         # MODIFY - Add workflow control endpoints
└── database/
    └── schema.rs                         # MODIFY - Add workflow tables

rust/durable-shannon/src/
├── lib.rs                                # MODIFY - Export new backends
└── worker/mod.rs                         # MODIFY - Enhanced EmbeddedWorker

desktop/src-tauri/src/
├── main.rs                               # MODIFY - Register workflow commands
└── embedded_api.rs                       # MODIFY - Initialize workflow engine
```

---

## Implementation Phases

### Phase 1: Foundation MVP (Weeks 1-6)

**Goal**: Basic workflow execution with CoT and Research patterns, event streaming, and state persistence.

**Success Criteria**:
- Submit task via REST API
- Execute Chain of Thought workflow
- Execute Research workflow with web search
- Stream events to UI via SSE
- Persist workflow state in SQLite
- Resume workflow after app restart

#### Week 1-2: Core Integration

**P1.1: SQLite Event Log Backend** (3 days)
- Files: `rust/durable-shannon/src/backends/sqlite.rs`
- Dependencies: None
- Test: `tests/workflow/unit/event_log_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Implement SqliteEventLog struct with rusqlite connection pool
// 2. Implement EventLog trait (append, replay, replay_since)
// 3. Create schema migration for workflow_events table
// 4. Add event serialization/deserialization with bincode
// 5. Implement event batching for performance (10 events/batch)
// 6. Add WAL mode for concurrent reads/writes
// 7. Unit tests for all operations
// 8. Property tests for event ordering guarantees
```

**P1.2: Workflow Database Schema** (2 days) [P]
- Files: `rust/shannon-api/src/database/workflow_store.rs`
- Dependencies: None (parallel with P1.1)
- Test: `tests/workflow/unit/workflow_store_test.rs`
- Coverage: 100%

```sql
-- Tasks:
-- 1. Create workflows table (id, type, status, input, output, timestamps)
-- 2. Create workflow_events table (workflow_id, sequence, event_data)
-- 3. Create workflow_checkpoints table (workflow_id, state_data, sequence)
-- 4. Create indexes for efficient querying
-- 5. Migration scripts for schema versioning
-- 6. Repository pattern for CRUD operations
```

**P1.3: Event Bus Infrastructure** (2 days)
- Files: `rust/shannon-api/src/workflow/embedded/event_bus.rs`
- Dependencies: None
- Test: `tests/workflow/unit/event_bus_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Implement EventBus with tokio broadcast channels (256 capacity)
// 2. WorkflowEvent enum with 26+ event types (matching cloud)
// 3. Event persistence strategy (ephemeral vs persistent)
// 4. Subscribe/unsubscribe workflow event streams
// 5. Channel cleanup on workflow completion
// 6. Backpressure handling for slow consumers
// 7. Unit tests for broadcast, subscribe, cleanup
```

**P1.4: EmbeddedWorkflowEngine Skeleton** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/engine.rs`
- Dependencies: P1.1, P1.2, P1.3
- Test: `tests/workflow/integration/engine_test.rs`
- Coverage: 90%

```rust
// Tasks:
// 1. Create EmbeddedWorkflowEngine struct
// 2. Initialize with DurableWorker, EventLog, EventBus
// 3. Implement submit_task() method
// 4. Implement stream_events() method
// 5. Implement pause/resume/cancel methods
// 6. Integrate with existing DurableEngine in engine.rs
// 7. Add health check method
// 8. Integration tests for basic workflow lifecycle
```

#### Week 3-4: Pattern Implementation

**P1.5: Pattern Registry** (2 days)
- Files: `rust/shannon-api/src/workflow/patterns/mod.rs`
- Dependencies: P1.4
- Test: `tests/workflow/unit/pattern_registry_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Create PatternRegistry with HashMap storage
// 2. Register native patterns (pre-WASM)
// 3. Pattern lookup by name
// 4. Pattern execution wrapper with timing & metrics
// 5. Error handling & retry logic
// 6. Unit tests for registration and execution
```

**P1.6: Chain of Thought Pattern** (3 days)
- Files: `rust/shannon-api/src/workflow/patterns/chain_of_thought.rs`
- Dependencies: P1.5
- Test: `tests/workflow/integration/pattern_test.rs::cot`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement ChainOfThought pattern
// 2. Integrate with LLM Orchestrator (call_llm activity)
// 3. Step-by-step reasoning with max_iterations (default 5)
// 4. ReasoningStep tracking with timestamps
// 5. Emit progress events for each step
// 6. Token usage tracking
// 7. Integration test with OpenAI API
// 8. Mock tests for LLM calls
```

**P1.7: LLM Activity Implementation** (2 days) [P]
- Files: `rust/durable-shannon/src/activities/llm.rs`
- Dependencies: None (parallel with P1.6)
- Test: `tests/workflow/unit/llm_activity_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Enhance call_llm() activity with full implementation
// 2. ActivityContext for dependency injection
// 3. LlmRequest/LlmResponse types
// 4. Streaming support via tokio channels
// 5. Retry logic with exponential backoff
// 6. Timeout handling (default 30s, research 5min)
// 7. Unit tests with mock LLM client
```

**P1.8: Research Pattern** (4 days)
- Files: `rust/shannon-api/src/workflow/patterns/research.rs`
- Dependencies: P1.5, P1.7
- Test: `tests/workflow/integration/pattern_test.rs::research`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement Research pattern
// 2. Query decomposition step
// 3. Web search integration (Tavily API)
// 4. Source collection & deduplication
// 5. Synthesis step with citations
// 6. Confidence scoring
// 7. Emit progress events (decomposition, search, synthesis)
// 8. Integration test with real web search
// 9. Mock tests for search API
```

#### Week 5-6: Event Streaming & Control

**P1.9: SSE Streaming Endpoint** (3 days)
- Files: `rust/shannon-api/src/gateway/routes.rs` (modify)
- Dependencies: P1.3, P1.4
- Test: `tests/workflow/integration/streaming_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. GET /api/v1/tasks/{id}/stream endpoint
// 2. Map WorkflowEvent → SSE format (data: {json})
// 3. Keep-alive heartbeat every 15s
// 4. Graceful disconnect handling
// 5. Event filtering by type (optional query param)
// 6. Integration test with actual SSE client
// 7. Load test for 100 concurrent streams
```

**P1.10: Workflow Control Signals** (2 days)
- Files: `rust/shannon-api/src/workflow/control.rs` (modify)
- Dependencies: P1.4
- Test: `tests/workflow/integration/control_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. POST /api/v1/tasks/{id}/pause endpoint
// 2. POST /api/v1/tasks/{id}/resume endpoint
// 3. POST /api/v1/tasks/{id}/cancel endpoint
// 4. Implement control signal propagation to worker
// 5. Emit WORKFLOW_PAUSING, WORKFLOW_PAUSED events
// 6. Emit WORKFLOW_CANCELLING, WORKFLOW_CANCELLED events
// 7. Integration tests for all control operations
```

**P1.11: Session Continuity** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/session.rs`
- Dependencies: P1.2, P1.4
- Test: `tests/workflow/integration/session_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Create SessionManager struct
// 2. Session CRUD operations (create, get, update, delete)
// 3. Associate workflow with session
// 4. Conversation history storage
// 5. Context key-value storage
// 6. Recovery: list incomplete workflows on startup
// 7. Resume workflow execution from checkpoint
// 8. Integration test: submit → close app → reopen → resume
```

**P1.12: Recovery & Replay** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/recovery.rs`
- Dependencies: P1.1, P1.4, P1.11
- Test: `tests/workflow/integration/recovery_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Implement recover_workflow() method
// 2. Load checkpoint if exists
// 3. Replay events since checkpoint
// 4. Reconstruct workflow state from events
// 5. Resume execution from last successful state
// 6. Handle corrupted checkpoints (fallback to full replay)
// 7. Integration test: crash during workflow → recover → complete
// 8. Property test: replay determinism
```

#### Phase 1 Deliverables

- ✅ Submit task via REST API and get task_id
- ✅ Execute Chain of Thought workflow end-to-end
- ✅ Execute Research workflow with web search & citations
- ✅ Stream real-time events to UI via SSE
- ✅ Persist workflow state in SQLite
- ✅ Pause/resume/cancel running workflows
- ✅ Resume incomplete workflows after app restart
- ✅ 100% unit test coverage for core components
- ✅ Integration tests for all workflows

---

### Phase 2: Advanced Features (Weeks 7-12)

**Goal**: Advanced patterns (ToT, ReAct, Debate), Deep Research 2.0, WASM execution, performance optimization.

**Success Criteria**:
- Execute Tree of Thoughts with branching
- Execute ReAct loops with tool observation
- Execute Debate pattern with multiple agents
- Deep Research 2.0 with iterative coverage loop
- WASM-compiled patterns execution
- Performance benchmarks meet targets (<200ms cold start)

#### Week 7-8: Advanced Patterns

**P2.1: Tree of Thoughts Pattern** (4 days)
- Files: `rust/shannon-api/src/workflow/patterns/tree_of_thoughts.rs`
- Dependencies: P1.5, P1.7
- Test: `tests/workflow/integration/pattern_test.rs::tot`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement TreeOfThoughts pattern
// 2. Thought tree data structure (Node with children)
// 3. Branch generation (N candidates per step)
// 4. Evaluation function (score each branch)
// 5. Backtracking & pruning (keep top K branches)
// 6. Breadth-first vs depth-first exploration
// 7. Final answer synthesis from best path
// 8. Integration test with complex reasoning task
```

**P2.2: ReAct Pattern** (3 days)
- Files: `rust/shannon-api/src/workflow/patterns/react.rs`
- Dependencies: P1.5, P1.7
- Test: `tests/workflow/integration/pattern_test.rs::react`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement ReAct pattern
// 2. Reason step (LLM generates action plan)
// 3. Act step (execute tool with params)
// 4. Observe step (record tool output)
// 5. Loop until goal achieved or max iterations
// 6. Tool activity integration (web_search, calculator, etc.)
// 7. Halting condition detection
// 8. Integration test with multi-step tool usage
```

**P2.3: Tool Activity Implementation** (2 days) [P]
- Files: `rust/durable-shannon/src/activities/tools.rs`
- Dependencies: None (parallel with P2.2)
- Test: `tests/workflow/unit/tool_activity_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Enhance execute_tool() activity
// 2. Tool registry with built-in tools (web_search, web_fetch, calculator)
// 3. Python tool delegation to agent-core
// 4. Tool parameter validation
// 5. Tool output formatting
// 6. Timeout handling per tool
// 7. Unit tests with mock tools
```

**P2.4: Debate Pattern** (3 days)
- Files: `rust/shannon-api/src/workflow/patterns/debate.rs`
- Dependencies: P1.5, P1.7
- Test: `tests/workflow/integration/pattern_test.rs::debate`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement Debate pattern
// 2. Multi-agent architecture (2-4 debaters)
// 3. Round-based discussion (max 3 rounds)
// 4. Perspective generation (each agent argues position)
// 5. Critique & response cycle
// 6. Synthesis step (final answer from debate)
// 7. Parallel agent execution with tokio::spawn
// 8. Integration test with philosophical question
```

**P2.5: Reflection Pattern** (2 days)
- Files: `rust/shannon-api/src/workflow/patterns/reflection.rs`
- Dependencies: P1.5, P1.7
- Test: `tests/workflow/integration/pattern_test.rs::reflection`
- Coverage: 95%

```rust
// Tasks:
// 1. Implement Reflection pattern
// 2. Initial answer generation
// 3. Self-critique step (identify weaknesses)
// 4. Improvement step (generate better answer)
// 5. Quality gating (threshold 0.5)
// 6. Max reflection iterations (3)
// 7. Integration test showing improvement over iterations
```

#### Week 9-10: Deep Research 2.0

**P2.6: Deep Research 2.0 Implementation** (6 days)
- Files: `rust/shannon-api/src/workflow/patterns/research.rs` (enhance)
- Dependencies: P1.8, P2.3
- Test: `tests/workflow/integration/deep_research_test.rs`
- Coverage: 95%

```rust
// Tasks:
// 1. Iterative coverage loop (max 3 iterations)
// 2. Coverage evaluation with LLM (score 0-1)
// 3. Gap identification step
// 4. Additional sub-question generation
// 5. Source accumulation & deduplication
// 6. Fact extraction (optional, flag-gated)
// 7. Claim verification (optional, flag-gated)
// 8. Final report synthesis
// 9. Integration test: 15 min complex research task
// 10. Performance optimization for parallel searches
```

**P2.7: Complexity Analysis & Routing** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/router.rs`
- Dependencies: P1.4
- Test: `tests/workflow/unit/router_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Implement analyze_complexity() method
// 2. Complexity scoring (0-1 scale) with heuristics
// 3. Routing logic: mode → pattern mapping
// 4. Simple (<0.3) → SimpleTaskWorkflow
// 5. Medium (0.3-0.7) → CoT or Research
// 6. Complex (>0.7) → ToT or Debate
// 7. Strategy override via context.cognitive_strategy
// 8. Unit tests for all routing scenarios
```

#### Week 11-12: WASM & Optimization

**P2.8: WASM Module Compilation** (4 days)
- Files: `rust/workflow-patterns/` (build config)
- Dependencies: P2.1-P2.5
- Test: `tests/workflow/integration/wasm_execution_test.rs`
- Coverage: 90%

```rust
// Tasks:
// 1. Configure Cargo.toml for WASM target
// 2. Add wasm32-wasi build target
// 3. Compile patterns to WASM modules (CoT, ToT, Research)
// 4. WASM module validation & loading
// 5. Test pattern execution in WASM sandbox
// 6. Benchmark: native vs WASM performance
// 7. Integration test: submit task → WASM execution → result
```

**P2.9: WASM Module Caching** (2 days) [P]
- Files: `rust/durable-shannon/src/worker/cache.rs`
- Dependencies: None (parallel with P2.8)
- Test: `tests/workflow/unit/wasm_cache_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Implement WasmCache with LRU eviction
// 2. Module preloading on startup
// 3. Lazy loading on first use
// 4. Size limit enforcement (100MB)
// 5. Module versioning & invalidation
// 6. Unit tests for cache hit/miss scenarios
```

**P2.10: Performance Benchmarks** (3 days)
- Files: `tests/workflow/benchmarks/`
- Dependencies: P2.8
- Test: N/A (benchmarks)
- Coverage: N/A

```rust
// Tasks:
// 1. Cold start benchmark (target <200ms)
// 2. Simple task latency benchmark (target <5s)
// 3. Research task latency benchmark (target <15min)
// 4. Event throughput benchmark (target >5K/s)
// 5. Memory usage tracking (target <150MB/workflow)
// 6. Concurrent workflow benchmark (target 5-10 concurrent)
// 7. Criterion report generation
```

**P2.11: Optimization Pass** (4 days)
- Files: Various (refactoring)
- Dependencies: P2.10
- Test: All existing tests
- Coverage: Maintain 90%+

```rust
// Tasks:
// 1. Event batching optimization (10 events/batch)
// 2. SQLite query optimization (prepared statements)
// 3. Connection pooling for database
// 4. Parallel pattern execution where possible
// 5. Memory pooling for event buffers
// 6. Reduce allocations in hot paths
// 7. Profile-guided optimization
// 8. Re-run benchmarks & validate improvements
```

#### Phase 2 Deliverables

- ✅ All 6 cognitive patterns working (CoT, ToT, ReAct, Research, Debate, Reflection)
- ✅ Deep Research 2.0 with iterative coverage
- ✅ WASM module execution for patterns
- ✅ Routing logic with complexity analysis
- ✅ Performance benchmarks showing <200ms cold start
- ✅ Performance benchmarks showing <15min research tasks
- ✅ Memory usage <150MB per workflow
- ✅ Integration tests for all patterns

---

### Phase 3: Production Ready (Weeks 13-16)

**Goal**: Error recovery, debugging tools, UI integration, end-to-end testing, production deployment.

**Success Criteria**:
- Comprehensive error recovery with retries
- Workflow replay debugging capability
- UI workflow history browser
- Export/import workflow state
- 100% API compatibility with cloud
- Zero compilation warnings
- All tests passing

#### Week 13: Reliability

**P3.1: Error Recovery & Retry** (4 days)
- Files: `rust/shannon-api/src/workflow/embedded/recovery.rs` (enhance)
- Dependencies: P1.12
- Test: `tests/workflow/integration/error_recovery_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Transient error classification (network, timeout, rate limit)
// 2. Exponential backoff retry (max 3 retries)
// 3. Fallback provider on persistent failure
// 4. Checkpoint on each retry
// 5. Circuit breaker for failing LLM providers
// 6. Graceful degradation (simpler pattern on repeated failure)
// 7. Error event emission (ACTIVITY_FAILED, RETRY_SCHEDULED)
// 8. Integration tests: network failure → retry → success
```

**P3.2: Workflow Replay Debugging** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/replay.rs`
- Dependencies: P1.1
- Test: `tests/workflow/integration/replay_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Export workflow history to JSON file
// 2. Replay workflow from JSON history
// 3. Step-through debugging mode
// 4. Breakpoints at specific events
// 5. State inspection at any point in history
// 6. Time-travel debugging UI (CLI tool)
// 7. Integration test: export → replay → verify identical result
```

**P3.3: Checkpoint Optimization** (2 days) [P]
- Files: `rust/durable-shannon/src/worker/checkpoint.rs`
- Dependencies: None (parallel with P3.2)
- Test: `tests/workflow/unit/checkpoint_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Adaptive checkpoint frequency (based on event count)
// 2. Compression with zstd (level 3)
// 3. Incremental checkpoints (delta encoding)
// 4. Checkpoint pruning (keep last 3)
// 5. Corruption detection with checksums
// 6. Unit tests for compression & corruption handling
```

#### Week 14: UI Integration

**P3.4: Tauri Workflow Commands** (3 days)
- Files: `desktop/src-tauri/src/workflow.rs`
- Dependencies: P1.4
- Test: `desktop/src-tauri/tests/workflow_commands.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. #[tauri::command] submit_workflow()
// 2. #[tauri::command] stream_workflow_events()
// 3. #[tauri::command] pause_workflow()
// 4. #[tauri::command] resume_workflow()
// 5. #[tauri::command] cancel_workflow()
// 6. #[tauri::command] get_workflow_history()
// 7. Error handling with serde-compatible types
// 8. Integration tests with Tauri test harness
```

**P3.5: Workflow History Browser** (4 days)
- Files: `desktop/app/(app)/workflows/history/page.tsx`
- Dependencies: P3.4
- Test: `desktop/tests/integration/workflow_history.test.tsx`
- Coverage: 90%

```typescript
// Tasks:
// 1. List all workflows (completed, failed, in-progress)
// 2. Filter by status, date, pattern type
// 3. Search by query text
// 4. Workflow detail view with timeline
// 5. Event viewer with syntax highlighting
// 6. Export workflow to JSON button
// 7. Re-run workflow button
// 8. Integration test with React Testing Library
```

**P3.6: Progress Visualization** (3 days) [P]
- Files: `desktop/components/workflow-progress.tsx`
- Dependencies: P3.4 (parallel with P3.5)
- Test: `desktop/tests/components/workflow-progress.test.tsx`
- Coverage: 95%

```typescript
// Tasks:
// 1. Real-time progress bar (0-100%)
// 2. Current step display
// 3. Reasoning step timeline
// 4. Source citation list (live updates)
// 5. Token usage gauge
// 6. Pause/Resume/Cancel buttons
// 7. Component tests with mock data
```

#### Week 15: API Compatibility

**P3.7: API Compatibility Test Suite** (4 days)
- Files: `tests/workflow/compatibility/`
- Dependencies: All Phase 1-2 tasks
- Test: N/A (test suite itself)
- Coverage: N/A

```rust
// Tasks:
// 1. Cloud API request/response recording
// 2. Embedded API request/response comparison
// 3. Event type compatibility check (26+ types)
// 4. Event data schema validation
// 5. Timing tolerance (embedded faster than cloud OK)
// 6. Token usage comparison (within 10%)
// 7. Source citation comparison
// 8. Integration test: submit same query to both → compare results
```

**P3.8: Export/Import Workflow State** (3 days)
- Files: `rust/shannon-api/src/workflow/embedded/export.rs`
- Dependencies: P1.2, P1.4
- Test: `tests/workflow/integration/export_import_test.rs`
- Coverage: 100%

```rust
// Tasks:
// 1. Export workflow to JSON (input, events, output, metadata)
// 2. Export workflow to Markdown (human-readable report)
// 3. Import workflow from JSON (restore state)
// 4. Sanitize exported data (remove API keys)
// 5. Versioned export format (schema v1)
// 6. Integration test: export → import → resume workflow
```

#### Week 16: Final Testing & Polish

**P3.9: End-to-End Test Suite** (4 days)
- Files: `tests/workflow/e2e/`
- Dependencies: All Phase 1-3 tasks
- Test: N/A (test suite itself)
- Coverage: N/A

```rust
// Tasks:
// 1. E2E test: Simple query (CoT pattern)
// 2. E2E test: Research query (Deep Research 2.0)
// 3. E2E test: Complex query (ToT pattern)
// 4. E2E test: Multi-step query (ReAct pattern)
// 5. E2E test: Debate query (Debate pattern)
// 6. E2E test: Pause/Resume workflow mid-execution
// 7. E2E test: App crash during workflow → recover
// 8. E2E test: 10 concurrent workflows
```

**P3.10: Documentation** (2 days) [P]
- Files: `docs/embedded-workflow-engine.md`
- Dependencies: None (parallel with P3.9)
- Test: N/A
- Coverage: N/A

```markdown
// Tasks:
// 1. Architecture overview diagram
// 2. API reference (all endpoints)
// 3. Pattern selection guide
// 4. Configuration reference
// 5. Debugging guide (replay, logs)
// 6. Performance tuning tips
// 7. Migration guide from cloud Temporal
// 8. Troubleshooting FAQ
```

**P3.11: Quality Gates & Audit** (3 days)
- Files: N/A (CI/CD updates)
- Dependencies: All Phase 1-3 tasks
- Test: All tests
- Coverage: 90%+ overall

```bash
# Tasks:
# 1. cargo fmt --check (all files formatted)
# 2. cargo clippy -- -D warnings (zero warnings)
# 3. cargo test (100% pass rate)
# 4. cargo tarpaulin (90%+ coverage)
# 5. RUST.md compliance audit
# 6. Security audit (no unsafe, deps audit)
# 7. Performance regression test
# 8. Memory leak detection (valgrind)
```

#### Phase 3 Deliverables

- ✅ Comprehensive error recovery with retries & fallbacks
- ✅ Workflow replay debugging capability
- ✅ UI workflow history browser
- ✅ Progress visualization components
- ✅ Export/import workflow state
- ✅ API compatibility tests pass (100% parity)
- ✅ End-to-end tests pass (all scenarios)
- ✅ Zero compilation warnings
- ✅ 90%+ overall test coverage
- ✅ Complete documentation

---

## Testing Strategy

### Test Coverage Targets

| Component | Unit Tests | Integration Tests | Property Tests | Benchmarks | Coverage Target |
|-----------|-----------|-------------------|----------------|-----------|-----------------|
| **Event Log** | ✅ | ✅ | ✅ | ✅ | 100% |
| **Event Bus** | ✅ | ✅ | - | ✅ | 100% |
| **Pattern Registry** | ✅ | ✅ | - | - | 100% |
| **Workflow Engine** | ✅ | ✅ | - | ✅ | 95% |
| **Patterns (CoT, ToT, etc.)** | ✅ | ✅ | - | ✅ | 95% |
| **Activities (LLM, Tools)** | ✅ | ✅ | - | - | 100% |
| **Recovery & Replay** | ✅ | ✅ | ✅ | - | 100% |
| **API Routes** | ✅ | ✅ | - | - | 95% |
| **Overall** | - | - | - | - | **90%+** |

### Test Types

#### Unit Tests

**Location**: `tests/workflow/unit/`

**Coverage**: Individual functions, methods, modules

**Examples**:
```rust
// tests/workflow/unit/event_log_test.rs
#[tokio::test]
async fn test_event_log_append() {
    let log = SqliteEventLog::new(":memory:").await.unwrap();
    let event = Event::WorkflowStarted { /* ... */ };
    log.append("wf-123", event).await.unwrap();
    let events = log.replay("wf-123").await.unwrap();
    assert_eq!(events.len(), 1);
}

#[tokio::test]
async fn test_event_log_replay_empty() {
    let log = SqliteEventLog::new(":memory:").await.unwrap();
    let events = log.replay("nonexistent").await.unwrap();
    assert_eq!(events.len(), 0);
}
```

#### Integration Tests

**Location**: `tests/workflow/integration/`

**Coverage**: Component interactions, end-to-end workflows

**Examples**:
```rust
// tests/workflow/integration/pattern_test.rs
#[tokio::test]
async fn test_chain_of_thought_execution() {
    let engine = setup_test_engine().await;
    let task = Task::new("What is 2+2?", Strategy::ChainOfThought);
    
    let handle = engine.submit(task).await.unwrap();
    let result = handle.result().await.unwrap();
    
    assert_eq!(result.state, TaskState::Completed);
    assert!(result.content.unwrap().contains("4"));
    assert!(result.reasoning_steps.len() > 0);
}
```

#### Property Tests

**Location**: `tests/workflow/property/`

**Coverage**: Invariants, consistency, determinism

**Examples**:
```rust
// tests/workflow/property/event_log_properties.rs
use proptest::prelude::*;

proptest! {
    #[test]
    fn event_log_preserves_order(events in prop::collection::vec(arbitrary_event(), 0..100)) {
        tokio_test::block_on(async {
            let log = SqliteEventLog::new(":memory:").await.unwrap();
            for (i, event) in events.iter().enumerate() {
                log.append("wf", event.clone()).await.unwrap();
            }
            
            let replayed = log.replay("wf").await.unwrap();
            assert_eq!(replayed.len(), events.len());
            
            for (original, replayed) in events.iter().zip(replayed.iter()) {
                assert_eq!(original.event_type(), replayed.event_type());
            }
        });
    }
}
```

#### Benchmarks

**Location**: `tests/workflow/benchmarks/`

**Tool**: Criterion

**Examples**:
```rust
// tests/workflow/benchmarks/cold_start_bench.rs
use criterion::{black_box, criterion_group, criterion_main, Criterion};

fn cold_start_benchmark(c: &mut Criterion) {
    c.bench_function("engine_cold_start", |b| {
        b.to_async(tokio::runtime::Runtime::new().unwrap())
            .iter(|| async {
                let engine = EmbeddedWorkflowEngine::new(/* ... */).await.unwrap();
                black_box(engine);
            });
    });
}

criterion_group!(benches, cold_start_benchmark);
criterion_main!(benches);
```

### Test Data & Mocks

**Mock LLM Responses**:
```rust
// tests/workflow/fixtures/mock_llm.rs
pub struct MockLlmClient {
    responses: Vec<String>,
    call_count: AtomicUsize,
}

impl MockLlmClient {
    pub fn with_responses(responses: Vec<String>) -> Self {
        Self {
            responses,
            call_count: AtomicUsize::new(0),
        }
    }
}

#[async_trait]
impl LlmClient for MockLlmClient {
    async fn chat_completion(&self, /* ... */) -> Result<LlmResponse> {
        let idx = self.call_count.fetch_add(1, Ordering::Relaxed);
        let content = self.responses.get(idx).cloned()
            .unwrap_or_else(|| "Default response".to_string());
        Ok(LlmResponse { content, /* ... */ })
    }
}
```

**Test Fixtures**:
```rust
// tests/workflow/fixtures/mod.rs
pub fn sample_task(query: &str) -> Task {
    Task {
        id: format!("task-{}", Uuid::new_v4()),
        query: query.to_string(),
        strategy: Strategy::ChainOfThought,
        // ... defaults
    }
}

pub async fn setup_test_engine() -> EmbeddedWorkflowEngine {
    let event_log = SqliteEventLog::new(":memory:").await.unwrap();
    EmbeddedWorkflowEngine::new(
        event_log,
        PathBuf::from("./wasm"),
        4,
    ).await.unwrap()
}
```

---

## Performance Targets

### Benchmarks & Acceptance Criteria

| Metric | Target | Measurement | Acceptance |
|--------|--------|-------------|-----------|
| **Cold Start** | <200ms | Engine initialization time | P99 <250ms |
| **Simple Task** | <5s | CoT pattern end-to-end | P95 <7s |
| **Research Task** | <15min | Deep Research 2.0 | P95 <20min |
| **Event Latency** | <100ms | Event emit → UI receive | P99 <150ms |
| **Memory/Workflow** | <150MB | RSS per active workflow | Max 200MB |
| **Concurrent Workflows** | 5-10 | Based on CPU cores | min(cores, 10) |
| **Event Throughput** | >5K/s | Events written to SQLite | Sustained >3K/s |
| **Storage/Workflow** | <100KB | SQLite per completed workflow | Typical <50KB |

### Performance Optimization Strategy

**Phase 2 Baseline**:
1. Run all benchmarks before optimization
2. Profile with `cargo flamegraph` to identify hot paths
3. Measure baseline performance metrics

**Optimization Targets**:
- **Event Batching**: Group 10 events before SQLite write
- **Connection Pooling**: Reuse database connections (r2d2)
- **Memory Pooling**: Reuse event buffers with `bytes::BytesMut`
- **Parallel Execution**: Use `rayon` for independent pattern steps
- **WASM Caching**: LRU cache for compiled modules

**Validation**:
- Re-run benchmarks after each optimization
- Compare against baseline
- Ensure no regressions in correctness

---

## Quality Gates

### Compilation & Linting

```bash
# Must pass with zero warnings
cargo fmt --check
cargo clippy -- -D warnings
cargo build --all-features
cargo test --all-features

# Security audit
cargo audit
cargo deny check
```

### Test Coverage

```bash
# Generate coverage report
cargo tarpaulin --out Html --output-dir coverage/

# Gates:
# - Overall coverage: ≥90%
# - Core engine: ≥95%
# - Event log: 100%
# - Pattern registry: 100%
# - Activities: 100%
```

### Rust Coding Standards

All code MUST follow [`docs/coding-standards/RUST.md`](../docs/coding-standards/RUST.md):

**Mandatory Lints** (in Cargo.toml):
```toml
[lints.rust]
ambiguous_negative_literals = "warn"
missing_debug_implementations = "warn"
redundant_imports = "warn"
redundant_lifetimes = "warn"
trivial_numeric_casts = "warn"
unsafe_op_in_unsafe_fn = "warn"
unused_lifetimes = "warn"

[lints.clippy]
cargo = { level = "warn", priority = -1 }
complexity = { level = "warn", priority = -1 }
correctness = { level = "warn", priority = -1 }
pedantic = { level = "warn", priority = -1 }
perf = { level = "warn", priority = -1 }
style = { level = "warn", priority = -1 }
suspicious = { level = "warn", priority = -1 }
allow_attributes_without_reason = "warn"
clone_on_ref_ptr = "warn"
empty_drop = "warn"
map_err_ignore = "warn"
redundant_type_annotations = "warn"
undocumented_unsafe_blocks = "warn"
```

**Documentation Standards**:
- All public items MUST have documentation
- Summary sentence under 15 words
- Document errors, panics, safety
- Module documentation required

**Error Handling**:
- Use `anyhow` for applications
- Use canonical error structs for libraries
- Never use panics for control flow
- Document panic conditions

---

## Integration Points

### Existing Shannon Components

Component | Location | Integration | Changes Required |
|-----------|----------|-------------|------------------|
**Gateway** | `rust/shannon-api/src/gateway/` | Add workflow control endpoints | MODIFY `routes.rs` |
**LLM Orchestrator** | `rust/shannon-api/src/llm/orchestrator.rs` | Pattern LLM calls | USE existing |
**Agent Core** | `rust/agent-core/src/` | Python tool execution | USE existing (in-process) |
**Database** | `rust/shannon-api/src/database/` | Workflow state schema | ADD `workflow_store.rs` |
**Events** | `rust/shannon-api/src/events/` | Event streaming | ENHANCE for workflows |
**Durable Worker** | `rust/durable-shannon/src/worker/` | WASM execution | ENHANCE with patterns |
**Workflow Patterns** | `rust/workflow-patterns/src/` | Pattern library | IMPLEMENT LLM integration |

### External Integrations

Service | Purpose | Configuration | Fallback |
|---------|---------|---------------|----------|
**OpenAI** | LLM provider | `OPENAI_API_KEY` | Anthropic |
**Anthropic** | LLM provider | `ANTHROPIC_API_KEY` | Google |
**Google Gemini** | LLM provider | `GOOGLE_API_KEY` | Groq |
**Groq** | LLM provider | `GROQ_API_KEY` | xAI |
**Tavily** | Web search | `TAVILY_API_KEY` | DuckDuckGo |

### Tauri Integration

**Commands to Add**:
```rust
// desktop/src-tauri/src/workflow.rs
#[tauri::command]
async fn submit_workflow(query: String, strategy: String) -> Result<WorkflowHandle, String>

#[tauri::command]
async fn stream_workflow_events(workflow_id: String) -> Result<EventStream, String>

#[tauri::command]
async fn pause_workflow(workflow_id: String) -> Result<bool, String>

#[tauri::command]
async fn resume_workflow(workflow_id: String) -> Result<bool, String>

#[tauri::command]
async fn cancel_workflow(workflow_id: String) -> Result<bool, String>

#[tauri::command]
async fn get_workflow_history() -> Result<Vec<WorkflowInfo>, String>
```

**Event Emission**:
```rust
// Emit workflow events to frontend
app.emit_all("workflow:started", WorkflowStartedEvent { /* ... */ });
app.emit_all("workflow:progress", ProgressEvent { /* ... */ });
app.emit_all("workflow:completed", CompletedEvent { /* ... */ });
```

---

## Risk Mitigation

### Technical Risks

Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
**WASM performance overhead** | Medium | Medium | Benchmark early (P2.10), optimize hot paths, consider native fallback |
**SQLite concurrency limits** | Low | Medium | Use WAL mode, connection pooling, event batching |
**LLM provider rate limits** | High | High | Multi-provider fallback, exponential backoff, token budgets |
**Memory leaks in long workflows** | Medium | High | Memory profiling in benchmarks, careful Arc/clone usage |
**Event log growth** | Medium | Medium | 7-day retention, compression, pruning strategy |
**Pattern complexity underestimation** | Medium | Medium | Incremental development, mock LLM for testing |

### Schedule Risks

Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
**Pattern implementation delays** | Medium | Medium | Parallel development (marked [P]), mock LLM responses |
**Integration issues with existing code** | Low | High | Early integration tests, incremental approach |
**Testing taking longer than planned** | High | Low | Property tests automated, benchmark suite reusable |
**Scope creep (additional patterns)** | Medium | Medium | Strict phase gating, pattern extensibility design |

### Dependency Risks

Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
**Wasmtime API changes** | Low | Medium | Pin version (0.40), monitor releases |
**rusqlite breaking changes** | Low | Low | Pin version, minimal API surface |
**LLM provider API changes** | Medium | High | Provider abstraction layer, version pinning |
**Existing code conflicts** | Low | High | Regular sync with main branch, early reviews |

---

## Success Criteria

### Functional Requirements (MVP - Phase 1)

- [x] Submit task via POST /api/v1/tasks
- [x] Execute Chain of Thought workflow with LLM
- [x] Execute Research workflow with web search & citations
- [x] Stream real-time events to UI via SSE
- [x] Persist workflow state in SQLite
- [x] Resume workflow after app restart
- [x] Pause/resume/cancel running workflows
- [x] Session continuity across sessions

### Advanced Features (Phase 2)

- [x] Tree of Thoughts with branching exploration
- [x] ReAct loops with tool observation
- [x] Debate pattern with multi-agent discussion
- [x] Reflection pattern with self-critique
- [x] Deep Research 2.0 with iterative coverage
- [x] WASM pattern execution
- [x] Complexity-based routing
- [x] Performance targets met

### Production Readiness (Phase 3)

- [x] Error recovery with retries & fallback
- [x] Workflow replay debugging
- [x] UI workflow history browser
- [x] Export/import workflow state
- [x] API compatibility with cloud (100%)
- [x] Zero compilation warnings
- [x] 90%+ test coverage
- [x] Complete documentation

### Non-Functional Requirements

**Performance**:
- ✅ Cold start <200ms (P99 <250ms)
- ✅ Simple task <5s (P95 <7s)
- ✅ Research task <15min (P95 <20min)
- ✅ Event latency <100ms (P99 <150ms)
- ✅ Memory <150MB per workflow

**Reliability**:
- ✅ 99.9% workflow completion rate
- ✅ Zero data loss on crash
- ✅ Automatic recovery from interruptions
- ✅ Graceful provider failover

**Quality**:
- ✅ 100% test coverage for core engine
- ✅ Zero compilation warnings
- ✅ Rust coding standards compliance
- ✅ API compatibility tests pass

**Usability**:
- ✅ Real-time progress indicators
- ✅ Detailed error messages
- ✅ Workflow history browsing
- ✅ Markdown/JSON export

---

## Task Summary

### Phase 1 (Weeks 1-6): 12 tasks, ~30 person-days

Task | Days | Parallel | Dependencies |
|------|------|----------|--------------|
P1.1 SQLite Event Log | 3 | No | None |
P1.2 Workflow Schema | 2 | Yes [P] | None |
P1.3 Event Bus | 2 | No | None |
P1.4 Engine Skeleton | 3 | No | P1.1, P1.2, P1.3 |
P1.5 Pattern Registry | 2 | No | P1.4 |
P1.6 CoT Pattern | 3 | No | P1.5 |
P1.7 LLM Activity | 2 | Yes [P] | None |
P1.8 Research Pattern | 4 | No | P1.5, P1.7 |
P1.9 SSE Streaming | 3 | No | P1.3, P1.4 |
P1.10 Control Signals | 2 | No | P1.4 |
P1.11 Session Continuity | 3 | No | P1.2, P1.4 |
P1.12 Recovery & Replay | 3 | No | P1.1, P1.4, P1.11 |

### Phase 2 (Weeks 7-12): 11 tasks, ~36 person-days

Task | Days | Parallel | Dependencies |
|------|------|----------|--------------|
P2.1 ToT Pattern | 4 | No | P1.5, P1.7 |
P2.2 ReAct Pattern | 3 | No | P1.5, P1.7 |
P2.3 Tool Activity | 2 | Yes [P] | None |
P2.4 Debate Pattern | 3 | No | P1.5, P1.7 |
P2.5 Reflection Pattern | 2 | No | P1.5, P1.7 |
P2.6 Deep Research 2.0 | 6 | No | P1.8, P2.3 |
P2.7 Router | 3 | No | P1.4 |
P2.8 WASM Compilation | 4 | No | P2.1-P2.5 |
P2.9 WASM Caching | 2 | Yes [P] | None |
P2.10 Benchmarks | 3 | No | P2.8 |
P2.11 Optimization | 4 | No | P2.10 |

### Phase 3 (Weeks 13-16): 11 tasks, ~32 person-days

Task | Days | Parallel | Dependencies |
|------|------|----------|--------------|
P3.1 Error Recovery | 4 | No | P1.12 |
P3.2 Replay Debugging | 3 | No | P1.1 |
P3.3 Checkpoint Opt | 2 | Yes [P] | None |
P3.4 Tauri Commands | 3 | No | P1.4 |
P3.5 History Browser | 4 | No | P3.4 |
P3.6 Progress Viz | 3 | Yes [P] | P3.4 |
P3.7 API Compat Tests | 4 | No | All P1-P2 |
P3.8 Export/Import | 3 | No | P1.2, P1.4 |
P3.9 E2E Tests | 4 | No | All P1-P3 |
P3.10 Documentation | 2 | Yes [P] | None |
P3.11 Quality Audit | 3 | No | All P1-P3 |

**Total Effort**: ~98 person-days (4 months with 2-3 engineers)

---

## Next Steps

### Immediate Actions (This Week)

1. ✅ **Review this plan** with engineering team
2. ⏳ **Set up project tracking** (GitHub milestones/issues)
3. ⏳ **Address clarifications**:
   - WASM module distribution strategy (bundle in app)
   - Agent Core integration (in-process preferred)
   - Research configuration UX (sensible defaults with advanced settings)
4. ⏳ **Environment setup**:
   - Create feature branch `feature/embedded-workflow-engine`
   - Set up CI pipeline for workflow tests
   - Configure test coverage reporting
5. ⏳ **Kickoff Phase 1** (Week 1):
   - P1.1: SQLite Event Log Backend
   - P1.2: Workflow Database Schema [P]

### Success Metrics Dashboard

Track these weekly:

Metric | Target | Week 6 | Week 12 | Week 16 |
|--------|--------|--------|---------|---------|
**Tasks Complete** | 34/34 | 12/12 | 23/23 | 34/34 |
**Test Coverage** | 90%+ | 95%+ | 92%+ | 90%+ |
**Warnings** | 0 | 0 | 0 | 0 |
**Patterns Working** | 6/6 | 2/6 | 6/6 | 6/6 |
**Cold Start (ms)** | <200 | - | <200 | <200 |
**Research Time (min)** | <15 | - | <15 | <15 |

### Milestone Checklist

**Phase 1 Milestone (End of Week 6)**:
- [ ] All Phase 1 tasks complete
- [ ] CoT and Research patterns working
- [ ] Event streaming functional
- [ ] SQLite persistence operational
- [ ] Session recovery tested
- [ ] Integration tests passing
- [ ] Demo: Submit → Stream → Recover

**Phase 2 Milestone (End of Week 12)**:
- [ ] All Phase 2 tasks complete
- [ ] All 6 patterns implemented
- [ ] WASM execution functional
- [ ] Benchmarks meet targets
- [ ] Performance optimized
- [ ] Demo: Complex workflow with ToT

**Phase 3 Milestone (End of Week 16)**:
- [ ] All Phase 3 tasks complete
- [ ] UI integration complete
- [ ] API compatibility verified
- [ ] Documentation finalized
- [ ] Quality gates passed
- [ ] Demo: Production-ready workflow engine

---

## Conclusion

This implementation plan provides a comprehensive roadmap for integrating an embedded workflow engine into Shannon's Tauri application. With disciplined execution across 3 phases over 16 weeks, we will deliver:

1. **Embedded workflow engine** with 100% cloud API parity
2. **6 cognitive patterns** (CoT, ToT, ReAct, Research, Debate, Reflection)
3. **SQLite-based durable state** with event sourcing
4. **Real-time event streaming** to Tauri UI
5. **Session continuity** across app restarts
6. **100% test coverage** for core components

**Key Success Factors**:
- Incremental development with clear milestones
- Parallel task execution where possible
- Early performance benchmarking
- Comprehensive testing strategy
- Strict adherence to Rust coding standards

**Ready to proceed to Code mode for implementation.**

---

**Document Version**: 1.0
**Last Updated**: January 12, 2026
**Reviewers**: Engineering Team
**Approval**: Pending
**Next Review**: Phase 1 completion (Week 6)