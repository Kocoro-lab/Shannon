# Changelog

All notable changes to the Shannon Python SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2025-12-15

### Added

#### Schedule Management
- **`create_schedule()`** - Create scheduled tasks with cron expressions
- **`get_schedule()`** - Get schedule details
- **`list_schedules()`** - List all schedules with pagination and status filter
- **`update_schedule()`** - Update schedule configuration
- **`pause_schedule()`** - Pause a schedule
- **`resume_schedule()`** - Resume a paused schedule
- **`delete_schedule()`** - Delete a schedule
- **`get_schedule_runs()`** - Get execution history for a schedule

#### Models
- **`Schedule`** - Full schedule details including cron, context, budget, run history
- **`ScheduleSummary`** - Summary for list operations
- **`ScheduleRun`** - Individual execution record with status, tokens, cost
- **`ScheduleStatus`** - Enum (ACTIVE, PAUSED, DELETED)

#### CLI Commands
- **`schedule-create <name> <cron> <query>`** - Create a scheduled task
  - Example: `shannon schedule-create "Daily Report" "0 9 * * *" "Summarize daily metrics" --force-research`
- **`schedule-list`** - List all schedules
- **`schedule-get <id>`** - Get schedule details
- **`schedule-update <id>`** - Update schedule configuration
- **`schedule-pause <id>`** - Pause a schedule
- **`schedule-resume <id>`** - Resume a paused schedule
- **`schedule-delete <id>`** - Delete a schedule
- **`schedule-runs <id>`** - View execution history

### Changed
- Version bumped from 0.4.2 to 0.5.0

### Migration Guide

**Creating a scheduled task:**
```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080")

# Create a daily research task at 9am UTC
result = client.create_schedule(
    name="Daily AI News",
    cron_expression="0 9 * * *",
    task_query="Summarize the latest AI research news",
    task_context={"force_research": "true", "research_strategy": "quick"},
    timezone="UTC",
    max_budget_per_run_usd=0.50,
)
print(f"Schedule created: {result['schedule_id']}")

# List all schedules
schedules, total = client.list_schedules()
for s in schedules:
    print(f"{s.name}: {s.cron_expression} ({s.status})")

# View execution history
runs, _ = client.get_schedule_runs(result['schedule_id'])
for run in runs:
    print(f"{run.triggered_at}: {run.status} - ${run.total_cost_usd:.4f}")

# Pause/resume schedule
client.pause_schedule(result['schedule_id'], reason="Maintenance")
client.resume_schedule(result['schedule_id'])
```

**CLI usage:**
```bash
# Create a scheduled task
shannon schedule-create "Daily Report" "0 9 * * 1-5" "Generate daily metrics summary" \
    --force-research --research-strategy quick --budget 0.25

# List schedules
shannon schedule-list

# View schedule details
shannon schedule-get <schedule-id>

# Pause/resume
shannon schedule-pause <schedule-id> --reason "Holiday"
shannon schedule-resume <schedule-id>

# View run history
shannon schedule-runs <schedule-id>
```

---

## [0.4.2] - 2025-01-14

### Fixed
- Empty response body handling in pause/resume operations (now defaults to True on 202 status)
- Timestamp parsing error logging with specific exceptions instead of silent failures

## [0.4.1] - 2025-01-14

### Fixed
- Minor bug fixes and improvements to control signal stability

## [0.4.0] - 2025-01-14

### Added

#### Task Control Features
- **`pause_task(task_id, reason=None)`** - Pause running tasks at safe checkpoints with optional reason
- **`resume_task(task_id, reason=None)`** - Resume previously paused tasks with optional reason
- **`get_control_state(task_id)`** - Get detailed pause/cancel control state including timestamps and metadata
- **`ControlState` model** - Control state data class with comprehensive pause/cancel tracking:
  - `is_paused` - Current pause status
  - `is_cancelled` - Current cancellation status
  - `paused_at` - Timestamp when task was paused
  - `pause_reason` - Reason provided for pause
  - `paused_by` - User/system that paused the task
  - `cancel_reason` - Reason provided for cancellation
  - `cancelled_by` - User/system that cancelled the task

#### CLI Control Commands
- **`pause <task_id> [--reason TEXT]`** - Pause task from CLI with optional reason
  - Example: `shannon pause task-123 --reason "Hold for review"`
- **`resume <task_id> [--reason TEXT]`** - Resume task from CLI with optional reason
  - Example: `shannon resume task-123 --reason "Ready to continue"`
- **`control-state <task_id>`** - View task control state with formatted output
  - Example: `shannon control-state task-123`

#### API Integration
- Pause endpoint: `POST /api/v1/tasks/{id}/pause`
- Resume endpoint: `POST /api/v1/tasks/{id}/resume`
- Control state endpoint: `GET /api/v1/tasks/{id}/control-state`
- HTTP 200/202 responses indicate successful pause/resume requests
- Pause takes effect at next workflow checkpoint (pre_synthesis, post_iteration, etc.)

### Changed
- README.md updated with control signal examples and CLI commands
- Version bumped from 0.3.0 to 0.4.0
- `ControlState` exported in public API via `__init__.py`

### Migration Guide

**Using control signals:**
```python
from shannon import ShannonClient

client = ShannonClient(base_url="http://localhost:8080", api_key="your-api-key")

# Pause a running task
client.pause_task("task-123", reason="User requested pause")

# Check control state
state = client.get_control_state("task-123")
if state.is_paused:
    print(f"Paused at: {state.paused_at}")
    print(f"Reason: {state.pause_reason}")

# Resume the task
client.resume_task("task-123", reason="User resumed")
```

**CLI usage:**
```bash
# Pause task
python -m shannon.cli --base-url http://localhost:8080 pause task-123 --reason "Hold for review"

# Check state
python -m shannon.cli --base-url http://localhost:8080 control-state task-123

# Resume task
python -m shannon.cli --base-url http://localhost:8080 resume task-123
```

---

## [0.3.0] - 2025-01-04

### Added

#### TaskStatus Model Enhancements
- **`workflow_id`** - Workflow identifier for streaming and debugging
- **`created_at`** - Task creation timestamp
- **`updated_at`** - Last update timestamp
- **`query`** - Original task query text
- **`session_id`** - Associated session identifier
- **`mode`** - Execution mode (simple/standard/complex/supervisor)
- **`context`** - Task context dictionary (research settings, etc.)
- **`model_used`** - Model identifier used for execution (e.g., "gpt-5-nano-2025-08-07")
- **`provider`** - Provider name (openai, anthropic, google, etc.)
- **`usage`** - Detailed token and cost breakdown dictionary
- **`metadata`** - Task metadata including citations and other execution data

#### Session Model Enhancements
- **`title`** - User-editable session title
- **`context`** - Session context dictionary
- **`token_budget`** - Token budget limit for session
- **`task_count`** - Number of tasks in session
- **`expires_at`** - Session expiration timestamp
- **`is_research_session`** - Flag indicating research workflow usage
- **`research_strategy`** - Research strategy used (quick/standard/deep/academic)

#### SessionSummary Model Enhancements
- **Budget Tracking:** `token_budget`, `budget_remaining`, `budget_utilization`, `is_near_budget_limit`
- **Activity Tracking:** `last_activity_at`, `is_active`, `expires_at`
- **Success Metrics:** `successful_tasks`, `failed_tasks`, `success_rate`
- **Cost Analytics:** `total_cost_usd`, `average_cost_per_task`
- **UI Features:** `title`, `latest_task_query`, `latest_task_status`
- **Research Detection:** `is_research_session`, `first_task_mode`

#### Client Parser Updates
- Updated `get_status()` to parse all new TaskStatus fields
- Updated `list_sessions()` to parse all new SessionSummary fields
- Updated `get_session()` to parse all new Session fields
- Enhanced timestamp parsing with better error handling

#### Documentation
- New "Usage and Cost Tracking" section with comprehensive examples
- New "Session Management" section documenting titles, budgets, and metrics
- Added examples for budget monitoring and cost analytics
- Added examples for session activity tracking

### Deprecated
- **`TaskStatus.metrics`** - Use `TaskStatus.usage` instead. Still supported for backward compatibility.

### Changed
- README.md updated with new features and usage examples
- Version bumped from 0.2.2 to 0.3.0

### Migration Guide

**Accessing new task metadata:**
```python
status = client.get_status(task_id)

# New in v0.3.0
print(f"Model: {status.model_used}")
print(f"Provider: {status.provider}")
if status.usage:
    print(f"Cost: ${status.usage.get('cost_usd', 0):.6f}")
    print(f"Tokens: {status.usage.get('total_tokens')}")
```

**Using session features:**
```python
# Set session title
client.update_session_title(session_id, "Q4 Analysis")

# Monitor session metrics
sessions, _ = client.list_sessions()
for s in sessions:
    print(f"{s.title}: {s.success_rate:.1%} success")
    if s.is_near_budget_limit:
        print("⚠️  Near budget limit!")
```

---

## [0.1.0a2] - 2025-01-07

### Fixed
- Added missing `wait()` method to both `AsyncShannonClient` and `ShannonClient` classes
- Fixed CLI error handling to show clean error messages instead of Python stack traces
- Fixed `TaskHandle` client reference in sync wrapper to use sync client for convenience methods

### Verified
- Context overrides including `system_prompt` parameter
- Template support (`template_name`, `template_version`, `disable_ai`)
- Custom labels for workflow routing and priority

## [0.1.0a1] - 2025-01-06

### Added
- Initial alpha release of Shannon Python SDK
- Support for task submission, status checking, and cancellation
- Streaming support (gRPC and SSE with auto-fallback)
- Session management for multi-turn conversations
- Approval workflow support
- Template-based task execution
- Custom labels and context overrides

## [0.2.1] - 2025-11-06

### Fixed
- WebSocket streaming compatibility with websockets 15.x (changed `extra_headers` to `additional_headers` parameter)

## [0.2.0] - 2025-11-05

### Added
- Model selection parameters to both async and sync clients:
  - `model_tier` (small|medium|large)
  - `model_override`
  - `provider_override`
  - `mode` (simple|standard|complex|supervisor)
- CLI flags for model selection (`--model-tier`, `--model-override`, `--provider-override`, `--mode`).
- Completed `EventType` enum with additional event types (e.g., `AGENT_THINKING`, `PROGRESS`, `DATA_PROCESSING`, `TEAM_STATUS`, etc.).
- Optional WebSocket streaming helper: `AsyncShannonClient.stream_ws()` and `ShannonClient.stream_ws()` (requires `websockets`).

### Changed
- Type hints: use `Literal` for `model_tier` and `mode` for better editor support.
