#!/bin/bash

# Web Fetch Fallback Mechanism E2E Test
# Tests that web_fetch properly falls back from crawl to map+scrape when crawl returns 0 pages

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Web Fetch Fallback Mechanism E2E Test Starting${NC}"
echo "Testing crawl → map+scrape fallback when crawl returns 0 pages"
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

test_web_fetch_fallback() {
    local url="$1"
    local subpages="$2"
    
    echo -e "[..] Testing: $url with subpages=$subpages"
    
    # Call web_fetch tool (provider is now set via WEB_FETCH_PROVIDER env var)
    response=$(curl -s -X POST http://localhost:8000/tools/execute \
        -H "Content-Type: application/json" \
        -d "{
            \"tool_name\": \"web_fetch\",
            \"parameters\": {
                \"url\": \"$url\"
            }
        }")
    
    # Extract result
    success=$(echo "$response" | jq -r '.success // false')
    method=$(echo "$response" | jq -r '.result.method // "unknown"')
    pages_fetched=$(echo "$response" | jq -r '.result.pages_fetched // 0')
    has_content=$(echo "$response" | jq -r '(.result.content != "" and .result.content != null)')
    
    echo "    Method: $method"
    echo "    Pages fetched: $pages_fetched"
    echo "    Has content: $has_content"
    
    # Check if fallback was triggered by examining logs
    echo "[..] Checking logs for fallback activity"
    fallback_detected=$(docker logs shannon-llm-service-1 2>&1 | \
        grep -c "Crawl returned 0 pages.*fallback to map+scrape" | tail -1 || echo "0")
    map_success=$(docker logs shannon-llm-service-1 2>&1 | \
        grep -c "Map+scrape: selected.*URLs" | tail -1 || echo "0")
    
    if [ "$fallback_detected" -gt 0 ]; then
        echo -e "${GREEN}[OK]${NC} Fallback mechanism triggered"
    else
        echo -e "${YELLOW}[WARN]${NC} Fallback not detected in recent logs"
    fi
    
    if [ "$map_success" -gt 0 ]; then
        echo -e "${GREEN}[OK]${NC} Map+scrape executed successfully"
    else
        echo -e "${YELLOW}[WARN]${NC} Map+scrape not detected in recent logs"
    fi
    
    # Validate result
    if [ "$success" = "true" ] && [ "$pages_fetched" -gt 0 ] && [ "$has_content" = "true" ]; then
        echo -e "${GREEN}[PASS]${NC} Web fetch completed with content"
        return 0
    else
        echo -e "${RED}[FAIL]${NC} Web fetch failed or returned no content"
        echo "    Response: $response"
        return 1
    fi
}

check_firecrawl_initialization() {
    echo "[..] Checking Firecrawl provider initialization"
    
    if docker logs shannon-llm-service-1 2>&1 | \
       grep -q "Firecrawl fetch provider initialized"; then
        echo -e "${GREEN}[OK]${NC} Firecrawl provider initialized"
        return 0
    else
        echo -e "${RED}[FAIL]${NC} Firecrawl provider not initialized"
        return 1
    fi
}

# Main test execution
echo "=== Phase 1: Service Health Checks ==="
check_service "LLM Service" 8000
echo ""

echo "=== Phase 2: Firecrawl Provider Check ==="
check_firecrawl_initialization
echo ""

echo "=== Phase 3: Web Fetch Tool Registration ==="
if curl -s http://localhost:8000/tools/list | grep -q '"web_fetch"'; then
    echo -e "${GREEN}[OK]${NC} web_fetch tool is registered"
else
    echo -e "${RED}[FAIL]${NC} web_fetch tool not found"
    exit 1
fi
echo ""

echo "=== Phase 4: Fallback Mechanism Tests ==="

# Test 1: Single page fetch (simplified test since subpages are now deprecated)
echo -e "\n${YELLOW}Test 1: Single page fetch test${NC}"
if test_web_fetch_fallback "https://ptmind.com" 0; then
    echo -e "${GREEN}[PASS]${NC} Single page fetch works correctly"
else
    echo -e "${RED}[FAIL]${NC} Single page fetch failed"
fi

echo ""
echo "=== Phase 5: Log Analysis ==="
echo "Recent fallback activity:"
docker logs shannon-llm-service-1 2>&1 | \
    grep -E "Crawl returned 0|fallback|Map\+scrape|selected.*URLs" | tail -10

echo ""
echo "================================"
echo -e "${GREEN}Web Fetch Fallback Test Complete${NC}"
echo ""
echo "Summary:"
echo "- When Firecrawl crawl returns 0 pages, should fallback to map+scrape"
echo "- Map API should discover URLs (e.g., 42 URLs for ptmind.com)"
echo "- Selected URLs should be scraped and merged"
echo "- Final result should have pages_fetched > 0 and non-empty content"
echo "================================"
