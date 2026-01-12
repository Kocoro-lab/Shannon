# Embedded Workflow Engine - Data Model

**Feature ID**: 002-embedded-workflow-engine  
**Version**: 1.0

---

## Core Entities

### Workflow

**Fields**:
- `id`: String (UUID v4)
- `workflow_type`: Enum (Simple, DAG, React, Research, Exploratory, Scientific, Supervisor)
- `status`: Enum (Pending, Running, Paused, Completed, Failed, Cancelled)
- `input`: TaskInput (JSON)
- `output`: Option<TaskResult> (JSON)
- `session_id`: String
- `user_id`: String
- `created_at`: DateTime<Utc>
- `started_at`: Option<DateTime<Utc>>
- `completed_at`: Option<DateTime<Utc>>

**Relationships**:
- Owns many ExecutionEvents
- Owns one WorkflowCheckpoint (optional)
- Belongs to one Session
- Uses many Patterns

**Validation**:
- `status` must transition in valid sequence (Pending → Running → Completed/Failed/Cancelled)
- `completed_at` must be after `started_at`
- `output` must be present when `status` is Completed

---

### ExecutionEvent

**Fields**:
- `id`: Integer (auto-increment)
- `workflow_id`: String (foreign key)
- `event_type`: String
- `event_data`: BLOB (bincode serialized)
- `sequence`: Integer (unique per workflow)
- `timestamp`: DateTime<Utc>

**Relationships**:
- Belongs to one Workflow

**Validation**:
- `sequence` must be monotonically increasing per workflow
- `event_data` must be valid bincode format
- Events cannot be deleted (append-only log)

---

### WorkflowCheckpoint

**Fields**:
- `workflow_id`: String (primary key, foreign key)
- `state_data`: BLOB (compressed bincode)
- `last_event_sequence`: Integer
- `created_at`: DateTime<Utc>

**Relationships**:
- Belongs to one Workflow

**Validation**:
- `last_event_sequence` must match an existing event sequence
- `state_data` must be valid compressed bincode
- Only one checkpoint per workflow (most recent)

---

### Session

**Fields**:
- `id`: String (UUID v4)
- `user_id`: String
- `active_workflow_id`: Option<String> (foreign key)
- `context`: HashMap<String, Value> (JSON)
- `created_at`: DateTime<Utc>
- `last_activity`: DateTime<Utc>

**Relationships**:
- Has many Workflows
- Has one active Workflow (optional)

**Validation**:
- `active_workflow_id` must reference an existing workflow
- `last_activity` must be >= `created_at`
- `context` must be valid JSON

---

### Pattern

**Fields**:
- `name`: String (primary key)
- `pattern_type`: Enum (ChainOfThought, TreeOfThoughts, Research, ReAct, Debate, Reflection)
- `wasm_module_path`: Option<PathBuf>
- `config`: PatternConfig (JSON)
- `version`: String

**Relationships**:
- Executed by many Workflows
- Produces many PatternOutputs

**Validation**:
- `name` must be unique
- `wasm_module_path` must exist if present
- `config` must be valid for pattern_type

---

### Activity

**Fields**:
- `id`: String (UUID v4)
- `activity_type`: Enum (LLM, Tool)
- `workflow_id`: String (foreign key)
- `status`: Enum (Scheduled, Running, Completed, Failed)
- `input`: ActivityInput (JSON)
- `output`: Option<ActivityOutput> (JSON)
- `scheduled_at`: DateTime<Utc>
- `started_at`: Option<DateTime<Utc>>
- `completed_at`: Option<DateTime<Utc>>
- `retry_count`: Integer

**Relationships**:
- Belongs to one Workflow
- May trigger child Activities

**Validation**:
- `status` must transition in valid sequence
- `completed_at` must be after `started_at`
- `retry_count` must be >= 0

---

## Value Objects

### TaskInput

**Fields**:
- `query`: String
- `session_id`: Option<String>
- `mode`: Option<String>
- `model_tier`: Option<String>
- `context`: TaskContext

**Validation**:
- `query` must not be empty
- `mode` must be one of: simple, supervisor, research, react
- `model_tier` must be one of: small, medium, large, premium

---

### TaskContext

**Fields**:
- `role`: Option<String>
- `system_prompt`: Option<String>
- `prompt_params`: HashMap<String, Value>
- `model_override`: Option<String>
- `provider_override`: Option<String>
- `cognitive_strategy`: Option<String>
- `research_strategy`: Option<String>
- `max_concurrent_agents`: Option<usize>
- `enable_verification`: bool
- `enable_citations`: bool
- `force_research`: bool
- `iterative_research_enabled`: bool
- `iterative_max_iterations`: Option<usize>

**Validation**:
- `cognitive_strategy` must be one of: exploratory, scientific, react, research
- `max_concurrent_agents` must be <= 10
- `iterative_max_iterations` must be <= 10

---

### TaskResult

**Fields**:
- `answer`: String
- `confidence`: f64 (0.0-1.0)
- `reasoning_steps`: Vec<ReasoningStep>
- `sources`: Vec<Source>
- `token_usage`: TokenUsage
- `model_used`: String
- `provider`: String
- `duration_ms`: u64
- `metadata`: Option<Value>

**Validation**:
- `answer` must not be empty
- `confidence` must be in range [0.0, 1.0]
- `duration_ms` must be > 0

---

### ReasoningStep

**Fields**:
- `step`: usize
- `step_type`: String (thought, action, observation, evaluation)
- `content`: String
- `timestamp`: DateTime<Utc>
- `confidence`: Option<f64>

**Validation**:
- `step` must be > 0
- `step_type` must be valid enum value
- `confidence` must be in range [0.0, 1.0] if present

---

### Source

**Fields**:
- `title`: String
- `url`: Option<String>
- `snippet`: Option<String>
- `confidence`: f64 (0.0-1.0)
- `accessed_at`: DateTime<Utc>

**Validation**:
- `title` must not be empty
- `url` must be valid URL if present
- `confidence` must be in range [0.0, 1.0]

---

### TokenUsage

**Fields**:
- `prompt_tokens`: u32
- `completion_tokens`: u32
- `total_tokens`: u32
- `model_breakdown`: HashMap<String, ModelTokenUsage>

**Validation**:
- `total_tokens` == `prompt_tokens` + `completion_tokens`
- All token counts must be >= 0

---

### ModelTokenUsage

**Fields**:
- `model`: String
- `provider`: String
- `prompt_tokens`: u32
- `completion_tokens`: u32
- `total_tokens`: u32
- `calls`: u32

**Validation**:
- `total_tokens` == `prompt_tokens` + `completion_tokens`
- `calls` must be > 0

---

## Event Types

### WorkflowEvent Enum

**Variants**:
- `WorkflowStarted { workflow_id, workflow_type, input, timestamp }`
- `AgentStarted { workflow_id, agent_id, agent_type }`
- `AgentCompleted { workflow_id, agent_id, output, duration_ms }`
- `LlmPrompt { workflow_id, model, messages }`
- `LlmPartial { workflow_id, delta, agent_id }`
- `LlmOutput { workflow_id, response, metadata }`
- `ToolInvoked { workflow_id, tool, params }`
- `ToolObservation { workflow_id, tool, output }`
- `ToolError { workflow_id, tool, error }`
- `Progress { workflow_id, percent, message }`
- `WorkflowPausing { workflow_id }`
- `WorkflowPaused { workflow_id }`
- `WorkflowResumed { workflow_id }`
- `WorkflowCancelling { workflow_id }`
- `WorkflowCancelled { workflow_id }`
- `WorkflowCompleted { workflow_id, output, duration_ms }`
- `WorkflowFailed { workflow_id, error }`
- `ActivityScheduled { activity_id, activity_type, input }`
- `ActivityCompleted { activity_id, output, duration_ms }`
- `ActivityFailed { activity_id, error, retryable }`
- `Checkpoint { workflow_id, sequence }`

**Persistence Strategy**:
- **Persistent Events**: WorkflowStarted, AgentCompleted, LlmOutput, ToolInvoked, ToolObservation, WorkflowCompleted, WorkflowFailed, Checkpoint
- **Ephemeral Events**: LlmPartial, Progress, WorkflowPausing, WorkflowCancelling

---

## Entity Relationships

```
Session 1───────* Workflow 1───────* ExecutionEvent
                     │
                     │ 1
                     │
                     │ 0..1
                     ▼
              WorkflowCheckpoint
                     
Workflow *───────* Pattern

Workflow 1───────* Activity
```

---

## State Machines

### Workflow Status Transitions

```
Pending ──┬─→ Running ──┬─→ Completed
          │             ├─→ Failed
          │             ├─→ Cancelled
          │             └─→ Paused ──┬─→ Running
          │                          ├─→ Completed
          │                          ├─→ Failed
          │                          └─→ Cancelled
          └─→ Cancelled
```

**Rules**:
- Pending → Running: When execution starts
- Running → Paused: User pause signal
- Paused → Running: User resume signal
- Running → Completed: Successful completion
- Running → Failed: Unrecoverable error
- Any → Cancelled: User cancel signal

### Activity Status Transitions

```
Scheduled ──→ Running ──┬─→ Completed
                        └─→ Failed ──→ Scheduled (if retryable)
```

**Rules**:
- Scheduled → Running: Activity execution starts
- Running → Completed: Activity succeeds
- Running → Failed: Activity fails
- Failed → Scheduled: Retry on transient error (max 3 retries)

---

## Invariants

1. **Event Sequence Monotonicity**: For any workflow, event sequences must be strictly increasing
2. **Checkpoint Consistency**: Checkpoint `last_event_sequence` must reference an existing event
3. **Workflow Completion**: A completed workflow must have `output` populated
4. **Session Activity**: A session's `active_workflow_id` must reference a Running or Paused workflow
5. **Token Usage Consistency**: `total_tokens` must equal sum of `prompt_tokens` + `completion_tokens`
6. **Timing Consistency**: `completed_at` >= `started_at` >= `created_at`
7. **Confidence Range**: All confidence scores must be in [0.0, 1.0]

---

## Data Access Patterns

### Read Patterns

1. **Workflow by ID**: Single-row lookup by `id`
2. **Workflows by Session**: Index scan on `session_id`
3. **Workflows by Status**: Index scan on `status`
4. **Events by Workflow**: Range scan on `workflow_id` + `sequence`
5. **Recent Workflows**: Sort by `created_at DESC` with LIMIT
6. **Active Workflows**: Filter by `status IN ('running', 'paused')`

### Write Patterns

1. **Append Event**: INSERT with auto-increment `sequence`
2. **Update Workflow Status**: UPDATE single row by `id`
3. **Create Checkpoint**: INSERT or REPLACE by `workflow_id`
4. **Update Session Activity**: UPDATE `last_activity` timestamp

### Indexes Required

```sql
CREATE INDEX idx_workflows_status ON workflows(status);
CREATE INDEX idx_workflows_session ON workflows(session_id);
CREATE INDEX idx_workflows_created ON workflows(created_at DESC);
CREATE INDEX idx_workflow_events_lookup ON workflow_events(workflow_id, sequence);
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

---

## Data Retention

### Retention Policies

- **Completed Workflows**: 7 days
- **Failed Workflows**: 30 days (for debugging)
- **Cancelled Workflows**: 3 days
- **Events**: Deleted with parent workflow
- **Checkpoints**: Deleted with parent workflow
- **Sessions**: 90 days of inactivity

### Cleanup Strategy

```rust
async fn cleanup_old_workflows(&self) -> Result<()> {
    let cutoff_completed = Utc::now() - Duration::days(7);
    let cutoff_failed = Utc::now() - Duration::days(30);
    let cutoff_cancelled = Utc::now() - Duration::days(3);
    
    self.database.delete_workflows_where(
        "status = 'completed' AND completed_at < ?",
        &[cutoff_completed],
    ).await?;
    
    // Similar for failed and cancelled...
}
```
