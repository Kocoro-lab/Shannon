# Shannon — Production AI Agents That Actually Work

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org/)
[![Rust](https://img.shields.io/badge/Rust-stable-orange.svg)](https://www.rust-lang.org/)
[![Docker](https://img.shields.io/badge/Docker-required-blue.svg)](https://www.docker.com/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Shannon is battle-tested infrastructure for AI agents that solves the problems you'll hit at scale: runaway costs, non-deterministic failures, and security nightmares.

**Why Shannon?**
- **Deterministic & Debuggable** — Temporal workflows with time-travel debugging; replay any execution step-by-step
- **Observable by Default** — Real-time dashboard, Prometheus metrics, OpenTelemetry tracing built-in
- **Governable** — OPA policies for fine-grained control; human-in-the-loop approvals for sensitive operations
- **Cost-Controlled** — Hard token budgets per task/agent; automatic model fallback when limits approached
- **Production-Ready** — WASI sandbox for secure code execution; multi-tenant isolation; on-prem friendly
- **Vendor-Agnostic** — HTTP REST API + Python SDK; works with OpenAI, Anthropic, Google, DeepSeek, local models
- **Multi-Agent Orchestration** — DAGs and supervisor patterns as first‑class primitives; dynamic team formation and coordination

<div align="center">

![Shannon Desktop App](docs/images/desktop-demo.gif)

*Native desktop app (macOS/iOS) showing real-time agent execution, event streams, and session management*

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│     Please ⭐ star this repo to show your support and stay updated! ⭐        │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```
</div>

## ⚡ Key Features

- **Zero-Token Templates** — YAML workflows eliminate LLM calls for common patterns; up to 85-95% token savings ([→ Docs](docs/template-workflows.md))
- **Time-Travel Debugging** — Temporal-backed workflows; export and replay any execution step-by-step ([→ Architecture](docs/multi-agent-workflow-architecture.md))
- **Hard Budget Limits** — Per-task/per-agent token caps with automatic model fallback when limits approached
- **WASI Sandbox** — Secure Python code execution (CPython 3.11, no network, read-only FS) ([→ Guide](docs/python-code-execution.md))
- **OPA Policies** — Fine-grained rules for tools, models, data access; human-in-the-loop approvals ([→ Auth](docs/authentication-and-multitenancy.md))
- **Multi-Provider** — OpenAI, Anthropic, Google, Groq, DeepSeek, Ollama; centralized pricing in `config/models.yaml`
- **Plug-and-Play Tools** — Add REST APIs via MCP/OpenAPI or write Python tools; no proto changes ([→ Guide](docs/adding-custom-tools.md))
- **Session Memory** — Redis + Qdrant vector store with MMR diversity; learns from failures across sessions ([→ Memory](docs/memory-system-architecture.md))
- **Observable** — Native desktop app (macOS/iOS), Prometheus metrics, OpenTelemetry tracing built-in
- **Multi-Tenant** — Tenant-scoped auth, sessions, memory, workflows with isolation guarantees

## 🎯 Shannon vs. Agent Libraries

**Why choose Shannon over LangGraph, AutoGen, or CrewAI?**

- **Deterministic Workflows** — Temporal-backed with time-travel debugging and replay (not just stateful graphs)
- **First-Class Observability** — Built-in dashboard, Prometheus metrics, OpenTelemetry tracing (not DIY)
- **Human Approvals + OPA Policies** — Fine-grained governance for production use (not basic tool filters)
- **Vendor-Agnostic** — On-prem friendly, HTTP REST API + SDKs, works with any OpenAI-compatible provider

<details>
<summary><b>Detailed Feature Comparison</b> (click to expand)</summary>

| Challenge | Shannon | LangGraph | AutoGen | CrewAI |
|---------|---------|-----------|---------|---------|
| **Multi-Agent Orchestration** | ✅ DAG/Graph workflows | ✅ Stateful graphs | ✅ Group chat | ✅ Crew/roles |
| **Agent Communication** | ✅ Message passing | ✅ Tool calling | ✅ Conversations | ✅ Delegation |
| **Memory & Context** | ✅ Chunked storage (character-based), MMR diversity, decomposition/failure pattern learning | ✅ Multiple types | ✅ Conversation history | ✅ Shared memory |
| **Debugging Production Issues** | ✅ Replay any workflow | ❌ Limited debugging | ❌ Basic logging | ❌ |
| **Token Cost Control** | ✅ Hard budget limits | ❌ | ❌ | ❌ |
| **Security Sandbox** | ✅ WASI isolation | ❌ | ❌ | ❌ |
| **Policy Control (OPA)** | ✅ Fine-grained rules | ❌ | ❌ | ❌ |
| **Deterministic Replay** | ✅ Time-travel debugging | ❌ | ❌ | ❌ |
| **Session Persistence** | ✅ Redis-backed, durable | ⚠️ In-memory only | ⚠️ Limited | ❌ |
| **Multi-Language** | ✅ Go/Rust/Python | ⚠️ Python only | ⚠️ Python only | ⚠️ Python only |
| **Production Metrics** | ✅ Dashboard/Prometheus | ⚠️ DIY | ❌ | ❌ |

</details>

## 🚀 Quick Start

### Prerequisites

- Docker and Docker Compose
- Make, curl
- An API key for at least one supported LLM provider

<details>
<summary><b>Docker Setup Instructions</b> (click to expand)</summary>

#### Installing Docker

**macOS:**
```bash
# Install Docker Desktop from https://www.docker.com/products/docker-desktop/
# Or using Homebrew:
brew install --cask docker
```

**Linux (Ubuntu/Debian):**
```bash
# Install Docker Engine
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER
# Log out and back in for group changes to take effect

# Install Docker Compose
sudo apt-get update
sudo apt-get install docker-compose-plugin
```

#### Verifying Docker Installation
```bash
docker --version
docker compose version
```
</details>

<details>
<summary><b>Windows Setup Guide</b> (click to expand)</summary>

For Windows users, additional configuration is required. See the dedicated guides:
- [Windows Setup Guide (English)](docs/windows-setup-guide-en.md)
- [Windows 本地环境适配指南 (中文)](docs/windows-setup-guide-cn.md)

</details>

### 30-Second Setup

```bash
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon

# One-stop setup: creates .env, generates protobuf files
make setup

# Add your LLM API key to .env
echo "OPENAI_API_KEY=your-key-here" >> .env

# Download Python WASI interpreter for secure code execution (20MB)
./scripts/setup_python_wasi.sh

# Start all services and verify
make dev
make smoke
```

<details>
<summary><b>Optional: Enable Grafana & Prometheus</b> (click to expand)</summary>

```bash
# Start monitoring stack (Grafana on :3030, Prometheus on :9090)
cd deploy/compose/grafana
docker compose -f docker-compose-grafana-prometheus.yml up -d
```

</details>

### Your First Agent

Shannon provides multiple ways to interact with your AI agents:

#### Option 1: Use the Desktop App (macOS/iOS)

```bash
# Build and run the native desktop app
cd desktop
npm install
npm run tauri:dev    # Development mode with hot reload

# Or build for distribution
npm run tauri:build  # macOS DMG
npm run tauri ios build  # iOS (requires Xcode)

# The desktop app provides:
# - Native macOS and iOS experience
# - Real-time SSE event streaming
# - Session and task management
# - Dark mode support
```

#### Option 2: Use the REST API

```bash
# For development (no auth required)
export GATEWAY_SKIP_AUTH=1

# Submit a task via API
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Analyze the sentiment of: Shannon makes AI agents simple!",
    "session_id": "demo-session-123"
  }'

# Response includes workflow_id for tracking
# {"workflow_id":"task-dev-1234567890","status":"running"}
```

#### Option 3: Python SDK (pip)

```bash
pip install shannon-sdk
```

```python
from shannon import ShannonClient

with ShannonClient(base_url="http://localhost:8080") as client:
    handle = client.submit_task(
        "Analyzing sentiment: Shannon makes AI agents simple!",
        session_id="demo-session"
    )
    status = client.wait(handle.task_id)
    print(status.status, status.result)
```

CLI is also available after install: `shannon --base-url http://localhost:8080 submit "Hello"`.

#### Watch Your Agent Work in Real-Time

```bash
# Stream live events as your agent works (replace with your workflow_id)
curl -N http://localhost:8080/api/v1/stream/sse?workflow_id=task-dev-1234567890

# You'll see human-readable events like:
# event: AGENT_THINKING
# data: {"message":"Analyzing sentiment: Shannon makes AI agents simple!"}
#
# event: TOOL_INVOKED
# data: {"message":"Processing natural language sentiment analysis"}
#
# event: AGENT_COMPLETED
# data: {"message":"Task completed successfully"}
```

#### Get Your Results

```bash
# Check final status and result
curl http://localhost:8080/api/v1/tasks/task-dev-1234567890

# Response includes status, result, tokens used, and metadata
```

#### Production Setup

For production, use API keys instead of GATEWAY_SKIP_AUTH:

```bash
# Create an API key (one-time setup)
make seed-api-key  # Creates test key: sk_test_123456

# Use in requests
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "X-API-Key: sk_test_123456" \
  -H "Content-Type: application/json" \
  -d '{"query":"Your task here"}'
```

<!-- For full API details, see Gateway README or the docs site. -->

#### Production Checklist

Before deploying Shannon to production:

- [ ] **Authentication** — Replace `GATEWAY_SKIP_AUTH=1` with API keys (`make seed-api-key`)
- [ ] **OPA Policies** — Configure fine-grained access rules in `config/opa/policies/`
- [ ] **Token Budgets** — Set per-task/per-agent limits in task submission context
- [ ] **Monitoring** — Enable Prometheus metrics and OpenTelemetry tracing (see [Grafana setup](#optional-enable-grafana--prometheus-click-to-expand))
- [ ] **Approvals** — Configure human-in-the-loop for sensitive operations in `config/features.yaml`
- [ ] **Secrets Management** — Use environment variables or secrets manager (never commit `.env`)
- [ ] **Rate Limits** — Configure provider-specific RPM/TPM limits in `config/models.yaml`

See [Configuration Guide](config/README.md) and [Authentication](docs/authentication-and-multitenancy.md) for details.

### Delete a session (soft delete)

```bash
# Soft-delete a session you own (idempotent, returns 204)
curl -X DELETE http://localhost:8080/api/v1/sessions/<SESSION_UUID> \
  -H "X-API-Key: sk_test_123456"

# Notes:
# - Marks the session as deleted (deleted_at/deleted_by); data remains in DB
# - Deleted sessions are excluded from reads and cannot be fetched
# - Redis cache for the session is cleared
```

<!-- Advanced methods (scripts, gRPC, admin streaming) are available in service READMEs and scripts/. Keeping the root README focused on core value and quick paths. -->

## 📚 Examples That Actually Matter

*Click each example below to expand. These showcase Shannon's unique features that set it apart from other frameworks.*

<details>
<summary><b>Example 1: Cost-Controlled Customer Support</b></summary>

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Help me troubleshoot my deployment issue",
    "session_id": "user-123-session"
  }'
```
Key features:
- **Session persistence** - Maintains conversation context across requests
- **Token tracking** - Every request returns token usage and costs
- **Policy control** - Apply OPA policies for allowed actions (see Example 3)
- **Result**: Up to 70% cost reduction through smart caching and session management (based on internal testing)

Typical Task Status response (shape):

```json
{
  "task_id": "task-...",
  "status": "TASK_STATUS_COMPLETED",
  "result": "...",
  "model_used": "gpt-5-mini-2025-08-07",
  "provider": "openai",
  "usage": {
    "total_tokens": 300,
    "input_tokens": 200,
    "output_tokens": 100,
    "estimated_cost": 0.006
  }
}
```

</details>

<details>
<summary><b>Example 2: Debugging Production Failures</b></summary>

```bash
# Production agent failed at 3am? No problem.
# Export and replay the workflow in one command
./scripts/replay_workflow.sh task-prod-failure-123

# Or specify a particular run ID
./scripts/replay_workflow.sh task-prod-failure-123 abc-def-ghi

# Output shows step-by-step execution with token counts, decisions, and state changes
# Fix the issue, add a test case, never see it again
```

</details>

<details>
<summary><b>Example 3: Multi-Team Model Governance</b></summary>

```rego
# config/opa/policies/data-science.rego
package shannon.teams.datascience

default allow = false

allow {
    input.team == "data-science"
    input.model in ["gpt-5-2025-08-07", "claude-sonnet-4-5-20250929"]
}

max_tokens = 50000 {
    input.team == "data-science"
}

# config/opa/policies/customer-support.rego
package shannon.teams.support

default allow = false

allow {
    input.team == "support"
    input.model == "gpt-5-mini-2025-08-07"
}

max_tokens = 5000 {
    input.team == "support"
}

deny_tool["database_write"] {
    input.team == "support"
}
```

</details>

<details>
<summary><b>Example 4: Security-First Code Execution</b></summary>

```bash
# Python code runs in isolated WASI sandbox with full standard library
./scripts/submit_task.sh "Execute Python: print('Hello from secure WASI!')"

# Even malicious code is safe
./scripts/submit_task.sh "Execute Python: import os; os.system('rm -rf /')"
# Result: OSError - system calls blocked by WASI sandbox

# Advanced: Session persistence for data analysis
./scripts/submit_task.sh "Execute Python with session 'analysis': data = [1,2,3,4,5]"
./scripts/submit_task.sh "Execute Python with session 'analysis': print(sum(data))"
# Output: 15
```
[→ Full Python Execution Guide](docs/python-code-execution.md)

</details>

<details>
<summary><b>Example 5: Human-in-the-Loop Approval</b></summary>

```bash
# Configure approval for high-complexity or dangerous operations
cat > config/features.yaml << 'EOF'
workflows:
  approval:
    enabled: true
    complexity_threshold: 0.7  # Require approval for complex tasks
    dangerous_tools: ["file_delete", "database_write", "api_call"]
EOF

# Submit a complex task that triggers approval
./scripts/submit_task.sh "Delete all temporary files older than 30 days from /tmp"

# Workflow pauses and waits for human approval
# Check Temporal UI: http://localhost:8088
# Approve via signal: temporal workflow signal --workflow-id <ID> --name approval --input '{"approved":true}'
```
**Unique to Shannon**: Configurable approval workflows based on complexity scoring and tool usage.

</details>

<details>
<summary><b>Example 6: Multi-Agent Memory & Learning</b></summary>

```bash
# Agent learns from conversation and applies knowledge
SESSION="learning-session-$(date +%s)"

# Agent learns your preferences
./scripts/submit_task.sh "I prefer Python over Java for data science" "$SESSION"
./scripts/submit_task.sh "I like using pandas and numpy for analysis" "$SESSION"
./scripts/submit_task.sh "My projects usually involve machine learning" "$SESSION"

# Later, agent recalls and applies this knowledge
./scripts/submit_task.sh "What language and tools should I use for my new data project?" "$SESSION"
# Response includes personalized recommendations based on learned preferences
```
**Unique to Shannon**: Persistent memory with intelligent chunking (4 chars ≈ 1 token) and MMR diversity ranking.

</details>

<details>
<summary><b>Example 7: Supervisor Workflow with Dynamic Strategy</b></summary>

```bash
# Complex task automatically delegates to multiple specialized agents
./scripts/submit_task.sh "Analyze our website performance, identify bottlenecks, and create an optimization plan with specific recommendations"

# Watch the orchestration in real-time
curl -N "http://localhost:8080/api/v1/stream/sse?workflow_id=<WORKFLOW_ID>"

# Events show:
# - Complexity analysis (score: 0.85)
# - Strategy selection (supervisor pattern chosen)
# - Dynamic agent spawning (analyzer, investigator, planner)
# - Parallel execution with coordination
# - Synthesis and quality reflection
```
**Unique to Shannon**: Automatic workflow pattern selection based on task complexity.

</details>

<details>
<summary><b>Example 8: Time-Travel Debugging with State Inspection</b></summary>

```bash
# Production issue at 3am? Debug it step-by-step
FAILED_WORKFLOW="task-prod-failure-20250928-0300"

# Export with full state history
./scripts/replay_workflow.sh export $FAILED_WORKFLOW debug.json

# Inspect specific decision points
go run ./tools/replay -history debug.json -inspect-step 5

# Modify and test fix locally
go run ./tools/replay -history debug.json -override-activity GetLLMResponse

# Validate fix passes all historical workflows
make ci-replay
```
**Unique to Shannon**: Complete workflow state inspection and modification for debugging.

</details>

<details>
<summary><b>Example 9: Token Budget with Circuit Breakers</b></summary>

```bash
# Set strict budget with automatic fallbacks
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk_test_123456" \
  -d '{
    "query": "Generate a comprehensive market analysis report",
    "session_id": "budget-test",
    "config": {
      "budget": {
        "max_tokens": 5000,
        "fallback_model": "gpt-5-mini-2025-08-07",
        "circuit_breaker": {
          "threshold": 0.8,
          "cooldown_seconds": 60
        }
      }
    }
  }'

# System automatically:
# - Switches to cheaper model when 80% budget consumed
# - Implements cooldown period to prevent runaway costs
# - Returns partial results if budget exhausted
```
**Unique to Shannon**: Real-time budget enforcement with automatic degradation.

</details>

<details>
<summary><b>Example 10: Multi-Tenant Agent Isolation</b></summary>

```bash
# Each tenant gets isolated agents with separate policies
# Tenant A: Data Science team
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "X-API-Key: sk_tenant_a_key" \
  -H "X-Tenant-ID: data-science" \
  -d '{"query": "Train a model on our dataset"}'

# Tenant B: Customer Support
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "X-API-Key: sk_tenant_b_key" \
  -H "X-Tenant-ID: support" \
  -d '{"query": "Access customer database"}'  # Denied by OPA policy

# Complete isolation:
# - Separate memory/vector stores per tenant
# - Independent token budgets
# - Custom model access
# - Isolated session management
```
**Unique to Shannon**: Enterprise-grade multi-tenancy with OPA policy enforcement.

</details>

<details>
<summary><b>More Production Examples</b> (click to expand)</summary>

- **Incident Response Bot**: Auto-triages alerts with budget limits
- **Code Review Agent**: Enforces security policies via OPA rules
- **Data Pipeline Monitor**: Replays failed workflows for debugging
- **Compliance Auditor**: Full trace of every decision and data access
- **Multi-Tenant SaaS**: Complete isolation between customer agents

</details>

## 🏗️ Architecture

### High-Level Overview

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Client    │────▶│ Orchestrator │────▶│ Agent Core  │
└─────────────┘     │     (Go)     │     │   (Rust)    │
                    └──────────────┘     └─────────────┘
                           │                     │
                           ▼                     ▼
                    ┌──────────────┐     ┌─────────────┐
                    │   Temporal   │     │ WASI Tools  │
                    │   Workflows  │     │   Sandbox   │
                    └──────────────┘     └─────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │ LLM Service  │
                    │   (Python)   │
                    └──────────────┘
```

### Production Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLIENT LAYER                            │
├─────────────┬─────────────┬─────────────┬───────────────────────┤
│    HTTP     │    gRPC     │     SSE     │     WebSocket         │
└─────────────┴─────────────┴─────────────┴───────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      ORCHESTRATOR (Go)                          │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────┐   │
│  │   Router   │──│   Budget   │──│  Session   │──│   OPA    │   │
│  │            │  │  Manager   │  │   Store    │  │ Policies │   │
│  └────────────┘  └────────────┘  └────────────┘  └──────────┘   │
└─────────────────────────────────────────────────────────────────┘
        │                │                 │                │
        ▼                ▼                 ▼                ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│   Temporal   │ │    Redis     │ │  PostgreSQL  │ │   Qdrant     │
│  Workflows   │ │    Cache     │ │    State     │ │   Vectors    │
│              │ │   Sessions   │ │   History    │ │   Memory     │
└──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘
        │
        ▼
┌─────────────────────────────────────────────────────────────────┐
│                       AGENT CORE (Rust)                         │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────┐   │
│  │    WASI    │──│   Policy   │──│    Tool    │──│  Agent   │   │
│  │   Sandbox  │  │  Enforcer  │  │  Registry  │  │  Comms   │   │
│  └────────────┘  └────────────┘  └────────────┘  └──────────┘   │
└─────────────────────────────────────────────────────────────────┘
        │                                              │
        ▼                                              ▼
┌────────────────────────────────┐    ┌─────────────────────────────────┐
│     LLM SERVICE (Python)       │    │     OBSERVABILITY LAYER         │
│  ┌────────────┐ ┌────────────┐ │    │  ┌────────────┐ ┌────────────┐  │
│  │  Provider  │ │    MCP     │ │    │  │ Prometheus │ │  OpenTel   │  │
│  │  Adapter   │ │   Tools    │ │    │  │  Metrics   │ │  Traces    │  │
│  └────────────┘ └────────────┘ │    │  └────────────┘ └────────────┘  │
└────────────────────────────────┘    └─────────────────────────────────┘
```

### Core Components

- **Orchestrator (Go)**: Task routing, budget enforcement, session management, OPA policy evaluation
- **Agent Core (Rust)**: WASI sandbox execution, policy enforcement, agent-to-agent communication
- **LLM Service (Python)**: Provider abstraction (15+ LLMs), MCP tools, prompt optimization
- **Gateway (Go)**: REST API, authentication, rate limiting, request validation
- **Desktop App (Tauri/Next.js)**: Native macOS/iOS app for real-time monitoring and event streaming
- **Data Layer**: PostgreSQL (workflow state), Redis (session cache), Qdrant (vector memory)
- **Observability**: Native desktop app, Prometheus metrics, OpenTelemetry tracing

## 🚦 Getting Started for Production

### Day 1: Basic Setup
```bash
# Clone and configure
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon
make setup-env
echo "OPENAI_API_KEY=sk-..." >> .env

# Launch
make dev

# Configure budgets via config (recommended)
# See go/orchestrator/README.md (Budget Management) and config/models.yaml for limits/pricing.
# If you need per-call hints, include them under `context` and handle in your workflow/template.
```

### Day 2: Add Policies
```bash
# Create your first OPA policy
cat > config/opa/policies/default.rego << EOF
package shannon

default allow = false

# Allow all for dev, restrict in prod
allow {
    input.environment == "development"
}

# Production rules
allow {
    input.environment == "production"
    input.tokens_requested < 10000
    input.model in ["gpt-5-mini-2025-08-07", "claude-haiku-4-5-20251001"]
}
EOF

# Hot reload - no restart needed!
```

### Day 7: Debug Your First Issue
```bash
# Something went wrong in production?
# 1. Find the workflow ID from logs
grep ERROR logs/orchestrator.log | tail -1

# 2. Export the workflow
./scripts/replay_workflow.sh export task-xxx-failed debug.json

# 3. Replay locally to see exactly what happened
./scripts/replay_workflow.sh replay debug.json

# 4. Fix, test, deploy with confidence
```

### Configuration

Shannon uses a layered configuration system with clear precedence:

1. **Environment Variables** (`.env`) - Highest priority, for secrets and deployment-specific settings
2. **Docker Compose** - Service configurations and port mappings
3. **YAML Files** (`config/features.yaml`) - Feature flags and default settings

Key configuration files:
- `config/features.yaml` - Feature toggles, workflow settings, enforcement policies
- `config/models.yaml` - LLM provider configuration and pricing
- `.env` - API keys and runtime overrides (see `.env.example`)

#### LLM Response Caching
- What: Client-side response cache in the Python LLM service.
- Defaults: In‑memory LRU with TTL from `config/models.yaml` → `prompt_cache.ttl_seconds` (fallback 3600s).
- Distributed: Set `REDIS_URL` (or `REDIS_HOST`/`REDIS_PORT`/`REDIS_PASSWORD`) to enable Redis‑backed cache across instances.
- Keying: Deterministic hash of messages + key params (tier, model override, temperature, max_tokens, functions, seed).
- Behavior: Non‑streaming calls are cacheable; streaming uses cache to return the full result as a single chunk when available.

See: docs/llm-service-caching.md

For detailed configuration documentation, see [config/README.md](config/README.md).

### Architecture
- [Multi-Agent Workflow Architecture](docs/multi-agent-workflow-architecture.md)
- [Agent Core Architecture](docs/agent-core-architecture.md)
- [Pattern Selection Guide](docs/pattern-usage-guide.md)

### API & Integration
- [Agent Core API Reference](docs/agent-core-api.md)
- [Streaming APIs](docs/streaming-api.md)
- [Providers & Models](docs/providers-models.md)
- [Python WASI Setup](docs/python-wasi-setup.md)

## 🔧 Development

### Local Development

```bash
# Run linters and formatters
make lint
make fmt

# Run smoke tests
make smoke

# View logs
make logs

# Check service status
make ps
```
## 🤝 Contributing

We love contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

## 🌟 Community

- **Twitter/X**: [@shannon_agents](https://twitter.com/shannon_agents)

### What's Coming (Roadmap)

**Now → v0.1 (Production Ready)**
- ✅ **Core platform stable** - Go orchestrator, Rust agent-core, Python LLM service
- ✅ **Deterministic replay debugging** - Export and replay any workflow execution
- ✅ **OPA policy enforcement** - Fine-grained security and governance rules
- ✅ **WebSocket streaming** - Real-time agent communication with event filtering and replay
- ✅ **SSE streaming** - Server-sent events for browser-native streaming
- ✅ **WASI sandbox** - Secure code execution environment with resource limits
- ✅ **Multi-agent orchestration** - DAG, parallel, sequential, hybrid (dependency-based), ReAct, Tree-of-Thoughts, Chain-of-Thought, Debate, Reflection patterns
- ✅ **Vector memory** - Qdrant-based semantic search and context retrieval
- ✅ **Hierarchical memory** - Recent + semantic retrieval with deduplication and compression
- ✅ **Near-duplicate detection** - 95% similarity threshold to prevent redundant storage
- ✅ **Token-aware context management** - Configurable windows (5-200 msgs), smart selection, sliding window compression
- ✅ **Circuit breaker patterns** - Automatic failure recovery and degradation
- ✅ **Multi-provider LLM support** - OpenAI, Anthropic, Google, DeepSeek, and more
- ✅ **Token budget management** - Per-agent and per-task limits with validation
- ✅ **Session management** - Durable state with Redis/PostgreSQL persistence
- ✅ **Agent Coordination** - Direct agent-to-agent messaging, dynamic team formation, collaborative planning
- ✅ **MCP integration** - Model Context Protocol support for standardized tool interfaces
- ✅ **OpenAPI integration** - REST API tools with retry logic, circuit breaker, and ~70% API coverage
- ✅ **Provider abstraction layer** - Unified interface for adding new LLM providers with automatic fallback
- ✅ **Advanced Task Decomposition** - Recursive decomposition with ADaPT patterns, chain-of-thought planning, task template library
- ✅ **Composable workflows** - YAML-based workflow templates with declarative orchestration patterns
- ✅ **Unified Gateway & SDK** - REST API gateway, Python SDK (v0.2.0a1 on PyPI), CLI tool for easy adoption
- 🚧 **Ship Docker Images** - Pre-built docker release images, make setup staightforward

**v0.2**
- [ ] **TypeScript/JavaScript SDK** - npm package for Node.js and browser usage
- [ ] **(Optional) Drag and Drop UI** - AgentKit-like drag & drop UI to generate workflow yaml templates
- [ ] **Native tool expansion** - Additional Rust-native tools for file operations and system interactions
- [ ] **Advanced Memory** - Episodic rollups, entity/temporal knowledge graphs, hybrid dense+sparse retrieval
- [ ] **Advanced Learning** - Pattern recognition from successful workflows, contextual bandits for agent selection
- [ ] **Agent Collaboration Foundation** - Agent roles/personas, agent-specific memory, supervisor hierarchies
- [ ] **MMR diversity reranking** - Implement actual MMR algorithm for diverse retrieval (config ready, 40% done)
- [ ] **Performance-based agent selection** - Epsilon-greedy routing using agent_executions metrics
- [ ] **Context streaming events** - Add 4 new event types (CONTEXT_BUILDING, MEMORY_RECALL, etc.)
- [ ] **Budget enforcement in supervisor** - Pre-spawn validation and circuit breakers for multi-agent cost control
- [ ] **Use case presets** - YAML-based presets for debugging/analysis modes with preset selection logic
- [ ] **Debate outcome persistence** - Store consensus decisions in Qdrant for learning
- [ ] **Shared workspace functions** - Agent artifact sharing (AppendToWorkspace/ListWorkspaceItems)
- [ ] **Intelligent Tool Selection** - Semantic tool result caching, agent experience learning, performance-based routing
- [ ] **Native RAG System** - Document chunking service, knowledge base integration, context injection with source attribution
- [ ] **Team-level quotas & policies** - Per-team budgets, model/tool allowlists via config

**v0.3**
- [ ] **Solana Integration** - Decentralized trust, on-chain attestation, and blockchain-based audit trails for agent actions
- [ ] **Production Observability** - Distributed tracing, custom Grafana dashboards, SLO monitoring
- [ ] **Enterprise Features** - SSO integration, multi-tenant isolation, approval workflows
- [ ] **Edge Deployment** - WASM execution in browser, offline-first capabilities
- [ ] **Autonomous Intelligence** - Self-organizing agent swarms, critic/reflection loops, group chat coordination
- [ ] **Cross-Organization Federation** - Secure agent communication across tenants, capability negotiation protocols
- [ ] **Regulatory & Compliance** - SOC 2, GDPR, HIPAA automation with audit trails
- [ ] **AI Safety Frameworks** - Constitutional AI, alignment mechanisms, adversarial testing
- [ ] **Personalized Model Training** - Learn from each user's successful task patterns, fine-tune models on user-specific interactions, apply trained models during agent inference

## 📚 Documentation

- Official docs: https://shannon.kocoro.dev/en/
- Local docs folder: [docs/](docs/)

### Get Involved

- 🐛 **Found a bug?** [Open an issue](https://github.com/Kocoro-lab/Shannon/issues)
- 💡 **Have an idea?** [Start a discussion](https://github.com/Kocoro-lab/Shannon/discussions)
- ⭐ **Like the project?** Give us a star!

## 🔮 Coming Soon

### Solana Integration for Web3 Trust
We're building decentralized trust infrastructure with Solana blockchain:
- **Cryptographic Verification**: On-chain attestation of AI agent actions and results
- **Immutable Audit Trail**: Blockchain-based proof of task execution
- **Smart Contract Interoperability**: Enable AI agents to interact with DeFi and Web3 protocols
- **Token-Gated Capabilities**: Control agent permissions through blockchain tokens
- **Decentralized Reputation**: Build trust through verifiable on-chain agent performance

Stay tuned for our Web3 trust layer - bringing transparency and verifiability to AI systems!

## 📄 License

MIT License - Use it anywhere, modify anything, zero restrictions. See [LICENSE](LICENSE).

---

<p align="center">
  <b>Stop debugging AI failures. Start shipping reliable agents.</b><br><br>
  <a href="https://github.com/Kocoro-lab/Shannon">GitHub</a>
</p>

<p align="center">
  <i>If Shannon saves you time or money, let us know! We love success stories.</i><br>
  <i>Twitter/X: @shannon_agents</i>
</p>
