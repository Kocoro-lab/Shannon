# Shannon Python SDK

Python client for Shannon multi-agent AI platform.

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

# Initialize client
client = ShannonClient(
    grpc_endpoint="localhost:50052",
    http_endpoint="http://localhost:8081",
    api_key="your-api-key"  # or use bearer_token
)

# Submit a task
handle = client.submit_task(
    "Analyze market trends for Q4 2024",
    session_id="my-session",
    user_id="alice"
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

## Async Usage

```python
import asyncio
from shannon import AsyncShannonClient

async def main():
    async with AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="your-api-key"
    ) as client:
        # Submit task
        handle = await client.submit_task(
            "What is 2+2?",
            user_id="test-user"
        )

        # Poll for completion
        while True:
            status = await client.get_status(handle.task_id)
            if status.status in ["COMPLETED", "FAILED", "CANCELLED"]:
                break
            await asyncio.sleep(1)

        print(f"Result: {status.result}")

asyncio.run(main())
```

## Features

- ✅ Task submission with metadata and context
- ✅ Task status polling with detailed metrics
- ✅ Task cancellation
- ✅ Event streaming (gRPC + SSE fallback with auto-reconnect)
- ✅ Approval workflows (approve, get_pending_approvals)
- ✅ Session management (7 RPCs for multi-turn conversations)
- ✅ Template support (pass template names to server)
- ✅ CLI tool (13 commands: submit, status, stream, approve, sessions, etc.)
- ✅ Async-first design with sync wrapper
- ✅ Type-safe enums (EventType, TaskStatusEnum)
- ✅ Comprehensive error handling

## Development

```bash
# Generate proto stubs
make proto

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
│   ├── errors.py        # Exception hierarchy
│   └── generated/       # Generated proto stubs
├── tests/               # Integration tests
├── examples/            # Usage examples
└── pyproject.toml       # Package metadata
```

## License

MIT
