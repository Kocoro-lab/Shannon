# Shannon Embedded API Reference

**Version**: 1.0  
**Last Updated**: 2026-01-11  
**Status**: Production Ready

---

## Overview

Shannon Embedded API provides a complete AI agent platform for desktop and mobile applications using Tauri. It runs entirely locally with no external dependencies beyond LLM provider APIs.

### Key Features

- **Local-First Architecture**: All data stored in SQLite
- **Encrypted API Keys**: AES-256-GCM encryption for provider credentials
- **Real-Time Streaming**: SSE and WebSocket support for live events
- **JWT Authentication**: Secure user authentication and multi-tenancy
- **OpenAI-Compatible**: Drop-in replacement for OpenAI SDK
- **Multi-Provider Support**: OpenAI, Anthropic, Google, Groq, xAI

---

## Base URL

```
http://localhost:8765/api/v1
```

Default embedded API port: `8765`

---

## Authentication

### JWT Token

```bash
Authorization: Bearer <jwt_token>
```

### Anonymous Embedded Mode

If no JWT provided, requests default to `embedded_user` identity.

---

## Endpoints

### 1. Health Check

**GET** `/health`

Check API health status.

**Response**:
```json
{
  "status": "healthy",
  "version": "0.1.0"
}
```

---

### 2. API Information

**GET** `/api/v1/info`

Get API metadata and capabilities.

**Response**:
```json
{
  "name": "Shannon API",
  "version": "0.1.0",
  "description": "Unified Rust Gateway and LLM Service",
  "endpoints": [
    {
      "path": "/api/v1/chat/completions",
      "method": "POST",
      "description": "OpenAI-compatible chat completions"
    }
  ]
}
```

---

### 3. Capabilities

**GET** `/api/v1/capabilities`

Get enabled features and providers.

**Response**:
```json
{
  "streaming": true,
  "websocket": true,
  "sse": true,
  "tool_calling": true,
  "multi_modal": true,
  "providers": ["openai", "anthropic", "google"]
}
```

---

## Settings Management

### List All Settings

**GET** `/api/v1/settings`

Get all user settings.

**Response**:
```json
{
  "settings": [
    {
      "key": "theme",
      "value": "dark",
      "updated_at": "2026-01-11T17:00:00Z"
    }
  ]
}
```

---

### Get Setting

**GET** `/api/v1/settings/{key}`

Get a specific setting value.

**Response**:
```json
{
  "key": "theme",
  "value": "dark",
  "updated_at": "2026-01-11T17:00:00Z"
}
```

---

### Set Setting

**POST** `/api/v1/settings`

Create or update a setting.

**Request**:
```json
{
  "key": "theme",
  "value": "dark"
}
```

**Response**:
```json
{
  "success": true,
  "key": "theme"
}
```

---

### Delete Setting

**DELETE** `/api/v1/settings/{key}`

Delete a setting.

**Response**:
```json
{
  "success": true
}
```

---

## API Key Management

### List Providers

**GET** `/api/v1/settings/api-keys`

List all configured LLM providers.

**Response**:
```json
{
  "providers": [
    {
      "provider": "openai",
      "configured": true,
      "masked_key": "sk-...abc123",
      "last_used": "2026-01-11T16:30:00Z"
    },
    {
      "provider": "anthropic",
      "configured": false
    }
  ]
}
```

---

### Get API Key

**GET** `/api/v1/settings/api-keys/{provider}`

Get metadata for a provider's API key (does not return actual key).

**Response**:
```json
{
  "provider": "openai",
  "configured": true,
  "masked_key": "sk-...abc123",
  "last_used": "2026-01-11T16:30:00Z"
}
```

---

### Set API Key

**POST** `/api/v1/settings/api-keys/{provider}`

Store an encrypted API key for a provider.

**Request**:
```json
{
  "api_key": "sk-1234567890abcdef"
}
```

**Response**:
```json
{
  "success": true,
  "provider": "openai",
  "masked_key": "sk-...cdef"
}
```

**Supported Providers**:
- `openai`
- `anthropic`
- `google`
- `groq`
- `xai`

---

### Delete API Key

**DELETE** `/api/v1/settings/api-keys/{provider}`

Remove an API key from storage.

**Response**:
```json
{
  "success": true
}
```

---

## Task Management

### Submit Task

**POST** `/api/v1/tasks`

Submit a new task for execution.

**Request**:
```json
{
  "prompt": "What is the weather in San Francisco?",
  "context": {
    "role_preset": "assistant",
    "system_prompt": "You are a helpful assistant.",
    "model": "gpt-4",
    "provider": "openai",
    "model_tier": "premium",
    "temperature": 0.7,
    "max_tokens": 2000,
    "research_strategy": "deep",
    "context_window_strategy": "adaptive"
  }
}
```

**Response**:
```json
{
  "task_id": "task_abc123",
  "status": "pending",
  "created_at": "2026-01-11T17:00:00Z"
}
```

---

### Get Task Status

**GET** `/api/v1/tasks/{id}`

Get current task status and result.

**Response**:
```json
{
  "task_id": "task_abc123",
  "status": "completed",
  "result": {
    "output": "The weather in San Francisco is...",
    "usage": {
      "prompt_tokens": 45,
      "completion_tokens": 120,
      "total_tokens": 165
    },
    "model_used": "gpt-4-turbo",
    "provider": "openai",
    "model_breakdown": [
      {
        "model": "gpt-4-turbo",
        "calls": 1,
        "total_tokens": 165
      }
    ]
  },
  "created_at": "2026-01-11T17:00:00Z",
  "completed_at": "2026-01-11T17:00:15Z"
}
```

**Status Values**:
- `pending` - Queued for execution
- `running` - Currently executing
- `completed` - Successfully completed
- `failed` - Execution failed
- `cancelled` - Cancelled by user
- `paused` - Execution paused

---

### List Tasks

**GET** `/api/v1/tasks`

List tasks with optional filtering.

**Query Parameters**:
- `status` - Filter by status (optional)
- `limit` - Results per page (default: 20, max: 100)
- `offset` - Pagination offset (default: 0)

**Response**:
```json
{
  "tasks": [
    {
      "task_id": "task_abc123",
      "status": "completed",
      "created_at": "2026-01-11T17:00:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

---

### Get Task Output

**GET** `/api/v1/tasks/{id}/output`

Get the final output of a completed task.

**Response**:
```json
{
  "task_id": "task_abc123",
  "output": "The weather in San Francisco is sunny with a high of 72°F.",
  "completed_at": "2026-01-11T17:00:15Z"
}
```

---

### Get Task Progress

**GET** `/api/v1/tasks/{id}/progress`

Get real-time progress information.

**Response**:
```json
{
  "task_id": "task_abc123",
  "status": "running",
  "progress_percent": 45,
  "current_step": "Researching topic",
  "estimated_completion": "2026-01-11T17:01:00Z"
}
```

---

### Get Control State

**GET** `/api/v1/tasks/{id}/control-state`

Get pause/cancel control state.

**Response**:
```json
{
  "task_id": "task_abc123",
  "paused": false,
  "cancelled": false,
  "checkpoint_id": null
}
```

---

### Pause Task

**POST** `/api/v1/tasks/{id}/pause`

Pause a running task.

**Response**:
```json
{
  "success": true,
  "task_id": "task_abc123",
  "checkpoint_id": "ckpt_xyz789"
}
```

---

### Resume Task

**POST** `/api/v1/tasks/{id}/resume`

Resume a paused task.

**Response**:
```json
{
  "success": true,
  "task_id": "task_abc123"
}
```

---

### Cancel Task

**POST** `/api/v1/tasks/{id}/cancel`

Cancel a running or paused task.

**Response**:
```json
{
  "success": true,
  "task_id": "task_abc123"
}
```

---

## Session Management

### Create Session

**POST** `/api/v1/sessions`

Create a new conversation session.

**Request**:
```json
{
  "name": "Research Session",
  "metadata": {
    "project": "AI Research"
  }
}
```

**Response**:
```json
{
  "session_id": "sess_xyz789",
  "created_at": "2026-01-11T17:00:00Z"
}
```

---

### List Sessions

**GET** `/api/v1/sessions`

List all sessions for the current user.

**Query Parameters**:
- `limit` - Results per page (default: 20)
- `offset` - Pagination offset (default: 0)

**Response**:
```json
{
  "sessions": [
    {
      "session_id": "sess_xyz789",
      "name": "Research Session",
      "created_at": "2026-01-11T17:00:00Z",
      "updated_at": "2026-01-11T17:30:00Z",
      "message_count": 15
    }
  ],
  "total": 1
}
```

---

### Get Session

**GET** `/api/v1/sessions/{id}`

Get session details.

**Response**:
```json
{
  "session_id": "sess_xyz789",
  "name": "Research Session",
  "created_at": "2026-01-11T17:00:00Z",
  "metadata": {
    "project": "AI Research"
  }
}
```

---

### Get Session History

**GET** `/api/v1/sessions/{id}/history`

Get conversation history for a session.

**Response**:
```json
{
  "session_id": "sess_xyz789",
  "messages": [
    {
      "role": "user",
      "content": "What is machine learning?",
      "timestamp": "2026-01-11T17:00:00Z"
    },
    {
      "role": "assistant",
      "content": "Machine learning is...",
      "timestamp": "2026-01-11T17:00:05Z"
    }
  ]
}
```

---

### Get Session Events

**GET** `/api/v1/sessions/{id}/events`

Get event stream for a session (SSE).

**Response**: Server-Sent Events stream

---

## Streaming

### Stream Task Events (SSE)

**GET** `/api/v1/tasks/{id}/stream`

Server-Sent Events stream for task updates.

**Query Parameters**:
- `event_types` - Comma-separated event type filter (optional)
- `last_event_id` - Resume from specific event (optional)

**Response**: SSE stream

**Example Event**:
```
event: thread.message.delta
id: evt_123
data: {"type":"message_delta","content":"Hello","seq":1}

event: thread.message.completed
id: evt_124
data: {"type":"message_complete","content":"Hello world","role":"assistant","seq":2}
```

---

### Stream Task Events (WebSocket)

**WS** `/api/v1/tasks/{id}/stream/ws`

WebSocket connection for bidirectional task communication.

**Message Format**:
```json
{
  "type": "message_delta",
  "content": "Hello",
  "seq": 1
}
```

---

## Event Types

Shannon Embedded API emits 26+ event types for comprehensive observability.

### LLM Events

| Event Type | Description |
|------------|-------------|
| `llm.prompt` | LLM prompt being sent (T114) |
| `thread.message.delta` | Streaming content delta (T115) |
| `thread.message.completed` | Message completed (T116) |

### Tool Events

| Event Type | Description |
|------------|-------------|
| `tool_call.delta` | Tool call streaming |
| `tool_call.complete` | Tool call completed (T117) |
| `tool.result` | Tool execution result (T118) |
| `tool.error` | Tool execution error (T119) |

### Workflow Events

| Event Type | Description |
|------------|-------------|
| `workflow.started` | Workflow execution started (T090) |
| `workflow.completed` | Workflow completed successfully (T091) |
| `workflow.failed` | Workflow execution failed (T092) |

### Agent Events

| Event Type | Description |
|------------|-------------|
| `agent.started` | Agent execution started (T093) |
| `agent.completed` | Agent completed successfully (T094) |
| `agent.failed` | Agent execution failed (T095) |

### Control Signal Events

| Event Type | Description |
|------------|-------------|
| `workflow.pausing` | Workflow is pausing (T120) |
| `workflow.paused` | Workflow paused (T121) |
| `workflow.resumed` | Workflow resumed (T122) |
| `workflow.cancelling` | Workflow is cancelling (T123) |
| `workflow.cancelled` | Workflow cancelled (T124) |

### Multi-Agent Events

| Event Type | Description |
|------------|-------------|
| `agent.role_assigned` | Role assigned to agent (T135) |
| `agent.delegation` | Task delegated to agent (T136) |
| `agent.progress` | Agent progress update (T137) |
| `team.recruited` | Team recruited for task (T138) |
| `team.retired` | Team retired after completion (T139) |
| `team.status` | Team status update (T140) |

### Advanced Events

| Event Type | Description |
|------------|-------------|
| `workflow.budget_threshold` | Token budget warning (T141) |
| `workflow.synthesis` | Research synthesis event (T142) |
| `workflow.reflection` | Reflection step event (T143) |
| `workflow.approval_requested` | Approval requested (T144) |
| `workflow.approval_decision` | Approval decision made (T145) |

### Misc Events

| Event Type | Description |
|------------|-------------|
| `thinking` | Model reasoning content |
| `usage` | Token usage statistics (T096) |
| `error` | Error occurred |
| `done` | Stream completed |

---

## Context Parameters

### TaskContext Object

```json
{
  "role_preset": "assistant",           // Role: assistant, researcher, developer
  "system_prompt": "Custom prompt",     // Override system prompt
  "prompt_params": {                    // Dynamic prompt variables
    "expertise": "machine learning"
  },
  "model_tier": "premium",              // Tier: basic, standard, premium, ultra
  "model": "gpt-4-turbo",              // Specific model override
  "provider": "openai",                 // Provider override
  "temperature": 0.7,                   // Sampling temperature (0-2)
  "max_tokens": 2000,                   // Max completion tokens
  "research_strategy": "deep",          // Strategy: quick, balanced, deep
  "research_depth": 3,                  // Recursion depth (1-5)
  "context_window_strategy": "adaptive", // Strategy: fixed, adaptive, dynamic
  "enable_reflection": true,            // Enable self-critique
  "enable_citations": true              // Enable source citations
}
```

---

## Embedded Mode vs Cloud API

### Feature Parity

| Feature | Embedded | Cloud | Notes |
|---------|----------|-------|-------|
| Task Submission | ✅ | ✅ | Identical API |
| SSE Streaming | ✅ | ✅ | Same event types |
| WebSocket | ✅ | ✅ | Bidirectional |
| API Keys | ✅ Encrypted | ✅ Vault | AES-256-GCM local encryption |
| Settings | ✅ SQLite | ✅ PostgreSQL | Local storage |
| Sessions | ✅ | ✅ | Full support |
| Control Signals | ✅ | ✅ | Pause/resume/cancel |
| Multi-Agent | ✅ | ✅ | Full event support |
| Scheduled Tasks | ⏳ Future | ✅ | Not yet in embedded |
| Team Collaboration | ❌ | ✅ | Cloud-only |
| Usage Analytics | ✅ Local | ✅ Cloud | Local tracking only |

### Key Differences

**Authentication**:
- **Embedded**: Optional JWT, defaults to `embedded_user`
- **Cloud**: Required JWT with RBAC

**Storage**:
- **Embedded**: SQLite with encrypted API keys
- **Cloud**: PostgreSQL + Redis + Qdrant

**Deployment**:
- **Embedded**: Single Rust binary in Tauri app
- **Cloud**: Docker/Kubernetes microservices

**Networking**:
- **Embedded**: Localhost only (127.0.0.1:8765)
- **Cloud**: Public endpoints with TLS

---

## Error Handling

### Error Response Format

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid task parameters",
    "details": {
      "field": "context.model",
      "reason": "Unsupported model"
    }
  }
}
```

### Error Codes

| Code | Status | Description |
|------|--------|-------------|
| `INVALID_REQUEST` | 400 | Malformed request |
| `UNAUTHORIZED` | 401 | Missing/invalid auth token |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `CONFLICT` | 409 | Resource conflict |
| `RATE_LIMITED` | 429 | Too many requests |
| `INTERNAL_ERROR` | 500 | Server error |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily down |

---

## Rate Limiting

Embedded mode uses local rate limiting with `governor` crate:

- **Default**: 100 requests per minute per user
- **Burst**: 10 concurrent requests
- **Configurable**: Via `config/shannon.yaml`

---

## Security

### Encryption

- **API Keys**: AES-256-GCM encryption at rest
- **Encryption Key**: Stored in `~/.shannon/encryption.key` with 0600 permissions
- **JWT Secrets**: Configurable via environment or config file

### Best Practices

1. **Never log decrypted API keys**
2. **Use HTTPS for Tauri IPC** (automatic with Tauri v2+)
3. **Rotate JWT secrets** periodically
4. **Validate all user input** server-side
5. **Use prepared statements** for SQL (automatic with `sqlx`)

---

## Usage Example

### TypeScript/JavaScript

```typescript
import { fetch } from '@tauri-apps/plugin-http';

// Submit task
const response = await fetch('http://localhost:8765/api/v1/tasks', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    prompt: 'Explain quantum computing',
    context: {
      model_tier: 'premium',
      research_strategy: 'deep'
    }
  })
});

const { task_id } = await response.json();

// Stream events
const eventSource = new EventSource(
  `http://localhost:8765/api/v1/tasks/${task_id}/stream`
);

eventSource.addEventListener('thread.message.delta', (event) => {
  const data = JSON.parse(event.data);
  console.log('Delta:', data.content);
});

eventSource.addEventListener('thread.message.completed', (event) => {
  const data = JSON.parse(event.data);
  console.log('Complete:', data.content);
  eventSource.close();
});
```

### Rust (Tauri Command)

```rust
use serde_json::json;
use tauri::Manager;

#[tauri::command]
async fn submit_task(
    prompt: String,
    app: tauri::AppHandle,
) -> Result<String, String> {
    let client = reqwest::Client::new();
    
    let response = client
        .post("http://localhost:8765/api/v1/tasks")
        .json(&json!({
            "prompt": prompt,
            "context": {
                "model_tier": "premium"
            }
        }))
        .send()
        .await
        .map_err(|e| e.to_string())?;
    
    let result: serde_json::Value = response
        .json()
        .await
        .map_err(|e| e.to_string())?;
    
    Ok(result["task_id"].as_str().unwrap().to_string())
}
```

---

## Performance

### Benchmarks (M1 Mac, Single Instance)

| Metric | Value |
|--------|-------|
| Cold start | ~100ms |
| Memory (idle) | ~50MB |
| Requests/sec | ~10,000 (health check) |
| P50 latency | ~5ms |
| P99 latency | ~20ms |

### Optimization Tips

1. **Connection pooling**: SQLite pool size 5-10
2. **Event buffer**: Ring buffer with 1000 events
3. **Streaming**: Use SSE for uni-directional, WebSocket for bi-directional
4. **Batch operations**: Use transaction for multiple inserts

---

## Troubleshooting

### Common Issues

**Q: API key validation fails**
- Ensure key is properly stored via `/api/v1/settings/api-keys/{provider}`
- Check encryption key exists at `~/.shannon/encryption.key`
- Verify provider name matches exactly: `openai`, `anthropic`, `google`, `groq`, `xai`

**Q: SSE connection drops**
- Implement reconnection logic with `Last-Event-ID` header
- Check network/firewall settings for localhost
- Verify task is still running

**Q: Task stuck in `pending`**
- Check API key is configured for selected provider
- Review logs for error messages
- Verify model name is supported

**Q: High memory usage**
- Reduce event buffer size in config
- Implement event pruning for old sessions
- Check for memory leaks in long-running tasks

---

## Migration from Cloud API

See [`docs/cloud-to-embedded-migration.md`](./cloud-to-embedded-migration.md) for detailed migration guide.

---

## Changelog

### v1.0 (2026-01-11)
- ✅ Full task management API
- ✅ Encrypted API key storage
- ✅ SSE and WebSocket streaming
- ✅ 26+ event types
- ✅ Session management
- ✅ Control signals (pause/resume/cancel)
- ✅ Multi-agent events
- ✅ Advanced workflow events

---

## Support

- **Documentation**: <https://docs.shannon.ai>
- **Issues**: <https://github.com/prometheus/shannon/issues>
- **Discord**: <https://discord.gg/shannon>

---

**Generated**: 2026-01-11 | **Specification**: [`specs/embedded-feature-parity-spec.md`](../specs/embedded-feature-parity-spec.md)
