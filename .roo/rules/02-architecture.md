+++
title = "Shannon Architecture Overview"
description = "Core services, data layer, and system architecture patterns"
applies_to = "all"
+++

# Shannon Architecture Overview

Shannon is a production-ready AI agent platform that solves real-world problems: runaway costs, non-deterministic failures, and security issues.

## Core Services

**Shannon API (Rust)** - `rust/shannon-api/` ⭐ **PRIMARY API**
- Unified Gateway + LLM service in a single high-performance binary
- HTTP REST API endpoint (port 8080) 
- JWT and API key authentication
- Rate limiting with governor, idempotency with Redis
- SSE and WebSocket streaming
- Multi-provider LLM routing (OpenAI, Anthropic, Google, Groq, xAI)
- Can be embedded in Tauri for desktop/mobile apps

**Orchestrator (Go)** - `go/orchestrator/`
- Central workflow management and task routing
- gRPC service (port 50052)
- Temporal workflow orchestration
- Token budget enforcement and model selection
- OPA policy evaluation

**Agent Core (Rust)** - `rust/agent-core/`
- WASI sandbox for secure Python code execution
- Tool execution and agent-to-agent communication
- Memory management and resource limits
- Metrics collection and circuit breakers
- Research pool for parallel task execution

**Playwright Service** - Browser automation for web interactions

## Data Layer

- **PostgreSQL** - Persistent storage with pgvector for embeddings
- **Redis** - Session management and distributed caching
- **Qdrant** - Vector similarity search for agent memory
- **Temporal** - Workflow orchestration and deterministic replay

## Key Architectural Patterns

**Microservices Communication**
- gRPC for internal service communication
- Protocol buffers defined in `protos/`
- Event streaming via SSE and WebSocket (port 8081)

**Security Model**
- WASI sandbox isolates Python code execution
- OPA (Open Policy Agent) for fine-grained access control
- JWT-based authentication with configurable policies

**Observability**
- OpenTelemetry tracing and metrics
- Prometheus metrics endpoint (port 2112)
- Grafana dashboard (port 3030)
- Temporal UI for workflow debugging (port 8088)

**Configuration Management**
- Hot-reloadable YAML configuration in `config/`
- Environment variable overrides via `.env`
- Feature flags in `config/features.yaml`
- Model routing in `config/models.yaml`

## Default Ports
- 8080 (Shannon API/Gateway)
- 8081 (Orchestrator Admin/Events)
- 8082 (Shannon API Admin/Metrics)
- 50052 (Orchestrator gRPC)
- 8088 (Temporal UI)
- 8765 (Embedded API for Tauri)
- 3030 (Grafana Dashboard)
- 2112 (Prometheus Metrics)