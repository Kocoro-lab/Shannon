#!/usr/bin/env bash
set -euo pipefail

echo "Testing budget controls with sample requests..."

# Send multiple requests to test budget tracking
for i in {1..5}; do
  echo "Sending request $i..."
  grpcurl -plaintext \
    -d '{"metadata":{"user_id":"test-budget","session_id":"budget-test"},"query":"Test budget request '$i'","context":{}}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/budget_test_$i.json 2>/dev/null || true
  
  # Extract task ID
  TASK_ID=$(sed -n 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/budget_test_$i.json | head -n1)
  echo "  Task ID: $TASK_ID"
  
  # Small delay between requests
  sleep 0.5
done

echo ""
echo "Checking orchestrator logs for budget activity..."
docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 50 | grep -i "budget" | tail -10 || echo "No budget logs found"

echo ""
echo "Testing with high token usage to trigger backpressure..."
grpcurl -plaintext \
  -d '{"metadata":{"user_id":"heavy-user","session_id":"heavy-test"},"query":"Generate a very detailed 10-page report about artificial intelligence, machine learning, deep learning, neural networks, transformers, attention mechanisms, backpropagation, gradient descent, optimization algorithms, regularization techniques, data preprocessing, feature engineering, model evaluation, cross-validation, hyperparameter tuning, transfer learning, fine-tuning, reinforcement learning, Q-learning, policy gradients, actor-critic methods, generative models, VAEs, GANs, diffusion models, prompt engineering, and the future of AI research","context":{}}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/heavy_request.json 2>/dev/null || true

HEAVY_TASK=$(sed -n 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /tmp/heavy_request.json | head -n1)
echo "Heavy task submitted: $HEAVY_TASK"

# Wait and check status
sleep 2
if [ -n "$HEAVY_TASK" ]; then
  grpcurl -plaintext -d '{"taskId":"'$HEAVY_TASK'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null | grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' || true
fi

echo ""
echo "Budget control tests completed."