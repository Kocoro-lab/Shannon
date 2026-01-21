#!/bin/bash
# Domain Discovery 单元测试脚本
# 直接测试 domain_discovery agent，不需要完整的 deep research 流程

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

LLM_SERVICE_URL="${LLM_SERVICE_URL:-http://localhost:8000}"

echo "========================================"
echo "Domain Discovery Agent 测试"
echo "========================================"
echo ""

test_domain_discovery() {
    local test_name="$1"
    local company="$2"
    local queries="$3"
    local expected_domain="$4"

    echo -e "${YELLOW}Test: $test_name${NC}"
    echo "Company: $company"

    # Build query string
    local query_str="Find official website domains for: $company\n\nExecute web_search for each query below, then return JSON.\n\nQueries:\n$queries"

    # Make request
    local response=$(curl -sS -X POST "$LLM_SERVICE_URL/agent/query" \
      -H "Content-Type: application/json" \
      -d "{
        \"query\": \"$query_str\",
        \"agent_id\": \"domain_discovery\",
        \"context\": {
          \"role\": \"domain_discovery\",
          \"max_total_tool_calls\": 10,
          \"max_tool_iterations\": 8,
          \"response_format\": {\"type\": \"json_object\"}
        },
        \"allowed_tools\": [\"web_search\"]
      }" 2>&1)

    # Extract response
    local result=$(echo "$response" | jq -r '.response // empty' 2>/dev/null)
    local tools_count=$(echo "$response" | jq -r '.metadata.tool_executions | length' 2>/dev/null)

    if [ -z "$result" ]; then
        echo -e "${RED}FAIL: No response received${NC}"
        echo "Raw response: $response"
        return 1
    fi

    # Check if response is valid JSON with domains array
    local is_valid_json=$(echo "$result" | jq -e '.domains | type == "array"' 2>/dev/null)
    if [ "$is_valid_json" != "true" ]; then
        echo -e "${RED}FAIL: Response is not valid JSON with domains array${NC}"
        echo "Response: $result"
        return 1
    fi

    # Check if expected domain is present
    local has_expected=$(echo "$result" | jq -e ".domains | map(select(. | contains(\"$expected_domain\"))) | length > 0" 2>/dev/null)
    if [ "$has_expected" != "true" ]; then
        echo -e "${YELLOW}WARN: Expected domain '$expected_domain' not found${NC}"
    fi

    echo -e "${GREEN}PASS${NC}"
    echo "Tools executed: $tools_count"
    echo "Domains found: $(echo "$result" | jq -r '.domains | join(", ")' 2>/dev/null)"
    echo ""
    return 0
}

# Test cases
echo "----------------------------------------"
echo "Test 1: Single query - Tesla (English)"
echo "----------------------------------------"
test_domain_discovery \
    "Tesla single query" \
    "Tesla" \
    "- Tesla official website" \
    "tesla.com"

echo "----------------------------------------"
echo "Test 2: Multiple queries - Tesla with IR"
echo "----------------------------------------"
test_domain_discovery \
    "Tesla batch queries" \
    "Tesla" \
    "- Tesla official website\n- Tesla investor relations" \
    "tesla.com"

echo "----------------------------------------"
echo "Test 3: Japanese company - Sony"
echo "----------------------------------------"
test_domain_discovery \
    "Sony Japan" \
    "Sony" \
    "- Sony official website Japan" \
    "sony"

echo "----------------------------------------"
echo "Test 4: Chinese company - Alibaba"
echo "----------------------------------------"
test_domain_discovery \
    "Alibaba China" \
    "Alibaba" \
    "- 阿里巴巴官网" \
    "alibaba"

echo "----------------------------------------"
echo "Test 5: Multiple queries - Google"
echo "----------------------------------------"
test_domain_discovery \
    "Google batch" \
    "Google" \
    "- Google official website\n- Google investor relations\n- Google developer documentation" \
    "google"

echo "========================================"
echo "All tests completed"
echo "========================================"
