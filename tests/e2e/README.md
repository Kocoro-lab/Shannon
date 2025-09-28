# Shannon E2E Test Suite

## Overview

This directory contains comprehensive end-to-end tests for the Shannon platform, covering workflows, tool execution, multi-agent orchestration, and memory systems.

## Test Organization

Tests are numbered by category for clear organization:

### Core Utility Scripts
- **`run.sh`** - Master test runner that executes all E2E tests in sequence
- **`submit_and_get_response.sh`** - Helper script for task submission and response retrieval
- **`verify_metrics.sh`** - Validates metrics increment correctly after workflow execution

### Feature Tests (01-09)
- **`01_basic_calculator_test.sh`** - Calculator tool functionality
  - Simple arithmetic (handled by LLM directly)
  - Complex calculations (triggers calculator tool)
  - Direct calculator tool API execution

- **`02_python_execution_test.sh`** - Python code execution via WASI sandbox
  - Direct WASM module compilation and execution
  - Base64-encoded WASM payload testing
  - Workflow integration with code_executor

- **`03_python_interpreter_test.sh`** - Python interpreter integration
  - Checks for Python WASM interpreter (~20MB)
  - Tests current architecture limitations

- **`04_web_search_test.sh`** - Web search and synthesis capabilities
  - Search query execution
  - Result synthesis

- **`05_cognitive_patterns_test.sh`** - Cognitive reasoning patterns
  - Chain of Thought
  - Tree of Thoughts
  - Graph of Thoughts
  - Reflexion patterns

### Workflow Tests (10-19)
- **`10_supervisor_workflow_test.sh`** - Complex multi-agent orchestration
  - Subtask decomposition
  - Agent coordination
  - Complex workflow execution
  - Supervisor memory learning (merged from enhanced tests)

### P2P Coordination Tests (20-29)
- **`20_p2p_coordination_test.sh`** - Comprehensive P2P agent messaging
  - Sequential dependency detection
  - Force P2P mode with `context: {"force_p2p": true}`
  - Complex pipeline dependencies
  - Mailbox communication
  - Workspace data exchange via Redis
  - Parallel vs sequential detection

- **`21_p2p_memory_test.sh`** - P2P with supervisor memory integration
  - Memory retrieval for identical tasks
  - Pattern recognition for similar tasks
  - Combined P2P and memory functionality

### Memory Tests (30-39)
- **`30_memory_system_test.sh`** - Memory persistence and retrieval
  - Session memory storage
  - Hierarchical memory retrieval
  - Vector similarity search in Qdrant

- **`31_session_continuity_test.sh`** - Session context continuity
  - Cross-query context retention
  - Token budget tracking
  - Session compression

## Running Tests

### Run All Tests
```bash
./run.sh
```

### Run Specific Category
```bash
# Feature tests only (01-09)
./run.sh --feature

# Workflow tests only (10-19)
./run.sh --workflow

# P2P tests only (20-29)
./run.sh --p2p

# Memory tests only (30-39)
./run.sh --memory
```

### Run Individual Test
```bash
# Run specific test directly
./01_basic_calculator_test.sh

# With custom session ID
SESSION_ID="test-$(date +%s)" ./10_supervisor_workflow_test.sh
```

### Verify Metrics
```bash
# Run test and verify metrics increments
./verify_metrics.sh
```

## Test Database

All tests now use the `task_executions` table as the primary data store:
- Tasks are persisted with full metrics
- Session linkage supports non-UUID session IDs
- Supervisor memory queries join with `task_executions`

## Prerequisites

1. Services must be running:
```bash
make dev
```

2. Environment variables configured:
```bash
make setup-env
```

3. Python WASI interpreter (for Python tests):
```bash
./scripts/setup_python_wasi.sh
```

## Common Test Patterns

### Submit Task and Wait
```bash
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId": "test", "sessionId": "test-session"},
  "query": "Your query here"
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask | jq -r .taskId)

# Poll for completion
./submit_and_get_response.sh "$TASK_ID"
```

### Force P2P Coordination
```bash
grpcurl -plaintext -d '{
  "metadata": {"userId": "test", "sessionId": "test-session"},
  "query": "Your query",
  "context": {"force_p2p": "true"}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### Check Database
```bash
# Check task_executions table
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c "SELECT * FROM task_executions ORDER BY created_at DESC LIMIT 5;"
```

## Troubleshooting

### Tests Failing
1. Check services are healthy: `docker compose ps`
2. View logs: `docker compose logs -f [service]`
3. Verify database: `make psql`

### Slow Performance
1. Check token budgets in Redis
2. Monitor metrics: `curl localhost:2112/metrics`
3. Review Temporal UI: http://localhost:8088

### P2P Not Triggering
1. Ensure query contains dependency keywords ("then", "after", "based on")
2. Or force with: `"context": {"force_p2p": "true"}`
3. Check orchestrator logs for routing decisions

## Test Maintenance

When adding new tests:
1. Follow the numbering convention (XX_test_name.sh)
2. Update this README with test description
3. Ensure test uses `task_executions` table
4. Add to appropriate section in `run.sh`

## CI Integration

Tests are run in CI via:
```bash
make test  # Runs unit tests
make smoke # Runs smoke tests
make e2e   # Runs full E2E suite
```