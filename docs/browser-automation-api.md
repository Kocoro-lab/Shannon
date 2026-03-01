# Browser Automation API Guide

This guide explains how to use Shannon's browser automation tools via the API.

## Quick Start

Submit a task with `role: "browser_use"` in the context:

```bash
curl -X POST https://api.shannon.run/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: YOUR_API_KEY" \
  -d '{
    "query": "Navigate to https://example.com and extract the main heading",
    "session_id": "my-session-123",
    "context": {
      "role": "browser_use"
    }
  }'
```

## How It Works

When `role: "browser_use"` is specified:

1. **React Workflow**: The task is routed to a multi-turn React workflow (not single-shot)
2. **Iterative Execution**: The agent can make multiple tool calls in sequence
3. **Session Persistence**: Browser sessions persist across tool calls within the same task
4. **Automatic Cleanup**: Sessions are cleaned up after task completion (5 min TTL)

## API Reference

### Submit Task

```
POST /api/v1/tasks
```

**Request Body:**

```json
{
  "query": "Your browser automation instruction",
  "session_id": "unique-session-id",
  "context": {
    "role": "browser_use"
  }
}
```

**Response:**

```json
{
  "task_id": "task-xxx-1234567890",
  "status": "STATUS_CODE_OK",
  "message": "Task submitted successfully"
}
```

### Get Task Status

```
GET /api/v1/tasks/{task_id}
```

**Response (completed):**

```json
{
  "task_id": "task-xxx-1234567890",
  "status": "TASK_STATUS_COMPLETED",
  "result": "The extracted content or summary...",
  "metadata": {
    "iterations": 3,
    "actions": 4,
    "observations": 3
  }
}
```

### Stream Task Events (SSE)

```
GET /api/v1/tasks/{task_id}/events
```

Events include:
- `WORKFLOW_STARTED` - Task processing began
- `ROLE_ASSIGNED` - browser_use role activated
- `AGENT_STARTED` - Agent iteration started
- `TOOL_STARTED` / `TOOL_COMPLETED` - Tool execution
- `AGENT_COMPLETED` - Agent iteration finished
- `WORKFLOW_COMPLETED` - Task finished

## Browser Tool

A single unified `browser` tool with an `action` parameter handles all browser automation.

### Actions

| Action | Description | Required Params | Optional Params |
|--------|-------------|----------------|-----------------|
| `navigate` | Go to a URL | `url` | `wait_until`, `timeout_ms` |
| `click` | Click an element | `selector` | `button`, `click_count`, `timeout_ms` |
| `type` | Type text into input | `selector`, `text` | `timeout_ms` |
| `screenshot` | Capture page image | — | `full_page` |
| `extract` | Get page/element content | — | `selector`, `extract_type`, `attribute` |
| `scroll` | Scroll page or element | — | `selector`, `x`, `y` |
| `wait` | Wait for element/duration | — | `selector`, `timeout_ms` |
| `close` | End browser session | — | — |

Note: An `evaluate` action (JavaScript execution) exists in code but is fully disabled — not advertised in the tool schema and blocked by session context sanitizers. To enable, add `allow_browser_evaluate` to safe_keys in both `api/tools.py` and `api/agent.py`.

## Example Use Cases

### 1. Read and Summarize a Web Page

```json
{
  "query": "Read https://example.com/article and summarize the main points",
  "session_id": "reader-001",
  "context": {
    "role": "browser_use"
  }
}
```

### 2. Take a Screenshot

```json
{
  "query": "Take a full-page screenshot of https://example.com",
  "session_id": "screenshot-001",
  "context": {
    "role": "browser_use"
  }
}
```

### 3. Fill and Submit a Form

```json
{
  "query": "Go to https://example.com/contact, fill in name as 'John Doe' and email as 'john@example.com', then submit the form",
  "session_id": "form-001",
  "context": {
    "role": "browser_use"
  }
}
```

### 4. Extract Specific Data

```json
{
  "query": "Navigate to https://example.com/products and extract all product names and prices",
  "session_id": "scrape-001",
  "context": {
    "role": "browser_use"
  }
}
```

### 5. Multi-Step Interaction

```json
{
  "query": "Go to https://example.com, click the 'Learn More' button, wait for the page to load, then extract the content from the main article",
  "session_id": "multistep-001",
  "context": {
    "role": "browser_use"
  }
}
```

## Frontend Integration Example

### React/TypeScript

```typescript
interface BrowserTaskRequest {
  query: string;
  session_id: string;
  context: {
    role: 'browser_use';
  };
}

interface TaskResponse {
  task_id: string;
  status: string;
  result?: string;
  metadata?: {
    iterations: number;
    actions: number;
  };
}

async function submitBrowserTask(query: string): Promise<string> {
  const response = await fetch('/api/v1/tasks', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': API_KEY,
    },
    body: JSON.stringify({
      query,
      session_id: `browser-${Date.now()}`,
      context: { role: 'browser_use' },
    }),
  });

  const data = await response.json();
  return data.task_id;
}

async function pollTaskStatus(taskId: string): Promise<TaskResponse> {
  const response = await fetch(`/api/v1/tasks/${taskId}`, {
    headers: { 'X-API-Key': API_KEY },
  });
  return response.json();
}

// Usage with SSE for real-time updates
function streamTaskEvents(taskId: string, onEvent: (event: any) => void) {
  const eventSource = new EventSource(
    `/api/v1/tasks/${taskId}/events?api_key=${API_KEY}`
  );

  eventSource.onmessage = (event) => {
    const data = JSON.parse(event.data);
    onEvent(data);

    if (data.type === 'WORKFLOW_COMPLETED' || data.type === 'STREAM_END') {
      eventSource.close();
    }
  };

  return eventSource;
}
```

## Best Practices

1. **Use Descriptive Queries**: Be specific about what you want the browser to do
   - Good: "Navigate to https://example.com, click the login button, and extract the form fields"
   - Bad: "Check the website"

2. **Handle Timeouts**: Browser operations can take time. Set appropriate timeouts (30-60s recommended)

3. **Session IDs**: Use unique session IDs for independent browser sessions

4. **Error Handling**: Check task status for failures and handle appropriately

5. **SSE for Real-time**: Use SSE streaming for real-time progress updates in UI

## Response Metadata

The `metadata` field in completed tasks includes:

| Field | Description |
|-------|-------------|
| `iterations` | Number of React loop iterations |
| `actions` | Total tool calls made |
| `observations` | Tool results observed |
| `thoughts` | Reasoning steps |
| `model` | LLM model used |
| `cost_usd` | Estimated API cost |

## Limitations

- **Session TTL**: Browser sessions expire after 5 minutes of inactivity
- **Max Sessions**: 50 concurrent sessions per service instance
- **Page Rendering**: JavaScript-heavy pages may need `browser(action="wait")` for dynamic content
- **Authentication**: For sites requiring login, include full login flow in the query
