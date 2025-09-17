# Shannon E2E Test Suite

## Overview

This directory contains end-to-end tests for the Shannon platform, providing comprehensive testing of workflows, tool execution, and multi-agent orchestration.

## Test Scripts

### Core Test Scripts

1. **calculator_test.sh** - Tests calculator tool functionality
   - Simple arithmetic (handled by LLM directly)
   - Complex calculations (triggers calculator tool)
   - Direct calculator tool API execution
   - Tool registration verification

2. **python_execution_test.sh** - Tests Python code execution via WASI sandbox
   - Direct WASM module compilation and execution
   - Base64-encoded WASM payload testing
   - Workflow integration with code_executor
   - Fibonacci WASM module execution

3. **python_interpreter_test.sh** - Tests Python interpreter integration
   - Checks for Python WASM interpreter (~20MB)
   - Tests current architecture limitations
   - Documents requirements for proper Python support

4. **supervisor_workflow_test.sh** - Tests complex multi-agent orchestration
   - Subtask decomposition
   - Agent coordination
   - Complex workflow execution

5. **cognitive_patterns_test.sh** - Tests cognitive reasoning patterns
   - Chain of Thought
   - Tree of Thoughts
   - Graph of Thoughts
   - Reflexion patterns

### Helper Scripts

- **submit_and_get_response.sh** - Helper for task submission and retrieval
- **run.sh** - Master script to run all E2E tests

## Running Tests

### Prerequisites
```bash
# Start all services
make dev

# Verify services are healthy
make smoke

# Install required tools
brew install wabt  # For wat2wasm
```

### Run Individual Tests
```bash
./tests/e2e/calculator_test.sh
./tests/e2e/python_execution_test.sh
./tests/e2e/supervisor_workflow_test.sh
./tests/e2e/cognitive_patterns_test.sh
```

### Run All Tests
```bash
./tests/e2e/run.sh
```

## Workflow Types and Examples

### 1. SimpleTaskWorkflow
Used for straightforward queries with complexity scores < 0.3.

```bash
# Simple calculations
./scripts/submit_task.sh "What is 2+2?"

# Basic definitions
./scripts/submit_task.sh "Define recursion"

# Direct calculations
./scripts/submit_task.sh "Calculate 37 * 89 + 156"
```

### 2. SupervisorWorkflow
Used for complex multi-agent orchestration with subtask decomposition.

```bash
# System design
./scripts/submit_task.sh "Design a distributed system with caching, load balancing, and fault tolerance"

# Multi-step analysis
./scripts/submit_task.sh "Perform a comprehensive analysis: \
1) Research the top 3 programming languages in 2024. \
2) Calculate growth rates over 5 years. \
3) Create a feature comparison matrix. \
4) Generate 2025 predictions. \
5) Synthesize findings into recommendations."
```

### 3. DAG Strategy Workflow
Selected for tasks with explicit dependency graphs.

```bash
# Sequential dependencies
./scripts/submit_task.sh "First analyze data trends, then predict outcomes, finally generate recommendations"

# Financial calculations with dependencies
./scripts/submit_task.sh "Execute dependent calculations: \
A: Calculate mortgage payment for $300K at 4.5% for 30 years. \
B: Determine total interest from A. \
C: Calculate investment break-even using B at 7% return."
```

### 4. Research Workflow
Optimized for research and information gathering.

```bash
./scripts/submit_task.sh "Research quantum computing: current state, major players, recent breakthroughs, and 5-year outlook"
```

### 5. React Workflow
For tasks requiring iterative reasoning and action.

```bash
./scripts/submit_task.sh "Debug why a web server returns 500 errors: check logs, analyze patterns, identify root cause"
```

## Cognitive Pattern Examples

### Chain of Thought (Sequential Reasoning)
```bash
./scripts/submit_task.sh "Solve step by step: Store offers 20% discount. \
Buy 3 shirts at $30 each and 2 pants at $50 each. \
Apply additional 10% loyalty discount. \
Calculate final payment with 8% sales tax."
```

### Tree of Thoughts (Exploration)
```bash
./scripts/submit_task.sh "Explore strategies to transport 100 people across a river. \
You have: 1 boat (capacity: 10), 1 raft (capacity: 5), 1 bridge (under repair). \
Consider safety, time, and resource constraints."
```

### Graph of Thoughts (Non-linear)
```bash
./scripts/submit_task.sh "Analyze the AI ecosystem: \
Map relationships between LLMs, training data, compute resources, and applications. \
Show how advances in one area impact others."
```

## Architecture Insights

### Tool Execution Flow
1. Task decomposition suggests required tools
2. Rust agent-core handles certain tools directly (calculator, code_executor)
3. Other tools forwarded to LLM service's `/tools/execute` endpoint
4. WASI sandbox executes WASM modules with security restrictions

### Current Limitations
- No Python-to-WASM transpilation bridge
- `python_wasi_runner` mentioned in docs but not implemented
- WASI sandbox cannot pass command-line arguments to interpreters
- Python interpreter requires special handling not yet supported

### Key Findings
- **Simple math**: Intentionally handled by LLM directly, not tools
- **WASM execution**: System correctly processes WASM but lacks Python-to-WASM bridge
- **Code executor**: Expects standalone WASM, not interpreters needing arguments
- **Workflow routing**: Based on DecompositionResult at `orchestrator_router.go:41-143`

## Debugging Failed Tests

### Check Service Health
```bash
# Service status
docker compose -f deploy/compose/compose.yml ps

# Service logs
docker compose -f deploy/compose/compose.yml logs orchestrator
docker compose -f deploy/compose/compose.yml logs agent-core
```

### Verify Workflow Execution
```bash
# Check Temporal workflows
docker compose exec temporal temporal workflow list --namespace default

# Check specific workflow
docker compose exec temporal temporal workflow describe \
  --workflow-id YOUR_WORKFLOW_ID --namespace default
```

### Database Verification
```bash
# Check task status
docker compose exec postgres psql -U shannon -d shannon \
  -c "SELECT workflow_id, status, result FROM tasks ORDER BY created_at DESC LIMIT 5;"
```

## Test Data Patterns

Tests create data with identifiable patterns for easy cleanup:
- Workflow IDs: `task-e2e-{test}-{timestamp}`
- User IDs: `e2e-test-user-{timestamp}`
- Session IDs: `e2e-test-session-{timestamp}`

## Future Improvements

1. **Implement python_wasi_runner tool**
   - Handle Python interpreter invocation
   - Pass code via stdin or arguments
   - Manage interpreter lifecycle

2. **Enhanced WASI Executor**
   - Support command-line arguments
   - File system mounting for scripts
   - Better interpreter integration

3. **Python Transpilation**
   - Direct Python → WASM conversion
   - Avoid interpreter overhead
   - Better performance for simple scripts

## Contributing

When adding new E2E tests:
1. Follow naming convention: `*_test.sh`
2. Include comprehensive validation steps
3. Add test description to this README
4. Use consistent output formatting (PASS/FAIL/INFO)
5. Include cleanup for test data
6. Add to `run.sh` for inclusion in full suite
