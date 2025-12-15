# Shannon Python SDK

Python client for Shannon multi-agent AI platform.

**Version:** 0.5.0

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

# Pause / resume controls
# client.pause_task(handle.task_id, reason="User requested pause")
# client.resume_task(handle.task_id, reason="User resumed")

client.close()
```

### Model Selection (cost/control)

```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080", api_key="your-api-key")

# Choose by tier (small|medium|large), or override model/provider explicitly
handle = client.submit_task(
    "Summarize this document",
    model_tier="small",
    # model_override="gpt-5-nano-2025-08-07",
    # provider_override="openai",
    # mode="standard",  # simple|standard|complex|supervisor
)

final = client.wait(handle.task_id)
print(final.result)

client.close()
```

## CLI Examples

```bash
# Submit a task and wait for completion
python -m shannon.cli --base-url http://localhost:8080 submit "What is 2+2?" --wait

# Strategy presets
python -m shannon.cli --base-url http://localhost:8080 \
  submit "Latest quantum computing breakthroughs" \
  --research-strategy deep --enable-verification

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
|          | `--model-tier` `--model-override` `--provider-override` `--mode` | Model selection and routing | |
| `status` | `task_id` | Get task status | `GET /api/v1/tasks/{id}` |
| `cancel` | `task_id` `--reason` | Cancel a running or queued task | `POST /api/v1/tasks/{id}/cancel` |
| `pause` | `task_id` `--reason` | Pause a running task at safe checkpoints | `POST /api/v1/tasks/{id}/pause` |
| `resume` | `task_id` `--reason` | Resume a previously paused task | `POST /api/v1/tasks/{id}/resume` |
| `control-state` | `task_id` | Get pause/cancel control state | `GET /api/v1/tasks/{id}/control-state` |
| `stream` | `workflow_id` `--types=a,b,c` `--traceparent` | Stream events via SSE (optionally filter types) | `GET /api/v1/stream/sse?workflow_id=...` |
| `approve` | `approval_id` `workflow_id` `--approve/--reject` `--feedback` | Submit approval decision | `POST /api/v1/approvals/decision` |
| `session-list` | `--limit` `--offset` | List sessions | `GET /api/v1/sessions` |
| `session-get` | `session_id` `--no-history` | Get session details (optionally fetch history) | `GET /api/v1/sessions/{id}` (+ `GET /api/v1/sessions/{id}/history`) |
| `session-title` | `session_id` `title` | Update session title | `PATCH /api/v1/sessions/{id}` |
| `session-delete` | `session_id` | Delete a session | `DELETE /api/v1/sessions/{id}` |
| `schedule-create` | `name` `cron` `query` `--force-research` `--research-strategy` `--budget` `--timeout` | Create scheduled task | `POST /api/v1/schedules` |
| `schedule-list` | `--page` `--page-size` `--status` | List schedules | `GET /api/v1/schedules` |
| `schedule-get` | `schedule_id` | Get schedule details | `GET /api/v1/schedules/{id}` |
| `schedule-update` | `schedule_id` `--name` `--cron` `--query` `--clear-context` | Update schedule | `PUT /api/v1/schedules/{id}` |
| `schedule-pause` | `schedule_id` `--reason` | Pause a schedule | `POST /api/v1/schedules/{id}/pause` |
| `schedule-resume` | `schedule_id` `--reason` | Resume a paused schedule | `POST /api/v1/schedules/{id}/resume` |
| `schedule-delete` | `schedule_id` | Delete a schedule | `DELETE /api/v1/schedules/{id}` |
| `schedule-runs` | `schedule_id` `--page` `--page-size` | View schedule execution history | `GET /api/v1/schedules/{id}/runs` |

One‑line examples:

- `submit`: `python -m shannon.cli --base-url http://localhost:8080 submit "Analyze quarterly revenue" --session-id my-session --wait`
- `submit` (with model selection): `python -m shannon.cli --base-url http://localhost:8080 submit "Summarize" --model-tier small --mode simple`
- `status`: `python -m shannon.cli --base-url http://localhost:8080 status task-123`
- `cancel`: `python -m shannon.cli --base-url http://localhost:8080 cancel task-123 --reason "No longer needed"`
- `pause`: `python -m shannon.cli --base-url http://localhost:8080 pause task-123 --reason "Hold for review"`
- `resume`: `python -m shannon.cli --base-url http://localhost:8080 resume task-123 --reason "Ready to continue"`
- `control-state`: `python -m shannon.cli --base-url http://localhost:8080 control-state task-123`
- `stream`: `python -m shannon.cli --base-url http://localhost:8080 stream workflow-123 --types WORKFLOW_STARTED,LLM_OUTPUT,WORKFLOW_COMPLETED`
- `approve`: `python -m shannon.cli --base-url http://localhost:8080 approve approval-uuid workflow-uuid --approve --feedback "Looks good"`
- `session-list`: `python -m shannon.cli --base-url http://localhost:8080 session-list --limit 10 --offset 0`
- `session-get`: `python -m shannon.cli --base-url http://localhost:8080 session-get my-session`
- `session-title`: `python -m shannon.cli --base-url http://localhost:8080 session-title my-session "My Session Title"`
- `session-delete`: `python -m shannon.cli --base-url http://localhost:8080 session-delete my-session`
- `schedule-create`: `python -m shannon.cli --base-url http://localhost:8080 schedule-create "Daily Report" "0 9 * * 1-5" "Summarize daily metrics" --force-research`
- `schedule-list`: `python -m shannon.cli --base-url http://localhost:8080 schedule-list --status ACTIVE`
- `schedule-get`: `python -m shannon.cli --base-url http://localhost:8080 schedule-get schedule-123`
- `schedule-pause`: `python -m shannon.cli --base-url http://localhost:8080 schedule-pause schedule-123 --reason "Maintenance"`
- `schedule-resume`: `python -m shannon.cli --base-url http://localhost:8080 schedule-resume schedule-123`
- `schedule-runs`: `python -m shannon.cli --base-url http://localhost:8080 schedule-runs schedule-123`

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

### Async streaming (tip)

When streaming events asynchronously, break out of the `async for` before calling other client methods (don’t `await` inside the loop):

```python
import asyncio
from shannon import AsyncShannonClient, EventType

async def main():
    async with AsyncShannonClient(base_url="http://localhost:8080") as client:
        h = await client.submit_task("Complex analysis")
        async for e in client.stream(h.workflow_id, types=[EventType.LLM_OUTPUT, EventType.WORKFLOW_COMPLETED]):
            if e.type == EventType.WORKFLOW_COMPLETED:
                break
        final = await client.wait(h.task_id)
        print(f"Result: {final.result}")

asyncio.run(main())
```

## Features

- ✅ HTTP-only client using httpx
- ✅ Task submission, status, wait, cancel
- ✅ Task control: pause, resume, control-state
- ✅ Schedule management: create, list, update, pause, resume, delete, view runs
- ✅ Event streaming via HTTP SSE (resume + filtering)
- ✅ Optional WebSocket streaming helper (`client.stream_ws`) — requires `pip install websockets`
- ✅ Approval decision endpoint
- ✅ Session endpoints: list/get/history/events/update title/delete
- ✅ CLI tool (submit, status, stream, approve, sessions, schedules)
- ✅ Async-first design with sync wrapper
- ✅ Type-safe enums (EventType, TaskStatusEnum, ScheduleStatus)
- ✅ Error mapping for common HTTP codes

## Usage and Cost Tracking

### Task-Level Usage

Token usage and cost information is now available directly from `get_status()` in the `usage` field:

```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080", api_key="your-api-key")

# Submit and get status
handle = client.submit_task("Analyze quarterly revenue trends")
status = client.wait(handle.task_id)

# Access usage metadata (new in v0.3.0)
print(f"Model used: {status.model_used}")
print(f"Provider: {status.provider}")
if status.usage:
    print(f"Total tokens: {status.usage.get('total_tokens')}")
    print(f"Prompt tokens: {status.usage.get('prompt_tokens')}")
    print(f"Completion tokens: {status.usage.get('completion_tokens')}")
    print(f"Cost: ${status.usage.get('cost_usd', 0):.6f}")

# Access task metadata (citations, etc.)
if status.metadata:
    print(f"Metadata: {status.metadata}")
```

### Aggregate Usage from Task Lists

```python
tasks, total = client.list_tasks(limit=10)

for t in tasks:
    tu = t.total_token_usage
    if tu:
        print(f"{t.task_id}: total={tu.total_tokens}, cost=${tu.cost_usd:.6f}")
```

### Session-Level Cost Tracking

Sessions now track comprehensive budget and cost metrics:

```python
# Get session with budget tracking
session = client.get_session("my-session-id")
print(f"Total cost: ${session.total_cost_usd:.4f}")
print(f"Total tokens: {session.total_tokens_used}")
print(f"Token budget: {session.token_budget}")
print(f"Task count: {session.task_count}")

# List sessions with metrics
sessions, count = client.list_sessions(limit=10)
for s in sessions:
    print(f"\nSession: {s.title or s.session_id}")
    print(f"  Tasks: {s.successful_tasks} succeeded, {s.failed_tasks} failed")
    print(f"  Success rate: {s.success_rate:.1%}")
    print(f"  Total cost: ${s.total_cost_usd:.4f}")
    print(f"  Avg cost/task: ${s.average_cost_per_task:.4f}")
    if s.token_budget:
        print(f"  Budget: {s.budget_utilization:.1%} used ({s.budget_remaining} remaining)")
        if s.is_near_budget_limit:
            print(f"  ⚠️  Near budget limit!")
```

## Session Management (New in v0.3.0)

### Session Titles

Sessions now support user-editable titles:

```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080", api_key="your-api-key")

# Create tasks in a session
handle = client.submit_task("Analyze Q4 revenue", session_id="quarterly-review")

# Update session title for better organization
client.update_session_title("quarterly-review", "Q4 2024 Financial Analysis")

# Get session with title
session = client.get_session("quarterly-review")
print(f"Session: {session.title}")
```

### Session Activity Tracking

Monitor session health and activity:

```python
sessions, _ = client.list_sessions(limit=20)

for s in sessions:
    if s.is_active:
        print(f"✓ Active: {s.title or s.session_id}")
        if s.last_activity_at:
            print(f"  Last used: {s.last_activity_at}")
        print(f"  Latest task: {s.latest_task_query}")
        print(f"  Status: {s.latest_task_status}")
```

### Research Session Detection

The SDK automatically detects research sessions based on task patterns:

```python
session = client.get_session("my-session")
if session.is_research_session:
    print(f"Research session using '{session.research_strategy}' strategy")
```

## Schedule Management (New in v0.5.0)

Create and manage scheduled tasks that run automatically on a cron schedule.

### Creating Scheduled Tasks

```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080", api_key="your-api-key")

# Create a daily research task at 9am UTC on weekdays
result = client.create_schedule(
    name="Daily AI News Summary",
    cron_expression="0 9 * * 1-5",  # Mon-Fri at 9am
    task_query="Summarize the latest developments in AI research",
    description="Daily automated research digest",
    timezone="UTC",
    task_context={
        "force_research": "true",
        "research_strategy": "quick",
    },
    max_budget_per_run_usd=0.50,
    timeout_seconds=300,
)
print(f"Schedule created: {result['schedule_id']}")
print(f"Next run: {result['next_run_at']}")
```

### Listing and Managing Schedules

```python
# List all active schedules
schedules, total = client.list_schedules(status="ACTIVE")
print(f"Found {total} active schedules")

for s in schedules:
    print(f"  {s.name}: {s.cron_expression} (next: {s.next_run_at})")

# Get schedule details
schedule = client.get_schedule("schedule-id")
print(f"Schedule: {schedule.name}")
print(f"Status: {schedule.status}")
print(f"Runs: {schedule.total_runs} total, {schedule.successful_runs} succeeded, {schedule.failed_runs} failed")

# Update schedule
client.update_schedule(
    "schedule-id",
    cron_expression="0 10 * * 1-5",  # Change to 10am
    max_budget_per_run_usd=1.00,
)

# Pause/resume
client.pause_schedule("schedule-id", reason="Holiday break")
client.resume_schedule("schedule-id", reason="Back from holiday")

# Delete
client.delete_schedule("schedule-id")
```

### Viewing Execution History

```python
# Get execution history for a schedule
runs, total = client.get_schedule_runs("schedule-id", page=1, page_size=10)

print(f"Last {len(runs)} runs (of {total} total):")
for run in runs:
    status_icon = "✓" if run.status == "COMPLETED" else "✗"
    print(f"  {status_icon} {run.triggered_at}: {run.status}")
    print(f"      Tokens: {run.total_tokens}, Cost: ${run.total_cost_usd:.4f}")
    if run.error_message:
        print(f"      Error: {run.error_message}")
```

### Cron Expression Examples

| Expression | Description |
|------------|-------------|
| `0 9 * * *` | Every day at 9:00 AM |
| `0 9 * * 1-5` | Weekdays at 9:00 AM |
| `0 */6 * * *` | Every 6 hours |
| `0 9 1 * *` | First day of each month at 9:00 AM |
| `30 8 * * 1` | Every Monday at 8:30 AM |

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

### Strategy Presets (Programmatic)

```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080")
handle = client.submit_task(
    "Compare LangChain and AutoGen frameworks",
    context={
        "research_strategy": "deep",
        "react_max_iterations": 6,
        "enable_verification": True,
    },
)
final = client.wait(handle.task_id)
print(final.result)
client.close()
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

## Changelog

### Version 0.5.0 (2025-12-15)

**New Features:**
- **Schedule Management** - Full CRUD for scheduled tasks with cron expressions
  - `create_schedule()`, `get_schedule()`, `list_schedules()`, `update_schedule()`
  - `pause_schedule()`, `resume_schedule()`, `delete_schedule()`
  - `get_schedule_runs()` - View execution history
- **Schedule Models** - `Schedule`, `ScheduleSummary`, `ScheduleRun`, `ScheduleStatus`
- **CLI Commands** - `schedule-create`, `schedule-list`, `schedule-get`, `schedule-update`, `schedule-pause`, `schedule-resume`, `schedule-delete`, `schedule-runs`

### Version 0.4.0 (2025-01-14)

**New Features:**
- **Task Control** - Pause, resume, and get control state for running tasks
  - `pause_task()`, `resume_task()`, `get_control_state()`
- **ControlState Model** - Comprehensive pause/cancel tracking
- **CLI Commands** - `pause`, `resume`, `control-state`

### Version 0.3.0 (2025-01-04)

**Breaking Changes:**
- None (all changes are backward-compatible additions)

**New Features:**
- **TaskStatus model expanded** with backend fields:
  - `workflow_id` - Workflow identifier for streaming/debugging
  - `created_at` / `updated_at` - Task lifecycle timestamps
  - `query` - Original task query
  - `session_id` - Associated session
  - `mode` - Execution mode (simple/standard/complex/supervisor)
  - `context` - Task context (research settings, etc.)
  - `model_used` - Model used for execution
  - `provider` - Provider used (openai, anthropic, etc.)
  - `usage` - Detailed token/cost breakdown (replaces deprecated `metrics`)
  - `metadata` - Task metadata including citations

- **Session models enhanced** with comprehensive tracking:
  - `title` - User-editable session titles
  - `token_budget` / `budget_remaining` / `budget_utilization` - Budget tracking
  - `last_activity_at` / `is_active` - Activity monitoring
  - `successful_tasks` / `failed_tasks` / `success_rate` - Success metrics
  - `total_cost_usd` / `average_cost_per_task` - Cost analytics
  - `is_near_budget_limit` - Budget warning flag
  - `latest_task_query` / `latest_task_status` - Latest task preview
  - `is_research_session` / `research_strategy` - Research detection
  - `expires_at` - Session expiration timestamp

**Deprecations:**
- `TaskStatus.metrics` - Use `TaskStatus.usage` instead (still supported for backward compatibility)

### Version 0.2.2 (Previous)
- Session endpoints
- CLI improvements
- Streaming enhancements

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
