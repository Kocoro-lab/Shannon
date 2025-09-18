#!/usr/bin/env bash
set -euo pipefail

QUERY=${1:-"Say hello"}
USER_ID=${USER_ID:-dev}
SESSION_ID=${2:-${SESSION_ID:-calculator-test-$(date +%s)}}

# Submit task
grpcurl -plaintext -d '{
  "metadata": {"userId":"'"$USER_ID"'","sessionId":"'"$SESSION_ID"'"},
  "query": "'"$QUERY"'",
  "context": {}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/submit_calc.json 2>/dev/null

TASK_ID=$(sed -n 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_calc.json | head -n1)
WORKFLOW_ID=$(sed -n 's/.*"workflowId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_calc.json | head -n1)

# Poll for completion
for i in {1..30}; do
  grpcurl -plaintext -d '{"taskId":"'"$TASK_ID"'"}' localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/status_calc.json 2>/dev/null
  STATUS=$(sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([A-Z_]*\)".*/\1/p' /tmp/status_calc.json | head -n1)
  if [[ "$STATUS" =~ COMPLETED|FAILED|CANCELLED|TIMEOUT ]]; then break; fi
  sleep 1
done

# Get the actual response from the database
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  psql -U shannon -d shannon -At -c "SELECT response FROM tasks WHERE workflow_id='${WORKFLOW_ID}' LIMIT 1;" 2>/dev/null