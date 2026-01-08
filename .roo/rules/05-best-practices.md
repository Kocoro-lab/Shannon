+++
title = "Best Practices and Common Issues"
description = "Development best practices, common gotchas, and troubleshooting"
applies_to = "best-practices"
+++

# Development Best Practices and Common Issues

## Development Best Practices

### Shannon vs Legacy Services
- **PREFER Shannon API (Rust)** over legacy Go Gateway + Python LLM service
- Shannon API is the unified replacement and future direction
- Only use legacy services if features not yet ported to Shannon API
- Start legacy with: `docker compose --profile legacy up`

### Testing Strategy
- Unit tests: Component-level testing per service
- Integration tests: Cross-service workflow testing  
- Smoke tests: Full end-to-end user workflows
- Replay tests: Deterministic workflow validation

### Testing Workflow
```bash
# Development cycle
make fmt lint          # Format and check code
make test             # Run unit tests
make smoke            # Run end-to-end tests
make coverage         # Check test coverage

# Before committing
make ci               # Full build and basic checks
make coverage-gate    # Enforce coverage thresholds
```

## Common Issues and Solutions

### Protocol Buffer Changes
Always regenerate protos after changing .proto files:
```bash
make proto            # Uses buf with BSR (preferred)
make proto-local      # Local generation fallback
make check-protos     # Verify proto files exist
make dev              # Restart services to pick up changes
```

### Environment Setup Issues
```bash
make setup-env        # Creates symlink and copies .env.example
make setup            # Full setup including proto generation
make check-env        # Validates configuration
```

### WASI Setup Required
Python code execution requires WASI interpreter:
```bash
./scripts/setup_python_wasi.sh  # Downloads ~20MB Python WASI
```

### API Keys Required
At minimum, one LLM provider API key is required:
- `OPENAI_API_KEY=sk-...`
- `ANTHROPIC_API_KEY=sk-ant-...`
- Or configure other providers in `config/models.yaml`

### Memory Limits
Default WASI memory limit is 512MB. Increase via:
```bash
export WASI_MEMORY_LIMIT_MB=1024
```

### Port Conflicts
Default ports in use:
- 8080 (Shannon API / Gateway)
- 8081 (Orchestrator Admin)
- 8082 (Shannon API Admin/Metrics)
- 50052 (Orchestrator gRPC)
- 8088 (Temporal UI)
- 8765 (Embedded API for Tauri)

## Debugging Techniques

### Workflow Debugging
Use Temporal's time-travel debugging:
```bash
# Export workflow history
make replay-export WORKFLOW_ID=task-123

# Replay locally for debugging  
make replay HISTORY=tests/histories/task-123.json
```

### Streaming Debug
```bash
make smoke-stream WF_ID=workflow-id  # Stream workflow events
```

### Service Health Checks
```bash
make ps               # Check all service status
curl http://localhost:8080/health  # Shannon API health
curl http://localhost:8081/health  # Orchestrator health
```

## Shannon API Development

### Features Available
| Feature | Description | Default |
|---------|-------------|---------|
| `gateway` | JWT auth, API key validation | ✅ |
| `grpc` | gRPC client for orchestrator | ❌ |
| `database` | PostgreSQL support | ❌ |

Enable features:
```bash
cargo build -p shannon-api --features "gateway,grpc,database"
```

### Performance Benchmarks
Benchmarks on Apple M1, single instance:
| Metric | Value |
|--------|-------|
| Cold start | ~100ms |
| Memory usage | ~50MB |
| Requests/sec | ~10,000 |
| P99 latency | ~20ms |

## When in Doubt

Prioritize Shannon API (Rust) development over legacy services, follow the mandatory Rust coding standards, and use the comprehensive Make targets for development workflow.

Always reference the comprehensive Rust coding standards at `docs/coding-standards/RUST.md` when generating any Rust code.