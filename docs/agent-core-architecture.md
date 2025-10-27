# Shannon Agent Core - Architecture Documentation

## Overview

The Shannon Agent Core is a high-performance Rust implementation of the agent execution layer, providing secure sandboxing, efficient memory management, and intelligent tool orchestration. This document describes the modernized architecture following 2025 best practices.

## Architecture Principles

1. **Separation of Concerns**: Intelligence (Python) vs Execution (Rust)
2. **Zero-Copy Operations**: Minimize string cloning and memory allocations
3. **Modern Concurrency**: Use `OnceLock` and `std::sync::Once` instead of `lazy_static`
4. **Comprehensive Error Handling**: `Result<T>` types with structured errors via `thiserror`
5. **Observable Systems**: OpenTelemetry tracing and Prometheus metrics
6. **Security First**: WASI sandboxing for untrusted code execution

## Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     gRPC Server (port 50051)                │
├─────────────────────────────────────────────────────────────┤
│                    Enforcement Gateway                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐ │
│  │  Timeouts    │  │  Rate Limits │  │ Circuit Breakers │ │
│  └──────────────┘  └──────────────┘  └──────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                    Tool Execution Layer                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │   Tool   │  │   Tool   │  │   Tool   │  │   WASI   │  │
│  │ Registry │  │   Cache  │  │ Executor │  │  Sandbox │  │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  │
├─────────────────────────────────────────────────────────────┤
│                    Infrastructure Layer                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │  Memory  │  │  Config  │  │ Tracing  │  │ Metrics  │  │
│  │   Pool   │  │  Manager │  │  (OTEL)  │  │  (Prom)  │  │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Enforcement Gateway (`src/enforcement.rs`)

Uniform per-request policy enforcement for every code path:

- Timeouts: hard wall clock limit per request
- Token ceiling: reject requests with excessive estimated tokens
- Rate limiting: simple per-key token bucket
- Circuit breaker: rolling error window per key
  - Optional distributed limiter: set `ENFORCE_RATE_REDIS_URL` to enable a Redis-backed token bucket shared across instances.

Configuration lives under `enforcement` in `config/agent.yaml` with environment variable overrides (`ENFORCE_*`).

### 2. Tool System

#### Tool Registry (`src/tool_registry.rs`)
- Centralized tool capability management
- Discovery API with filtering and relevance scoring
- Metadata including schemas, permissions, and TTL

#### Tool Cache (`src/tool_cache.rs`)
- LRU caching with configurable TTL
- Deterministic cache key generation
- Automatic expiration and sweeping
- Comprehensive statistics tracking

#### Tool Executor (`src/tools.rs`)
- Unified interface for tool execution
- Integration with Python LLM service
- WASI sandbox routing for code execution
- Automatic result caching

### 3. WASI Sandbox (`src/wasi_sandbox.rs`)

Secure WebAssembly execution environment with:
- Filesystem isolation (read-only `/tmp` access)
- Memory limits (configurable, default 256MB)
- Execution timeouts (default 30s)
- Fuel metering for CPU usage control

### 4. Memory Management (`src/memory.rs`)

Efficient memory pool with:
- Pre-allocated memory blocks
- Automatic garbage collection
- Pressure-based rejection
- Thread-safe allocation/deallocation

### 5. Configuration (`src/config.rs`)

Centralized configuration management:
- YAML-based configuration files
- Environment variable overrides (including enforcement: `ENFORCE_*`)
- Hot-reload support (future)
- Structured configuration types

### 6. Observability

#### Tracing (`src/tracing.rs`)
- OpenTelemetry integration
- W3C trace context propagation
- Active span context injection
- Cross-service tracing support

#### Metrics (`src/metrics.rs`)
- Prometheus metrics export
- Tool execution metrics
- Memory usage tracking
- Cache performance stats
- Enforcement metrics: drops by reason, allowed outcomes

## API Contracts

### gRPC API

The agent exposes the following gRPC services:

```protobuf
service AgentService {
  rpc ExecuteTask(ExecuteTaskRequest) returns (ExecuteTaskResponse);
  rpc StreamExecuteTask(ExecuteTaskRequest) returns (stream TaskUpdate);
  rpc GetCapabilities(GetCapabilitiesRequest) returns (GetCapabilitiesResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc DiscoverTools(DiscoverToolsRequest) returns (DiscoverToolsResponse);
  rpc GetToolCapability(GetToolCapabilityRequest) returns (GetToolCapabilityResponse);
}
```

### Python-Rust Contract

The Rust agent communicates with Python LLM service via HTTP:

#### Tool Selection
```
POST /tools/select
{
  "task": "string",
  "context": {},
  "exclude_dangerous": boolean,
  "max_tools": number
}
```

#### Tool Execution
```
POST /tools/execute
{
  "tool_name": "string",
  "parameters": {}
}
```

#### Task Analysis
```
POST /analyze_task
{
  "query": "string",
  "context": {}
}
```

## Error Handling

Comprehensive error taxonomy using `thiserror`:

```rust
pub enum AgentError {
    ToolNotFound { name: String },
    ToolExecutionFailed { tool: String, reason: String },
    MemoryExhausted { requested: usize, available: usize },
    SandboxViolation { operation: String },
    ConfigurationError(String),
    NetworkError(String),
    // ... 20+ error variants
}
```

## Performance Optimizations

### 1. Zero-Copy Strings
Using `Cow<str>` for string handling to avoid unnecessary allocations:
```rust
pub fn process_text<'a>(input: &'a str) -> Cow<'a, str>
```

### 2. Lazy Initialization
Modern `OnceLock` pattern for metrics:
```rust
static METRICS: OnceLock<HashMap<String, Counter>> = OnceLock::new();
```

### 3. Parallel Tool Execution
Concurrent tool execution with tokio:
```rust
let futures = tools.iter().map(|tool| executor.execute_tool(tool));
let results = futures::future::join_all(futures).await;
```

### 4. Cache-First Architecture
- Tool result caching with configurable TTL
- LLM response caching for simple queries
- Discovery result caching

## Security Model

### WASI Sandbox Isolation
- No network access
- Limited filesystem access (read-only `/tmp`)
- Memory limits enforced
- CPU usage controlled via fuel metering

### Tool Permission System
```rust
pub struct ToolCapability {
    pub required_permissions: Vec<String>,
    pub is_dangerous: bool,
    pub requires_confirmation: bool,
}
```

### Input Validation
- Parameter schema validation
- Size limits on inputs
- Timeout protection

## Testing Strategy

### Unit Tests
- Component-level testing
- Mock dependencies
- Property-based testing for complex logic

### Integration Tests
- Python-Rust contract validation
- End-to-end tool execution
- Cache behavior verification
- Error handling scenarios

### Performance Tests
- Benchmark critical paths
- Memory usage profiling
- Concurrent execution stress tests

## Deployment

### Docker Container
```dockerfile
FROM rust:latest as builder
WORKDIR /usr/src/app

# Copy manifests and sources
COPY rust/agent-core/Cargo.toml ./
COPY rust/agent-core/build.rs ./
COPY protos /protos
COPY rust/agent-core/src ./src

# Build
RUN apt-get update && apt-get install -y protobuf-compiler \
 && cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates netcat-openbsd \
 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /usr/src/app/target/release/shannon-agent-core /usr/local/bin/shannon-agent-core
EXPOSE 50051
CMD ["shannon-agent-core"]
```

### Configuration
Environment variables:
- `RUST_LOG`: Logging level
- `OTEL_EXPORTER_OTLP_ENDPOINT`: Tracing endpoint
- `MEMORY_POOL_SIZE_MB`: Memory pool size
- `WASI_MEMORY_LIMIT_MB`: WASI sandbox memory limit
- `LLM_SERVICE_URL`: Python LLM service base URL

Note: tool result cache TTL is configured via `tools.cache_ttl_secs` in the agent config (YAML). There is no `TOOL_CACHE_TTL_SECONDS` env override.

### Health Checks
- gRPC HealthCheck RPC on port `50051`
- Metrics endpoint: `:2113/metrics`

## Migration Guide

### From Legacy Patterns

#### Replace `lazy_static!`
```rust
// Old
lazy_static! {
    static ref METRICS: Mutex<HashMap<String, Counter>> = Mutex::new(HashMap::new());
}

// New
static METRICS: OnceLock<Mutex<HashMap<String, Counter>>> = OnceLock::new();
```

#### Error Handling
```rust
// Old
let result = operation().unwrap();

// New
let result = operation().context("Failed to perform operation")?;
```

#### String Operations
```rust
// Old
fn process(input: String) -> String

// New
fn process(input: &str) -> Cow<str>
```

## Future Enhancements

1. **WebAssembly Component Model**: Support for WASI Preview 2
2. **Distributed Caching**: Redis integration for cache sharing
3. **GPU Acceleration**: CUDA/ROCm support for ML operations
4. **Multi-Region Support**: Geo-distributed agent deployment
5. **Advanced Monitoring**: Custom metrics and tracing spans

## Contributing

Please follow these guidelines:
1. Use `cargo fmt` and `cargo clippy` before commits
2. Add tests for new functionality
3. Update documentation for API changes
4. Follow error handling best practices
5. Minimize unnecessary allocations

## License

Copyright 2025 Shannon Project. All rights reserved.
