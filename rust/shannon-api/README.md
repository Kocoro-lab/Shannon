# Shannon API

High-performance unified API for the Shannon AI platform, combining Gateway and LLM service functionality in a single Rust binary.

## Features

- **HTTP Gateway**: REST API with OpenAI-compatible endpoints
- **Authentication**: JWT tokens and API key validation
- **Rate Limiting**: Per-user limits with governor, Redis-backed for distributed deployments
- **Idempotency**: Request deduplication for safe retries
- **Sessions**: Multi-turn conversation management
- **Tasks**: Async task submission and progress tracking
- **Streaming**: Server-Sent Events (SSE) and WebSocket support
- **LLM Providers**: OpenAI, Anthropic, Google, Groq, xAI
- **Tool Loop**: Automatic tool call handling with configurable limits
- **Embeddable**: Can be embedded in Tauri for desktop/mobile apps

## Quick Start

### Prerequisites

- Rust 1.77+ (stable)
- Redis (optional, for distributed rate limiting)
- PostgreSQL (optional, for persistence)

### Running Standalone

```bash
# Set environment variables
export OPENAI_API_KEY=sk-...
export REDIS_URL=redis://localhost:6379  # Optional

# Build and run
cargo run -p shannon-api

# Or with release optimizations
cargo build -p shannon-api --release
./target/release/shannon-api --port 8080
```

### Using Docker

```bash
# Build the image
docker build -t shannon-api .

# Run
docker run -p 8080:8080 \
  -e OPENAI_API_KEY=sk-... \
  -e REDIS_URL=redis://redis:6379 \
  shannon-api
```

### Embedded in Tauri

Add to your Tauri project's `Cargo.toml`:

```toml
[dependencies]
shannon-api = { path = "../../rust/shannon-api" }

[features]
embedded-api = ["dep:shannon-api"]
```

Start the embedded server:

```rust
use shannon_api::embedded_api;

let handle = embedded_api::start_embedded_api(Some(8765)).await?;
println!("API running at {}", handle.base_url());
```

## API Endpoints

### Health & Info

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |
| `/api/v1/info` | GET | API information |
| `/api/v1/capabilities` | GET | Available features |

### Chat Completions (OpenAI-compatible)

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ],
    "stream": true
  }'
```

### Sessions

```bash
# Create session
curl -X POST http://localhost:8080/api/v1/sessions \
  -H "Authorization: Bearer sk-..." \
  -H "Content-Type: application/json" \
  -d '{"name": "my-session"}'

# Get session
curl http://localhost:8080/api/v1/sessions/{id} \
  -H "Authorization: Bearer sk-..."

# Add message
curl -X POST http://localhost:8080/api/v1/sessions/{id}/messages \
  -H "Authorization: Bearer sk-..." \
  -H "Content-Type: application/json" \
  -d '{"role": "user", "content": "Hello"}'
```

### Tasks

```bash
# Submit task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer sk-..." \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Research quantum computing"}'

# Get status
curl http://localhost:8080/api/v1/tasks/{id} \
  -H "Authorization: Bearer sk-..."

# Get progress
curl http://localhost:8080/api/v1/tasks/{id}/progress \
  -H "Authorization: Bearer sk-..."

# Stream events (SSE)
curl -N http://localhost:8080/api/v1/tasks/{id}/stream \
  -H "Authorization: Bearer sk-..."

# WebSocket
wscat -c ws://localhost:8080/api/v1/tasks/{id}/ws
```

## Configuration

Configuration is loaded from environment variables and optional config files.

### Environment Variables

```bash
# Server
SHANNON_API_HOST=0.0.0.0
SHANNON_API_PORT=8080
SHANNON_API_ADMIN_PORT=8081

# Database (optional)
POSTGRES_URL=postgres://user:pass@localhost/shannon

# Redis (optional, for sessions and rate limiting)
REDIS_URL=redis://localhost:6379

# Orchestrator gRPC
ORCHESTRATOR_GRPC=http://localhost:50052

# Authentication
JWT_SECRET=your-secret-key

# LLM Providers
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
GROQ_API_KEY=...
XAI_API_KEY=...

# Logging
RUST_LOG=info
```

### Config File

Create `config/shannon-api.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 8080
  timeout_secs: 300

gateway:
  jwt_expiry_secs: 86400
  rate_limit_per_minute: 60
  rate_limit_burst: 10
  idempotency_enabled: true

llm:
  model: gpt-4o
  max_tokens: 4096
  temperature: 0.7
  max_tool_iterations: 10
```

## Architecture

```
shannon-api/
├── src/
│   ├── lib.rs              # Library exports, AppState
│   ├── main.rs             # Binary entry point
│   ├── config/             # Configuration management
│   ├── gateway/            # Gateway functionality
│   │   ├── auth.rs         # JWT/API key authentication
│   │   ├── rate_limit.rs   # Rate limiting middleware
│   │   ├── idempotency.rs  # Idempotency key handling
│   │   ├── sessions.rs     # Session endpoints
│   │   ├── tasks.rs        # Task endpoints
│   │   ├── streaming.rs    # SSE/WebSocket
│   │   └── grpc_client.rs  # Orchestrator client
│   ├── llm/                # LLM abstraction
│   │   ├── orchestrator.rs # Tool loop logic
│   │   └── providers/      # Provider implementations
│   ├── api/                # HTTP endpoints
│   ├── domain/             # Domain models
│   ├── events/             # Streaming events
│   ├── runtime/            # Execution management
│   ├── tools/              # Built-in tools
│   └── server.rs           # Server setup
└── Dockerfile
```

## Development

### Building

```bash
# Debug build
cargo build -p shannon-api

# Release build
cargo build -p shannon-api --release

# With all features
cargo build -p shannon-api --all-features
```

### Testing

```bash
# Run tests
cargo test -p shannon-api

# With coverage
cargo llvm-cov --package shannon-api
```

### Linting

```bash
cargo fmt --package shannon-api
cargo clippy --package shannon-api
```

## Features

| Feature | Description | Default |
|---------|-------------|---------|
| `gateway` | JWT auth, API key validation | ✅ |
| `grpc` | gRPC client for orchestrator | ❌ |
| `database` | PostgreSQL support | ❌ |

Enable features:

```bash
cargo build -p shannon-api --features "gateway,grpc,database"
```

## Performance

Benchmarks on Apple M1, single instance:

| Metric | Value |
|--------|-------|
| Cold start | ~100ms |
| Memory usage | ~50MB |
| Requests/sec | ~10,000 |
| P99 latency | ~20ms |

## License

MIT License - see [LICENSE](../../LICENSE)

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for guidelines.
