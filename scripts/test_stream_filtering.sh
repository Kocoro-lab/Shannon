#!/bin/bash
# Smoke test for SSE event type filtering
# Tests that gateway allows filtering by event types

set -e

BASE_URL="${SHANNON_BASE_URL:-http://localhost:8080}"

echo "=========================================="
echo "SSE Event Type Filtering Smoke Test"
echo "=========================================="

# Submit a simple task
echo "1. Submitting test task..."
RESPONSE=$(curl -sS -X POST "${BASE_URL}/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d '{"query": "What is 2+2?"}')

TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
echo "   Task ID: ${TASK_ID}"

# Wait for task to complete
echo ""
echo "2. Waiting for task completion..."
sleep 5

# Test 1: Stream without filter (should get all events)
echo ""
echo "3. Testing SSE stream WITHOUT filter..."
ALL_EVENTS=$(timeout 5 curl -N -sS "${BASE_URL}/api/v1/stream/sse?workflow_id=${TASK_ID}" 2>/dev/null | grep "^event:" | wc -l || echo "0")
echo "   Received ${ALL_EVENTS} events (unfiltered)"

if [ "$ALL_EVENTS" -lt 5 ]; then
    echo "   ❌ FAIL: Expected at least 5 events, got ${ALL_EVENTS}"
    exit 1
fi
echo "   ✅ PASS: Unfiltered stream works"

# Test 2: Stream with LLM_OUTPUT filter
echo ""
echo "4. Testing SSE stream WITH filter (LLM_OUTPUT)..."
LLM_EVENTS=$(timeout 5 curl -N -sS "${BASE_URL}/api/v1/stream/sse?workflow_id=${TASK_ID}&types=LLM_OUTPUT" 2>/dev/null | grep "^event:" | wc -l || echo "0")
echo "   Received ${LLM_EVENTS} LLM_OUTPUT events"

if [ "$LLM_EVENTS" -lt 1 ]; then
    echo "   ❌ FAIL: Expected at least 1 LLM_OUTPUT event, got ${LLM_EVENTS}"
    exit 1
fi
echo "   ✅ PASS: LLM_OUTPUT filter works"

# Test 3: Stream with multiple filters
echo ""
echo "5. Testing SSE stream WITH filter (LLM_OUTPUT,WORKFLOW_COMPLETED)..."
MULTI_EVENTS=$(timeout 5 curl -N -sS "${BASE_URL}/api/v1/stream/sse?workflow_id=${TASK_ID}&types=LLM_OUTPUT,WORKFLOW_COMPLETED" 2>/dev/null | grep "^event:" | wc -l || echo "0")
echo "   Received ${MULTI_EVENTS} filtered events"

if [ "$MULTI_EVENTS" -lt 2 ]; then
    echo "   ❌ FAIL: Expected at least 2 events (LLM_OUTPUT + WORKFLOW_COMPLETED), got ${MULTI_EVENTS}"
    exit 1
fi
echo "   ✅ PASS: Multi-type filter works"

# Test 4: Verify no gateway validation error
echo ""
echo "6. Testing gateway doesn't reject valid event types..."
ERROR_RESPONSE=$(curl -sS "${BASE_URL}/api/v1/stream/sse?workflow_id=${TASK_ID}&types=LLM_OUTPUT,WORKFLOW_COMPLETED" 2>/dev/null | head -1)

if echo "$ERROR_RESPONSE" | grep -q "Invalid event type"; then
    echo "   ❌ FAIL: Gateway still rejecting valid event types"
    echo "   Response: ${ERROR_RESPONSE}"
    exit 1
fi
echo "   ✅ PASS: Gateway accepts event type filters"

# Test 5: Test Redis stream ID in last_event_id parameter
echo ""
echo "7. Testing Redis stream ID in last_event_id parameter..."
REDIS_ID_RESPONSE=$(curl -sS "${BASE_URL}/api/v1/stream/sse?workflow_id=${TASK_ID}&last_event_id=1700000000000-0" 2>/dev/null | head -1)

if echo "$REDIS_ID_RESPONSE" | grep -q "Invalid last_event_id"; then
    echo "   ❌ FAIL: Gateway rejecting Redis stream IDs in last_event_id"
    echo "   Response: ${REDIS_ID_RESPONSE}"
    exit 1
fi
echo "   ✅ PASS: Gateway accepts Redis stream IDs"

echo ""
echo "=========================================="
echo "✅ All event filtering tests passed!"
echo "=========================================="
