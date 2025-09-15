# Workflow History Files

This directory contains Temporal workflow history files for replay testing.

## Generating History Files

To generate history files for replay testing:

```bash
# Export a workflow history
make replay-export WORKFLOW_ID=task-dev-XXX OUT=histories/dag_v2_simple.json

# Or use the replay tool directly
cd go/orchestrator
go run tools/replay/main.go -export -workflow-id task-dev-XXX -output histories/dag_v2_simple.json
```

## Required History Files

The replay tests expect these files:
- `dag_v2_simple.json` - Simple DAG workflow execution
- `dag_v2_parallel.json` - Parallel agent execution
- `dag_v2_reflection.json` - Workflow with reflection
- `react_v2_basic.json` - Basic ReAct workflow
- `react_v2_reflection.json` - ReAct with reflection

## Running Replay Tests

```bash
# Run replay tests
cd go/orchestrator
go test ./tests/replay

# Or use make
make ci-replay
```

Note: History files are not checked into version control as they can be large and contain environment-specific data.