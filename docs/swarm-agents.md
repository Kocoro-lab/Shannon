# Swarm Agents

## Overview

Swarm mode deploys persistent, autonomous agents that coordinate through peer-to-peer messaging and a shared workspace. Each agent runs a reason-act loop for up to 25 iterations, using tools, sharing findings, and converging independently. A supervisor monitors execution and can dynamically spawn helper agents on demand.

Inspired by patterns from Claude Code (long-running iteration loops, convergence detection) and Manus (file-as-memory, externalized artifacts).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      SwarmWorkflow                           │
│  (Temporal workflow: decompose → spawn → monitor → synth)    │
└──────────┬────────────────────────────────┬─────────────────┘
           │                                │
     ┌─────▼─────┐                    ┌─────▼─────┐
     │ AgentLoop  │ ◄──── P2P ────►   │ AgentLoop  │   (child workflows)
     │  (Takao)   │     messages       │  (Mitaka)  │
     └─────┬──────┘                    └─────┬──────┘
           │                                 │
     ┌─────▼─────────────────────────────────▼─────┐
     │            Shared Workspace (Redis)          │
     │     publish_data / WorkspaceListAll          │
     └─────────────────────────────────────────────┘
```

**Key components:**

| Component | File | Purpose |
|-----------|------|---------|
| SwarmWorkflow | `go/orchestrator/internal/workflows/swarm_workflow.go` | Top-level Temporal workflow |
| AgentLoop | Same file (child workflow) | Per-agent reason-act loop |
| P2P activities | `go/orchestrator/internal/activities/p2p.go` | Mailbox + workspace Redis operations |
| Agent LLM endpoint | `python/llm-service/llm_service/api/agent.py` | `/agent/loop` — builds prompt, calls LLM |
| Config | `config/features.yaml` + `go/.../activities/config.go` | Swarm parameters and defaults |

## Lifecycle

### Phase 1: Decomposition

SwarmWorkflow receives a `TaskInput` with `"force_swarm": true` in context. It decomposes the query into subtasks (via the standard decomposition activity or a preplanned decomposition), capping at `swarm_max_agents`.

### Phase 2: Spawn

For each subtask, SwarmWorkflow spawns an **AgentLoop child workflow** with:
- A deterministic agent name (Japanese station name via `agents.GetAgentName(workflowID, index)`)
- The subtask description
- A team roster (all agent IDs and their tasks)
- Workspace guardrails (max entries, snippet char limits)

### Phase 3: Agent Iteration Loop

Each AgentLoop runs up to `max_iterations` (default: 25) reason-act cycles:

```
┌──────────────────────────────────────────────────┐
│  1. Fetch mailbox (FetchAgentMessages)           │
│  2. Fetch workspace (WorkspaceListAll)           │
│  3. Call LLM (/agent/loop) with full context     │
│  4. Execute action:                              │
│     • tool_call → run tool, record result        │
│     • publish_data → append to shared workspace  │
│     • send_message → P2P to specific agent       │
│     • request_help → ask supervisor to spawn     │
│     • done → return final response               │
│  5. Check convergence / error thresholds         │
│  6. Loop or exit                                 │
└──────────────────────────────────────────────────┘
```

### Phase 4: Dynamic Spawn

The supervisor polls its own mailbox every 3 seconds. On `request_help` messages:
- Checks spawn limits (max total agents, 1 spawn per requesting agent)
- Spawns a new AgentLoop with the helper task
- Strips `force_swarm` from context (prevents recursive swarm)
- Notifies the requesting agent with the new agent's ID

### Phase 5: Synthesis

Once all agents complete:
- **Single result**: Returns directly (no synthesis LLM call)
- **Multiple results**: Calls `SynthesizeResultsLLM` to merge findings
- Builds metadata (per-agent summaries, iterations, tokens, models)

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

The `force_swarm: true` context flag routes the task to SwarmWorkflow instead of the standard DAG/Research workflows.

## Agent Actions

Each iteration, the LLM returns exactly one action as JSON:

### tool_call
```json
{"action": "tool_call", "tool": "web_search", "tool_params": {"query": "NVIDIA H100 market share 2025"}}
```

### publish_data
Share findings with the team via workspace:
```json
{"action": "publish_data", "topic": "findings", "data": "US market dominated by NVIDIA with 80% share"}
```

### send_message
Direct P2P message to a specific teammate:
```json
{"action": "send_message", "to": "mitaka", "message_type": "info", "payload": {"message": "Can you check Japan domestic chip vendors?"}}
```

### request_help
Ask the supervisor to spawn a new agent:
```json
{"action": "request_help", "help_description": "Need help analyzing EU regulatory impact", "help_skills": ["web_search"]}
```

### done
Return final response (must be under 500 chars — save details to files first):
```json
{"action": "done", "response": "US leads with NVIDIA dominance. Japan focuses on edge AI. South Korea leverages Samsung foundry. Full report in takao-report.md"}
```

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

Agents share findings through topic-based workspace lists in Redis.

### Redis Keys

```
wf:{workflow_id}:ws:seq          # Global sequence counter
wf:{workflow_id}:ws:{topic}      # List of entries per topic
```

Each entry:
```json
{
  "seq": 5,
  "topic": "findings",
  "entry": {"author": "takao", "data": "Key insight about market trends"},
  "ts": 1707782400000000000
}
```

### How Agents See Workspace

Before each LLM call, the Go workflow fetches recent entries via `WorkspaceListAll`:
- Scans all `wf:{id}:ws:*` keys (auto-paginated)
- Returns last 5 entries per topic, sorted by global seq
- Capped at `workspace_max_entries` total
- Each entry truncated to `workspace_snippet_chars` (800 chars)

These appear in the agent's prompt as:
```
## Shared Findings
- takao: Key insight about market trends
- mitaka: Japan domestic vendors include Preferred Networks and...
```

## Convergence Detection

Three mechanisms prevent agents from looping indefinitely:

### 1. No-Progress Convergence

If an agent takes 3 consecutive non-tool actions (only messaging/publishing without doing real work), it's converged:

```go
if consecutiveNonToolActions >= 3 {
    // Build summary from last 3 iterations
    // Return as done with partial findings
}
```

Reset on any `tool_call` action.

### 2. Consecutive Error Abort

If 3 consecutive **permanent** tool errors occur (not transient), the agent aborts:

```go
if consecutiveToolErrors >= 3 {
    return AgentLoopResult{Success: false, Error: "consecutive tool errors"}
}
```

Reset on any successful tool execution.

### 3. Max Iterations Force-Done

On the last iteration, if the agent hasn't called `done`, the workflow forces it:

```go
if iteration == input.MaxIterations-1 && stepResult.Action != "done" {
    stepResult.Action = "done"
    stepResult.Response = summaryFromLast3Iterations
}
```

Two iterations before the limit, the prompt adds a warning:
```
⚠️ FINAL ITERATIONS: You MUST return done NOW with your best answer. Do not start new searches.
```

## Smart Retry

Transient errors get automatic retry with backoff; permanent errors count toward abort.

### Transient Error Detection

```go
func isTransientError(err error) bool {
    transientPatterns := []string{
        "rate limit", "429", "timeout", "timed out",
        "temporary", "unavailable", "503", "502",
    }
    // Case-insensitive pattern matching on error message
}
```

### Retry Behavior

| Error Type | Behavior | Backoff | Counts Toward Abort? |
|-----------|----------|---------|---------------------|
| Transient | Retry with backoff | 5s x attempt (max 30s) | No |
| Permanent | Record failure | None | Yes (3 strikes) |

Backoff uses `workflow.Sleep()` for Temporal determinism.

## Tiered History

Agent context grows with each iteration. Tiered truncation keeps it manageable:

| Iteration Age | Max Chars | Rationale |
|--------------|-----------|-----------|
| Last 3 turns | 4,000 | Full detail for recent work |
| Older turns | 500 | Summary only |

At 25 iterations: 3 x 4000 + 22 x 500 = 23K chars (~6K tokens).

### Token-Aware Trimming

If the full prompt exceeds 400K chars (~100K tokens), the oldest history entries are dropped until it fits, keeping a minimum of 3 recent entries:

```python
while len(trimmed_history) > 3 and (system_chars + len(user_prompt)) > max_prompt_chars:
    trimmed_history = trimmed_history[1:]
    # Rebuild history section, recalculate prompt length
```

## File-as-Memory Pattern

Agents are instructed to externalize large results to files:

1. **Tool results > 500 chars** → save to file, note filename
2. **Before calling done** → write full findings to `{agent-id}-report.md`
3. **Done response** → short summary only (under 500 chars)
4. **Before researching** → check teammates' files with `file_read`

This keeps agent context clean and gives all teammates access to full data through the shared session workspace.

## Configuration

### features.yaml

```yaml
workflows:
  swarm:
    enabled: true
    max_agents: 10                    # Max total agents (initial + dynamic)
    max_iterations_per_agent: 25      # Max reason-act loops per agent
    agent_timeout_seconds: 600        # Per-agent timeout (10 minutes)
    max_messages_per_agent: 20        # Max P2P messages per agent
    workspace_snippet_chars: 800      # Max chars per workspace entry in prompt
    workspace_max_entries: 5          # Max recent entries shown to agents
```

### Go Defaults (config.go)

If YAML values are missing or zero, these defaults apply:

| Parameter | Default | Notes |
|-----------|---------|-------|
| `SwarmMaxAgents` | 10 | Total cap including dynamic spawns |
| `SwarmMaxIterationsPerAgent` | 25 | Per-agent iteration limit |
| `SwarmAgentTimeoutSeconds` | 600 | 10 minutes per agent |
| `SwarmMaxMessagesPerAgent` | 20 | P2P message cap |
| `SwarmWorkspaceSnippetChars` | 800 | Truncation for prompt injection |
| `SwarmWorkspaceMaxEntries` | 5 | Recent entries per topic |

### Temporal Timeouts

| Activity | Timeout | Retries |
|----------|---------|---------|
| Agent LLM call (`/agent/loop`) | 90s | 2 |
| P2P activities (mailbox, workspace) | 10s | 1 |
| Event emission | 5s | 1 |
| Decomposition / Synthesis | 10 min | 3 |
| AgentLoop child workflow | `agent_timeout_seconds` | — |

## Streaming Events

SwarmWorkflow emits SSE events for real-time dashboards:

| Event Type | Agent ID | When |
|------------|----------|------|
| `workflow_started` | `swarm-supervisor` | Workflow begins |
| `progress` | `swarm-supervisor` | Planning, spawning, monitoring, synthesizing |
| `agent_started` | `{agent-name}` | Agent begins first iteration |
| `agent_completed` | `{agent-name}` | Agent finishes (done/converged/aborted) |
| `message_sent` | `{sender}` | P2P message sent |
| `message_received` | `{receiver}` | P2P message delivered |
| `workspace_updated` | `workspace` | New workspace entry published |
| `workflow_completed` | `swarm-supervisor` | Final synthesis complete |

## Agent Prompt Structure

Each `/agent/loop` call builds this prompt:

```
[System] AGENT_LOOP_SYSTEM_PROMPT
  - Available actions (tool_call, publish_data, send_message, request_help, done)
  - Memory management rules (file-as-memory)
  - Collaboration rules (check before duplicating, share via files)

[User]
  ## Task
  {agent's subtask description}

  ## Your Team (shared session workspace)
  - **takao (you)**: "Research US AI chip market"
  - mitaka: "Research Japan AI chip market"
  - kichijoji: "Compare findings and write report"

  ## Shared Findings
  - takao: NVIDIA dominates US with 80% market share...
  - mitaka: Japan focuses on edge AI chips...

  ## Previous Actions
  - Iteration 0: tool_call:web_search → {full 4000-char result}   ← recent
  - Iteration 1: tool_call:web_search → {full 4000-char result}   ← recent
  - Iteration 2: publish_data → {full 4000-char result}           ← recent
  - Iteration 3: tool_call:web_search → {500-char summary}        ← older

  ## Inbox Messages
  - From mitaka (info): {"message": "Check Samsung's foundry plans"}

  ## Budget: Iteration 4 of 25
  Decide your next action. Return ONLY valid JSON.

[Assistant prefill] {
```

The `{` prefill ensures the LLM returns valid JSON starting with `{`.

## Model Tier

All swarm agent LLM calls use **MEDIUM tier** (`ModelTier.MEDIUM`) with:
- Temperature: 0.3
- Max output tokens: 2,048

Final synthesis (if multiple agents) uses the standard synthesis path which forces **LARGE tier**.

## Key Source Files

| File | What It Does |
|------|-------------|
| `go/orchestrator/internal/workflows/swarm_workflow.go` | SwarmWorkflow + AgentLoop Temporal workflows |
| `go/orchestrator/internal/activities/p2p.go` | SendAgentMessage, FetchAgentMessages, WorkspaceAppend, WorkspaceList, WorkspaceListAll |
| `go/orchestrator/internal/activities/config.go` | SwarmConfig struct and defaults |
| `python/llm-service/llm_service/api/agent.py` | `/agent/loop` endpoint, system prompt, tiered history, context trimming |
| `config/features.yaml` | Swarm configuration section |
| `go/orchestrator/internal/agents/names.go` | Agent name generation (station names) |
