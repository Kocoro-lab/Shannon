#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/compose.yml}"

echo "[e2e] Ensuring stack is up..."
docker compose -f "$COMPOSE_FILE" ps >/dev/null || docker compose -f "$COMPOSE_FILE" up -d

echo "[e2e] Running smoke checks..."
make smoke

echo "[e2e] Submitting a more complex task to exercise Standard/Complex modes..."
grpcurl -plaintext -d '{"metadata":{"user_id":"dev2","session_id":"s2"},"query":"Analyze, compare, and summarize three items with references","context":{}}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/submit_resp_complex.json

WF=$(sed -n 's/.*"workflowId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_resp_complex.json | head -n1)
echo "[e2e] WorkflowId=$WF"

echo "[e2e] Checking task_executions for complex workflow..."
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U shannon -d shannon -c "SELECT workflow_id,status,total_tokens,total_cost_usd FROM task_executions WHERE workflow_id='${WF}' ORDER BY created_at DESC LIMIT 1;"

echo "[e2e] Done."

