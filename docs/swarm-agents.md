# Swarm Agents

## Overview

Swarm mode deploys persistent, autonomous agents orchestrated by a **Lead Agent** through an event-driven decision loop with a **file_read inner loop**. The Lead decomposes the user query into tasks, spawns role-specialized agents, monitors their progress through events (idle, completed, checkpoint), reads workspace files to verify quality (zero LLM cost), and coordinates multi-phase execution until all work is done. A closing checkpoint decides whether the Lead can reply directly or trigger LLM synthesis.

Agents run reason-act loops for up to 50 iterations, using tools, sharing findings through a workspace filesystem, and going **idle** (not done) when their current task is complete — with a mandatory **QUALITY SELF-CHECK** and `key_findings` before idle. The Lead controls agent lifecycle and can reassign idle agents to new tasks. Agents can escalate issues to the Lead via `send_message` to `"lead"`.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                          SwarmWorkflow                                │
│  Phase 1: Lead initial_plan → Phase 2: event loop → Phase 3: close  │
└──────────┬──────────────────────┬────────────────────┬───────────────┘
           │                      │                    │
     ┌─────▼──────┐        ┌─────▼──────┐      ┌──────▼──────┐
     │ AgentLoop   │        │ AgentLoop   │      │ Lead Agent  │
     │ (Takao)     │        │ (Mitaka)    │      │ /lead/decide│
     │ role:       │        │ role:       │      │ event-driven│
     │ researcher  │        │ analyst     │      │ loop        │
     └──────┬──────┘        └──────┬──────┘      └──────┬──────┘
            │                      │                     │
            │   idle / completed   │    agent_idle /     │
            │   ─────────────────► │    agent_completed  │
            │                      │    ────────────────►│
            │                      │                     │
      ┌─────▼──────────────────────▼─────┐               │
      │     Session Workspace (Files)     │◄──── reads ───┘
      │   file_write / file_read / etc.   │
      └───────────────────────────────────┘
```

**Key components:**

| Component | File | Purpose |
|-----------|------|---------|
| SwarmWorkflow | `go/.../workflows/swarm_workflow.go` | Top-level Temporal workflow (Lead loop + agent coordination) |
| AgentLoop | Same file (child workflow) | Per-agent reason-act loop |
| LeadDecision | `go/.../activities/lead.go` | Activity calling `/lead/decide` + file_read inner loop |
| Lead protocol | `python/.../roles/swarm/lead_protocol.py` | Lead system prompt, event types, 12 actions |
| Agent protocol | `python/.../roles/swarm/agent_protocol.py` | Agent system prompt, 4 actions + QUALITY SELF-CHECK |
| Role prompts | `python/.../roles/swarm/role_prompts.py` | 12 role-specific methodology prompts |
| Lead endpoint | `python/.../api/lead.py` | `/lead/decide` HTTP endpoint |
| P2P + TaskList | `go/.../activities/p2p.go` | Mailbox, workspace, task CRUD (Redis) |
| Config | `config/features.yaml` + `go/.../activities/config.go` | Swarm parameters and defaults |
| Synthesis template | `config/templates/synthesis/swarm_default.tmpl` | Final output formatting |
| Agent names | `go/.../agents/names.go` | Deterministic station-name generation |
| Stream events | `go/.../activities/stream_events.go` | SSE event emission |

## Lifecycle

### Phase 1: Lead Initial Planning

SwarmWorkflow receives a `TaskInput` with `"force_swarm": true` in context. Instead of a static decomposition, the **Lead Agent** makes the first decision:

1. Lead receives an `initial_plan` event containing the user query
2. Lead decomposes the query into tasks (with IDs like `T1`, `T2`, `T3`)
3. Lead decides which agents to spawn, assigning each a **role** and **task**
4. Lead can set `depends_on` relationships between tasks

The Lead's actions from initial planning are executed: tasks stored in Redis, agents spawned as child workflows with role-specific prompts.

### Phase 2: Agent Execution + Lead Event Loop

Each agent runs an **AgentLoop** (child workflow) with up to `max_iterations_per_agent` (default: 50) reason-act cycles.

**Agent iteration loop:**
```
┌──────────────────────────────────────────────────────────────┐
│  1. Non-blocking shutdown check (signal from Lead)           │
│  2. Inject context: Task Board + Running Notes + Role prompt │
│  3. Call LLM (/agent/loop) with history + workspace state    │
│  4. Execute action:                                          │
│     • tool_call  → run tool, record result                   │
│     • publish_data → share findings with team workspace      │
│     • send_message → P2P to specific agent                   │
│     • idle → signal parent, wait for reassignment            │
│  5. If agent says "done" and TeamRoster exists → convert to  │
│     "idle" (Lead controls lifecycle)                         │
│  6. Check convergence / error thresholds                     │
│  7. Loop or exit                                             │
└──────────────────────────────────────────────────────────────┘
```

**Lead event-driven loop** (runs concurrently):

The Lead monitors agents through Temporal signals and timers:

| Event Type | Trigger | Lead Response |
|-----------|---------|---------------|
| `agent_idle` | Agent finished task, waiting | Assign new task, spawn helper, or shutdown |
| `agent_completed` | Agent child workflow done | Quality gate (ACCEPT/RETRY), update task status |
| `checkpoint` | Every 2 minutes | Review progress, revise plan if needed |
| `human_input` | User sends HITL message | Incorporate feedback, adjust plan |

For each event, the Lead calls `/lead/decide` with: current TaskList, agent states, budget, decision history, and any agent→Lead messages. The Lead returns actions that the workflow executes. If the Lead returns `file_read` actions, the workflow reads the requested files and calls `/lead/decide` again with file contents injected — this **file_read inner loop** runs up to 3 rounds per event at zero LLM cost. The Lead can also return `interim_reply` to send user-visible progress messages without interrupting the workflow.

### Phase 3: Closing & Synthesis

Once all agents are complete (or budget exhausted):

1. **Build closing summary** — collect agent results + workspace file listing
2. **Lead closing checkpoint** — Lead reviews all work with `closing_checkpoint` event
3. **Lead chooses ONE action** (first action wins, rest ignored):
   - `reply`: Lead provides a direct answer. Validated by `isLeadReplyValid()` against workspace files — if invalid, falls back to synthesis. Bypasses synthesis LLM call when valid.
   - `synthesize`: Trigger full LLM synthesis using `swarm_default.tmpl` template
   - `done`: Legacy/backward-compatible — triggers synthesis
4. **Return** — TaskResult with metadata (per-agent summaries, iterations, tokens, models)

**Reply guidelines**: If a `synthesis_writer` or analyst already wrote a comprehensive report, Lead reply can be short (3-5 sentences) referencing the report file path. If no dedicated synthesis agent, Lead reply should comprehensively answer the user query.

## Triggering Swarm Mode

### API Payload

```bash
curl -X POST /api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Compare AI chip markets across US, Japan, and South Korea",
    "session_id": "session-123",
    "context": {
      "force_swarm": true
    }
  }'
```

**Important**: `force_swarm: true` in context is the **only** way to trigger SwarmWorkflow. The early routing in `orchestrator_router.go` checks `GetContextBool(input.Context, "force_swarm")` before decomposition. Using `execution_mode: swarm` does **NOT** trigger SwarmWorkflow — it goes through normal decompose → DAG.

### HITL: Mid-Execution Human Input

Users can send messages to a running swarm via:

```bash
POST /api/v1/swarm/{workflowID}/message
Content-Type: application/json

{"message": "Focus more on Samsung's foundry strategy"}
```

The message arrives as a `human_input` event in the Lead's event loop, and the Lead adjusts the plan accordingly.

## Lead Agent

### Event Types

The Lead wakes up on these events:

| Event | When | Typical Actions |
|-------|------|-----------------|
| `initial_plan` | Workflow start | `interim_reply` (ALWAYS) + `revise_plan` (create tasks) + `spawn_agent` |
| `agent_idle` | Agent finished current task | `file_read` (verify), `assign_task`, `shutdown_agent`, or `noop` |
| `agent_completed` | Agent child workflow ended | Quality gate with optional `file_read`, spawn replacement if needed |
| `checkpoint` | Every 2 minutes | Review, `revise_plan`, `broadcast` |
| `human_input` | User HITL message | `interim_reply` (ALWAYS) + adjust plan based on feedback |
| `closing_checkpoint` | All agents done | `reply` or `synthesize` (exactly ONE action) |

### Available Actions

| Action | Parameters | Effect |
|--------|-----------|--------|
| `interim_reply` | `content` (1-3 sentences) | User-visible progress message; NOT terminal, execution continues |
| `spawn_agent` | `role`, `task_description`, `task_id`, `model_tier` | Create new AgentLoop child workflow |
| `assign_task` | `task_id`, `agent_id`, `task_description`, `model_tier` | Reassign idle agent to new task |
| `send_message` | `to`, `content` | P2P message to specific agent |
| `broadcast` | `content` | Message all agents |
| `revise_plan` | `create` (new tasks with `depends_on`), `cancel` (task IDs) | Update TaskList dynamically |
| `file_read` | `path` | Read workspace file for verification (zero LLM cost, triggers inner loop) |
| `shutdown_agent` | `agent_id` | Gracefully stop agent |
| `noop` | — | Wait for running agents (NEVER use `done` while agents running) |
| `done` | — | All agents complete, proceed to synthesis |
| `reply` | `content` (Markdown) | Direct answer, bypass synthesis (closing_checkpoint only) |
| `synthesize` | — | Trigger LLM synthesis (closing_checkpoint only) |

### Quality Gate

On every `agent_completed` or `agent_idle` event, the Lead runs a quality gate:

**Step 1 — Optional file verification (zero LLM cost):**
- Agent wrote files → use `file_read` to check main deliverable content
- Agent claims specific numbers → verify in file
- Skip only for simple tasks or when `key_findings` are clearly concrete with data

**Step 2 — Decision:**

| Decision | Criteria | Action |
|----------|----------|--------|
| **ACCEPT** (default) | `key_findings` contain specific data/numbers/evidence; file content (if read) confirms substance | Continue |
| **RETRY ONCE** (rare) | Empty/broken results < 50 words, or file empty/doesn't match requirements | Assign ONE follow-up with "CONTINUE:" prefix |
| **ANTI-SPIRAL** | Prevent verification loops | NEVER create verify/check/diagnostic tasks; `file_read` is NOT a verification task (0 LLM cost); when 2+ agents idle + no pending tasks + 0 running → `done` IMMEDIATELY; when agents running → use `noop` |

### Lead File Read Inner Loop

When the Lead needs to verify agent output before making quality decisions, it can use `file_read` actions:

```
Event: agent_completed (researcher)
  → Lead returns: file_read path="/research/findings.md"
  → Go reads file (max 4000 chars, truncation flag if longer)
  → Go calls /lead/decide again with file_contents injected
  → Lead sees actual content, makes ACCEPT/RETRY decision
```

**Constraints:**
- Max 3 file read rounds per event (prevents infinite loops)
- Max 3 files per round
- Max 4000 chars per file (truncated with `[TRUNCATED]` marker)
- Zero LLM cost — pure file I/O, no token usage

### Adaptive Planning

After reviewing a completed agent's output, the Lead can dynamically revise the plan:

- Agent discovered something warranting a NEW follow-up task → `revise_plan` to create it (with `depends_on`)
- Agent found the original scope was wrong → `revise_plan` to cancel irrelevant tasks
- Findings reveal a gap not in original plan → create gap-fill task + assign idle agent

**Constraints:**
- NEVER create verify/check/diagnostic tasks (anti-spiral)
- NEVER revise just because results are imperfect — accept and move on
- Max 2 new tasks per revision — keep scope controlled
- Only revise when agent findings reveal genuinely NEW information

### Interim Reply (User Progress Updates)

The Lead can send user-visible progress messages via `interim_reply` without interrupting the workflow:

**When to include:**
- `initial_plan`: ALWAYS — describe approach before spawning agents
- Phase change (e.g., research complete → synthesis): YES
- `human_input` response: ALWAYS — acknowledge and explain adjustment

**When NOT to include:**
- Routine `noop`/waiting: NEVER
- `checkpoint`: NEVER

**Rules:**
- Max 1 `interim_reply` per decision
- Reply in same language as user's query
- Focus on WHAT is happening for user, not agent internals (no agent names, task IDs)

### LLM Configuration

- **Model tier**: MEDIUM (always)
- **Temperature**: 0.3
- **Max tokens**: 2048 (closing checkpoint: 4096)
- **System prompt**: `LEAD_SYSTEM_PROMPT` from `lead_protocol.py`
- **Assistant prefill**: `{` (ensures valid JSON output)

## Role System

### 3-Layer Prompt Architecture

```
┌─────────────────────────────────┐
│  Layer 1: Core Protocol         │  ← AGENT_LOOP_SYSTEM_PROMPT (shared by all agents)
│  Available actions, memory mgmt │     agent_protocol.py
├─────────────────────────────────┤
│  Layer 2: Role Methodology      │  ← SWARM_ROLE_PROMPTS[role] (per-role specialization)
│  Domain expertise, approach     │     role_prompts.py
├─────────────────────────────────┤
│  Layer 3: Dynamic Context       │  ← Built per iteration (task, team, history, budget)
│  Task, workspace, inbox, notes  │     agent.py / swarm_workflow.go
└─────────────────────────────────┘
```

### Role Catalog (12 Roles)

9 core roles with swarm methodology prompts in `SWARM_ROLE_PROMPTS`, plus 3 extended roles using `presets.py` system prompts:

| Role | Specialization | Type |
|------|---------------|------|
| `researcher` | Information gathering, market analysis, fact-finding | Core |
| `company_researcher` | Company due diligence, corporate background, competitive landscape | Core |
| `analyst` | Data analysis, statistics, comparisons, charts | Core |
| `financial_analyst` | Financial analysis, valuation, bull/bear cases, risk assessment | Core |
| `planner` | Strategic decomposition, research design, dependency mapping | Core |
| `critic` | Critical review, verification, gap-finding | Core |
| `coder` | Code implementation, scripting, debugging | Core |
| `generalist` | Flexible, any simple/mixed task | Core |
| `synthesis_writer` | Synthesize all findings into structured report | Core |
| `writer` | Technical writing, reports, documentation | Extended |
| `browser_use` | Web browser automation, page interaction | Extended |
| `deep_research_agent` | Extended multi-step deep research | Extended |

### Model Tier Selection

The Lead assigns a `model_tier` per agent when spawning:

| Tier | Use Case |
|------|----------|
| `small` | Fast, cheap — simple lookups, formatting |
| `medium` | Balanced — analysis, research, reasoning (default) |
| `large` | Most capable — complex code, deep analysis |

## Agent Actions

Each iteration, the LLM returns exactly one action as JSON:

### tool_call

Execute a tool from the available set:

```json
{"action": "tool_call", "tool": "web_search", "tool_params": {"query": "NVIDIA H100 market share 2025"}}
```

Available tools include: `web_search`, `python_executor`, `file_write`, `file_read`, `file_edit`, `file_list`, `file_search`, `diff_files`, `json_query`, `web_fetch`, `web_subpage_fetch`, `web_crawl`, `calculator`.

### send_message

Direct P2P message to a specific teammate or Lead (for escalation):

```json
{"action": "send_message", "to": "mitaka", "message_type": "info", "payload": {"message": "Check Samsung foundry plans"}}
```

**Agent→Lead escalation** — agents can send messages to `"lead"` to report blockers or request guidance:

```json
{"action": "send_message", "to": "lead", "message_type": "request", "payload": {"message": "Cannot find reliable enterprise adoption data for Svelte - should we adjust scope?"}}
```

Lead receives these in the `## Agent Messages` section of the next decision context.

### publish_data

Share findings with the team via workspace:

```json
{"action": "publish_data", "topic": "findings", "data": "US market dominated by NVIDIA with 80% share"}
```

### idle

Signal task completion and wait for reassignment. Requires **QUALITY SELF-CHECK** before going idle:

1. Did I address ALL aspects of task description?
2. Are findings backed by specific data, numbers, evidence?
3. Did I miss any dimension? → If yes, one more search before idle

```json
{
  "decision_summary": "Saved report to research/takao-us-market.md, self-check passed",
  "notes": "see file at research/takao-us-market.md",
  "action": "idle",
  "key_findings": [
    "NVIDIA holds 80% US AI chip market share (2025 Q3)",
    "AMD gained 12% with MI300X, up from 8% YoY",
    "Intel Gaudi3 at 5% share, focused on inference workloads"
  ],
  "response": "Completed US market analysis. Full report in research/takao-us-market.md"
}
```

**`key_findings`**: 3-5 bullet points with concrete data. The Lead uses these to assess quality without reading full files (though it can `file_read` to verify).

**Note**: If an agent returns `done` while in a swarm (TeamRoster present), the workflow automatically converts it to `idle` — the Lead controls agent lifecycle. The done response is preserved in `savedDoneResponse` for the idle signal.

### Running Notes

Agents have a `notes` field that persists across all turns as working memory. Unlike tool results that get truncated in history, notes survive the full session:

```json
{"action": "tool_call", "tool": "web_search", "tool_params": {"query": "..."}, "notes": "Key finding: NVIDIA holds 80% market share. Need to verify Samsung data next."}
```

## Task Management

### SwarmTask Structure

```go
type SwarmTask struct {
    ID          string   `json:"id"`            // e.g. "T1", "T2"
    Description string   `json:"description"`
    Status      string   `json:"status"`        // "pending", "in_progress", "completed"
    Owner       string   `json:"owner"`         // agent ID
    CreatedBy   string   `json:"created_by"`    // "decompose" or agent ID
    DependsOn   []string `json:"depends_on"`    // task IDs that must complete first
    CreatedAt   string   `json:"created_at"`    // RFC3339
    CompletedAt string   `json:"completed_at,omitempty"`
}
```

### Redis Storage

Tasks are stored in a Redis hash:

```
Key: wf:{workflow_id}:tasklist
Field: {task_id}    (e.g. "T1")
Value: {json}       (serialized SwarmTask)
```

### Task Activities

| Activity | Purpose |
|----------|---------|
| `InitTaskList` | Bulk-write tasks from Lead's initial plan |
| `GetTaskList` | Fetch all tasks, sorted by ID |
| `UpdateTaskStatus` | Validate transition, update status/owner |
| `ClaimTask` | Atomic Lua script for task claiming |
| `CreateTask` | Add new task (from `revise_plan`) |

### Status Transitions

```
pending ──► in_progress ──► completed
   │                           ▲
   └───────────────────────────┘  (Lead can directly complete/cancel)
         in_progress ──► in_progress (Lead reassign)
```

### Dependency Enforcement

Tasks with `depends_on` cannot be spawned/assigned until all dependencies are `completed`. The workflow checks via `taskHasUnmetDeps()` before allowing the Lead to assign work.

## P2P Messaging

Agents communicate through Redis-backed mailboxes.

### Message Types

| Type | Constant | Use Case |
|------|----------|----------|
| `request` | `MessageTypeRequest` | Task delegation or help request |
| `offer` | `MessageTypeOffer` | Offer to assist |
| `accept` | `MessageTypeAccept` | Accept a task |
| `delegation` | `MessageTypeDelegation` | Delegate a subtask |
| `info` | `MessageTypeInfo` | General information sharing |

### Redis Keys

```
wf:{workflow_id}:mbox:{agent_id}:seq    # Atomic counter
wf:{workflow_id}:mbox:{agent_id}:msgs   # List of JSON messages
```

Each message:
```json
{
  "seq": 1,
  "from": "takao",
  "to": "mitaka",
  "type": "info",
  "payload": {"message": "Found relevant data"},
  "ts": 1707782400000000000
}
```

All keys have 48-hour TTL for automatic cleanup.

## Shared Workspace

Agents share findings through topic-based workspace lists in Redis and through session workspace files.

### Redis Workspace Keys

```
wf:{workflow_id}:ws:seq          # Global sequence counter
wf:{workflow_id}:ws:{topic}      # List of entries per topic
```

### File-as-Memory Pattern

Agents externalize large results to workspace files:

1. **Tool results > 500 chars** → save to file, note filename
2. **Before going idle** → write full findings to `findings/{agent-id}-{topic}.md`
3. **Idle response** → short summary only (under ~1500 chars)
4. **Before researching** → check teammates' files with `file_read` + `file_list`

File naming convention:
- Reports: `findings/{agent-id}-{topic}.md`
- Code: appropriate source files
- Data: `data/` directory

## Convergence Detection

Three mechanisms prevent agents from looping indefinitely:

### 1. No-Progress Convergence

If an agent takes 3 consecutive non-tool actions (only idle without progress), it converges. Reset on any `tool_call`, `send_message`, or `publish_data` action (collaborative actions count as progress).

### 2. Consecutive Error Abort

If 3 consecutive **permanent** tool errors occur (not transient), the agent aborts. Reset on any successful tool execution.

### 3. Max Iterations Force-Done

On the last iteration, if the agent hasn't gone idle, the workflow forces completion. Two iterations before the limit, the prompt adds a warning.

## Smart Retry

Transient errors get automatic retry with backoff; permanent errors count toward abort.

| Error Type | Behavior | Backoff | Counts Toward Abort? |
|-----------|----------|---------|---------------------|
| Transient | Retry with backoff | 5s x attempt (max 30s) | No |
| Permanent | Record failure | None | Yes (3 strikes) |

Backoff uses `workflow.Sleep()` for Temporal determinism.

## Idle Snapshot Fallback

When a child workflow times out (e.g., agent was idle between phases), the swarm falls back to the **last idle snapshot** captured before the timeout:

1. **Capture**: On every `agent_idle` signal, save `AgentLoopResult` to `idleSnapshots` map
2. **Fallback**: If `future.Get()` returns `!result.Success`, check `idleSnapshots[agentID]`
3. **Use snapshot**: If snapshot has a longer response than the error result, use it
4. **Multi-phase context**: When reassigning an agent, inject previous idle snapshot as context (truncated to 1500 chars)

This was critical for fixing the multi-phase swarm empty result bug where agents timed out between phases.

## Budget Management

The swarm enforces three budget limits:

| Budget | Default | Source |
|--------|---------|--------|
| Max LLM calls | 200 | Hardcoded in SwarmWorkflow |
| Max tokens | 500,000 | Hardcoded in SwarmWorkflow |
| Max wall-clock | 30 minutes | `features.yaml` `max_wall_clock_minutes` |

**Tracking**: Each Lead decision and agent iteration increments `budgetTotalLLMCalls` and `budgetTotalTokens`. Wall clock is measured from `swarmStartTime`.

**Budget info** is passed to Lead in every decision via `LeadBudget`:
```go
type LeadBudget struct {
    TotalLLMCalls       int
    RemainingLLMCalls   int
    TotalTokens         int
    RemainingTokens     int
    ElapsedSeconds      int
    MaxWallClockSeconds int
}
```

**Exhaustion**: If any limit is exceeded, the workflow forces exit from the event loop and proceeds to closing/synthesis.

## Tiered History

Agent context grows with each iteration. Tiered truncation keeps it manageable:

| Iteration Age | Max Chars | Rationale |
|--------------|-----------|-----------|
| Last 3 turns | 4,000 | Full detail for recent work |
| Older turns | 500 | Summary only |

### Token-Aware Trimming

If the full prompt exceeds 400K chars (~100K tokens), the oldest history entries are dropped until it fits, keeping a minimum of 3 recent entries.

## Configuration

### features.yaml

```yaml
workflows:
  swarm:
    enabled: true
    max_agents: 10                    # Max total agents (initial + dynamic)
    max_iterations_per_agent: 50      # Max reason-act loops per agent
    agent_timeout_seconds: 1800       # Per-agent timeout (must cover idle wait between phases)
    max_messages_per_agent: 20        # Max P2P messages per agent
    workspace_snippet_chars: 800      # Max chars per workspace entry in prompt
    workspace_max_entries: 5          # Max recent entries shown to agents
    # Global budget controls
    max_total_llm_calls: 200          # Safety net for dynamic spawn + idle + Lead overhead
    max_total_tokens: 500000          # Total tokens across all agents + Lead
    max_wall_clock_minutes: 30        # Hard wall-clock limit for entire swarm workflow
```

### Go Defaults (config.go)

If YAML values are missing or zero, these defaults apply:

| Parameter | Default | Notes |
|-----------|---------|-------|
| `SwarmMaxAgents` | 10 | Total cap including dynamic spawns |
| `SwarmMaxIterationsPerAgent` | 25 | Per-agent iteration limit (YAML overrides to 50) |
| `SwarmAgentTimeoutSeconds` | 1200 | 20 minutes per agent (YAML overrides to 1800) |
| `SwarmMaxMessagesPerAgent` | 20 | P2P message cap |
| `SwarmWorkspaceSnippetChars` | 800 | Truncation for prompt injection |
| `SwarmWorkspaceMaxEntries` | 5 | Recent entries per topic |

### Temporal Timeouts

| Activity | Timeout | Retries |
|----------|---------|---------|
| Agent LLM call (`/agent/loop`) | 30-90s | 2 |
| Lead decision (`/lead/decide`) | 90s | 2 |
| P2P activities (mailbox, workspace) | 10s | 1 |
| Event emission | 5s | 1 |
| Synthesis | 10 min | 3 |
| AgentLoop child workflow | `agent_timeout_seconds` | — |

## Streaming Events

SwarmWorkflow emits SSE events for real-time dashboards:

| Event Type | Agent ID | Payload | When |
|-----------|----------|---------|------|
| `WORKFLOW_STARTED` | `swarm-lead` | — | Workflow begins |
| `AGENT_STARTED` | `{agent-name}` | `{role: "researcher"}` | Agent spawned with role |
| `AGENT_COMPLETED` | `{agent-name}` | — | Agent finishes (idle/converged/aborted) |
| `TASKLIST_UPDATED` | `tasklist` | `{tasks: SwarmTask[]}` | Task list changed (init, status update, create) |
| `LEAD_DECISION` | `swarm-lead` | `{event_type, actions_count}` | Lead coordination decision |
| `INTERIM_REPLY` | `swarm-lead` | `{message: string}` | Lead progress update for user (via LLM_OUTPUT stream) |
| `MESSAGE_SENT` | `{sender}` | — | P2P message sent |
| `MESSAGE_RECEIVED` | `{receiver}` | — | P2P message delivered |
| `WORKSPACE_UPDATED` | `workspace` | — | New workspace entry published |
| `WORKFLOW_COMPLETED` | `swarm-lead` | — | Final synthesis complete |

### TASKLIST_UPDATED Payload

```json
{
  "type": "TASKLIST_UPDATED",
  "agent_id": "tasklist",
  "message": "task=T1 status=in_progress",
  "payload": {
    "tasks": [
      {
        "id": "T1",
        "description": "Research US AI chip market",
        "status": "in_progress",
        "owner": "takao",
        "created_by": "decompose",
        "depends_on": [],
        "created_at": "2026-02-26T10:00:00Z"
      },
      {
        "id": "T2",
        "description": "Research Japan AI chip market",
        "status": "pending",
        "owner": "",
        "depends_on": ["T1"],
        "created_at": "2026-02-26T10:00:00Z"
      }
    ]
  }
}
```

### Streaming Example

```bash
# Watch swarm task board and agent lifecycle
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=TASKLIST_UPDATED,AGENT_STARTED,AGENT_COMPLETED,LEAD_DECISION"
```

## Agent Prompt Structure

Each `/agent/loop` call builds this prompt:

```
[System]
  Layer 1: AGENT_LOOP_SYSTEM_PROMPT
    - Available actions (tool_call, publish_data, send_message, idle)
    - Memory management rules (file-as-memory, notes)
    - Work phases: ORIENT → EXECUTE → SAVE & COMPLETE
    - Collaboration rules (check before duplicating, share via files)

  Layer 2: ROLE METHODOLOGY (from role_prompts.py)
    - Domain-specific approach and expertise
    - e.g. "researcher" methodology, "analyst" methodology

[User]
  ## Task
  {agent's task description, with previous work context if reassigned}

  ## Your Team (shared session workspace)
  - **takao (you)** [researcher]: "Research US AI chip market"
  - mitaka [analyst]: "Analyze comparative data"
  - kichijoji [writer]: "Write final report"

  ## Task Board
  - T1 [completed] Research US AI chip market (owner: takao)
  - T2 [in_progress] Analyze data (owner: mitaka)
  - T3 [pending] Write report → depends on T1, T2

  ## Running Notes (persistent working memory)
  {accumulated notes from previous iterations}

  ## Shared Findings (workspace)
  - takao: NVIDIA dominates US with 80% market share...
  - mitaka: Japan focuses on edge AI chips...

  ## Previous Actions
  - Iteration 0: tool_call:web_search → {full 4000-char result}   ← recent
  - Iteration 1: tool_call:web_search → {full 4000-char result}   ← recent
  - Iteration 2: publish_data → {full 4000-char result}           ← recent
  - Iteration 3: tool_call:web_search → {500-char summary}        ← older

  ## Inbox Messages
  - From mitaka (info): {"message": "Check Samsung foundry plans"}

  ## Budget: Iteration 4 of 50

[Assistant prefill] {
```

The `{` prefill ensures the LLM returns valid JSON starting with `{`.

## Model Tier

Agent LLM calls use the tier assigned by the Lead (default: **MEDIUM**) with:
- Temperature: 0.3
- Max output tokens: 2,048

Lead decisions always use **MEDIUM tier**.

Final synthesis (if triggered) uses the standard synthesis path which forces **LARGE tier**.

## Key Source Files

| File | What It Does |
|------|-------------|
| `go/orchestrator/internal/workflows/swarm_workflow.go` | SwarmWorkflow + AgentLoop Temporal workflows (~2300 lines) |
| `go/orchestrator/internal/activities/lead.go` | LeadDecision activity + file_read inner loop, LeadEvent/LeadAction/LeadBudget types |
| `go/orchestrator/internal/activities/p2p.go` | P2P mailbox, workspace, SwarmTask CRUD, Redis operations |
| `go/orchestrator/internal/activities/stream_events.go` | SSE event type constants and emission |
| `go/orchestrator/internal/activities/config.go` | SwarmConfig struct and fallback defaults |
| `go/orchestrator/internal/agents/names.go` | Agent name generation (Japanese station names) |
| `python/llm-service/llm_service/api/lead.py` | `/lead/decide` endpoint, prompt construction, response parsing |
| `python/llm-service/llm_service/roles/swarm/lead_protocol.py` | `LEAD_SYSTEM_PROMPT` — Lead event handling + actions |
| `python/llm-service/llm_service/roles/swarm/agent_protocol.py` | `AGENT_LOOP_SYSTEM_PROMPT` — agent actions + QUALITY SELF-CHECK + key_findings |
| `python/llm-service/llm_service/roles/swarm/role_prompts.py` | `SWARM_ROLE_PROMPTS` — 12 role methodology definitions |
| `config/features.yaml` | Swarm configuration section (L114-128) |
| `config/templates/synthesis/swarm_default.tmpl` | Swarm synthesis output template |
