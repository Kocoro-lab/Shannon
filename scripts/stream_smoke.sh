#!/usr/bin/env bash
set -euo pipefail

# Simple smoke test for Shannon streaming endpoints (SSE + gRPC).
# Usage:
#   WF_ID=your-workflow-id ./scripts/stream_smoke.sh
# Optional env:
#   ADMIN=http://localhost:8081   # admin HTTP (health/approvals/stream)
#   GRPC=localhost:50052          # orchestrator gRPC endpoint

ADMIN=${ADMIN:-http://localhost:8081}
GRPC=${GRPC:-localhost:50052}

if [[ -z "${WF_ID:-}" ]]; then
  echo "WF_ID is required (export WF_ID=<workflow_id>)" >&2
  exit 1
fi

echo "== SSE stream (5s) =="
(
  curl -NsS "${ADMIN}/stream/sse?workflow_id=${WF_ID}&types=WORKFLOW_STARTED,AGENT_STARTED,AGENT_COMPLETED,ERROR_OCCURRED" &
  cpid=$!
  sleep 5
  kill ${cpid} >/dev/null 2>&1 || true
) || true

echo
echo "== gRPC stream (grpcurl) =="
if command -v grpcurl >/dev/null 2>&1; then
  grpcurl -plaintext -d '{"workflow_id":"'"${WF_ID}"'","types":["AGENT_STARTED","AGENT_COMPLETED"],"last_event_id":"0"}' \
    "${GRPC}" shannon.orchestrator.StreamingService/StreamTaskExecution || true
else
  echo "grpcurl not found; skipping gRPC smoke"
fi

echo
echo "Done. Tip: Use Last-Event-ID or last_event_id to resume streams."

