#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "Temporal UI reachable"
curl -fsS http://localhost:8088 > /dev/null || fail "Temporal UI not reachable"
pass "Temporal UI"

info "Agent-Core gRPC health"
grpcurl -plaintext \
  -import-path protos \
  -proto common/common.proto \
  -proto agent/agent.proto \
  localhost:50051 shannon.agent.AgentService/HealthCheck \
  | grep -q '"healthy"[[:space:]]*:[[:space:]]*true' || fail "Agent-Core health false or unreachable"
pass "Agent-Core health"

info "Agent-Core ExecuteTask"
grpcurl -plaintext \
  -import-path protos \
  -proto common/common.proto \
  -proto agent/agent.proto \
  -d '{"query":"hello from smoke","mode":1}' \
  localhost:50051 shannon.agent.AgentService/ExecuteTask > /dev/null || fail "Agent-Core ExecuteTask failed"
pass "Agent-Core ExecuteTask"

info "Wait for orchestrator gRPC (50052) to be ready"
for i in $(seq 1 60); do
  if nc -z localhost 50052 2>/dev/null; then
    pass "Orchestrator gRPC ready"
    break
  fi
  sleep 1
  info "...waiting ($i)"
  if [ "$i" -eq 60 ]; then fail "Orchestrator gRPC not ready on :50052"; fi
done

info "Orchestrator SubmitTask via reflection"
if grpcurl -plaintext -d '{"metadata":{"user_id":"dev","session_id":"s1"},"query":"Say hello","context":{}}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/submit_resp.json 2>/dev/null; then
  pass "Orchestrator SubmitTask (reflection)"
else
  info "Reflection failed; retrying with local protos"
  grpcurl -plaintext \
    -import-path protos \
    -proto common/common.proto -proto orchestrator/orchestrator.proto \
    -d '{"metadata":{"user_id":"dev","session_id":"s1"},"query":"Say hello","context":{}}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/submit_resp.json || fail "SubmitTask failed"
  pass "Orchestrator SubmitTask (proto)"
fi

TASK_ID=$(sed -n 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_resp.json | head -n1)
WORKFLOW_ID=$(sed -n 's/.*"workflowId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_resp.json | head -n1)
[ -n "$TASK_ID" ] || fail "No task_id in SubmitTask response"
info "SubmitTask -> task_id=$TASK_ID workflow_id=$WORKFLOW_ID"

info "Poll GetTaskStatus until terminal (30s max)"
TERMINAL=false
for i in $(seq 1 30); do
  if grpcurl -plaintext -d '{"taskId":"'"$TASK_ID"'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status.json 2>/dev/null || \
     grpcurl -plaintext -import-path protos -proto orchestrator/orchestrator.proto -proto common/common.proto \
       -d '{"taskId":"'"$TASK_ID"'"}' localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status.json; then
    STATUS=$(cat /tmp/status.json | grep -o '"status"\s*:\s*"[A-Z_]*"' | sed -E 's/.*:\s*"(.*)"/\1/')
    info "Status=$STATUS (attempt $i)"
    if echo "$STATUS" | grep -Eq "COMPLETED|FAILED|CANCELLED|TIMEOUT"; then TERMINAL=true; break; fi
  fi
  sleep 1
done

if [ "$TERMINAL" != true ]; then
  fail "workflow did not reach terminal state within timeout"
fi
pass "Orchestrator reached terminal status"

info "Verify tasks row persisted"
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT status FROM task_executions WHERE workflow_id='${WORKFLOW_ID}' ORDER BY created_at DESC LIMIT 1;" \
  | grep -Eiq "completed|failed|cancelled|timeout" || fail "tasks row not found or status not terminal"
pass "Orchestrator persistence verified"

info "Metrics endpoints respond"
curl -fsS http://localhost:2112/metrics > /dev/null || fail "Orchestrator metrics not reachable"
curl -fsS http://localhost:2113/metrics > /dev/null || fail "Agent-Core metrics not reachable"
pass "Metrics endpoints reachable"

info "LLM service health"
curl -fsS http://localhost:8000/health/ > /dev/null || fail "LLM /health failed"
curl -fsS http://localhost:8000/health/live > /dev/null || fail "LLM /health/live failed"
# Readiness may be false without providers; do not fail, just report
if curl -fsS http://localhost:8000/health/ready | grep -qi '"ready"[[:space:]]*:[[:space:]]*true'; then
  pass "LLM ready"
else
  info "LLM ready=false (expected in dev without providers)"
fi
pass "LLM health endpoints"

info "MCP: register a mock echo tool"
curl -fsS -X POST http://localhost:8000/tools/mcp/register \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "mock_echo",
    "url": "http://localhost:8000/mcp/mock",
    "func_name": "echo",
    "description": "MCP echo mock",
    "parameters": [
      {"name": "text", "type": "string", "required": true}
    ]
  }' | grep -qi '"success"\s*:\s*true' || fail "MCP register failed"
pass "MCP tool registered"

info "MCP: execute mock echo tool"
MCP_OUT=$(curl -fsS -X POST http://localhost:8000/tools/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "tool_name": "mock_echo",
    "parameters": {"text": "hello-mcp"}
  }') || fail "MCP tool execute failed"
echo "$MCP_OUT" | grep -qi '"success"\s*:\s*true' || fail "MCP execute did not return success"
echo "$MCP_OUT" | grep -qi 'hello-mcp' || fail "MCP execute output missing echo text"
pass "MCP tool executed"

info "Qdrant /readyz"
curl -fsS http://localhost:6333/readyz > /dev/null || fail "Qdrant not ready"
info "Qdrant collections include tool_results and cases"
collections_json=$(curl -fsS http://localhost:6333/collections)
echo "$collections_json" | grep -q 'tool_results' || fail "Qdrant missing tool_results collection"
echo "$collections_json" | grep -q 'cases' || fail "Qdrant missing cases collection"
pass "Qdrant collections present"

info "Postgres connectivity"
docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U shannon -d shannon -c 'select 1' > /dev/null || fail "Postgres query failed"
pass "Postgres query"

echo "All smoke checks passed."
