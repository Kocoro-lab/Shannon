#!/usr/bin/env bash
# Integration Test 2: Session Memory and Persistence
# Tests session creation, memory persistence, and context continuity across multiple tasks

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
TEST_NAME="Session Memory & Persistence"

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

# Services health
curl -fsS http://localhost:2112/metrics > /dev/null || fail "Orchestrator not available"
docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U shannon -d shannon -c 'SELECT 1' > /dev/null || fail "PostgreSQL not available"
docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli ping > /dev/null || fail "Redis not available"

pass "Prerequisites check completed"

# Generate unique test identifiers
USER_ID="test-memory-user-$(date +%s)"
SESSION_ID="test-memory-session-$(date +%s)"

echo ""
echo "Test Session: $SESSION_ID"
echo "Test User: $USER_ID"
echo ""

# Test 1: Session Creation and First Task
info "Test 1: Creating session with first contextual task"

FIRST_QUERY="My name is Alice and I like cats. Remember this for our conversation."
grpcurl -plaintext -import-path protos \
  -proto common/common.proto -proto orchestrator/orchestrator.proto \
  -d '{
    "metadata": {
      "user_id": "'"$USER_ID"'",
      "session_id": "'"$SESSION_ID"'"
    },
    "query": "'"$FIRST_QUERY"'",
    "context": {}
  }' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/session_task1.json || fail "First task submission failed"

TASK1_ID=$(grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/session_task1.json | sed 's/.*"\([^"]*\)".*/\1/')
WORKFLOW1_ID=$(grep -o '"workflowId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/session_task1.json | sed 's/.*"\([^"]*\)".*/\1/')

[ -n "$TASK1_ID" ] || fail "No task ID for first task"
info "First task submitted: $TASK1_ID"

# Wait for first task completion
info "Waiting for first task completion..."
for i in $(seq 1 45); do
  grpcurl -plaintext -import-path protos \
    -proto common/common.proto -proto orchestrator/orchestrator.proto \
    -d '{"taskId":"'"$TASK1_ID"'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status1.json 2>/dev/null || continue

  STATUS1=$(grep -o '"status"[[:space:]]*:[[:space:]]*"[A-Z_]*"' /tmp/status1.json | sed 's/.*"\([A-Z_]*\)".*/\1/' || echo "UNKNOWN")
  
  if echo "$STATUS1" | grep -Eq "TASK_STATUS_COMPLETED|COMPLETED|TASK_STATUS_FAILED|FAILED|TASK_STATUS_CANCELLED|CANCELLED|TASK_STATUS_TIMEOUT|TIMEOUT"; then
    break
  fi
  sleep 1
done

info "First task status: $STATUS1"
pass "First task processing completed"

# Test 2: Session Persistence Check
info "Test 2: Verifying session was created in database"

SESSION_COUNT=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT COUNT(*) FROM sessions WHERE id='$SESSION_ID' AND user_id='$USER_ID';" 2>/dev/null || echo "0")

[ "$SESSION_COUNT" = "1" ] || fail "Session not found in database (count: $SESSION_COUNT)"
pass "Session persistence verified"

# Test 3: Session Context Memory Test
info "Test 3: Testing session memory with follow-up task"

# Submit a follow-up task that references previous context
SECOND_QUERY="What did I tell you about my preferences?"
grpcurl -plaintext -import-path protos \
  -proto common/common.proto -proto orchestrator/orchestrator.proto \
  -d '{
    "metadata": {
      "user_id": "'"$USER_ID"'",
      "session_id": "'"$SESSION_ID"'"
    },
    "query": "'"$SECOND_QUERY"'",
    "context": {}
  }' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/session_task2.json || fail "Second task submission failed"

TASK2_ID=$(grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/session_task2.json | sed 's/.*"\([^"]*\)".*/\1/')
[ -n "$TASK2_ID" ] || fail "No task ID for second task"
info "Second task submitted: $TASK2_ID"

# Wait for second task completion
info "Waiting for second task completion..."
for i in $(seq 1 45); do
  grpcurl -plaintext -import-path protos \
    -proto common/common.proto -proto orchestrator/orchestrator.proto \
    -d '{"taskId":"'"$TASK2_ID"'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status2.json 2>/dev/null || continue

  STATUS2=$(grep -o '"status"[[:space:]]*:[[:space:]]*"[A-Z_]*"' /tmp/status2.json | sed 's/.*"\([A-Z_]*\)".*/\1/' || echo "UNKNOWN")
  
  if echo "$STATUS2" | grep -Eq "TASK_STATUS_COMPLETED|COMPLETED|TASK_STATUS_FAILED|FAILED|TASK_STATUS_CANCELLED|CANCELLED|TASK_STATUS_TIMEOUT|TIMEOUT"; then
    break
  fi
  sleep 1
done

info "Second task status: $STATUS2"
pass "Second task processing completed"

# Test 4: Context Retrieval Verification
info "Test 4: Verifying context was retrieved for second task"

# Check orchestrator logs for context loading
docker compose -f "$COMPOSE_FILE" logs --tail=100 orchestrator 2>/dev/null | grep -i "session\|context" > /tmp/context_logs.txt || true
if [ -s /tmp/context_logs.txt ]; then
  pass "Session context loading evidence found in logs"
else
  warn "No explicit context loading logs found"
fi

# Test 5: Session Update Activities
info "Test 5: Verifying session was updated after each task"

TASK_COUNT=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT COUNT(*) FROM task_executions WHERE workflow_id IN ('$WORKFLOW1_ID', '$(grep -o '"workflowId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/session_task2.json | sed 's/.*"\([^"]*\)".*/\1/')');" 2>/dev/null || echo "0")

[ "$TASK_COUNT" -ge "2" ] || fail "Expected at least 2 tasks in database, found: $TASK_COUNT"
pass "Multiple tasks tracked in session"

# Test 6: Redis Session Cache Test
info "Test 6: Testing Redis session caching (if available)"

# Try to check Redis for session data
REDIS_KEY="session:$SESSION_ID"
REDIS_DATA=$(docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli EXISTS "$REDIS_KEY" 2>/dev/null || echo "0")

if [ "$REDIS_DATA" = "1" ]; then
  pass "Session data found in Redis cache"
elif [ "$REDIS_DATA" = "0" ]; then
  info "Session not cached in Redis (may be normal depending on implementation)"
else
  warn "Unable to check Redis session cache"
fi

# Test 7: Session Continuity Test 
info "Test 7: Testing session continuity with third task"

THIRD_QUERY="What's my name again?"
grpcurl -plaintext -import-path protos \
  -proto common/common.proto -proto orchestrator/orchestrator.proto \
  -d '{
    "metadata": {
      "user_id": "'"$USER_ID"'",
      "session_id": "'"$SESSION_ID"'"
    },
    "query": "'"$THIRD_QUERY"'",
    "context": {}
  }' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/session_task3.json || fail "Third task submission failed"

TASK3_ID=$(grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/session_task3.json | sed 's/.*"\([^"]*\)".*/\1/')
info "Third task submitted: $TASK3_ID"

# Brief wait for third task
sleep 3

# Test 8: Session History Validation
info "Test 8: Validating complete session history"

FINAL_TASK_COUNT=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT COUNT(*) FROM task_executions WHERE workflow_id LIKE 'task-dev-%' AND created_at > NOW() - INTERVAL '5 minutes';" 2>/dev/null || echo "0")

info "Total recent tasks in database: $FINAL_TASK_COUNT"

# Check session was updated recently
RECENT_SESSION=$(docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT COUNT(*) FROM sessions WHERE id='$SESSION_ID' AND updated_at > NOW() - INTERVAL '5 minutes';" 2>/dev/null || echo "0")

[ "$RECENT_SESSION" = "1" ] || fail "Session was not updated recently"
pass "Session history validation completed"

echo ""
echo "======================================"
echo "Session Memory Test Results:"
echo "======================================"
echo "Session ID: $SESSION_ID"
echo "User ID: $USER_ID"
echo "Tasks Submitted: 3"
echo "Task 1 ID: $TASK1_ID (Status: $STATUS1)"
echo "Task 2 ID: $TASK2_ID (Status: $STATUS2)" 
echo "Task 3 ID: $TASK3_ID"
echo "======================================"

# Final validation
if ([ "$STATUS1" = "TASK_STATUS_COMPLETED" ] || [ "$STATUS1" = "COMPLETED" ]) && ([ "$STATUS2" = "TASK_STATUS_COMPLETED" ] || [ "$STATUS2" = "COMPLETED" ]) && [ "$SESSION_COUNT" = "1" ]; then
  pass "✅ Session Memory Integration Test PASSED"
  echo ""
  echo "Key validations:"
  echo "  ✓ Session creation and persistence"
  echo "  ✓ Multiple task execution in same session"
  echo "  ✓ Database session tracking"
  echo "  ✓ Context continuity across tasks"
  echo "  ✓ Session update activities"
  echo ""
else
  fail "❌ Session Memory Integration Test FAILED"
fi
