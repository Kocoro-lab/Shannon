# Shannon — Enterprise Multi-Agent AI Platform

An open-source, enterprise-ready AI agent platform built with Rust for performance, Go for orchestration, Python for LLMs, and Solana for Web3 trust. Shannon combines:

- **Go Orchestrator**: Temporal workflows with intelligent routing (Chain of Thought, Tree of Thoughts, ReAct, Debate, Reflection)
- **Python LLM Service**: Multi-provider support (OpenAI, Anthropic, Google, AWS, Azure, Groq) with MCP tools
- **Rust Agent Core**: WASI sandbox for secure tool execution with enforcement gateway

Built for enterprise reliability with deterministic replay, comprehensive observability, and production-ready security.

## 🚀 Quick Start

### Prerequisites
- Docker & Docker Compose
- Make, curl, grpcurl
- Go 1.24+ (for proto generation)
- At least one LLM API key (OpenAI or Anthropic)

### Initial Setup

#### For Remote Ubuntu Server
If you're installing on a remote Ubuntu server, run the setup script first:
```bash
./scripts/setup-remote.sh
```
This will install buf, generate proto files, and prepare the environment.

#### Standard Setup

```bash
# 1. Setup environment
make setup-env                          # Creates .env and compose symlink
echo 'OPENAI_API_KEY=sk-your-key' >> .env  # Add your API key

# 2. Start the platform
make dev                                # Starts all services

# 3. Run database migrations (first-time setup)
# Apply PostgreSQL migrations (in order)
for file in migrations/postgres/*.sql; do
  echo "Applying migration: $file"
  docker compose -f deploy/compose/compose.yml exec -T postgres \
    psql -U shannon -d shannon < "$file"
done
# Note: Migrations are numbered (001_, 002_, etc.) and must run in order

# Initialize Qdrant collections
docker run --rm --network shannon_shannon-net \
  -v $(pwd)/migrations/qdrant:/app \
  -e QDRANT_HOST=qdrant \
  python:3.11-slim sh -c \
  "pip install -q qdrant-client && python /app/create_collections.py"

# 4. Verify health
make ps                                 # Show running containers
curl http://localhost:8081/health      # Orchestrator health
curl http://localhost:8000/health      # LLM service health

# 5. Run smoke test
make smoke                              # End-to-end verification
```

### Python Development Setup (Optional)

For local Python development without Docker:

```bash
# Option 1: Using virtual environment (recommended)
cd python/llm-service
python3 -m venv venv
source venv/bin/activate  # On Windows: venv\Scripts\activate
pip install -r requirements.txt
python -m uvicorn main:app --reload --port 8000

# Option 2: Global installation (not recommended)
cd python/llm-service
pip install --user -r requirements.txt
python -m uvicorn main:app --reload --port 8000
```

### Submit Your First Task

```bash
# Via helper script
./scripts/submit_task.sh "What is the capital of France?"

# Via gRPC directly
grpcurl -plaintext \
  -d '{"metadata":{"user_id":"demo","session_id":"demo"},"query":"Explain quantum computing"}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### Stream Real-time Updates

```bash
# SSE streaming (replace <WORKFLOW_ID> with actual ID from SubmitTask)
curl -N "http://localhost:8081/stream/sse?workflow_id=<WORKFLOW_ID>"

# WebSocket streaming
wscat -c "ws://localhost:8081/stream/ws?workflow_id=<WORKFLOW_ID>"
```

## 🏗️ Architecture

### Intelligent Cognitive Workflows

Shannon implements multiple cognitive strategies that automatically route based on task complexity:

- **Chain of Thought (CoT)**: Sequential reasoning for logical problems
- **Tree of Thoughts (ToT)**: Explores multiple solution paths with backtracking
- **ReAct**: Combines reasoning with action for interactive tasks
- **Debate**: Multi-agent argumentation for complex decisions
- **Reflection**: Self-improvement through iterative refinement

### Core Components

```
┌─────────────────────────────────────────────────────────┐
│                    User Request                         │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              Go Orchestrator (Temporal)                 │
│  • Intelligent Router (CoT/ToT/ReAct/Debate/Reflection)│
│  • Budget Manager & Token Tracking                      │
│  • Session Management & Vector Memory                   │
└─────────────────────────────────────────────────────────┘
                            │
                ┌───────────┴───────────┐
                ▼                       ▼
┌──────────────────────┐   ┌──────────────────────┐
│  Rust Agent Core     │   │  Python LLM Service  │
│  • WASI Sandbox      │   │  • 6 LLM Providers   │
│  • Tool Execution    │   │  • MCP Integration   │
│  • Enforcement       │   │  • Tool Selection    │
└──────────────────────┘   └──────────────────────┘
                            │
                ┌───────────┴───────────┐
                ▼                       ▼
┌──────────────────────┐   ┌──────────────────────┐
│  PostgreSQL/Redis    │   │  Qdrant Vector DB    │
│  • Task Storage      │   │  • Semantic Search   │
│  • Session Cache     │   │  • Memory Retrieval  │
└──────────────────────┘   └──────────────────────┘
```

## 📋 Service Ports

| Service | Primary Port | Secondary Ports | Description |
|---------|-------------|-----------------|-------------|
| **Orchestrator** | 50052 (gRPC) | 8081 (Admin HTTP), 2112 (Metrics) | Workflow orchestration |
| **LLM Service** | 8000 (HTTP) | - | LLM providers & tools |
| **Agent Core** | 50051 (gRPC) | 2113 (Metrics) | Secure execution |
| **Temporal** | 7233 (gRPC) | 8088 (Web UI) | Workflow engine |
| **PostgreSQL** | 5432 | - | Primary database |
| **Redis** | 6379 | - | Session cache |
| **Qdrant** | 6333 | - | Vector database |
| **Prometheus** | 9090 | - | Metrics collection (not yet configured) |
| **Grafana** | 3000 | - | Monitoring dashboards (not yet configured) |

## 🔧 Development

### Essential Commands

```bash
# Service Management
make dev            # Start all services
make down           # Stop all services
make clean          # Clean state (removes volumes)
make restart        # Restart specific service: make restart SVC=orchestrator

# Testing
make smoke          # End-to-end smoke test
make test           # Run unit tests
make ci             # Full CI pipeline locally
make ci-replay      # Test workflow determinism

# Code Management
make proto          # Regenerate protobuf files
make fmt            # Format all code
make lint           # Run linters
make coverage       # Generate coverage reports

# Debugging
make logs           # View all logs
make logs-service SVC=orchestrator  # View specific service logs
docker compose exec temporal temporal workflow list  # List workflows
```

### Service-Specific Testing

```bash
# Test individual components
cd rust/agent-core && cargo test
cd go/orchestrator && go test -race ./...
cd python/llm-service && python3 -m pytest

# Test WASI sandbox
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm
cd rust/agent-core && cargo run --example wasi_hello -- /tmp/hello-wasi.wasm

# Test Temporal replay
make replay-export WORKFLOW_ID=task-xxx OUT=history.json
make replay HISTORY=history.json
```

### WASI Code Executor Setup

The Rust Agent Core includes a WASI (WebAssembly System Interface) sandbox for secure code execution. To use this feature:

```bash
# Install WebAssembly tools (macOS)
brew install wabt  # Provides wat2wasm for compiling WebAssembly

# Test WASI execution
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm
cd rust/agent-core && cargo run --example wasi_hello -- /tmp/hello-wasi.wasm

# For detailed setup instructions, see: rust/agent-core/WASI_SETUP.md
```

## 🛠️ Configuration

### Environment Variables (.env)

```bash
# Required: At least one LLM provider
OPENAI_API_KEY=sk-your-key
ANTHROPIC_API_KEY=your-key

# Optional: Additional providers
GOOGLE_API_KEY=your-key
AWS_BEDROCK_ACCESS_KEY=your-key
AZURE_OPENAI_API_KEY=your-key
GROQ_API_KEY=your-key

# Optional: Tool providers
EXA_API_KEY=your-key
WEATHER_API_KEY=your-key

# Performance tuning
TOOL_PARALLELISM=4          # Parallel tool execution
ENABLE_TOOL_SELECTION=1     # Auto-select tools
PRIORITY_QUEUES=on          # Enable priority queues
```

### Configuration Files

Main config: `config/shannon.yaml` (hot-reload supported)

```yaml
# Production settings
auth:
  enabled: true
  skip_auth: false

policy:
  enabled: true
  mode: "enforce"
  fail_closed: true

# Strategy-specific timeouts
patterns:
  chain_of_thought:
    max_iterations: 10
    timeout: "5m"
  tree_of_thoughts:
    max_depth: 5
    branching_factor: 3
  react:
    max_steps: 15
    timeout: "10m"
```

## 🔌 Tool System

### List Available Tools

```bash
curl http://localhost:8000/tools/list | jq
```

### Execute a Tool

```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool_name":"calculator","parameters":{"expression":"2+2"}}'
```

### Auto-Select Tools

```bash
curl -X POST http://localhost:8000/tools/select \
  -H "Content-Type: application/json" \
  -d '{"task": "Find latest news about AI", "max_tools": 3}'
```

### MCP Tool Registration

For production (config-based):
```yaml
# config/shannon.yaml
mcp_tools:
  - name: weather_api
    url: "https://api.weather.com/v1"
    auth_token: "${WEATHER_API_KEY}"
```

For development (runtime):
```bash
curl -X POST http://localhost:8000/tools/mcp/register \
  -H "Content-Type: application/json" \
  -d '{"name":"custom_tool","url":"http://localhost:8000/mcp/mock","func_name":"process"}'
```

## 📊 Observability

### Metrics

```bash
# Orchestrator metrics
curl http://localhost:2112/metrics

# Agent Core metrics
curl http://localhost:2113/metrics

# LLM Service metrics
curl http://localhost:8000/metrics
```

### Monitoring

- **Temporal UI**: http://localhost:8088 (✅ Available)
- **Service Metrics**:
  - Orchestrator: http://localhost:2112/metrics
  - Agent Core: http://localhost:2113/metrics
  - LLM Service: http://localhost:8000/metrics
- **Grafana**: Not yet configured (planned for port 3000)
- **Prometheus**: Not yet configured (planned for port 9090)

### Streaming APIs

Shannon supports multiple streaming protocols:

- **SSE**: `GET /stream/sse?workflow_id=<id>`
- **WebSocket**: `GET /stream/ws?workflow_id=<id>`
- **gRPC Stream**: Via `StreamWorkflowUpdates` RPC

See `docs/streaming-api.md` for details.

## 🚀 Production Deployment

### Security Checklist

- [ ] Enable authentication (`auth.enabled: true`)
- [ ] Enable policy enforcement (`policy.enabled: true`)
- [ ] Use TLS for all services
- [ ] Put admin ports behind authenticated proxy
- [ ] Store secrets in vault (not in Git)
- [ ] Enable rate limiting and circuit breakers
- [ ] Configure resource limits for containers

### Performance Tuning

```bash
# Enable priority queues
PRIORITY_QUEUES=on

# Tune parallel execution
TOOL_PARALLELISM=8

# Configure worker pools
TEMPORAL_WORKER_COUNT=10
TEMPORAL_MAX_CONCURRENT_ACTIVITIES=20

# Enable caching
ENABLE_REDIS_CACHE=true
ENABLE_EMBEDDING_CACHE=true
```

### Scaling

- **Horizontal Scaling**: Agent Core and LLM Service are stateless
- **Temporal Workers**: Scale by increasing worker count
- **Database**: Use read replicas for PostgreSQL
- **Caching**: Redis cluster for session management
- **Vector DB**: Qdrant supports clustering

## 📚 Documentation

- **Architecture**: [docs/multi-agent-workflow-architecture.md](docs/multi-agent-workflow-architecture.md)
- **Pattern Guide**: [docs/pattern-usage-guide.md](docs/pattern-usage-guide.md)
- **Streaming APIs**: [docs/streaming-api.md](docs/streaming-api.md)
- **MCP Integration**: [docs/mcp-integration.md](docs/mcp-integration.md)
- **Provider Models**: [docs/providers-models.md](docs/providers-models.md)
- **Orchestrator Details**: [go/orchestrator/README.md](go/orchestrator/README.md)

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) (coming soon).

### Development Workflow

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run `make ci` to ensure tests pass
5. Submit a pull request

## 📄 License

To be added prior to public release.

---

**Need help?** Start with `make setup-env`, then `make dev`, and submit your first task. For deeper understanding, explore the architecture documentation and pattern guides.