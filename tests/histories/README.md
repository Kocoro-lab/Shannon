# Temporal Workflow History Replay Testing

## Overview
This directory contains workflow history files for deterministic replay testing in CI.

## Current Status (2025-09-07)

### ✅ Accomplished
1. **Fixed UpdateSessionResult handling** - All workflows now properly wait for session updates
2. **Verified consistency** - All 4 workflow types consistently handle session updates:
   - `simple_workflow.go`
   - `dag_workflow.go`  
   - `dag_workflow_with_approval.go`
   - `dag_workflow_budgeted.go`

3. **Rebuilt and tested** - New workflows complete with 29 events (no pending activities)

### 📝 Test Results
- **Old workflows** (before fix): 24 events with 1 pending `UpdateSessionResult` activity
- **New workflows** (after fix): 29 events with NO pending activities
- Old workflow examples: task-dev-1757245829, task-dev-1757246966
- New workflow examples: task-dev-1757247424, task-dev-1757247667

### ⚠️ Known Issues
- **Replay failures for old workflows are EXPECTED** - This is due to the intentional breaking change where we fixed the fire-and-forget bug

## How to Add Test Histories

1. **Run a workflow** to completion:
   ```bash
   ./scripts/submit_task.sh "Your test query"
   ```

2. **Export the history** using temporal CLI:
   ```bash
   # Export directly as JSON
   docker compose -f deploy/compose/compose.yml exec temporal \
     temporal workflow show \
     --workflow-id <workflow-id> \
     --address temporal:7233 \
     --output json > tests/histories/history.json
   ```

3. **Save with descriptive name**:
   ```bash
   mv tests/histories/history.json tests/histories/test-<description>.json
   ```

## CI Integration
The CI pipeline automatically runs `make ci-replay` which:
- Checks for any `*.json` files in this directory
- Replays each one against current workflow code
- Fails the build if any replay shows non-determinism
- Skips gracefully if no histories are present

## Best Practices
- Create new test histories after significant workflow changes
- Name files descriptively (e.g., `simple-math-task.json`, `complex-dag-workflow.json`)
- Document any expected failures in this README
- Keep only representative histories, not every test run