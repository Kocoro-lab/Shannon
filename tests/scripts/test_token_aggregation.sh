#!/bin/bash
# Test token aggregation across the system

set -e

echo "Testing token aggregation..."

# Submit a task
echo "Submitting test task..."
TASK_ID=$(grpcurl -plaintext -d '{
  "query": "What is 2+2?",
  "metadata": {
    "user_id": "test-user",
    "session_id": "test-session"
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask | jq -r '.workflow_id')

echo "Task submitted: $TASK_ID"

# Wait for completion
echo "Waiting for task to complete..."
sleep 3

# Get status with metrics
echo "Getting task status with token metrics..."
grpcurl -plaintext -d "{\"task_id\": \"$TASK_ID\"}" \
  localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus | \
  jq '{
    task_id: .task_id,
    status: .status,
    tokens_used: .metrics.token_usage.total_tokens,
    cost_usd: .metrics.token_usage.cost_usd,
    agents_used: .metrics.agents_used
  }'

echo "Token aggregation test complete!"