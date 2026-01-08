# Shannon Rust Architecture

This document describes the unified Rust architecture for Shannon, including the migration path from the legacy multi-language services to a fully Rust-based core.

## Vision: Full Rust Core Services

Shannon is transitioning to a unified Rust architecture to achieve:

- **Performance**: Rust's zero-cost abstractions and memory safety
- **Cross-Platform**: Single codebase for cloud, desktop, and mobile
- **Simplified Deployment**: Fewer services, single binary options
- **Type Safety**: Compile-time guarantees across the entire system

## Architecture Phases

```
┌──────────────────────────────────────────────────────────────────┐
│                     PHASE 1: Unified API (Current)               │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              shannon-api-rs (Rust)                       │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │   Gateway   │  │ LLM Service │  │    Streaming    │  │    │
│  │  │  Auth/Rate  │  │  Providers  │  │   SSE/WebSocket │  │    │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  │    │
│  └─────────────────────────────────────────────────────────┘    │
│                              │                                   │
│                              ▼                                   │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Orchestrator (Go + Temporal)                │    │
│  └─────────────────────────────────────────────────────────┘    │
│                              │                                   │
│                              ▼                                   │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Agent Core (Rust)                           │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │    WASI     │  │  Research   │  │   Checkpoint    │  │    │
│  │  │   Sandbox   │  │    Pool     │  │    Manager      │  │    │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                     PHASE 2: Rust Temporal Worker (Future)       │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  shannon-api-rs │  │ Orchestrator Go │  │ Orchestrator Rs │  │
│  │     (Rust)      │  │   (Existing)    │  │   (New Worker)  │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│                              │                     │             │
│                              └──────┬──────────────┘             │
│                                     │                            │
│                              ┌──────▼──────┐                     │
│                              │   Temporal  │                     │
│                              │   Server    │                     │
│                              └─────────────┘                     │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                     PHASE 3: Full Rust (Target)                  │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    shannon-core-rs                       │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │     API     │  │Orchestrator │  │   Agent Core    │  │    │
│  │  │   Gateway   │  │  Workflows  │  │  WASI/Sandbox   │  │    │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  │    │
│  └─────────────────────────────────────────────────────────┘    │
│                              │                                   │
│                              ▼                                   │
│                       Single Binary                              │
│               (Cloud, Desktop, Mobile, Edge)                     │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

## Phase 1: Unified API (Current)

### Components

| Component | Language | Status | Description |
|-----------|----------|--------|-------------|
| `shannon-api` | Rust | ✅ Active | Unified Gateway + LLM Service |
| `orchestrator` | Go | ✅ Active | Temporal workflow orchestration |
| `agent-core` | Rust | ✅ Active | WASI sandbox, tool execution |
| `gateway` | Go | ⚠️ Deprecated | Legacy HTTP gateway |
| `llm-service` | Python | ⚠️ Deprecated | Legacy LLM service |

### Shannon API Features

- **HTTP Gateway**: Axum-based REST API, OpenAI-compatible endpoints
- **Authentication**: JWT tokens, API key validation, multi-tenant support
- **Rate Limiting**: Per-user limits with governor, Redis-backed for distributed
- **Streaming**: SSE and WebSocket for real-time events
- **LLM Providers**: OpenAI, Anthropic, Google, Groq, xAI
- **Tool Loop**: Automatic tool call handling with configurable limits

### Deployment Options

#### Cloud Deployment (Docker)

```yaml
# docker-compose.yml
services:
  shannon-api:
    build: ./rust/shannon-api
    ports:
      - "8080:8080"
      - "8082:8081"
    environment:
      - REDIS_URL=redis://redis:6379
      - POSTGRES_URL=postgres://...
      - ORCHESTRATOR_GRPC=http://orchestrator:50052
```

#### Embedded in Tauri

```rust
// desktop/src-tauri/Cargo.toml
[dependencies]
shannon-api = { path = "../../rust/shannon-api" }

// Start embedded server
let handle = embedded_api::start_embedded_api(Some(8765)).await?;
```

#### Standalone Binary

```bash
# Build and run
cargo build -p shannon-api --release
./target/release/shannon-api --port 8080
```

## Phase 2: Rust Temporal Worker (Future)

### Goal

Run Rust-based workflow activities alongside the Go orchestrator, enabling gradual migration of workflow logic.

### Technical Approach

1. Use `temporal-sdk-core` (official Rust SDK)
2. Implement activity workers for compute-heavy tasks
3. Share Redis/PostgreSQL connections with existing services
4. Maintain full compatibility with existing workflows

### Candidate Activities for Rust

| Activity | Benefit |
|----------|---------|
| LLM calls | Connection pooling, streaming |
| Tool execution | Memory safety, parallelism |
| Embedding generation | SIMD optimization |
| Cache management | Lock-free data structures |

## Phase 3: Full Rust Core (Target)

### Vision

A single Rust binary that includes:
- HTTP/gRPC API gateway
- Workflow orchestration (Temporal-compatible)
- Agent execution runtime
- WASI sandbox

### Benefits

- **Single Binary**: Deploy one artifact everywhere
- **Reduced Latency**: No network hops between services
- **Memory Efficiency**: Shared memory, no serialization overhead
- **Cross-Platform**: Native desktop, mobile, edge deployment

### Timeline

| Phase | Target | Status |
|-------|--------|--------|
| Phase 1 | Q1 2026 | ✅ Complete |
| Phase 2 | Q3 2026 | 📋 Planned |
| Phase 3 | Q1 2027 | 📋 Planned |

## Migration Guide

### From Legacy Gateway/LLM Service

1. **Update docker-compose.yml**:
   - Legacy services now require `--profile legacy`
   - Shannon API starts by default

2. **Update client code**:
   - Endpoints remain the same (`:8080`)
   - API compatibility maintained

3. **Environment variables**:
   - Use `SHANNON_*` prefix for new config
   - Legacy `LLM_SERVICE_*` still work

### Configuration Changes

```bash
# Old (Python LLM Service)
LLM_SERVICE_PORT=8000
LLM_SERVICE_HOST=0.0.0.0

# New (Shannon API)
SHANNON_API_PORT=8080
SHANNON_API_HOST=0.0.0.0
```

## Key Rust Crates

| Crate | Purpose | Version |
|-------|---------|---------|
| `axum` | HTTP framework | 0.8 |
| `tokio` | Async runtime | 1.x |
| `tonic` | gRPC client/server | 0.12 |
| `sqlx` | PostgreSQL client | 0.8 |
| `redis` | Redis client | 0.27 |
| `jsonwebtoken` | JWT handling | 9.x |
| `governor` | Rate limiting | 0.10 |
| `reqwest` | HTTP client | 0.12 |

## Performance Comparison

| Metric | Python LLM Service | Shannon API (Rust) | Improvement |
|--------|-------------------|-------------------|-------------|
| Cold start | ~3s | ~100ms | 30x |
| Memory usage | ~500MB | ~50MB | 10x |
| Requests/sec | ~1000 | ~10000 | 10x |
| P99 latency | ~200ms | ~20ms | 10x |

*Benchmarks on M1 Mac, single instance, no LLM calls*

## Contributing

When contributing to Shannon's Rust codebase:

1. Follow Rust idioms and best practices
2. Use `cargo fmt` and `cargo clippy` before committing
3. Add tests for new functionality
4. Update documentation for API changes

See [CONTRIBUTING.md](../CONTRIBUTING.md) for general guidelines.
