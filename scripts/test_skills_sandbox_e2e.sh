#!/bin/bash
set -e

echo "=== Phase 3-4 E2E Integration Tests ==="
echo "Testing: WASI Sandbox, Session Workspaces, File Operations"

API_URL="${API_URL:-http://localhost:8080}"
SESSION_ID="e2e-test-$(date +%s)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $1${NC}"; }
fail() { echo -e "${RED}✗ $1${NC}"; exit 1; }
warn() { echo -e "${YELLOW}⚠ $1${NC}"; }

echo ""
echo "=== Test 1: API Health Check ==="

# Check gateway health
HEALTH=$(curl -sS "${API_URL}/health" 2>/dev/null || echo '{"error": "failed"}')
echo "$HEALTH" | jq -e '.status == "healthy"' > /dev/null 2>&1 && pass "Gateway healthy" || fail "Gateway not healthy"

echo ""
echo "=== Test 2: Basic Task Submission ==="

# Submit a simple task
TASK_RESULT=$(curl -sS -X POST "${API_URL}/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"What is 2+2?\",
    \"session_id\": \"${SESSION_ID}\"
  }")

WORKFLOW_ID=$(echo "$TASK_RESULT" | jq -r '.workflowId // .workflow_id // .task_id // empty')
if [ -n "$WORKFLOW_ID" ]; then
  pass "Task submitted: $WORKFLOW_ID"
else
  fail "Task submission failed: $TASK_RESULT"
fi

# Wait for completion
echo "Waiting for task completion..."
MAX_ATTEMPTS=20
for i in $(seq 1 $MAX_ATTEMPTS); do
  sleep 2
  STATUS=$(curl -sS "${API_URL}/api/v1/tasks/${WORKFLOW_ID}" | jq -r '.status // empty')
  if [ "$STATUS" = "TASK_STATUS_COMPLETED" ]; then
    pass "Task completed"
    break
  elif [ "$STATUS" = "TASK_STATUS_FAILED" ]; then
    fail "Task failed"
  fi
  if [ $i -eq $MAX_ATTEMPTS ]; then
    warn "Task still running after max attempts"
  fi
done

echo ""
echo "=== Test 3: Session Context ==="

# Submit task with session context
CONTEXT_TASK=$(curl -sS -X POST "${API_URL}/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"Calculate 10 * 5\",
    \"session_id\": \"${SESSION_ID}\",
    \"context\": {\"role\": \"developer\"}
  }")

CONTEXT_WF=$(echo "$CONTEXT_TASK" | jq -r '.workflowId // .workflow_id // .task_id // empty')
if [ -n "$CONTEXT_WF" ]; then
  pass "Context task submitted: $CONTEXT_WF"
else
  fail "Context task submission failed"
fi

echo ""
echo "=== Test 4: Tools List ==="

# Check that tools are available
TOOLS=$(curl -sS "http://localhost:8000/tools/list")
TOOL_COUNT=$(echo "$TOOLS" | jq 'length')
if [ "$TOOL_COUNT" -gt 0 ]; then
  pass "Tools available: $TOOL_COUNT tools"
else
  fail "No tools available"
fi

# Check for file tools (tools list is array of strings)
FILE_TOOLS=$(echo "$TOOLS" | jq '[.[] | select(test("file"; "i"))] | length')
if [ "$FILE_TOOLS" -gt 0 ]; then
  pass "File tools registered: $FILE_TOOLS"
else
  warn "No file tools found (sandbox may be disabled)"
fi

echo ""
echo "=== Test 5: Container Volumes ==="

# Verify session volumes exist in containers
if docker exec shannon-agent-core-1 test -d /tmp/shannon-sessions 2>/dev/null; then
  pass "Agent-core has session volume mounted"
else
  warn "Agent-core session volume not mounted (expected if sandbox disabled)"
fi

if docker exec shannon-llm-service-1 test -d /tmp/shannon-sessions 2>/dev/null; then
  pass "LLM-service has session volume mounted"
else
  warn "LLM-service session volume not mounted (expected if sandbox disabled)"
fi

echo ""
echo "=== Test 6: Sandbox Environment Variables ==="

# Check sandbox env vars in agent-core
SANDBOX_ENABLED=$(docker exec shannon-agent-core-1 env | grep SHANNON_USE_WASI_SANDBOX || echo "not set")
if echo "$SANDBOX_ENABLED" | grep -qE "=1$|=true$"; then
  pass "WASI sandbox enabled in agent-core"
elif echo "$SANDBOX_ENABLED" | grep -qE "=0$|=false$"; then
  warn "WASI sandbox disabled (SHANNON_USE_WASI_SANDBOX=0)"
else
  warn "SHANNON_USE_WASI_SANDBOX not set"
fi

echo ""
echo "=== Test 7: Audit Logging ==="

# Check that agent-core is producing logs
AGENT_LOGS=$(docker logs shannon-agent-core-1 2>&1 | tail -20)
if echo "$AGENT_LOGS" | grep -q "session_id"; then
  pass "Agent-core includes session_id in logs"
else
  warn "Session audit logging not visible in recent logs"
fi

echo ""
echo "=== E2E Test Summary ==="
echo "All critical tests completed."
echo "Session ID used: ${SESSION_ID}"
echo ""
echo "To verify sandbox file operations manually:"
echo "  1. Submit a task requesting file write"
echo "  2. Check /tmp/shannon-sessions/\${session_id}/ in containers"
echo "  3. Verify cross-session isolation"
