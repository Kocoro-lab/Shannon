# Changelog

All notable changes to the Shannon Python SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
