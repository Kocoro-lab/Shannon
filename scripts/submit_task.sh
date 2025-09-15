#!/usr/bin/env bash
set -euo pipefail

QUERY=${1:-"Say hello"}
USER_ID=${USER_ID:-dev}
# Accept session ID as second parameter, or from env, or default
SESSION_ID=${2:-${SESSION_ID:-s1}}

echo "Submitting task: $QUERY"

grpcurl -plaintext -d '{
  "metadata": {"userId":"'"$USER_ID"'","sessionId":"'"$SESSION_ID"'"},
  "query": "'"$QUERY"'",
  "context": {}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask | tee /tmp/submit_cli.json

echo
TASK_ID=$(sed -n 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/submit_cli.json | head -n1)
echo "Polling status for task: $TASK_ID"
for i in {1..10}; do
  grpcurl -plaintext -d '{"taskId":"'"$TASK_ID"'"}' localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus | tee /tmp/status_cli.json >/dev/null
  STATUS=$(sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([A-Z_]*\)".*/\1/p' /tmp/status_cli.json | head -n1)
  echo "  attempt $i: status=$STATUS"
  if [[ "$STATUS" =~ COMPLETED|FAILED|CANCELLED|TIMEOUT ]]; then break; fi
  sleep 1
done
echo "Done."

