#!/bin/bash
# Comprehensive test script to validate parameter flow through all layers
# Run this after `make dev` to test the fixes

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "================================"
echo "Parameter Flow Integration Tests"
echo "================================"

# Get API key from env
API_KEY="${SHANNON_TEST_API_KEY:-test-key-123}"
BASE_URL="http://localhost:8080/api/v1"

# Helper function to submit task and check logs
test_task() {
    local test_name="$1"
    local json_payload="$2"
    local expected_checks="$3"

    echo -e "\n${YELLOW}Test: ${test_name}${NC}"
    echo "Payload: ${json_payload}"

    # Submit task
    response=$(curl -s -X POST "${BASE_URL}/tasks" \
        -H "Authorization: Bearer ${API_KEY}" \
        -H "Content-Type: application/json" \
        -d "${json_payload}")

    task_id=$(echo $response | jq -r '.task_id // .workflow_id // empty')

    if [ -z "$task_id" ]; then
        echo -e "${RED}✗ Failed to submit task${NC}"
        echo "Response: $response"
        return 1
    fi

    echo "Task ID: $task_id"

    # Wait for task to process
    sleep 3

    # Check orchestrator logs for the workflow
    echo "Checking orchestrator logs..."
    docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 100 | grep -A5 -B5 "$task_id" || true

    # Check Python service logs
    echo "Checking Python service logs..."
    docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail 100 | grep -A5 -B5 "$task_id" || true

    # Get task status
    status_response=$(curl -s -X GET "${BASE_URL}/tasks/${task_id}" \
        -H "Authorization: Bearer ${API_KEY}")

    echo "Task Status: $(echo $status_response | jq -r '.status')"

    echo -e "${GREEN}✓ Test completed${NC}"
}

# Test 1: Mode parameter flow
echo -e "\n${YELLOW}=== Test 1: Mode Parameter Flow ===${NC}"
test_task "Mode=supervisor" \
    '{"query": "test mode routing", "mode": "supervisor"}' \
    "Expecting mode to route to SupervisorWorkflow"

# Test 2: Top-level model_tier injection
echo -e "\n${YELLOW}=== Test 2: Top-level model_tier ===${NC}"
test_task "model_tier top-level" \
    '{"query": "test tier injection", "model_tier": "large"}' \
    "Expecting model_tier=large in context"

# Test 3: Context model_tier (no top-level)
echo -e "\n${YELLOW}=== Test 3: Context model_tier ===${NC}"
test_task "model_tier in context" \
    '{"query": "test context tier", "context": {"model_tier": "small"}}' \
    "Expecting model_tier=small from context"

# Test 4: Top-level overrides context
echo -e "\n${YELLOW}=== Test 4: Top-level overrides context ===${NC}"
test_task "tier override" \
    '{"query": "test override", "model_tier": "large", "context": {"model_tier": "small"}}' \
    "Expecting model_tier=large (top-level wins)"

# Test 5: Template alias (template_name)
echo -e "\n${YELLOW}=== Test 5: Template alias ===${NC}"
test_task "template_name alias" \
    '{"query": "test template", "context": {"template_name": "research_summary"}}' \
    "Expecting template_name to be recognized"

# Test 6: Combined parameters
echo -e "\n${YELLOW}=== Test 6: Combined parameters ===${NC}"
test_task "all parameters" \
    '{
        "query": "test all params",
        "session_id": "test-session-123",
        "mode": "supervisor",
        "model_tier": "large",
        "context": {
            "role": "analysis",
            "template": "research_summary",
            "prompt_params": {"user_id": "12345"},
            "history_window_size": 50
        }
    }' \
    "Expecting all parameters to flow correctly"

# Test 7: Check Rust tier override
echo -e "\n${YELLOW}=== Test 7: Rust tier override ===${NC}"
# This would need agent-core logs to verify
test_task "Rust tier check" \
    '{"query": "execute: print(\"Testing Rust tier\")", "model_tier": "large", "mode": "simple"}' \
    "Expecting Rust to use large tier despite simple mode"

echo -e "\n${GREEN}================================${NC}"
echo -e "${GREEN}All tests completed!${NC}"
echo -e "${GREEN}================================${NC}"

echo -e "\n${YELLOW}Manual verification steps:${NC}"
echo "1. Check Temporal UI at http://localhost:8088 for workflow details"
echo "2. Check Grafana at http://localhost:3000 for metrics"
echo "3. Run: docker compose -f deploy/compose/docker-compose.yml logs -f orchestrator"
echo "4. Run: docker compose -f deploy/compose/docker-compose.yml logs -f llm-service"
echo "5. Run: docker compose -f deploy/compose/docker-compose.yml logs -f agent-core"