#!/usr/bin/env bash
set -euo pipefail

QUERY=${1:-"Say hello"}
# Accept user ID as second parameter, or from env, or default
USER_ID=${2:-${USER_ID:-dev}}
# Accept session ID as third parameter, or from env, or generate unique
SESSION_ID=${3:-${SESSION_ID:-"session-$(date +%s)"}}

echo "Submitting task: $QUERY"
echo "Using session: $SESSION_ID (user: $USER_ID)"

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

# Extract and display the final result
if [[ "$STATUS" == "TASK_STATUS_COMPLETED" ]]; then
  RESULT=$(jq -r '.result // "No result available"' /tmp/status_cli.json 2>/dev/null)
  TOKENS=$(jq -r '.metrics.tokenUsage.totalTokens // 0' /tmp/status_cli.json 2>/dev/null)
  COST=$(jq -r '.metrics.tokenUsage.costUsd // 0' /tmp/status_cli.json 2>/dev/null)
  echo ""
  echo "✅ Task completed successfully!"
  echo "Result: $RESULT"
  echo "Tokens used: $TOKENS (cost: \$$COST)"
elif [[ "$STATUS" == "TASK_STATUS_FAILED" ]]; then
  ERROR=$(jq -r '.errorMessage // .error // .result // "Unknown error"' /tmp/status_cli.json 2>/dev/null)
  echo ""
  echo "❌ Task failed!"
  echo "Error: $ERROR"
elif [[ "$STATUS" == "TASK_STATUS_CANCELLED" ]]; then
  echo ""
  echo "⚠️ Task was cancelled"
elif [[ "$STATUS" == "TASK_STATUS_TIMEOUT" ]]; then
  echo ""
  echo "⏱️ Task timed out"
else
  echo ""
  echo "Status: $STATUS"
fi
