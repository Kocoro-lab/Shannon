# Shannon Agent Core - API Documentation

## Table of Contents
1. [gRPC API](#grpc-api)
2. [Tool Registry API](#tool-registry-api)
3. [Python Integration API](#python-integration-api)
4. [Error Codes](#error-codes)
5. [Examples](#examples)

## gRPC API

The agent core exposes a gRPC service on port 50051.

### ExecuteTask

Execute a task via the Agent Core gateway. Modes are advisory and routed to Python; Rust enforces request-level policies (timeouts, rate limits, circuit breakers) uniformly.

**Request:**
```protobuf
message ExecuteTaskRequest {
  TaskMetadata metadata = 1;
  string query = 2;
  google.protobuf.Struct context = 3;
  ExecutionMode mode = 4;
  repeated string available_tools = 5;
  AgentConfig config = 6;
  SessionContext session_context = 7;
}
```

**Response:**
```protobuf
message ExecuteTaskResponse {
  string task_id = 1;
  StatusCode status = 2;
  string result = 3;
  repeated ToolCall tool_calls = 4;
  repeated ToolResult tool_results = 5;
  ExecutionMetrics metrics = 6;
  string error_message = 7;
  AgentState final_state = 8;
}
```

**Example:**
```rust
let request = ExecuteTaskRequest {
    query: "Calculate the sum of 42 and 58".to_string(),
    mode: ExecutionMode::Simple as i32,
    ..Default::default()
};

let response = client.execute_task(request).await?;
println!("Result: {}", response.result);
```

### StreamExecuteTask

Execute a task with streaming updates.

**Request:** Same as ExecuteTask

**Response Stream:**
```protobuf
message TaskUpdate {
  string task_id = 1;
  AgentState state = 2;
  string message = 3;
  ToolCall tool_call = 4;
  ToolResult tool_result = 5;
  double progress = 6;
}
```

### DiscoverTools

Discover available tools based on query and filters.

**Request:**
```protobuf
message DiscoverToolsRequest {
  string query = 1;
  repeated string categories = 2;
  repeated string tags = 3;
  bool exclude_dangerous = 4;
  int32 max_results = 5;
}
```

**Response:**
```protobuf
message DiscoverToolsResponse {
  repeated ToolCapability tools = 1;
}

message ToolCapability {
  string id = 1;
  string name = 2;
  string description = 3;
  string category = 4;
  google.protobuf.Struct input_schema = 5;
  google.protobuf.Struct output_schema = 6;
  repeated string required_permissions = 7;
  int64 estimated_duration_ms = 8;
  bool is_dangerous = 9;
  string version = 10;
  string author = 11;
  repeated string tags = 12;
  repeated ToolExample examples = 13;
  RateLimit rate_limit = 14;
  int64 cache_ttl_ms = 15;
}
```

**Example:**
```rust
let request = DiscoverToolsRequest {
    query: "search".to_string(),
    exclude_dangerous: true,
    max_results: 5,
    ..Default::default()
};

let response = client.discover_tools(request).await?;
for tool in response.tools {
    println!("Found tool: {} - {}", tool.name, tool.description);
}
```

### GetToolCapability

Get detailed information about a specific tool.

**Request:**
```protobuf
message GetToolCapabilityRequest {
  string tool_id = 1;
}
```

**Response:**
```protobuf
message GetToolCapabilityResponse {
  ToolCapability tool = 1;
}
```

### GetCapabilities

Get agent capabilities and configuration.

**Response:**
```protobuf
message GetCapabilitiesResponse {
  repeated string supported_tools = 1;
  repeated ExecutionMode supported_modes = 2;
  int64 max_memory_mb = 3;
  int32 max_concurrent_tasks = 4;
  string version = 5;
}
```

### HealthCheck

Check agent health status.

**Response:**
```protobuf
message HealthCheckResponse {
  bool healthy = 1;
  string message = 2;
  int64 uptime_seconds = 3;
  int32 active_tasks = 4;
  double memory_usage_percent = 5;
}
```

## Tool Registry API

### Rust API

```rust
use shannon_agent_core::tool_registry::{ToolRegistry, ToolCapability, ToolDiscoveryRequest};

// Create registry
let registry = ToolRegistry::new();

// Register a tool
let tool = ToolCapability {
    id: "my_tool".to_string(),
    name: "My Tool".to_string(),
    description: "A custom tool".to_string(),
    category: "custom".to_string(),
    // ... other fields
};
registry.register_tool(tool);

// Discover tools
let request = ToolDiscoveryRequest {
    query: Some("search".to_string()),
    categories: None,
    tags: None,
    exclude_dangerous: Some(true),
    max_results: Some(10),
};
let tools = registry.discover_tools(request);

// Get specific tool
let tool = registry.get_tool("calculator");
```

## Python Integration API

The Rust agent communicates with Python LLM service via HTTP REST API.

### Tool Selection

**Endpoint:** `POST /tools/select`

**Request:**
```json
{
  "task": "Search for information about Rust programming",
  "context": {
    "session_id": "abc123",
    "previous_tools": ["web_search"]
  },
  "exclude_dangerous": true,
  "max_tools": 3
}
```

**Response:**
```json
{
  "calls": [
    {
      "tool_name": "web_search",
      "parameters": {
        "query": "Rust programming",
        "max_results": 5
      }
    }
  ],
  "provider_used": "openai"
}
```

### Tool Execution

**Endpoint:** `POST /tools/execute`

**Request:**
```json
{
  "tool_name": "calculator",
  "parameters": {
    "expression": "42 + 58"
  }
}
```

**Response:**
```json
{
  "success": true,
  "output": 100,
  "error": null
}
```

### Task Analysis

**Endpoint:** `POST /analyze_task`

**Request:**
```json
{
  "query": "Build a web application with user authentication",
  "context": {
    "session_id": "abc123"
  }
}
```

**Response:**
```json
{
  "execution_mode": "complex",
  "subtasks": [
    "Design database schema",
    "Implement authentication",
    "Create API endpoints",
    "Build frontend"
  ],
  "estimated_tokens": 5000,
  "confidence": 0.85
}
```

### Tool List

**Endpoint:** `GET /tools/list`

**Query Parameters:**
- `exclude_dangerous` (boolean): Filter out dangerous tools

**Response:**
```json
[
  "calculator",
  "web_search",
  "database_query",
  "code_executor"
]
```

## Error Codes

### gRPC Status Codes

| Code | Name | Description |
|------|------|-------------|
| 0 | OK | Success |
| 3 | INVALID_ARGUMENT | Invalid request parameters |
| 5 | NOT_FOUND | Tool or resource not found |
| 8 | RESOURCE_EXHAUSTED | Memory or rate limit exceeded |
| 13 | INTERNAL | Internal server error |
| 14 | UNAVAILABLE | Service temporarily unavailable |

### Agent Error Types

```rust
pub enum AgentError {
    // Tool errors
    ToolNotFound { name: String },
    ToolExecutionFailed { tool: String, reason: String },
    ToolTimeout { tool: String, timeout_ms: u64 },
    
    // Resource errors
    MemoryExhausted { requested: usize, available: usize },
    TokenLimitExceeded { used: u32, limit: u32 },
    
    // Sandbox errors
    SandboxViolation { operation: String },
    WasmExecutionError(String),
    
    // Network errors
    NetworkError(String),
    ServiceUnavailable { service: String },
    
    // Configuration errors
    ConfigurationError(String),
    InvalidParameter { param: String, reason: String },
}
```

## Examples

### Complete Task Execution Example

```rust
use shannon_agent_core::grpc_server::proto::agent::*;
use tonic::Request;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Connect to agent
    let mut client = agent_service_client::AgentServiceClient::connect(
        "http://localhost:50051"
    ).await?;
    
    // Prepare request
    let request = Request::new(ExecuteTaskRequest {
        query: "Search for the latest Rust programming news and summarize it".to_string(),
        mode: ExecutionMode::Standard as i32,
        available_tools: vec![
            "web_search".to_string(),
            "calculator".to_string(),
        ],
        config: Some(AgentConfig {
            max_iterations: 5,
            timeout_seconds: 30,
            enable_sandbox: true,
            memory_limit_mb: 256,
            enable_learning: false,
        }),
        session_context: Some(SessionContext {
            session_id: "user-123".to_string(),
            history: vec![
                "User: What is Rust?".to_string(),
                "Agent: Rust is a systems programming language...".to_string(),
            ],
            total_tokens_used: 1500,
            total_cost_usd: 0.003,
            ..Default::default()
        }),
        ..Default::default()
    });
    
    // Execute task
    let response = client.execute_task(request).await?;
    let response = response.into_inner();
    
    // Process response
    println!("Task ID: {}", response.task_id);
    println!("Status: {:?}", response.status);
    println!("Result: {}", response.result);
    
    // Check metrics
    if let Some(metrics) = response.metrics {
        println!("Tokens used: {}", metrics.token_usage.as_ref().unwrap().total_tokens);
        println!("Execution time: {}ms", metrics.latency_ms);
    }
    
    Ok(())
}
```

### Streaming Execution Example

```rust
use tokio_stream::StreamExt;

let mut stream = client.stream_execute_task(request).await?.into_inner();

while let Some(update) = stream.next().await {
    match update {
        Ok(task_update) => {
            println!("Progress: {}%", (task_update.progress * 100.0) as u32);
            println!("State: {:?}", task_update.state);
            println!("Message: {}", task_update.message);
            
            if let Some(tool_call) = task_update.tool_call {
                println!("Calling tool: {}", tool_call.tool_name);
            }
        }
        Err(e) => {
            eprintln!("Stream error: {}", e);
            break;
        }
    }
}
```

### Tool Discovery Example

```rust
// Discover calculation tools
let request = DiscoverToolsRequest {
    categories: vec!["calculation".to_string()],
    exclude_dangerous: true,
    max_results: 10,
    ..Default::default()
};

let response = client.discover_tools(Request::new(request)).await?;
let tools = response.into_inner().tools;

for tool in tools {
    println!("Tool: {} ({})", tool.name, tool.category);
    println!("  Description: {}", tool.description);
    println!("  Duration: ~{}ms", tool.estimated_duration_ms);
    println!("  Dangerous: {}", tool.is_dangerous);
    
    // Check rate limits
    if let Some(rate_limit) = tool.rate_limit {
        println!("  Rate limit: {}/min", rate_limit.requests_per_minute);
    }
}
```

### Error Handling Example

```rust
match client.execute_task(request).await {
    Ok(response) => {
        let response = response.into_inner();
        if response.status == StatusCode::Ok as i32 {
            println!("Success: {}", response.result);
        } else {
            eprintln!("Task failed: {}", response.error_message);
        }
    }
    Err(status) => {
        match status.code() {
            tonic::Code::InvalidArgument => {
                eprintln!("Invalid request: {}", status.message());
            }
            tonic::Code::ResourceExhausted => {
                eprintln!("Resource limit exceeded: {}", status.message());
            }
            tonic::Code::Unavailable => {
                eprintln!("Service unavailable, retry later");
            }
            _ => {
                eprintln!("Unexpected error: {}", status);
            }
        }
    }
}
```

## Rate Limiting

The agent core supports rate limiting enforcement at multiple levels:

1. **Tool-level**: Each tool can specify rate limits
2. **Request-level**: Per-request timeout and budget limits
3. **Global**: Overall system rate limits

**Note:** HTTP rate limit headers (`X-RateLimit-*`) are returned by the Gateway service, not directly by agent-core gRPC endpoints. See `go/orchestrator/cmd/gateway` for HTTP-level rate limiting.

## Metrics

Prometheus metrics available at `http://localhost:2113/metrics`:

**Task Metrics:**
- `agent_core_tasks_total{mode, status}`: Total tasks processed
- `agent_core_task_duration_seconds{mode}`: Task execution duration
- `agent_core_task_tokens{mode, model}`: Tokens used per task

**Tool Metrics:**
- `agent_core_tool_executions_total{tool_name, status}`: Total tool executions
- `agent_core_tool_duration_seconds{tool_name}`: Tool execution latency
- `agent_core_tool_selection_duration_seconds{status}`: Tool selection latency

**Memory Metrics:**
- `agent_core_memory_pool_used_bytes`: Current memory pool usage
- `agent_core_memory_pool_total_bytes`: Total memory pool size

**gRPC Metrics:**
- `agent_core_grpc_requests_total{method, status}`: Total gRPC requests
- `agent_core_grpc_request_duration_seconds{method}`: gRPC request duration

**Enforcement Metrics:**
- `agent_core_enforcement_drops_total{reason}`: Requests dropped by enforcement layer
- `agent_core_enforcement_allowed_total{outcome}`: Requests allowed by enforcement layer

**FSM Metrics:**
- `agent_core_fsm_transitions_total{from_state, to_state}`: FSM state transitions
- `agent_core_fsm_current_state`: Current FSM state (encoded as number)

## Versioning

The API follows semantic versioning:
- **Major**: Breaking changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes

Current version: `0.1.0`

Check version via capabilities:
```rust
let capabilities = client.get_capabilities(Request::new(GetCapabilitiesRequest {})).await?;
println!("Agent version: {}", capabilities.into_inner().version);
```
