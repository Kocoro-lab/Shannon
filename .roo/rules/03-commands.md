+++
title = "Development Commands"
description = "Essential Make commands and development workflow"
applies_to = "development"
+++

# Development Commands

## Initial Setup
```bash
make setup                              # Complete first-time setup
echo "OPENAI_API_KEY=sk-..." >> .env    # Add API keys
./scripts/setup_python_wasi.sh          # Setup Python WASI interpreter
make check-env                          # Validate environment
```

## Building and Running
```bash
make dev                # Start all services (builds locally)
make down              # Stop all services
make logs              # View logs from all services
make ps                # Check service status
make smoke             # Run end-to-end smoke tests
```

## Development Workflow
```bash
make fmt lint          # Format code (Go, Rust, Python) and run linters
make test             # Run all unit tests
make proto            # Generate protobuf files (uses buf with BSR)
make proto-local      # Local proto generation fallback
make ci               # CI build (compiles everything, runs basic checks)
```

## Shannon API Development (Preferred)
```bash
make build-api        # Build unified Rust API
make run-api          # Run Rust API standalone
make test-api         # Run Shannon API tests
make fmt-api          # Format Shannon API code
make lint-api         # Lint Shannon API code
make check-api        # Check compilation without building
```

## Testing
```bash
make coverage         # Generate coverage reports
make coverage-gate    # Enforce coverage thresholds
make integration-tests # All integration tests
make replay HISTORY=path # Replay specific workflow history
make replay-export WORKFLOW_ID=task-123 # Export workflow for replay
```

## Desktop App Development
```bash
cd desktop
npm install           # Install dependencies
npm run dev           # Run in development mode (web UI)
npm run tauri:build   # Build native desktop app
npm run tauri:dev     # Run in Tauri dev mode
```

## Environment Troubleshooting
```bash
make setup-env        # Creates symlink and copies .env.example
make setup            # Full setup including proto generation
make check-env        # Validates configuration
```

## API Key Testing
```bash
make seed-api-key     # Creates sk_test_123456 for development
curl http://localhost:8080/health -H "Authorization: Bearer sk_test_123456"
```

## Common Workflow Patterns

### Adding New Tools
1. Define tool interface in `protos/agent/agent.proto`
2. Implement tool in appropriate service (usually LLM service)
3. Register tool in `python/llm-service/llm_service/tools/`
4. Add to tool routing logic

### Model Integration
1. Add provider configuration to `config/models.yaml`
2. Implement provider in `python/llm-service/llm_provider/`
3. Add routing logic and fallback handling

### Configuration Changes
Most configuration supports hot-reload:
1. Edit YAML files in `config/`
2. Changes auto-detected within ~30 seconds
3. Check logs: `docker compose logs orchestrator -f`

### Debugging Workflows
Use Temporal's time-travel debugging:
```bash
# Export workflow history
make replay-export WORKFLOW_ID=task-123

# Replay locally for debugging
make replay HISTORY=tests/histories/task-123.json
```