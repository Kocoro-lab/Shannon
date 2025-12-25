# Shannon — Production AI Agents That Actually Work

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Documentation](https://img.shields.io/badge/docs-shannon.run-blue.svg)](https://docs.shannon.run)
[![Docker Hub](https://img.shields.io/badge/Docker%20Hub-waylandzhang%2Fshannon-blue.svg)](https://hub.docker.com/u/waylandzhang)
[![Version](https://img.shields.io/badge/version-v0.1.0-green.svg)](https://github.com/Kocoro-lab/Shannon/releases)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue.svg)](https://golang.org/)
[![Rust](https://img.shields.io/badge/Rust-stable-orange.svg)](https://www.rust-lang.org/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Battle-tested infrastructure for AI agents that solves the problems you hit at scale: **runaway costs**, **non-deterministic failures**, and **security nightmares**.

<div align="center">


![Shannon Desktop App](docs/images/desktop-demo.gif)

*Native desktop app showing real-time agent execution and event streams*

</div>

## Why Shannon?

| The Problem                         | Shannon's Solution                                           |
| ----------------------------------- | ------------------------------------------------------------ |
| _Agents fail silently?_             | Temporal workflows with time-travel debugging — replay any execution step-by-step |
| _Costs spiral out of control?_      | Hard token budgets per task/agent with automatic model fallback |
| _No visibility into what happened?_ | Real-time dashboard, Prometheus metrics, OpenTelemetry tracing |
| _Security concerns?_                | WASI sandbox for code execution, OPA policies, multi-tenant isolation |
| _Vendor lock-in?_                   | Works with OpenAI, Anthropic, Google, DeepSeek, local models |

## Quick Start

### Prerequisites

- Docker and Docker Compose
- An API key for at least one LLM provider (OpenAI, Anthropic, etc.)

### Installation

Choose your preferred installation method:

#### Option 1: Quick Install (Recommended)

One-command installation with interactive setup:

```bash
curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/Shannon/v0.1.0/scripts/install.sh | bash
```

This script will:
- Download production configuration
- Prompt for API keys interactively
- Pull Docker images and start services
- Verify everything is running

#### Option 2: Manual Install

For users who prefer manual control:

```bash
# Clone repository (or download specific release)
git clone --depth 1 --branch v0.1.0 https://github.com/Kocoro-lab/Shannon.git
cd Shannon

# Configure environment
cp .env.example .env
nano .env  # Add your API keys

# Start services (automatically uses latest images)
docker compose -f deploy/compose/docker-compose.release.yml up -d

# Verify services
docker compose -f deploy/compose/docker-compose.release.yml ps
```

**Required API Keys (choose one):**
- OpenAI: `OPENAI_API_KEY=sk-...`
- Anthropic: `ANTHROPIC_API_KEY=sk-ant-...`
- Or any OpenAI-compatible endpoint

**Optional but recommended:**
- Web Search: `SERPAPI_API_KEY=...` (get key at [serpapi.com](https://serpapi.com))

> **For Contributors:** Want to build from source? See [Development Setup](#development) below.
>
> **Platform-specific guides:** [Ubuntu](docs/ubuntu-quickstart.md) · [Rocky Linux](docs/rocky-linux-quickstart.md) · [Windows](docs/windows-setup-guide-en.md) · [Windows (中文)](docs/windows-setup-guide-cn.md)

### Your First Agent

Shannon provides multiple ways to interact with AI agents. Choose the option that works best for you:

#### Option 1: Web UI (Local Development)

The fastest way to try Shannon — run the desktop app as a local web server:

```bash
# In a new terminal (backend should already be running)
cd desktop
npm install
npm run dev

# Open http://localhost:3000 in your browser
```

**Perfect for:**
- Quick testing and exploration
- Development and debugging
- Real-time event streaming visualization

#### Option 2: Native Desktop App

Download pre-built desktop applications from [GitHub Releases](https://github.com/Kocoro-lab/Shannon/releases/latest):

- **[macOS (Universal)](https://github.com/Kocoro-lab/Shannon/releases/latest)** — Intel & Apple Silicon
- **[Windows (x64)](https://github.com/Kocoro-lab/Shannon/releases/latest)** — MSI or EXE installer
- **[Linux (x64)](https://github.com/Kocoro-lab/Shannon/releases/latest)** — AppImage or DEB package

Or build from source:

```bash
cd desktop
npm install
npm run tauri:build  # Builds for your platform
```

**Native app benefits:**
- System tray integration and native notifications
- Offline task history (Dexie.js local database)
- Better performance and lower memory usage
- Auto-updates from GitHub releases

See [Desktop App Guide](desktop/README.md) for more details.

#### Option 3: REST API

Use Shannon's HTTP REST API directly. For complete API documentation, see **[docs.shannon.run](https://docs.shannon.run)**.

```bash
# Submit a task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is the capital of France?",
    "session_id": "demo-session"
  }'

# Response: {"task_id":"task-dev-123","status":"running"}

# Stream events in real-time
curl -N "http://localhost:8080/api/v1/stream/sse?workflow_id=task-dev-123"

# Get final result
curl "http://localhost:8080/api/v1/tasks/task-dev-123"
```

**Perfect for:**
- Integrating Shannon into existing applications
- Automation scripts and workflows
- Language-agnostic integration

#### Option 4: Python SDK

Install the official Shannon Python SDK:

```bash
pip install shannon-sdk
```

```python
from shannon import ShannonClient

# Create client
with ShannonClient(base_url="http://localhost:8080") as client:
    # Submit task
    handle = client.submit_task(
        "What is the capital of France?",
        session_id="demo-session"
    )

    # Wait for completion
    result = client.wait(handle.task_id)
    print(result.result)
```

CLI is also available:

```bash
shannon submit "What is the capital of France?"
```

**Perfect for:**
- Python-based applications and notebooks
- Data science workflows
- Batch processing and automation

See [Python SDK Documentation](https://pypi.org/project/shannon-sdk/) for the full API reference.

### Configuring Tool API Keys

Add these to your `.env` file based on which tools you need:

```bash
# Web Search (choose one provider)
WEB_SEARCH_PROVIDER=serpapi             # serpapi | google | bing | exa
SERPAPI_API_KEY=your-serpapi-key        # serpapi.com
# OR
GOOGLE_SEARCH_API_KEY=your-google-key   # Google Custom Search
GOOGLE_SEARCH_ENGINE_ID=your-engine-id

# Web Fetch/Crawl (for deep research)
WEB_FETCH_PROVIDER=firecrawl            # firecrawl | exa | python
FIRECRAWL_API_KEY=your-firecrawl-key    # firecrawl.dev (recommended for production)
```

> **Tip:** For quick setup, just add `SERPAPI_API_KEY`. Get a key at [serpapi.com](https://serpapi.com).

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Client    │────▶│ Orchestrator │────▶│ Agent Core  │
│  (SDK/API)  │     │     (Go)     │     │   (Rust)    │
└─────────────┘     └──────────────┘     └─────────────┘
                           │                    │
                    ┌──────┴──────┐      ┌──────┴──────┐
                    │  Temporal   │      │    WASI     │
                    │  Workflows  │      │   Sandbox   │
                    └─────────────┘      └─────────────┘
                           │
                    ┌──────┴──────┐
                    │ LLM Service │
                    │  (Python)   │
                    └─────────────┘
```

**Components:**

- **Orchestrator (Go)** — Task routing, budget enforcement, session management, OPA policies
- **Agent Core (Rust)** — WASI sandbox, policy enforcement, agent-to-agent communication
- **LLM Service (Python)** — Provider abstraction (15+ LLMs), MCP tools, prompt optimization
- **Data Layer** — PostgreSQL (state), Redis (sessions), Qdrant (vector memory)

## Core Capabilities

### OpenAI-Compatible API
```bash
# Drop-in replacement for OpenAI API
export OPENAI_API_BASE=http://localhost:8080/v1
# Your existing OpenAI code works unchanged
```

### 15+ LLM Providers
- **OpenAI**: GPT-4, GPT-3.5, GPT-4 Turbo
- **Anthropic**: Claude 3 Opus/Sonnet/Haiku, Claude 3.5 Sonnet
- **Google**: Gemini Pro, Gemini Ultra
- **DeepSeek**: DeepSeek Chat, DeepSeek Coder
- **Local Models**: Ollama, LM Studio, vLLM
- Automatic failover between providers

### Scheduled Tasks
```bash
# Run tasks on a schedule (cron syntax)
curl -X POST http://localhost:8080/api/v1/schedules \
  -d '{
    "name": "Daily Market Analysis",
    "cron": "0 9 * * *",
    "task_query": "Analyze market trends",
    "max_budget_per_run_usd": 0.50
  }'
```

### Research Workflows
Multiple research strategies for different use cases:
- **Quick**: Fast searches with small models
- **Standard**: Balanced quality and cost
- **Deep**: Multi-step research with synthesis
- **Academic**: Citation-focused research
- **Exploratory**: Tree-of-thoughts exploration

### MCP Integration
Native support for Model Context Protocol:
- Custom tool registration
- OAuth2 server authentication
- Rate limiting and circuit breakers
- Cost tracking for MCP tool usage

### Native Desktop Apps
- **macOS**: Native app with system integration
- **iOS**: Mobile agent execution
- Real-time event streaming
- Workflow visualization

## Key Features

### Time-Travel Debugging

```bash
# Production agent failed? Replay it locally step-by-step
./scripts/replay_workflow.sh task-prod-failure-123

# Output shows every decision, tool call, and state change
```

### Token Budget Control

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Generate a market analysis report",
    "config": {
      "budget": {
        "max_tokens": 5000,
        "fallback_model": "gpt-5-mini-2025-08-07"
      }
    }
  }'
# Automatically switches to cheaper model when 80% budget consumed
```

### OPA Policy Governance

```rego
# config/opa/policies/teams.rego
package shannon.teams

allow {
    input.team == "data-science"
    input.model in ["gpt-5-2025-08-07", "claude-sonnet-4-5-20250929"]
}

deny_tool["database_write"] {
    input.team == "support"
}
```

### Secure Code Execution

```bash
# Python runs in isolated WASI sandbox — no network, read-only FS
./scripts/submit_task.sh "Execute Python: import os; os.system('rm -rf /')"
# Result: OSError - system calls blocked by WASI sandbox
```

## Shannon vs. Alternatives

| Capability                    | Shannon                      | LangGraph        | Dify             | AutoGen          | CrewAI           |
| ----------------------------- | ---------------------------- | ---------------- | ---------------- | ---------------- | ---------------- |
| **Scheduled Tasks**           | ✅ Cron-based workflows       | ❌                | ⚠️ Basic          | ❌                | ❌                |
| **Research Workflows**        | ✅ Multi-strategy (5 types)   | ⚠️ Manual setup   | ⚠️ Manual setup   | ⚠️ Manual setup   | ⚠️ Manual setup   |
| **Deterministic Replay**      | ✅ Time-travel debugging      | ❌                | ❌                | ❌                | ❌                |
| **Token Budget Limits**       | ✅ Hard caps + auto-fallback  | ❌                | ❌                | ❌                | ❌                |
| **Security Sandbox**          | ✅ WASI isolation             | ❌                | ❌                | ❌                | ❌                |
| **OPA Policy Control**        | ✅ Fine-grained governance    | ❌                | ❌                | ❌                | ❌                |
| **Production Metrics**        | ✅ Dashboard/Prometheus       | ⚠️ DIY            | ⚠️ Basic          | ❌                | ❌                |
| **Native Desktop Apps**       | ✅ macOS/iOS                  | ❌                | ❌                | ❌                | ❌                |
| **Multi-Language Core**       | ✅ Go/Rust/Python             | ⚠️ Python only    | ⚠️ Python only    | ⚠️ Python only    | ⚠️ Python only    |
| **Session Persistence**       | ✅ Redis-backed               | ⚠️ In-memory      | ✅ Database        | ⚠️ Limited        | ❌                |
| **Multi-Agent Orchestration** | ✅ DAG/Supervisor/Strategies  | ✅ Graphs         | ⚠️ Workflows      | ✅ Group chat     | ✅ Crews          |

## Built for Enterprise

- **Multi-Tenant Isolation** — Separate memory, budgets, and policies per tenant
- **Human-in-the-Loop** — Configurable approval workflows for sensitive operations
- **Audit Trail** — Complete trace of every decision and data access
- **On-Premise Ready** — No cloud dependencies, runs entirely in your infrastructure

```bash
# Multi-tenant example
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "X-API-Key: sk_tenant_a_key" \
  -H "X-Tenant-ID: data-science" \
  -d '{"query": "Train a model on our dataset"}'
```

## Configuration

Shannon uses layered configuration:

1. **Environment Variables** (`.env`) — API keys, secrets
2. **YAML Files** (`config/`) — Feature flags, model pricing, policies

Key files:

- `config/models.yaml` — LLM providers, pricing, tier configuration
- `config/features.yaml` — Feature toggles, workflow settings
- `config/opa/policies/` — Access control rules

See [Configuration Guide](config/README.md) for details.

## Documentation

| Resource                                                  | Description                 |
| --------------------------------------------------------- | --------------------------- |
| [Official Docs](https://docs.shannon.run)                 | Full documentation site     |
| [Architecture](docs/multi-agent-workflow-architecture.md) | System design deep-dive     |
| [API Reference](docs/agent-core-api.md)                   | Agent Core API              |
| [Streaming APIs](docs/streaming-api.md)                   | SSE and WebSocket streaming |
| [Python Execution](docs/python-code-execution.md)         | WASI sandbox guide          |
| [Adding Tools](docs/adding-custom-tools.md)               | Custom tool development     |

## Development

### Building from Source

Contributors can build and run Shannon locally from source:

```bash
# Clone the repository
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon

# Setup development environment
make setup                              # Creates .env, generates proto files
echo "OPENAI_API_KEY=sk-..." >> .env    # Add your API key
./scripts/setup_python_wasi.sh          # Download Python WASI interpreter (20MB)

# Start all services (builds locally)
make dev

# Run tests
make smoke  # E2E smoke tests
make ci     # Full CI suite
```

### Development Commands

```bash
make lint   # Run linters (Go, Rust, Python)
make fmt    # Format code
make proto  # Regenerate proto files
make logs   # View service logs
make ps     # Service status
make down   # Stop all services
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development guidelines.

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

- [Open an issue](https://github.com/Kocoro-lab/Shannon/issues) — Bug reports and questions
- [View roadmap](ROADMAP.md) — What's coming next

## License

MIT License — Use it anywhere, modify anything. See [LICENSE](LICENSE).

---

<p align="center">
  <b>Stop debugging AI failures. Start shipping reliable agents.</b><br><br>
  <a href="https://github.com/Kocoro-lab/Shannon">GitHub</a> ·
  <a href="https://docs.shannon.run">Docs</a> ·
  <a href="https://x.com/shannon_agents">X</a>
</p>
