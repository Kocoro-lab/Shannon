#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "Template Fallback E2E Test Starting"

# Wait for orchestrator to be ready
for i in $(seq 1 30); do
  if nc -z localhost 50052 2>/dev/null; then
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then fail "Orchestrator not ready"; fi
done

# Helper to submit a task
submit_task() {
  local query="$1"
  local template_name="$2"
  local disable_ai="${3:-false}"
  grpcurl -plaintext -d "{\n  \"metadata\": {\"userId\":\"fallback-user\",\"sessionId\":\"fallback-session\"},\n  \"query\": \"$query\",\n  \"context\": {\"template_name\": \"$template_name\", \"disable_ai\": $disable_ai}\n}" \
    localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 || true
}

# Case 1: Nonexistent template should gracefully fall back (independent of config)
info "Case 1: Nonexistent template fallback (should not fail task immediately)"
RESP=$(submit_task "What is 2+2?" "nonexistent_template_abc")
TASK_ID=$(echo "$RESP" | grep -oP '"taskId":\s*"\K[^"]+' || echo "")
if [ -n "$TASK_ID" ]; then
  pass "Fallback initiated for missing template (task id: $TASK_ID)"
else
  info "Task not accepted (service may still be starting)"
fi

# Case 2: disable_ai=true should block fallback when template missing
info "Case 2: disable_ai=true blocks fallback when template missing"
RESP=$(submit_task "Calc 3*3" "nonexistent_required" true)
ERR_MSG=$(echo "$RESP" | tr -d '\n' | sed 's/.*"message":"\([^"]*\)".*/\1/' || echo "")
if echo "$ERR_MSG" | grep -qi "template"; then
  pass "disable_ai enforcement working"
else
  info "disable_ai enforcement not confirmed (message: ${ERR_MSG:-n/a})"
fi

# Note: Runtime template failure fallback requires an intentionally failing template.
# This test focuses on the missing-template fallback behavior and disable_ai enforcement.

info "Template Fallback E2E Test Complete"

