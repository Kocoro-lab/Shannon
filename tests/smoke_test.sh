#!/bin/bash
# Quick Smoke Test - Validates Critical Fixes
# Run this after deployments or code changes to verify system health

set -e

BASE_URL="${BASE_URL:-http://localhost:8080}"

GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}Shannon Quick Smoke Test${NC}"
echo "========================="
echo ""

# Test 1: Service Health
echo -e "${BLUE}1. Checking service health...${NC}"
if curl -sf "$BASE_URL/health" > /dev/null; then
    echo -e "${GREEN}✓ Gateway is healthy${NC}"
else
    echo -e "${RED}✗ Gateway is not accessible${NC}"
    exit 1
fi

# Test 2: Simple Task
echo -e "${BLUE}2. Testing simple task submission...${NC}"
TASK_ID=$(curl -sS -X POST "$BASE_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -d '{"query":"5+5","context":{"role":"simple"}}' | jq -r '.task_id')

if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    echo -e "${GREEN}✓ Task submitted: $TASK_ID${NC}"
else
    echo -e "${RED}✗ Task submission failed${NC}"
    exit 1
fi

# Test 3: UTF-8 Handling
echo -e "${BLUE}3. Testing UTF-8 Chinese text...${NC}"
TASK_ID=$(curl -sS -X POST "$BASE_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -d '{"query":"你好世界","context":{"role":"simple"}}' | jq -r '.task_id')

if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    echo -e "${GREEN}✓ UTF-8 task submitted: $TASK_ID${NC}"
else
    echo -e "${RED}✗ UTF-8 task submission failed${NC}"
    exit 1
fi

# Test 4: Task Completion Check
echo -e "${BLUE}4. Checking task completion...${NC}"
sleep 15
RESULT=$(curl -sS "$BASE_URL/api/v1/tasks/$TASK_ID")
STATUS=$(echo "$RESULT" | jq -r '.status')

if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] || [ "$STATUS" = "TASK_STATUS_RUNNING" ]; then
    echo -e "${GREEN}✓ Task is progressing (status: $STATUS)${NC}"

    # Check metadata if completed
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ]; then
        MODEL=$(echo "$RESULT" | jq -r '.model_used')
        PROVIDER=$(echo "$RESULT" | jq -r '.provider')
        if [ -n "$MODEL" ] && [ "$MODEL" != "null" ]; then
            echo -e "${GREEN}✓ Metadata populated (model: $MODEL, provider: $PROVIDER)${NC}"
        fi
    fi
else
    echo -e "${RED}✗ Task failed with status: $STATUS${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✨ All smoke tests passed!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "System is ready for testing."
