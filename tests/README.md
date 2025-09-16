# Shannon Test Suite

This directory contains comprehensive testing infrastructure for Shannon, including end-to-end tests, integration tests, and configuration validation. Unit tests live next to source code in each language.

## Structure

- `e2e/` – End-to-end test scripts that call gRPC endpoints, check database state and metrics
- `integration/` – Cross-service integration tests for core functionality
- `fixtures/` – Optional seed data or sample payloads for test scenarios
- `histories/` – Temporal workflow history files for deterministic replay testing
- `utils/` – Utility tests for specific features and development tools
- `config_hot_reload_test.md` – Dynamic configuration hot-reload test documentation

## Prerequisites

The test scripts require the following tools on the host:
- `docker` and `docker compose` - Container orchestration
- `grpcurl` - gRPC API testing
- `psql` - PostgreSQL client (or use docker exec)
- `nc` (netcat) - Network connectivity checks (optional)
- `awk` - Text processing (standard on most systems)

## Running Tests

### Quick Start

1. Bring the stack up:
```bash
docker compose -f deploy/compose/compose.yml up -d
```

2. Run the smoke test:
```bash
make smoke
```

### Test Categories

#### End-to-End Tests
```bash
# Run all e2e tests
tests/e2e/run.sh

# Or individual e2e tests
./tests/e2e/calculator_test.sh
./tests/e2e/python_execution_test.sh
```

#### Integration Tests
```bash
# Run all integration tests
make integration-tests

# Or individual integration tests
make integration-single   # Single agent flow
make integration-session  # Session memory persistence
make integration-qdrant   # Vector database operations
```

#### Configuration Hot-Reload Test
See `config_hot_reload_test.md` for testing dynamic configuration updates without service restart.

## Temporal Workflow Replay Testing

The `histories/` directory contains workflow history files for deterministic replay testing. This ensures workflow code changes don't break compatibility.

### How to Export a Workflow History

1. **Using Make target** (recommended):
```bash
# Export with automatic naming to tests/histories/
make replay-export WORKFLOW_ID=task-dev-1234567890

# Export with custom path (creates directories if needed)
make replay-export WORKFLOW_ID=task-dev-1234567890 OUT=tests/histories/my-test.json
```

2. **Using the script**:
```bash
./scripts/replay_workflow.sh task-dev-1234567890
```

3. **Direct with Temporal CLI**:
```bash
docker compose -f deploy/compose/compose.yml exec temporal \
  temporal workflow show --workflow-id task-dev-1234567890 \
  --namespace default --address temporal:7233 --output json > tests/histories/my-test.json
```

**Note:** The Make target now defaults to `tests/histories/` with timestamp-based filenames when no OUT is specified.

### How to Test Replay

1. **Single history file**:
```bash
make replay HISTORY=tests/histories/simple-math-task.json
```

2. **All histories (CI)**:
```bash
make ci-replay
```

### Adding Test Histories

1. Run a workflow to completion
2. Export its history using one of the methods above
3. Save to `tests/histories/` with a descriptive name
4. The CI pipeline will automatically replay all histories on every build
