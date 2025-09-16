# Shannon Platform: Entry Points and Data Flow Reference

## Primary Entry Points

### 1. gRPC Orchestrator Service (Port 50052)
**Main Entry Point for AI Tasks**

```protobuf
service OrchestratorService {
  // Primary entry point for task submission
  rpc SubmitTask(SubmitTaskRequest) returns (SubmitTaskResponse);
  
  // Task monitoring and management
  rpc GetTaskStatus(GetTaskStatusRequest) returns (GetTaskStatusResponse);
  rpc CancelTask(CancelTaskRequest) returns (CancelTaskResponse);
  rpc ListTasks(ListTasksRequest) returns (ListTasksResponse);
  
  // Session management
  rpc GetSessionContext(GetSessionContextRequest) returns (GetSessionContextResponse);
  
  // Human-in-the-loop workflows
  rpc ApproveTask(ApproveTaskRequest) returns (ApproveTaskResponse);
  rpc GetPendingApprovals(GetPendingApprovalsRequest) returns (GetPendingApprovalsResponse);
}
```

**Usage Example**:
```bash
grpcurl -plaintext -d '{
  "metadata": {"userId":"user123","sessionId":"session456"},
  "query": "Analyze the market trends for renewable energy",
  "context": {"priority": "high", "budget": 1000}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### 2. HTTP Admin Interface (Port 8081)
**Administrative and Monitoring Interface**

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/health` | GET | Overall platform health status |
| `/stream/sse?workflow_id=<id>` | GET | Server-sent events streaming |
| `/stream/ws?workflow_id=<id>` | GET | WebSocket streaming |
| `/approvals/decision` | POST | Human approval decisions |

### 3. LLM Service REST API (Port 8000)
**AI Intelligence and Tool Integration**

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/completions` | POST | Generate LLM text completions |
| `/embeddings` | POST | Create vector embeddings |
| `/complexity` | POST | Analyze task complexity |
| `/tools/execute` | POST | Execute specific tools |
| `/tools/list` | GET | List available tools |
| `/tools/select` | POST | Auto-select tools for tasks |
| `/tools/mcp/register` | POST | Register MCP tools |
| `/health/live` | GET | Liveness probe |
| `/health/ready` | GET | Readiness probe |

### 4. Agent Core gRPC Service (Port 50051)
**Secure Execution Environment**

```protobuf
service AgentService {
  rpc ExecuteTask(ExecuteTaskRequest) returns (ExecuteTaskResponse);
  rpc GetHealth(HealthRequest) returns (HealthResponse);
}
```

## Complete Data Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    User/Client Request                      │
│    gRPC (50052) / HTTP (8081) / WebSocket                  │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│              Go Orchestrator Service                        │
│                                                             │
│  1. Request Validation & Authentication                     │
│  2. Session Context Retrieval (PostgreSQL/Redis)           │
│  3. Task Complexity Analysis → LLM Service                 │
│  4. Cognitive Strategy Selection                            │
│  5. Budget & Token Management                               │
│                                                             │
│  Cognitive Strategies:                                      │
│  ├── Simple Tasks: Direct LLM call                         │
│  ├── Chain of Thought: Sequential reasoning                │
│  ├── Tree of Thoughts: Branching exploration               │
│  ├── ReAct: Reason-Act-Observe loops                       │
│  ├── Debate: Multi-agent argumentation                     │
│  └── Reflection: Iterative improvement                     │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│                 Temporal Workflow Engine                    │
│                                                             │
│  • Reliable task orchestration                             │
│  • Deterministic replay                                    │
│  • Retry and error handling                                │
│  • Parallel/sequential execution                           │
│  • Human-in-the-loop checkpoints                          │
└─────────────┬───────────────────────────────────────────────┘
              │
    ┌─────────┼─────────┐
    │         │         │
    ▼         ▼         ▼
┌─────────┐ ┌─────────┐ ┌─────────────────┐
│ Python  │ │ Rust    │ │ External APIs   │
│ LLM     │ │ Agent   │ │ & Tools         │
│ Service │ │ Core    │ │                 │
│ (8000)  │ │ (50051) │ │ • Web Search    │
│         │ │         │ │ • Calculators   │
│ ┌─────┐ │ │ ┌─────┐ │ │ • File Systems  │
│ │LLMs │ │ │ │WASI │ │ │ • APIs          │
│ │     │ │ │ │Sand │ │ │ • Databases     │
│ │Open │ │ │ │box  │ │ │                 │
│ │AI   │ │ │ │     │ │ │                 │
│ │Anth │ │ │ │Tool │ │ │                 │
│ │ropie│ │ │ │Exec │ │ │                 │
│ │etc  │ │ │ │     │ │ │                 │
│ └─────┘ │ │ └─────┘ │ │                 │
│         │ │         │ │                 │
│ ┌─────┐ │ │ ┌─────┐ │ │                 │
│ │MCP  │ │ │ │OPA  │ │ │                 │
│ │Tools│ │ │ │Pol  │ │ │                 │
│ │     │ │ │ │icy │ │ │                 │
│ └─────┘ │ │ └─────┘ │ │                 │
└─────────┘ └─────────┘ └─────────────────┘
    │         │         │
    └─────────┼─────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Storage & State Layer                     │
│                                                             │
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────────────────┐ │
│ │ PostgreSQL  │ │    Redis    │ │        Qdrant           │ │
│ │   (5432)    │ │   (6379)    │ │        (6333)           │ │
│ │             │ │             │ │                         │ │
│ │• Tasks      │ │• Sessions   │ │• Vector Embeddings      │ │
│ │• Users      │ │• Cache      │ │• Semantic Search        │ │
│ │• Workflows  │ │• Rate Limits│ │• Memory Storage         │ │
│ │• Audit Logs │ │• Circuit    │ │• Context Chunks         │ │
│ │• Results    │ │  Breakers   │ │                         │ │
│ └─────────────┘ └─────────────┘ └─────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Response & Streaming                     │
│                                                             │
│  • Task Results (gRPC/JSON)                                │
│  • Real-time Progress (WebSocket/SSE)                      │
│  • Metrics & Monitoring (Prometheus)                       │
│  • Audit Trails (Structured Logs)                          │
└─────────────────────────────────────────────────────────────┘
```

## Output Formats and Interfaces

### 1. Task Response Format (gRPC)

```protobuf
message SubmitTaskResponse {
  string task_id = 1;
  string workflow_id = 2;
  shannon.common.TaskStatus status = 3;
  google.protobuf.Timestamp created_at = 4;
  TaskDecomposition decomposition = 5;
  int32 estimated_cost = 6;
}

message TaskResult {
  string task_id = 1;
  shannon.common.TaskStatus status = 2;
  string result = 3;
  repeated AgentResult agent_results = 4;
  TaskMetrics metrics = 5;
  google.protobuf.Timestamp completed_at = 6;
}
```

### 2. Streaming Output (WebSocket/SSE)

```json
{
  "type": "progress",
  "workflow_id": "wf_123",
  "timestamp": "2024-01-15T10:30:00Z",
  "data": {
    "stage": "agent_execution",
    "agent_id": "agent_1",
    "message": "Analyzing market trends...",
    "progress": 0.3,
    "tokens_used": 150,
    "estimated_remaining": 300
  }
}
```

### 3. Metrics Output (Prometheus)

```
# HELP shannon_tasks_total Total number of tasks processed
# TYPE shannon_tasks_total counter
shannon_tasks_total{status="completed"} 1234
shannon_tasks_total{status="failed"} 45

# HELP shannon_token_usage_total Total tokens consumed
# TYPE shannon_token_usage_total counter
shannon_token_usage_total{provider="openai",model="gpt-4"} 56789

# HELP shannon_response_duration_seconds Task processing duration
# TYPE shannon_response_duration_seconds histogram
shannon_response_duration_seconds_bucket{le="1.0"} 100
shannon_response_duration_seconds_bucket{le="5.0"} 450
```

## Integration Examples

### Basic Task Submission

```bash
# Via gRPC
grpcurl -plaintext -d '{
  "metadata": {"userId":"user1","sessionId":"sess1"},
  "query": "Create a business plan for a coffee shop"
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask

# Via HTTP API helper
curl -X POST http://localhost:8081/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "Create a business plan for a coffee shop"}'
```

### Real-time Monitoring

```javascript
// WebSocket streaming
const ws = new WebSocket('ws://localhost:8081/stream/ws?workflow_id=wf_123');
ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log('Progress:', update.data.progress);
};

// Server-Sent Events
const eventSource = new EventSource('http://localhost:8081/stream/sse?workflow_id=wf_123');
eventSource.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log('Update:', update);
};
```

### Tool Integration

```bash
# Execute specific tool
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "web_search",
    "parameters": {
      "query": "renewable energy market 2024",
      "num_results": 10
    }
  }'

# Auto-select tools for task
curl -X POST http://localhost:8000/tools/select \
  -H "Content-Type: application/json" \
  -d '{
    "task": "Research renewable energy trends and create a report",
    "max_tools": 3
  }'
```

This comprehensive reference covers all major entry points, data flows, and output formats in the Shannon platform.