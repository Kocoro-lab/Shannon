# Shannon Streaming APIs

This document describes the minimal, deterministic streaming interfaces exposed by the orchestrator. It covers gRPC, Server‑Sent Events (SSE), and WebSocket (WS) endpoints, including filters and resume semantics for rejoining sessions.

## Event Model

- Fields: `workflow_id`, `type`, `agent_id?`, `message?`, `timestamp`, `seq`.
- Minimal event types (behind `streaming_v1` gate):
  - `WORKFLOW_STARTED`, `AGENT_STARTED`, `AGENT_COMPLETED`, `ERROR_OCCURRED`.
  - P2P v1 adds: `MESSAGE_SENT`, `MESSAGE_RECEIVED`, `WORKSPACE_UPDATED`.
- Determinism: events are emitted from workflows as activities, recorded in Temporal history, and published to a local stream manager.

## gRPC: StreamingService

- RPC: `StreamingService.StreamTaskExecution(StreamRequest) returns (stream TaskUpdate)`
- Request fields:
  - `workflow_id` (required)
  - `types[]` (optional) — filter by event types
  - `last_event_id` (optional) — resume with events where `seq > last_event_id`
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

- Method: `GET /stream/sse?workflow_id=<id>&types=<csv>&last_event_id=<n>`
- Headers: supports `Last-Event-ID` for browser auto‑resume.
- CORS: `Access-Control-Allow-Origin: *` (dev‑friendly; front door should enforce auth in prod).

Example (curl):

```bash
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=AGENT_STARTED,AGENT_COMPLETED"
```

Notes:
- Server emits `id: <seq>` so browsers can reconnect with `Last-Event-ID`.
- Heartbeats are sent as SSE comments every 15s to keep proxies alive.

## WebSocket: HTTP `/stream/ws`

- Method: `GET /stream/ws?workflow_id=<id>&types=<csv>&last_event_id=<n>`
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

### Browser Demo
Open `docs/streaming-demo.html` in your browser for an interactive SSE demo with:
- Configurable host, workflow ID, event type filters
- Auto-resume support with Last-Event-ID
- Real-time event log display

## Configuration

### Environment Variables
- `STREAMING_RING_CAPACITY` (default: 256) - Number of recent events retained per workflow for replay

### Configuration File (`config/shannon.yaml`)
```yaml
streaming:
  ring_capacity: 256  # Number of recent events to retain per workflow for replay
```

The configuration supports both environment variables and YAML file settings, with environment variables taking precedence.

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
  
  useEffect(() => {
    const eventSource = new EventSource(
      `/stream/sse?workflow_id=${workflowId}&types=AGENT_COMPLETED`
    );
    
    eventSource.onmessage = (e) => {
      const event = JSON.parse(e.data);
      setEvents(prev => [...prev, event]);
    };
    
    return () => eventSource.close();
  }, [workflowId]);
  
  return (
    <div>
      {events.map(event => (
        <div key={event.seq}>
          {event.type}: {event.agent_id}
        </div>
      ))}
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

**"Events missing after reconnect"**
- Use `last_event_id` parameter or `Last-Event-ID` header
- Check ring buffer capacity - events older than buffer size are lost
- Consider increasing `STREAMING_RING_CAPACITY` for longer workflows

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

# Monitor ring buffer usage (logs)
docker compose logs orchestrator | grep "streaming"
```

## Roadmap

### Phase 1 (Current)
- ✅ Minimal event types: WORKFLOW_STARTED, AGENT_STARTED, AGENT_COMPLETED, ERROR_OCCURRED
- ✅ Three protocols: gRPC, SSE, WebSocket
- ✅ Replay support with ring buffer
- ✅ Configuration via shannon.yaml and environment variables

### Phase 2 (Multi-Agent Features)
- Extended event types after `roles_v1/supervisor_v1/mailbox_v1` are enabled:
  - `ROLE_ASSIGNED`, `AGENT_MESSAGE_SENT`, `SUPERVISOR_DELEGATED`
  - `POLICY_EVALUATED`, `BUDGET_THRESHOLD`, `WASI_SANDBOX_EVENT`

### Phase 3 (Advanced Features)
- WebSocket multiplexing for multiple workflows in one connection
- SDK helpers in Python/TypeScript for easy consumption
- Real-time dashboard components and visualization tools
