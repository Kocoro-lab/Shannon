# Shannon Desktop UI - Architecture Plan

## Overview

Shannon Desktop is a native application for visualizing Shannon's multi-agent orchestration workflows in real-time. Unlike generic chat interfaces (ChatGPT, Cherry Studio), it exposes Shannon's unique capabilities: workflow strategies, multi-agent coordination, token budgeting, and research synthesis.

**Location**: `desktop/` (root-level directory)

---

## Why Not Generic Chat Apps?

**Architectural Mismatch**:
- Generic chat apps: Single-model conversations with basic streaming
- Shannon: Multi-agent orchestration with DAG/React/Research workflows

**Shannon-Specific Features**:
- Workflow pattern selection (SimpleTaskWorkflow, ResearchWorkflow, etc.)
- Multi-agent parallel execution tracking
- Per-agent cost and token usage
- Citation credibility scoring
- Session continuity with external ID mapping
- Gap-filling and iterative refinement

**Streaming Model**:
- Generic apps: Token-by-token from single LLM
- Shannon: Orchestration events (WORKFLOW_STARTED, AGENT_STARTED, TEAM_RECRUITED, etc.)

---

## Tech Stack

```
Desktop Wrapper: Tauri 2.0 (2-10 MB vs Electron's 80-120 MB)
├── Frontend: Next.js 15 + React 19
├── UI Components: shadcn/ui + Tailwind v4 (dark mode native)
├── Workflow Viz: React Flow (auto-layout DAG)
├── Streaming: Native EventSource (no AI SDK)
├── State: Redux Toolkit + Redux Persist
├── Local Cache: Dexie.js (IndexedDB)
└── Markdown: Streamdown + Shiki
```

**Why Tauri?**
- 75% smaller bundle size
- Rust backend aligns with Shannon's agent-core
- 50% less memory usage
- Mobile support path (iOS/Android)

---

## Core Architecture

### 1. Thread-Style Chat Interface

**Primary View**: Chat thread accumulating multi-round conversations

```
Session: "Quantum Computing Research"
┌─────────────────────────────────────────┐
│ User: What is quantum error correction? │
├─────────────────────────────────────────┤
│ Assistant: [ResearchWorkflow result]    │
│ • 5 agents executed                     │
│ • 3,400 tokens ($0.12)                  │
│ • Citations: 4 sources                  │
│ [Expand for event stream ▼]            │
├─────────────────────────────────────────┤
│ User: Compare topological vs surface... │
├─────────────────────────────────────────┤
│ Assistant: [In progress...]             │
│ ● Searcher-1 running                    │
│ ● Searcher-2 running                    │
│ ○ Synthesizer queued                    │
└─────────────────────────────────────────┘
```

**Message Structure**:
- User query → Shannon task submission
- Assistant response = final `result` from `GET /api/v1/tasks/{id}`
- Each message pair is collapsible to show:
  - Real-time event stream
  - Workflow graph visualization
  - Per-agent cost breakdown

### 2. Real-Time Event Streaming

**SSE Connection**: Native EventSource to `GET /api/v1/stream/sse?workflow_id={id}`

**Event Types** (per `docs/event-types.md`):
```typescript
// Core Events (always available)
- WORKFLOW_STARTED
- AGENT_STARTED
- AGENT_COMPLETED
- LLM_PARTIAL        // Incremental streaming
- LLM_OUTPUT         // Final complete answer
- TOOL_INVOKED
- TOOL_OBSERVATION
- ERROR_OCCURRED

// Feature-Gated Events (require specific flags)
- MESSAGE_SENT       // Requires p2p_v1
- MESSAGE_RECEIVED   // Requires p2p_v1
- WORKSPACE_UPDATED  // Requires p2p_v1
- TEAM_RECRUITED     // Requires dynamic_team_v1
- TEAM_RETIRED       // Requires dynamic_team_v1
```

**CRITICAL**: Use `LLM_PARTIAL` for real-time token accumulation, `LLM_OUTPUT` for final result.

**Resume Support**: Pass `last_event_id` header to resume from disconnect.

### 3. Workflow Visualization (React Flow)

**Purpose**: Read-only DAG showing agent execution flow

**Node Types**:
- Agent nodes (status: queued → running → completed → error)
- LLM call nodes
- Tool invocation nodes
- Team nodes (when dynamic_team_v1 enabled)

**Layout Strategy**:
- Auto-layout using dagre (top-to-bottom)
- Infer edges from event timing and order
- No explicit DAG structure in events (build heuristically)

**Data Sources**:
1. **Live Mode**: Build from SSE events as they arrive
2. **History Mode**: Fetch from Timeline API (`GET /api/v1/tasks/{id}/timeline?mode=full`)
3. **Replay Mode**: Step through timeline events with UI controls

**Node Details on Click**:
- Agent role/preset
- Model used (`metadata.agent_usages[].model`)
- Token counts (prompt/completion/total)
- Cost (`metadata.agent_usages[].cost_usd`)
- Execution duration
- Tool calls made

### 4. Task Submission & Settings

**API Endpoint**: `POST /api/v1/tasks`

**Supported Parameters** (aligned with `docs/task-submission-api.md`):

✅ **Correctly Mapped**:
```json
{
  "query": "...",
  "session_id": "...",              // Supports external ID mapping
  "research_strategy": "quick | standard | deep | academic",
  "model_tier": "small | medium | large",
  "max_concurrent_agents": 1-20,
  "context": {
    "gap_filling_enabled": true,
    "react_max_iterations": 1-5    // NOT top-level max_iterations
  }
}
```

⚠️ **Currently Not Available** (needs backend changes):
- **Workflow strategy name**: API returns `mode` (simple/standard/complex/supervisor) but NOT "React vs DAG vs Research"
  - Would need: `metadata.strategy` field added to TaskResult
- **Citation credibility tuning**: Credibility scores exist in `metadata.citations[].credibility_score` but are NOT tunable per-task
  - Desktop can only filter/visualize, not control threshold
- **Streaming toggle**: Streaming is UI behavior (open SSE or not), NOT an API flag

### 5. Session Management

**API Endpoints**:
- `GET /api/v1/sessions` - List all sessions with task counts
- `GET /api/v1/sessions/{id}` - Get session details
- `GET /api/v1/sessions/{id}/history` - All tasks in session
- `GET /api/v1/sessions/{id}/events` - Chat-optimized events (LLM_PARTIAL stripped)

**Features**:
- Auto-generated session titles (via GenerateSessionTitle activity)
- External ID mapping (Shannon's dual UUID system)
- Token usage per session (from API response)
- Total cost per session (aggregate `total_cost_usd` from task history)

**Session Sidebar**:
```
Sessions
├─ Quantum Computing Research
│  • 5 tasks
│  • 8,400 tokens ($0.32)
│  • Last: 2h ago
├─ LLM Model Comparison
│  • 2 tasks
│  • 1,800 tokens ($0.06)
│  • Last: 1d ago
└─ + New Session
```

### 6. Cost & Usage Analytics

**Data Availability**:

✅ **Post-Completion** (via `GET /api/v1/tasks/{id}`):
```json
{
  "usage": {
    "total_tokens": 3400,
    "prompt_tokens": 2100,
    "completion_tokens": 1300,
    "total_cost_usd": 0.12
  },
  "metadata": {
    "model_used": "gpt-5-nano",
    "provider": "openai",
    "agent_usages": [
      {
        "role": "searcher",
        "model": "gpt-5-nano",
        "total_tokens": 450,
        "cost_usd": 0.03
      }
    ]
  }
}
```

❌ **NOT Available in Real-Time**:
- SSE events do NOT carry token counts or costs
- Running cost counter requires polling `GET /api/v1/tasks/{id}`
- Tokens/second calculation not directly available

**Workaround for Live Metrics**:
- Poll task endpoint every 2-5 seconds during execution
- Or implement new event types (`TOKEN_USAGE_UPDATE`)

### 7. Workflow Replay & Timeline

**Timeline API**: `GET /api/v1/tasks/{id}/timeline?mode=full`

Returns deterministic event sequence from Temporal history.

**UI Controls**:
```
[◀◀] [▶] [⏸] [▶▶] [●]
Step  Play Pause Step  Export
Back              Fwd

Timeline: [====●=========>----------------]
          ^                                ^
       AGENT_1_STARTED              WORKFLOW_COMPLETED
```

**CRITICAL**: This is timeline stepping through stored events, NOT re-execution of Temporal workflow. Actual replay (for determinism testing) is CLI-only via `make replay`.

**Use Cases**:
- Debugging workflow execution
- Understanding agent coordination
- Exporting execution history
- Verifying event ordering

---

## UI Layout

```
┌─────────────────────────────────────────────────────┐
│  Shannon Desktop            🔔  ⚙️  ● Connected    │
├──────────┬──────────────────────────────────────────┤
│          │  ┌────────────────────────────────────┐  │
│ Sessions │  │ Task Submission                    │  │
│ ────────│  │ [Enter query...]                   │  │
│          │  │ Strategy: standard ▼  Tier: med ▼  │  │
│ ● Quantum│  │ [Submit]                           │  │
│   Comp   │  └────────────────────────────────────┘  │
│  5 tasks │                                           │
│          │  ┌────────────────────────────────────┐  │
│ ● LLM    │  │ Conversation Thread                │  │
│   Models │  ├────────────────────────────────────┤  │
│  2 tasks │  │ User: What is quantum computing?   │  │
│          │  │                                    │  │
│ + New    │  │ Assistant: Quantum computing is... │  │
│          │  │ • ResearchWorkflow                 │  │
│          │  │ • 3,400 tokens ($0.12)             │  │
│          │  │ • 4 citations                      │  │
│          │  │ [Show event stream ▼]              │  │
│          │  │                                    │  │
│          │  │ User: Compare error correction...  │  │
│          │  │                                    │  │
│          │  │ Assistant: [In progress...]        │  │
│          │  │ ● Searcher-1 running               │  │
│          │  │ ● Searcher-2 running               │  │
│          │  │ ○ Synthesizer queued               │  │
│          │  └────────────────────────────────────┘  │
└──────────┴──────────────────────────────────────────┘

[Click message to expand]
┌────────────────────────────────────────────────────┐
│ Workflow Graph                    Event Stream     │
├─────────────────────┬──────────────────────────────┤
│  [Query Parser]     │ ● WORKFLOW_STARTED           │
│         ↓           │ ● AGENT_STARTED (searcher-1) │
│    [Planner] ●      │ → LLM_PARTIAL: "Searching..."│
│     ↙  ↓  ↘         │ ● AGENT_STARTED (searcher-2) │
│  [S1][S2][S3] ✓     │ → TOOL_INVOKED: exa_search   │
│     ↘  ↓  ↙         │ → TOOL_OBSERVATION: {...}    │
│  [Synthesizer] ○    │ ● AGENT_COMPLETED (search-1) │
└─────────────────────┴──────────────────────────────┘
```

---

## Data Flow

```
User Input (Query)
    ↓
POST /api/v1/tasks
    ↓
{workflow_id} returned
    ↓
Open SSE: GET /api/v1/stream/sse?workflow_id={id}
    ↓
Events Stream In:
├─ WORKFLOW_STARTED → Show "processing" indicator
├─ AGENT_STARTED → Add node to graph, show agent badge
├─ LLM_PARTIAL → Accumulate streaming text
├─ LLM_OUTPUT → Finalize text chunk
├─ AGENT_COMPLETED → Update node status to completed
└─ (implicit completion) → Fetch final result
    ↓
GET /api/v1/tasks/{id} → Fetch complete result + metadata
    ↓
Display as assistant message in thread
Cache to Dexie.js (local persistence)
Update Redux (session state)
```

---

## Known Gaps & Future Enhancements

### Backend Changes Needed for Full Feature Set

1. **Workflow Strategy Metadata**
   - Add `metadata.strategy` field (e.g., "React", "Research", "DAG")
   - Add `metadata.patterns_used` array for multi-pattern workflows
   - Enable "Multi-Agent Pattern Inspector" without guesswork

2. **Real-Time Token/Cost Streaming**
   - Add `TOKEN_USAGE_UPDATE` event type
   - Include per-agent token counts in streaming events
   - Enable live cost counter without polling

3. **Citation Credibility Tuning**
   - Add `context.citation_credibility_threshold` parameter
   - Allow per-task control of strict/balanced/permissive filtering
   - Currently only UI-side filtering is possible

4. **Team Topology Visualization**
   - Add explicit parent/child relationships in TEAM_RECRUITED events
   - Currently requires heuristics to infer hierarchy

### UI-Only Limitations

1. **Edge Inference**
   - Workflow edges (sequential vs parallel) not explicit in events
   - Desktop builds edges from timing and event order
   - May not perfectly reflect Temporal workflow logic

2. **Replay vs Timeline**
   - UI replay steps through timeline events (read-only)
   - Actual Temporal replay (determinism testing) is CLI-only
   - No re-execution from desktop UI

3. **Session Cost Aggregation**
   - No pre-computed `session_cost_usd` field
   - Desktop must sum `total_cost_usd` from task history
   - Acceptable for desktop, but could optimize for web dashboard

---

## Implementation Phases

### Phase 1: Core Streaming MVP (Weeks 1-6)
- Tauri + Next.js scaffolding
- SSE streaming (LLM_PARTIAL + LLM_OUTPUT handling)
- Thread-style chat interface
- Task submission form
- Session list sidebar
- Basic result display with metadata

### Phase 2: Visualization (Weeks 7-10)
- React Flow integration
- Auto-layout DAG from events
- Node status animations
- Click for agent details
- Timeline API integration
- Event stream filtering

### Phase 3: Advanced Features (Weeks 11-14)
- Timeline replay controls
- Cost analytics dashboard
- Session export/import
- Multi-window support (separate graph inspector)
- Keyboard shortcuts
- Desktop notifications

### Phase 4: Polish (Weeks 15-16)
- Dark mode refinement
- Error handling & retry logic
- Performance optimization (large workflows)
- System tray integration
- Build & distribution (Windows, macOS, Linux)

---

## Key Differentiators

**Shannon Desktop is NOT:**
- Generic chat app (ChatGPT, Claude)
- Single-agent wrapper (Cherry Studio)
- Direct LLM interface

**Shannon Desktop IS:**
- Multi-agent orchestration visualizer
- Workflow strategy inspector
- Research synthesis platform
- Token budget monitor
- Enterprise session manager

**Must-Have Features**:
1. See which workflow pattern was executed (needs backend: metadata.strategy)
2. Track multiple agents running in parallel
3. Inspect per-agent costs and token usage
4. Replay timeline for debugging
5. Filter event streams by type
6. Export results with citations and metadata
7. Thread-style multi-turn conversations

---

## References

### Shannon Documentation
- [Event Types](./event-types.md) - SSE event specifications
- [Streaming API](./streaming-api.md) - SSE endpoint details
- [Task Submission API](./task-submission-api.md) - POST parameters
- [Task History & Timeline](./task-history-and-timeline.md) - GET endpoints
- [Gateway README](../go/orchestrator/cmd/gateway/README.md) - HTTP API reference

### Technology Documentation
- [Tauri 2.0](https://v2.tauri.app/start/)
- [Next.js 15](https://nextjs.org/docs)
- [shadcn/ui](https://ui.shadcn.com/)
- [React Flow](https://reactflow.dev/)
- [Redux Toolkit](https://redux-toolkit.js.org/)

### Similar Projects
- [LangGraph Studio](https://www.langchain.com/langgraph-studio)
- [AutoGen Studio](https://github.com/microsoft/autogen)

---

## Contributing

**Design Principles**:
1. Stream-first: Event stream is primary interface
2. Read-only MVP: Visualization before authoring
3. Dark mode native: Match modern AI tool aesthetic
4. Desktop-first: Leverage native features (notifications, multi-window)
5. Performance matters: Handle 100+ agents without lag

**Key Files to Monitor**:
- `docs/event-types.md` - SSE event model
- `docs/streaming-api.md` - SSE endpoint spec
- `go/orchestrator/cmd/gateway/internal/handlers/` - API implementations
- `config/research_strategies.yaml` - Available strategies

---

## License

Copyright (c) 2025 Shannon AI Platform. See main LICENSE file.
