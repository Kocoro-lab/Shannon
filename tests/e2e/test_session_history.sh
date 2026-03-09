#!/bin/bash
# E2E test: Multi-turn session history preservation
# Verifies that conversation context is maintained across turns
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
SESSION_ID="test-history-$(date +%s)"
PASS=0
FAIL=0

log_pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
log_fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "=== Session History E2E Test ==="
echo "API: $API_URL"
echo "Session: $SESSION_ID"
echo ""

# Turn 1: Establish context
echo "--- Turn 1: Establish context ---"
RESP1=$(curl -sS -X POST "$API_URL/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"The capital of France is Paris. Remember this fact.\",
    \"session_id\": \"$SESSION_ID\",
    \"mode\": \"simple\",
    \"context\": {
      \"skip_synthesis\": true
    }
  }")

TASK1=$(echo "$RESP1" | jq -r '.task_id // empty')
if [ -z "$TASK1" ]; then
  log_fail "Turn 1 task submission (no task_id)"
  echo "Response: $RESP1"
  exit 1
fi
log_pass "Turn 1 submitted: $TASK1"

# Poll turn 1
for i in $(seq 1 30); do
  sleep 2
  STATUS=$(curl -sS "$API_URL/api/v1/tasks/$TASK1" | jq -r '.status // empty')
  if [[ "$STATUS" == *"COMPLETED"* ]]; then
    break
  fi
  if [[ "$STATUS" == *"FAILED"* ]]; then
    log_fail "Turn 1 failed"
    exit 1
  fi
done

RESULT1=$(curl -sS "$API_URL/api/v1/tasks/$TASK1" | jq -r '.result // empty')
if [ -n "$RESULT1" ]; then
  log_pass "Turn 1 completed"
  echo "  Result preview: ${RESULT1:0:100}..."
else
  log_fail "Turn 1 no result"
fi

echo ""

# Turn 2: Reference prior context
echo "--- Turn 2: Reference prior context ---"
RESP2=$(curl -sS -X POST "$API_URL/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"What capital city did I just mention?\",
    \"session_id\": \"$SESSION_ID\",
    \"mode\": \"simple\",
    \"context\": {
      \"skip_synthesis\": true
    }
  }")

TASK2=$(echo "$RESP2" | jq -r '.task_id // empty')
if [ -z "$TASK2" ]; then
  log_fail "Turn 2 task submission"
  exit 1
fi
log_pass "Turn 2 submitted: $TASK2"

# Poll turn 2
for i in $(seq 1 30); do
  sleep 2
  STATUS=$(curl -sS "$API_URL/api/v1/tasks/$TASK2" | jq -r '.status // empty')
  if [[ "$STATUS" == *"COMPLETED"* ]]; then
    break
  fi
  if [[ "$STATUS" == *"FAILED"* ]]; then
    log_fail "Turn 2 failed"
    exit 1
  fi
done

RESULT2=$(curl -sS "$API_URL/api/v1/tasks/$TASK2" | jq -r '.result // empty')
echo "  Result: ${RESULT2:0:200}"

# Check if Paris is mentioned in the response
if echo "$RESULT2" | grep -qi "paris"; then
  log_pass "Turn 2 has context from Turn 1 (mentions Paris)"
else
  log_fail "Turn 2 lost context (does not mention Paris)"
fi

echo ""

# Turn 3: Multi-line content preservation test
echo "--- Turn 3: Verify multi-line history not truncated ---"
RESP3=$(curl -sS -X POST "$API_URL/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"Now tell me: what was my very first message in this conversation? Quote it.\",
    \"session_id\": \"$SESSION_ID\",
    \"mode\": \"simple\",
    \"context\": {
      \"skip_synthesis\": true
    }
  }")

TASK3=$(echo "$RESP3" | jq -r '.task_id // empty')
if [ -z "$TASK3" ]; then
  log_fail "Turn 3 task submission"
  exit 1
fi
log_pass "Turn 3 submitted: $TASK3"

# Poll turn 3
for i in $(seq 1 30); do
  sleep 2
  STATUS=$(curl -sS "$API_URL/api/v1/tasks/$TASK3" | jq -r '.status // empty')
  if [[ "$STATUS" == *"COMPLETED"* ]]; then
    break
  fi
  if [[ "$STATUS" == *"FAILED"* ]]; then
    log_fail "Turn 3 failed"
    exit 1
  fi
done

RESULT3=$(curl -sS "$API_URL/api/v1/tasks/$TASK3" | jq -r '.result // empty')
echo "  Result: ${RESULT3:0:200}"

# Check if the original message is referenced
if echo "$RESULT3" | grep -qi "capital\|france\|paris"; then
  log_pass "Turn 3 has full conversation context"
else
  log_fail "Turn 3 lost conversation context"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="

# Check llm-service logs for history_rehydrated
echo ""
echo "--- Checking llm-service logs for history rehydration ---"
docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=50 2>/dev/null | grep "history_rehydrated\|Parsed history" | tail -10 || echo "(no logs found — check manually)"

exit $FAIL
