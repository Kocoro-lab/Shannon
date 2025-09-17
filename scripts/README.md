# Shannon Scripts

This directory contains utility scripts for Shannon platform operations, development, and testing.

## Core Scripts

### User Interface
- **`submit_task.sh`** - Submit tasks to Shannon via gRPC. Primary user interface for testing.
  ```bash
  ./scripts/submit_task.sh "Your query here"
  ```

### Testing & Validation
- **`smoke_e2e.sh`** - End-to-end smoke test that validates the entire system flow.
  Used by `make smoke`.

- **`stream_smoke.sh`** - Tests SSE/WebSocket streaming capabilities.
  Used by `make stream`.

### Setup & Initialization
- **`bootstrap_qdrant.sh`** - Creates required Qdrant collections from the host. Used by `make dev`.
- **`init_qdrant.sh`** - Container entrypoint wrapper that invokes the shared Qdrant migration script.
- **`seed_postgres.sh`** - Seeds PostgreSQL with initial schema and data. Used by `make dev`.
- **`install_buf.sh`** - Installs the `buf` CLI tool for protobuf management when missing.

### Development Tools
- **`replay_workflow.sh`** - Replays a Temporal workflow from exported history for determinism debugging.
- **`signal_team.sh`** - Sends Temporal signals (recruit/retire) to running workflows during manual testing.

## Test Scripts

Test scripts have been moved to `tests/scripts/` for better organization:
- `test_budget_controls.sh` - Tests token budget enforcement
- `test_ci_local.sh` - Runs CI pipeline locally
- `test_grpc_reflection.sh` - Validates gRPC reflection API
- `test_token_aggregation.sh` - Tests token usage aggregation

## Usage in Makefile

These scripts are integrated into the Makefile targets:
- `make dev` → Uses `seed_postgres.sh`, `bootstrap_qdrant.sh`
- `make smoke` → Runs `smoke_e2e.sh`
- `make stream` → Runs `stream_smoke.sh`
- `make proto` → May call `install_buf.sh`

## Best Practices

1. All scripts should have proper shebangs (`#!/bin/bash` or `#!/usr/bin/env python3`)
2. Use `set -e` to exit on errors
3. Include descriptive comments at the top
4. Make scripts idempotent where possible
5. Use meaningful exit codes

## Contributing

When adding new scripts:
1. Follow the snake_case naming convention
2. Add a `.sh` extension for bash scripts
3. Make the script executable: `chmod +x script_name.sh`
4. Update this README with a description
5. Consider if it should be integrated into the Makefile
