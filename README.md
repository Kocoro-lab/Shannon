# Shannon — Production AI Agents That Actually Work

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue.svg)](https://golang.org/)
[![Rust](https://img.shields.io/badge/Rust-stable-orange.svg)](https://www.rust-lang.org/)
[![Docker](https://img.shields.io/badge/Docker-required-blue.svg)](https://www.docker.com/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Battle-tested infrastructure for AI agents that solves the problems you hit at scale: **runaway costs**, **non-deterministic failures**, and **security nightmares**.

```bash
pip install shannon-sdk
```

```python
from shannon import ShannonClient

with ShannonClient(base_url="http://localhost:8080") as client:
    result = client.submit_task("Analyze tesla quarter sales data")
    print(client.wait(result.task_id).result)
```

<div align="center">

![Shannon Desktop App](docs/images/desktop-demo.gif)

*Native desktop app showing real-time agent execution and event streams*

</div>

## Why Shannon?

| The Problem | Shannon's Solution |
|---------|-------------------|
| _Agents fail silently?_ | Temporal workflows with time-travel debugging — replay any execution step-by-step |
| _Costs spiral out of control?_ | Hard token budgets per task/agent with automatic model fallback |
| _No visibility into what happened?_ | Real-time dashboard, Prometheus metrics, OpenTelemetry tracing |
| _Security concerns?_ | WASI sandbox for code execution, OPA policies, multi-tenant isolation |
| _Vendor lock-in?_ | Works with OpenAI, Anthropic, Google, DeepSeek, local models |

## Quick Start

### Prerequisites
- Docker and Docker Compose
- An API key for at least one LLM provider (OpenAI, Anthropic, etc.)

### Setup

```bash
git clone https://github.com/Kocoro-lab/Shannon.git && cd Shannon

make setup                              # Creates .env, generates proto files
echo "OPENAI_API_KEY=sk-..." >> .env    # Add your API key
./scripts/setup_python_wasi.sh          # Download Python WASI interpreter (20MB)

make dev    # Start all services
make smoke  # Verify everything works
```

> **Platform-specific guides:** [Ubuntu](docs/ubuntu-quickstart.md) · [Rocky Linux](docs/rocky-linux-quickstart.md) · [Windows](docs/windows-setup-guide-en.md) · [Windows (中文)](docs/windows-setup-guide-cn.md)

### Submit Your First Task

```bash
# REST API
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "What is the capital of France?", "session_id": "demo"}'

# Or use the CLI
shannon submit "What is the capital of France?"

# Stream events in real-time
curl -N "http://localhost:8080/api/v1/stream/sse?workflow_id=<WORKFLOW_ID>"
```

## Built-in Tools

Shannon includes production-ready tools out of the box:

| Tool | Description | API Key Required |
|------|-------------|------------------|
| **Web Search** | Google, Bing, or Serper search | Yes (see below) |
| **Web Fetch** | Deep content extraction with JS rendering | Optional (Firecrawl recommended) |
| **Web Crawl** | Multi-page website exploration | Yes (Firecrawl) |
| **Calculator** | Mathematical computations | No |
| **Python Executor** | Secure code execution in WASI sandbox | No |
| **File Operations** | Read/write workspace files | No |

### Configuring Tool API Keys

Add these to your `.env` file based on which tools you need:

```bash
# Web Search (choose one provider)
WEB_SEARCH_PROVIDER=serper              # serper | serpapi | google | bing | exa
SERPER_API_KEY=your-serper-key          # serper.dev (recommended, easy setup)
# OR
SERPAPI_API_KEY=your-serpapi-key        # serpapi.com
# OR
GOOGLE_SEARCH_API_KEY=your-google-key   # Google Custom Search
GOOGLE_SEARCH_ENGINE_ID=your-engine-id

# Web Fetch/Crawl (for deep research)
WEB_FETCH_PROVIDER=firecrawl            # firecrawl | exa | python
FIRECRAWL_API_KEY=your-firecrawl-key    # firecrawl.dev (recommended for production)
```

> **Tip:** For quick setup, just add `SERPER_API_KEY` — it's the fastest way to enable web search. Get a key at [serper.dev](https://serper.dev).

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

| Capability | Shannon | LangGraph | AutoGen | CrewAI |
|-----------|---------|-----------|---------|--------|
| **Deterministic Replay** | ✅ Time-travel debugging | ❌ | ❌ | ❌ |
| **Token Budget Limits** | ✅ Hard caps + fallback | ❌ | ❌ | ❌ |
| **Security Sandbox** | ✅ WASI isolation | ❌ | ❌ | ❌ |
| **OPA Policy Control** | ✅ Fine-grained rules | ❌ | ❌ | ❌ |
| **Production Metrics** | ✅ Dashboard/Prometheus | ⚠️ DIY | ❌ | ❌ |
| **Multi-Language** | ✅ Go/Rust/Python | ⚠️ Python only | ⚠️ Python only | ⚠️ Python only |
| **Session Persistence** | ✅ Redis-backed | ⚠️ In-memory | ⚠️ Limited | ❌ |
| **Multi-Agent Orchestration** | ✅ DAG/Supervisor | ✅ Graphs | ✅ Group chat | ✅ Crews |

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

| Resource | Description |
|----------|-------------|
| [Official Docs](https://shannon.kocoro.dev/en/) | Full documentation site |
| [Architecture](docs/multi-agent-workflow-architecture.md) | System design deep-dive |
| [API Reference](docs/agent-core-api.md) | Agent Core API |
| [Streaming APIs](docs/streaming-api.md) | SSE and WebSocket streaming |
| [Python Execution](docs/python-code-execution.md) | WASI sandbox guide |
| [Adding Tools](docs/adding-custom-tools.md) | Custom tool development |

## Development

```bash
make lint   # Run linters
make fmt    # Format code
make smoke  # E2E tests
make logs   # View logs
make ps     # Service status
```

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

- [Open an issue](https://github.com/Kocoro-lab/Shannon/issues) — Bug reports
- [Start a discussion](https://github.com/Kocoro-lab/Shannon/discussions) — Ideas and questions
- [View roadmap](ROADMAP.md) — What's coming next

## License

MIT License — Use it anywhere, modify anything. See [LICENSE](LICENSE).

---

<p align="center">
  <b>Stop debugging AI failures. Start shipping reliable agents.</b><br><br>
  <a href="https://github.com/Kocoro-lab/Shannon">GitHub</a> ·
  <a href="https://shannon.kocoro.dev/en/">Docs</a> ·
  <a href="https://twitter.com/shannon_agents">Twitter</a>
</p>
