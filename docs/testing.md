# Shannon Testing Guide

Comprehensive guide for testing the Shannon platform, from unit tests to end-to-end scenarios.

## Test Types

### Unit Tests
- **Go**: Co-located with code as `_test.go` files
- **Rust**: Inline `#[cfg(test)]` modules inside crates
- **Python**: Located in `python/llm-service/tests/`

### Integration Tests
- **Temporal Workflows**: In-memory execution using Temporal testsuite with stubbed activities
- **Smoke Tests**: Basic cross-service connectivity, persistence, metrics validation
- **End-to-End (E2E)**: Multi-service scenarios under `tests/` with Docker Compose

## Quick Start

```bash
# 1. Run all unit tests
make test

# 2. Start the full stack
make dev

# 3. Run smoke test (health, gRPC, persistence, metrics)
make smoke

# 4. Run E2E scenarios
tests/e2e/run.sh
```

## Detailed Test Commands

### Unit Testing by Language

```bash
# Go tests with race detection
cd go/orchestrator && go test -race ./...

# Rust tests with output
cd rust/agent-core && cargo test -- --nocapture

# Python tests with coverage
cd python/llm-service && python3 -m pytest --cov

# WASI sandbox test
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm
cd rust/agent-core && cargo run --example wasi_hello -- /tmp/hello-wasi.wasm
```

### Temporal Workflow Testing

```bash
# Export workflow history
make replay-export WORKFLOW_ID=task-dev-XXX OUT=history.json

# Test determinism
make replay HISTORY=history.json

# Run all replay tests
make ci-replay
```

### Smoke Test Details

The smoke test (`make smoke`) validates:
- Temporal UI reachability (http://localhost:8088)
- Agent-Core gRPC health and ExecuteTask
- Orchestrator SubmitTask + GetTaskStatus progression
- LLM service health/live/ready endpoints
- Qdrant readiness and required collections
- PostgreSQL connectivity and migrations
- Prometheus metrics endpoints (:2112, :2113)

### Manual Service Verification

```bash
# Agent-Core health check
grpcurl -plaintext localhost:50051 shannon.agent.AgentService/HealthCheck

# Orchestrator submit task
grpcurl -plaintext \
  -d '{"metadata":{"user_id":"dev","session_id":"s1"},"query":"Say hello"}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask

# LLM service health
curl -fsS http://localhost:8000/health
curl -fsS http://localhost:8000/health/ready

# Metrics endpoints
curl http://localhost:2112/metrics | head  # Orchestrator
curl http://localhost:2113/metrics | head  # Agent Core
```

## Test File Locations

### Go Tests
- `go/orchestrator/internal/workflows/*_test.go` - Workflow tests
- `go/orchestrator/internal/activities/*_test.go` - Activity tests
- `go/orchestrator/internal/session/manager_test.go` - Session management
- `go/orchestrator/tests/replay/workflow_replay_test.go` - Replay validation

### Rust Tests
- `rust/agent-core/src/memory.rs` - TTL, limits, LRU eviction
- `rust/agent-core/src/wasi_sandbox.rs` - Path validation, sandboxed FS
- `rust/agent-core/src/tools.rs` - Tool executor tests

### Python Tests
- `python/llm-service/tests/test_manager.py` - Provider routing, cache, fallback
- `python/llm-service/tests/test_ratelimiter.py` - Rate limiting
- `python/llm-service/tests/test_tools.py` - Tool execution

## CI Pipeline

GitHub Actions runs:
1. Rust tests with clippy linting
2. Go build and tests with race detection
3. Python linting (ruff) and pytest
4. Temporal workflow replay tests
5. Coverage reporting (informational)

## Coverage Requirements & Setup

### Coverage Thresholds
- **Go**: Minimum 50% coverage (current: ~57%)
- **Python**: Baseline 20% coverage (current: ~15%, target: 70%)
- **Rust**: Informational only (no minimum yet)

### Running Coverage Tests

```bash
# Run all coverage gates
make coverage-gate

# Individual language coverage
make coverage-go       # Go coverage with threshold check
make coverage-python   # Python coverage with venv setup

# For Rust coverage, use cargo tarpaulin directly:
cd rust/agent-core && cargo tarpaulin --out Html

# Generate detailed reports
cd go/orchestrator && go test -coverprofile=coverage.out -covermode=atomic ./...
cd python/llm-service && pytest --cov=. --cov-report=html
```

### Well-Covered Modules
- **Go Budget Manager**: 81%+ coverage
- **Go Circuit Breaker**: 61%+ coverage
- **Python Rate Limiter**: Good timing test coverage
- **Rust Memory Manager**: LRU eviction and TTL tests

### Coverage Goals

Current focus areas:
- Gateway enforcement: timeouts, rate limits, circuit breakers
- Budget manager: edge cases, DB operations, token tracking
- LLM routing: provider fallback, rate limiting, caching
- WASI sandbox: path traversal protection, resource limits
- Pattern workflows: CoT, ToT, ReAct, Debate, Reflection

## Troubleshooting

### Common Issues

**Orchestrator cannot connect to Temporal**
- Verify `TEMPORAL_HOST=temporal:7233` in environment
- Check Temporal worker is started: `docker compose logs temporal`

**PostgreSQL migration failures**
```bash
docker compose down -v  # Clear volumes
make dev                # Start fresh
```

**LLM service not ready**
- Provider API keys are optional in dev mode
- Service may show ready=false without keys but still functions

**Port conflicts**
Ensure these ports are free:
- 50051 (Agent-Core gRPC)
- 50052 (Orchestrator gRPC)
- 8000 (LLM Service HTTP)
- 8088 (Temporal UI)
- 5432 (PostgreSQL)
- 6379 (Redis)

### Debugging Commands

```bash
# View all logs
make logs

# View specific service logs
docker compose logs -f orchestrator
docker compose logs -f agent-core
docker compose logs -f llm-service

# Check database state
docker compose exec postgres psql -U shannon -d shannon \
  -c "SELECT workflow_id, status FROM task_executions ORDER BY created_at DESC LIMIT 5;"

# Check Redis sessions
docker compose exec redis redis-cli KEYS "session:*"

# List Temporal workflows
docker compose exec temporal temporal workflow list --address temporal:7233
```

## Performance Testing

```bash
# Load test with concurrent requests
for i in {1..10}; do
  ./scripts/submit_task.sh "Test query $i" &
done
wait

# Monitor metrics during load
watch -n 1 'curl -s http://localhost:2112/metrics | grep shannon_'
```

## Cleanup

```bash
# Stop services
make down

# Clean all data (including volumes)
make clean
```