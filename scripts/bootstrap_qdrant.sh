#!/usr/bin/env bash
set -euo pipefail

QDRANT_URL="http://localhost:6333"

echo "Bootstrapping Qdrant collections (if needed)..."

create_collection() {
  local name="$1"
  curl -fsS -X PUT "$QDRANT_URL/collections/$name" \
    -H 'Content-Type: application/json' \
    -d '{
      "vectors": {"size": 1536, "distance": "Cosine"},
      "on_disk_payload": true
    }' >/dev/null && echo " - ensured collection: $name"
}

create_collection "tool_results"
create_collection "cases"

echo "Qdrant bootstrap complete."

