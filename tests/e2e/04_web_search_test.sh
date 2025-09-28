#!/bin/bash

# Web Search Synthesis E2E Test
# Tests that web_search results are properly synthesized at the orchestrator level

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Web Search Synthesis E2E Test Starting${NC}"
echo "Testing web_search tool synthesis at orchestrator level"
echo ""

# Helper functions
check_service() {
    local service=$1
    local port=$2
    if nc -zv localhost $port 2>/dev/null; then
        echo -e "${GREEN}✓${NC} $service is ready on port $port"
        return 0
    else
        echo -e "${RED}✗${NC} $service is not ready on port $port"
        return 1
    fi
}

submit_task() {
    local query="$1"
    local session_id="$2"

    echo -e "[..] Submitting: $query"

    # Submit task and capture response
    response=$(cd "$PROJECT_ROOT" && SESSION_ID="$session_id" ./scripts/submit_task.sh "$query" 2>&1)

    # Extract workflow ID
    workflow_id=$(echo "$response" | grep -o '"workflowId": *"[^"]*"' | head -1 | cut -d'"' -f4)

    if [ -z "$workflow_id" ]; then
        echo -e "${RED}Failed to submit task${NC}"
        echo "$response"
        return 1
    fi

    echo "[..] Workflow ID: $workflow_id"

    # Poll for completion (max 30 seconds)
    for i in {1..30}; do
        sleep 1
        status_response=$(grpcurl -plaintext \
            -d "{\"taskId\":\"$workflow_id\"}" \
            localhost:50052 \
            shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

        if echo "$status_response" | grep -q "TASK_STATUS_COMPLETED"; then
            echo -e "${GREEN}[OK]${NC} Task completed"

            # Extract result
            result=$(echo "$response" | grep "^Result:" | sed 's/^Result: //')
            tokens=$(echo "$response" | grep "^Tokens used:" | sed 's/^Tokens used: //')

            echo "    Result preview: ${result:0:100}..."
            echo "    Tokens: $tokens"

            # Check if result is synthesized (not raw JSON)
            if [[ "$result" == "["* ]] || [[ "$result" == "{"* ]]; then
                echo -e "${RED}[FAIL]${NC} Result appears to be raw JSON, not synthesized"
                return 1
            fi

            return 0
        elif echo "$status_response" | grep -q "TASK_STATUS_FAILED"; then
            echo -e "${RED}[FAIL]${NC} Task failed"
            echo "$status_response"
            return 1
        fi
    done

    echo -e "${RED}[TIMEOUT]${NC} Task did not complete in 30 seconds"
    return 1
}

check_orchestrator_logs() {
    local workflow_id="$1"
    echo "[..] Checking orchestrator logs for synthesis activity"

    # Check for synthesis in orchestrator logs
    if docker compose -f "$PROJECT_ROOT/deploy/compose/docker-compose.yml" logs orchestrator 2>/dev/null | \
       grep -A5 "$workflow_id" | grep -q "performing synthesis"; then
        echo -e "${GREEN}[OK]${NC} Synthesis detected in orchestrator logs"
        return 0
    else
        echo -e "${YELLOW}[WARN]${NC} Could not confirm synthesis in logs"
        return 1
    fi
}

# Main test execution
echo "=== Phase 1: Service Health Checks ==="
check_service "Orchestrator" 50052
check_service "LLM Service" 8000
check_service "Agent Core" 50051
echo ""

echo "=== Phase 2: Web Search Tool Registration ==="
# Check if web_search tool is registered
if curl -s http://localhost:8000/tools/list | grep -q '"web_search"'; then
    echo -e "${GREEN}[OK]${NC} web_search tool is registered"
else
    echo -e "${RED}[FAIL]${NC} web_search tool not found"
    exit 1
fi
echo ""

echo "=== Phase 3: Web Search Query Tests ==="

# Test 1: Stock price query (should trigger web_search)
echo -e "\n${YELLOW}Test 1: Stock price query${NC}"
if submit_task "What is the current stock price of Apple?" "web-search-test-$$-1"; then
    echo -e "${GREEN}[PASS]${NC} Stock price query synthesized correctly"
else
    echo -e "${RED}[FAIL]${NC} Stock price query failed or returned raw JSON"
fi

# Test 2: Current events query (should trigger web_search)
echo -e "\n${YELLOW}Test 2: Current events query${NC}"
if submit_task "What are the latest AI announcements from OpenAI?" "web-search-test-$$-2"; then
    echo -e "${GREEN}[PASS]${NC} Current events query synthesized correctly"
else
    echo -e "${RED}[FAIL]${NC} Current events query failed or returned raw JSON"
fi

# Test 3: Research query (should trigger web_search)
echo -e "\n${YELLOW}Test 3: Research query${NC}"
if submit_task "What are the top programming languages in 2025?" "web-search-test-$$-3"; then
    echo -e "${GREEN}[PASS]${NC} Research query synthesized correctly"
else
    echo -e "${RED}[FAIL]${NC} Research query failed or returned raw JSON"
fi

echo ""
echo "=== Phase 4: Non-Web Search Control Tests ==="

# Test 4: Calculator (should NOT trigger synthesis)
echo -e "\n${YELLOW}Test 4: Calculator query (control)${NC}"
if submit_task "Calculate: (100 * 5) / 2" "calc-test-$$"; then
    echo -e "${GREEN}[PASS]${NC} Calculator query completed"
else
    echo -e "${RED}[FAIL]${NC} Calculator query failed"
fi

# Test 5: Simple question (should NOT use web_search)
echo -e "\n${YELLOW}Test 5: Simple factual query (control)${NC}"
if submit_task "What is 2 + 2?" "simple-test-$$"; then
    echo -e "${GREEN}[PASS]${NC} Simple query completed"
else
    echo -e "${RED}[FAIL]${NC} Simple query failed"
fi

echo ""
echo "================================"
echo -e "${GREEN}Web Search Synthesis Test Complete${NC}"
echo ""
echo "Summary:"
echo "- Web search queries should return synthesized natural language"
echo "- Synthesis happens at Go orchestrator level (SimpleTaskWorkflow)"
echo "- Python /tools/execute returns raw results only"
echo "- Non-web-search tools should not trigger synthesis"
echo "================================"