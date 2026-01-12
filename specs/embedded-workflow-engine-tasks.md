# Embedded Workflow Engine - Implementation Tasks

**Specification**: [specs/embedded-workflow-engine-spec.md](embedded-workflow-engine-spec.md)  
**Plan**: [plans/embedded-workflow-engine-implementation-plan.md](../plans/embedded-workflow-engine-implementation-plan.md)

## Overview

**Total Tasks**: 34 across 3 phases  
**Estimated Timeline**: 16 weeks (4 months)  
**Estimated Effort**: 98 person-days  
**Test Coverage Target**: 100% core engine, 90%+ overall  
**Team Size**: 2-3 engineers

## Execution Rules

1. Complete phases sequentially (Phase 1 → 2 → 3)
2. Within a phase, respect task dependencies
3. Tasks marked **[P]** can run in parallel with other tasks
4. Tasks affecting same files must be sequential
5. All tests must pass before moving to next phase
6. Zero compilation warnings required (cargo clippy -- -D warnings)
7. All code must follow [docs/coding-standards/RUST.md](../docs/coding-standards/RUST.md)

---

## Phase 1: Foundation MVP (Weeks 1-6)

**Goal**: Basic workflow execution with CoT and Research patterns, event streaming, and state persistence.

**Success Criteria**:
- ✅ Submit task via REST API
- ✅ Execute Chain of Thought workflow
- ✅ Execute Research workflow with web search
- ✅ Stream events to UI via SSE
- ✅ Persist workflow state in SQLite
- ✅ Resume workflow after app restart

---

### Task P1.1: SQLite Event Log Backend ✅ COMPLETE

**Description**: Implement SQLite-based event log storage for durable workflow execution with event sourcing pattern. This is the foundational persistence layer for all workflow state.

**Files**:
- `rust/durable-shannon/src/backends/sqlite.rs` (created ✅)
- `rust/durable-shannon/src/backends/mod.rs` (modified ✅)
- `rust/durable-shannon/src/lib.rs` (modified ✅)

**Dependencies**:
- None - this is a foundational task

**Status**: ✅ **COMPLETE**
- All files created/modified
- 12/12 tests passing
- Zero compilation warnings
- Zero clippy warnings
- 100% acceptance criteria met

**Test Requirements**:
- Unit tests:
  - Event log creation with in-memory database (`:memory:`)
  - Event log creation with file-based database
  - Event append operation with single event
  - Event append operation with batch of events (10 events)
  - Event replay by workflow_id (all events)
  - Event replay_since by workflow_id and sequence number
  - Event ordering preservation (sequence numbers)
  - Transaction rollback on error
  - Concurrent writes from multiple threads (WAL mode validation)
  - Event serialization/deserialization with bincode
- Property tests:
  - Event ordering consistency across any sequence of appends
  - Deterministic replay produces same event order
  - Event count invariant (append N events → replay returns N events)
- Performance tests:
  - Batch insert throughput (target >1000 events/sec)
  - Replay performance with 10K events (target <100ms)
- Coverage target: **100%**
- Test file: `rust/durable-shannon/tests/backends/sqlite_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] SqliteEventLog struct implements EventLog trait
- [ ] Schema migration creates workflow_events table with proper indexes
- [ ] Tests pass with 100% coverage
- [ ] WAL mode enabled for concurrent reads/writes
- [ ] Event batching implemented (configurable batch size, default 10)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete for all public items

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P1.2: Workflow Database Schema ✅ COMPLETE

**Description**: Create SQLite database schema for workflow metadata, checkpoints, and state management. This complements the event log with structured workflow information.

**Files**:
- `rust/shannon-api/src/database/workflow_store.rs` (created ✅)
- `rust/shannon-api/src/database/mod.rs` (modified ✅)
- `rust/shannon-api/Cargo.toml` (modified - added crc32fast ✅)

**Dependencies**:
- None - can run parallel with P1.1

**Status**: ✅ **COMPLETE**
- All files created/modified
- 12/12 tests passing
- Zero compilation warnings
- Zero clippy warnings
- 100% acceptance criteria met
- Workflow CRUD operations implemented
- Checkpoint save/load with CRC32 verification
- Session association support

**Test Requirements**:
- Unit tests:
  - Workflow CRUD operations (create, read, update, delete)
  - Workflow list by status (pending, running, completed, failed)
  - Workflow search by session_id
  - Workflow checkpoint save and load
  - Checkpoint compression with zstd
  - Checkpoint corruption detection with checksums
  - Transaction handling for atomic operations
  - Migration up/down operations
  - Index usage verification (EXPLAIN QUERY PLAN)
- Integration tests:
  - Create workflow → save checkpoint → load checkpoint → verify state
  - Concurrent workflow creation from multiple threads
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/database/workflow_store_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] All three tables created: workflows, workflow_events, workflow_checkpoints
- [ ] Indexes created for efficient querying (workflow_id, status, session_id)
- [ ] Repository pattern implemented for CRUD operations
- [ ] Tests pass with 100% coverage
- [ ] Migration scripts versioned and idempotent
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with schema diagram

**Parallel**: **[P]** - Can run parallel with P1.1

**Estimated Duration**: 2 days

---

### Task P1.3: Event Bus Infrastructure ✅ COMPLETE

**Description**: Implement event bus using tokio broadcast channels for real-time event streaming from workflows to UI. This enables live progress updates.

**Files**:
- `rust/shannon-api/src/workflow/embedded/event_bus.rs` (created ✅)
- `rust/shannon-api/src/workflow/embedded/mod.rs` (created ✅)
- `rust/shannon-api/src/workflow/mod.rs` (modified ✅)

**Dependencies**:
- None - foundational infrastructure

**Status**: ✅ **COMPLETE**
- All files created/modified
- 11/11 tests passing
- Zero compilation warnings
- Zero clippy warnings
- 21+ event types defined (cloud parity)
- Pub/sub with tokio broadcast channels
- Backpressure handling
- Multi-subscriber support

**Test Requirements**:
- Unit tests:
  - EventBus creation and initialization
  - Subscribe to workflow events (single subscriber)
  - Subscribe to workflow events (multiple subscribers)
  - Broadcast event to all subscribers
  - Unsubscribe from workflow events
  - Channel cleanup on workflow completion
  - Backpressure handling (slow consumer)
  - Event filtering by type (persistent vs ephemeral)
  - Channel capacity enforcement (256 events)
  - Graceful degradation when channel full
- Integration tests:
  - Workflow emits events → EventBus → multiple subscribers receive
  - Subscriber disconnect doesn't affect other subscribers
  - High-throughput stress test (10K events/sec)
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/unit/event_bus_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] EventBus struct with tokio broadcast channels
- [ ] WorkflowEvent enum with 26+ event types (matching cloud)
- [ ] Event persistence strategy (ephemeral vs persistent) implemented
- [ ] Tests pass with 100% coverage
- [ ] Backpressure handling prevents memory leaks
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with event flow diagram

**Parallel**: No

**Estimated Duration**: 2 days

---

### Task P1.4: EmbeddedWorkflowEngine Skeleton

**Description**: Create the core EmbeddedWorkflowEngine struct that coordinates all workflow operations. This is the main orchestration component that integrates all other pieces.

**Files**:
- `rust/shannon-api/src/workflow/embedded/engine.rs` (create)
- `rust/shannon-api/src/workflow/mod.rs` (modify - export embedded module)
- `rust/shannon-api/src/workflow/engine.rs` (modify - integrate EmbeddedWorkflowEngine)

**Dependencies**: 
- P1.1 (SQLite Event Log)
- P1.2 (Workflow State Schema)
- P1.3 (Event Bus)

**Test Requirements**:
- Unit tests:
  - EmbeddedWorkflowEngine initialization
  - submit_task() method with valid input
  - submit_task() method with invalid input
  - stream_events() method returns event stream
  - pause_workflow() method transitions state to paused
  - resume_workflow() method resumes execution
  - cancel_workflow() method terminates workflow
  - Health check method returns engine status
  - Concurrent workflow submission (5 workflows)
  - Max concurrent workflows enforcement
- Integration tests:
  - Submit task → workflow starts → events emitted
  - Submit task → pause → resume → complete
  - Submit task → cancel → workflow terminates
  - Engine initialization with all dependencies
  - Engine shutdown gracefully stops all workflows
- Coverage target: **90%**
- Test file: `rust/shannon-api/tests/workflow/integration/engine_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] EmbeddedWorkflowEngine struct created with all dependencies
- [ ] All core methods implemented (submit, stream, pause, resume, cancel)
- [ ] Integration with DurableWorker from durable-shannon
- [ ] Tests pass with 90%+ coverage
- [ ] Error handling for all operations
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with architecture diagram

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P1.5: Pattern Registry

**Description**: Create pattern registry for managing and executing cognitive patterns (CoT, ToT, Research, etc.). This provides a unified interface for pattern execution.

**Files**:
- `rust/shannon-api/src/workflow/patterns/mod.rs` (create)
- `rust/shannon-api/src/workflow/patterns/base.rs` (create - trait definitions)

**Dependencies**: 
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - PatternRegistry creation
  - Register native pattern (CoT)
  - Register multiple patterns
  - Pattern lookup by name (existing pattern)
  - Pattern lookup by name (non-existent pattern returns error)
  - Pattern execution wrapper with timing
  - Pattern execution wrapper with metrics tracking
  - Error handling during pattern execution
  - Retry logic on transient failures (3 retries max)
  - Pattern execution timeout enforcement
- Integration tests:
  - Register pattern → execute → verify result
  - Execute multiple patterns concurrently
  - Pattern execution with mock LLM client
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/unit/pattern_registry_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] PatternRegistry struct with HashMap storage
- [ ] CognitivePattern trait defined
- [ ] Pattern registration and lookup methods
- [ ] Pattern execution wrapper with timing and metrics
- [ ] Tests pass with 100% coverage
- [ ] Error handling and retry logic
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with usage examples

**Parallel**: No

**Estimated Duration**: 2 days

---

### Task P1.6: Chain of Thought Pattern

**Description**: Implement Chain of Thought (CoT) pattern with LLM integration for step-by-step reasoning. This is the first cognitive pattern implementation.

**Files**:
- `rust/shannon-api/src/workflow/patterns/chain_of_thought.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export CoT)

**Dependencies**: 
- P1.5 (Pattern Registry)

**Test Requirements**:
- Unit tests:
  - ChainOfThought pattern creation with defaults
  - ChainOfThought pattern with custom max_iterations
  - ChainOfThought pattern with custom model
  - Step-by-step reasoning execution (mock LLM)
  - ReasoningStep tracking with timestamps
  - Token usage tracking across all steps
  - Max iterations enforcement (default 5)
  - Early termination on answer found
- Integration tests:
  - Execute CoT with OpenAI API (simple query: "What is 2+2?")
  - Execute CoT with Anthropic API
  - Execute CoT with complex reasoning task
  - Verify reasoning steps are recorded
  - Verify token usage is tracked
  - Progress event emission (one per reasoning step)
- Mock tests:
  - Mock LLM responses for deterministic testing
  - Mock LLM failure and retry behavior
  - Mock LLM timeout handling
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::cot`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] ChainOfThought struct implements CognitivePattern trait
- [ ] Integration with LLM Orchestrator (call_llm activity)
- [ ] Step-by-step reasoning with configurable max_iterations
- [ ] ReasoningStep tracking with timestamps and confidence
- [ ] Progress events emitted for each step
- [ ] Tests pass with 95%+ coverage
- [ ] Works with real LLM APIs (OpenAI, Anthropic)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with reasoning examples

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P1.7: LLM Activity Implementation

**Description**: Implement complete LLM activity for pattern execution, including streaming, retry logic, and timeout handling. This is the bridge between patterns and LLM providers.

**Files**:
- `rust/durable-shannon/src/activities/llm.rs` (enhance existing)
- `rust/durable-shannon/src/activities/mod.rs` (modify - export llm)

**Dependencies**: 
- None - can run parallel with P1.6

**Test Requirements**:
- Unit tests:
  - call_llm() activity with valid request
  - call_llm() activity with invalid request
  - ActivityContext dependency injection
  - LlmRequest/LlmResponse serialization
  - Streaming support via tokio channels
  - Retry logic with exponential backoff (max 3 retries)
  - Timeout handling (default 30s)
  - Timeout handling for research (5 min)
  - Provider failover on persistent failure
  - Token usage tracking
- Mock tests:
  - Mock LLM client for deterministic responses
  - Mock network failure → retry → success
  - Mock timeout → fallback to faster model
  - Mock rate limit → exponential backoff
- Coverage target: **100%**
- Test file: `rust/durable-shannon/tests/activities/llm_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] call_llm() activity fully implemented
- [ ] ActivityContext for dependency injection
- [ ] Streaming support with tokio channels
- [ ] Retry logic with exponential backoff
- [ ] Timeout handling (configurable)
- [ ] Tests pass with 100% coverage
- [ ] Integration with shannon-api LLM Orchestrator
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with error handling guide

**Parallel**: **[P]** - Can run parallel with P1.6

**Estimated Duration**: 2 days

---

### Task P1.8: Research Pattern

**Description**: Implement Research pattern with web search integration, source collection, and synthesis. This enables autonomous information gathering workflows.

**Files**:
- `rust/shannon-api/src/workflow/patterns/research.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export Research)

**Dependencies**: 
- P1.5 (Pattern Registry)
- P1.7 (LLM Activity)

**Test Requirements**:
- Unit tests:
  - Research pattern creation with defaults
  - Query decomposition step (mock LLM)
  - Web search integration (mock search API)
  - Source collection and deduplication
  - Synthesis step with citations (mock LLM)
  - Confidence scoring (0.0-1.0 range)
  - Min sources enforcement (default 8)
  - Sources per round configuration (default 6)
- Integration tests:
  - Execute Research with real web search (Tavily API)
  - Execute Research with real LLM synthesis
  - Verify citations in final report
  - Verify confidence score calculation
  - Verify source deduplication works
  - Progress events emitted (decomposition, search, synthesis)
- Mock tests:
  - Mock web search responses
  - Mock LLM decomposition and synthesis
  - Mock search API failures with retry
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::research`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Research pattern implements CognitivePattern trait
- [ ] Query decomposition step implemented
- [ ] Web search integration (Tavily API)
- [ ] Source collection and deduplication
- [ ] Synthesis with citations
- [ ] Confidence scoring
- [ ] Tests pass with 95%+ coverage
- [ ] Works with real web search API
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with research workflow diagram

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P1.9: SSE Streaming Endpoint

**Description**: Create Server-Sent Events (SSE) endpoint for streaming workflow events to the UI in real-time. This enables live progress visualization.

**Files**:
- `rust/shannon-api/src/gateway/routes.rs` (modify - add streaming endpoint)
- `rust/shannon-api/src/gateway/streaming.rs` (create - SSE implementation)

**Dependencies**: 
- P1.3 (Event Bus)
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - SSE endpoint registration
  - WorkflowEvent to SSE format conversion
  - SSE keep-alive heartbeat (every 15s)
  - Event filtering by type (query parameter)
  - Graceful disconnect handling
  - Multiple concurrent SSE connections
- Integration tests:
  - Submit workflow → subscribe to SSE → receive events
  - SSE connection with real EventSource client
  - Multiple clients subscribe to same workflow
  - Client disconnect doesn't affect workflow
  - Event ordering preserved in SSE stream
- Load tests:
  - 100 concurrent SSE connections
  - High-throughput event streaming (1K events/sec per connection)
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/streaming_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] GET /api/v1/tasks/{id}/stream endpoint implemented
- [ ] WorkflowEvent mapped to SSE format (data: {json})
- [ ] Keep-alive heartbeat every 15 seconds
- [ ] Graceful disconnect handling
- [ ] Event filtering by type (optional query param)
- [ ] Tests pass with 100% coverage
- [ ] Load test passes (100 concurrent connections)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with SSE protocol details

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P1.10: Workflow Control Signals

**Description**: Implement pause, resume, and cancel control endpoints for managing running workflows. This enables user control over workflow execution.

**Files**:
- `rust/shannon-api/src/workflow/control.rs` (modify/create)
- `rust/shannon-api/src/gateway/routes.rs` (modify - add control endpoints)

**Dependencies**: 
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - Pause workflow (running → paused transition)
  - Pause workflow (invalid state transitions)
  - Resume workflow (paused → running transition)
  - Resume workflow (invalid state transitions)
  - Cancel workflow (any state → cancelled transition)
  - Control signal propagation to worker
  - Concurrent control signal handling
- Integration tests:
  - Submit workflow → pause → verify paused state
  - Paused workflow → resume → verify running state
  - Running workflow → cancel → verify cancelled state
  - Control signals emit appropriate events (WORKFLOW_PAUSING, WORKFLOW_PAUSED, etc.)
  - Pause during LLM call → wait for completion → pause
  - Cancel during LLM call → immediate termination
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/control_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] POST /api/v1/tasks/{id}/pause endpoint implemented
- [ ] POST /api/v1/tasks/{id}/resume endpoint implemented
- [ ] POST /api/v1/tasks/{id}/cancel endpoint implemented
- [ ] Control signal propagation to durable-shannon worker
- [ ] Appropriate events emitted (PAUSING, PAUSED, CANCELLING, CANCELLED)
- [ ] Tests pass with 100% coverage
- [ ] State transitions validated
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with state machine diagram

**Parallel**: No

**Estimated Duration**: 2 days

---

### Task P1.11: Session Continuity

**Description**: Implement session management for workflow-session association and conversation history persistence. This enables context continuity across app sessions.

**Files**:
- `rust/shannon-api/src/workflow/embedded/session.rs` (create)
- `rust/shannon-api/src/database/schema.rs` (modify - add sessions table)

**Dependencies**: 
- P1.2 (Workflow Database Schema)
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - SessionManager creation
  - Create session with user_id
  - Get session by session_id
  - Update session context (key-value storage)
  - Associate workflow with session
  - List active sessions
  - Delete expired sessions (cleanup)
  - Conversation history storage and retrieval
- Integration tests:
  - Create session → associate workflow → get session → verify workflow_id
  - Submit workflow → close app → reopen → resume from session
  - Multiple workflows in same session
  - Session context preserved across app restarts
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/session_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] SessionManager struct implemented
- [ ] Session CRUD operations (create, get, update, delete)
- [ ] Workflow-session association
- [ ] Conversation history storage
- [ ] Context key-value storage
- [ ] Tests pass with 100% coverage
- [ ] Session persistence in SQLite
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with session lifecycle diagram

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P1.12: Recovery & Replay

**Description**: Implement workflow recovery and replay for crash resilience and debugging. This ensures workflows can resume after app crashes.

**Files**:
- `rust/shannon-api/src/workflow/embedded/recovery.rs` (create)

**Dependencies**: 
- P1.1 (SQLite Event Log)
- P1.4 (EmbeddedWorkflowEngine)
- P1.11 (Session Continuity)

**Test Requirements**:
- Unit tests:
  - recover_workflow() with checkpoint present
  - recover_workflow() without checkpoint (full replay)
  - Replay events since checkpoint
  - Reconstruct workflow state from events
  - Resume execution from last successful state
  - Handle corrupted checkpoint (fallback to full replay)
  - Checkpoint creation on major phase completion
  - Checkpoint creation on workflow pause
- Integration tests:
  - Submit workflow → kill app mid-execution → restart → recover → complete
  - Submit workflow → checkpoint → crash → replay from checkpoint
  - Multiple workflows recover on startup
  - Corrupted checkpoint → full replay fallback
- Property tests:
  - Replay determinism (same events → same final state)
  - Checkpoint invariants (checkpoint + replay_since ≡ full replay)
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/recovery_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] recover_workflow() method implemented
- [ ] Checkpoint loading and event replay
- [ ] State reconstruction from events
- [ ] Resume execution from last successful state
- [ ] Corrupted checkpoint handling
- [ ] Tests pass with 100% coverage
- [ ] Property tests verify deterministic replay
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with recovery flow diagram

**Parallel**: No

**Estimated Duration**: 3 days

---

## Phase 2: Advanced Features (Weeks 7-12)

**Goal**: Advanced patterns (ToT, ReAct, Debate), Deep Research 2.0, WASM execution, performance optimization.

**Success Criteria**:
- ✅ All 6 cognitive patterns working
- ✅ Deep Research 2.0 with iterative coverage
- ✅ WASM module execution
- ✅ Performance benchmarks meet targets
- ✅ Memory usage <150MB per workflow

---

### Task P2.1: Tree of Thoughts Pattern

**Description**: Implement Tree of Thoughts (ToT) pattern with branching exploration, evaluation, and backtracking. This enables exploring multiple solution paths.

**Files**:
- `rust/shannon-api/src/workflow/patterns/tree_of_thoughts.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export ToT)

**Dependencies**: 
- P1.5 (Pattern Registry)
- P1.7 (LLM Activity)

**Test Requirements**:
- Unit tests:
  - TreeOfThoughts pattern creation with defaults
  - Thought tree data structure (Node with children)
  - Branch generation (N candidates per step)
  - Evaluation function (score each branch 0-1)
  - Backtracking logic (keep top K branches)
  - Breadth-first exploration
  - Depth-first exploration
  - Final answer synthesis from best path
  - Pruning low-score branches (threshold 0.3)
- Integration tests:
  - Execute ToT with complex reasoning task (e.g., "Plan a 3-day trip to Paris")
  - Verify multiple branches explored
  - Verify best path selected based on scores
  - Verify final answer quality
  - Progress events emitted for each branch explored
- Mock tests:
  - Mock LLM responses for branch generation
  - Mock evaluation scores for deterministic testing
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::tot`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] TreeOfThoughts implements CognitivePattern trait
- [ ] Thought tree data structure with branching
- [ ] Branch generation and evaluation
- [ ] Backtracking and pruning
- [ ] Both BFS and DFS exploration modes
- [ ] Tests pass with 95%+ coverage
- [ ] Works with real LLM APIs
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with tree visualization example

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P2.2: ReAct Pattern

**Description**: Implement ReAct (Reason-Act-Observe) pattern for multi-step tool usage with feedback loops. This enables autonomous task completion with tools.

**Files**:
- `rust/shannon-api/src/workflow/patterns/react.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export ReAct)

**Dependencies**: 
- P1.5 (Pattern Registry)
- P1.7 (LLM Activity)

**Test Requirements**:
- Unit tests:
  - ReAct pattern creation with defaults
  - Reason step (LLM generates action plan)
  - Act step (execute tool with params)
  - Observe step (record tool output)
  - Loop logic (max iterations, default 5)
  - Halting condition detection ("FINAL ANSWER:" marker)
  - Tool parameter extraction from LLM response
  - Tool output formatting
- Integration tests:
  - Execute ReAct with multi-step tool usage (e.g., "What's the weather in the capital of France?")
  - Verify reason-act-observe loop executes
  - Verify tool invocations are logged
  - Verify halting condition stops loop
  - Progress events emitted for each step
- Mock tests:
  - Mock LLM responses for action planning
  - Mock tool executions (web_search, calculator)
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::react`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] ReAct implements CognitivePattern trait
- [ ] Reason-Act-Observe loop implemented
- [ ] Tool integration (via execute_tool activity)
- [ ] Halting condition detection
- [ ] Max iterations enforcement
- [ ] Tests pass with 95%+ coverage
- [ ] Works with real tools (web_search, calculator)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with ReAct flow diagram

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P2.3: Tool Activity Implementation

**Description**: Implement comprehensive tool execution activity with built-in tools and Python tool delegation. This is the execution layer for all tool calls.

**Files**:
- `rust/durable-shannon/src/activities/tools.rs` (enhance existing)
- `rust/durable-shannon/src/activities/mod.rs` (modify - export tools)

**Dependencies**: 
- None - can run parallel with P2.2

**Test Requirements**:
- Unit tests:
  - execute_tool() activity with web_search
  - execute_tool() activity with web_fetch
  - execute_tool() activity with calculator
  - execute_tool() activity with Python tool (delegate to agent-core)
  - Tool registry lookup
  - Tool parameter validation
  - Tool output formatting
  - Timeout handling per tool (default 30s)
  - Tool execution error handling
- Mock tests:
  - Mock web search API responses
  - Mock agent-core tool execution
  - Mock tool timeout scenarios
- Coverage target: **100%**
- Test file: `rust/durable-shannon/tests/activities/tools_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] execute_tool() activity fully implemented
- [ ] Built-in tools: web_search, web_fetch, calculator
- [ ] Python tool delegation to agent-core
- [ ] Tool parameter validation
- [ ] Timeout handling (configurable per tool)
- [ ] Tests pass with 100% coverage
- [ ] Integration with shannon-api tool registry
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with tool catalog

**Parallel**: **[P]** - Can run parallel with P2.2

**Estimated Duration**: 2 days

---

### Task P2.4: Debate Pattern

**Description**: Implement Debate pattern with multi-agent discussion, critique cycles, and consensus synthesis. This enables exploring multiple perspectives.

**Files**:
- `rust/shannon-api/src/workflow/patterns/debate.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export Debate)

**Dependencies**: 
- P1.5 (Pattern Registry)
- P1.7 (LLM Activity)

**Test Requirements**:
- Unit tests:
  - Debate pattern creation with defaults
  - Multi-agent architecture (2-4 debaters)
  - Round-based discussion (max 3 rounds)
  - Perspective generation (each agent argues position)
  - Critique and response cycle
  - Synthesis step (final answer from debate)
  - Parallel agent execution with tokio::spawn
- Integration tests:
  - Execute Debate with philosophical question (e.g., "Is AI consciousness possible?")
  - Verify multiple perspectives generated
  - Verify critique-response cycles
  - Verify final synthesis quality
  - Progress events emitted for each round
- Mock tests:
  - Mock LLM responses for different debate positions
  - Mock parallel agent execution
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::debate`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Debate implements CognitivePattern trait
- [ ] Multi-agent architecture (configurable 2-4 agents)
- [ ] Round-based discussion logic
- [ ] Critique and response cycles
- [ ] Final synthesis from all perspectives
- [ ] Parallel agent execution
- [ ] Tests pass with 95%+ coverage
- [ ] Works with real LLM APIs
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with debate flow diagram

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P2.5: Reflection Pattern

**Description**: Implement Reflection pattern with self-critique and iterative improvement. This enables quality improvement through self-evaluation.

**Files**:
- `rust/shannon-api/src/workflow/patterns/reflection.rs` (create)
- `rust/shannon-api/src/workflow/patterns/mod.rs` (modify - export Reflection)

**Dependencies**: 
- P1.5 (Pattern Registry)
- P1.7 (LLM Activity)

**Test Requirements**:
- Unit tests:
  - Reflection pattern creation with defaults
  - Initial answer generation
  - Self-critique step (identify weaknesses)
  - Improvement step (generate better answer)
  - Quality gating (threshold 0.5)
  - Max reflection iterations (default 3)
  - Early termination on quality threshold
- Integration tests:
  - Execute Reflection with improvable answer
  - Verify self-critique identifies issues
  - Verify improved answer is better quality
  - Verify max iterations enforcement
  - Progress events emitted for each reflection cycle
- Mock tests:
  - Mock LLM responses for critique and improvement
  - Mock quality scores
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/pattern_test.rs::reflection`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Reflection implements CognitivePattern trait
- [ ] Initial answer generation
- [ ] Self-critique and improvement loop
- [ ] Quality gating (threshold 0.5)
- [ ] Max iterations enforcement
- [ ] Tests pass with 95%+ coverage
- [ ] Demonstrates quality improvement
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with reflection examples

**Parallel**: No

**Estimated Duration**: 2 days

---

### Task P2.6: Deep Research 2.0 Implementation

**Description**: Enhance Research pattern with iterative coverage loop, gap identification, and verification. This implements Manus.ai-inspired autonomous research.

**Files**:
- `rust/shannon-api/src/workflow/patterns/research.rs` (enhance)
- `rust/shannon-api/src/workflow/patterns/research/coverage.rs` (create - coverage evaluation)
- `rust/shannon-api/src/workflow/patterns/research/verification.rs` (create - fact checking)

**Dependencies**: 
- P1.8 (Research Pattern)
- P2.3 (Tool Activity)

**Test Requirements**:
- Unit tests:
  - Iterative coverage loop (max 3 iterations)
  - Coverage evaluation with LLM (score 0-1)
  - Coverage threshold enforcement (0.8)
  - Gap identification step
  - Additional sub-question generation
  - Source accumulation and deduplication
  - Fact extraction (optional, flag-gated)
  - Claim verification (optional, flag-gated)
  - Final report synthesis
- Integration tests:
  - Execute Deep Research 2.0 with complex query (15 min task)
  - Verify iterative coverage improvement
  - Verify gap identification works
  - Verify final coverage score meets threshold
  - Verify fact extraction when enabled
  - Verify claim verification when enabled
  - Performance: complete in <15 minutes
- Mock tests:
  - Mock coverage evaluation scores
  - Mock gap identification
  - Mock fact extraction and verification
- Coverage target: **95%**
- Test file: `rust/shannon-api/tests/workflow/integration/deep_research_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Iterative coverage loop implemented
- [ ] Coverage evaluation with LLM scoring
- [ ] Gap identification and additional questions
- [ ] Source accumulation and deduplication
- [ ] Optional fact extraction (flag-gated)
- [ ] Optional claim verification (flag-gated)
- [ ] Tests pass with 95%+ coverage
- [ ] Performance target met (<15 min for complex research)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with coverage loop diagram

**Parallel**: No

**Estimated Duration**: 6 days

---

### Task P2.7: Complexity Analysis & Routing

**Description**: Implement complexity analysis heuristics and workflow routing logic for automatic pattern selection. This enables intelligent workflow dispatching.

**Files**:
- `rust/shannon-api/src/workflow/embedded/router.rs` (create)

**Dependencies**: 
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - analyze_complexity() with simple query (score <0.3)
  - analyze_complexity() with medium query (score 0.3-0.7)
  - analyze_complexity() with complex query (score >0.7)
  - Routing logic for simple queries (→ SimpleTask)
  - Routing logic for medium queries (→ CoT or Research)
  - Routing logic for complex queries (→ ToT or Debate)
  - Mode override (mode="simple" forces SimpleTask)
  - Strategy override (cognitive_strategy="exploratory" forces ToT)
  - Routing decision logging
- Integration tests:
  - Submit simple query → verify SimpleTask selected
  - Submit research query → verify Research selected
  - Submit complex query → verify ToT selected
  - Override mode → verify override respected
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/unit/router_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] analyze_complexity() method with heuristics (0-1 scale)
- [ ] Routing logic for all complexity levels
- [ ] Mode override support
- [ ] Strategy override support (cognitive_strategy)
- [ ] Tests pass with 100% coverage
- [ ] Routing decisions logged for observability
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with routing decision tree

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P2.8: WASM Module Compilation

**Description**: Configure WASM target and compile cognitive patterns to WASM modules for sandboxed execution. This enables isolated pattern execution.

**Files**:
- `rust/workflow-patterns/Cargo.toml` (modify - add wasm32-wasi target)
- `rust/workflow-patterns/build.rs` (create - WASM build script)
- `.cargo/config.toml` (modify - WASM build config)

**Dependencies**: 
- P2.1 (ToT Pattern)
- P2.2 (ReAct Pattern)
- P2.4 (Debate Pattern)
- P2.5 (Reflection Pattern)

**Test Requirements**:
- Unit tests:
  - WASM module compilation succeeds
  - WASM module validation (wasmtime validate)
  - WASM module loading into Wasmtime
  - WASM module instantiation
- Integration tests:
  - Compile CoT pattern to WASM
  - Load WASM module and execute
  - Submit task → WASM pattern execution → result
  - Verify WASM execution matches native execution
  - Test all patterns compiled to WASM
- Performance tests:
  - Benchmark: native vs WASM execution time
  - WASM module load time (target <50ms)
- Coverage target: **90%**
- Test file: `rust/shannon-api/tests/workflow/integration/wasm_execution_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] wasm32-wasi target configured in Cargo.toml
- [ ] All patterns compile to WASM successfully
- [ ] WASM modules validated with wasmtime
- [ ] WASM execution matches native execution results
- [ ] Tests pass with 90%+ coverage
- [ ] Performance overhead acceptable (<2x native)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with WASM build guide

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P2.9: WASM Module Caching

**Description**: Implement LRU cache for compiled WASM modules with preloading and lazy loading. This optimizes WASM module loading performance.

**Files**:
- `rust/durable-shannon/src/worker/cache.rs` (create)
- `rust/durable-shannon/src/worker/mod.rs` (modify - integrate cache)

**Dependencies**: 
- None - can run parallel with P2.8

**Test Requirements**:
- Unit tests:
  - WasmCache creation with size limit
  - Module preloading on startup
  - Module lazy loading on first use
  - LRU eviction when cache full
  - Cache hit scenario
  - Cache miss scenario
  - Size limit enforcement (100MB)
  - Module versioning and invalidation
  - Thread-safe access (concurrent reads)
- Integration tests:
  - Preload modules → verify loaded
  - First access → lazy load → verify cached
  - Cache full → evict LRU → verify evicted
  - Multiple threads access cache concurrently
- Performance tests:
  - Cache hit latency (target <1ms)
  - Cache miss latency (lazy load)
- Coverage target: **100%**
- Test file: `rust/durable-shannon/tests/worker/wasm_cache_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] WasmCache with LRU eviction
- [ ] Module preloading on startup
- [ ] Lazy loading on first use
- [ ] Size limit enforcement (100MB)
- [ ] Module versioning support
- [ ] Tests pass with 100% coverage
- [ ] Thread-safe concurrent access
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with caching strategy

**Parallel**: **[P]** - Can run parallel with P2.8

**Estimated Duration**: 2 days

---

### Task P2.10: Performance Benchmarks

**Description**: Create comprehensive performance benchmark suite using Criterion for measuring cold start, latency, throughput, and memory usage.

**Files**:
- `rust/shannon-api/benches/cold_start_bench.rs` (create)
- `rust/shannon-api/benches/task_latency_bench.rs` (create)
- `rust/shannon-api/benches/event_throughput_bench.rs` (create)
- `rust/shannon-api/benches/memory_bench.rs` (create)

**Dependencies**: 
- P2.8 (WASM Module Compilation)

**Test Requirements**:
- Benchmarks (Criterion):
  - Cold start: engine initialization time (target <200ms, accept <250ms P99)
  - Simple task: CoT pattern end-to-end (target <5s, accept <7s P95)
  - Research task: Deep Research 2.0 (target <15min, accept <20min P95)
  - Event latency: event emit → UI receive (target <100ms, accept <150ms P99)
  - Event throughput: events written to SQLite (target >5K/s, accept >3K/s sustained)
  - Memory per workflow: RSS measurement (target <150MB, accept <200MB max)
  - Concurrent workflows: max concurrent (target 5-10 based on cores)
- Coverage: N/A (benchmarks)
- Test file: N/A (benchmarks in benches/)

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] All 7 benchmarks implemented with Criterion
- [ ] Cold start benchmark meets P99 <250ms
- [ ] Simple task benchmark meets P95 <7s
- [ ] Research task benchmark meets P95 <20min
- [ ] Event latency benchmark meets P99 <150ms
- [ ] Throughput benchmark meets >3K/s sustained
- [ ] Memory benchmark measures RSS accurately
- [ ] Criterion HTML reports generated
- [ ] Documentation complete with performance targets

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P2.11: Optimization Pass

**Description**: Profile and optimize hot paths identified in benchmarks. Implement event batching, connection pooling, memory pooling, and parallel execution.

**Files**:
- Various files (refactoring based on profiling)
- `rust/shannon-api/src/workflow/embedded/optimizations.rs` (create - optimization utilities)

**Dependencies**: 
- P2.10 (Performance Benchmarks)

**Test Requirements**:
- All existing tests must pass after optimizations
- Performance regression tests:
  - Re-run all benchmarks
  - Verify improvements over baseline
  - Ensure no correctness regressions
- Property tests:
  - Verify event batching maintains ordering
  - Verify memory pooling doesn't leak
  - Verify parallel execution produces same results
- Coverage: Maintain 90%+ overall

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Event batching implemented (10 events/batch)
- [ ] SQLite connection pooling (r2d2)
- [ ] Memory pooling for event buffers
- [ ] Parallel pattern execution (rayon for independent steps)
- [ ] All tests pass (no regressions)
- [ ] Benchmarks show improvement over baseline
- [ ] Memory leaks eliminated (valgrind clean)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with optimization techniques

**Parallel**: No

**Estimated Duration**: 4 days

---

## Phase 3: Production Ready (Weeks 13-16)

**Goal**: Error recovery, debugging tools, UI integration, end-to-end testing, production deployment.

**Success Criteria**:
- ✅ Comprehensive error recovery
- ✅ Workflow replay debugging
- ✅ UI workflow history browser
- ✅ API compatibility verified
- ✅ Zero compilation warnings
- ✅ Complete documentation

---

### Task P3.1: Error Recovery & Retry

**Description**: Implement comprehensive error recovery with retry logic, exponential backoff, provider fallback, and circuit breaker. This ensures workflow resilience.

**Files**:
- `rust/shannon-api/src/workflow/embedded/recovery.rs` (enhance)
- `rust/shannon-api/src/workflow/embedded/circuit_breaker.rs` (create)

**Dependencies**: 
- P1.12 (Recovery & Replay)

**Test Requirements**:
- Unit tests:
  - Transient error classification (network, timeout, rate limit)
  - Exponential backoff calculation (2^n seconds, max 3 retries)
  - Fallback provider selection logic
  - Checkpoint on each retry
  - Circuit breaker open/closed/half-open states
  - Circuit breaker threshold enforcement (5 failures)
  - Graceful degradation (simpler pattern on repeated failure)
- Integration tests:
  - Network failure → retry → success
  - Persistent failure → fallback provider
  - Rate limit → exponential backoff → resume
  - Circuit breaker opens after 5 failures
  - Circuit breaker half-open test after cooldown
  - Error events emitted (ACTIVITY_FAILED, RETRY_SCHEDULED)
- Mock tests:
  - Mock transient network failures
  - Mock LLM provider failures
  - Mock rate limit responses
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/error_recovery_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Transient error classification implemented
- [ ] Exponential backoff retry (max 3 retries)
- [ ] Fallback provider on persistent failure
- [ ] Checkpoint on each retry
- [ ] Circuit breaker for failing providers
- [ ] Graceful degradation strategy
- [ ] Tests pass with 100% coverage
- [ ] Error events emitted appropriately
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with error recovery flow

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P3.2: Workflow Replay Debugging

**Description**: Implement workflow replay capability for deterministic debugging and time-travel debugging. This enables developers to debug failed workflows.

**Files**:
- `rust/shannon-api/src/workflow/embedded/replay.rs` (create)
- `rust/shannon-api/src/bin/workflow_replay.rs` (create - CLI tool)

**Dependencies**: 
- P1.1 (SQLite Event Log)

**Test Requirements**:
- Unit tests:
  - Export workflow history to JSON file
  - Import workflow from JSON file
  - Replay workflow from JSON history
  - Step-through debugging mode
  - Breakpoint at specific event sequence
  - State inspection at any point in history
  - Deterministic replay verification
- Integration tests:
  - Execute workflow → export → replay → verify identical result
  - Execute workflow → export → replay with breakpoints → inspect state
  - Failed workflow → export → replay → identify failure point
- Property tests:
  - Replay determinism (same events → same final state)
  - Export-import round-trip (workflow ≡ export ≡ import ≡ replay)
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/replay_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Export workflow history to JSON
- [ ] Import and replay workflow from JSON
- [ ] Step-through debugging mode
- [ ] Breakpoint support at event sequences
- [ ] State inspection capability
- [ ] CLI tool for replay debugging
- [ ] Tests pass with 100% coverage
- [ ] Deterministic replay verified
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with replay debugging guide

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P3.3: Checkpoint Optimization

**Description**: Optimize checkpoint strategy with adaptive frequency, compression, incremental checkpoints, and corruption detection. This improves recovery performance.

**Files**:
- `rust/durable-shannon/src/worker/checkpoint.rs` (enhance)

**Dependencies**: 
- None - can run parallel with P3.2

**Test Requirements**:
- Unit tests:
  - Adaptive checkpoint frequency (based on event count)
  - Compression with zstd level 3
  - Compression ratio measurement
  - Incremental checkpoints (delta encoding)
  - Checkpoint pruning (keep last 3)
  - Corruption detection with CRC32 checksums
  - Corrupt checkpoint recovery (fallback to previous)
- Integration tests:
  - Long-running workflow → adaptive checkpointing
  - Checkpoint compression → decompression → verify state
  - Incremental checkpoint → apply deltas → verify state
  - Corrupted checkpoint → fallback to previous → resume
- Performance tests:
  - Checkpoint compression time (target <100ms)
  - Checkpoint decompression time (target <50ms)
  - Compression ratio (target >50% size reduction)
- Coverage target: **100%**
- Test file: `rust/durable-shannon/tests/worker/checkpoint_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Adaptive checkpoint frequency implemented
- [ ] zstd compression level 3
- [ ] Incremental checkpoints with delta encoding
- [ ] Checkpoint pruning (keep last 3)
- [ ] CRC32 checksum for corruption detection
- [ ] Tests pass with 100% coverage
- [ ] Performance targets met
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with checkpoint strategy

**Parallel**: **[P]** - Can run parallel with P3.2

**Estimated Duration**: 2 days

---

### Task P3.4: Tauri Workflow Commands

**Description**: Create Tauri command bindings for workflow operations to enable frontend-backend communication. This bridges the Rust engine with the Tauri frontend.

**Files**:
- `desktop/src-tauri/src/workflow.rs` (create)
- `desktop/src-tauri/src/main.rs` (modify - register commands)

**Dependencies**:
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - submit_workflow() command with valid input
  - submit_workflow() command with invalid input
  - stream_workflow_events() command initialization
  - pause_workflow() command
  - resume_workflow() command
  - cancel_workflow() command
  - get_workflow_history() command
  - Error handling with serde-compatible types
  - Command parameter serialization/deserialization
- Integration tests:
  - Tauri test harness for all commands
  - Frontend → command → engine → result flow
  - Event emission from backend to frontend
- Coverage target: **100%**
- Test file: `desktop/src-tauri/tests/workflow_commands.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] All 6 workflow commands implemented (#[tauri::command])
- [ ] Error handling returns serde-compatible types
- [ ] Integration with EmbeddedWorkflowEngine
- [ ] Tests pass with 100% coverage
- [ ] Commands registered in main.rs
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with command usage examples

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P3.5: Workflow History Browser

**Description**: Create UI component for browsing workflow history with filtering, search, and detail view. This enables users to view past workflows.

**Files**:
- `desktop/app/(app)/workflows/history/page.tsx` (create)
- `desktop/lib/shannon/workflows.ts` (create - API client)
- `desktop/components/workflow-list.tsx` (create - list component)
- `desktop/components/workflow-detail.tsx` (create - detail component)

**Dependencies**:
- P3.4 (Tauri Workflow Commands)

**Test Requirements**:
- Component tests:
  - WorkflowList renders workflow items
  - WorkflowList filtering by status (completed, failed, in-progress)
  - WorkflowList filtering by date range
  - WorkflowList search by query text
  - WorkflowDetail displays workflow information
  - WorkflowDetail timeline renders correctly
  - Export workflow button functionality
  - Re-run workflow button functionality
- Integration tests:
  - Load workflow history → display list
  - Click workflow → display detail view
  - Filter workflows → list updates
  - Search workflows → filtered results
- Coverage target: **90%**
- Test file: `desktop/tests/integration/workflow_history.test.tsx`

**Acceptance Criteria**:
- [ ] Compiles with no warnings (tsc --noEmit)
- [ ] Workflow history page implemented
- [ ] List, filter, and search functionality working
- [ ] Workflow detail view with timeline
- [ ] Event viewer with syntax highlighting
- [ ] Export to JSON button
- [ ] Re-run workflow button
- [ ] Tests pass with 90%+ coverage
- [ ] Responsive design (mobile + desktop)
- [ ] Documentation complete with UI screenshots

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P3.6: Progress Visualization

**Description**: Create real-time progress visualization component for running workflows. This shows users live workflow progress.

**Files**:
- `desktop/components/workflow-progress.tsx` (create)
- `desktop/components/reasoning-timeline.tsx` (create - CoT steps)
- `desktop/components/source-citations.tsx` (create - research sources)

**Dependencies**:
- P3.4 (Tauri Workflow Commands) - can run parallel with P3.5

**Test Requirements**:
- Component tests:
  - WorkflowProgress renders progress bar (0-100%)
  - WorkflowProgress updates in real-time
  - Current step display updates
  - ReasoningTimeline renders steps correctly
  - SourceCitations renders citations with links
  - Token usage gauge displays correctly
  - Pause/Resume/Cancel buttons functional
  - Component updates on SSE events
- Integration tests:
  - Start workflow → progress updates in real-time
  - Click pause → workflow pauses → button changes to resume
  - Click cancel → workflow cancels → progress stops
- Coverage target: **95%**
- Test file: `desktop/tests/components/workflow-progress.test.tsx`

**Acceptance Criteria**:
- [ ] Compiles with no warnings (tsc --noEmit)
- [ ] Real-time progress bar (0-100%)
- [ ] Current step display
- [ ] Reasoning step timeline (for CoT, ToT)
- [ ] Source citation list (for Research)
- [ ] Token usage gauge
- [ ] Pause/Resume/Cancel buttons
- [ ] Tests pass with 95%+ coverage
- [ ] Smooth animations and transitions
- [ ] Documentation complete with component API

**Parallel**: **[P]** - Can run parallel with P3.5

**Estimated Duration**: 3 days

---

### Task P3.7: API Compatibility Test Suite

**Description**: Create comprehensive test suite comparing embedded API responses with cloud API for 100% compatibility verification.

**Files**:
- `tests/workflow/compatibility/cloud_comparison_test.rs` (create)
- `tests/workflow/compatibility/event_schema_test.rs` (create)
- `tests/workflow/compatibility/api_contract_test.rs` (create)

**Dependencies**:
- All Phase 1-2 tasks

**Test Requirements**:
- Compatibility tests:
  - Task submission: request/response schema match
  - Event types: all 26+ event types present
  - Event data: schema validation for each type
  - API endpoints: same paths and methods
  - Error responses: same error format
  - Token usage: within 10% of cloud
  - Source citations: same format
  - Reasoning steps: same structure
- Integration tests:
  - Submit same query to cloud and embedded → compare results
  - Timing tolerance (embedded faster than cloud is OK)
  - Validate event ordering matches
- Coverage: N/A (test suite itself)

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] All API endpoints have compatibility tests
- [ ] All event types validated against cloud
- [ ] Event data schemas match cloud exactly
- [ ] Token usage comparison within 10%
- [ ] Tests pass with 100% compatibility
- [ ] Documentation of any intentional differences
- [ ] CI integration for ongoing compatibility checks

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P3.8: Export/Import Workflow State

**Description**: Implement workflow export to JSON/Markdown and import for state restoration. This enables workflow portability and sharing.

**Files**:
- `rust/shannon-api/src/workflow/embedded/export.rs` (create)
- `rust/shannon-api/src/workflow/embedded/import.rs` (create)

**Dependencies**:
- P1.2 (Workflow Database Schema)
- P1.4 (EmbeddedWorkflowEngine)

**Test Requirements**:
- Unit tests:
  - Export workflow to JSON (complete structure)
  - Export workflow to Markdown (human-readable)
  - Import workflow from JSON (restore state)
  - Sanitize exported data (remove API keys)
  - Versioned export format (schema v1)
  - Export validation (valid JSON schema)
- Integration tests:
  - Execute workflow → export to JSON → verify structure
  - Export workflow → import → resume → complete
  - Export with sensitive data → verify sanitized
  - Export multiple workflows → batch import
- Coverage target: **100%**
- Test file: `rust/shannon-api/tests/workflow/integration/export_import_test.rs`

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] Export workflow to JSON (input, events, output, metadata)
- [ ] Export workflow to Markdown (readable report)
- [ ] Import workflow from JSON (restore state)
- [ ] Data sanitization (remove API keys, secrets)
- [ ] Versioned export format (schema v1)
- [ ] Tests pass with 100% coverage
- [ ] Round-trip verified (export → import → resume)
- [ ] Clippy clean (no warnings)
- [ ] Documentation complete with export format spec

**Parallel**: No

**Estimated Duration**: 3 days

---

### Task P3.9: End-to-End Test Suite

**Description**: Create comprehensive E2E test suite covering all patterns, control operations, and failure scenarios. This validates the complete system.

**Files**:
- `tests/workflow/e2e/simple_task_test.rs` (create)
- `tests/workflow/e2e/research_task_test.rs` (create)
- `tests/workflow/e2e/complex_task_test.rs` (create)
- `tests/workflow/e2e/multi_step_test.rs` (create)
- `tests/workflow/e2e/debate_test.rs` (create)
- `tests/workflow/e2e/control_operations_test.rs` (create)
- `tests/workflow/e2e/crash_recovery_test.rs` (create)
- `tests/workflow/e2e/concurrent_workflows_test.rs` (create)

**Dependencies**:
- All Phase 1-3 tasks

**Test Requirements**:
- E2E tests:
  - Simple query (CoT pattern) end-to-end
  - Research query (Deep Research 2.0) end-to-end
  - Complex query (ToT pattern) end-to-end
  - Multi-step query (ReAct pattern) end-to-end
  - Debate query (Debate pattern) end-to-end
  - Pause/Resume workflow mid-execution
  - Cancel workflow during execution
  - App crash during workflow → recover → complete
  - 10 concurrent workflows execute successfully
  - All patterns complete without errors
- Coverage: N/A (E2E test suite)

**Acceptance Criteria**:
- [ ] Compiles with no warnings
- [ ] All 6 patterns have E2E tests
- [ ] Control operations tested (pause, resume, cancel)
- [ ] Crash recovery tested
- [ ] Concurrent execution tested (10 workflows)
- [ ] All tests pass reliably
- [ ] Test execution time reasonable (<30 min total)
- [ ] CI integration complete
- [ ] Documentation with test scenarios

**Parallel**: No

**Estimated Duration**: 4 days

---

### Task P3.10: Documentation

**Description**: Create complete documentation for the embedded workflow engine covering architecture, API, patterns, configuration, debugging, and troubleshooting.

**Files**:
- `docs/embedded-workflow-engine.md` (create - main doc)
- `docs/workflow-patterns.md` (create - pattern guide)
- `docs/workflow-debugging.md` (create - debugging guide)
- `docs/workflow-configuration.md` (create - config reference)

**Dependencies**:
- None - can run parallel with P3.9

**Test Requirements**:
- N/A (documentation)

**Acceptance Criteria**:
- [ ] Architecture overview with component diagram
- [ ] API reference for all endpoints
- [ ] Pattern selection guide (when to use each)
- [ ] Configuration reference (all options documented)
- [ ] Debugging guide (replay, logs, troubleshooting)
- [ ] Performance tuning tips
- [ ] Migration guide from cloud Temporal
- [ ] Troubleshooting FAQ (common issues)
- [ ] Code examples for common use cases
- [ ] Mermaid diagrams for complex flows

**Parallel**: **[P]** - Can run parallel with P3.9

**Estimated Duration**: 2 days

---

### Task P3.11: Quality Gates & Audit

**Description**: Final quality audit ensuring all code meets standards, tests pass, performance targets met, and documentation complete. This is the production readiness gate.

**Files**:
- `.github/workflows/embedded-workflow-ci.yml` (create - CI pipeline)
- `scripts/quality-audit.sh` (create - audit script)

**Dependencies**:
- All Phase 1-3 tasks

**Test Requirements**:
- Quality checks:
  - cargo fmt --check (all files formatted)
  - cargo clippy -- -D warnings (zero warnings)
  - cargo test (100% pass rate)
  - cargo tarpaulin (90%+ overall coverage)
  - RUST.md compliance audit (manual review)
  - Security audit (cargo audit, cargo deny)
  - Performance regression test (re-run benchmarks)
  - Memory leak detection (valgrind)
  - API compatibility check (100% pass)
  - Documentation completeness check
- Coverage: N/A (audit process)

**Acceptance Criteria**:
- [ ] Zero compilation warnings
- [ ] Zero clippy warnings
- [ ] 100% test pass rate
- [ ] 90%+ overall code coverage
- [ ] 100% core engine coverage
- [ ] Rust coding standards compliance verified
- [ ] No security vulnerabilities (cargo audit)
- [ ] Performance targets met (all benchmarks)
- [ ] Memory leak free (valgrind clean)
- [ ] API compatibility 100%
- [ ] Documentation complete and accurate
- [ ] CI pipeline configured and passing

**Parallel**: No

**Estimated Duration**: 3 days

---

## Task Dependencies Graph

```
Phase 1 (Foundation MVP):
P1.1 (SQLite Event Log)
P1.2 (Workflow Schema) [P]
P1.3 (Event Bus)
  ├─> P1.4 (Engine Skeleton) ───┬─> P1.5 (Pattern Registry)
  │                              │     ├─> P1.6 (CoT Pattern)
  │                              │     └─> P1.8 (Research Pattern)
  │                              │
  │                              ├─> P1.9 (SSE Streaming)
  │                              ├─> P1.10 (Control Signals)
  │                              └─> P1.11 (Session Continuity)
  │
  └───────────────────────────────> P1.12 (Recovery & Replay)

P1.7 (LLM Activity) [P]

Phase 2 (Advanced Features):
P1.5 + P1.7 ──┬─> P2.1 (ToT Pattern)
              ├─> P2.2 (ReAct Pattern)
              ├─> P2.4 (Debate Pattern)
              └─> P2.5 (Reflection Pattern)

P2.3 (Tool Activity) [P]

P1.8 + P2.3 ──> P2.6 (Deep Research 2.0)

P1.4 ──> P2.7 (Router)

P2.1-P2.5 ──> P2.8 (WASM Compilation)

P2.9 (WASM Caching) [P]

P2.8 ──> P2.10 (Benchmarks) ──> P2.11 (Optimization)

Phase 3 (Production Ready):
P1.12 ──> P3.1 (Error Recovery)

P1.1 ──> P3.2 (Replay Debugging)

P3.3 (Checkpoint Opt) [P]

P1.4 ──> P3.4 (Tauri Commands) ──┬─> P3.5 (History Browser)
                                  └─> P3.6 (Progress Viz) [P]

All P1-P2 ──> P3.7 (API Compat Tests)

P1.2 + P1.4 ──> P3.8 (Export/Import)

All P1-P3 ──┬─> P3.9 (E2E Tests)
            └─> P3.10 (Documentation) [P]
            └─> P3.11 (Quality Audit)
```

---

## Test Coverage Map

| Component | Unit Tests | Integration Tests | Property Tests | Benchmarks | E2E Tests | Coverage Target |
|-----------|-----------|-------------------|----------------|-----------|-----------|-----------------|
| **SQLite Event Log** | P1.1 | P1.1 | P1.1 | P2.10 | P3.9 | 100% |
| **Workflow Schema** | P1.2 | P1.2 | - | - | P3.9 | 100% |
| **Event Bus** | P1.3 | P1.3 | - | P2.10 | P3.9 | 100% |
| **Workflow Engine** | P1.4 | P1.4 | - | P2.10 | P3.9 | 90% |
| **Pattern Registry** | P1.5 | P1.5 | - | - | P3.9 | 100% |
| **CoT Pattern** | P1.6 | P1.6 | - | P2.10 | P3.9 | 95% |
| **LLM Activity** | P1.7 | P1.7 | - | P2.10 | P3.9 | 100% |
| **Research Pattern** | P1.8 | P1.8 | - | P2.10 | P3.9 | 95% |
| **SSE Streaming** | P1.9 | P1.9 | - | P2.10 | P3.9 | 100% |
| **Control Signals** | P1.10 | P1.10 | - | - | P3.9 | 100% |
| **Session Mgmt** | P1.11 | P1.11 | - | - | P3.9 | 100% |
| **Recovery & Replay** | P1.12 | P1.12 | P1.12 | - | P3.9 | 100% |
| **ToT Pattern** | P2.1 | P2.1 | - | P2.10 | P3.9 | 95% |
| **ReAct Pattern** | P2.2 | P2.2 | - | P2.10 | P3.9 | 95% |
| **Tool Activity** | P2.3 | P2.3 | - | - | P3.9 | 100% |
| **Debate Pattern** | P2.4 | P2.4 | - | P2.10 | P3.9 | 95% |
| **Reflection Pattern** | P2.5 | P2.5 | - | P2.10 | P3.9 | 95% |
| **Deep Research 2.0** | P2.6 | P2.6 | - | P2.10 | P3.9 | 95% |
| **Router** | P2.7 | P2.7 | - | - | P3.9 | 100% |
| **WASM Compilation** | P2.8 | P2.8 | - | P2.10 | P3.9 | 90% |
| **WASM Caching** | P2.9 | P2.9 | - | P2.10 | P3.9 | 100% |
| **Error Recovery** | P3.1 | P3.1 | - | - | P3.9 | 100% |
| **Replay Debugging** | P3.2 | P3.2 | P3.2 | - | P3.9 | 100% |
| **Checkpoint Opt** | P3.3 | P3.3 | - | P3.3 | P3.9 | 100% |
| **Tauri Commands** | P3.4 | P3.4 | - | - | P3.9 | 100% |
| **UI Components** | P3.5, P3.6 | P3.5, P3.6 | - | - | - | 90-95% |
| **API Compatibility** | - | P3.7 | - | - | P3.7 | N/A |
| **Export/Import** | P3.8 | P3.8 | - | - | P3.9 | 100% |
| **Overall** | - | - | - | - | - | **≥90%** |

---

## Parallelization Opportunities

### Phase 1
- **Week 1**: P1.1 (SQLite) + P1.2 (Schema) can run in parallel
- **Week 3**: P1.6 (CoT) + P1.7 (LLM Activity) can run in parallel

### Phase 2
- **Week 7-8**: P2.2 (ReAct) + P2.3 (Tool Activity) can run in parallel
- **Week 11**: P2.8 (WASM) + P2.9 (WASM Cache) can run in parallel

### Phase 3
- **Week 13**: P3.2 (Replay) + P3.3 (Checkpoint) can run in parallel
- **Week 14**: P3.5 (History Browser) + P3.6 (Progress Viz) can run in parallel
- **Week 16**: P3.9 (E2E Tests) + P3.10 (Documentation) can run in parallel

**Total Parallelizable Tasks**: 8 out of 34 (24%)

---

## Summary

### Task Breakdown by Phase

| Phase | Tasks | Person-Days | Parallel Tasks | Duration (Weeks) |
|-------|-------|-------------|----------------|------------------|
| **Phase 1** | 12 | 30 | 2 [P] | 6 |
| **Phase 2** | 11 | 36 | 3 [P] | 6 |
| **Phase 3** | 11 | 32 | 3 [P] | 4 |
| **Total** | **34** | **98** | **8 [P]** | **16** |

### Key Dependency Chains

**Critical Path (Longest)**:
```
P1.1 → P1.4 → P1.5 → P1.6 → P2.1 → P2.8 → P2.10 → P2.11 → P3.9 → P3.11
(~34 days)
```

**Pattern Implementation Path**:
```
P1.5 + P1.7 → {P1.6, P1.8, P2.1, P2.2, P2.4, P2.5} → P2.6 → P2.8 → P3.9
(~25 days)
```

**UI Integration Path**:
```
P1.4 → P3.4 → {P3.5, P3.6} → P3.9
(~13 days)
```

### Test Coverage Summary

- **Unit Tests**: All 34 tasks include unit tests
- **Integration Tests**: 28 tasks have integration tests
- **Property Tests**: 3 tasks (P1.1, P1.12, P3.2) have property tests
- **Benchmarks**: 1 task (P2.10) dedicated benchmarks, measured in P2.11
- **E2E Tests**: 1 comprehensive suite (P3.9) covering all patterns

**Total Test Coverage Target**: ≥90% overall, 100% for core engine

---

## Next Steps

### Immediate Actions

1. **Review and Approve** this task breakdown with the engineering team
2. **Set up project tracking** (GitHub milestones for each phase, issues for each task)
3. **Create feature branch**: `feature/embedded-workflow-engine`
4. **Configure CI pipeline**:
   - Automated testing on each commit
   - Code coverage reporting
   - Performance regression detection
5. **Environment setup**:
   - Install WASM toolchain (`rustup target add wasm32-wasi`)
   - Configure test database (SQLite)
   - Set up test LLM API keys

### Kickoff Phase 1 (Week 1)

**Start with**:
- P1.1 (SQLite Event Log Backend) - Engineer 1
- P1.2 (Workflow Database Schema) - Engineer 2 [P]

**Success Criteria for Week 1**:
- Both tasks complete
- Zero compilation warnings
- 100% test coverage for both
- Code review approved
- Ready to start P1.3 and P1.4

### Weekly Check-ins

Track these metrics weekly:
- Tasks completed vs planned
- Test coverage percentage
- Compilation warnings count
- Blocker issues
- Performance benchmark results (starting Phase 2)

### Phase Gates

**Phase 1 → Phase 2**:
- All 12 Phase 1 tasks complete
- Integration tests passing
- CoT and Research patterns working end-to-end
- Demo: Submit → Stream → Recover

**Phase 2 → Phase 3**:
- All 11 Phase 2 tasks complete
- All 6 patterns implemented and tested
- Performance benchmarks meet targets
- Demo: Complex workflow with ToT pattern

**Phase 3 → Production**:
- All 11 Phase 3 tasks complete
- API compatibility verified (100%)
- E2E tests passing
- Documentation complete
- Quality audit passed
- Demo: Production-ready embedded workflow engine

---

**Document Status**: Complete
**Total Tasks Defined**: 34
**Total Phases**: 3
**Ready for Implementation**: Yes
**Next Action**: Switch to Code mode to begin P1.1 implementation