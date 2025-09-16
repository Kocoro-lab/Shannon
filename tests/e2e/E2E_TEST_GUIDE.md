# Shannon Platform E2E Testing Guide

## Overview
This guide provides comprehensive E2E testing examples and accurate technical guidance for the Shannon multi-agent AI platform based on the current implementation.

## Core Workflow Types

### 1. SimpleTaskWorkflow
Used for straightforward queries with low complexity scores (< 0.3).
```bash
# Simple tasks
./scripts/submit_task.sh "What is 2+2?"
./scripts/submit_task.sh "Define recursion"
./scripts/submit_task.sh "Calculate 37 * 89 + 156"
```

### 2. SupervisorWorkflow
Used for complex multi-agent orchestration with subtask decomposition.
```bash
# Complex tasks requiring multiple agents
./scripts/submit_task.sh "Design a distributed system with caching, load balancing, and fault tolerance"

# Multi-step analysis
./scripts/submit_task.sh "Perform a comprehensive analysis: \
1) Research the top 3 programming languages in 2024. \
2) For each language, calculate its growth rate over the past 5 years. \
3) Create a comparison matrix of their features. \
4) Generate predictions for their popularity in 2025. \
5) Synthesize all findings into an executive summary with recommendations."
```

### 3. DAG Strategy (strategies.DAGWorkflow)
Selected based on DecompositionResult for tasks with dependency graphs.
```bash
# Tasks with explicit dependencies
./scripts/submit_task.sh "First analyze data trends, then predict outcomes, finally generate recommendations"

# Financial calculation chain
./scripts/submit_task.sh "Execute these dependent calculations: \
Step A: Calculate monthly payment for a $300,000 mortgage at 4.5% for 30 years. \
Step B: Use result from A to determine total interest paid over loan lifetime. \
Step C: Use result from B to calculate break-even point if investing the interest instead at 7% annual return."
```

**Note**: Router selects workflow type based on DecompositionResult at `go/orchestrator/internal/workflows/orchestrator_router.go:41-143`.

## Cognitive Pattern Test Examples

### Chain of Thought (Sequential Reasoning)
```bash
./scripts/submit_task.sh "Solve step by step: A store offers 20% discount on all items. \
If you buy 3 shirts at $30 each and 2 pants at $50 each, \
and there's an additional 10% loyalty discount on the discounted price, \
what is your final payment including 8% sales tax?"
```

### Tree of Thoughts (Exploration)
```bash
./scripts/submit_task.sh "Explore different strategies to solve: \
You need to transport 100 people across a river. \
You have one boat that holds 5 people and takes 10 minutes per trip. \
Consider different optimization strategies for minimal time."
```

### ReAct (Reasoning + Action)
```bash
./scripts/submit_task.sh "Research current Bitcoin price, \
calculate how many you could buy with $50,000, \
then determine the value if Bitcoin increases by 25% in 6 months."
```

### Debate Pattern
```bash
./scripts/submit_task.sh "Debate the pros and cons of AI automation in healthcare. \
Present arguments from medical professionals, patients, and technology advocates. \
Synthesize a balanced conclusion with policy recommendations."
```

### Reflection Pattern
```bash
./scripts/submit_task.sh "Write a sorting algorithm in Python. \
Analyze its time complexity. \
Identify potential improvements. \
Implement an optimized version with explanations."
```

## Supervisor Workflow Characteristics

### Sequential Execution with Retry Logic
The SupervisorWorkflow executes tasks **sequentially**, NOT concurrently:

Key behaviors:
- **Sequential task execution** (`for i, st := range decomp.Subtasks`)
- **Max 3 retries per task** (`maxRetriesPerTask := 3`)
- **50%+1 failure threshold** before workflow abort
- **Workspace updates** after each task completion
- **NO 5-concurrent-agent semaphore** (that's outdated documentation)

Test examples:
```bash
# Parallel task description (but executes sequentially)
./scripts/submit_task.sh "Break this into 3 separate tasks: \
Task 1: Calculate the factorial of 20. \
Task 2: Generate the first 15 Fibonacci numbers. \
Task 3: Find all prime numbers between 1 and 100. \
Then combine all results into a summary report."

# Complex data processing
./scripts/submit_task.sh "Analyze this dataset: \
1) Calculate mean, median, mode for: [23, 45, 67, 89, 12, 34, 56, 78, 90, 23, 45]. \
2) Identify outliers using IQR method. \
3) Generate a statistical summary. \
4) Provide insights about the data distribution."
```

## Streaming APIs

### gRPC Streaming (Correct Service)
```bash
# Correct gRPC streaming command
grpcurl -plaintext -d '{"workflow_id":"<workflow_id>"}' \
  localhost:50052 shannon.streaming.StreamingService/StreamTaskExecution

# Or use the streaming script
./scripts/stream_smoke.sh <workflow_id>
```
**Note**: Old `OrchestratorService/StreamTaskUpdates` no longer exists.

### SSE Streaming
```bash
# Submit task and capture workflow ID
RESULT=$(./scripts/submit_task.sh "Hello world")
WF_ID=$(echo $RESULT | grep -o 'workflowId":"[^"]*' | cut -d'"' -f3)

# Test SSE streaming
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF_ID"
```

### WebSocket Streaming
```bash
wscat -c "ws://localhost:8081/stream/ws?workflow_id=$WF_ID"
```

## Session Memory Tests

```bash
# First query - establish context
SESSION_ID="test-session-$(date +%s)"
./scripts/submit_task.sh "My name is Alice and I'm working on a project about renewable energy" "$SESSION_ID"

# Follow-up query - test memory
./scripts/submit_task.sh "What's my name and what project am I working on?" "$SESSION_ID"

# Context-dependent calculation
./scripts/submit_task.sh "Based on my project topic, calculate the ROI of a 10kW solar installation" "$SESSION_ID"

# Verify database persistence
docker compose exec postgres psql -U shannon -d shannon \
  -c "SELECT workflow_id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 5;"
```

## Tool Execution Tests

```bash
# Web search tool
./scripts/submit_task.sh "Search for the latest developments in quantum computing and summarize the top 3 breakthroughs"

# Calculator tool
./scripts/submit_task.sh "Use the calculator tool to solve: (sqrt(144) * 3^4) / (2 * pi)"

# Python executor (requires WASI setup)
./scripts/submit_task.sh "Execute this Python code: \
import math
result = sum([math.factorial(i) for i in range(1, 6)])
print(f'Sum of factorials 1-5: {result}')"

# MCP tool registration
curl -X POST http://localhost:8000/tools/register \
  -H "Content-Type: application/json" \
  -d '{"name": "test_echo", "type": "mcp", "config": {...}}'
```

## Budget & Token Management

### Budget Preflight Checks
Budget enforcement requires configuration to be enabled:

```bash
# Test backpressure (requires budgets enabled in config/shannon.yaml)
for i in {1..10}; do
  ./scripts/submit_task.sh "Generate a long story" &
done
```

**Important**: The 80% backpressure threshold only activates if:
1. Budget configuration is enabled in `config/shannon.yaml`
2. User has token limits configured
3. Database tracking is operational

### Token Usage Tests
```bash
# Token-limited task
./scripts/submit_task.sh "In exactly 50 words, explain how photosynthesis works"

# Check metrics
curl -s http://localhost:2112/metrics | grep shannon_token

# Query database
docker compose exec postgres psql -U shannon -d shannon \
  -c "SELECT workflow_id, tokens_used, status FROM tasks ORDER BY created_at DESC LIMIT 5;"
```

## Error Handling and Recovery

```bash
# Invalid calculation
./scripts/submit_task.sh "Calculate the square root of -16 in real numbers"

# Timeout simulation
./scripts/submit_task.sh "Perform an extremely complex calculation that might timeout: \
Calculate all prime numbers up to 1 million and find their sum"

# Recovery from partial failure
./scripts/submit_task.sh "Complete these tasks even if some fail: \
1) Calculate 10/0 \
2) Find factorial of 20 \
3) Compute fibonacci(15)"
```

## Health & Metrics

```bash
# Health endpoints
curl http://localhost:8081/health    # Orchestrator
curl http://localhost:8000/health/   # LLM Service
grpcurl -plaintext localhost:50051 shannon.agent.AgentService/HealthCheck

# Metrics
curl -s http://localhost:2112/metrics | grep shannon_  # Orchestrator
curl -s http://localhost:2113/metrics | grep shannon_  # Agent-Core
```

## Common Test Commands

```bash
# Run smoke tests
make smoke

# Run full E2E suite
./tests/e2e/run.sh

# Run specific test suite
./supervisor_workflow_test.sh
./cognitive_patterns_test.sh

# Monitor workflows in Temporal UI
open http://localhost:8088

# Check recent workflow types
docker compose exec temporal temporal workflow list --address temporal:7233 | head -10

# Verify specific workflow completion
docker compose exec temporal temporal workflow describe \
  --workflow-id <workflow_id> --address temporal:7233 | grep Status

# WASI tool testing
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm
cd rust/agent-core && cargo test test_code_executor_with_base64_payload
```

## Performance Benchmarks

Based on current implementation:
- **SimpleTaskWorkflow**: < 5s
- **SupervisorWorkflow** (2 subtasks): 30-60s
- **SupervisorWorkflow** (4+ subtasks): 60-120s
- **DAG Strategy**: 15-60s depending on complexity

### Token Usage Expectations
- Simple queries: 100-500 tokens
- Standard analysis: 500-2000 tokens
- Complex workflows: 2000-10000 tokens
- Supervisor orchestration: 5000-20000 tokens

## Troubleshooting

### Stuck Workflows
If a SupervisorWorkflow gets stuck:
1. Check for P2P demo code (should be disabled with `if false`)
2. Check dependency sync code (should be disabled with `if false`)
3. Ensure full service restart to clear cached workflow code

```bash
# Force restart to clear cached code
docker compose -f deploy/compose/compose.yml down
docker compose -f deploy/compose/compose.yml up -d

# Terminate stuck workflow
docker compose exec temporal temporal workflow terminate \
  --workflow-id WORKFLOW_ID --address temporal:7233 --reason "Manual termination"
```

### Build ID Mismatches
Temporal workers cache workflow code. After changes:
1. Rebuild: `docker compose -f deploy/compose/compose.yml build orchestrator`
2. Restart: `docker compose -f deploy/compose/compose.yml restart orchestrator`
3. New workflows will use updated code (check BuildId in workflow describe)

### Memory Issues
```bash
# Check memory usage
docker stats

# Restart services if needed
docker compose restart orchestrator llm-service
```

## Architecture Summary

1. **No BudgetedDAGWorkflow**: System uses SimpleTaskWorkflow, SupervisorWorkflow, or strategies.DAGWorkflow
2. **Sequential Supervisor**: Tasks execute in order with retry/failure logic, NOT concurrent semaphore
3. **Streaming Service**: Use StreamingService/StreamTaskExecution, not OrchestratorService methods
4. **Budget Optional**: Backpressure tests require budget configuration enabled
5. **Build Caching**: Full restart needed after workflow code changes to clear Temporal worker cache

## Best Practices

1. **Test Isolation**: Use unique session IDs for each test run
2. **Cleanup**: Always terminate long-running workflows after tests
3. **Validation**: Check both Temporal workflows and database records
4. **Monitoring**: Use metrics endpoints to track performance
5. **Documentation**: Document new test patterns as you discover them

## References

- [Multi-Agent Workflow Architecture](../../docs/multi-agent-workflow-architecture.md)
- [Pattern Usage Guide](../../docs/pattern-usage-guide.md)
- [Streaming API Documentation](../../docs/streaming-api.md)
- [Testing Guide](../../docs/testing.md)