#!/bin/bash

# Web Fetch LLM Usage E2E Test
# Tests that LLM actually uses urls=[] parameter when appropriate
#
# NOTE: This is an OBSERVATIONAL test, not a pass/fail test.
# It submits a research task and checks if the LLM used batch mode.
# If batch mode is not used, it's not necessarily a failure - the LLM
# may have valid reasons for using single URL mode.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Web Fetch LLM Usage Test${NC}"
echo "Testing if LLM uses urls=[] for batch fetching"
echo ""
echo -e "${BLUE}Note: This is an observational test. It checks if the LLM${NC}"
echo -e "${BLUE}decides to use batch mode, but doesn't fail if it doesn't.${NC}"
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

wait_for_task() {
    local task_id=$1
    local max_wait=${2:-120}  # Default 120 seconds
    local wait_interval=3
    local elapsed=0

    while [ $elapsed -lt $max_wait ]; do
        status=$(curl -s "http://localhost:8080/api/v1/tasks/$task_id" | jq -r '.status // "UNKNOWN"')

        if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
            echo -e "${GREEN}[OK]${NC} Task completed"
            return 0
        elif [ "$status" = "TASK_STATUS_FAILED" ]; then
            echo -e "${RED}[FAIL]${NC} Task failed"
            return 1
        fi

        echo "    Status: $status (waiting...)"
        sleep $wait_interval
        elapsed=$((elapsed + wait_interval))
    done

    echo -e "${YELLOW}[TIMEOUT]${NC} Task did not complete in ${max_wait}s"
    return 1
}

# === Phase 1: Service Health Checks ===
echo "=== Phase 1: Service Health Checks ==="
check_service "Gateway" 8080
check_service "LLM Service" 8000
echo ""

# Clear previous batch logs (mark current position)
echo "=== Phase 2: Marking Log Position ==="
log_marker=$(docker logs shannon-llm-service-1 2>&1 | wc -l)
echo "    Log position: line $log_marker"
echo ""

# === Phase 3: Submit Research Task ===
echo "=== Phase 3: Submitting Research Task ==="
echo "Submitting a task that should benefit from batch URL fetching..."

# This query should trigger web_search followed by web_fetch of multiple URLs
task_response=$(curl -s -X POST http://localhost:8080/api/v1/tasks \
    -H "Content-Type: application/json" \
    -d '{
        "query": "Compare the main features of Python and JavaScript programming languages based on their official documentation",
        "context": {
            "force_research": true
        }
    }')

task_id=$(echo "$task_response" | jq -r '.task_id // ""')

if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
    echo -e "${RED}[FAIL]${NC} Failed to submit task"
    echo "    Response: $task_response"
    exit 1
fi

echo -e "${GREEN}[OK]${NC} Task submitted: $task_id"
echo ""

# === Phase 4: Wait for Completion ===
echo "=== Phase 4: Waiting for Task Completion ==="
if ! wait_for_task "$task_id" 180; then
    echo -e "${YELLOW}[WARN]${NC} Task did not complete successfully"
fi
echo ""

# === Phase 5: Analyze Batch Usage ===
echo "=== Phase 5: Analyzing Batch Mode Usage ==="

# Check logs for batch fetch usage (only logs after our marker)
echo "Checking logs for batch fetch activity..."

# Count batch mode invocations
batch_calls=$(docker logs shannon-llm-service-1 2>&1 | tail -n +$log_marker | \
    grep -c "Batch fetch mode:" || echo "0")

batch_fetching=$(docker logs shannon-llm-service-1 2>&1 | tail -n +$log_marker | \
    grep -c "Batch fetching" || echo "0")

single_url_calls=$(docker logs shannon-llm-service-1 2>&1 | tail -n +$log_marker | \
    grep -c "Fetching with.*:" || echo "0")

echo ""
echo "=== Results ==="
echo "    Batch mode invocations: $batch_calls"
echo "    Batch fetching operations: $batch_fetching"
echo "    Single URL fetches (approx): $single_url_calls"
echo ""

if [ "$batch_calls" -gt 0 ] || [ "$batch_fetching" -gt 0 ]; then
    echo -e "${GREEN}[OBSERVED]${NC} LLM used batch fetch mode!"
    echo "    This indicates the LLM understood and used urls=[] parameter."
else
    echo -e "${YELLOW}[NOT OBSERVED]${NC} No batch fetch detected in this run."
    echo "    Possible reasons:"
    echo "    - LLM chose to fetch URLs one at a time"
    echo "    - Task only required single URL fetches"
    echo "    - Prompt guidance may need strengthening"
    echo ""
    echo "    This is not necessarily a failure - review the task output."
fi

# === Phase 6: Show Relevant Logs ===
echo ""
echo "=== Phase 6: Relevant Log Excerpts ==="
echo "web_fetch related logs:"
docker logs shannon-llm-service-1 2>&1 | tail -n +$log_marker | \
    grep -E "Batch fetch|web_fetch|Fetching with|urls=" | tail -10 || echo "(no relevant logs)"

# === Phase 7: Get Task Result ===
echo ""
echo "=== Phase 7: Task Result Summary ==="
result=$(curl -s "http://localhost:8080/api/v1/tasks/$task_id")
status=$(echo "$result" | jq -r '.status')
has_result=$(echo "$result" | jq -r '.result != null and .result != ""')

echo "    Final status: $status"
echo "    Has result: $has_result"

echo ""
echo "================================"
echo -e "${GREEN}Web Fetch LLM Usage Test Complete${NC}"
echo ""
echo "Summary:"
echo "- Batch fetch calls detected: $batch_calls"
echo "- This test observes LLM behavior, it doesn't enforce batch usage"
echo "- If batch mode is not used, consider:"
echo "  1. Checking if web_search returned multiple URLs"
echo "  2. Reviewing the deep_research_agent prompt"
echo "  3. Testing with queries that clearly need multiple sources"
echo "================================"
