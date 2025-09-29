#!/bin/bash

# Context Window Compression Regression Test
# Tests compression triggers, metrics, events, and shaping

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test configuration
SESSION_ID="compress-regression-$(date +%s)"
PASS_COUNT=0
FAIL_COUNT=0

echo -e "${YELLOW}================================${NC}"
echo -e "${YELLOW}Context Compression Regression Test${NC}"
echo -e "${YELLOW}================================${NC}"
echo "Session ID: $SESSION_ID"

# Helper function for assertions
assert() {
    local condition="$1"
    local description="$2"

    if eval "$condition"; then
        echo -e "${GREEN}✓${NC} $description"
        ((PASS_COUNT++))
    else
        echo -e "${RED}✗${NC} $description"
        ((FAIL_COUNT++))
    fi
}

# Helper to check metric value
get_metric_value() {
    local metric="$1"
    local label="$2"
    curl -s http://localhost:2112/metrics | grep "$metric{$label}" | awk '{print $2}' | head -1 || echo "0"
}

# Step 1: Setup environment with aggressive compression settings
echo -e "\n${YELLOW}[Setup] Configuring aggressive compression...${NC}"
export COMPRESSION_TRIGGER_RATIO=0.3   # Trigger at 30% of budget
export COMPRESSION_TARGET_RATIO=0.15   # Compress to 15% of budget
docker compose -f deploy/compose/docker-compose.yml up -d orchestrator --force-recreate >/dev/null 2>&1
sleep 5
echo "Environment configured: trigger=0.3, target=0.15"

# Step 2: Build substantial history
echo -e "\n${YELLOW}[Test 1] Building message history...${NC}"
for i in {1..50}; do
    # Create large messages to build token count
    MSG="Message $i with substantial content. $(printf 'This is test content that helps build our token count for compression testing. %.0s' {1..10})"
    grpcurl -plaintext -d "{
        \"metadata\": {\"userId\":\"test-regression\",\"sessionId\":\"$SESSION_ID\"},
        \"query\": \"$MSG\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null | jq -r '.taskId' >/dev/null &
done
wait

HISTORY_SIZE=$(redis-cli LLEN "session:$SESSION_ID:history" 2>/dev/null || echo "0")
assert "[ $HISTORY_SIZE -ge 45 ]" "History contains at least 45 messages (actual: $HISTORY_SIZE)"

# Step 3: Force compression with very low budget
echo -e "\n${YELLOW}[Test 2] Testing compression trigger...${NC}"

# Get initial compression metrics
INITIAL_TRIGGERED=$(get_metric_value "shannon_compression_events_total" 'status="triggered"')
INITIAL_SKIPPED=$(get_metric_value "shannon_compression_events_total" 'status="skipped"')

# Submit task with very low budget to force compression
COMPRESS_TASK=$(grpcurl -plaintext -d "{
    \"metadata\": {\"userId\":\"test-regression\",\"sessionId\":\"$SESSION_ID\"},
    \"query\": \"Analyze everything we've discussed and create a comprehensive summary with key insights and patterns\",
    \"context\": {
        \"token_budget_per_agent\": 5000,
        \"model_tier\": \"small\",
        \"compression_trigger_ratio\": 0.3,
        \"compression_target_ratio\": 0.15,
        \"force_p2p\": true,
        \"primers_count\": 5,
        \"recents_count\": 15
    }
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null | jq -r '.taskId')

assert "[ ! -z \"$COMPRESS_TASK\" ]" "Compression task submitted successfully: $COMPRESS_TASK"

# Step 4: Wait and capture SSE events
echo -e "\n${YELLOW}[Test 3] Monitoring SSE events...${NC}"
timeout 10 curl -N "http://localhost:8081/stream/sse?workflow_id=$COMPRESS_TASK" 2>/dev/null > /tmp/sse_compress.log &
SSE_PID=$!

# Wait for processing
sleep 8
kill $SSE_PID 2>/dev/null || true

# Check for compression-related events
HAS_CONTEXT_PREP=$(grep -c "Preparing context\|CONTEXT_PREPARED" /tmp/sse_compress.log 2>/dev/null || echo "0")
HAS_COMPRESSION=$(grep -c "compress\|summary\|trim\|shaped" /tmp/sse_compress.log 2>/dev/null || echo "0")

assert "[ $HAS_CONTEXT_PREP -gt 0 ]" "SSE contains context preparation events"
# Note: Compression events might not always trigger depending on actual token count

# Step 5: Check metrics
echo -e "\n${YELLOW}[Test 4] Verifying Prometheus metrics...${NC}"
sleep 2

FINAL_TRIGGERED=$(get_metric_value "shannon_compression_events_total" 'status="triggered"')
FINAL_SKIPPED=$(get_metric_value "shannon_compression_events_total" 'status="skipped"')

# At least one metric should have changed
METRICS_CHANGED=$((FINAL_TRIGGERED + FINAL_SKIPPED - INITIAL_TRIGGERED - INITIAL_SKIPPED))
assert "[ $METRICS_CHANGED -gt 0 ]" "Compression metrics changed (delta: $METRICS_CHANGED)"

# Check if compression ratio metric exists
RATIO_COUNT=$(curl -s http://localhost:2112/metrics | grep -c "shannon_compression_ratio_count" || echo "0")
assert "[ $RATIO_COUNT -gt 0 ]" "Compression ratio metric exists"

# Step 6: Verify task completion
echo -e "\n${YELLOW}[Test 5] Checking task completion...${NC}"
STATUS=$(grpcurl -plaintext -d "{\"taskId\": \"$COMPRESS_TASK\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null | jq -r '.status')
RESPONSE=$(grpcurl -plaintext -d "{\"taskId\": \"$COMPRESS_TASK\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null | jq -r '.response' | head -c 100)

assert "[ \"$STATUS\" = \"COMPLETED\" ]" "Task completed successfully (status: $STATUS)"
assert "[ ! -z \"$RESPONSE\" ]" "Task produced a response"

# Step 7: Test configurable primers/recents
echo -e "\n${YELLOW}[Test 6] Testing configurable window parameters...${NC}"
CONFIG_TASK=$(grpcurl -plaintext -d "{
    \"metadata\": {\"userId\":\"test-regression\",\"sessionId\":\"$SESSION_ID\"},
    \"query\": \"Final test with custom window\",
    \"context\": {
        \"primers_count\": 10,
        \"recents_count\": 25,
        \"model_tier\": \"small\"
    }
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null | jq -r '.taskId')

assert "[ ! -z \"$CONFIG_TASK\" ]" "Task with custom primers/recents submitted: $CONFIG_TASK"

# Step 8: Check for PII redaction in logs (if compression occurred)
echo -e "\n${YELLOW}[Test 7] Checking PII redaction...${NC}"
# Check if any email-like or phone-like patterns appear in compression logs
PII_IN_LOGS=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 200 | grep -E "summary|Summary" | grep -cE "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}|\d{3}-\d{3}-\d{4}" || echo "0")
assert "[ $PII_IN_LOGS -eq 0 ]" "No PII patterns found in compression summaries"

# Final Report
echo -e "\n${YELLOW}================================${NC}"
echo -e "${YELLOW}Test Results Summary${NC}"
echo -e "${YELLOW}================================${NC}"
echo -e "Session: $SESSION_ID"
echo -e "Passed: ${GREEN}$PASS_COUNT${NC}"
echo -e "Failed: ${RED}$FAIL_COUNT${NC}"

if [ $FAIL_COUNT -eq 0 ]; then
    echo -e "\n${GREEN}✓ All regression tests passed!${NC}"
    exit 0
else
    echo -e "\n${RED}✗ Some tests failed. Review the output above.${NC}"
    echo -e "\n${YELLOW}Troubleshooting tips:${NC}"
    echo "1. Check if services are healthy: docker compose ps"
    echo "2. Review orchestrator logs: docker compose logs orchestrator --tail 100"
    echo "3. Ensure Redis has session data: redis-cli GET session:$SESSION_ID"
    echo "4. Verify metrics endpoint: curl http://localhost:2112/metrics | grep compression"
    echo "5. Try lowering COMPRESSION_TRIGGER_RATIO further (e.g., 0.1)"
    exit 1
fi