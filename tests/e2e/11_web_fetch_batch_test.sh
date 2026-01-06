#!/bin/bash

# Web Fetch Batch Mode E2E Test
# Tests that web_fetch(urls=[]) works correctly for batch URL fetching

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Web Fetch Batch Mode E2E Test${NC}"
echo "Testing batch URL fetching with urls=[] parameter"
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

# === Phase 1: Service Health Checks ===
echo "=== Phase 1: Service Health Checks ==="
check_service "LLM Service" 8000
echo ""

# === Phase 2: Web Fetch Tool Registration ===
echo "=== Phase 2: Web Fetch Tool Registration ==="
if curl -s http://localhost:8000/tools/list | grep -q '"web_fetch"'; then
    echo -e "${GREEN}[OK]${NC} web_fetch tool is registered"
else
    echo -e "${RED}[FAIL]${NC} web_fetch tool not found"
    exit 1
fi
echo ""

# === Phase 3: Batch Mode Tests ===
echo "=== Phase 3: Batch Mode Tests ==="

# Test 1: Batch fetch with urls[] parameter
echo ""
echo -e "${YELLOW}Test 1: Batch URL fetch with 2 URLs${NC}"
response=$(curl -s -X POST http://localhost:8000/tools/execute \
    -H "Content-Type: application/json" \
    -d '{
        "tool_name": "web_fetch",
        "parameters": {
            "urls": ["https://httpbin.org/get", "https://httpbin.org/headers"]
        }
    }')

success=$(echo "$response" | jq -r '.success // false')
pages_count=$(echo "$response" | jq -r '.output.pages | length // 0')
succeeded=$(echo "$response" | jq -r '.output.succeeded // 0')
failed=$(echo "$response" | jq -r '.output.failed // 0')

echo "    Success: $success"
echo "    Pages count: $pages_count"
echo "    Succeeded: $succeeded"
echo "    Failed: $failed"

if [ "$success" = "true" ] && [ "$pages_count" -ge 1 ]; then
    echo -e "${GREEN}[PASS]${NC} Batch fetch returned $pages_count pages"
else
    echo -e "${RED}[FAIL]${NC} Batch fetch failed"
    echo "    Response: $response"
    exit 1
fi

# Test 2: Error when both url and urls provided
echo ""
echo -e "${YELLOW}Test 2: Error on conflicting params (url + urls)${NC}"
response=$(curl -s -X POST http://localhost:8000/tools/execute \
    -H "Content-Type: application/json" \
    -d '{"tool_name": "web_fetch", "parameters": {"url": "https://example.com", "urls": ["https://a.com"]}}')

success=$(echo "$response" | jq -r '.success')
error=$(echo "$response" | jq -r '.error // ""')

echo "    Response success: $success"
echo "    Error: $error"

if [ "$success" = "false" ]; then
    echo -e "${GREEN}[PASS]${NC} Correctly rejected conflicting params"
else
    echo -e "${RED}[FAIL]${NC} Should have rejected conflicting params"
    exit 1
fi

# Test 3: Error when neither url nor urls provided
echo ""
echo -e "${YELLOW}Test 3: Error when neither url nor urls provided${NC}"
response=$(curl -s -X POST http://localhost:8000/tools/execute \
    -H "Content-Type: application/json" \
    -d '{"tool_name": "web_fetch", "parameters": {"max_length": 5000}}')

success=$(echo "$response" | jq -r '.success')
error=$(echo "$response" | jq -r '.error // ""')

echo "    Response success: $success"
echo "    Error: $error"

if [ "$success" = "false" ]; then
    echo -e "${GREEN}[PASS]${NC} Correctly rejected missing params"
else
    echo -e "${RED}[FAIL]${NC} Should have rejected missing params"
    exit 1
fi

# Test 4: Single URL mode still works
echo ""
echo -e "${YELLOW}Test 4: Single URL mode (backwards compatibility)${NC}"
response=$(curl -s -X POST http://localhost:8000/tools/execute \
    -H "Content-Type: application/json" \
    -d '{"tool_name": "web_fetch", "parameters": {"url": "https://httpbin.org/get"}}')

success=$(echo "$response" | jq -r '.success')
has_content=$(echo "$response" | jq -r '(.output.content != null and .output.content != "")')

echo "    Response success: $success"

if [ "$success" = "true" ] && [ "$has_content" = "true" ]; then
    echo -e "${GREEN}[PASS]${NC} Single URL mode works"
else
    echo -e "${RED}[FAIL]${NC} Single URL mode failed"
    echo "    Response: $response"
    exit 1
fi

# Test 5: Batch with invalid URLs (should skip invalid, process valid)
echo ""
echo -e "${YELLOW}Test 5: Batch with mixed valid/invalid URLs${NC}"
response=$(curl -s -X POST http://localhost:8000/tools/execute \
    -H "Content-Type: application/json" \
    -d '{"tool_name": "web_fetch", "parameters": {"urls": ["https://httpbin.org/get", "not-a-valid-url", "ftp://invalid.com"]}}')

success=$(echo "$response" | jq -r '.success')
succeeded=$(echo "$response" | jq -r '.output.succeeded // 0')

echo "    Response success: $success"
echo "    Succeeded: $succeeded"

if [ "$success" = "true" ] && [ "$succeeded" -ge 1 ]; then
    echo -e "${GREEN}[PASS]${NC} Batch processed valid URLs, skipped invalid"
else
    echo -e "${RED}[FAIL]${NC} Batch failed with mixed URLs"
    echo "    Response: $response"
    exit 1
fi

# === Phase 4: Log Analysis ===
echo ""
echo "=== Phase 4: Log Analysis ==="
echo "Recent batch fetch activity:"
docker logs shannon-llm-service-1 2>&1 | \
    grep -E "Batch fetch mode|Batch fetching" | tail -5 || echo "(no batch logs found)"

echo ""
echo "================================"
echo -e "${GREEN}Web Fetch Batch Mode Test Complete${NC}"
echo ""
echo "Summary:"
echo "- Batch mode with urls=[] parameter works correctly"
echo "- Parameter validation (url vs urls) works"
echo "- Single URL mode (backwards compatibility) works"
echo "- Invalid URLs in batch are properly skipped"
echo "================================"
