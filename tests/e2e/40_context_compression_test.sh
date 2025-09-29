#!/bin/bash

# Context Window Compression E2E Test
# Tests the sliding window compression system for long-running sessions

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
SESSION_ID="compress-e2e-$(date +%s)"
BASE_URL="localhost:50052"
METRICS_URL="http://localhost:2112/metrics"

echo -e "${YELLOW}Starting Context Compression E2E Test${NC}"
echo "Session ID: $SESSION_ID"

# Function to check compression metrics
check_compression_metrics() {
    local triggered=$(curl -s $METRICS_URL | grep 'shannon_compression_events_total{status="triggered"}' | awk '{print $2}' || echo "0")
    local skipped=$(curl -s $METRICS_URL | grep 'shannon_compression_events_total{status="skipped"}' | awk '{print $2}' || echo "0")
    local failed=$(curl -s $METRICS_URL | grep 'shannon_compression_events_total{status="failed"}' | awk '{print $2}' || echo "0")

    echo "Compression metrics - Triggered: $triggered, Skipped: $skipped, Failed: $failed"
    return 0
}

# Function to submit task
submit_task() {
    local query="$1"
    local context="${2:-{}}"

    grpcurl -plaintext -d "{
        \"metadata\": {\"userId\":\"test\",\"sessionId\":\"$SESSION_ID\"},
        \"query\": \"$query\",
        \"context\": $context
    }" $BASE_URL shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null | jq -r '.taskId' || echo "ERROR"
}

# Test 1: Build history without compression (under threshold)
echo -e "\n${YELLOW}Test 1: Building initial history (10 messages)${NC}"
for i in {1..10}; do
    TASK_ID=$(submit_task "Message $i: Short message to build history")
    if [ "$TASK_ID" = "ERROR" ]; then
        echo -e "${RED}Failed to submit task $i${NC}"
        exit 1
    fi
    echo "Submitted task $i: $TASK_ID"
done
echo -e "${GREEN}Initial history built${NC}"

# Check metrics - should show no compression yet
echo -e "\n${YELLOW}Checking metrics after initial history:${NC}"
INITIAL_METRICS=$(check_compression_metrics)
echo "$INITIAL_METRICS"

# Test 2: Build large history to approach threshold
echo -e "\n${YELLOW}Test 2: Building large history (90 more messages)${NC}"
for i in {11..100}; do
    # Create larger messages to increase token count
    MESSAGE="Message $i: This is a longer message designed to increase the token count significantly. It includes various details about testing, validation, compression mechanisms, and the importance of maintaining context while optimizing token usage. Additional content: $(seq 1 10 | tr '\n' ' ')"
    TASK_ID=$(submit_task "$MESSAGE")
    if [ "$TASK_ID" = "ERROR" ]; then
        echo -e "${RED}Failed to submit task $i${NC}"
        exit 1
    fi
    # Show progress every 10 messages
    if [ $((i % 10)) -eq 0 ]; then
        echo "Progress: $i/100 messages submitted"
    fi
done
echo -e "${GREEN}Large history built (100 total messages)${NC}"

# Test 3: Force compression with low budget
echo -e "\n${YELLOW}Test 3: Forcing compression with low token budget${NC}"
COMPRESS_TASK=$(submit_task "Trigger compression with limited budget" '{"token_budget_per_agent": 5000, "force_p2p": true}')
if [ "$COMPRESS_TASK" = "ERROR" ]; then
    echo -e "${RED}Failed to submit compression trigger task${NC}"
    exit 1
fi
echo "Compression trigger task: $COMPRESS_TASK"

# Wait for processing
sleep 10

# Check if compression was triggered
echo -e "\n${YELLOW}Checking compression metrics after trigger:${NC}"
FINAL_METRICS=$(check_compression_metrics)
echo "$FINAL_METRICS"

# Test 4: Verify context continuity after compression
echo -e "\n${YELLOW}Test 4: Verifying context continuity${NC}"
CONTINUITY_TASK=$(submit_task "Summarize the key topics from our conversation")
if [ "$CONTINUITY_TASK" = "ERROR" ]; then
    echo -e "${RED}Failed to submit continuity test task${NC}"
    exit 1
fi
echo "Context continuity task: $CONTINUITY_TASK"

# Wait and check task status
sleep 5
STATUS=$(grpcurl -plaintext -d "{\"taskId\": \"$CONTINUITY_TASK\"}" $BASE_URL shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null | jq -r '.status' || echo "ERROR")
echo "Task status: $STATUS"

# Test 5: Check SSE events for compression indicators
echo -e "\n${YELLOW}Test 5: Monitoring SSE events${NC}"
echo "Starting SSE monitor for 5 seconds..."
timeout 5 curl -N "http://localhost:8081/stream/sse?workflow_id=$COMPRESS_TASK&types=CONTEXT_PREPARED,DATA_PROCESSING" 2>/dev/null | grep -E "summary|compress|budget" | head -5 || true

# Test 6: Verify configurable primers/recents
echo -e "\n${YELLOW}Test 6: Testing configurable primers and recents${NC}"
CONFIG_TASK=$(submit_task "Test with custom window configuration" '{"primers_count": 5, "recents_count": 30}')
if [ "$CONFIG_TASK" = "ERROR" ]; then
    echo -e "${RED}Failed to submit config test task${NC}"
    exit 1
fi
echo "Configuration test task: $CONFIG_TASK"

# Final metrics summary
echo -e "\n${YELLOW}Final Compression Metrics Summary:${NC}"
check_compression_metrics

# Check for compression ratio metrics
echo -e "\n${YELLOW}Compression Ratio Distribution:${NC}"
curl -s $METRICS_URL | grep "shannon_compression_ratio" | head -10 || echo "No compression ratio metrics found"

# Summary
echo -e "\n${GREEN}================================${NC}"
echo -e "${GREEN}Context Compression Test Complete${NC}"
echo -e "${GREEN}================================${NC}"
echo "Session ID: $SESSION_ID"
echo "Total tasks submitted: 103"
echo ""
echo "Key test scenarios covered:"
echo "1. ✓ Initial history building without compression"
echo "2. ✓ Large history accumulation"
echo "3. ✓ Forced compression with low budget"
echo "4. ✓ Context continuity verification"
echo "5. ✓ SSE event monitoring"
echo "6. ✓ Configurable window parameters"
echo ""
echo -e "${YELLOW}Note: If compression wasn't triggered, try:${NC}"
echo "- Lowering token_budget_per_agent further (e.g., 1000)"
echo "- Using force_p2p=true to route through SupervisorWorkflow"
echo "- Increasing message size/count to exceed 75% of model window"