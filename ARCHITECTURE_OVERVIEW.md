# Shannon Platform: High-Level Architectural Overview

## Executive Summary

Shannon is an enterprise-ready multi-agent AI platform that combines intelligent orchestration, secure execution, and comprehensive tool integration. The platform is built using a three-language architecture optimized for each layer's specific requirements:

- **Go Orchestrator**: Temporal-based workflow engine for reliable task coordination
- **Python LLM Service**: AI intelligence layer with multi-provider LLM support
- **Rust Agent Core**: Security-first execution environment with WASI sandboxing

The platform enables sophisticated AI reasoning through composable cognitive patterns (Chain of Thought, Tree of Thoughts, ReAct, Debate, Reflection) with production-grade reliability, security, and observability.

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    User Request                             │
│              (gRPC/HTTP/WebSocket)                          │
└─────────────────────┬───────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────┐
│              Go Orchestrator (Temporal)                     │
│  • Intelligent Router (CoT/ToT/ReAct/Debate/Reflection)    │
│  • Budget Manager & Token Tracking                         │
│  • Session Management & Vector Memory                      │
│  • Health Monitoring & Circuit Breakers                    │
│  Ports: 50052 (gRPC), 8081 (Admin HTTP), 2112 (Metrics)  │
└─────────────────────┬───────────────────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
┌───────▼──────┐ ┌───▼────┐ ┌──────▼──────┐
│ Rust Agent   │ │ Python │ │ External    │
│ Core         │ │ LLM    │ │ Services    │
│ • WASI       │ │ Service│ │ • Temporal  │
│   Sandbox    │ │ • 6+   │ │ • PostgreSQL│
│ • Tool       │ │   LLM  │ │ • Redis     │
│   Execution  │ │   Providers│ │ • Qdrant│
│ • Policy     │ │ • MCP  │ │             │
│   Enforcement│ │   Tools│ │             │
│ Port: 50051  │ │Port:8000│ │             │
└──────────────┘ └────────┘ └─────────────┘
```

## Core Components

### 1. Go Orchestrator (`go/orchestrator/main.go`)

**Primary Entry Point**: The main coordinator that receives user requests and orchestrates the entire workflow.

**Key Responsibilities**:
- **Request Processing**: Accepts tasks via gRPC on port 50052
- **Intelligent Routing**: Automatically selects cognitive strategies based on task complexity
- **Budget Management**: Tracks and enforces token usage limits
- **Session Management**: Maintains conversation context and memory
- **Health Monitoring**: Comprehensive health checks for all dependent services

**Entry Points**:
- `gRPC`: `localhost:50052` - Main service API
- `HTTP Admin`: `localhost:8081` - Health, streaming, approvals
- `Metrics`: `localhost:2112` - Prometheus metrics

**Key Workflows**:
- `SubmitTask`: Main entry point for processing user queries
- `StreamWorkflowUpdates`: Real-time progress streaming
- Multiple cognitive strategies: DAG, ReAct, Research, Exploratory, Scientific

### 2. Python LLM Service (`python/llm-service/main.py`)

**Primary Entry Point**: FastAPI-based service providing AI intelligence and tool integration.

**Key Responsibilities**:
- **LLM Integration**: Support for 6+ providers (OpenAI, Anthropic, Google, AWS, Azure, Groq)
- **Tool Management**: MCP (Model Context Protocol) tool integration
- **Embedding Services**: Vector embeddings for semantic search
- **Complexity Analysis**: Task complexity scoring for routing decisions

**Entry Points**:
- `HTTP API`: `localhost:8000` - Main service endpoint
- `Health`: `/health/live` and `/health/ready`
- `Completions`: `/completions` - LLM text generation
- `Embeddings`: `/embeddings` - Vector embedding generation
- `Tools`: `/tools/*` - Tool execution and management

**Key APIs**:
```python
POST /completions - Generate text completions
POST /embeddings - Generate vector embeddings  
POST /tools/execute - Execute specific tools
GET /tools/list - List available tools
POST /tools/select - Auto-select tools for tasks
```

### 3. Rust Agent Core (`rust/agent-core/src/main.rs`)

**Primary Entry Point**: Secure execution environment with WASI sandboxing.

**Key Responsibilities**:
- **Secure Execution**: WASI-based sandboxed execution environment
- **Tool Execution**: Safe execution of external tools and code
- **Policy Enforcement**: OPA (Open Policy Agent) policy enforcement
- **Performance**: High-performance execution with memory safety

**Entry Points**:
- `gRPC`: `localhost:50051` - Agent service API
- `Metrics`: `localhost:2113` - Prometheus metrics

**Key Services**:
- Tool execution with sandboxing
- Policy evaluation and enforcement
- Resource monitoring and limits

## Data Flow Architecture

### Request Processing Flow

1. **User Input** → Go Orchestrator (gRPC port 50052)
2. **Task Analysis** → Python LLM Service (`POST /complexity`)
3. **Strategy Selection** → Orchestrator chooses cognitive pattern
4. **Agent Execution** → Multi-agent coordination via selected pattern
5. **Tool Execution** → Rust Agent Core or Python LLM Service tools
6. **Result Synthesis** → Orchestrator combines agent outputs
7. **Response Streaming** → Real-time updates via WebSocket/SSE

### Cognitive Patterns

Shannon implements multiple reasoning strategies that are automatically selected:

- **Chain of Thought (CoT)**: Sequential reasoning for logical problems
- **Tree of Thoughts (ToT)**: Explores multiple solution paths with backtracking  
- **ReAct**: Combines reasoning with action for interactive tasks
- **Debate**: Multi-agent argumentation for complex decisions
- **Reflection**: Self-improvement through iterative refinement

### Data Storage

```
┌─────────────────┐ ┌──────────────────┐ ┌─────────────────┐
│   PostgreSQL    │ │      Redis       │ │    Qdrant       │
│   Port: 5432    │ │    Port: 6379    │ │   Port: 6333    │
│                 │ │                  │ │                 │
│ • Task Storage  │ │ • Session Cache  │ │ • Vector Search │
│ • User Sessions │ │ • Rate Limiting  │ │ • Semantic      │
│ • Audit Logs    │ │ • Circuit Breaker│ │   Memory        │
│ • Workflows     │ │   State          │ │ • Embeddings    │
└─────────────────┘ └──────────────────┘ └─────────────────┘
```

## External Dependencies

### Required Services

- **Temporal**: Workflow engine for reliable orchestration
  - `Port 7233`: Core temporal service
  - `Port 8088`: Temporal Web UI
- **PostgreSQL**: Primary database for persistent storage
- **Redis**: Caching and session management
- **Qdrant**: Vector database for semantic search

### Optional Integrations

- **Prometheus**: Metrics collection (port 9090)
- **Grafana**: Monitoring dashboards (port 3000)
- **OpenTelemetry**: Distributed tracing
- **OPA**: Policy enforcement engine

## Outputs and Interfaces

### Primary Outputs

1. **Task Results**: Structured responses to user queries
2. **Real-time Streaming**: Progress updates via WebSocket/SSE
3. **Metrics**: Comprehensive observability data
4. **Audit Trails**: Complete execution history in PostgreSQL

### API Interfaces

#### gRPC Services
```protobuf
// Orchestrator Service (port 50052)
service OrchestratorService {
  rpc SubmitTask(SubmitTaskRequest) returns (SubmitTaskResponse);
  rpc StreamWorkflowUpdates(StreamRequest) returns (stream WorkflowUpdate);
}

// Agent Service (port 50051)  
service AgentService {
  rpc ExecuteTask(ExecuteTaskRequest) returns (ExecuteTaskResponse);
  rpc GetHealth(HealthRequest) returns (HealthResponse);
}
```

#### HTTP REST APIs
```yaml
# LLM Service (port 8000)
POST /completions: Generate LLM responses
POST /embeddings: Create vector embeddings
POST /tools/execute: Execute specific tools
GET /tools/list: List available tools

# Orchestrator Admin (port 8081)
GET /health: Service health status
GET /stream/sse: Server-sent events streaming
GET /stream/ws: WebSocket streaming
POST /approvals/decision: Human approval workflow
```

### Streaming Interfaces

Shannon provides real-time updates through multiple protocols:

- **Server-Sent Events (SSE)**: `GET /stream/sse?workflow_id=<id>`
- **WebSocket**: `GET /stream/ws?workflow_id=<id>`  
- **gRPC Streaming**: `StreamWorkflowUpdates` RPC

## Configuration and Deployment

### Docker Compose Setup

The platform uses Docker Compose for easy deployment with all services:

```bash
# Setup and start all services
make setup-env  # Creates .env and compose symlink
make dev        # Starts all Docker services

# Key services started:
# - temporal:7233, temporal-ui:8088
# - postgres:5432, redis:6379, qdrant:6333
# - orchestrator:50052, llm-service:8000, agent-core:50051
```

### Environment Configuration

Required environment variables:
```bash
# At least one LLM provider required
OPENAI_API_KEY=sk-your-key
ANTHROPIC_API_KEY=your-key

# Optional additional providers
GOOGLE_API_KEY=your-key
AWS_BEDROCK_ACCESS_KEY=your-key
AZURE_OPENAI_API_KEY=your-key
GROQ_API_KEY=your-key
```

### Hot-Reload Configuration

Shannon supports hot-reload configuration via `config/shannon.yaml`:

```yaml
# Example configuration with hot-reload
service:
  port: 50052
  health_port: 8081

auth:
  enabled: true
  skip_auth: true  # Development mode

patterns:
  chain_of_thought:
    max_iterations: 10
    timeout: "5m"
  tree_of_thoughts:
    max_depth: 5
    branching_factor: 3
```

## Security and Production Readiness

### Security Features

- **WASI Sandboxing**: Secure code execution in Rust Agent Core
- **Policy Enforcement**: OPA-based policy engine
- **Authentication**: JWT-based auth with multi-tenancy support
- **Circuit Breakers**: Automatic failure isolation and recovery
- **Rate Limiting**: Per-tenant and per-API rate limits

### Production Features

- **Deterministic Replay**: Temporal workflow replay for debugging
- **Comprehensive Monitoring**: Prometheus metrics for all services
- **Health Checks**: Multi-layer health monitoring
- **Graceful Degradation**: Fallback behaviors when services are unavailable
- **Session Management**: Persistent conversation context

## Getting Started

### Quick Start
```bash
# 1. Setup environment
make setup-env
echo 'OPENAI_API_KEY=sk-your-key' >> .env

# 2. Start the platform  
make dev

# 3. Verify health
curl http://localhost:8081/health
curl http://localhost:8000/health

# 4. Submit first task via helper script
./scripts/submit_task.sh "What is the capital of France?"

# Or submit directly via gRPC
grpcurl -plaintext \
  -d '{"metadata":{"userId":"demo","sessionId":"demo"},"query":"What is the capital of France?"}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### Available Scripts

Shannon provides several utility scripts for common operations:

```bash
# Task submission and monitoring
./scripts/submit_task.sh "Your query here"
./scripts/stream_smoke.sh     # Test streaming functionality
./scripts/smoke_e2e.sh        # End-to-end smoke test

# Database and infrastructure
./scripts/seed_postgres.sh    # Initialize database
./scripts/bootstrap_qdrant.sh # Setup vector database
./scripts/verify_metrics.sh   # Check metrics endpoints

# Development and debugging
./scripts/replay_workflow.sh  # Replay Temporal workflows
make smoke                    # Run comprehensive smoke tests
make test                     # Run all unit tests
make ci                       # Full CI pipeline locally
```

### Monitoring and Observability

- **Temporal UI**: http://localhost:8088 - Workflow execution monitoring
- **Service Health**: http://localhost:8081/health - Overall platform health
- **Orchestrator Metrics**: http://localhost:2112/metrics - Performance metrics
- **Agent Core Metrics**: http://localhost:2113/metrics - Execution metrics
- **LLM Service Metrics**: http://localhost:8000/metrics - AI service metrics

### Testing and Verification

Shannon includes comprehensive testing infrastructure:

```bash
# Run smoke tests (end-to-end verification)
make smoke

# Run unit tests for each component
cd rust/agent-core && cargo test
cd go/orchestrator && go test -race ./...
cd python/llm-service && python3 -m pytest

# Test streaming functionality
curl -N "http://localhost:8081/stream/sse?workflow_id=<WORKFLOW_ID>"
wscat -c "ws://localhost:8081/stream/ws?workflow_id=<WORKFLOW_ID>"

# Test tool execution
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool_name":"calculator","parameters":{"expression":"2+2"}}'
```

### Production Deployment Checklist

Before deploying to production:

- [ ] Enable authentication (`auth.enabled: true` in config)
- [ ] Configure secure JWT secrets
- [ ] Enable policy enforcement (`policy.enabled: true`)
- [ ] Set up TLS certificates for all services
- [ ] Configure resource limits and monitoring
- [ ] Set up backup strategies for PostgreSQL and Redis
- [ ] Configure log aggregation and alerting
- [ ] Test disaster recovery procedures

## Summary

This architecture provides a robust, scalable foundation for enterprise AI agent deployments with:

- **Intelligent Multi-Agent Orchestration**: Automatic cognitive pattern selection
- **Production-Grade Reliability**: Temporal workflows with deterministic replay
- **Comprehensive Security**: WASI sandboxing and policy enforcement
- **Real-Time Observability**: Metrics, health checks, and streaming APIs
- **Flexible Tool Integration**: MCP protocol support for extensible tooling
- **Hot-Reload Configuration**: Dynamic configuration updates without restarts

The three-language architecture (Go/Python/Rust) optimizes each layer for its specific requirements while providing clean interfaces and comprehensive testing coverage.