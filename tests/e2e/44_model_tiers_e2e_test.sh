#!/bin/bash
# Comprehensive E2E Test Suite for Shannon Platform
set -e

GATEWAY="http://localhost:8080"
PASSED=0
FAILED=0
TOTAL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

RESULTS_FILE="/tmp/test_results.txt"
> "$RESULTS_FILE"

log_section() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

log_test() {
    ((TOTAL++))
    echo ""
    echo -e "${YELLOW}[TEST $TOTAL]${NC} $1"
}

log_pass() {
    ((PASSED++))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

log_fail() {
    ((FAILED++))
    echo -e "${RED}✗ FAIL${NC}: $1"
}

record_result() {
    echo "$1|$2" >> "$RESULTS_FILE"
}

wait_for_task() {
    local task_id="$1"
    local max_attempts=40
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        response=$(curl -sS "$GATEWAY/api/v1/tasks/$task_id" 2>/dev/null || echo "")
        status=$(echo "$response" | jq -r '.status' 2>/dev/null || echo "")

        if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
            echo "$response"
            return 0
        elif [ "$status" = "TASK_STATUS_FAILED" ]; then
            echo "$response"
            return 1
        fi
        sleep 2
        ((attempt++))
    done
    echo "$response"
    return 2
}

submit_task() {
    local query="$1"
    local model_override="$2"
    local provider_override="$3"
    local model_tier="$4"

    local payload="{\"query\": \"$query\""
    [ -n "$model_override" ] && payload="$payload, \"model_override\": \"$model_override\""
    [ -n "$provider_override" ] && payload="$payload, \"provider_override\": \"$provider_override\""
    [ -n "$model_tier" ] && payload="$payload, \"model_tier\": \"$model_tier\""
    payload="$payload}"

    curl -sS -X POST "$GATEWAY/api/v1/tasks" -H 'Content-Type: application/json' -d "$payload" 2>/dev/null
}

log_section "COMPREHENSIVE E2E TEST SUITE - $(date)"

# Test 1: OpenAI small tier (gpt-5-nano-2025-08-07)
log_test "OpenAI Small Tier (gpt-5-nano-2025-08-07) - Simple Math"
RESPONSE=$(submit_task "What is 15 + 27? Reply with just the number." "" "openai" "small")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "42"; then
        log_pass "OpenAI small tier: 15+27=42 ✓"
        record_result "OpenAI Small (gpt-5-nano)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 42, got: $RESULT"
        record_result "OpenAI Small (gpt-5-nano)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "OpenAI Small (gpt-5-nano)" "❌ FAIL - Submit failed"
fi

# Test 2: OpenAI medium tier (gpt-5-2025-08-07)
log_test "OpenAI Medium Tier (gpt-5-2025-08-07) - Multiplication"
RESPONSE=$(submit_task "Calculate 13 * 8. Respond with only the number." "" "openai" "medium")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "104"; then
        log_pass "OpenAI medium tier: 13*8=104 ✓"
        record_result "OpenAI Medium (gpt-5-2025-08-07)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 104, got: $RESULT"
        record_result "OpenAI Medium (gpt-5-2025-08-07)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "OpenAI Medium (gpt-5-2025-08-07)" "❌ FAIL - Submit failed"
fi

# Test 3: OpenAI large tier (gpt-4.1-2025-04-14)
log_test "OpenAI Large Tier (gpt-4.1-2025-04-14) - Reasoning"
RESPONSE=$(submit_task "Explain why a train traveling at 60 mph for 2.5 hours covers 150 miles, then verify this calculation is correct. Answer with 'yes' or 'no'." "" "openai" "large")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "yes"; then
        log_pass "OpenAI large tier: Reasoning verification ✓"
        record_result "OpenAI Large (gpt-4.1)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 'yes', got: $RESULT"
        record_result "OpenAI Large (gpt-4.1)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "OpenAI Large (gpt-4.1)" "❌ FAIL - Submit failed"
fi

# Test 4: Anthropic small tier (claude-haiku-4-5-20251001)
log_test "Anthropic Small Tier (claude-haiku-4-5-20251001) - Simple Math"
RESPONSE=$(submit_task "What is 25 + 18? Reply with just the number." "" "anthropic" "small")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "43"; then
        log_pass "Anthropic small tier: 25+18=43 ✓"
        record_result "Anthropic Small (claude-haiku)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 43, got: $RESULT"
        record_result "Anthropic Small (claude-haiku)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "Anthropic Small (claude-haiku)" "❌ FAIL - Submit failed"
fi

# Test 5: Anthropic medium tier (claude-sonnet-4-5-20250929)
log_test "Anthropic Medium Tier (claude-sonnet-4-5-20250929) - Reasoning with Math"
RESPONSE=$(submit_task "Consider a rectangular garden layout. If someone plants flowers in seventeen rows, and carefully places exactly six plants in each row, what would be the mathematical relationship between rows and total plants? Explain your reasoning and provide the total count." "" "anthropic" "medium")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -q "102"; then
        log_pass "Anthropic medium tier: 17 rows * 6 plants = 102 ✓"
        record_result "Anthropic Medium (claude-sonnet)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 102 in response, got: $RESULT"
        record_result "Anthropic Medium (claude-sonnet)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "Anthropic Medium (claude-sonnet)" "❌ FAIL - Submit failed"
fi

# Test 6: Anthropic large tier (claude-opus-4-1-20250805)
log_test "Anthropic Large Tier (claude-opus-4-1-20250805) - Complex Reasoning"
RESPONSE=$(submit_task "You're shopping and buy 3 items priced at 12.50 dollars each. The store applies a 20% discount to your total. Explain the calculation steps, then state the final amount you pay." "" "anthropic" "large")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -q "30"; then
        log_pass "Anthropic large tier: Shopping calculation = $30 ✓"
        record_result "Anthropic Large (claude-opus)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 30 in response, got: $RESULT"
        record_result "Anthropic Large (claude-opus)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "Anthropic Large (claude-opus)" "❌ FAIL - Submit failed"
fi

# Test 7: Direct model override (gpt-5-nano-2025-08-07)
log_test "Direct Model Override - gpt-5-nano-2025-08-07"
RESPONSE=$(submit_task "What is 9 * 7? Just the number." "gpt-5-nano-2025-08-07" "" "")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "63"; then
        log_pass "Direct model override: 9*7=63 ✓"
        record_result "Direct Override (gpt-5-nano)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 63, got: $RESULT"
        record_result "Direct Override (gpt-5-nano)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "Direct Override (gpt-5-nano)" "❌ FAIL - Submit failed"
fi

# Test 8: Direct model override (claude-sonnet-4-5-20250929)
log_test "Direct Model Override - claude-sonnet-4-5-20250929"
RESPONSE=$(submit_task "What is 8 * 9? Just the number." "claude-sonnet-4-5-20250929" "" "")
TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id')
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "null" ]; then
    FINAL=$(wait_for_task "$TASK_ID")
    STATUS=$(echo "$FINAL" | jq -r '.status')
    RESULT=$(echo "$FINAL" | jq -r '.result')
    if [ "$STATUS" = "TASK_STATUS_COMPLETED" ] && echo "$RESULT" | grep -qi "72"; then
        log_pass "Direct model override: 8*9=72 ✓"
        record_result "Direct Override (claude-sonnet)" "✅ PASS - Result: $RESULT"
    else
        log_fail "Expected 72, got: $RESULT"
        record_result "Direct Override (claude-sonnet)" "❌ FAIL - Got: $RESULT"
    fi
else
    log_fail "Failed to submit"
    record_result "Direct Override (claude-sonnet)" "❌ FAIL - Submit failed"
fi

log_section "TEST RESULTS SUMMARY"
echo ""
echo "Total Tests: $TOTAL"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${RED}Failed: $FAILED${NC}"
echo ""

log_section "DETAILED RESULTS"
cat "$RESULTS_FILE" | while IFS='|' read -r test_name result; do
    echo "  $test_name: $result"
done

if [ $FAILED -eq 0 ]; then
    echo ""
    log_section "✅ ALL TESTS PASSED!"
    exit 0
else
    echo ""
    log_section "⚠️  SOME TESTS FAILED"
    exit 1
fi
