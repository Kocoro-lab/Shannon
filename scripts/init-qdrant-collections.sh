#!/bin/bash
# Initialize Qdrant collections for Shannon
# Run this after Qdrant is deployed but before orchestrator starts
#
# Usage:
#   ./scripts/init-qdrant-collections.sh [qdrant-host] [port]
#
# Default: localhost:6333 (use with port-forward)
# EKS example: kubectl -n shannon port-forward svc/qdrant 6333:6333

set -e

QDRANT_HOST="${1:-localhost}"
QDRANT_PORT="${2:-6333}"
QDRANT_URL="http://${QDRANT_HOST}:${QDRANT_PORT}"

# Vector dimension for OpenAI text-embedding-3-small
VECTOR_DIM=1536

echo "Initializing Qdrant collections at ${QDRANT_URL}..."

# Collections required by Shannon
COLLECTIONS=(
    "task_embeddings"
    "decomposition_patterns"
)

for collection in "${COLLECTIONS[@]}"; do
    echo -n "  Creating ${collection}... "
    
    # Check if collection exists
    EXISTS=$(curl -s "${QDRANT_URL}/collections/${collection}" | jq -r '.status // "not_found"')
    
    if [ "$EXISTS" = "ok" ]; then
        echo "already exists, skipping"
        continue
    fi
    
    # Create collection
    RESULT=$(curl -s -X PUT "${QDRANT_URL}/collections/${collection}" \
        -H "Content-Type: application/json" \
        -d "{
            \"vectors\": {
                \"size\": ${VECTOR_DIM},
                \"distance\": \"Cosine\"
            }
        }" | jq -r '.status // "error"')
    
    if [ "$RESULT" = "ok" ]; then
        echo "created"
    else
        echo "FAILED"
        exit 1
    fi
done

echo ""
echo "Qdrant collections initialized successfully!"
echo ""
echo "Collections:"
curl -s "${QDRANT_URL}/collections" | jq -r '.result.collections[].name' | sed 's/^/  - /'
