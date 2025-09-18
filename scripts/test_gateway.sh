#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-sk_test_123456}"

echo -e "${YELLOW}Testing Shannon Gateway API${NC}"
echo "Gateway URL: $GATEWAY_URL"
echo ""

# Test 1: Health Check (no auth required)
echo -e "${YELLOW}1. Testing health endpoint...${NC}"
response=$(curl -s -w "\n%{http_code}" "$GATEWAY_URL/health")
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ Health check passed${NC}"
    echo "Response: $body"
else
    echo -e "${RED}✗ Health check failed (HTTP $http_code)${NC}"
    echo "Response: $body"
fi
echo ""

# Test 2: OpenAPI Spec (no auth required)
echo -e "${YELLOW}2. Testing OpenAPI spec endpoint...${NC}"
response=$(curl -s -w "\n%{http_code}" "$GATEWAY_URL/openapi.json")
http_code=$(echo "$response" | tail -n1)

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ OpenAPI spec retrieved${NC}"
    echo "Spec contains $(echo "$response" | sed '$d' | jq -r '.paths | keys | length') endpoints"
else
    echo -e "${RED}✗ Failed to get OpenAPI spec (HTTP $http_code)${NC}"
fi
echo ""

# Test 3: Submit task without auth (should fail)
echo -e "${YELLOW}3. Testing task submission without auth...${NC}"
response=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -d '{"query":"What is 2+2?"}')
http_code=$(echo "$response" | tail -n1)

if [ "$http_code" = "401" ]; then
    echo -e "${GREEN}✓ Correctly rejected unauthorized request${NC}"
else
    echo -e "${RED}✗ Expected 401 but got HTTP $http_code${NC}"
fi
echo ""

# Test 4: Submit task with auth
echo -e "${YELLOW}4. Testing task submission with auth...${NC}"
response=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $API_KEY" \
    -d '{"query":"What is 2+2?", "mode":"simple"}')
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ Task submitted successfully${NC}"
    task_id=$(echo "$body" | jq -r '.task_id')
    echo "Task ID: $task_id"

    # Test 5: Get task status
    echo ""
    echo -e "${YELLOW}5. Testing task status retrieval...${NC}"
    sleep 2
    response=$(curl -s -w "\n%{http_code}" "$GATEWAY_URL/api/v1/tasks/$task_id" \
        -H "X-API-Key: $API_KEY")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$http_code" = "200" ]; then
        echo -e "${GREEN}✓ Task status retrieved${NC}"
        echo "Status: $(echo "$body" | jq -r '.status')"
    else
        echo -e "${RED}✗ Failed to get task status (HTTP $http_code)${NC}"
    fi
else
    echo -e "${RED}✗ Task submission failed (HTTP $http_code)${NC}"
    echo "Response: $body"
fi
echo ""

# Test 6: Test idempotency
echo -e "${YELLOW}6. Testing idempotency...${NC}"
idempotency_key="test-$(date +%s)"
response1=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $API_KEY" \
    -H "Idempotency-Key: $idempotency_key" \
    -d '{"query":"Test idempotency"}')

response2=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $API_KEY" \
    -H "Idempotency-Key: $idempotency_key" \
    -d '{"query":"Test idempotency"}')

task_id1=$(echo "$response1" | sed '$d' | jq -r '.task_id')
task_id2=$(echo "$response2" | sed '$d' | jq -r '.task_id')

if [ "$task_id1" = "$task_id2" ]; then
    echo -e "${GREEN}✓ Idempotency works - same task ID returned${NC}"
else
    echo -e "${RED}✗ Idempotency failed - different task IDs${NC}"
fi
echo ""

# Test 7: Test rate limiting
echo -e "${YELLOW}7. Testing rate limiting...${NC}"
echo "Sending 10 rapid requests..."
rate_limited=false
for i in {1..10}; do
    response=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -H "X-API-Key: $API_KEY" \
        -d '{"query":"Rate limit test"}')
    http_code=$(echo "$response" | tail -n1)

    if [ "$http_code" = "429" ]; then
        rate_limited=true
        echo -e "${GREEN}✓ Rate limit enforced at request $i${NC}"
        break
    fi
done

if [ "$rate_limited" = false ]; then
    echo -e "${YELLOW}⚠ Rate limit not triggered in 10 requests (may have high limit)${NC}"
fi
echo ""

# Summary
echo -e "${YELLOW}=== Test Summary ===${NC}"
echo "Gateway endpoint tests completed"
echo "Check the logs for any errors: docker compose logs gateway"