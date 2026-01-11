# Shannon Embedded API - Feature Parity Specification

**Version**: 1.0  
**Created**: 2026-01-10  
**Status**: Draft  
**Target**: 100% Feature Parity with Go Gateway + Go Orchestrator + Python LLM Service

## Executive Summary

This specification defines complete feature parity requirements for the Shannon Embedded API (Rust + Durable workflows) to match the capabilities of the cloud deployment stack (Go Gateway + Go Orchestrator + Python LLM Service + Temporal workflows).

**Scope**: All features documented in the `docs/` directory must be implemented or have a documented equivalent in the embedded stack.

**Success Criteria**:
- 100% API endpoint coverage
- 100% streaming event type coverage  
- 100% workflow capability coverage
- 100% database operation coverage
- Integration tests verify all features work end-to-end

---

## 1. API Endpoints Feature Matrix

### 1.1 Task Submission & Management

| Feature | Go Gateway Endpoint | Embedded API Status | Implementation Notes |
|---------|---------------------|---------------------|---------------------|
| Submit task | `POST /api/v1/tasks` | ✅ Implemented | Shannon API |
| Submit with stream URL | `POST /api/v1/tasks/stream` | ❌ Missing | Returns 201 with stream URL |
| Get task status | `GET /api/v1/tasks/{id}` | ✅ Implemented | Shannon API |
| Get task output | `GET /api/v1/tasks/{id}/output` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| Get task progress | `GET /api/v1/tasks/{id}/progress` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| List tasks | `GET /api/v1/tasks` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| Pause task | `POST /api/v1/tasks/{id}/pause` | ❌ Missing | Control signals |
| Resume task | `POST /api/v1/tasks/{id}/resume` | ❌ Missing | Control signals |
| Cancel task | `POST /api/v1/tasks/{id}/cancel` | ❌ Missing | Control signals |
| Get control state | `GET /api/v1/tasks/{id}/control-state` | ❌ Missing | **CRITICAL - IN PROGRESS** |

**Source**: `docs/task-submission-api.md`, `docs/control-signals.md`

### 1.2 Session Management

| Feature | Go Gateway Endpoint | Embedded API Status | Implementation Notes |
|---------|---------------------|---------------------|---------------------|
| List sessions | `GET /api/v1/sessions` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| Get session | `GET /api/v1/sessions/{id}` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| Get session history | `GET /api/v1/sessions/{id}/history` | ❌ Missing | **NEW ENDPOINT NEEDED** |
| Get session events | `GET /api/v1/sessions/{id}/events` | ❌ Missing | **NEW ENDPOINT NEEDED** |

**Source**: `docs/task-submission-api.md`

### 1.3 Streaming APIs

| Feature | Go Gateway Endpoint | Embedded API Status | Implementation Notes |
|---------|---------------------|---------------------|---------------------|
| SSE streaming | `GET /stream/sse` | ❌ Missing | **CRITICAL** |
| WebSocket streaming | `GET /stream/ws` | ❌ Missing | **CRITICAL** |
| gRPC streaming | `StreamingService.StreamTaskExecution` | ❌ Missing | Optional for embedded |

**Source**: `docs/streaming-api.md`

### 1.4 Schedule Management

| Feature | Go Gateway Endpoint | Embedded API Status | Implementation Notes |
|---------|---------------------|---------------------|---------------------|
| List schedules | `GET /api/v1/schedules` | ❌ Missing | Lower priority for embedded |
| Create schedule | `POST /api/v1/schedules` | ❌ Missing | Lower priority |
| Get schedule | `GET /api/v1/schedules/{id}` | ❌ Missing | Lower priority |
| Update schedule | `PUT /api/v1/schedules/{id}` | ❌ Missing | Lower priority |
| Pause schedule | `POST /api/v1/schedules/{id}/pause` | ❌ Missing | Lower priority |
| Resume schedule | `POST /api/v1/schedules/{id}/resume` | ❌ Missing | Lower priority |
| Delete schedule | `DELETE /api/v1/schedules/{id}` | ❌ Missing | Lower priority |
| Get schedule runs | `GET /api/v1/schedules/{id}/runs` | ❌ Missing | Lower priority |

**Source**: `docs/scheduled-tasks.md`

### 1.5 Health & Info Endpoints

| Feature | Go Gateway Endpoint | Embedded API Status | Implementation Notes |
|---------|---------------------|---------------------|---------------------|
| Health check | `GET /health` | ✅ Implemented | Shannon API |
| Readiness check | `GET /ready` | ✅ Implemented | Shannon API |
| Startup check | `GET /startup` | ✅ Implemented | Shannon API |
| Metrics | `GET /metrics` | ❌ Missing | Prometheus format |
| API info | `GET /api/v1/info` | ✅ Implemented | Shannon API |
| Capabilities | `GET /api/v1/capabilities` | ✅ Implemented | Shannon API |

**Source**: Various docs

### 1.6 Settings & Configuration (NEW)

| Feature | Endpoint | Embedded API Status | Implementation Notes |
|---------|----------|---------------------|---------------------|
| Get all settings | `GET /api/v1/settings` | ❌ Missing | **NEW - IN PROGRESS** |
| Get setting | `GET /api/v1/settings/{key}` | ❌ Missing | **NEW - IN PROGRESS** |
| Set setting | `POST /api/v1/settings` | ❌ Missing | **NEW - IN PROGRESS** |
| Delete setting | `DELETE /api/v1/settings/{key}` | ❌ Missing | **NEW - IN PROGRESS** |
| List API keys | `GET /api/v1/settings/api-keys` | ❌ Missing | **NEW - IN PROGRESS** |
| Get API key status | `GET /api/v1/settings/api-keys/{provider}` | ❌ Missing | **NEW - IN PROGRESS** |
| Set API key | `POST /api/v1/settings/api-keys/{provider}` | ❌ Missing | **NEW - IN PROGRESS** |
| Delete API key | `DELETE /api/v1/settings/api-keys/{provider}` | ❌ Missing | **NEW - IN PROGRESS** |

**Source**: New requirement for embedded mode

---

## 2. Request/Response Feature Matrix

### 2.1 Task Submission Parameters

| Parameter | Location | Cloud Support | Embedded Status | Implementation Notes |
|-----------|----------|---------------|-----------------|---------------------|
| `query` | Body | ✅ Required | ✅ Implemented | |
| `session_id` | Body | ✅ Optional | ✅ Implemented | |
| `mode` | Body | ✅ Optional | ❌ Missing | simple/standard/complex/supervisor |
| `model_tier` | Body | ✅ Optional | ❌ Missing | small/medium/large |
| `model_override` | Body | ✅ Optional | ❌ Missing | Specific model name |
| `provider_override` | Body | ✅ Optional | ❌ Missing | Force provider |
| `research_strategy` | Body | ✅ Optional | ❌ Missing | quick/standard/deep/academic |
| `max_concurrent_agents` | Body | ✅ Optional | ❌ Missing | 1-20 |
| `enable_verification` | Body | ✅ Optional | ❌ Missing | Citation verification |
| `context` | Body | ✅ Optional | ⚠️ Partial | See context matrix below |

**Source**: `docs/task-submission-api.md`

### 2.2 Context Parameters

| Parameter | Cloud Support | Embedded Status | Implementation Notes |
|-----------|---------------|-----------------|---------------------|
| `context.role` | ✅ | ❌ | Role presets (analysis, research, writer) |
| `context.system_prompt` | ✅ | ❌ | Custom system prompt |
| `context.prompt_params` | ✅ | ❌ | Arbitrary key-value pairs |
| `context.model_tier` | ✅ | ❌ | Fallback tier |
| `context.model_override` | ✅ | ❌ | Specific model |
| `context.provider_override` | ✅ | ❌ | Force provider |
| `context.template` | ✅ | ❌ | Template name |
| `context.template_version` | ✅ | ❌ | Template version |
| `context.disable_ai` | ✅ | ❌ | Template-only mode |
| `context.force_research` | ✅ | ❌ | Enable Deep Research 2.0 |
| `context.iterative_research_enabled` | ✅ | ❌ | Iterative loop toggle |
| `context.iterative_max_iterations` | ✅ | ❌ | Max iterations 1-5 |
| `context.enable_fact_extraction` | ✅ | ❌ | Extract structured facts |
| `context.enable_citations` | ✅ | ❌ | Citation collection toggle |
| `context.react_max_iterations` | ✅ | ❌ | ReAct loop depth |
| `context.history_window_size` | ✅ | ❌ | Max conversation history |
| `context.primers_count` | ✅ | ❌ | Early messages to keep |
| `context.recents_count` | ✅ | ❌ | Recent messages to keep |
| `context.compression_trigger_ratio` | ✅ | ❌ | Trigger at % of window |
| `context.compression_target_ratio` | ✅ | ❌ | Compress to % of window |

**Source**: `docs/task-submission-api.md`, `docs/context-window-management.md`

### 2.3 Response Fields

| Field | Cloud Response | Embedded Status | Implementation Notes |
|-------|----------------|-----------------|---------------------|
| `task_id` | ✅ | ✅ | |
| `workflow_id` | ✅ | ✅ | |
| `status` | ✅ | ✅ | |
| `result` | ✅ | ✅ | |
| `model_used` | ✅ | ❌ | Model name |
| `provider` | ✅ | ❌ | Provider name |
| `usage.total_tokens` | ✅ | ❌ | Total tokens |
| `usage.input_tokens` | ✅ | ❌ | Input tokens |
| `usage.output_tokens` | ✅ | ❌ | Output tokens |
| `usage.estimated_cost` | ✅ | ❌ | Cost in USD |
| `metadata.model_breakdown` | ✅ | ❌ | Per-model usage breakdown |

**Source**: `docs/task-submission-api.md`

---

## 3. Streaming Events Feature Matrix

### 3.1 Core Event Types

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `WORKFLOW_STARTED` | ✅ | ❌ | PostgreSQL | |
| `WORKFLOW_COMPLETED` | ✅ | ❌ | PostgreSQL | |
| `WORKFLOW_FAILED` | ✅ | ❌ | PostgreSQL | |
| `AGENT_STARTED` | ✅ | ❌ | Redis only | |
| `AGENT_COMPLETED` | ✅ | ❌ | PostgreSQL | |
| `AGENT_FAILED` | ✅ | ❌ | PostgreSQL | |
| `ERROR_OCCURRED` | ✅ | ❌ | PostgreSQL | |

**Source**: `docs/streaming-api.md`, `docs/event-types.md`

### 3.2 LLM Events

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `LLM_PROMPT` | ✅ | ❌ | Redis only | Prompt sent to LLM |
| `LLM_PARTIAL` | ✅ | ❌ | Redis only | Streaming token delta |
| `LLM_OUTPUT` | ✅ | ❌ | PostgreSQL | Final response + usage |

**SSE Mapping**:
- `LLM_PARTIAL` → `event: thread.message.delta`
- `LLM_OUTPUT` → `event: thread.message.completed`

**Source**: `docs/streaming-api.md`

### 3.3 Tool Events

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `TOOL_INVOKED` | ✅ | ❌ | PostgreSQL | Tool execution started |
| `TOOL_OBSERVATION` | ✅ | ❌ | PostgreSQL | Tool result received |
| `TOOL_ERROR` | ✅ | ❌ | PostgreSQL | Tool execution failed |

**Source**: `docs/streaming-api.md`

### 3.4 Control Signal Events

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `WORKFLOW_PAUSING` | ✅ | ❌ | Redis only | Pause signal received |
| `WORKFLOW_PAUSED` | ✅ | ❌ | PostgreSQL | At checkpoint, paused |
| `WORKFLOW_RESUMED` | ✅ | ❌ | PostgreSQL | Resumed from checkpoint |
| `WORKFLOW_CANCELLING` | ✅ | ❌ | Redis only | Cancel signal received |
| `WORKFLOW_CANCELLED` | ✅ | ❌ | PostgreSQL | Workflow terminated |

**Source**: `docs/control-signals.md`, `docs/streaming-api.md`

### 3.5 Multi-Agent Events

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `ROLE_ASSIGNED` | ✅ | ❌ | PostgreSQL | Role-based agent activated |
| `DELEGATION` | ✅ | ❌ | PostgreSQL | Subtask delegated |
| `PROGRESS` | ✅ | ❌ | Redis only | Progress update |
| `TEAM_RECRUITED` | ✅ | ❌ | PostgreSQL | Agent added to team |
| `TEAM_RETIRED` | ✅ | ❌ | PostgreSQL | Agent removed from team |
| `TEAM_STATUS` | ✅ | ❌ | Redis only | Team status update |

**Source**: `docs/streaming-api.md`, `docs/multi-agent-workflow-architecture.md`

### 3.6 Advanced Events

| Event Type | Cloud Support | Embedded Status | Persistence | Implementation Notes |
|------------|---------------|-----------------|-------------|---------------------|
| `BUDGET_THRESHOLD` | ✅ | ❌ | PostgreSQL | Token budget warning |
| `DATA_PROCESSING` | ✅ | ❌ | Redis only | Data processing step |
| `SYNTHESIS` | ✅ | ❌ | PostgreSQL | Research synthesis |
| `REFLECTION` | ✅ | ❌ | PostgreSQL | Reflection step |
| `WAITING` | ✅ | ❌ | Redis only | Waiting state |
| `ERROR_RECOVERY` | ✅ | ❌ | PostgreSQL | Error recovery attempted |
| `DEPENDENCY_SATISFIED` | ✅ | ❌ | Redis only | Dependency met |
| `APPROVAL_REQUESTED` | ✅ | ❌ | PostgreSQL | Human approval needed |
| `APPROVAL_DECISION` | ✅ | ❌ | PostgreSQL | Approval granted/denied |
| `MESSAGE_SENT` | ✅ | ❌ | Redis only | P2P message sent |
| `MESSAGE_RECEIVED` | ✅ | ❌ | Redis only | P2P message received |
| `WORKSPACE_UPDATED` | ✅ | ❌ | Redis only | Workspace state changed |
| `STATUS_UPDATE` | ✅ | ❌ | Redis only | Generic status update |
| `STREAM_END` | ✅ | ❌ | Redis only | Stream completion marker |

**Source**: `docs/streaming-api.md`, `docs/event-types.md`

---

## 4. Workflow Types Feature Matrix

### 4.1 Core Workflows

| Workflow Type | Cloud Support | Embedded Status | Implementation Notes |
|---------------|---------------|-----------------|---------------------|
| **OrchestratorWorkflow** | ✅ Temporal | ❌ | Top-level router |
| **SimpleTaskWorkflow** | ✅ Temporal | ❌ | Direct execution, no decomposition |
| **ReactWorkflow** | ✅ Temporal | ❌ | ReAct pattern with tools |
| **DAGWorkflow** | ✅ Temporal | ❌ | Multi-step DAG execution |
| **SupervisorWorkflow** | ✅ Temporal | ❌ | Multi-agent coordination |
| **ResearchWorkflow** | ✅ Temporal | ❌ | Deep research with iterations |

**Source**: `docs/multi-agent-workflow-architecture.md`, `docs/task-submission-api.md`

### 4.2 Workflow Routing Logic

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Mode-based routing | ✅ | ❌ | simple/standard/complex/supervisor |
| Complexity detection | ✅ | ❌ | Auto-detect from query |
| Role-based bypass | ✅ | ❌ | Skip decomposition with role preset |
| Template-only mode | ✅ | ❌ | No AI, template rendering only |
| Research strategy | ✅ | ❌ | quick/standard/deep/academic |

**Source**: `docs/task-submission-api.md`

### 4.3 Workflow Capabilities

| Capability | Cloud Support | Embedded Status | Implementation Notes |
|------------|---------------|-----------------|---------------------|
| Checkpoints | ✅ | ❌ | Pause-safe execution points |
| Version gates | ✅ | ❌ | Feature flag versioning |
| Child workflows | ✅ | ❌ | Nested workflow execution |
| Signal handling | ✅ | ❌ | pause/resume/cancel signals |
| Query handlers | ✅ | ❌ | control_state_v1 query |
| Activities | ✅ | ❌ | Tool execution, LLM calls |
| Timers | ✅ | ❌ | Timeout handling |
| Retry policies | ✅ | ❌ | Automatic retries |

**Source**: `docs/control-signals.md`

---

## 5. Database Features Feature Matrix

### 5.1 Core Tables

| Table | Cloud Schema | Embedded Status | Implementation Notes |
|-------|--------------|-----------------|---------------------|
| `tasks` | PostgreSQL | ❌ | Task metadata |
| `sessions` | PostgreSQL | ❌ | Session tracking |
| `events` | PostgreSQL | ❌ | Event audit trail |
| `workflow_events` | | ✅ Implemented | Durable event log (SQLite) |
| `runs` | | ✅ Implemented | Run metadata (SQLite) |
| `memories` | | ✅ Implemented | Conversation memory (SQLite) |
| `workflow_control_state` | | ✅ Implemented | Control state tracking (SQLite) |
| `user_settings` | | ✅ Implemented | User settings (SQLite) |
| `api_keys` | | ✅ Implemented | Encrypted API keys (SQLite) |

**Source**: `docs/authentication-and-multitenancy.md`, Implementation

### 5.2 Vector Search

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Embedding storage | pgvector | ✅ USearch | |
| Similarity search | ✅ | ✅ | Cosine similarity |
| Metadata filtering | ✅ | ❌ | Filter by metadata |
| Hybrid search | ✅ | ❌ | Vector + keyword |

**Source**: `docs/memory-system-architecture.md`

### 5.3 Data Operations

| Operation | Cloud Support | Embedded Status | Implementation Notes |
|-----------|---------------|-----------------|---------------------|
| Create task | ✅ | ❌ | Insert task record |
| Get task | ✅ | ❌ | Retrieve task by ID |
| Update task | ✅ | ❌ | Update task status/result |
| List tasks | ✅ | ❌ | Paginated task list |
| Delete task | ✅ | ❌ | Soft delete |
| Store event | ✅ | ✅ | Event persistence |
| Replay events | ✅ | ✅ | Event sourcing |
| Store memory | ✅ | ✅ | Conversation memory |
| Search memories | ✅ | ✅ | Vector search |

**Source**: Implementation

---

## 6. Authentication & Authorization

### 6.1 Authentication Methods

| Method | Cloud Support | Embedded Status | Implementation Notes |
|--------|---------------|-----------------|---------------------|
| JWT tokens | ✅ | ❌ | For multi-tenant |
| API keys | ✅ | ❌ | For programmatic access |
| OAuth2 | ✅ | ❌ | For SSO |
| Embedded bypass | ❌ | ✅ Implemented | No auth in embedded mode |

**Source**: `docs/authentication-and-multitenancy.md`

### 6.2 Authorization

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Tenant isolation | ✅ | ❌ | Multi-tenancy |
| Rate limiting | ✅ | ❌ | Per-user limits |
| Quota enforcement | ✅ | ❌ | Token/cost limits |
| OPA policies | ✅ | ❌ | Policy-based access control |

**Source**: `docs/authentication-and-multitenancy.md`

---

## 7. Configuration Features

### 7.1 Model Configuration

| Feature | Cloud Config | Embedded Status | Implementation Notes |
|---------|--------------|-----------------|---------------------|
| Provider routing | `config/models.yaml` | ❌ | Multi-provider routing |
| Tier mapping | `config/models.yaml` | ❌ | small/medium/large → models |
| Fallback chains | `config/models.yaml` | ❌ | Automatic fallback |
| Cost tracking | `config/models.yaml` | ❌ | Per-model pricing |

**Source**: `docs/providers-models.md`, `docs/centralized-pricing.md`

### 7.2 System Configuration

| Feature | Cloud Config | Embedded Status | Implementation Notes |
|---------|--------------|-----------------|---------------------|
| Feature flags | `config/features.yaml` | ❌ | Runtime feature toggles |
| Research strategies | `config/research_strategies.yaml` | ❌ | Strategy presets |
| Timeout config | `config/shannon.yaml` | ❌ | Global timeouts |
| Hot reload | ✅ | ❌ | Config auto-reload |

**Source**: `docs/environment-configuration.md`, `docs/testing-hot-reload-config.md`

### 7.3 Tool Configuration

| Feature | Cloud Config | Embedded Status | Implementation Notes |
|---------|--------------|-----------------|---------------------|
| Web search | `config/web_search.yaml` | ❌ | Search provider config |
| Web fetch | `config/web_fetch.yaml` | ❌ | Fetch settings |
| Custom tools | `config/tools/` | ❌ | Tool definitions |
| OpenAPI tools | `config/openapi/` | ❌ | OpenAPI specs |

**Source**: `docs/web-search-configuration.md`, `docs/web-fetch-configuration.md`, `docs/openapi-tools.md`

---

## 8. Tool Integration

### 8.1 Built-in Tools

| Tool | Cloud Support | Embedded Status | Implementation Notes |
|------|---------------|-----------------|---------------------|
| web_search | ✅ | ❌ | Web search via providers |
| web_fetch | ✅ | ❌ | Fetch web content |
| calculator | ✅ | ❌ | Math calculations |
| python_exec | ✅ | ❌ | Python code execution |
| file_read | ✅ | ❌ | Read files |
| file_write | ✅ | ❌ | Write files |

**Source**: `docs/adding-custom-tools.md`

### 8.2 Tool Types

| Type | Cloud Support | Embedded Status | Implementation Notes |
|------|---------------|-----------------|---------------------|
| Native tools | ✅ | ❌ | Built-in Rust/Go tools |
| OpenAPI tools | ✅ | ❌ | Generated from OpenAPI specs |
| MCP tools | ✅ | ❌ | Model Context Protocol |
| Python tools | ✅ | ❌ | Custom Python functions |

**Source**: `docs/openapi-tools.md`, `docs/adding-custom-tools.md`

### 8.3 Tool Execution

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Tool allowlist | ✅ | ❌ | Explicit tool control |
| Tool caching | ✅ | ❌ | Cache tool results |
| Tool retries | ✅ | ❌ | Automatic retry on failure |
| Tool timeouts | ✅ | ❌ | Per-tool timeout |
| Tool events | ✅ | ❌ | TOOL_INVOKED/OBSERVATION |

**Source**: `docs/api-reference.md`

---

## 9. Memory & Context Management

### 9.1 Memory Features

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Conversation memory | ✅ | ✅ Implemented | Store conversation history |
| Vector search | ✅ | ✅ Implemented | Semantic search |
| Memory compression | ✅ | ❌ | Compress old messages |
| Memory retrieval | ✅ | ✅ Implemented | Retrieve relevant context |

**Source**: `docs/memory-system-architecture.md`

### 9.2 Context Window Management

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Dynamic window sizing | ✅ | ❌ | Adjust to model limits |
| Primer retention | ✅ | ❌ | Keep early messages |
| Recent retention | ✅ | ❌ | Keep recent messages |
| Compression triggers | ✅ | ❌ | Auto-compress at threshold |
| Compression strategies | ✅ | ❌ | Summarize vs. truncate |

**Source**: `docs/context-window-management.md`

---

## 10. Observability Features

### 10.1 Logging

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Structured logging | ✅ | ✅ Partial | tracing crate |
| Log levels | ✅ | ✅ Implemented | trace/debug/info/warn/error |
| Log context | ✅ | ❌ | Workflow/task/agent context |
| IPC logging | ❌ | ✅ Implemented | Tauri IPC log events |

**Source**: Implementation

### 10.2 Metrics

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| Prometheus metrics | ✅ | ❌ | /metrics endpoint |
| Custom metrics | ✅ | ❌ | Business metrics |
| Token usage metrics | ✅ | ❌ | Per-model tracking |
| Cost metrics | ✅ | ❌ | Per-task cost |

**Source**: `docs/tier-drift-metrics.md`, `docs/token-budget-tracking.md`

### 10.3 Tracing

| Feature | Cloud Support | Embedded Status | Implementation Notes |
|---------|---------------|-----------------|---------------------|
| OpenTelemetry | ✅ | ❌ | Distributed tracing |
| Span creation | ✅ | ❌ | Trace spans |
| Trace propagation | ✅ | ❌ | Cross-service tracing |
| Trace export | ✅ | ❌ | OTLP export |

**Source**: Implementation

---

## 11. Implementation Priorities

### 11.1 Critical Path (P0 - Required for MVP)

1. ✅ **Database migrations** (control_state, user_settings, api_keys)
2. ⏳ **Settings API** (GET/POST/DELETE for settings and API keys)
3. ⏳ **Encryption utilities** (AES-256-GCM for API keys)
4. ⏳ **Control-state endpoint** (`GET /api/v1/tasks/{id}/control-state`)
5. ⏳ **Task output endpoint** (`GET /api/v1/tasks/{id}/output`)
6. ⏳ **API key validation** (Check before task submission)
7. ⏳ **Frontend integration** (Toast notifications, settings UI)

### 11.2 High Priority (P1 - Core Functionality)

1. **Task management endpoints**
   - `POST /api/v1/tasks/stream` (submit with stream URL)
   - `GET /api/v1/tasks/{id}/progress` (progress polling)
   - `GET /api/v1/tasks` (list tasks)
   - `POST /api/v1/tasks/{id}/pause` (pause workflow)
   - `POST /api/v1/tasks/{id}/resume` (resume workflow)
   - `POST /api/v1/tasks/{id}/cancel` (cancel workflow)

2. **Session management endpoints**
   - `GET /api/v1/sessions` (list sessions)
   - `GET /api/v1/sessions/{id}` (get session)
   - `GET /api/v1/sessions/{id}/history` (session history)

3. **Streaming APIs**
   - SSE endpoint (`GET /stream/sse`)
   - WebSocket endpoint (`GET /stream/ws`)
   - Event persistence (Redis simulation with in-memory store)

4. **Core event types**
   - WORKFLOW_STARTED, WORKFLOW_COMPLETED, WORKFLOW_FAILED
   - AGENT_STARTED, AGENT_COMPLETED
   - ERROR_OCCURRED

### 11.3 Medium Priority (P2 - Enhanced Functionality)

1. **Context parameters support**
   - role, system_prompt, prompt_params
   - model_tier, model_override, provider_override
   - Research strategy parameters
   - Context window management parameters

2. **LLM & Tool events**
   - LLM_PROMPT, LLM_PARTIAL, LLM_OUTPUT
   - TOOL_INVOKED, TOOL_OBSERVATION, TOOL_ERROR

3. **Control signal events**
   - WORKFLOW_PAUSING, WORKFLOW_PAUSED, WORKFLOW_RESUMED
   - WORKFLOW_CANCELLING, WORKFLOW_CANCELLED

4. **Response enhancements**
   - model_used, provider
   - usage.total_tokens, input_tokens, output_tokens, cost
   - metadata.model_breakdown

### 11.4 Low Priority (P3 - Advanced Features)

1. **Schedule management** (Lower priority for embedded mode)
2. **Multi-agent events** (ROLE_ASSIGNED, DELEGATION, TEAM_RECRUITED, etc.)
3. **Advanced events** (BUDGET_THRESHOLD, SYNTHESIS, REFLECTION, etc.)
4. **gRPC streaming** (Optional for embedded)
5. **Metrics endpoint** (Prometheus format)
6. **Template system** (Template-only execution)
7. **Advanced tool features** (Tool caching, allowlist, retries)

### 11.5 Future Considerations

1. **Multi-tenancy** (Authentication beyond embedded bypass)
2. **Rate limiting** (Per-user quotas)
3. **OPA policies** (Policy-based access control)
4. **Hot-reload configuration** (Runtime config updates)
5. **OpenTelemetry tracing** (Distributed tracing)
6. **Memory compression** (Context window optimization)

---

## 12. Testing Requirements

### 12.1 Integration Test Coverage

All features must have integration tests that verify end-to-end functionality:

1. **API Endpoint Tests**
   - Submit task and verify response
   - Poll task status until completion
   - Retrieve task output
   - Get control state
   - Pause/resume/cancel workflows

2. **Settings Tests**
   - Store and retrieve settings
   - Encrypt/decrypt API keys
   - Validate API key requirement for task submission
   - Test API key rotation

3. **Streaming Tests**
   - Connect to SSE stream
   - Receive all expected event types
   - Reconnect with last_event_id
   - Test event persistence

4. **Database Tests**
   - Create and retrieve tasks
   - Store and replay workflow events
   - Search memories by vector similarity
   - Update control state

5. **Workflow Tests**
   - Execute simple workflow
   - Test checkpoint and pause/resume
   - Test cancellation
   - Verify event emission

### 12.2 Test Infrastructure

- **Test Framework**: Use `cargo test` with integration tests in `tests/` directory
- **Test Database**: In-memory SQLite or temporary file
- **Test Server**: Spawn embedded API on random port
- **Assertions**: Verify HTTP status codes, response schemas, database state
- **Cleanup**: Automatic cleanup after each test

### 12.3 Coverage Goals

- **API Endpoints**: 100% of critical path (P0)
- **Event Types**: 100% of core events (P1)
- **Database Operations**: 100% of CRUD operations
- **Error Handling**: All error paths tested
- **Edge Cases**: Null values, empty arrays, invalid inputs

---

## 13. Documentation Requirements

1. **API Documentation**
   - OpenAPI/Swagger spec for all endpoints
   - Request/response examples
   - Error codes and messages

2. **Migration Guide**
   - Cloud → Embedded migration instructions
   - Feature parity matrix
   - Known limitations

3. **Configuration Guide**
   - Environment variables
   - Configuration files
   - Default values

4. **Integration Guide**
   - Desktop app integration
   - Frontend SDK usage
   - Common patterns

---

## 14. Success Metrics

| Metric | Target | Current | Status |
|--------|--------|---------|--------|
| API endpoint coverage | 100% | ~20% | ❌ |
| Event type coverage | 100% | 0% | ❌ |
| Request parameter support | 100% | ~30% | ❌ |
| Response field coverage | 100% | ~40% | ❌ |
| Integration test coverage | 100% | ~10% | ❌ |
| Documentation completeness | 100% | ~30% | ❌ |

**Definition of Done**: All metrics at 100% with passing integration tests.

---

## 15. Timeline Estimate

| Phase | Effort | Dependencies |
|-------|--------|--------------|
| **P0: Critical Path** | 10-15 hours | Database migrations ✅ |
| **P1: Core Functionality** | 20-30 hours | P0 complete |
| **P2: Enhanced Functionality** | 30-40 hours | P1 complete |
| **P3: Advanced Features** | 40-60 hours | P2 complete |
| **Testing & Documentation** | 20-30 hours | Parallel with development |
| **Total** | 120-175 hours | ~3-4 weeks full-time |

---

## 16. Risk Assessment

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Scope creep | High | High | Strict P0/P1/P2/P3 prioritization |
| Complex workflows missing | High | Medium | Use simpler Durable patterns |
| Performance issues | Medium | Medium | Benchmark early, optimize incrementally |
| API incompatibilities | High | Low | Maintain strict API contract compatibility |
| Database migration issues | Medium | Low | Test migrations thoroughly |

---

## Appendix A: Documentation Reference

All features in this specification are sourced from official documentation:

- `docs/api-reference.md` - LLM Service API
- `docs/task-submission-api.md` - Task submission parameters
- `docs/streaming-api.md` - Streaming events and protocols
- `docs/control-signals.md` - Pause/resume/cancel
- `docs/scheduled-tasks.md` - Schedule management
- `docs/multi-agent-workflow-architecture.md` - Workflow types
- `docs/authentication-and-multitenancy.md` - Auth methods
- `docs/memory-system-architecture.md` - Memory features
- `docs/context-window-management.md` - Context management
- `docs/providers-models.md` - Model configuration
- `docs/centralized-pricing.md` - Cost tracking
- `docs/web-search-configuration.md` - Web search
- `docs/web-fetch-configuration.md` - Web fetch
- `docs/openapi-tools.md` - OpenAPI tool integration
- `docs/adding-custom-tools.md` - Custom tools
- `docs/event-types.md` - Event type reference

---

## Appendix B: Change Log

| Date | Version | Changes |
|------|---------|---------|
| 2026-01-10 | 1.0 | Initial specification created |

---

**END OF SPECIFICATION**
