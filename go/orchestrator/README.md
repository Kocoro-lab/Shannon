# Shannon Orchestrator (Go)

The orchestrator is Shannon's central coordination service, managing AI agent workflows through Temporal with pattern-based cognitive architectures, enforcing policies, and handling session state.

## 🎯 Core Responsibilities

- **Pattern-Based Orchestration** - Routes tasks to cognitive patterns (CoT, ToT, ReAct, Debate, Reflection)
- **Workflow Management** - Coordinates multi-agent execution via Temporal workflows
- **Budget Management** - Tracks and enforces token usage limits across agents
- **Policy Enforcement** - Integrates OPA for security and compliance rules
- **Session Management** - Maintains conversation state across interactions
- **Service Coordination** - Routes between Rust agent core and Python LLM services

## 🏗️ Architecture

```
User Request → gRPC Server (:50052)
    ↓
OrchestratorRouter (Pattern Selection)
    ↓
Pattern Analysis → Cognitive Pattern Selection
    ├── Chain of Thought (CoT) - Sequential reasoning
    ├── Tree of Thoughts (ToT) - Exploration with backtracking
    ├── ReAct - Reasoning + Acting loops
    ├── Debate - Multi-agent argumentation
    └── Reflection - Self-improvement iterations
    ↓
Pattern Execution → Agent Coordination
    ↓
Results → Synthesis → Session Update → Response
```

## 📁 Project Structure

```
go/orchestrator/
├── main.go                      # Service entry point
├── Dockerfile                   # Container build
├── internal/
│   ├── activities/              # Temporal activity implementations
│   │   ├── agent.go            # Agent execution activities
│   │   ├── budget.go           # Budget tracking activities
│   │   ├── decompose.go        # Task decomposition
│   │   ├── synthesis.go        # Result synthesis
│   │   └── metrics.go          # Pattern metrics tracking
│   ├── workflows/               # Temporal workflow definitions
│   │   ├── orchestrator_router.go  # Main pattern router
│   │   ├── supervisor_workflow.go  # Retry & supervision
│   │   ├── simple_workflow.go      # Simple task execution
│   │   ├── patterns/               # Cognitive patterns
│   │   │   ├── chain_of_thought.go
│   │   │   ├── tree_of_thoughts.go
│   │   │   ├── react.go
│   │   │   ├── debate.go
│   │   │   └── reflection.go
│   │   ├── strategies/            # Legacy workflow strategies
│   │   │   ├── dag.go
│   │   │   ├── exploratory.go
│   │   │   ├── research.go
│   │   │   └── scientific.go
│   │   └── execution/             # Execution patterns
│   │       ├── parallel.go
│   │       ├── sequential.go
│   │       └── hybrid.go
│   ├── server/                  # gRPC service implementation
│   ├── policy/                  # OPA policy engine integration
│   ├── budget/                  # Token budget management
│   ├── auth/                    # Authentication & authorization
│   ├── db/                      # PostgreSQL operations
│   ├── health/                  # Health checks & degradation
│   ├── config/                  # Configuration management
│   ├── streaming/               # SSE/WebSocket streaming
│   └── circuitbreaker/          # Failure protection
├── histories/                   # Workflow replay test files
├── tests/                       # Integration tests
│   └── replay/                  # Determinism testing
└── tools/replay/                # Temporal replay tooling
```

## 🚀 Quick Start

### Prerequisites
- Go 1.21+
- Docker & Docker Compose
- PostgreSQL, Redis, Temporal running

### Development

```bash
# Install dependencies
go mod download

# Run tests
go test -race ./...

# Build binary
go build -o orchestrator .

# Run locally (requires services)
./orchestrator
```

### Docker Deployment

```bash
# Build image
docker build -t shannon-orchestrator .

# Run with compose (recommended)
make dev  # From repository root
```

## ⚙️ Configuration

Configuration is loaded from `/app/config/shannon.yaml` (mounted in Docker):

```yaml
# Key configuration sections
service:
  port: 50052           # gRPC port
  health_port: 8081     # Health check HTTP port

policy:
  enabled: true         # OPA policy enforcement
  mode: "dry-run"      # off | dry-run | enforce
  path: "/app/config/opa/policies"

temporal:
  host_port: "temporal:7233"
  namespace: "default"
  task_queue: "shannon-task-queue"

patterns:
  chain_of_thought:
    max_iterations: 10
    timeout: "5m"
  tree_of_thoughts:
    max_depth: 5
    branching_factor: 3
  react:
    max_steps: 15
    timeout: "10m"

budget:
  max_tokens_per_request: 10000
  max_cost_per_request: 1.0
```

### Environment Variables

- `PRIORITY_QUEUES` (default: empty)
  - When set to `on`/`true`/`1`, the orchestrator starts one Temporal worker per priority queue:
    - `shannon-tasks-critical`, `shannon-tasks-high`, `shannon-tasks` (normal), `shannon-tasks-low`
  - Concurrency per queue is tuned in `main.go`

- `ENABLE_TOOL_SELECTION` (default: `1`)
  - When enabled, the orchestrator calls the LLM service `/tools/select` to auto-populate `context.tool_calls`
  - This enables parallel tool execution in Agent Core when `TOOL_PARALLELISM > 1`

- Priority worker concurrency (optional overrides):
  - `WORKER_ACT_CRITICAL` / `WORKER_WF_CRITICAL` (default: `12` / `12`)
  - `WORKER_ACT_HIGH` / `WORKER_WF_HIGH` (default: `10` / `10`)
  - `WORKER_ACT_NORMAL` / `WORKER_WF_NORMAL` (default: `8` / `8`)
  - `WORKER_ACT_LOW` / `WORKER_WF_LOW` (default: `4` / `4`)

- Single-queue mode concurrency (when `PRIORITY_QUEUES` is off):
  - `WORKER_ACT` / `WORKER_WF` (default: `10` / `10`)

### Submit with Priority

Set the priority via `metadata.labels["priority"]` in `SubmitTaskRequest`.

Valid values: `critical`, `high`, `normal`, `low` (case-insensitive). Invalid values fall back to the default queue.

Example:
```go
req := &pb.SubmitTaskRequest{
    Metadata: &common.TaskMetadata{
        UserId: "user-123",
        Labels: map[string]string{"priority": "critical"},
    },
    Query: "Plan and execute task",
}
resp, err := client.SubmitTask(ctx, req)
```

## 🔧 Key Features

### Pattern-Based Workflows

**Cognitive Patterns:**
- `ChainOfThought` - Step-by-step logical reasoning
- `TreeOfThoughts` - Explores multiple solution paths with backtracking
- `ReAct` - Combines reasoning with action for interactive tasks
- `Debate` - Multi-agent argumentation for complex decisions
- `Reflection` - Iterative self-improvement

**Core Workflows:**
- `OrchestratorRouter` - Main entry point that selects patterns
- `SupervisorWorkflow` - Handles retries and supervision
- `SimpleWorkflow` - Direct execution for simple tasks

**Key Activities:**
- `DecomposeTask` - Analyzes complexity and creates subtasks
- `ExecuteAgent` - Runs individual agent tasks
- `SynthesizeResults` - Combines agent outputs
- `UpdateSessionResult` - Persists session state
- `RecordPatternMetrics` - Tracks pattern performance

### Budget Management

Token usage is tracked at multiple levels:
- Per-request budgets with backpressure
- Per-user quotas with circuit breakers
- Cost estimation before execution
- Real-time usage monitoring

### Policy Enforcement

OPA policies control:
- Task execution permissions
- Agent access controls
- Resource usage limits
- Data access boundaries

### Health & Degradation

Automatic degradation under load:
- Complex → Standard mode fallback
- Circuit breakers for external services
- Graceful timeout handling
- Health endpoint at `:8081/health`

## 📊 Observability

### Metrics (Prometheus format)
- **Endpoint**: `:2112/metrics`
- Workflow execution times
- Pattern selection distribution
- Token usage per pattern
- Error rates by workflow type

### Logging
- Structured JSON logging with zap
- Correlation IDs for request tracing
- Debug mode available via `LOG_LEVEL=debug`

### Streaming
- **SSE**: `/stream/sse?workflow_id=<id>`
- **WebSocket**: `/stream/ws?workflow_id=<id>`
- Real-time workflow progress updates

## 🧪 Testing

### Unit Tests
```bash
go test ./internal/...
```

### Integration Tests
```bash
# Requires running services
go test ./tests/integration/...
```

### Replay Testing
```bash
# Export workflow history
make replay-export WORKFLOW_ID=task-xxx OUT=histories/test.json

# Test determinism
make replay HISTORY=histories/test.json

# Run all replay tests
go test ./tests/replay
```

## 🚨 Common Issues

### Workflow Non-Determinism
- Ensure no `time.Sleep()` in activities
- Use `workflow.Sleep()` in workflows
- Register all activities with consistent names

### Budget Exceeded
- Check token limits in config
- Monitor usage via metrics
- Adjust `max_tokens_per_request`

### Pattern Selection
- Review decomposition results
- Check pattern confidence scores
- Monitor pattern metrics

## 📚 Further Documentation

- [Pattern Usage Guide](../../docs/pattern-usage-guide.md)
- [Multi-Agent Architecture](../../docs/multi-agent-workflow-architecture.md)
- [Testing Guide](../../docs/testing.md)
- [Main README](../../README.md)