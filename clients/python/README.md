# Shannon Python SDK

Python client for Shannon multi-agent AI platform.

**Version:** 0.2.0a1 (Alpha)

## Installation

```bash
# Development installation (from this directory)
pip install -e .

# With dev dependencies
pip install -e ".[dev]"
```

## Quick Start

```python
from shannon import ShannonClient

# Initialize client (HTTP-only)
client = ShannonClient(
    base_url="http://localhost:8080",
    api_key="your-api-key"  # or use bearer_token
)

# Submit a task
handle = client.submit_task(
    "Analyze market trends for Q4 2024",
    session_id="my-session",
)

print(f"Task submitted: {handle.task_id}")
print(f"Workflow ID: {handle.workflow_id}")

# Get status
status = client.get_status(handle.task_id)
print(f"Status: {status.status}")
print(f"Progress: {status.progress:.1%}")

# Cancel if needed
# client.cancel(handle.task_id, reason="User requested")

client.close()
```

## CLI Examples

```bash
# Submit a task and wait for completion
python -m shannon.cli --base-url http://localhost:8080 submit "What is 2+2?" --wait

# List sessions (first 5)
python -m shannon.cli --base-url http://localhost:8080 session-list --limit 5
```

## CLI Commands

Global flags:
- `--base-url` (default: `http://localhost:8080`)
- `--api-key` or `--bearer-token`

| Command | Arguments | Description | HTTP Endpoint |
|--------|-----------|-------------|---------------|
| `submit` | `query` `--session-id` `--wait` `--idempotency-key` `--traceparent` | Submit a task (optionally wait) | `POST /api/v1/tasks` |
| `status` | `task_id` | Get task status | `GET /api/v1/tasks/{id}` |
| `cancel` | `task_id` `--reason` | Cancel a running or queued task | `POST /api/v1/tasks/{id}/cancel` |
| `stream` | `workflow_id` `--types=a,b,c` `--traceparent` | Stream events via SSE (optionally filter types) | `GET /api/v1/stream/sse?workflow_id=...` |
| `approve` | `approval_id` `workflow_id` `--approve/--reject` `--feedback` | Submit approval decision | `POST /api/v1/approvals/decision` |
| `session-list` | `--limit` `--offset` | List sessions | `GET /api/v1/sessions` |
| `session-get` | `session_id` `--no-history` | Get session details (optionally fetch history) | `GET /api/v1/sessions/{id}` (+ `GET /api/v1/sessions/{id}/history`) |
| `session-title` | `session_id` `title` | Update session title | `PATCH /api/v1/sessions/{id}` |
| `session-delete` | `session_id` | Delete a session | `DELETE /api/v1/sessions/{id}` |

One‑line examples:

- `submit`: `python -m shannon.cli --base-url http://localhost:8080 submit "Analyze quarterly revenue" --session-id my-session --wait`
- `status`: `python -m shannon.cli --base-url http://localhost:8080 status task-123`
- `cancel`: `python -m shannon.cli --base-url http://localhost:8080 cancel task-123 --reason "No longer needed"`
- `stream`: `python -m shannon.cli --base-url http://localhost:8080 stream workflow-123 --types WORKFLOW_STARTED,LLM_OUTPUT,WORKFLOW_COMPLETED`
- `approve`: `python -m shannon.cli --base-url http://localhost:8080 approve approval-uuid workflow-uuid --approve --feedback "Looks good"`
- `session-list`: `python -m shannon.cli --base-url http://localhost:8080 session-list --limit 10 --offset 0`
- `session-get`: `python -m shannon.cli --base-url http://localhost:8080 session-get my-session`
- `session-title`: `python -m shannon.cli --base-url http://localhost:8080 session-title my-session "My Session Title"`
- `session-delete`: `python -m shannon.cli --base-url http://localhost:8080 session-delete my-session`

## Async Usage

```python
import asyncio
from shannon import AsyncShannonClient

async def main():
    async with AsyncShannonClient(
        base_url="http://localhost:8080",
        api_key="your-api-key"
    ) as client:
        handle = await client.submit_task("What is 2+2?")
        final = await client.wait(handle.task_id)
        print(f"Result: {final.result}")

asyncio.run(main())
```

## Features

- ✅ HTTP-only client using httpx
- ✅ Task submission, status, wait, cancel
- ✅ Event streaming via HTTP SSE (resume + filtering)
- ✅ Approval decision endpoint
- ✅ Session endpoints: list/get/history/events/update title/delete
- ✅ CLI tool (submit, status, stream, approve, sessions)
- ✅ Async-first design with sync wrapper
- ✅ Type-safe enums (EventType, TaskStatusEnum)
- ✅ Error mapping for common HTTP codes

## Examples

The SDK includes comprehensive examples demonstrating key features:

- **`simple_task.py`** - Basic task submission and status polling
- **`simple_streaming.py`** - Event streaming with filtering
- **`streaming_with_approvals.py`** - Approval workflow handling
- **`workflow_routing.py`** - Using labels for workflow routing and task categorization
- **`session_continuity.py`** - Multi-turn conversations with session management
- **`template_usage.py`** - Template-based task execution with versioning

Run any example:
```bash
cd clients/python
python examples/simple_task.py
```

## Development

```bash
# Run tests
make test

# Lint
make lint

# Format
make format
```

## Project Structure

```
clients/python/
├── src/shannon/
│   ├── __init__.py      # Public API
│   ├── client.py        # AsyncShannonClient, ShannonClient
│   ├── models.py        # Data models (TaskHandle, TaskStatus, Event, etc.)
│   └── errors.py        # Exception hierarchy
├── tests/               # Integration tests
├── examples/            # Usage examples
└── pyproject.toml       # Package metadata
```

## License

MIT
## Security

- Do not hardcode credentials in code. Prefer environment variables (`SHANNON_API_KEY`) or a secrets manager.
- Use `--bearer-token` for short-lived tokens where possible.
- Rotate API keys regularly and scope access minimally.
- Avoid logging sensitive headers (e.g., `Authorization`, `X-API-Key`, `traceparent`).

## Rate Limits

- The Gateway may enforce rate limits. Use `Idempotency-Key` for safe retries of `submit` calls.
- Backoff on 429/5xx responses and consider adding application-level retry logic if needed.
