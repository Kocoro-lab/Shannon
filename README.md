# Shannon — Production AI Agents That Actually Work

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Documentation](https://img.shields.io/badge/docs-shannon.run-blue.svg)](https://docs.shannon.run)
[![Docker Hub](https://img.shields.io/badge/Docker%20Hub-waylandzhang%2Fshannon-blue.svg)](https://hub.docker.com/u/waylandzhang)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org/)
[![Rust](https://img.shields.io/badge/Rust-stable-orange.svg)](https://www.rust-lang.org/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Ship reliable AI agents to production. Multi-strategy orchestration, swarm collaboration, token budget control, human approval workflows, and time-travel debugging — all built in. **<a href="https://shannon.run" target="_blank">Live Demo</a>**

<div align="center">

![Shannon Desktop App](docs/images/desktop-demo.gif)

*View real-time agent execution and event streams*

</div>

<div align="center">

![Shannon Architecture](docs/images/architecture-oss.png)

*Multi-agent orchestration with execution strategies, WASI sandboxing, and built-in observability*

</div>

## Why Shannon?

| The Problem | Shannon's Solution |
|---|---|
| *Agents fail silently?* | Temporal workflows with time-travel debugging — replay any execution step-by-step |
| *Costs spiral out of control?* | Hard token budgets per task/agent with automatic model fallback |
| *No visibility into what happened?* | Real-time event streaming, Prometheus metrics, OpenTelemetry tracing |
| *Security concerns?* | WASI sandbox for code execution, OPA policies, multi-tenant isolation |
| *Vendor lock-in?* | Works with OpenAI, Anthropic, Google, DeepSeek, xAI, local models via Ollama |

## Quick Start

### Prerequisites

- Docker and Docker Compose
- An API key for at least one LLM provider (OpenAI, Anthropic, etc.)

### One-Command Install

```bash
curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/Shannon/main/scripts/install.sh | bash
```

This downloads config, prompts for API keys, pulls Docker images, and starts services.

**Required API Keys** (choose one):
- OpenAI: `OPENAI_API_KEY=sk-...`
- Anthropic: `ANTHROPIC_API_KEY=sk-ant-...`
- Or any OpenAI-compatible endpoint

**Optional but recommended:**
- Web Search: `SERPAPI_API_KEY=...` ([serpapi.com](https://serpapi.com))
- Web Fetch: `FIRECRAWL_API_KEY=...` ([firecrawl.dev](https://firecrawl.dev))

> **Building from source?** See [Development](#development) below.
>
> **Platform-specific guides:** [Ubuntu](docs/ubuntu-quickstart.md) | [Rocky Linux](docs/rocky-linux-quickstart.md) | [Windows](docs/windows-setup-guide-en.md) | [Windows (中文)](docs/windows-setup-guide-cn.md)

### Your First Agent

Shannon provides multiple ways to interact with AI agents:

#### REST API

```bash
# Submit a task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "What is the capital of France?", "session_id": "demo"}'

# Stream events in real-time
curl -N "http://localhost:8080/api/v1/stream/sse?workflow_id=<task_id>"
```

#### Python SDK

```bash
pip install shannon-sdk
```

```python
from shannon import ShannonClient

with ShannonClient(base_url="http://localhost:8080") as client:
    handle = client.submit_task("What is the capital of France?", session_id="demo")
    result = client.wait(handle.task_id)
    print(result.result)
```

See [Python SDK Documentation](https://pypi.org/project/shannon-sdk/).

#### OpenAI-Compatible API

```bash
# Drop-in replacement for OpenAI API
export OPENAI_API_BASE=http://localhost:8080/v1
# Your existing OpenAI code works unchanged
```

#### Desktop App

Download from [GitHub Releases](https://github.com/Kocoro-lab/Shannon/releases/latest) (macOS, Windows, Linux), or build from source:

```bash
cd desktop && npm install && npm run tauri:build
```

See [Desktop App Guide](desktop/README.md).

## Architecture

```
Client --> Gateway (Go) --> Orchestrator (Go) --> Agent Core (Rust) --> LLM Service (Python) --> Providers
             |                    |                      |                      |
             | Auth/Rate limit    | Temporal workflows    | WASI sandbox         | Tool execution
             |                    | Budget management     | Token enforcement    | Agent loop
             |                    | Complexity routing    | Circuit breaker      | Context management
             v                    v                       v                      v
           PostgreSQL          Temporal                 Redis              Tool Adapters
```

### Core Services

| Service | Language | Port | Role |
|---------|----------|------|------|
| **Gateway** | Go | 8080 | REST API, auth (JWT/API key), rate limiting |
| **Orchestrator** | Go | 50052 | Temporal workflows, task decomposition, budget management |
| **Agent Core** | Rust | 50051 | Enforcement gateway, WASI sandbox, token counting |
| **LLM Service** | Python | 8000 | Provider abstraction, MCP tools, agent loop |
| **Playwright** | Python | 8002 | Browser automation for web scraping |

### Execution Strategies

Tasks are automatically routed based on complexity:

| Strategy | Trigger | Use Case |
|----------|---------|----------|
| **Simple** | Complexity < 0.3 | Single-agent, direct response |
| **DAG** | Multi-step tasks (default) | Fan-out/fan-in with dependency tracking |
| **ReAct** | Iterative reasoning | Reasoning + tool use loops |
| **Research** | Multi-step research | Tiered models for cost optimization (50-70% reduction) |
| **Exploratory** | Tree-of-Thoughts | Parallel hypothesis exploration |
| **Browser Use** | Web interaction tasks | Playwright-backed browsing agent |
| **Domain Analysis** | Specialized analysis | Domain-specific deep research |
| **Swarm** | Autonomous teams | Lead-orchestrated multi-agent with convergence detection |

## Core Capabilities

### Research Workflows
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Compare renewable energy adoption in EU vs US",
    "context": {"force_research": true, "research_strategy": "deep"}
  }'
```

### Swarm Multi-Agent
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Analyze this dataset from multiple perspectives",
    "context": {"force_swarm": true}
  }'
```

### Skills System
```bash
curl http://localhost:8080/api/v1/skills           # List skills
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "Review the auth module", "skill": "code-review", "session_id": "review-123"}'
```

Create custom skills in `config/skills/user/` (create the directory if it doesn't exist — it's gitignored). See [Skills System](docs/skills-system.md).

### Human-in-the-Loop Approval

Enable approval gates via OPA policy or workflow templates with `require_approval: true`. Approvals route to connected daemon clients via WebSocket.

```bash
# Submit an approval decision
curl -X POST http://localhost:8080/api/v1/approvals/decision \
  -H "Content-Type: application/json" \
  -d '{"workflow_id": "<workflow_id>", "approval_id": "<approval_id>", "approved": true}'
```

### Session Continuity
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "What is GDP?", "session_id": "econ-101"}'
# Follow-up remembers context
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "How does it relate to inflation?", "session_id": "econ-101"}'
```

### Scheduled Tasks
```bash
curl -X POST http://localhost:8080/api/v1/schedules \
  -H "Content-Type: application/json" \
  -d '{"name": "Daily Analysis", "cron_expression": "0 9 * * *", "task_query": "Analyze market trends"}'
```

### Token Budget Control
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Generate a market report",
    "context": {"budget_max": 5000}
  }'
```

### Time-Travel Debugging
```bash
./scripts/replay_workflow.sh task-prod-failure-123
```

### 10+ LLM Providers
- **Anthropic**: Claude Opus 4.6, Sonnet 4.6, Haiku 4.5
- **OpenAI**: GPT-5.1, GPT-5 mini, GPT-5 nano
- **Google**: Gemini 2.5 Pro, Gemini 2.5 Flash, Gemini 3 Pro Preview
- **xAI**: Grok 4 (reasoning & non-reasoning), Grok 3 Mini
- **DeepSeek**: DeepSeek Chat, DeepSeek Reasoner
- **MiniMax**: M2.7, M2.7-highspeed
- **Groq**: Llama, Mixtral (ultra-fast inference)
- **Others**: Qwen, Meta (Llama 4), Zhipu (GLM), Kimi
- **Local**: Ollama, LM Studio, vLLM — any OpenAI-compatible endpoint
- Automatic failover between providers

## API Endpoints

| Endpoint | Orchestrator? | Format | Use Case |
|----------|:---:|--------|----------|
| `POST /v1/chat/completions` | Yes | OpenAI-compatible | Apps using OpenAI SDK |
| `POST /v1/completions` | No (proxy) | OpenAI-compatible | Thin LLM proxy |
| `POST /api/v1/tasks` | Yes | Shannon native (sync) | Full orchestrator pipeline |
| `POST /api/v1/tasks/stream` | Yes | Shannon native (SSE) | Streaming orchestration |

**Tool Execution:** `GET /api/v1/tools`, `POST /api/v1/tools/{name}/execute`

**Auth:** `GET /api/v1/auth/me`, `POST /api/v1/auth/refresh-key`, `GET/POST/DELETE /api/v1/auth/api-keys`

**Sessions:** `GET/PATCH/DELETE /api/v1/sessions/{id}`, history, events, files

**Task Control:** `POST /api/v1/tasks/{id}/cancel|pause|resume`, `GET .../control-state|events|timeline`

**Schedules:** Full CRUD at `/api/v1/schedules`, plus `pause`, `resume`, `runs`

**Real-Time:** `GET /api/v1/stream/sse`, `GET /api/v1/stream/ws`, `WS /v1/ws/messages` (daemon)

## Project Structure

```
shannon/
├── go/orchestrator/          # Temporal workflows, budget manager, gateway
│   ├── cmd/gateway/          # REST API gateway (auth, rate limiting)
│   └── internal/             # Workflows, strategies, activities
├── rust/agent-core/          # Enforcement gateway, WASI sandbox
├── python/llm-service/       # LLM providers, MCP tools, agent loop
├── desktop/                  # Tauri + Next.js desktop application
├── clients/python/           # Python SDK
├── protos/                   # Shared protobuf definitions
├── config/                   # YAML configuration files
├── deploy/compose/           # Docker Compose for local dev + release
├── migrations/               # PostgreSQL schema migrations
├── scripts/                  # Automation and helper scripts
└── docs/                     # Architecture and API documentation
```

### Configuration

| File | Purpose |
|------|---------|
| `.env` | API keys, runtime settings |
| `config/shannon.yaml` | Feature flags, auth, tracing |
| `config/models.yaml` | LLM providers, pricing, capabilities |
| `config/features.yaml` | Workflow settings, execution modes |
| `config/openai_models.yaml` | Custom `shannon-*` model names for OpenAI-compatible API |
| `config/research_strategies.yaml` | Research strategy model tiers |

### Service Ports (Local Development)

| Service | Port | Protocol |
|---------|------|----------|
| Gateway | 8080 | HTTP |
| Orchestrator | 50052 (gRPC), 8081 (health) | gRPC/HTTP |
| Agent Core | 50051 | gRPC |
| LLM Service | 8000 | HTTP |
| Temporal | 7233 (gRPC), 8088 (UI) | gRPC/HTTP |
| PostgreSQL | 5432 | TCP |
| Redis | 6379 | TCP |

## Development

### Building from Source

```bash
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon
make setup                              # Create .env + generate protobuf stubs
vim .env                                # Add your API key
./scripts/setup_python_wasi.sh          # Download Python WASI interpreter
make dev                                # Start all services
make smoke                              # Run E2E smoke tests
```

### Development Commands

```bash
make dev      # Start all services
make smoke    # E2E smoke tests
make ci       # Full CI suite
make proto    # Regenerate protobuf files
make lint     # Run linters
make logs     # View service logs
make down     # Stop all services
```

### Browser Automation (Optional)

The Playwright service enables browser automation workflows (3.4GB image with Chromium). It is **not started by default**. To enable it:

```bash
# Development
docker compose -f deploy/compose/docker-compose.yml --profile browser up -d

# Pre-built images
docker compose -f deploy/compose/docker-compose.release.yml --profile browser up -d
```

Test it with:
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query":"Go to https://waylandz.com and get the page title","session_id":"test","context":{"role":"browser_use"}}'
```

### Using Pre-built Images

```bash
cd Shannon
cp .env.example .env && vim .env
docker compose -f deploy/compose/docker-compose.release.yml up -d
```

## Troubleshooting

### Health Checks

```bash
docker compose -f deploy/compose/docker-compose.yml ps
curl http://localhost:8080/health
curl http://localhost:8081/health
```

### Common Issues

**Services not starting:**
- Check `.env` has required API keys
- Ensure ports 8080, 8081, 50052 are not in use
- Run `docker compose down && docker compose up -d` to recreate

**Task execution fails:**
- Verify LLM API key is valid
- Check orchestrator logs: `docker compose logs -f orchestrator`

**Out of memory:**
- Reduce `WASI_MEMORY_LIMIT_MB` (default: 512)
- Lower `HISTORY_WINDOW_MESSAGES` (default: 50)


## FAQ

### What is Shannon?

Shannon is a **production-oriented multi-agent orchestration framework** built with Go, Rust, and Python. It focuses on reliability: Temporal workflows for fault tolerance, time-travel debugging, token budget control, human approval workflows, and WASI sandboxing for secure code execution.

### How is Shannon different from LangChain or CrewAI?

| Framework | Focus | Key Differentiator |
|-----------|-------|-------------------|
| **Shannon** | Production reliability | Temporal workflows, time-travel debugging, hard token budgets |
| **LangChain** | LLM chains & RAG | Chain-of-thought composition, retrieval augmentation |
| **CrewAI** | Role-playing agents | Multi-agent roles, task delegation |

Shannon is designed for **production deployment** with built-in observability, budget management, and workflow replay — not just prototyping.

### What execution strategies are available?

Shannon automatically routes tasks based on complexity:

- **Simple**: Single-agent direct response (complexity < 0.3)
- **DAG**: Fan-out/fan-in with dependency tracking (default for multi-step)
- **ReAct**: Reasoning + tool use loops
- **Research**: Tiered models for 50-70% cost reduction
- **Swarm**: Lead-orchestrated multi-agent teams
- **Browser Use**: Playwright-backed web interaction

### Which LLM providers are supported?

Shannon supports 10+ providers with automatic failover:

- **Anthropic**: Claude Opus 4.6, Sonnet 4.6, Haiku 4.5
- **OpenAI**: GPT-5.1, GPT-5 mini, GPT-5 nano
- **Google**: Gemini 2.5 Pro, Gemini 2.5 Flash
- **xAI**: Grok 4, Grok 3 Mini
- **DeepSeek**, **MiniMax**, **Groq**, **Qwen**, **Llama**, **Zhipu**, **Kimi**
- **Local**: Ollama, LM Studio, vLLM — any OpenAI-compatible endpoint

### How do I install Shannon?

**One-command install:**
```bash
curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/Shannon/main/scripts/install.sh | bash
```

**Prerequisites:**
- Docker and Docker Compose
- An API key for at least one LLM provider

### What is time-travel debugging?

Time-travel debugging lets you replay any workflow execution step-by-step to understand failures or unexpected behavior:
```bash
./scripts/replay_workflow.sh task-prod-failure-123
```

### How does token budget control work?

Set `budget_max` in context parameter per task. Shannon enforces hard token limits and automatically falls back to cheaper models if budgets approach exhaustion.

### Can I use Shannon with existing OpenAI SDKs?

Yes! Shannon provides an OpenAI-compatible API endpoint:
```bash
export OPENAI_API_BASE=http://localhost:8080/v1
# Your existing OpenAI code works unchanged
```

### Where can I get help?

- **Documentation**: [docs.shannon.run](https://docs.shannon.run)
- **GitHub Issues**: [github.com/Kocoro-lab/Shannon/issues](https://github.com/Kocoro-lab/Shannon/issues)
- **Live Demo**: [shannon.run](https://shannon.run)

## Documentation

| Resource | Description |
|----------|-------------|
| [Official Docs](https://docs.shannon.run) | Full documentation site |
| [Architecture](docs/multi-agent-workflow-architecture.md) | System design deep-dive |
| [Streaming APIs](docs/streaming-api.md) | SSE and WebSocket streaming |
| [Skills System](docs/skills-system.md) | Custom skill development |
| [Session Workspaces](docs/session-workspaces.md) | WASI sandbox guide |
| [Extending Shannon](docs/extending-shannon.md) | Custom tools and templates |
| [Swarm Agents](docs/swarm-agents.md) | Multi-agent collaboration |

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

- [Open an issue](https://github.com/Kocoro-lab/Shannon/issues)
- [View roadmap](ROADMAP.md)

## License

MIT License — Use it anywhere, modify anything. See [LICENSE](LICENSE).

---

<p align="center">
  <b>Stop debugging AI failures. Start shipping reliable agents.</b><br><br>
  <a href="https://github.com/Kocoro-lab/Shannon">GitHub</a> ·
  <a href="https://docs.shannon.run">Docs</a>
</p>
