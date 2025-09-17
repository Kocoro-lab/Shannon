#!/usr/bin/env bash
set -euo pipefail

# Export a workflow history from Temporal and replay it against current code.
# Usage: scripts/replay_workflow.sh <workflow_id> [run_id]

if [ $# -lt 1 ]; then
  echo "Usage: $0 <workflow_id> [run_id]" >&2
  exit 2
fi

WORKFLOW_ID="$1"
RUN_ID="${2:-}"
OUT_FILE="/tmp/${WORKFLOW_ID}_history.json"

echo "[replay] Exporting history for workflow_id=${WORKFLOW_ID} run_id=${RUN_ID:-latest}" >&2

# Use the new temporal CLI which outputs proper JSON
if [ -n "$RUN_ID" ]; then
  docker compose -f deploy/compose/docker-compose.yml exec -T temporal \
    temporal workflow show --workflow-id "$WORKFLOW_ID" --run-id "$RUN_ID" \
    --namespace default --address temporal:7233 --output json > "$OUT_FILE"
else
  docker compose -f deploy/compose/docker-compose.yml exec -T temporal \
    temporal workflow show --workflow-id "$WORKFLOW_ID" \
    --namespace default --address temporal:7233 --output json > "$OUT_FILE"
fi

echo "[replay] Replaying history from $OUT_FILE" >&2
(cd go/orchestrator && GO111MODULE=on go run ./tools/replay -history "$OUT_FILE")

echo "[replay] Success" >&2
