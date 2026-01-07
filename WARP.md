# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

Shannon is a production-grade AI agent platform designed for enterprise use with battle-tested infrastructure. It solves critical problems like runaway costs, non-deterministic failures, and security vulnerabilities through a multi-language architecture (Go, Rust, Python, TypeScript) with Temporal workflow orchestration.

## Core Architecture

Shannon implements a three-layer multi-agent workflow system:

```
Orchestrator Router (Go) → Strategy Workflows → Patterns Library
```

**Key Components:**
- **Orchestrator (Go)**: Task routing, complexity analysis, budget enforcement, session management (port 50052)
- **Agent Core (Rust)**: WASI sandbox for secure code execution, policy enforcement (port 50051)
- **LLM Service (Python)**: Multi-provider LLM interface, MCP tools (port 8000)
- **Gateway (Go)**: REST API layer with authentication (port 8080)
- **Desktop App (TypeScript/Rust/Tauri)**: Native cross-platform UI

**Data Layer:**
- PostgreSQL (primary state)
- Redis (session cache)
- Qdrant (vector memory)
- Temporal (workflow orchestration)

## Common Development Commands

### Initial Setup
```bash
# Complete setup for fresh clones
make setup
# Add API keys to .env
echo "OPENAI_API_KEY=sk-..." >> .env
# Setup Python WASI interpreter for secure code execution
./scripts/setup_python_wasi.sh
```

### Development Workflow
```bash
# Start all services
make dev

# Run tests
make test          # Unit tests for all languages
make smoke         # E2E smoke tests
make ci           # Full CI suite

# Code quality
make fmt          # Format all code
make lint         # Lint all languages

# View logs and status
make logs         # All service logs
make ps          # Service status
docker compose -f deploy/compose/docker-compose.yml logs -f orchestrator
```

### Building and Proto Generation
```bash
# Regenerate protocol buffers after modifying .proto files
make proto        # Using buf (preferred)
make proto-local  # Local generation (fallback)

# After proto changes, rebuild affected services
docker compose build
docker compose up -d
```

### Testing Specific Components
```bash
# Go tests with coverage
cd go/orchestrator && go test -race ./...

# Rust tests
cd rust/agent-core && cargo test

# Python tests
cd python/llm-service && python -m pytest

# Test workflow replay (time-travel debugging)
./scripts/submit_task.sh "test query"
# Note the workflow_id, then:
make replay-export WORKFLOW_ID=task-dev-1234567890 OUT=test.json
make replay HISTORY=test.json
```

### Desktop Development
```bash
# Native app development
cd desktop
npm install
npm run tauri:dev    # Hot reload development
npm run tauri:build  # Production build

# Web UI development
npm run dev         # Next.js dev server on port 3000
```

## Workflow Patterns and Strategy Selection

Shannon uses complexity-based routing to different workflow strategies:

**Complexity Thresholds:**
- Simple (< 0.3): Direct single-agent execution
- Medium (0.3-0.5): Standard workflows
- Complex (> 0.5): Multi-agent orchestration

**Strategy Workflows:**
- `DAGWorkflow`: General task decomposition with parallel/sequential/hybrid execution
- `ReactWorkflow`: Reason-Act-Observe loops for iterative problem solving
- `ResearchWorkflow`: Information gathering with parallel agents and synthesis
- `ExploratoryWorkflow`: Tree-of-Thoughts pattern for open-ended discovery
- `ScientificWorkflow`: Hypothesis generation and testing

**Execution Patterns:**
- Parallel: Concurrent agent execution with semaphore control
- Sequential: Step-by-step execution with result passing
- Hybrid: Dependency graph execution with topological sorting

**Reasoning Patterns:**
- React, Reflection, Chain-of-Thought, Debate, Tree-of-Thoughts

## Configuration Management

**Key Configuration Files:**
- `config/models.yaml`: LLM provider pricing and tier configuration
- `config/features.yaml`: Feature toggles, workflow settings, complexity thresholds
- `config/shannon.yaml`: System configuration, MCP tools, OpenAPI tools
- `config/opa/policies/`: Access control rules
- `.env`: API keys and secrets

**Model Pricing:** All model pricing is centralized in `config/models.yaml`. When adding new models, specify `input_per_1k` and `output_per_1k` in USD.

**Memory System:** Character-based chunking (4 chars ≈ 1 token) with MMR (Maximal Marginal Relevance) for diversity.

## Security and Sandboxing

- **WASI Sandbox**: Python code execution isolated in WebAssembly runtime
- **OPA Policies**: Fine-grained access control in `config/opa/policies/`
- **Token Budgets**: Hard caps with automatic model fallback
- **Circuit Breakers**: Automatic failure detection and recovery

## Development Environment Details

**Service Ports:**
- Gateway: 8080 (REST API)
- Admin/Events: 8081 (SSE streaming)
- Orchestrator: 50052 (gRPC internal)
- Agent Core: 50051 (gRPC internal)
- LLM Service: 8000 (HTTP internal)
- Temporal UI: 8088
- Grafana: 3030

**Docker Compose Management:**
- Use `docker compose down && docker compose up -d` instead of `docker compose restart` for service updates
- Config files are located in `deploy/compose/`
- Environment variables are linked from root `.env` to `deploy/compose/.env`

## Adding New Components

**New LLM Provider:**
1. Implement in `python/llm-service/providers/`
2. Add pricing to `config/models.yaml`
3. Update tier assignments if needed
4. Add tests

**New Tool:**
1. Define in `python/llm-service/tools/`
2. Register with MCP if external
3. Include `print()` statements for WASI compatibility
4. Write integration tests

**New Workflow Pattern:**
1. Create in `go/orchestrator/internal/workflows/strategies/`
2. Register in workflow router
3. Add complexity analysis logic
4. Test with replay functionality

## Debugging and Troubleshooting

**Health Checks:**
```bash
curl http://localhost:8080/health  # Gateway
curl http://localhost:8081/health  # Admin
```

**Common Issues:**
- Missing API keys: Check `.env` has `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`
- Proto changes not reflected: Run `make proto && docker compose build && docker compose up -d`
- Out of memory: Reduce `WASI_MEMORY_LIMIT_MB` or `HISTORY_WINDOW_MESSAGES`

**Database Access:**
```bash
docker compose exec postgres psql -U shannon -d shannon
redis-cli GET session:SESSION_ID | jq '.total_tokens_used'
```

**Temporal Workflow Debugging:**
- View Temporal UI at http://localhost:8088
- Use time-travel debugging with `make replay-export` and `make replay`
- Check workflow status: `temporal workflow describe --workflow-id <id> --address localhost:7233`

## Multi-Language Development Notes

- **Go**: Use Go 1.24+, standard formatting with `gofmt`
- **Rust**: Use stable channel, format with `cargo fmt`, lint with `cargo clippy`
- **Python**: PEP 8 style, type hints required, format with `ruff`
- **Node.js**: Use Node 22 for Docker containers
- **Protocol Buffers**: Generated files are git-tracked, regenerate with `make proto`

Never use heredoc syntax in scripts as it's problematic in this environment.