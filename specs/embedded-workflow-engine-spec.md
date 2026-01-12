# Embedded Workflow Engine Integration Specification

**Version**: 1.0  
**Date**: January 12, 2026  
**Status**: Research Complete - Ready for Planning  
**Target**: Shannon Tauri Desktop Application

---

## Executive Summary

This specification defines the integration of an embedded workflow engine for Shannon's Tauri desktop application, enabling autonomous AI agent execution with deep research capabilities while maintaining feature parity with the cloud-based Temporal implementation.

### Goals

- **Embedded Execution**: Run complex multi-agent workflows locally without cloud dependencies
- **Temporal Parity**: Maintain API compatibility and feature equivalence with cloud workflows
- **Deep Research**: Implement Manus.ai-inspired autonomous research with CoT, ToT patterns
- **Performance**: Optimize for local execution with efficient resource utilization
- **Persistence**: Durable state management across sessions with SQLite
- **Event Streaming**: Real-time progress updates to Tauri UI

---

## 1. Current State Analysis

### 1.1 Existing Shannon Workflow Architecture

Shannon currently implements workflows using **Go + Temporal** for cloud deployment:

| Component | Technology | Capabilities |
|-----------|-----------|--------------|
| **Orchestrator** | Go + Temporal | Workflow routing, decomposition, strategy selection |
| **Patterns** | Go | CoT, ToT, ReAct, Debate, Reflection, Research |
| **Agent Core** | Rust | WASI sandbox, tool execution, Python runtime |
| **Event Streaming** | Redis + SSE/WS | Real-time workflow events |
| **State** | PostgreSQL + Redis | Persistent workflow state, caching |

**Key Workflows**:
- SimpleTaskWorkflow - Direct execution
- DAGWorkflow - Multi-step with dependencies
- ReactWorkflow - Reason-Act-Observe loops
- ResearchWorkflow - Information gathering with citations
- ExploratoryWorkflow - Tree-of-Thoughts exploration
- ScientificWorkflow - Hypothesis testing with debate

**Performance Characteristics**:
- Complexity scoring (0-1 scale) for routing
- Token budget enforcement
- Parallel execution with semaphore control (max 5 concurrent)
- Reflection gating (threshold 0.5)
- Human-in-the-loop approval for high-risk tasks

### 1.2 Existing Rust Implementation

Shannon has **partial Rust implementation** ready for embedded use:

**`rust/workflow-patterns/`** - Pattern library:
- ✅ Pattern trait definitions
- ✅ Input/Output structs with reasoning steps
- ⚠️ CoT, ToT, Research implementations (LLM integration pending)
- ⚠️ ReAct, Debate, Reflection (implementation pending)

**`rust/durable-shannon/`** - Embedded worker:
- ✅ Event-sourced workflow execution
- ✅ WASM-based workflow modules via Wasmtime v40
- ✅ MicroSandbox with capability-based security
- ✅ EmbeddedWorker with concurrent execution limits
- ✅ WorkflowHandle with event streaming
- ⚠️ Activity implementations (LLM, Tools) pending

**`rust/shannon-api/`** - API Gateway:
- ✅ HTTP REST API (Axum)
- ✅ Embedded mode support
- ✅ SQLite database integration
- ✅ Event streaming infrastructure
- ⚠️ Workflow engine integration pending

### 1.3 Cloud vs Embedded Requirements

| Requirement | Cloud (Temporal) | Embedded (Target) |
|-------------|------------------|-------------------|
| **Orchestration** | Temporal Server | durable-shannon worker |
| **State Storage** | PostgreSQL | SQLite (embedded) |
| **Caching** | Redis | In-memory (TTL-based) |
| **Event Streaming** | Redis Streams | Broadcast channels |
| **Workflow Language** | Go | Rust + WASM |
| **Python Execution** | gRPC to agent-core | In-process WASI |
| **Concurrency** | Horizontal scaling | Thread pool (max concurrent) |
| **Durability** | Temporal history | Event log replay |
| **Session Continuity** | Workflow ID resume | SQLite checkpoint recovery |

---

## 2. Research Findings

### 2.1 Durable Workflow Engine Pattern

**Core Concepts** (from research):
- **Event Sourcing**: Immutable append-only event log
- **Deterministic Replay**: Reconstruct state from events
- **WASM Execution**: Sandboxed workflow code
- **Activity Pattern**: Side effects isolated to activities
- **Checkpoint Recovery**: Resume from last successful state

**Shannon's Implementation**:
```rust
// rust/durable-shannon/src/lib.rs
pub enum Event {
    WorkflowStarted { workflow_id, workflow_type, input, timestamp },
    ActivityScheduled { activity_id, activity_type, input },
    ActivityCompleted { activity_id, output, duration_ms },
    ActivityFailed { activity_id, error, retryable },
    Checkpoint { state },
    WorkflowCompleted { output, timestamp },
    WorkflowFailed { error, timestamp },
}
```

**Event Log Interface**:
```rust
#[async_trait]
pub trait EventLog: Send + Sync {
    async fn append(&self, workflow_id: &str, event: Event) -> Result<()>;
    async fn replay(&self, workflow_id: &str) -> Result<Vec<Event>>;
}
```

**Storage Backend**: SQLite with table structure:
```sql
CREATE TABLE workflow_events (
    id INTEGER PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_data BLOB NOT NULL,
    sequence INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    INDEX idx_workflow_id (workflow_id, sequence)
);
```

### 2.2 MicroSandbox Architecture

**Wasmtime v40** with WASI Preview 1:
```rust
// rust/durable-shannon/src/microsandbox/wasm_sandbox.rs
pub struct SandboxCapabilities {
    pub timeout_ms: u64,
    pub max_memory_mb: usize,
    pub allow_network: bool,
    pub allow_filesystem: bool,
    pub allowed_hosts: Vec<String>,
}

impl WasmSandbox {
    pub fn load(wasm_bytes: &[u8]) -> Result<Self>;
    pub async fn instantiate(&self, caps: SandboxCapabilities) -> Result<WasmProcess>;
}

impl WasmProcess {
    pub async fn call_json(&mut self, func: &str, input: &Value) -> Result<Value>;
    pub fn pid(&self) -> u32;
    pub fn kill(&mut self);
}
```

**Security Model**:
- Virtualized filesystem (no host access)
- Network allow-list per workflow type
- Fuel-based CPU limits
- Memory caps (default 512MB)
- Deterministic execution (no system randomness)

### 2.3 Parking Lot Synchronization

**Finding**: `parking_lot` crate is a standard Rust synchronization library (faster mutexes/rwlocks), not a custom thread pool manager.

**Usage in Shannon**:
- Already integrated via Cargo.toml
- Used for `RwLock<HashMap<String, WorkflowInfo>>`
- Used for `Mutex<HashMap<String, broadcast::Sender>>`
- No custom implementation needed

**Concurrency Strategy**:
```rust
// rust/durable-shannon/src/worker/mod.rs
pub struct EmbeddedWorker<E: EventLog> {
    max_concurrent: usize,  // Default: based on CPU cores
    workflows: RwLock<HashMap<String, WorkflowInfo>>,
    channels: Mutex<HashMap<String, broadcast::Sender<WorkflowEvent>>>,
}
```

### 2.4 Manus.ai Feature Analysis

**Manus.ai Capabilities** (from research):
1. **Autonomous Execution**: Tasks complete without constant supervision
2. **Deep Research**: Multi-step information gathering with synthesis
3. **Planning & Orchestration**: Break down complex goals into subtasks
4. **Tool Integration**: Web search, data analysis, code execution
5. **Iteration & Refinement**: Improve outputs through reflection
6. **Progress Transparency**: Real-time status updates
7. **Performance**: 4-15 minute task completion (optimized)
8. **Model Agnostic**: Uses Anthropic, Alibaba, others via API

**Key Differentiators**:
- Orchestration layer (not just better prompts)
- ReAct, CoT, Tree-of-Thoughts frameworks
- Real-world task completion focus
- Execution engine vs conversational assistant

**Shannon's Current Implementation**:
- ✅ Multi-agent patterns (CoT, ToT, ReAct, Debate)
- ✅ Tool integration (web_search, web_fetch, calculator, etc.)
- ✅ Reflection for quality improvement
- ✅ Progress streaming via events
- ⚠️ Deep Research 2.0 (iterative coverage loop - cloud only)
- ⚠️ Autonomous planning (requires workflow engine integration)

---

## 3. Feature Specification

### 3.1 Embedded Workflow Engine Core

**Component**: `rust/shannon-api/src/workflow/embedded.rs`

**Responsibilities**:
1. Workflow submission and routing
2. Pattern execution coordination
3. Activity scheduling (LLM, Tools)
4. Event streaming to UI
5. State persistence and recovery
6. Concurrency control

**Architecture**:
```rust
pub struct EmbeddedWorkflowEngine {
    worker: Arc<durable_shannon::EmbeddedWorker<SqliteEventLog>>,
    patterns: Arc<PatternRegistry>,
    llm_client: Arc<LlmOrchestrator>,
    event_bus: Arc<EventBus>,
    database: Arc<Database>,
}

impl EmbeddedWorkflowEngine {
    pub async fn submit_task(&self, input: TaskInput) -> Result<WorkflowHandle>;
    pub async fn stream_events(&self, workflow_id: &str) -> Result<EventStream>;
    pub async fn pause_workflow(&self, workflow_id: &str) -> Result<()>;
    pub async fn resume_workflow(&self, workflow_id: &str) -> Result<()>;
    pub async fn cancel_workflow(&self, workflow_id: &str) -> Result<()>;
}
```

**Workflow Routing Logic**:
```rust
async fn route_workflow(&self, input: &TaskInput) -> Result<WorkflowType> {
    let complexity = self.analyze_complexity(&input.query).await?;
    
    match input.mode.as_deref() {
        Some("simple") => Ok(WorkflowType::Simple),
        Some("supervisor") => Ok(WorkflowType::Supervisor),
        _ => {
            if complexity < 0.3 {
                Ok(WorkflowType::Simple)
            } else if let Some(strategy) = &input.context.cognitive_strategy {
                match strategy.as_str() {
                    "exploratory" => Ok(WorkflowType::Exploratory),
                    "scientific" => Ok(WorkflowType::Scientific),
                    "react" => Ok(WorkflowType::React),
                    "research" => Ok(WorkflowType::Research),
                    _ => Ok(WorkflowType::DAG)
                }
            } else {
                Ok(WorkflowType::DAG)
            }
        }
    }
}
```

### 3.2 Pattern Integration

**Pattern Execution Flow**:
```
TaskInput → Router → Pattern Selection → WASM Module → Activities → Output
                                                            ↓
                                                    LLM Calls, Tools
```

**Pattern Registry**:
```rust
pub struct PatternRegistry {
    patterns: HashMap<String, Box<dyn CognitivePattern>>,
    wasm_cache: RwLock<HashMap<String, Vec<u8>>>,
}

impl PatternRegistry {
    pub fn register(&mut self, name: &str, pattern: Box<dyn CognitivePattern>);
    pub async fn execute(&self, name: &str, input: PatternInput) -> Result<PatternOutput>;
    pub async fn load_wasm_module(&self, name: &str) -> Result<Vec<u8>>;
}
```

**Native Pattern Execution** (for patterns not yet compiled to WASM):
```rust
// Temporary until WASM modules ready
let cot = ChainOfThought::new()
    .with_max_iterations(5)
    .with_model("claude-sonnet-4-20250514");

let output = cot.execute(input).await?;
```

**WASM Pattern Execution** (target state):
```rust
let wasm_bytes = self.patterns.load_wasm_module("chain_of_thought").await?;
let handle = self.worker.submit("chain_of_thought", input_json).await?;
let output = handle.result().await?;
```

### 3.3 Activity Implementations

**LLM Activity**:
```rust
// rust/durable-shannon/src/activities/llm.rs
pub async fn call_llm(ctx: ActivityContext, request: LlmRequest) -> Result<LlmResponse> {
    let client = ctx.get::<LlmOrchestrator>("llm_client")?;
    
    let response = client.chat_completion(
        &request.model,
        &request.messages,
        request.temperature,
        request.max_tokens,
    ).await?;
    
    Ok(LlmResponse {
        content: response.content,
        model: response.model,
        usage: response.usage,
    })
}
```

**Tool Activity**:
```rust
// rust/durable-shannon/src/activities/tools.rs
pub async fn execute_tool(ctx: ActivityContext, request: ToolRequest) -> Result<ToolResponse> {
    let registry = ctx.get::<ToolRegistry>("tool_registry")?;
    
    match request.tool.as_str() {
        "web_search" => {
            let params: WebSearchParams = serde_json::from_value(request.params)?;
            let results = web_search(&params.query, params.max_results).await?;
            Ok(ToolResponse { output: serde_json::to_value(results)? })
        },
        "web_fetch" => {
            let params: WebFetchParams = serde_json::from_value(request.params)?;
            let content = web_fetch(&params.url).await?;
            Ok(ToolResponse { output: serde_json::to_value(content)? })
        },
        _ => {
            // Delegate to agent-core for Python tools
            let agent_core = ctx.get::<AgentCoreClient>("agent_core")?;
            agent_core.execute_tool(&request.tool, request.params).await
        }
    }
}
```

### 3.4 Deep Research Implementation

**Research Pattern** (enhanced from Manus.ai capabilities):

```rust
pub struct DeepResearch {
    pub max_iterations: usize,           // Default: 3
    pub sources_per_round: usize,        // Default: 6
    pub min_sources: usize,              // Default: 8
    pub enable_verification: bool,       // Default: false
    pub enable_fact_extraction: bool,    // Default: false
    pub coverage_threshold: f64,         // Default: 0.8
}

impl CognitivePattern for DeepResearch {
    async fn execute(&self, input: PatternInput) -> Result<PatternOutput> {
        let mut iteration = 0;
        let mut sources = Vec::new();
        let mut coverage_score = 0.0;
        
        // Step 1: Query decomposition
        let sub_questions = self.decompose_query(&input.query).await?;
        
        // Step 2: Iterative research loop
        while iteration < self.max_iterations && coverage_score < self.coverage_threshold {
            // Search for each sub-question
            for question in &sub_questions {
                let results = self.search_sources(question).await?;
                sources.extend(results);
            }
            
            // Evaluate coverage
            coverage_score = self.evaluate_coverage(&input.query, &sources).await?;
            
            // Generate additional sub-questions if needed
            if coverage_score < self.coverage_threshold {
                let gaps = self.identify_gaps(&input.query, &sources).await?;
                sub_questions.extend(gaps);
            }
            
            iteration += 1;
        }
        
        // Step 3: Synthesis
        let report = self.synthesize_report(&input.query, &sources).await?;
        
        // Step 4: Fact extraction (optional)
        let facts = if self.enable_fact_extraction {
            self.extract_facts(&report).await?
        } else {
            Vec::new()
        };
        
        // Step 5: Verification (optional)
        if self.enable_verification && !sources.is_empty() {
            self.verify_claims(&report, &sources).await?;
        }
        
        Ok(PatternOutput {
            answer: report,
            confidence: coverage_score,
            sources,
            metadata: json!({
                "iterations": iteration,
                "facts_extracted": facts.len(),
                "coverage_score": coverage_score,
            }),
        })
    }
}
```

**Coverage Evaluation**:
```rust
async fn evaluate_coverage(&self, query: &str, sources: &[Source]) -> Result<f64> {
    let prompt = format!(
        "Evaluate how well these sources answer the query: '{query}'\n\n\
        Sources: {sources:?}\n\n\
        Rate coverage from 0.0 (no coverage) to 1.0 (complete coverage).\n\
        Consider: breadth, depth, credibility, recency."
    );
    
    let response = self.llm_client.call(&prompt).await?;
    parse_coverage_score(&response.content)
}
```

### 3.5 Event Streaming Architecture

**Event Flow**:
```
Workflow → Event Log → Broadcast Channel → SSE/WebSocket → Tauri Frontend
```

**Event Types** (maintaining cloud parity):
```rust
pub enum WorkflowEvent {
    // Core events
    WorkflowStarted { workflow_id: String },
    AgentStarted { workflow_id: String, agent_id: String },
    AgentCompleted { workflow_id: String, agent_id: String, output: Value },
    
    // LLM events
    LlmPartial { workflow_id: String, delta: String, agent_id: String },
    LlmOutput { workflow_id: String, response: String, metadata: LlmMetadata },
    
    // Tool events
    ToolInvoked { workflow_id: String, tool: String, params: Value },
    ToolObservation { workflow_id: String, tool: String, output: String },
    
    // Progress events
    Progress { workflow_id: String, percent: u8, message: Option<String> },
    
    // Control events
    WorkflowPaused { workflow_id: String },
    WorkflowResumed { workflow_id: String },
    WorkflowCancelled { workflow_id: String },
    
    // Completion events
    WorkflowCompleted { workflow_id: String, output: Value },
    WorkflowFailed { workflow_id: String, error: String },
}
```

**Event Persistence** (two-tier strategy from cloud):
```rust
pub enum EventPersistence {
    Ephemeral,  // In-memory only (LLM_PARTIAL, HEARTBEAT)
    Persistent, // SQLite storage (WORKFLOW_COMPLETED, AGENT_COMPLETED, etc.)
}

impl WorkflowEvent {
    fn should_persist(&self) -> bool {
        matches!(self,
            WorkflowEvent::WorkflowStarted { .. } |
            WorkflowEvent::AgentCompleted { .. } |
            WorkflowEvent::LlmOutput { .. } |
            WorkflowEvent::ToolInvoked { .. } |
            WorkflowEvent::ToolObservation { .. } |
            WorkflowEvent::WorkflowCompleted { .. } |
            WorkflowEvent::WorkflowFailed { .. }
        )
    }
}
```

**Streaming Implementation**:
```rust
pub async fn stream_events(
    workflow_id: String,
    event_bus: Arc<EventBus>,
) -> impl Stream<Item = Result<WorkflowEvent>> {
    let mut rx = event_bus.subscribe(&workflow_id);
    
    async_stream::stream! {
        while let Ok(event) = rx.recv().await {
            yield Ok(event);
        }
    }
}
```

### 3.6 State Persistence and Recovery

**SQLite Schema**:
```sql
-- Workflow metadata
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    workflow_type TEXT NOT NULL,
    status TEXT NOT NULL,  -- pending, running, completed, failed, cancelled
    input TEXT NOT NULL,   -- JSON
    output TEXT,           -- JSON
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER
);

-- Event log
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

-- Checkpoints
CREATE TABLE workflow_checkpoints (
    workflow_id TEXT PRIMARY KEY,
    state_data BLOB NOT NULL,
    last_event_sequence INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);
```

**Recovery Implementation**:
```rust
pub async fn recover_workflow(&self, workflow_id: &str) -> Result<WorkflowHandle> {
    // Load checkpoint if exists
    let checkpoint = self.database.load_checkpoint(workflow_id).await?;
    
    if let Some(cp) = checkpoint {
        // Replay events since checkpoint
        let events = self.event_log.replay_since(workflow_id, cp.last_event_sequence).await?;
        
        // Reconstruct state
        let state = cp.state_data;
        for event in events {
            state.apply(event)?;
        }
        
        // Resume execution
        self.worker.resume(workflow_id, state).await
    } else {
        // Full replay from beginning
        let events = self.event_log.replay(workflow_id).await?;
        self.worker.replay(workflow_id, events).await
    }
}
```

**Checkpoint Strategy**:
- Checkpoint after each major phase (decomposition, execution, synthesis)
- Checkpoint on workflow pause
- Auto-checkpoint every 10 activities
- Maximum checkpoint age: 5 minutes

### 3.7 Session Continuity

**Session Management**:
```rust
pub struct SessionManager {
    database: Arc<Database>,
    active_sessions: RwLock<HashMap<String, SessionInfo>>,
}

pub struct SessionInfo {
    session_id: String,
    user_id: String,
    active_workflow_id: Option<String>,
    conversation_history: Vec<Message>,
    context: HashMap<String, Value>,
    created_at: DateTime<Utc>,
    last_activity: DateTime<Utc>,
}

impl SessionManager {
    pub async fn create_session(&self, user_id: &str) -> Result<String>;
    pub async fn get_session(&self, session_id: &str) -> Result<SessionInfo>;
    pub async fn update_context(&self, session_id: &str, key: &str, value: Value) -> Result<()>;
    pub async fn associate_workflow(&self, session_id: &str, workflow_id: &str) -> Result<()>;
}
```

**Cross-Session Workflow Resume**:
```rust
pub async fn resume_session_workflow(&self, session_id: &str) -> Result<Option<WorkflowHandle>> {
    let session = self.session_manager.get_session(session_id).await?;
    
    if let Some(workflow_id) = session.active_workflow_id {
        let status = self.workflow_engine.status(&workflow_id).await?;
        
        if status == WorkflowState::Running || status == WorkflowState::Paused {
            let handle = self.workflow_engine.recover_workflow(&workflow_id).await?;
            return Ok(Some(handle));
        }
    }
    
    Ok(None)
}
```

---

## 4. API Compatibility

### 4.1 Task Submission API

**Endpoint**: `POST /api/v1/tasks`

**Request** (identical to cloud):
```json
{
  "query": "Research latest AI agent frameworks",
  "session_id": "session-123",
  "mode": "research",
  "model_tier": "large",
  "context": {
    "force_research": true,
    "iterative_research_enabled": true,
    "iterative_max_iterations": 3,
    "research_strategy": "deep",
    "max_concurrent_agents": 7,
    "enable_verification": true,
    "enable_citations": true
  }
}
```

**Response** (identical to cloud):
```json
{
  "task_id": "task-abc123",
  "workflow_id": "workflow-xyz789",
  "session_id": "session-123",
  "status": "running",
  "stream_url": "/api/v1/tasks/task-abc123/stream"
}
```

### 4.2 Event Streaming API

**SSE Endpoint**: `GET /api/v1/tasks/{task_id}/stream`

**Event Types** (identical to cloud):
- `workflow.started`
- `agent.started` / `agent.completed`
- `thread.message.delta` / `thread.message.completed`
- `tool.invoked` / `tool.result`
- `workflow.completed` / `workflow.failed`

**WebSocket Endpoint**: `GET /api/v1/tasks/{task_id}/stream/ws`

### 4.3 Control Signals API

**Pause**: `POST /api/v1/tasks/{task_id}/pause`
**Resume**: `POST /api/v1/tasks/{task_id}/resume`
**Cancel**: `POST /api/v1/tasks/{task_id}/cancel`
**Status**: `GET /api/v1/tasks/{task_id}/control-state`

---

## 5. Performance Specifications

### 5.1 Target Metrics

| Metric | Cloud (Temporal) | Embedded (Target) | Justification |
|--------|------------------|-------------------|---------------|
| **Cold Start** | 500-1000ms | <200ms | No network calls, in-process |
| **Task Latency** | 5-30s | 2-15s | Local LLM calls, no network hops |
| **Memory Usage** | 200-500MB | <150MB | Single process, shared memory |
| **Concurrent Workflows** | Unlimited | 5-10 | CPU core based limit |
| **Event Throughput** | 10K events/s | 5K events/s | In-memory channels |
| **Storage Growth** | Unlimited | ~100MB/1K workflows | SQLite with pruning |

### 5.2 Resource Constraints

**CPU**:
- Max concurrent workflows: `num_cpus::get().min(10)`
- WASM fuel limit: 10M instructions per activity
- Pattern execution: Multi-threaded with rayon

**Memory**:
- WASM instance: 512MB default, 1GB max
- Event buffer: 256 events per workflow (ring buffer)
- LRU cache for WASM modules: 100MB

**Storage**:
- Event log pruning: 7-day retention for completed workflows
- Checkpoint compression: zstd level 3
- SQLite page size: 4KB
- WAL mode enabled

**Network**:
- LLM API calls: Connection pooling (max 10 connections)
- Tool execution: Rate limiting per provider
- Timeout: 30s default, 5min max for research

### 5.3 Optimization Strategies

**WASM Module Caching**:
```rust
struct WasmCache {
    modules: RwLock<LruCache<String, Module>>,
    max_size_bytes: usize,
}

impl WasmCache {
    async fn get_or_load(&self, path: &Path) -> Result<Module> {
        let key = path.to_string_lossy().to_string();
        
        {
            let modules = self.modules.read().await;
            if let Some(module) = modules.peek(&key) {
                return Ok(module.clone());
            }
        }
        
        let bytes = tokio::fs::read(path).await?;
        let module = Module::from_binary(&self.engine, &bytes)?;
        
        let mut modules = self.modules.write().await;
        modules.put(key, module.clone());
        
        Ok(module)
    }
}
```

**Event Batching**:
```rust
struct EventBatcher {
    pending: Mutex<Vec<Event>>,
    batch_size: usize,
    flush_interval: Duration,
}

impl EventBatcher {
    async fn append(&self, event: Event) {
        let mut pending = self.pending.lock().await;
        pending.push(event);
        
        if pending.len() >= self.batch_size {
            self.flush_locked(pending).await;
        }
    }
    
    async fn flush_locked(&self, mut pending: MutexGuard<'_, Vec<Event>>) {
        let batch = std::mem::take(&mut *pending);
        self.database.batch_insert(batch).await;
    }
}
```

**Pattern Parallelization** (for independent sub-tasks):
```rust
async fn execute_parallel_agents(&self, sub_questions: Vec<String>) -> Result<Vec<String>> {
    use futures::stream::{FuturesUnordered, StreamExt};
    
    let tasks: FuturesUnordered<_> = sub_questions
        .into_iter()
        .map(|question| self.execute_agent(question))
        .collect();
    
    tasks.collect().await
}
```

---

## 6. Success Criteria

### 6.1 Functional Requirements

**MUST HAVE** (MVP):
- ✅ Execute Chain of Thought workflows
- ✅ Execute Research workflows with citations
- ✅ Event streaming to Tauri UI
- ✅ State persistence in SQLite
- ✅ Workflow pause/resume/cancel
- ✅ Session continuity across app restarts
- ✅ LLM provider integration (OpenAI, Anthropic)
- ✅ Basic tool execution (web_search, calculator)

**SHOULD HAVE** (v1.1):
- ✅ Tree of Thoughts exploration
- ✅ ReAct loops with observation
- ✅ Deep Research 2.0 (iterative coverage)
- ✅ Workflow replay for debugging
- ✅ Multi-agent debate pattern
- ✅ Reflection gating

**COULD HAVE** (v1.2):
- ✅ WASM-compiled workflow modules
- ✅ Distributed workflow execution (multi-device)
- ✅ Scheduled workflows
- ✅ Human-in-the-loop approval
- ✅ Advanced tool execution (file_system, code_execution)

### 6.2 Non-Functional Requirements

**Performance**:
- [ ] Research workflow completes in <15 minutes (Manus.ai parity)
- [ ] Simple task latency <5 seconds
- [ ] Event streaming latency <100ms
- [ ] App startup time <3 seconds with embedded engine

**Reliability**:
- [ ] 99.9% workflow completion rate
- [ ] Zero data loss on app crash (event log durability)
- [ ] Automatic recovery from interrupted workflows
- [ ] Graceful degradation when LLM providers fail

**Quality**:
- [ ] 100% test coverage for core workflow engine
- [ ] Zero compilation warnings in Rust code
- [ ] Adherence to Rust coding standards (docs/coding-standards/RUST.md)
- [ ] API compatibility tests pass (cloud vs embedded)

**Usability**:
- [ ] Real-time progress indicators
- [ ] Detailed error messages with recovery suggestions
- [ ] Workflow history browsing in UI
- [ ] Export workflow results (Markdown, JSON)

### 6.3 Measurable Success Metrics

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| **Task Completion Time** | <15 min (deep research) | Workflow duration tracking |
| **Memory Footprint** | <150MB per workflow | Process memory profiler |
| **Storage Efficiency** | <100KB per completed workflow | SQLite database size |
| **Event Latency** | P99 <100ms | Event timestamp deltas |
| **Recovery Time** | <5s for any workflow | Checkpoint to resume duration |
| **Test Coverage** | 100% core engine | cargo-tarpaulin report |
| **Crash Rate** | <0.1% of executions | Telemetry (local only) |

---

## 7. Key Entities and Data Model

### 7.1 Core Entities

**Workflow**:
```rust
pub struct Workflow {
    pub id: String,
    pub workflow_type: WorkflowType,
    pub status: WorkflowStatus,
    pub input: TaskInput,
    pub output: Option<TaskResult>,
    pub created_at: DateTime<Utc>,
    pub started_at: Option<DateTime<Utc>>,
    pub completed_at: Option<DateTime<Utc>>,
    pub session_id: String,
    pub user_id: String,
}

pub enum WorkflowType {
    Simple,
    DAG,
    React,
    Research,
    Exploratory,
    Scientific,
    Supervisor,
}

pub enum WorkflowStatus {
    Pending,
    Running,
    Paused,
    Completed,
    Failed,
    Cancelled,
}
```

**Task**:
```rust
pub struct TaskInput {
    pub query: String,
    pub session_id: Option<String>,
    pub mode: Option<String>,
    pub model_tier: Option<String>,
    pub context: TaskContext,
}

pub struct TaskContext {
    pub role: Option<String>,
    pub system_prompt: Option<String>,
    pub prompt_params: HashMap<String, Value>,
    pub model_override: Option<String>,
    pub provider_override: Option<String>,
    pub research_strategy: Option<String>,
    pub max_concurrent_agents: Option<usize>,
    pub enable_verification: bool,
    pub enable_citations: bool,
    pub force_research: bool,
    pub iterative_research_enabled: bool,
    pub iterative_max_iterations: Option<usize>,
}

pub struct TaskResult {
    pub answer: String,
    pub confidence: f64,
    pub reasoning_steps: Vec<ReasoningStep>,
    pub sources: Vec<Source>,
    pub token_usage: TokenUsage,
    pub model_used: String,
    pub provider: String,
    pub duration_ms: u64,
}
```

**ExecutionEvent**:
```rust
pub struct ExecutionEvent {
    pub workflow_id: String,
    pub sequence: u64,
    pub event_type: String,
    pub event_data: Value,
    pub timestamp: DateTime<Utc>,
}
```

**WorkflowCheckpoint**:
```rust
pub struct WorkflowCheckpoint {
    pub workflow_id: String,
    pub state_data: Vec<u8>,  // Compressed bincode
    pub last_event_sequence: u64,
    pub created_at: DateTime<Utc>,
}
```

### 7.2 Pattern Entities

**PatternInput**:
```rust
pub struct PatternInput {
    pub query: String,
    pub context: Option<String>,
    pub max_iterations: Option<usize>,
    pub model: Option<String>,
    pub temperature: Option<f32>,
    pub config: Option<Value>,
}
```

**PatternOutput**:
```rust
pub struct PatternOutput {
    pub answer: String,
    pub confidence: f64,
    pub reasoning_steps: Vec<ReasoningStep>,
    pub sources: Vec<Source>,
    pub token_usage: TokenUsage,
    pub duration_ms: u64,
    pub metadata: Option<Value>,
}
```

**ReasoningStep**:
```rust
pub struct ReasoningStep {
    pub step: usize,
    pub step_type: String,  // thought, action, observation, evaluation
    pub content: String,
    pub timestamp: DateTime<Utc>,
    pub confidence: Option<f64>,
}
```

**Source**:
```rust
pub struct Source {
    pub title: String,
    pub url: Option<String>,
    pub snippet: Option<String>,
    pub confidence: f64,
    pub accessed_at: DateTime<Utc>,
}
```

---

## 8. Integration Points

### 8.1 Component Integration Map

```
┌─────────────────────────────────────────────────────────┐
│                    Tauri Frontend                        │
│               (Next.js + TypeScript)                     │
└────────────────┬────────────────────────────────────────┘
                 │ Tauri IPC / HTTP
                 ▼
┌─────────────────────────────────────────────────────────┐
│              Shannon API (Rust)                          │
│           embedded_api.rs (port 8765)                    │
├─────────────────────────────────────────────────────────┤
│  Gateway Layer                                           │
│  • HTTP REST API (Axum)                                  │
│  • WebSocket/SSE streaming                               │
│  • Embedded auth (no JWT required)                       │
│  • Rate limiting (in-memory)                             │
└────────────────┬────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────┐
│         Embedded Workflow Engine (NEW)                   │
│      shannon-api/src/workflow/embedded.rs                │
├─────────────────────────────────────────────────────────┤
│  • Task routing (complexity analysis)                    │
│  • Pattern selection (CoT, ToT, Research, etc.)          │
│  • Workflow orchestration                                │
│  • Event streaming coordination                          │
│  • Session management                                    │
└─────┬───────────────────────────┬───────────────────────┘
      │                           │
      ▼                           ▼
┌─────────────────────┐  ┌─────────────────────────────────┐
│  Durable Worker     │  │     Pattern Registry            │
│  (durable-shannon)  │  │  (workflow-patterns)            │
├─────────────────────┤  ├─────────────────────────────────┤
│ • WASM execution    │  │ • ChainOfThought                │
│ • Event sourcing    │  │ • TreeOfThoughts                │
│ • State recovery    │  │ • Research (Deep 2.0)           │
│ • Checkpoint mgmt   │  │ • ReAct                         │
│ • Concurrency ctrl  │  │ • Debate                        │
└─────┬───────────────┘  │ • Reflection                    │
      │                  └──────┬──────────────────────────┘
      │                         │
      └──────────┬──────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────┐
│                Activity Execution                        │
├─────────────────────────────────────────────────────────┤
│  LLM Activities          Tool Activities                 │
│  • chat_completion       • web_search                    │
│  • streaming_chat        • web_fetch                     │
│  • embeddings            • calculator                    │
│                          • Python tools (via agent-core) │
└────────────────┬──────────────────┬─────────────────────┘
                 │                  │
                 ▼                  ▼
┌──────────────────────┐  ┌──────────────────────────────┐
│   LLM Orchestrator   │  │    Agent Core (Rust)         │
│   (shannon-api)      │  │    (agent-core)              │
├──────────────────────┤  ├──────────────────────────────┤
│ • Provider routing   │  │ • WASI Python sandbox        │
│ • Streaming          │  │ • Tool execution             │
│ • Retry logic        │  │ • Memory management          │
│ • Token tracking     │  │ • Resource limits            │
└──────────────────────┘  └──────────────────────────────┘
```

### 8.2 Data Flow

**Task Submission Flow**:
```
1. UI → POST /api/v1/tasks → Shannon API Gateway
2. Gateway → EmbeddedWorkflowEngine.submit_task()
3. Engine → analyze_complexity() → route_workflow()
4. Engine → PatternRegistry.execute(pattern_name, input)
5. Pattern → DurableWorker.submit(wasm_module, input)
6. Worker → WasmSandbox.instantiate() → call_json("run_workflow")
7. WASM → call_activity("llm", request) → LLM Orchestrator
8. WASM → call_activity("tool", request) → Agent Core
9. Worker → emit WorkflowEvent → EventBus
10. EventBus → broadcast → SSE/WebSocket → UI
11. Worker → append Event → SQLite EventLog
12. Worker → return WorkflowHandle → Engine → Gateway → UI
```

**Event Streaming Flow**:
```
1. UI → GET /api/v1/tasks/{id}/stream → Shannon API
2. API → EmbeddedWorkflowEngine.stream_events(workflow_id)
3. Engine → EventBus.subscribe(workflow_id)
4. EventBus → broadcast::Receiver<WorkflowEvent>
5. API → map WorkflowEvent → SSE format
6. API → send SSE event → UI
7. UI → EventSource.addEventListener() → handle event
```

**State Recovery Flow**:
```
1. App startup → EmbeddedWorkflowEngine.recover_all()
2. Engine → Database.list_incomplete_workflows()
3. For each workflow:
   a. Load checkpoint (if exists)
   b. Replay events since checkpoint
   c. Reconstruct state
   d. Resume execution
4. Emit recovery events → UI
```

### 8.3 External Integrations

**LLM Providers** (via shannon-api/src/llm/):
- OpenAI (GPT-4, GPT-5)
- Anthropic (Claude)
- Google (Gemini)
- Groq (LLaMA, Mixtral)
- xAI (Grok)

**Tool Providers**:
- Web Search: Tavily API
- Web Fetch: HTTP client (reqwest)
- Calculator: In-process evaluation
- Python Tools: agent-core WASI sandbox

**Storage**:
- SQLite: Workflow state, events, checkpoints
- In-memory: Event buffers, WASM cache, session cache

---

## 9. Assumptions and Constraints

### 9.1 Assumptions

**Technical Assumptions**:
1. Users have sufficient disk space (~1GB for app + data)
2. Users have internet connection for LLM API calls
3. SQLite can handle ~10K events per workflow efficiently
4. WASM modules are <10MB each
5. LLM providers maintain API compatibility

**Product Assumptions**:
1. Users primarily work with single workflows at a time
2. Deep research workflows complete in <15 minutes
3. Users accept local-only data storage (no cloud sync)
4. Workflow history retention of 7 days is acceptable
5. Users have API keys for at least one LLM provider

**Architecture Assumptions**:
1. Durable-shannon provides equivalent functionality to Temporal
2. WASM execution performance is acceptable for workflows
3. In-memory event streaming is sufficient for UI updates
4. SQLite can replace PostgreSQL for embedded use case
5. Parking lot crate provides adequate synchronization

### 9.2 Constraints

**Platform Constraints**:
- **Desktop Only** (Phase 1): macOS, Windows, Linux
- **Mobile Later** (Phase 2): iOS, Android (requires SQLite backend)
- **Single Process**: No distributed execution initially
- **Local First**: No cloud dependency after download

**Resource Constraints**:
- **Memory**: 150MB per workflow (target), 500MB absolute max
- **CPU**: Max concurrent workflows = min(num_cpus, 10)
- **Storage**: 100MB/1K workflows, 1GB total app data limit
- **Network**: LLM API rate limits apply per provider

**Feature Constraints**:
- **No Temporal UI**: Cannot use Temporal's workflow debugger
- **Limited Scalability**: Cannot horizontally scale (single device)
- **No Team Features**: Single-user focus initially
- **No Scheduled Tasks**: Manual workflow submission only (Phase 1)

**Compatibility Constraints**:
- **API Compatibility**: Must maintain parity with cloud API
- **Event Types**: Must match cloud event schema exactly
- **WASM ABI**: Workflow modules must use standard WASI Preview 1
- **SQLite Version**: Requires SQLite 3.35+ (for JSON functions)

**Security Constraints**:
- **API Keys**: Stored encrypted in SQLite (AES-256-GCM)
- **WASI Sandbox**: Capability-based security model
- **No Network Isolation**: Workflows can call external APIs
- **No Code Signing**: WASM modules not cryptographically verified (Phase 1)

### 9.3 Clarification Needed

[NEEDS CLARIFICATION 1]: **Workflow Module Distribution**
- How should WASM workflow modules be packaged and updated?
- Options:
  a) Bundle in app binary (increases download size)
  b) Download on-demand from CDN (requires network)
  c) User-provided (advanced users only)

[NEEDS CLARIFICATION 2]: **Agent Core Integration**
- Should agent-core run in-process or as separate service?
- Trade-offs:
  a) In-process: Simpler, faster, shared memory
  b) Separate: Better isolation, crash recovery, but IPC overhead

[NEEDS CLARIFICATION 3]: **Deep Research Configuration**
- Should users configure research parameters in UI?
- Or use sensible defaults with advanced settings hidden?
- Parameters: max_iterations, sources_per_round, coverage_threshold

---

## 10. Next Steps

### 10.1 Immediate Actions (Planning Phase)

1. **Review this specification** with team
2. **Address clarification points** (section 9.3)
3. **Estimate effort** for each component
4. **Create implementation plan** with phases
5. **Set up project tracking** (GitHub issues/milestones)

### 10.2 Phase 1: Foundation (MVP)

**Week 1-2: Core Integration**
- [ ] Implement EmbeddedWorkflowEngine skeleton
- [ ] Integrate durable-shannon worker
- [ ] Set up SQLite event log
- [ ] Connect to LLM orchestrator
- [ ] Basic pattern execution (native, not WASM yet)

**Week 3-4: Pattern Implementation**
- [ ] Complete ChainOfThought with LLM integration
- [ ] Complete Research pattern with web search
- [ ] Implement event streaming to Tauri
- [ ] Add workflow pause/resume/cancel
- [ ] Session continuity basics

**Week 5-6: Testing & Polish**
- [ ] Integration tests for all patterns
- [ ] API compatibility tests (vs cloud)
- [ ] Performance benchmarking
- [ ] Error handling improvements
- [ ] Documentation for developers

### 10.3 Phase 2: Advanced Features (v1.1)

**Week 7-8: Advanced Patterns**
- [ ] Tree of Thoughts implementation
- [ ] ReAct loops with observation
- [ ] Debate pattern
- [ ] Reflection gating

**Week 9-10: Deep Research 2.0**
- [ ] Iterative coverage loop
- [ ] Fact extraction
- [ ] Claim verification
- [ ] Gap identification

**Week 11-12: Optimization**
- [ ] WASM module compilation for patterns
- [ ] Performance tuning
- [ ] Memory optimization
- [ ] Storage pruning strategies

### 10.4 Phase 3: Production Ready (v1.2)

**Week 13-14: Reliability**
- [ ] Comprehensive error recovery
- [ ] Workflow replay debugging
- [ ] Checkpoint optimization
- [ ] Crash recovery testing

**Week 15-16: UI Integration**
- [ ] Workflow history browser
- [ ] Progress visualization
- [ ] Settings UI for research config
- [ ] Export/import workflows

---

## 11. Research Findings Summary

### 11.1 Key Technologies

**Durable Workflow Pattern**:
- Event sourcing with immutable log
- WASM-based workflow execution
- Deterministic replay for recovery
- Shannon already has custom implementation

**MicroSandbox (Wasmtime v40)**:
- WASI Preview 1 for system interface
- Capability