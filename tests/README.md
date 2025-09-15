# End-to-End Tests

This directory contains cross-service, end-to-end test harnesses that exercise Shannon with the Docker Compose stack. Unit tests live next to source code in each language.

## Structure

- `e2e/` – scripts that call gRPC endpoints, check database state and metrics
- `fixtures/` – optional seed data or sample payloads for scenarios
- `histories/` – Temporal workflow history files for deterministic replay testing

## Prerequisites

The test scripts require the following tools on the host:
- `docker` and `docker compose` - Container orchestration
- `grpcurl` - gRPC API testing
- `psql` - PostgreSQL client (or use docker exec)
- `nc` (netcat) - Network connectivity checks (optional)
- `awk` - Text processing (standard on most systems)

## Running

1. Bring the stack up:

```bash
docker compose -f deploy/compose/compose.yml up -d
```

2. Run the smoke + e2e checks:

```bash
make smoke
tests/e2e/run.sh
```

The smoke test verifies service health, a basic SubmitTask→GetTaskStatus round-trip, persistence in Postgres, and metrics endpoints. The e2e script can be extended with more scenarios over time.

## Temporal Workflow Replay Testing

The `histories/` directory contains workflow history files for deterministic replay testing. This ensures workflow code changes don't break compatibility.

### How to Export a Workflow History

1. **Using Make target** (recommended):
```bash
make replay-export WORKFLOW_ID=task-dev-1234567890 OUT=my-test.json
```

2. **Using the script**:
```bash
./scripts/replay_workflow.sh task-dev-1234567890
```

3. **Direct with Temporal CLI**:
```bash
docker compose -f deploy/compose/compose.yml exec temporal \
  temporal workflow show --workflow-id task-dev-1234567890 \
  --namespace default --address temporal:7233 --output json > history.json
```

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

### Migration Notes (2025-09-07)

- **Migrated from `tctl` to `temporal` CLI** for proper JSON export
- **Helper script available**: `scripts/tctl_to_json.py` for legacy environments
- **Breaking change**: Fixed `UpdateSessionResult` handling - old workflows (before the fix) will fail replay as expected

