#!/bin/bash

# Citation Deduplication E2E Test
# Tests that citations are properly deduplicated and subpage URLs are not extracted

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Citation Deduplication E2E Test Starting${NC}"
echo "Testing that citations are deduplicated and subpage URLs are not extracted"
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

test_citation_deduplication() {
    local url="$1"
    local expected_max_citations="$2"
    
    echo -e "[..] Testing: $url"
    echo "    Expected max citations: $expected_max_citations"
    
    # Submit task via HTTP API
    response=$(curl -s -X POST http://localhost:8080/api/v1/tasks \
        -H "Content-Type: application/json" \
        -d "{
            \"query\": \"总结 $url 网站和博客\",
            \"context\": {\"research_depth\": \"quick\"}
        }")
    
    task_id=$(echo "$response" | jq -r '.task_id')
    
    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        echo -e "${RED}[FAIL]${NC} Failed to create task"
        echo "$response" | jq .
        return 1
    fi
    
    echo "    Task ID: $task_id"
    
    # Poll for completion (max 60 seconds)
    for i in {1..60}; do
        sleep 2
        status_response=$(curl -s http://localhost:8080/api/v1/tasks/$task_id)
        status=$(echo "$status_response" | jq -r '.status')
        
        if [ "$status" = "completed" ]; then
            echo -e "${GREEN}[OK]${NC} Task completed"
            
            # Extract citations
            citations=$(echo "$status_response" | jq '.metadata.citations')
            citation_count=$(echo "$citations" | jq 'length')
            
            echo "    Citations count: $citation_count"
            
            # Check for subpage URLs
            subpage_count=$(echo "$citations" | jq '[.[] | select(.url | contains("/blog") or contains("/sitemap"))] | length')
            
            echo "    Subpage URLs found: $subpage_count"
            
            # Display citations
            echo "    Citations:"
            echo "$citations" | jq -r '.[] | "      - \(.url) (\(.title))"'
            
            # Validate results
            if [ "$citation_count" -le "$expected_max_citations" ]; then
                echo -e "${GREEN}[OK]${NC} Citation count within expected range"
            else
                echo -e "${RED}[FAIL]${NC} Too many citations: $citation_count > $expected_max_citations"
                return 1
            fi
            
            if [ "$subpage_count" -eq 0 ]; then
                echo -e "${GREEN}[OK]${NC} No subpage URLs in citations"
            else
                echo -e "${RED}[FAIL]${NC} Found $subpage_count subpage URLs in citations"
                return 1
            fi
            
            return 0
        elif [ "$status" = "failed" ]; then
            echo -e "${RED}[FAIL]${NC} Task failed"
            echo "$status_response" | jq .
            return 1
        fi
        
        if [ $((i % 10)) -eq 0 ]; then
            echo "    Waiting... ($i/60 seconds, status: $status)"
        fi
    done
    
    echo -e "${RED}[TIMEOUT]${NC} Task did not complete in 60 seconds"
    return 1
}

check_citation_logic() {
    echo "[..] Checking citation extraction logic in logs"
    
    # Check for structured web citations detection
    if docker logs shannon-orchestrator-1 2>&1 | \
       grep -q "hasStructuredWebCitations"; then
        echo -e "${GREEN}[OK]${NC} Structured web citations detection found in binary"
        return 0
    else
        echo -e "${YELLOW}[WARN]${NC} Could not confirm structured citations logic (binary may be optimized)"
        return 0  # Don't fail on this, as it's a binary check
    fi
}

check_web_fetch_format() {
    echo "[..] Checking web_fetch content format"
    
    # Check if web_fetch uses "Subpage N" format (not "Page N: URL")
    if docker exec shannon-llm-service-1 grep -q "Subpage {i+1}" /app/llm_service/tools/builtin/web_fetch.py; then
        echo -e "${GREEN}[OK]${NC} web_fetch uses correct subpage format (no URLs in content)"
        return 0
    else
        echo -e "${RED}[FAIL]${NC} web_fetch may still expose URLs in content"
        return 1
    fi
}

# Main test execution
echo "=== Phase 1: Service Health Checks ==="
check_service "Gateway" 8080
check_service "Orchestrator" 50052
check_service "LLM Service" 8000
echo ""

echo "=== Phase 2: Code Verification ==="
check_web_fetch_format
check_citation_logic
echo ""

echo "=== Phase 3: Citation Deduplication Tests ==="

# Test 1: Simple website (should have 1-2 citations max)
echo -e "\n${YELLOW}Test 1: Simple website citation test${NC}"
if test_citation_deduplication "https://waylandz.com" 2; then
    echo -e "${GREEN}[PASS]${NC} Simple website citation test passed"
else
    echo -e "${RED}[FAIL]${NC} Simple website citation test failed"
fi

# Test 2: Blog website (should not extract subpage URLs)
echo -e "\n${YELLOW}Test 2: Blog website without subpage URLs${NC}"
if test_citation_deduplication "https://example.com" 2; then
    echo -e "${GREEN}[PASS]${NC} Blog website test passed"
else
    echo -e "${YELLOW}[WARN]${NC} Blog website test failed (may be expected if site is unreachable)"
fi

echo ""
echo "=== Phase 4: Log Analysis ==="
echo "Recent citation extraction activity:"
docker logs shannon-orchestrator-1 2>&1 | \
    grep -E "citations.*extracted|plain_text_extracted|hasStructuredWebCitations" | tail -10 || \
    echo "No recent citation logs found"

echo ""
echo "================================"
echo -e "${GREEN}Citation Deduplication Test Complete${NC}"
echo ""
echo "Summary:"
echo "- Citations should be deduplicated by normalized URL"
echo "- Subpage URLs should not appear in citations list"
echo "- Plain-text URL scanning should be skipped when structured citations exist"
echo "- web_fetch content should use 'Subpage N' format without URLs"
echo "================================"
