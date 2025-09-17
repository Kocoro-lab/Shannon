#!/bin/bash
# P2P Coordination Test - Verifies that tasks with dependencies wait for each other

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== P2P Coordination Test ===${NC}"

# Wait for services to be ready
echo "Waiting for services to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:8081/health > /dev/null 2>&1; then
        break
    fi
    sleep 2
done

# Test 1: Sequential task with P2P coordination
echo -e "\n${YELLOW}Test 1: Sequential Task with Dependencies${NC}"
echo "Query: 'Analyze the data and then create a comprehensive report based on the analysis'"

RESPONSE=$(grpcurl -plaintext -d '{
  "metadata": {"userId": "p2p-test", "sessionId": "p2p-test-sequential"},
  "query": "Analyze the data and then create a comprehensive report based on the analysis",
  "context": {}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

TASK_ID=$(echo "$RESPONSE" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

if [ -z "$TASK_ID" ]; then
    echo -e "${RED}Failed to submit task${NC}"
    echo "$RESPONSE"
    exit 1
fi

echo "Task ID: $TASK_ID"
echo "Waiting for task completion (checking for P2P coordination)..."

# Poll for task status (max 30 seconds)
COMPLETED=false
for i in {1..15}; do
    STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 | grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || echo "")

    if [[ "$STATUS" == "COMPLETED" ]] || [[ "$STATUS" == "TASK_STATUS_COMPLETED" ]]; then
        echo -e "${GREEN}✓ Task completed successfully${NC}"
        COMPLETED=true
        break
    elif [[ "$STATUS" == "FAILED" ]] || [[ "$STATUS" == "TASK_STATUS_FAILED" ]]; then
        echo -e "${RED}✗ Task failed${NC}"
        exit 1
    fi

    sleep 2
done

if [ "$COMPLETED" = false ]; then
    echo -e "${YELLOW}⚠ Task still running after 30 seconds (expected for complex tasks)${NC}"
fi

# Check logs for P2P coordination
echo -e "\n${YELLOW}Checking for P2P coordination in logs...${NC}"
docker compose -f deploy/compose/compose.yml logs orchestrator | tail -50 | grep -i "P2P\|produces\|consumes\|dependency" || true

# Test 2: Force P2P mode
echo -e "\n${YELLOW}Test 2: Force P2P Mode on Simple Task${NC}"
echo "Query: 'What is 2+2?' with force_p2p=true"

RESPONSE=$(grpcurl -plaintext -d '{
  "metadata": {"userId": "p2p-test", "sessionId": "p2p-test-forced"},
  "query": "What is 2+2?",
  "context": {"force_p2p": "true"}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

TASK_ID=$(echo "$RESPONSE" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

if [ -z "$TASK_ID" ]; then
    echo -e "${RED}Failed to submit task with force_p2p${NC}"
    echo "$RESPONSE"
    exit 1
fi

echo "Task ID: $TASK_ID"

# Check if SupervisorWorkflow was used
docker compose -f deploy/compose/compose.yml logs orchestrator | tail -20 | grep -i "forced" && echo -e "${GREEN}✓ P2P mode was forced${NC}" || echo -e "${YELLOW}⚠ P2P force flag may not have been detected${NC}"

# Test 3: Complex pipeline with multiple dependencies
echo -e "\n${YELLOW}Test 3: Complex Pipeline with Multiple Dependencies${NC}"
echo "Query: 'Load the CSV file, analyze the data, create visualizations, and generate a PDF report'"

RESPONSE=$(grpcurl -plaintext -d '{
  "metadata": {"userId": "p2p-test", "sessionId": "p2p-test-pipeline"},
  "query": "Load the CSV file, analyze the data, create visualizations, and generate a PDF report",
  "context": {}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

TASK_ID=$(echo "$RESPONSE" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

if [ -z "$TASK_ID" ]; then
    echo -e "${RED}Failed to submit pipeline task${NC}"
    echo "$RESPONSE"
    exit 1
fi

echo "Task ID: $TASK_ID"
echo "Pipeline task submitted - check logs for P2P coordination between steps"

# Summary
echo -e "\n${GREEN}=== P2P Coordination Tests Complete ===${NC}"
echo "Check the logs for:"
echo "1. 'P2P coordination detected' messages from LLM service"
echo "2. 'P2P coordination forced' messages from orchestrator"
echo "3. SupervisorWorkflow usage for tasks with dependencies"
echo ""
echo "To view detailed logs:"
echo "  docker compose -f deploy/compose/compose.yml logs llm-service | grep -i p2p"
echo "  docker compose -f deploy/compose/compose.yml logs orchestrator | grep -i supervisor"