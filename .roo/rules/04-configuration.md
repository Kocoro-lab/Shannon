+++
title = "Configuration and File Locations"
description = "Important files, configuration management, and system setup"
applies_to = "configuration"
+++

# Configuration and File Locations

## Important File Locations

### Configuration
- `config/shannon.yaml` - Main system configuration
- `config/features.yaml` - Feature flags and execution modes  
- `config/models.yaml` - LLM model routing and fallbacks
- `config/opa/policies/` - OPA security policies
- `.env` - Environment variables and API keys

### Coding Standards
- `docs/coding-standards/RUST.md` - MANDATORY Rust coding guidelines

### Core Code
- `rust/shannon-api/` - Unified Gateway + LLM service (Rust) ⭐ PRIMARY
- `go/orchestrator/` - Workflow orchestration with Temporal
- `rust/agent-core/` - WASI sandbox and agent execution
- `desktop/` - Next.js/Tauri desktop application

### Proto Definitions
- `protos/` - gRPC service definitions
- Generated files in `python/llm-service/llm_service/grpc_gen/`

### Scripts
- `scripts/setup_python_wasi.sh` - Setup WASI Python interpreter
- `scripts/smoke_e2e.sh` - End-to-end testing
- `scripts/replay_workflow.sh` - Temporal workflow replay for debugging

## Configuration Management

Configuration files support hot-reload (changes detected within ~30s):
- Always validate with `make check-env` after changes
- Use feature flags in `config/features.yaml` for experimental features
- Model routing and pricing configured in `config/models.yaml`

## Build Dependencies

- **Go 1.22+** (Orchestrator and Gateway services)
- **Rust (stable)** (Agent Core and Shannon API services)
- **Python 3.9+** (LLM service and tools - legacy)
- **Node.js 18+** (Desktop application)
- **Docker & Docker Compose** (local development)
- **Protocol Buffers compiler** (buf preferred)

## Environment Variables

### Server Configuration
```bash
SHANNON_API_HOST=0.0.0.0
SHANNON_API_PORT=8080
SHANNON_API_ADMIN_PORT=8081
```

### Database Configuration (optional)
```bash
POSTGRES_URL=postgres://user:pass@localhost/shannon
REDIS_URL=redis://localhost:6379
```

### Orchestrator gRPC
```bash
ORCHESTRATOR_GRPC=http://localhost:50052
```

### Authentication
```bash
JWT_SECRET=your-secret-key
```

### LLM Providers
```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
GROQ_API_KEY=...
XAI_API_KEY=...
```

### Logging
```bash
RUST_LOG=info
```

## Development vs Legacy Services

### Shannon vs Legacy Services
- **PREFER Shannon API (Rust)** over legacy Go Gateway + Python LLM service
- Shannon API is the unified replacement and future direction
- Only use legacy services if features not yet ported to Shannon API
- Start legacy with: `docker compose --profile legacy up`

### Working with Multiple Services
1. Always use `make dev` to start the full stack
2. Check logs with `make logs` rather than individual docker commands  
3. Use `make ps` to verify all services running correctly
4. Run `make smoke` after significant changes for end-to-end testing