# Embedded Workflow Engine

**Version**: 1.0  
**Status**: Production Ready  
**Feature ID**: 002-embedded-workflow-engine

## Overview

The Embedded Workflow Engine enables Shannon to run durable, fault-tolerant workflows directly within the Tauri desktop application without requiring external infrastructure (no Temporal, no orchestrator service, no PostgreSQL).

### Key Features

- ✅ **6 Cognitive Patterns**: CoT, ToT, Research, ReAct, Debate, Reflection
- ✅ **Deep Research 2.0**: Iterative coverage with gap identification
- ✅ **Event Sourcing**: Append-only event log for crash recovery
- ✅ **WASM Execution**: Sandboxed pattern execution
- ✅ **Real-Time Streaming**: Live progress updates via Tauri events
- ✅ **Session Continuity**: Resume workflows after app restart
- ✅ **Time-Travel Debugging**: Replay workflows with breakpoints
- ✅ **Cloud+Embedded Compatible**: Single codebase, automatic mode detection

## Architecture

```
┌─────────────────────────────────────────────┐
│             Tauri Desktop App                │
│  ┌───────────────────────────────────────┐  │
│  │          Next.js Frontend             │  │
│  │  ┌─────────────────────────────────┐  │  │
│  │  │   WorkflowAPI (TypeScript)      │  │  │
│  │  │   - Auto mode detection         │  │  │
│  │  │   - Tauri commands (embedded)   │  │  │
│  │  │   - HTTP REST (cloud)           │  │  │
│  │  └─────────────────────────────────┘  │  │
│  └───────────────────────────────────────┘  │
│                    ↕ IPC                    │
│  ┌───────────────────────────────────────┐  │
│  │      Embedded Shannon API (Rust)      │  │
│  │  ┌─────────────────────────────────┐  │  │
│  │  │  EmbeddedWorkflowEngine         │  │  │
│  │  │  ├─ Pattern Registry            │  │  │
│  │  │  ├─ Event Bus                   │  │  │
│  │  │  ├─ Recovery Manager            │  │  │
│  │  │  └─ Session Manager             │  │  │
│  │  └─────────────────────────────────┘  │  │
│  │  ┌─────────────────────────────────┐  │  │
│  │  │  Durable Worker                 │  │  │
│  │  │  ├─ WASM Runtime                │  │  │
│  │  │  ├─ Event Log (SQLite)          │  │  │
│  │  │  └─ Checkpoint Manager          │  │  │
│  │  └─────────────────────────────────┘  │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

## Quick Start

### Installation

The embedded workflow engine is included by default in desktop builds:

```bash
cd desktop
npm install
npm run tauri:dev
```

### Basic Usage

#### Submit a Workflow (TypeScript)

```typescript
import { workflowAPI } from '@/lib/shannon/workflows';

// Submit workflow
const response = await workflowAPI.submitWorkflow({
  pattern_type: 'chain_of_thought',
  query: 'What is 2+2?',
  session_id: 'sess-123',
});

console.log('Workflow ID:', response.workflow_id);

// Get status
const status = await workflowAPI.getWorkflow(response.workflow_id);
console.log('Status:', status.status);

// Export workflow
const json = await workflowAPI.exportWorkflow(response.workflow_id);
```

#### Using Tauri Commands Directly

```typescript
import { invoke } from '@tauri-apps/api/core';

// Submit workflow
const response = await invoke('submit_workflow', {
  request: {
    pattern_type: 'research',
    query: 'What are quantum computers?',
  }
});

// Pause workflow
await invoke('pause_workflow', {
  workflowId: response.workflow_id
});

// Resume workflow
await invoke('resume_workflow', {
  workflowId: response.workflow_id
});
```

## Core Components

### EmbeddedWorkflowEngine

**Location**: [`rust/shannon-api/src/workflow/embedded/engine.rs`](../rust/shannon-api/src/workflow/embedded/engine.rs)

The main orchestration component that coordinates all workflow operations.

**Responsibilities**:
- Workflow submission and routing
- Pattern execution coordination
- Event streaming to UI
- State persistence and recovery
- Concurrency control

### Durable Worker

**Location**: [`rust/durable-shannon/src/worker/mod.rs`](../rust/durable-shannon/src/worker/mod.rs)

Executes WASM workflows with durable state management.

**Responsibilities**:
- WASM module instantiation
- Event sourcing (append-only log)
- Deterministic replay
- Checkpoint creation
- Concurrent workflow limits

### Pattern Registry

**Location**: [`rust/shannon-api/src/workflow/patterns/`](../rust/shannon-api/src/workflow/patterns/)

Manages cognitive pattern implementations.

**Available Patterns**:
- **Chain of Thought** - Step-by-step reasoning
- **Tree of Thoughts** - Branching exploration  
- **Research** - Web search with synthesis
- **ReAct** - Reason-Act-Observe loops
- **Debate** - Multi-agent discussion
- **Reflection** - Self-critique and improvement

## Database Schema

### SQLite Tables

**workflows**:
```sql
CREATE TABLE workflows (
    workflow_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    session_id TEXT,
    pattern_type TEXT NOT NULL,
    status TEXT NOT NULL,
    input TEXT NOT NULL,
    output TEXT,
    created_at INTEGER NOT NULL,
    completed_at INTEGER
);
```

**workflow_events**:
```sql
CREATE TABLE workflow_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_data BLOB NOT NULL,
    sequence INTEGER NOT NULL,
    timestamp INTEGER NOT NULL
);
```

**workflow_checkpoints**:
```sql
CREATE TABLE workflow_checkpoints (
    workflow_id TEXT PRIMARY KEY,
    sequence INTEGER NOT NULL,
    state_data BLOB NOT NULL,
    checksum INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
```

## Configuration

Configuration is in [`config/shannon.yaml`](../config/shannon.yaml):

```yaml
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
```

## Error Recovery

The engine includes comprehensive error recovery:

- **Circuit Breaker**: Opens after 5 failures, 60s cooldown
- **Exponential Backoff**: 1s, 2s, 4s, 8s... (capped at 60s)
- **Max Retries**: 3 attempts
- **Error Classification**: Network, Timeout, RateLimit, Permanent, Unknown

See [`docs/workflow-debugging.md`](workflow-debugging.md) for details.

## Performance Targets

| Metric | Target | Acceptable |
|--------|--------|-----------|
| Cold Start | <200ms | <250ms |
| Simple Task (CoT) | <5s | <7s |
| Research Task | <15min | <20min |
| Event Latency | <100ms | <150ms |
| Memory/Workflow | <150MB | <200MB |
| Concurrent Workflows | 5-10 | 3-5 |

## Migration from Cloud

If migrating from cloud Temporal workflows:

1. Replace orchestrator gRPC calls with embedded engine
2. Use same API endpoints (100% compatible)
3. Events match cloud format exactly
4. Sessions work the same way
5. No code changes needed in frontend

See [`docs/cloud-to-embedded-migration.md`](cloud-to-embedded-migration.md) for full guide.

## Troubleshooting

### Workflow Not Starting

Check that:
- LLM API keys are configured
- SQLite database is writable
- Max concurrent workflows not exceeded

### Events Not Streaming

Verify:
- Event bus initialized
- Tauri event listener registered
- Network connectivity (cloud mode)

### Checkpoint Corruption

Recovery steps:
- Falls back to full replay automatically
- Check disk space
- Verify zstd compression working

## Next Steps

- **Pattern Development**: See [`docs/workflow-patterns.md`](workflow-patterns.md)
- **Debugging**: See [`docs/workflow-debugging.md`](workflow-debugging.md)
- **Configuration**: See [`docs/workflow-configuration.md`](workflow-configuration.md)

## References

- [Architecture](rust-architecture.md)
- [Rust Coding Standards](coding-standards/RUST.md)
- [Testing Strategy](testing-strategy.md)
