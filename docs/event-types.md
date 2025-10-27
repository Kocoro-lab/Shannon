# Shannon Event Types Reference

This document provides a comprehensive reference for all event types emitted by Shannon workflows.

## Overview

Shannon emits events at various stages of workflow execution to provide real-time visibility into task processing, agent coordination, LLM interactions, and tool execution. Events follow a minimal, deterministic model designed for streaming via SSE, WebSocket, or gRPC.

## Event Structure

All events share a common structure:

```json
{
  "workflow_id": "task-00000000-0000-0000-0000-000000000002-1761545271",
  "type": "AGENT_STARTED",
  "agent_id": "simple-agent",
  "message": "Processing query",
  "timestamp": "2025-10-27T06:07:51.49277Z",
  "seq": 7,
  "stream_id": "1761545276767-0"
}
```

**Fields:**
- `workflow_id` (string, required) - Unique workflow identifier
- `type` (string, required) - Event type (see categories below)
- `agent_id` (string, optional) - Agent that emitted the event
- `message` (string, optional) - Human-readable description
- `timestamp` (RFC3339, required) - Event timestamp
- `seq` (integer, required) - Monotonic sequence number for ordering
- `stream_id` (string, optional) - Stream identifier for event grouping

## Event Categories

### Core Workflow Events

Events that mark major workflow lifecycle transitions.

#### `WORKFLOW_STARTED`
**Description:** Task processing started by the orchestrator
**Agent ID:** `orchestrator`
**Example Message:** `"Task processing started"`
**Typical Sequence:** First event in workflow

```json
{
  "type": "WORKFLOW_STARTED",
  "agent_id": "orchestrator",
  "message": "Task processing started",
  "seq": 1
}
```

#### `WORKFLOW_COMPLETED`
**Description:** Workflow finished successfully
**Agent ID:** Agent that completed the task
**Example Message:** `"All done"`
**Typical Sequence:** Final event in successful workflows

```json
{
  "type": "WORKFLOW_COMPLETED",
  "agent_id": "simple-agent",
  "message": "All done",
  "seq": 12
}
```

#### `AGENT_STARTED`
**Description:** Agent began processing a task
**Agent ID:** Agent identifier
**Example Message:** `"Processing query"`, `"Starting simple task workflow"`

```json
{
  "type": "AGENT_STARTED",
  "agent_id": "simple-agent",
  "message": "Processing query",
  "seq": 7
}
```

#### `AGENT_COMPLETED`
**Description:** Agent finished processing
**Agent ID:** Agent identifier
**Example Message:** `"Task done"`

```json
{
  "type": "AGENT_COMPLETED",
  "agent_id": "simple-agent",
  "message": "Task done",
  "seq": 11
}
```

#### `ERROR_OCCURRED`
**Description:** An error occurred during workflow execution
**Agent ID:** Agent where error occurred (optional)
**Example Message:** Error description
**Note:** May include error details in message field

```json
{
  "type": "ERROR_OCCURRED",
  "agent_id": "data-agent",
  "message": "Failed to connect to database: timeout",
  "seq": 8
}
```

#### `AGENT_THINKING`
**Description:** Agent is in planning/reasoning phase
**Agent ID:** Agent identifier
**Example Message:** `"Thinking: What is 5 + 5?"`
**Note:** Provides visibility into agent decision-making

```json
{
  "type": "AGENT_THINKING",
  "agent_id": "simple-agent",
  "message": "Thinking: What is 5 + 5?",
  "seq": 6
}
```

---

### LLM Events

Events related to Large Language Model interactions.

#### `LLM_PROMPT`
**Description:** Sanitized prompt sent to LLM
**Agent ID:** Agent making the LLM call
**Example Message:** The actual prompt text
**Privacy:** May be sanitized for PII/sensitive data

```json
{
  "type": "LLM_PROMPT",
  "agent_id": "simple-agent",
  "message": "What is 5 + 5?",
  "seq": 8
}
```

#### `LLM_PARTIAL`
**Description:** Incremental LLM output chunk during streaming
**Agent ID:** Agent receiving the stream
**Example Message:** Partial text chunk
**Note:** Often filtered out in UI for cleaner chat history
**Filtering:** Excluded by `/api/v1/sessions/{sessionId}/events` endpoint

```json
{
  "type": "LLM_PARTIAL",
  "agent_id": "simple-agent",
  "message": "5 + 5",
  "seq": 9
}
```

#### `LLM_OUTPUT`
**Description:** Final complete LLM response
**Agent ID:** Agent that made the LLM call
**Example Message:** Complete response text
**Note:** This is the final answer, not streaming chunks

```json
{
  "type": "LLM_OUTPUT",
  "agent_id": "simple-agent",
  "message": "5 + 5 equals 10.",
  "seq": 10
}
```

#### `TOOL_OBSERVATION`
**Description:** Result/output from tool execution
**Agent ID:** Agent executing the tool
**Example Message:** Tool output or observation

```json
{
  "type": "TOOL_OBSERVATION",
  "agent_id": "data-agent",
  "message": "Database query returned 42 rows",
  "seq": 15
}
```

---

### Multi-Agent Coordination Events

Events for multi-agent workflows and team collaboration.

#### `DELEGATION`
**Description:** Task delegated to another agent or workflow
**Agent ID:** Delegating agent (optional)
**Example Message:** `"Handing off to simple task"`, delegation reason

```json
{
  "type": "DELEGATION",
  "message": "Handing off to simple task",
  "seq": 4
}
```

#### `MESSAGE_SENT`
**Description:** Agent sent a message to another agent
**Agent ID:** Sending agent
**Example Message:** Message content
**Feature Gate:** Requires `p2p_v1` enabled

```json
{
  "type": "MESSAGE_SENT",
  "agent_id": "coordinator",
  "message": "Please analyze section 3",
  "seq": 12
}
```

#### `MESSAGE_RECEIVED`
**Description:** Agent received a message
**Agent ID:** Receiving agent
**Example Message:** Message content
**Feature Gate:** Requires `p2p_v1` enabled

```json
{
  "type": "MESSAGE_RECEIVED",
  "agent_id": "analyzer",
  "message": "Received task: analyze section 3",
  "seq": 13
}
```

#### `TEAM_RECRUITED`
**Description:** New agent recruited to the team
**Agent ID:** Role of recruited agent
**Example Message:** Description/reason for recruitment
**Feature Gate:** Requires `dynamic_team_v1` enabled

```json
{
  "type": "TEAM_RECRUITED",
  "agent_id": "writer",
  "message": "Summarize section 3",
  "seq": 8
}
```

#### `TEAM_RETIRED`
**Description:** Agent retired from the team
**Agent ID:** Retired agent identifier
**Example Message:** Retirement reason
**Feature Gate:** Requires `dynamic_team_v1` enabled

```json
{
  "type": "TEAM_RETIRED",
  "agent_id": "agent-xyz",
  "message": "Task completed",
  "seq": 20
}
```

#### `ROLE_ASSIGNED`
**Description:** Role assigned to an agent
**Agent ID:** Agent receiving the role
**Example Message:** Role details

```json
{
  "type": "ROLE_ASSIGNED",
  "agent_id": "agent-123",
  "message": "Assigned role: data_analyst",
  "seq": 5
}
```

---

### Progress & Status Events

Events that provide status updates and progress information.

#### `PROGRESS`
**Description:** Step completion or progress update
**Agent ID:** Agent reporting progress
**Example Message:** `"Created a plan with 1 steps"`, `"Completed step 2 of 5"`
**Use Case:** Progress bars, status indicators

```json
{
  "type": "PROGRESS",
  "agent_id": "planner",
  "message": "Created a plan with 1 steps",
  "seq": 3
}
```

#### `DATA_PROCESSING`
**Description:** Processing, analyzing, or preparing data
**Agent ID:** Processing agent (optional)
**Example Message:** `"Preparing context"`, `"Analyzing results"`, `"Final answer ready"`
**Note:** Used for various processing stages

```json
{
  "type": "DATA_PROCESSING",
  "message": "Preparing context",
  "seq": 1
}
```

#### `TEAM_STATUS`
**Description:** Multi-agent coordination status update
**Agent ID:** Coordinator or team lead
**Example Message:** Team status description
**Use Case:** Multi-agent workflow visibility

```json
{
  "type": "TEAM_STATUS",
  "agent_id": "supervisor",
  "message": "3 agents active, 2 completed",
  "seq": 14
}
```

#### `WAITING`
**Description:** Waiting for resources, responses, or approvals
**Agent ID:** Waiting agent
**Example Message:** What the agent is waiting for

```json
{
  "type": "WAITING",
  "agent_id": "integration-agent",
  "message": "Waiting for API response",
  "seq": 9
}
```

#### `WORKSPACE_UPDATED`
**Description:** Workspace or context was modified
**Agent ID:** Agent that updated workspace
**Example Message:** Update description
**Feature Gate:** Requires `p2p_v1` enabled

```json
{
  "type": "WORKSPACE_UPDATED",
  "agent_id": "editor",
  "message": "Updated shared document",
  "seq": 16
}
```

---

### Tool Execution Events

Events related to tool and function invocation.

#### `TOOL_INVOKED`
**Description:** Tool/function execution started
**Agent ID:** Agent invoking the tool
**Example Message:** Tool name and parameters
**Note:** May include sanitized parameters

```json
{
  "type": "TOOL_INVOKED",
  "agent_id": "data-agent",
  "message": "Calling database_query with table=users",
  "seq": 10
}
```

---

### Human Interaction Events

Events for human-in-the-loop workflows.

#### `APPROVAL_REQUESTED`
**Description:** Workflow requests human approval
**Agent ID:** Agent requesting approval
**Example Message:** Approval request details
**Use Case:** Human-in-the-loop workflows

```json
{
  "type": "APPROVAL_REQUESTED",
  "agent_id": "approval-agent",
  "message": "Approve database modification: DELETE 100 records?",
  "seq": 12
}
```

#### `APPROVAL_DECISION`
**Description:** Human approval decision received
**Agent ID:** Agent that requested approval
**Example Message:** Decision (approved/rejected) and optional comment

```json
{
  "type": "APPROVAL_DECISION",
  "agent_id": "approval-agent",
  "message": "Approved by user@example.com",
  "seq": 13
}
```

---

### Advanced Features

#### `DEPENDENCY_SATISFIED`
**Description:** Workflow dependency was satisfied
**Agent ID:** Agent or workflow identifier
**Example Message:** Dependency description

```json
{
  "type": "DEPENDENCY_SATISFIED",
  "agent_id": "workflow-2",
  "message": "Data preprocessing completed",
  "seq": 6
}
```

#### `ERROR_RECOVERY`
**Description:** System recovering from an error
**Agent ID:** Agent performing recovery
**Example Message:** Recovery action description

```json
{
  "type": "ERROR_RECOVERY",
  "agent_id": "resilient-agent",
  "message": "Retrying with exponential backoff (attempt 2/3)",
  "seq": 11
}
```

---

## Event Filtering

### By Type

Filter events by type using the `types` parameter:

**SSE:**
```bash
curl -N "http://localhost:8081/stream/sse?workflow_id=task-123&types=AGENT_STARTED,AGENT_COMPLETED"
```

**gRPC:**
```go
request := &pb.StreamRequest{
    WorkflowId: "task-123",
    Types:      []string{"LLM_OUTPUT", "ERROR_OCCURRED"},
}
```

### Common Filtering Patterns

**Chat UI (exclude streaming chunks):**
```
types=WORKFLOW_STARTED,AGENT_THINKING,LLM_PROMPT,LLM_OUTPUT,AGENT_COMPLETED,ERROR_OCCURRED
```

**Progress Tracking:**
```
types=WORKFLOW_STARTED,PROGRESS,AGENT_COMPLETED,WORKFLOW_COMPLETED
```

**Team Coordination:**
```
types=TEAM_RECRUITED,TEAM_RETIRED,MESSAGE_SENT,MESSAGE_RECEIVED
```

**Error Monitoring:**
```
types=ERROR_OCCURRED,ERROR_RECOVERY
```

### Session Events API

The session events endpoint (`GET /api/v1/sessions/{sessionId}/events`) automatically excludes `LLM_PARTIAL` events for cleaner chat history:

```bash
curl "http://localhost:8080/api/v1/sessions/{sessionId}/events?limit=200" \
  -H "X-API-Key: your-key"
```

**Why LLM_PARTIAL is excluded:**
- Reduces noise in conversation history
- Prevents duplicate content (partial + final)
- Improves UI performance with fewer events
- Final output is always available via `LLM_OUTPUT`

---

## Event Ordering

Events are guaranteed to be:
1. **Monotonically increasing** - `seq` numbers always increase
2. **Deterministic** - Same workflow replay produces same events
3. **Ordered by timestamp** - Earlier events have earlier timestamps

### Typical Event Flow

Simple query workflow:
```
1. WORKFLOW_STARTED (orchestrator)
2. DATA_PROCESSING (preparing context)
3. PROGRESS (planner creates plan)
4. DELEGATION (hand off to agent)
5. AGENT_STARTED (simple-agent)
6. AGENT_THINKING (reasoning)
7. LLM_PROMPT (query sent)
8. LLM_PARTIAL (streaming...) [often filtered]
9. LLM_OUTPUT (final answer)
10. AGENT_COMPLETED (simple-agent)
11. WORKFLOW_COMPLETED (simple-agent)
```

Complex multi-agent workflow:
```
1. WORKFLOW_STARTED
2. TEAM_RECRUITED (analyzer)
3. TEAM_RECRUITED (writer)
4. MESSAGE_SENT (coordinator → analyzer)
5. MESSAGE_RECEIVED (analyzer)
6. AGENT_STARTED (analyzer)
7. TOOL_INVOKED (database_query)
8. TOOL_OBSERVATION (results)
9. LLM_OUTPUT (analysis)
10. MESSAGE_SENT (analyzer → writer)
11. AGENT_STARTED (writer)
12. LLM_OUTPUT (final document)
13. WORKFLOW_COMPLETED
```

---

## Implementation Notes

### Event Emission

Events are emitted through Temporal activities to ensure determinism:

```go
EmitTaskUpdate(ctx, EmitTaskUpdateInput{
    WorkflowID: workflowID,
    EventType:  StreamEventAgentStarted,
    AgentID:    "simple-agent",
    Message:    "Processing query",
    Timestamp:  time.Now(),
})
```

### Version Gates

Some event types require feature gates to be enabled:

| Event Type | Feature Gate Required |
|------------|----------------------|
| `MESSAGE_SENT`, `MESSAGE_RECEIVED`, `WORKSPACE_UPDATED` | `p2p_v1` |
| `TEAM_RECRUITED`, `TEAM_RETIRED` | `dynamic_team_v1` |

### Storage & Retention

- Events are stored in `event_logs` table in PostgreSQL
- Real-time streaming uses in-memory ring buffer (default: 256 events)
- Historical events are queryable via REST API
- Ring buffer size configurable via `STREAMING_RING_CAPACITY`

---

## API Endpoints

### Stream Real-Time Events

**SSE:**
```bash
GET /stream/sse?workflow_id={id}&types={csv}&last_event_id={n}
```

**WebSocket:**
```bash
GET /stream/ws?workflow_id={id}&types={csv}&last_event_id={n}
```

**gRPC:**
```protobuf
rpc StreamTaskExecution(StreamRequest) returns (stream TaskUpdate);
```

### Query Historical Events

**Per-Task Events:**
```bash
GET /api/v1/tasks/{id}/events?limit=50&offset=0
```

**Session Events (excludes LLM_PARTIAL):**
```bash
GET /api/v1/sessions/{sessionId}/events?limit=200&offset=0
```

---

## Best Practices

### For Frontend Developers

1. **Filter appropriately** - Don't subscribe to all events if you only need a subset
2. **Handle reconnections** - Use `last_event_id` to resume from disconnects
3. **Exclude LLM_PARTIAL for chat UI** - Use session events API or filter manually
4. **Show progress events** - Use `PROGRESS` and `DATA_PROCESSING` for status updates
5. **Group by workflow_id** - Multiple workflows may emit events concurrently

### For Backend Developers

1. **Always emit through activities** - Ensures determinism and Temporal replay safety
2. **Include meaningful messages** - Help frontend display useful information
3. **Use appropriate event types** - Don't overload generic types
4. **Consider feature gates** - Check if advanced event types are enabled
5. **Sanitize sensitive data** - Especially in `LLM_PROMPT` events

### For Operations

1. **Monitor ERROR_OCCURRED events** - Set up alerts
2. **Track ERROR_RECOVERY patterns** - Identify reliability issues
3. **Adjust ring buffer size** - Based on workflow duration and event volume
4. **Use event filtering** - Reduce storage and bandwidth for analytics

---

## Related Documentation

- **Streaming API**: `/docs/streaming-api.md` - SSE, WebSocket, gRPC protocols
- **Session API**: OpenAPI spec at `/openapi.json` - REST endpoints
- **Event Types Source**: `go/orchestrator/internal/activities/stream_events.go`
- **Frontend Types**: `observability/dashboard/shannon/types.ts`

---

## Quick Reference

| Category | Event Types | Count |
|----------|-------------|-------|
| **Core Workflow** | WORKFLOW_STARTED, WORKFLOW_COMPLETED, AGENT_STARTED, AGENT_COMPLETED, ERROR_OCCURRED, AGENT_THINKING | 6 |
| **LLM Events** | LLM_PROMPT, LLM_PARTIAL, LLM_OUTPUT, TOOL_OBSERVATION | 4 |
| **Multi-Agent** | DELEGATION, MESSAGE_SENT, MESSAGE_RECEIVED, TEAM_RECRUITED, TEAM_RETIRED, ROLE_ASSIGNED | 6 |
| **Progress/Status** | PROGRESS, DATA_PROCESSING, TEAM_STATUS, WAITING, WORKSPACE_UPDATED | 5 |
| **Tools** | TOOL_INVOKED | 1 |
| **Human Interaction** | APPROVAL_REQUESTED, APPROVAL_DECISION | 2 |
| **Advanced** | DEPENDENCY_SATISFIED, ERROR_RECOVERY | 2 |
| **Total** | | **26** |

---

*Last Updated: 2025-10-27*
*Shannon Version: 0.1.0*
