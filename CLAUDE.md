# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Shannon is an enterprise-grade multi-agent AI platform that combines Rust (agent core), Go (orchestration with Temporal), and Python (LLM services) to create a token-efficient, distributed AI system.

## Essential Commands

```bash
# Environment Setup
make setup-env                # Setup environment configuration
vim .env                      # Add your API keys (OpenAI or Anthropic)
make dev                      # Start all services

# Development
make smoke                    # Run E2E smoke tests
./scripts/submit_task.sh "Your query here"  # Submit a test task
docker compose -f deploy/compose/docker-compose.yml logs -f [service]  # View logs

# Proto Changes
make proto                    # Regenerate proto files
make ci                       # Run CI checks
docker compose build --no-cache  # Rebuild services (use --no-cache for Go updates)

# Testing
make replay-export WORKFLOW_ID=xxx OUT=test.json  # Export workflow history
make replay HISTORY=test.json                     # Test determinism
make ci-replay                                    # Test all histories

# Service Testing
cd rust/agent-core && cargo test
cd go/orchestrator && go test -race ./...
cd python/llm-service && python3 -m pytest
```

## Service Ports

- **Agent Core**: 50051 (gRPC), 2113 (metrics)
- **Orchestrator**: 50052 (gRPC), 2112 (metrics), 8081 (health)
- **LLM Service**: 8000 (HTTP)
- **Temporal**: 7233 (gRPC), 8088 (UI)
- **PostgreSQL**: 5432
- **Redis**: 6379
- **Qdrant**: 6333
- **Prometheus**: 9090
- **Grafana**: 3000

## Project Structure

- **`rust/agent-core/`**: Enforcement gateway, WASI sandbox, gRPC server
- **`go/orchestrator/`**: Temporal workflows, budget manager, complexity analyzer
- **`python/llm-service/`**: LLM providers, MCP tools
- **`protos/`**: Shared protocol buffer definitions
- **`deploy/compose/`**: Docker Compose configuration
- **`config/`**: Hot-reload configuration files
- **`docs/`**: Architecture and API documentation
- **`scripts/`**: Automation and helper scripts

## Workflow Types & Strategies

Shannon's orchestrator uses different workflow types based on task complexity and execution patterns. **All workflows populate usage metadata** (model, provider, tokens, cost) in API responses.

### Core Workflows

| Workflow | File | Trigger | Use Case |
|----------|------|---------|----------|
| **SimpleTaskWorkflow** | `simple_workflow.go` | Complexity < 0.3 | Single-agent, no dependencies |
| **SupervisorWorkflow** | `supervisor_workflow.go` | >5 subtasks or dependencies | Large plans with coordination |
| **StreamingWorkflow** | `streaming_workflow.go` | Streaming mode | Real-time token streaming |
| **ParallelStreamingWorkflow** | `streaming_workflow.go` | Streaming + parallel | Multi-agent streaming |
| **TemplateWorkflow** | `template_workflow.go` | Template execution | Pre-defined workflows |
| **ScheduledTaskWorkflow** | `scheduled/scheduled_task_workflow.go` | Temporal schedule trigger | Recurring cron-based execution |
| **SwarmWorkflow** | `swarm_workflow.go` | `force_swarm` or strategy `"swarm"` | Multi-agent collaboration with shared workspace |
| **OrchestratorWorkflow** | `orchestrator_router.go` | Entry point | Routes to appropriate workflow |

### Strategy Workflows (in `strategies/`)

| Strategy | File | Pattern | Use Case |
|----------|------|---------|----------|
| **DAGWorkflow** | `dag.go` | Fan-out/fan-in | Standard parallel/sequential execution |
| **ReactWorkflow** | `react.go` | Reasoning loop | Iterative reasoning + tool use |
| **ResearchWorkflow** | `research.go` | React + parallel | Multi-step research tasks |
| **ExploratoryWorkflow** | `exploratory.go` | Tree-of-Thoughts | Complex decision-making |
| **ScientificWorkflow** | `scientific.go` | Multi-pattern | Hypothesis testing, debate |
| **BrowserUseWorkflow** | `browser_use.go` | `"browser_use"` strategy | Web browsing and UI interaction |
| **DomainAnalysisWorkflow** | `domain_analysis_workflow.go` | Child of Research | Deep company/entity research enrichment |

### Research Strategy Model Tiers (Cost Optimization)

ResearchWorkflow uses a **tiered model architecture** for cost efficiency (50-70% reduction):

| Activity Type | Model Tier | Rationale |
|--------------|------------|-----------|
| Utility activities (coverage eval, fact extraction, subquery gen) | small | Structured output tasks |
| Agent execution (quick strategy) | small | Fast, cheap research |
| Agent execution (standard/deep/academic) | medium | Iterative refinement compensates |
| **Final synthesis** | **large** | User-facing quality critical |

**Key implementation details:**
- `config/research_strategies.yaml` defines `agent_model_tier` per strategy
- `strategy_helpers.go:determineModelTier()` selects tier based on strategy
- `synthesis.go` defaults to large tier but respects `synthesis_model_tier` context override
- Industry research shows agentic workflows with smaller models + iteration match or outperform single large-model calls

**Config example:**
```yaml
strategies:
  quick:
    agent_model_tier: small
  deep:
    agent_model_tier: medium  # NOT large - synthesis uses large
```

### Workflow Selection Logic

```go
// orchestrator_router.go
if complexity < 0.3 && simpleByShape:
    → SimpleTaskWorkflow
else if len(subtasks) > 5 || hasDependencies:
    → SupervisorWorkflow
else if cognitiveStrategy != "":
    → Strategy workflows (React, Exploratory, etc.)
else:
    → DAGWorkflow (standard)
```

### Tool Execution Paths

Shannon has **two tool execution architectures**:

#### 1. Agent-Core Path (Generic Tools)
**Used for**: Built-in tools (`web_search`, `calculator`, `file_read`, etc.)

**Flow**:
```
Orchestrator → /tools/select → tool_calls injection → Agent-core executes tools → ToolResults → SSE events
```

**Key functions**:
- Orchestrator calls `/tools/select` to choose tools
- `tool_calls` injected into protobuf context (see `ExecuteAgent()` in `agent.go`)
- Agent-core detects `context.fields["tool_calls"]` and executes directly (see `process_request()` in `grpc_server.rs`)
- Returns `ToolResults` in response
- Emits SSE events: `TOOL_INVOKED`, `TOOL_OBSERVATION` (see `publishToolObservation()` in `agent.go`)

#### 2. Python-Only Path (Custom Tools)
**Used for**: Custom tools defined in Python (`python/llm-service/llm_service/tools/`)

**Flow**:
```
Orchestrator → Agent-core (suggested_tools) → LLM-service (internal function calling) → Results in text
```

- Agent-core passes `suggested_tools` to llm-service
- LLM-service uses native function calling (see `agent_query()` / `/agent/query` endpoint in `agent.py`)
- Tools execute inside Python, results embedded in LLM response text
- **NO `ToolResults` returned** to agent-core
- **NO SSE events emitted** (by design)

### Metadata Behavior (ALL Workflows)

Every workflow populates `TaskResult.Metadata` with:
```json
{
    "cost_usd": 0.000087,
    "input_tokens": 104,
    "mode": "simple",
    "model": "gpt-5-nano-2025-08-07",
    "model_used": "gpt-5-nano-2025-08-07",
    "num_agents": 1,
    "output_tokens": 70,
    "provider": "openai",
    "total_tokens": 174
}
```

**Implementation**: All workflows call `metadata.AggregateAgentMetadata()` (in `go/orchestrator/internal/metadata/aggregate.go`) to extract metadata from agent execution results.

**Data flow**:
1. LLM provider returns token counts
2. Agent activity captures in `AgentExecutionResult`
3. Workflow aggregates via `metadata.AggregateAgentMetadata()`
4. Service layer extracts from `result.Metadata` → database
5. Gateway API returns in `GET /api/v1/tasks/{id}` response

**Token Recording**: Every agent execution records usage exactly once (no duplicates):
- **Budgeted runs**: ExecuteAgentWithBudget records in activity
- **Non-budgeted runs**: Workflow patterns record at completion
- See `docs/token-budget-tracking.md` for complete details

## Synthesis Templates (Output Customization)

Customize how Shannon formats final answers from multi-agent research.

### Quick Reference

| Method | Context Parameter | Use Case |
|--------|-------------------|----------|
| Named template | `synthesis_template: "my_format"` | Reusable `.tmpl` files |
| Verbatim override | `synthesis_template_override: "..."` | One-time custom format |

### Example: Custom Template

```bash
# Use a named template
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "...",
  "context": {
    "synthesis_template": "test_bullet_summary",
    "force_research": true
  }
}'
```

### Example: Verbatim Override

```bash
# Pass complete synthesis instructions (bypasses _base.tmpl)
curl -X POST http://localhost:8080/api/v1/tasks -d '{
  "query": "...",
  "context": {
    "synthesis_template_override": "You are a market analyst...\n## Citation Rules\n- Use [n] format...",
    "force_research": true
  }
}'
```

**Note**: `synthesis_template_override` bypasses the protected citation contract. You must restate citation rules (`[n]` format) in your override text.

### Documentation
- **Template files**: `config/templates/synthesis/`
- **Full guide**: `docs/extending-shannon.md#synthesis-templates-output-customization`
- **Template README**: `config/templates/synthesis/README.md`

---

## Scheduled Tasks

Shannon supports **recurring task execution** using Temporal's native Schedule API. Users can create cron-based schedules that automatically execute tasks at specified intervals.

### Quick Reference

| Feature | Implementation |
|---------|----------------|
| **Scheduling Engine** | Temporal Schedule API (native) |
| **Cron Validation** | `robfig/cron/v3` (validation only, not execution) |
| **Resource Limits** | Configurable via environment variables |
| **Database Tables** | `scheduled_tasks`, `scheduled_task_executions` |
| **API Endpoints** | 7 REST endpoints (CRUD + pause/resume) |

### Environment Configuration

```bash
# Maximum schedules per user (default: 50)
SCHEDULE_MAX_PER_USER=50

# Minimum interval between runs in minutes (default: 60)
SCHEDULE_MIN_INTERVAL_MINS=60

# Maximum budget per execution in USD (default: 10.0)
SCHEDULE_MAX_BUDGET_USD=10.0
```

### Architecture Flow

```
User Request → Gateway (HTTP) → Orchestrator (gRPC) → Schedule Manager
                                                             ↓
                                                    Temporal Schedule API
                                                             ↓
                                           ScheduledTaskWorkflow (wrapper)
                                                             ↓
                                           OrchestratorWorkflow (delegates to existing workflows)
```

### Key Implementation Details

- **Workflow**: `internal/workflows/scheduled/scheduled_task_workflow.go` wraps existing workflows
- **Manager**: `internal/schedules/manager.go` handles CRUD, Temporal integration, resource limits
- **Activities**: `internal/activities/schedule_activities.go` tracks execution start/completion
- **Database**: Uses `*sql.DB` directly to avoid import cycles with `db` package
- **Validation**: Cron expressions validated with `robfig/cron/v3` before sending to Temporal
- **Execution Tracking**: Every run persisted to `scheduled_task_executions` with cost/status
- **Ownership**: All operations enforce user_id and tenant_id validation at gRPC layer

### API Endpoints

All endpoints require authentication (`/api/v1/schedules`):
- `POST /` - Create schedule
- `GET /` - List schedules (paginated, filterable by status)
- `GET /{id}` - Get schedule details
- `PUT /{id}` - Update schedule configuration
- `DELETE /{id}` - Soft delete schedule
- `POST /{id}/pause` - Pause execution
- `POST /{id}/resume` - Resume execution

### Cron Expression Examples

```bash
"0 9 * * *"      # Daily at 9:00 AM
"0 */4 * * *"    # Every 4 hours
"0 0 * * 1"      # Every Monday at midnight
"30 8 1 * *"     # First day of month at 8:30 AM
"0 12 * * 1-5"   # Weekdays at noon
```

### Documentation

- **Complete guide**: `docs/scheduled-tasks.md`
- **Environment config**: `docs/environment-configuration.md`
- **Database schema**: `migrations/postgres/009_scheduled_tasks.sql`

---

## Adding/Fixing LLM Providers

Shannon uses a **single source of truth** for all LLM provider configurations: `config/models.yaml`. When adding or fixing a provider, update files in this order:

### 1. Configuration (`config/models.yaml`)
```yaml
# Add to model_tiers (with priority ranking)
model_tiers:
  small:
    providers:
      - provider: newprovider
        model: model-name
        priority: N

# Add to model_catalog (capabilities)
model_catalog:
  newprovider:
    model-name:
      model_id: model-name
      tier: small
      context_window: 128000
      max_tokens: 4096
      supports_functions: true
      supports_streaming: true

# Add to pricing (cost per 1K tokens)
pricing:
  models:
    newprovider:
      model-name:
        input_per_1k: 0.001
        output_per_1k: 0.005

# Add to provider_settings (API config)
provider_settings:
  newprovider:
    base_url: https://api.newprovider.com/v1
    timeout: 60
    max_retries: 3
```

### 2. Python Provider Implementation
- **Create**: `python/llm-service/llm_provider/{provider}_provider.py`
  - Implement `LLMProvider` base class
  - Handle API calls, token counting, cost estimation
  - Convert message formats if needed

### 3. Provider Registry
- **Edit**: `python/llm-service/llm_service/providers/__init__.py`
  - Add to `ProviderType` enum: `NEWPROVIDER = "newprovider"`
  - Add to `_PROVIDER_NAME_MAP`: `"newprovider": ProviderType.NEWPROVIDER`

### 4. Go Provider Detection (Optional)
- **Edit**: `go/orchestrator/internal/models/provider.go`
  - Add pattern matching in `detectProviderFromPattern()` for fallback
  - Only needed if model names don't match existing catalog patterns

### File Locations Reference

| Layer | File | Purpose |
|-------|------|---------|
| **Config** | `config/models.yaml` | Model tiers, pricing, capabilities, API settings |
| **Go Pricing** | `go/orchestrator/internal/pricing/pricing.go` | Cost calculation from YAML |
| **Go Detection** | `go/orchestrator/internal/models/provider.go` | Provider detection (catalog + patterns) |
| **Python Provider** | `python/llm-service/llm_provider/{provider}_provider.py` | API client implementation |
| **Python Registry** | `python/llm-service/llm_service/providers/__init__.py` | Provider enum + mapping |

### Testing Checklist
- ✅ Model appears in tier via API: `GET /api/v1/models`
- ✅ Pricing calculated correctly from `config/models.yaml`
- ✅ Provider detection works: `DetectProvider("model-name")` → `"newprovider"`
- ✅ API calls succeed with proper auth
- ✅ Token counts and costs reported in response metadata

---

## Critical Implementation Rules

### Temporal Workflows
- Always await activity completion with `.Get(ctx, &result)`
- Use `workflow.Sleep()` in workflow code, never `time.Sleep()` in workflow code (breaks determinism; activities may use `time.Sleep()` freely)
- Maintain workflow determinism for replay testing

### Database
- Tasks table: `task_executions` (primary table for task persistence)
- Status values: UPPERCASE for API/DB (`"COMPLETED"` not `"completed"`). Some internal workflow state may use lowercase for struct fields.
- Empty UUID strings must convert to NULL

### Session ID Handling (Gateway APIs)
- **CRITICAL**: Session IDs support dual-format mapping (UUID + external string IDs)
- When a non-UUID `session_id` is provided (e.g., `"manual-test-123"`):
  - Orchestrator creates internal UUID for `sessions.id`
  - Stores original string in `sessions.context->>'external_id'`
  - Stores original string in `task_executions.session_id`
- **All session queries MUST check both**:
  ```sql
  WHERE (id::text = $1 OR context->>'external_id' = $1)
  ```
- **Task queries MUST also check both formats**:
  ```sql
  WHERE (session_id = $1 OR session_id = $2) AND user_id = $3
  -- $1 = session UUID, $2 = external_id, $3 = verified user_id
  ```
- **Applies to all session endpoints**:
  - `GetSession`, `GetSessionHistory`, `GetSessionEvents`
  - `UpdateSessionTitle`, `DeleteSession`
  - `ListSessions` JOIN: `ON (t.session_id = s.id::text OR t.session_id = s.context->>'external_id') AND t.user_id = s.user_id`
- **Database indexes** (migration 007):
  - `idx_sessions_external_id` - Functional index for external_id lookups
  - `idx_sessions_user_external_id` - Unique constraint per user
  - `idx_sessions_not_deleted` - Filtered index for active sessions
- See: `go/orchestrator/internal/db/task_writer.go` (`CreateSession()` function)
- See: `go/orchestrator/cmd/gateway/internal/handlers/session.go` (all endpoints)
- See: `migrations/postgres/007_session_soft_delete.sql` (indexes and constraints)

### Proto/gRPC
- Rust enums: Use `ExecutionMode::Simple` (not `ExecutionMode::ExecutionModeSimple`)
- Regenerate with `make proto` after any `.proto` changes

### Testing
- Run `make ci` before pushing changes
- Test replay determinism after workflow changes
- Use race detection for Go tests (`-race` flag)
- Verify API response bodies, not just status codes
- Check database persistence after writes
- Review orchestrator logs and Temporal event history for workflow execution

## Common Pitfalls & Solutions

### Build Failures
```bash
# After proto changes:
make proto
cd go/orchestrator && go mod tidy
cd rust/agent-core && cargo build

# After Go changes:
docker compose -f deploy/compose/docker-compose.yml build orchestrator
docker compose -f deploy/compose/docker-compose.yml up -d orchestrator
```

### Debugging
```bash
# Check workflow status
docker compose exec temporal temporal workflow describe --workflow-id XXX --address temporal:7233

# Database queries
SELECT workflow_id, status, created_at FROM task_executions ORDER BY created_at DESC LIMIT 5;
SELECT id, user_id, created_at FROM sessions WHERE id = 'xxx';

# Redis session data
redis-cli GET session:SESSION_ID | jq '.total_tokens_used'
```

## Documentation References

For detailed information, refer to:
- Architecture: `docs/multi-agent-workflow-architecture.md`
- Pattern Guide: `docs/pattern-usage-guide.md`
- Streaming APIs: `docs/streaming-api.md`
- Skills System: `docs/skills-system.md`
- Memory System: `docs/memory-system-architecture.md`
- Swarm Agents: `docs/swarm-agents.md`
- Authentication: `docs/authentication-and-multitenancy.md`
- Adding Custom Tools: `docs/adding-custom-tools.md`
- System Prompts & Roles: `docs/system-prompts.md`
- Python Execution: `docs/python-code-execution.md`
- Token & Budget Tracking: `docs/token-budget-tracking.md`
- Centralized Pricing: `docs/centralized-pricing.md`
- Synthesis Templates: `docs/extending-shannon.md#synthesis-templates-output-customization`
- Troubleshooting: `docs/troubleshooting.md`
- Configuration: `config/shannon.yaml`

## Python WASI Execution Notes

- Python executor requires explicit `print()` statements for output
- Tool descriptions in `llm_service/api/agent.py` must instruct LLM to use print()
- Python WASI interpreter must be downloaded: `./scripts/setup_python_wasi.sh`
- Table limits in `rust/agent-core/src/wasi_sandbox.rs` must be ≥10000 for Python
- When modifying the `.env` file, services must be **recreated** (not just restarted):
  ```bash
  docker compose -f deploy/compose/docker-compose.yml down
  docker compose -f deploy/compose/docker-compose.yml up -d
  ```

## Python Executor

Shannon uses a WASI-based Python executor (~10ms boot, stdlib only) for sandboxed code execution.

- Config: `rust/agent-core/config/agent.yaml` (python_executor section)
- Workspace: Mounts `/workspace/` with read-write access; files persist within a session (default 24h cleanup)
- See `docs/python-executor.md` for full documentation

## Release Process Lessons

### Protobuf Compatibility
- `grpcio-tools 1.76.0+` works with `protobuf 6.x` (embeds libprotoc 31.x)
- Generated proto files require matching MAJOR version at runtime
- Pre-generate Python protos locally, commit to `grpc_gen/`
- Document generator versions in commit for reproducibility

### Docker Compose for Release
- **Named volumes are empty** on fresh installs - use bind mounts + download scripts
- **Migrations must be downloaded** - `install.sh` fetches `migrations/postgres/*.sql` and qdrant scripts
- **Healthchecks**: Verify binaries exist in target image (qdrant has bash+timeout, not wget)

### GitHub Actions Workflow
- **gitignored directories don't exist** in checkout - add `mkdir -p` before protoc (e.g., `go/orchestrator/internal/pb`)
- **Go protos**: Generate in CI (gitignored)
- **Python protos**: Pre-commit to avoid version mismatch

### Install Script (`scripts/install.sh`)
- **`curl | bash` breaks stdin** - use `< /dev/tty` for interactive prompts
- **WASM URL**: Use vmware-labs official releases, not forks

### Multi-arch Docker Builds
- **Rust + QEMU arm64 = 60+ min** - build amd64-only, rely on Rosetta 2
- Add `platform: linux/amd64` in compose for Apple Silicon compatibility

### Git Tagging for Releases
- **Delete + recreate tag** → orphans release → sends new notifications to watchers
- **Better approach**: `git tag -f v0.x.x && git push origin v0.x.x --force` → updates existing release, no notifications

---
