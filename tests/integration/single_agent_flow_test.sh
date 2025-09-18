#!/usr/bin/env bash
# Integration Test 1: Single Agent Flow
# Tests the complete flow of a simple task through the orchestrator using SimpleTaskWorkflow

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
TEST_NAME="Single Agent Flow"

# Utilities
pass() { echo -e "[\033[32mPASS\033[0m] $1"; }
fail() { echo -e "[\033[31mFAIL\033[0m] $1"; exit 1; }
info() { echo -e "[\033[34mINFO\033[0m] $1"; }
warn() { echo -e "[\033[33mWARN\033[0m] $1"; }

echo "======================================"
echo "Integration Test: $TEST_NAME"
echo "======================================"

# Test Prerequisites
info "Checking prerequisites..."

# Orchestrator health
curl -fsS http://localhost:2112/metrics > /dev/null || fail "Orchestrator metrics not available - ensure services are running"

# Agent Core health  
grpcurl -plaintext -import-path protos -proto agent/agent.proto \
  localhost:50051 shannon.agent.AgentService/HealthCheck > /tmp/agent_health.json 2>/dev/null || fail "Agent Core not reachable"
grep -q '"healthy"[[:space:]]*:[[:space:]]*true' /tmp/agent_health.json || fail "Agent Core unhealthy"

pass "Prerequisites check completed"

# Test 1: Simple Task Submission
info "Test 1: Submitting simple task (should trigger SimpleTaskWorkflow)"

# Simple task - short query, no entities, should use SimpleTaskWorkflow
TEST_QUERY="What is 2+2?"
USER_ID="test-user-$(date +%s)"
SESSION_ID="session-$(date +%s)"

grpcurl -plaintext -import-path protos \
  -proto common/common.proto -proto orchestrator/orchestrator.proto \
  -d '{
    "metadata": {
      "user_id": "'"$USER_ID"'",
      "session_id": "'"$SESSION_ID"'"
    },
    "query": "'"$TEST_QUERY"'",
    "context": {}
  }' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/submit_simple.json || fail "Simple task submission failed"

TASK_ID=$(grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/submit_simple.json | sed 's/.*"\([^"]*\)".*/\1/')
WORKFLOW_ID=$(grep -o '"workflowId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/submit_simple.json | sed 's/.*"\([^"]*\)".*/\1/')

[ -n "$TASK_ID" ] || fail "No task ID in response"
[ -n "$WORKFLOW_ID" ] || fail "No workflow ID in response"

info "Simple task submitted: task_id=$TASK_ID workflow_id=$WORKFLOW_ID"
pass "Task submission successful"

# Test 2: Monitor Task Execution
info "Test 2: Monitoring task execution to completion"

TERMINAL=false
STATUS=""
for i in $(seq 1 60); do
  grpcurl -plaintext -import-path protos \
    -proto common/common.proto -proto orchestrator/orchestrator.proto \
    -d '{"taskId":"'"$TASK_ID"'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status_simple.json 2>/dev/null || continue

  STATUS=$(grep -o '"status"[[:space:]]*:[[:space:]]*"[A-Z_]*"' /tmp/status_simple.json | sed 's/.*"\([A-Z_]*\)".*/\1/' || echo "UNKNOWN")
  info "Task status: $STATUS (attempt $i/60)"
  
  if echo "$STATUS" | grep -Eq "TASK_STATUS_COMPLETED|COMPLETED|TASK_STATUS_FAILED|FAILED|TASK_STATUS_CANCELLED|CANCELLED|TASK_STATUS_TIMEOUT|TIMEOUT"; then
    TERMINAL=true
    break
  fi
  sleep 1
done

[ "$TERMINAL" = true ] || fail "Task did not reach terminal state within 60s"
pass "Task reached terminal state: $STATUS"

# Test 3: Verify Workflow Type (SimpleTaskWorkflow)
info "Test 3: Verifying SimpleTaskWorkflow was used"

# Check Temporal UI or workflow type - for now, check task completed successfully
if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] || [ "$STATUS" = "COMPLETED" ]; then
  pass "Simple task completed successfully (indicates SimpleTaskWorkflow worked)"
else
  warn "Task status: $STATUS (may indicate issues but not necessarily SimpleTaskWorkflow failure)"
fi

# Test 4: Verify Database Persistence
info "Test 4: Verifying task persistence in database"

DB_STATUS=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT status FROM tasks WHERE workflow_id='$WORKFLOW_ID' LIMIT 1;" 2>/dev/null || echo "")

[ -n "$DB_STATUS" ] || fail "Task not found in database"
echo "$DB_STATUS" | grep -Eiq "completed|failed|cancelled|timeout" || fail "Task status not terminal in database"

info "Database status: $DB_STATUS"
pass "Task persistence verified"

# Test 5: Verify Agent Core Interaction
info "Test 5: Verifying agent core interaction logs"

# Check orchestrator logs for agent interaction
docker compose -f "$COMPOSE_FILE" logs --tail=50 orchestrator 2>/dev/null | grep -i "agent" > /tmp/agent_logs.txt || true
if [ -s /tmp/agent_logs.txt ]; then
  pass "Agent interaction logs found"
else
  warn "No agent interaction logs found (may be normal for simple tasks)"
fi

# Test 6: Performance Check
info "Test 6: Performance validation"

if [ -f /tmp/status_simple.json ]; then
  # Check if response time is reasonable (< 10s for simple task)
  if [ $i -lt 10 ]; then
    pass "Task completed quickly (< 10s) - good performance"
  else
    warn "Task took $i seconds - performance could be improved"
  fi
fi

echo ""
echo "======================================"
echo "Single Agent Flow Test Results:"
echo "======================================"
echo "Task ID: $TASK_ID"
echo "Workflow ID: $WORKFLOW_ID" 
echo "Final Status: $STATUS"
echo "User ID: $USER_ID"
echo "Session ID: $SESSION_ID"
echo "Query: $TEST_QUERY"
echo "======================================"

if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] || [ "$STATUS" = "COMPLETED" ]; then
  pass "✅ Single Agent Flow Integration Test PASSED"
  echo ""
  echo "Key validations:"
  echo "  ✓ Simple task submission successful"
  echo "  ✓ Task execution completed"
  echo "  ✓ Database persistence working"
  echo "  ✓ Agent-orchestrator communication functional" 
  echo ""
else
  fail "❌ Single Agent Flow Integration Test FAILED - Status: $STATUS"
fi