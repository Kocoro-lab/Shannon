# Shannon Streaming APIs

This document describes the minimal, deterministic streaming interfaces exposed by the orchestrator. It covers gRPC, Server‑Sent Events (SSE), and WebSocket (WS) endpoints, including filters and resume semantics for rejoining sessions.

## Event Persistence Strategy

Shannon uses a **two-tier persistence model** optimized for performance:

### Redis (All Events)
- **Purpose**: Real-time SSE/WebSocket delivery
- **Retention**: Last 256 events per workflow, 24-hour TTL
- **Events**: ALL events including `LLM_PARTIAL` (streaming tokens)
- **Storage**: ~30-50KB per 500-token response

### PostgreSQL (Important Events Only)
- **Purpose**: Historical audit trail and replay
- **Retention**: Permanent (or per retention policy)
- **Events**: Only critical events persisted:
  - ✅ `WORKFLOW_COMPLETED`, `WORKFLOW_FAILED`
  - ✅ `AGENT_COMPLETED`, `AGENT_FAILED`
  - ✅ `TOOL_INVOKED`, `TOOL_OBSERVATION`, `TOOL_ERROR`
  - ✅ `ERROR_OCCURRED`, `LLM_OUTPUT`, `STREAM_END`
  - ❌ `LLM_PARTIAL` (thread.message.delta) - **NOT persisted**
  - ❌ `HEARTBEAT`, `PING` - **NOT persisted**
- **Reduction**: ~95% fewer DB writes (500 deltas → 5-10 important events)

**Rationale**: Streaming deltas are ephemeral and only needed for real-time delivery. Redis provides sufficient retention (24h) for debugging, while PostgreSQL stores the permanent audit trail.

---

## Event Model

- Fields: `workflow_id`, `type`, `agent_id?`, `message?`, `timestamp`, `seq`.
- Minimal event types (behind `streaming_v1` gate):
  - `WORKFLOW_STARTED`, `AGENT_STARTED`, `AGENT_COMPLETED`, `ERROR_OCCURRED`.
  - P2P v1 adds: `MESSAGE_SENT`, `MESSAGE_RECEIVED`, `WORKSPACE_UPDATED`.
- Determinism: events are emitted from workflows as activities, recorded in Temporal history, and published to a local stream manager.

### Enhanced Event Types

**LLM Response Events:**
- `LLM_PARTIAL`: Emitted during agent execution with streaming text deltas (token‑by‑token or in small chunks).
  - Streaming text: `message` contains the text delta.
  - SSE mapping: sent as `event: thread.message.delta` with data:
    ```json
    {
      "delta": "partial text...",
      "workflow_id": "task-...",
      "agent_id": "simple-agent",
      "seq": 9,
      "stream_id": "1700000000000-0"
    }
    ```
- `LLM_OUTPUT`: Emitted when an LLM call finishes with the final response and usage metadata.
  - Final text: `message` contains the final response text (truncated for safety).
  - Usage metadata: JSON structure in the `payload` field when available:
    ```json
    {
      "tokens_used": 174,
      "input_tokens": 104,
      "output_tokens": 70,
      "cost_usd": 0.0012,
      "model_used": "gpt-5-nano-2025-08-07",
      "provider": "openai"
    }
    ```
  - SSE mapping: sent as `event: thread.message.completed` with data:
    ```json
    {
      "response": "5 + 5 equals 10.",
      "workflow_id": "task-...",
      "agent_id": "simple-agent",
      "seq": 10,
      "stream_id": "1700000000001-0",
      "metadata": {
        "tokens_used": 174,
        "input_tokens": 104,
        "output_tokens": 70,
        "cost_usd": 0.0012,
        "model_used": "gpt-5-nano-2025-08-07",
        "provider": "openai"
      }
    }
    ```

**Tool Execution Events:**
- `TOOL_INVOKED`: Emitted when agent-core executes a tool (web_search, calculator, file_read, etc.)
  - `message` is a human‑readable description (for example, `"Calling web_search with query: 'latest news'"`)
  - `payload` contains structured data:
    ```json
    {
      "tool": "web_search",
      "params": {
        "query": "latest news"
      }
    }
    ```
- `TOOL_OBSERVATION`: Emitted when tool execution completes with results
  - `message` contains truncated tool output or a short error description (UTF‑8 safe, up to ~2000 chars)
  - `payload` includes metadata such as:
    ```json
    {
      "tool": "web_search",
      "success": true,
      "duration_ms": 1234
    }
    ```

**Note**: Python-only tools (vendor adapters, custom integrations) use internal function calling and do not emit `TOOL_INVOKED`/`TOOL_OBSERVATION` events by design. Results are embedded in LLM response text.

## gRPC: StreamingService

- RPC: `StreamingService.StreamTaskExecution(StreamRequest) returns (stream TaskUpdate)`
- Request fields:
  - `workflow_id` (required)
  - `types[]` (optional) — filter by event types
  - `last_event_id` (optional) — resume point; accepts either a Redis `stream_id` (preferred) or a numeric `seq`. When a numeric value is used, replay includes events where `seq > last_event_id`.
- Response: `TaskUpdate` mirrors the event model.

Example (pseudo‑Go):

```go
client := pb.NewStreamingServiceClient(conn)
stream, _ := client.StreamTaskExecution(ctx, &pb.StreamRequest{
    WorkflowId: wfID,
    Types:      []string{"AGENT_STARTED", "AGENT_COMPLETED"},
    LastEventId: 42,
})
for {
    upd, err := stream.Recv()
    if err != nil { break }
    fmt.Println(upd.Type, upd.AgentId, upd.Seq)
}
```

## SSE: HTTP `/stream/sse`

- Method: `GET /stream/sse?workflow_id=<id>&types=<csv>&last_event_id=<id-or-seq>`
- Headers: supports `Last-Event-ID` for browser auto‑resume.
- CORS: `Access-Control-Allow-Origin: *` (dev‑friendly; front door should enforce auth in prod).

Example (curl):

```bash
# Watch agent lifecycle events
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=AGENT_STARTED,AGENT_COMPLETED"

# Watch LLM output and usage metadata
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=LLM_OUTPUT"

# Watch tool execution
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=TOOL_INVOKED,TOOL_OBSERVATION"

# Watch everything
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF"
```

Notes:
- Server emits `id` as the Redis `stream_id` when available (preferred) or falls back to numeric `seq`. You can reconnect using the `Last-Event-ID` header or `last_event_id` query param with either form.
- Heartbeats are sent as SSE comments every ~10s to keep intermediaries alive.
- `LLM_PARTIAL` events are mapped to `thread.message.delta` SSE events with a `delta` field for streaming text.
- `LLM_OUTPUT` events contain the final response text in `message` and usage metadata in `payload`; the SSE handler maps these to `thread.message.completed` events with `response` (text) and optional `metadata`.

## WebSocket: HTTP `/stream/ws`

- Method: `GET /stream/ws?workflow_id=<id>&types=<csv>&last_event_id=<id-or-seq>`
- Messages: JSON objects matching the event model.
- Heartbeats: server pings every ~20s; client should reply with pong.

Example (JS):

```js
const ws = new WebSocket(`ws://localhost:8081/stream/ws?workflow_id=${wf}`);
ws.onmessage = (e) => {
  const evt = JSON.parse(e.data); // {workflow_id,type,agent_id,message,timestamp,seq}
};
```

## Invalid Workflow Detection

Both gRPC and SSE streaming endpoints automatically validate workflow existence to fail fast for invalid workflow IDs:

### Behavior

- **Validation Timeout**: 30 seconds from connection start
- **Validation Method**: Uses Temporal `DescribeWorkflowExecution` API
- **First Event Timer**: Fires after 30s if no events are received

### Response by Transport

**gRPC (`StreamingService.StreamTaskExecution`)**
- Returns `NotFound` gRPC error code
- Error message: `"workflow not found"` or `"workflow not found or unavailable"`

**SSE (`/stream/sse`)**
- Emits `ERROR_OCCURRED` event before closing:
  ```
  event: ERROR_OCCURRED
  data: {"workflow_id":"xxx","type":"ERROR_OCCURRED","message":"Workflow not found"}
  ```
- Includes heartbeat pings (`: ping`) every 10s while waiting

**WebSocket (`/stream/ws`)**
- Same behavior as SSE, sends JSON error event then closes connection

### Valid Workflow Edge Cases

- **Workflow exists but produces no events within 30s**: Stream stays open, timer resets
- **Temporal unavailable during validation**: Returns error immediately
- **Valid workflows**: Timer is disabled after first event arrives

### Example Usage

```bash
# Invalid workflow - returns error after ~30s
shannon stream "invalid-workflow-123"
# Output after 30s:
# ERROR_OCCURRED: Workflow not found

# Valid workflow - streams normally
shannon stream "task-user-1234567890"
# Output: immediate streaming of events
```

### Notes

- This prevents indefinite hanging when streaming non-existent workflows
- The 30s timeout balances responsiveness with allowing slow workflow startup
- Heartbeats keep connections alive through proxies during validation period

### Gateway Behavior

- The HTTP gateway forwards streaming filters to the orchestrator and accepts any event types. Unknown types simply yield no events.
- `last_event_id` accepts both numeric sequences and Redis stream IDs (e.g., `1700000000000-0`).

## Dynamic Teams (Signals) + Team Events

When `dynamic_team_v1` is enabled in `SupervisorWorkflow`, the workflow accepts signals:

- Recruit: signal name `recruit_v1` with JSON `{ "Description": string, "Role"?: string }`.
- Retire:  signal name `retire_v1` with JSON `{ "AgentID": string }`.

Authorized actions emit streaming events:

- `TEAM_RECRUITED` with `agent_id` as the role (for minimal v1) and `message` as the description.
- `TEAM_RETIRED` with `agent_id` as the retired agent.

Helper script to send signals via Temporal CLI inside docker compose:

```bash
# Recruit a new worker for a subtask
./scripts/signal_team.sh recruit <WORKFLOW_ID> "Summarize section 3" writer

# Retire a worker
./scripts/signal_team.sh retire <WORKFLOW_ID> agent-xyz
```

Tip: Use SSE/WS filters to only watch team events:

```bash
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=TEAM_RECRUITED,TEAM_RETIRED"
```

## Quick Start

### Development Testing
```bash
# Start Shannon services
make dev

# Test streaming for a specific workflow 
make smoke-stream WF_ID=<workflow_id>

# Optional: custom endpoints
make smoke-stream WF_ID=workflow-123 ADMIN=http://localhost:8081 GRPC=localhost:50052
```

<!-- Browser demo section removed: file no longer included -->

## Capacity & Replay Behaviour

- Live streaming uses Redis Streams with a bounded length (approximate maxlen ~256) per workflow for deterministic replay.
- Capacity can be adjusted via environment variable or code:
  - `STREAMING_RING_CAPACITY` (integer) — orchestrator reads this and calls `streaming.Configure(n)` at startup
  - Programmatic: call `streaming.Configure(n)` during initialization

## Operational Notes

- Replay safety: event emission is version‑gated and routed through activities, preserving Temporal determinism.
- Backpressure: drops events to slow subscribers (non‑blocking channels); clients should reconnect with `last_event_id` as needed.
- Security: front the admin HTTP port with an authenticated proxy in production; gRPC should require TLS when exposed externally.

### Anti‑patterns and Load Considerations
- Avoid unbounded per‑client buffers. The in‑process manager uses bounded channels and a fixed ring to prevent memory growth.
- Do not rely on every event being delivered to slow clients. Instead, reconnect with `last_event_id` to catch up deterministically.
- Prefer SSE for simple dashboards and logs; use WebSocket only when you need bi‑directional control messages.
- For high fan‑out, place an external event gateway (e.g., NGINX or a thin Go fan‑out) in front; the in‑process manager is not a message broker.

## Architecture

### Event Flow
```
Workflow → EmitTaskUpdate (Activity) → Stream Manager → Ring Buffer + Live Subscribers
                                                           ↓
                        SSE ← HTTP Gateway ← Event Distribution → gRPC Stream  
                         ↓                                       ↓
                    WebSocket ←────────────────────────────── Client SDKs
```

### Key Components
- **Stream Manager**: In-memory pub/sub with per-workflow ring buffers
- **Ring Buffer**: Configurable capacity (default: 256 events) for replay support
- **Multiple Protocols**: gRPC (enterprise), SSE (browser-native), WebSocket (interactive)
- **Deterministic Events**: All events routed through Temporal activities for replay safety

### Service Ports
- **Admin HTTP**: 8081 (SSE `/stream/sse`, WebSocket `/stream/ws`, health, approvals)
- **gRPC**: 50052 (StreamingService, OrchestratorService, SessionService)

## Integration Examples

### Python SDK (Pseudo-code)
```python
import grpc
from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc

# gRPC Streaming
channel = grpc.insecure_channel('localhost:50052')
client = orchestrator_pb2_grpc.StreamingServiceStub(channel)
request = orchestrator_pb2.StreamRequest(
    workflow_id='workflow-123',
    types=['AGENT_STARTED', 'AGENT_COMPLETED'],
    last_event_id=0
)

for update in client.StreamTaskExecution(request):
    print(f"Agent {update.agent_id}: {update.type} (seq: {update.seq})")
```

### React Component
```jsx
import React, { useEffect, useState } from 'react';

function WorkflowStream({ workflowId }) {
  const [events, setEvents] = useState([]);
  const [llmOutput, setLlmOutput] = useState('');
  const [usage, setUsage] = useState(null);

  useEffect(() => {
    const eventSource = new EventSource(
      `/stream/sse?workflow_id=${workflowId}&types=LLM_OUTPUT,AGENT_COMPLETED,TOOL_OBSERVATION`
    );

    eventSource.onmessage = (e) => {
      const event = JSON.parse(e.data);

      if (event.type === 'LLM_OUTPUT') {
        // Check if message is usage metadata (JSON) or text chunk (string)
        try {
          const parsed = JSON.parse(event.message);
          if (parsed.usage) {
            // Usage metadata
            setUsage(parsed);
          }
        } catch {
          // Text chunk
          setLlmOutput(prev => prev + event.message);
        }
      }

      setEvents(prev => [...prev, event]);
    };

    return () => eventSource.close();
  }, [workflowId]);

  return (
    <div>
      <div className="llm-output">
        <pre>{llmOutput}</pre>
        {usage && (
          <div className="usage-stats">
            Model: {usage.model} ({usage.provider})<br/>
            Tokens: {usage.usage.total_tokens}
            (in: {usage.usage.input_tokens}, out: {usage.usage.output_tokens})
          </div>
        )}
      </div>

      <div className="events">
        {events.map(event => (
          <div key={event.seq}>
            {event.type}: {event.agent_id || event.message?.substring(0, 50)}
          </div>
        ))}
      </div>
    </div>
  );
}
```

## Troubleshooting

### Common Issues

**"No events received"**
- Verify workflow_id exists and is running
- Check that `streaming_v1` version gate is enabled in the workflow
- Ensure admin HTTP port (8081) is accessible
- If using `types`, make sure the workflow actually emits those event types

**"Events missing after reconnect"**
- Use `last_event_id` parameter or `Last-Event-ID` header
- Replay reads from the bounded Redis Stream; very old events may be pruned once the stream (~256 items) evicts them

**Python async: RuntimeError when awaiting inside async-for**
- Do not `await` other client calls inside the `async for` over the stream. Break out first, then `await`:

```python
async with AsyncShannonClient() as client:
    h = await client.submit_task("Complex analysis")
    async for e in client.stream(h.workflow_id, types=["LLM_OUTPUT","WORKFLOW_COMPLETED"]):
        if e.type == "WORKFLOW_COMPLETED":
            break
    final = await client.wait(h.task_id)
    print(final.result)
```

**"High memory usage"**
- Reduce ring buffer capacity in config
- Implement client-side filtering to reduce event volume
- Use connection pooling for multiple concurrent streams

### Debug Commands
```bash
# Check streaming endpoints
curl -s http://localhost:8081/health
curl -N "http://localhost:8081/stream/sse?workflow_id=test" | head -10

# Test gRPC connectivity
grpcurl -plaintext localhost:50052 list shannon.orchestrator.StreamingService

# Monitor streaming logs
docker compose logs orchestrator | grep "stream"
```

## Provider Compatibility

### Usage Metadata Streaming Support

Shannon's LLM providers emit usage metadata in `LLM_OUTPUT` streaming events with varying levels of support:

| Provider | Streaming | Usage Metadata | Implementation |
|----------|-----------|----------------|----------------|
| **OpenAI** | ✅ | ✅ | Uses `stream_options: {"include_usage": true}` |
| **Anthropic** | ✅ | ✅ | Uses `await stream.get_final_message()` after streaming |
| **Groq** | ✅ | ✅ | Usage available in final streaming chunk |
| **Google** | ✅ | ✅ | Usage available via `usage_metadata` in final chunk |
| **xAI** | ✅ | ⚠️ API Limitation | REST API does not emit usage in streaming mode |
| **OpenAI-compatible** | ✅ | ⚠️ Varies | Depends on endpoint; some don't support `stream_options` |

### Known Limitations

**xAI Streaming**
- xAI's REST API (OpenAI-compatible endpoint at `api.x.ai/v1`) does **not** emit usage metadata during streaming
- Usage is only available in non-streaming responses
- This is a limitation of xAI's API, not Shannon's implementation
- Alternative: Use non-streaming mode for xAI if usage metadata is required
- Native xAI gRPC SDK (`xai-sdk` package) has different behavior but would require complete provider rewrite

**OpenAI GPT-5 Models**
- GPT-5-nano and GPT-5-mini models do not stream text deltas (OpenAI API bug)
- Only usage metadata is streamed; no content chunks
- This affects models: `gpt-5-nano-2025-08-07`, `gpt-5-mini-2025-08-07`
- Workaround: Use GPT-4o models for streaming text + usage

**OpenAI-Compatible Endpoints**
- Some OpenAI-compatible endpoints (DeepSeek, Qwen, local models) may not support the `stream_options` parameter
- Shannon gracefully handles this - streaming will work without usage metadata
- Usage metadata will still be available in final task completion response

**Python-Only Tools**
- Vendor-specific tools (GA4, custom adapters) execute via internal function calling
- Do not emit `TOOL_INVOKED`/`TOOL_OBSERVATION` events (by architectural design)
- Results are embedded in LLM response text
- This avoids complex cross-language parameter mapping between Python ↔ Go ↔ Rust

### Implementation Details

**Fallback Streaming**
- When primary provider fails, Shannon automatically falls back to alternate providers
- Usage metadata dict chunks are now preserved through the fallback path
- Fix applied: `manager.py:737-738` accepts `(str, dict)` instead of just `str`

**UTF-8 Safety**
- Tool observation messages truncated to 2000 characters using UTF-8 safe truncation
- Uses rune-based truncation (Go's `truncateQuery()`) to prevent splitting multi-byte characters
- Fix applied: `agent.go:1346-1347`

**Usage Metadata Structure**
All providers that support usage metadata emit a consistent structure:
```json
{
  "usage": {
    "total_tokens": 174,
    "input_tokens": 104,
    "output_tokens": 70
  },
  "model": "gpt-5-nano-2025-08-07",
  "provider": "openai"
}
```

This metadata is also aggregated and available in:
- Final task completion API response (`GET /api/v1/tasks/{id}`)
- Task execution database records
- Temporal workflow metadata

## Roadmap

### Phase 1 (Complete)
- ✅ Minimal event types: WORKFLOW_STARTED, AGENT_STARTED, AGENT_COMPLETED, ERROR_OCCURRED
- ✅ Extended event types: LLM_OUTPUT, TOOL_INVOKED, TOOL_OBSERVATION
- ✅ Usage metadata streaming for OpenAI, Anthropic, Groq, Google providers
- ✅ Three protocols: gRPC, SSE, WebSocket
- ✅ Replay support using bounded Redis Streams
- ✅ UTF-8 safe message truncation
- ✅ Fallback streaming preserves usage metadata
- ✅ Selective PostgreSQL persistence (filters ephemeral events)

### Phase 2A: Multi-Agent Coordination (Complete)
Quick wins leveraging existing infrastructure:
- ✅ **ROLE_ASSIGNED**: Emitted when role-based agents are activated (e.g., `data_analytics`, `ga4_analytics`)
  - Payload: role name, available tools, tool count
  - Location: `orchestrator_router.go:205-217`
- ✅ **DELEGATION**: Emitted when orchestrator delegates subtasks to agents
  - Already implemented in `orchestrator_router.go`
- ✅ **BUDGET_THRESHOLD**: Emitted when token budget crosses warning thresholds
  - Payload: usage_percent, tokens_used, budget_type (task/session)
  - Location: `budget/manager.go:258-274`

**Database Persistence**: All Phase 2A events persisted to PostgreSQL (via `shouldPersistEvent` filter)

### Phase 2B: Advanced Multi-Agent (Planned)
Requires new system implementations:
- ⏳ **AGENT_MESSAGE_SENT**: Inter-agent communication (requires `mailbox_v1` system)
- ⏳ **POLICY_EVALUATED**: Policy framework events (OPA integration exists, needs streaming)
- ⏳ **WASI_SANDBOX_EVENT**: Python code execution events (requires Rust→Go event bridge)

### Phase 3 (Future)
- WebSocket multiplexing for multiple workflows in one connection
- SDK helpers in Python/TypeScript for easy consumption
- Real-time dashboard components and visualization tools
