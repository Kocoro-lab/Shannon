#!/usr/bin/env bash
set -euo pipefail

# Send dynamic team signals (recruit/retire) to a running workflow via Temporal CLI inside docker compose.
# Requires the Temporal container from deploy/compose/docker-compose.yml.
#
# Usage:
#   ./scripts/signal_team.sh recruit WF_ID "Description text" [role]
#   ./scripts/signal_team.sh retire  WF_ID AGENT_ID
#
# Examples:
#   ./scripts/signal_team.sh recruit my-workflow-123 "Fact-check section 2" research
#   ./scripts/signal_team.sh retire  my-workflow-123 agent-abc

ACTION=${1:-}
WF_ID=${2:-}

if [[ -z "$ACTION" || -z "$WF_ID" ]]; then
  echo "Usage:" >&2
  echo "  $0 recruit WF_ID \"Description\" [role]" >&2
  echo "  $0 retire  WF_ID AGENT_ID" >&2
  exit 1
fi

COMPOSE_FILE="deploy/compose/docker-compose.yml"
NAMESPACE=${NAMESPACE:-default}
ADDRESS=${ADDRESS:-temporal:7233}

case "$ACTION" in
  recruit)
    DESC=${3:-}
    ROLE=${4:-}
    if [[ -z "$DESC" ]]; then
      echo "Description is required for recruit" >&2
      exit 1
    fi
    PAYLOAD=$(jq -nc --arg d "$DESC" --arg r "$ROLE" '{Description:$d, Role:$r}')
    docker compose -f "$COMPOSE_FILE" exec -T temporal \
      temporal workflow signal \
        --workflow-id "$WF_ID" \
        --name recruit_v1 \
        --namespace "$NAMESPACE" \
        --address "$ADDRESS" \
        --input "$PAYLOAD"
    ;;
  retire)
    AGENT=${3:-}
    if [[ -z "$AGENT" ]]; then
      echo "AGENT_ID is required for retire" >&2
      exit 1
    fi
    PAYLOAD=$(jq -nc --arg a "$AGENT" '{AgentID:$a}')
    docker compose -f "$COMPOSE_FILE" exec -T temporal \
      temporal workflow signal \
        --workflow-id "$WF_ID" \
        --name retire_v1 \
        --namespace "$NAMESPACE" \
        --address "$ADDRESS" \
        --input "$PAYLOAD"
    ;;
  *)
    echo "Unknown action: $ACTION (use recruit|retire)" >&2
    exit 1
    ;;
esac

echo "Signal sent: $ACTION -> $WF_ID"

