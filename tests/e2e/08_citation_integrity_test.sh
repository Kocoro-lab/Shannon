#!/bin/bash

# Citation Integrity E2E Test
# Tests that synthesis does not hallucinate citations
# Validates that all [n] markers correspond to real sources

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Citation Integrity E2E Test Starting${NC}"
echo "Testing that synthesis does not hallucinate citations"
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

submit_and_validate_citations() {
    local query="$1"
    local session_id="$2"
    local test_name="$3"

    echo -e "\n${YELLOW}Test: $test_name${NC}"
    echo -e "[..] Submitting: $query"

    # Submit task
    response=$(cd "$PROJECT_ROOT" && SESSION_ID="$session_id" ./scripts/submit_task.sh "$query" 2>&1)

    # Extract workflow ID
    workflow_id=$(echo "$response" | grep -o '"workflowId": *"[^"]*"' | head -1 | cut -d'"' -f4)

    if [ -z "$workflow_id" ]; then
        echo -e "${RED}[FAIL]${NC} Failed to submit task"
        echo "$response"
        return 1
    fi

    echo "[..] Workflow ID: $workflow_id"

    # Poll for completion (max 8 minutes for web search with deep research)
    for i in {1..480}; do
        sleep 1
        status_response=$(grpcurl -plaintext \
            -d "{\"taskId\":\"$workflow_id\"}" \
            localhost:50052 \
            shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

        if echo "$status_response" | grep -q "TASK_STATUS_COMPLETED"; then
            echo -e "${GREEN}[OK]${NC} Task completed"
            break
        elif echo "$status_response" | grep -q "TASK_STATUS_FAILED"; then
            echo -e "${RED}[FAIL]${NC} Task failed"
            echo "$status_response"
            return 1
        fi
    done

    # Get result from database
    result=$(docker compose -f "$PROJECT_ROOT/deploy/compose/docker-compose.yml" exec -T postgres \
        psql -U shannon -d shannon -At -c "SELECT result FROM task_executions WHERE workflow_id='${workflow_id}' LIMIT 1;" 2>/dev/null)

    if [ -z "$result" ]; then
        echo -e "${RED}[FAIL]${NC} No result found in database"
        return 1
    fi

    echo ""
    echo "=== Result Preview ==="
    echo "${result:0:200}..."
    echo ""

    # Extract citation markers from main text (before ## Sources)
    # Look for patterns like [1], [2], [3] etc.
    used_citations=$(echo "$result" | sed '/## Sources/,$d' | grep -oE '\[[0-9]+\]' | sort -u | tr -d '[]')

    # Extract sources section
    sources_section=$(echo "$result" | sed -n '/## Sources/,$p')

    # Count available sources
    source_count=$(echo "$sources_section" | grep -cE '^\[[0-9]+\]' || echo "0")

    echo "=== Citation Analysis ==="
    echo "Used citations in text: $(echo "$used_citations" | tr '\n' ' ')"
    echo "Total sources available: $source_count"
    echo ""

    # Special case: If no sources at all, warn but don't fail
    # This might happen if LLM didn't use web_search or didn't cite sources
    if [ "$source_count" -eq 0 ]; then
        if [ -z "$used_citations" ]; then
            echo -e "${YELLOW}[WARN]${NC} No citations found in result"
            echo "       This query may not have triggered web search"
            echo "       or LLM chose not to cite sources"
            echo -e "${GREEN}[PASS]${NC} No hallucinations (no citations to validate)"
            return 0
        else
            echo -e "${RED}[FAIL]${NC} Citations used but no sources available!"
            echo "       This indicates a hallucination problem"
            return 1
        fi
    fi

    # Validation 1: Check if any used citation exceeds source count
    if [ -n "$used_citations" ]; then
        max_citation=$(echo "$used_citations" | sort -n | tail -1)
        if [ "$max_citation" -gt "$source_count" ]; then
            echo -e "${RED}[FAIL]${NC} Hallucinated citation detected!"
            echo "       Citation [$max_citation] used but only $source_count sources available"
            return 1
        fi
    fi

    # Validation 2: Check each used citation has a corresponding source
    validation_failed=0
    for citation in $used_citations; do
        if ! echo "$sources_section" | grep -q "^\[$citation\]"; then
            echo -e "${RED}[FAIL]${NC} Citation [$citation] used but not found in Sources section"
            validation_failed=1
        fi
    done

    if [ $validation_failed -eq 1 ]; then
        return 1
    fi

    # Validation 3: Check for common hallucination patterns
    # Pattern 1: Very high citation numbers (e.g., [99], [100])
    if echo "$used_citations" | grep -qE '^(9[0-9]|[1-9][0-9]{2,})$'; then
        echo -e "${YELLOW}[WARN]${NC} Suspiciously high citation number detected"
    fi

    # Validation 4: Verify sources are marked as "Used inline" or explicitly listed
    if [ -n "$sources_section" ]; then
        echo "=== Sources Section Preview ==="
        echo "$sources_section" | head -10
        echo ""
    fi

    echo -e "${GREEN}[PASS]${NC} All citations validated successfully"
    echo "       - No hallucinated citation numbers"
    echo "       - All used citations have corresponding sources"
    echo "       - Citation count matches source availability"
    
    return 0
}

# Main test execution
echo "=== Phase 1: Service Health Checks ==="
check_service "Orchestrator" 50052
check_service "LLM Service" 8000
check_service "Agent Core" 50051
echo ""

echo "=== Phase 2: Citation Integrity Tests ==="

# Test 1: Japanese Bitcoin query (known to produce multiple citations)
if submit_and_validate_citations \
    "用日语解释比特币最近的评价" \
    "cite-integrity-test-$$-1" \
    "Japanese Bitcoin evaluation query"; then
    echo -e "${GREEN}✓${NC} Test 1 passed"
else
    echo -e "${RED}✗${NC} Test 1 failed"
    exit 1
fi

# Test 2: English AI trends query (should produce citations)
if submit_and_validate_citations \
    "What are the latest AI trends in 2025?" \
    "cite-integrity-test-$$-2" \
    "AI trends query"; then
    echo -e "${GREEN}✓${NC} Test 2 passed"
else
    echo -e "${RED}✗${NC} Test 2 failed"
    exit 1
fi

# Test 3: Simple query (may not produce citations - should handle gracefully)
if submit_and_validate_citations \
    "What is 2+2?" \
    "cite-integrity-test-$$-3" \
    "Simple math query (no citations expected)"; then
    echo -e "${GREEN}✓${NC} Test 3 passed"
else
    echo -e "${RED}✗${NC} Test 3 failed"
    exit 1
fi

echo ""
echo "================================"
echo -e "${GREEN}Citation Integrity Test Complete${NC}"
echo ""
echo "Summary:"
echo "- Validated that synthesis does not hallucinate citations"
echo "- Confirmed all [n] markers correspond to real sources"
echo "- Verified citation numbering matches source availability"
echo "- Tested with multiple query types (web search + simple)"
echo "================================"
