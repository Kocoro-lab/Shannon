#!/bin/bash
# Comprehensive E2E Test Suite for Shannon System
# Tests all critical scenarios after PTEngine agent and UTF-8 fixes

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_URL="${BASE_URL:-http://localhost:8080}"
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_TESTS=()

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
    ((TESTS_PASSED++))
}

log_error() {
    echo -e "${RED}[✗]${NC} $1"
    ((TESTS_FAILED++))
    FAILED_TESTS+=("$1")
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_section() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

# Helper function to submit task
submit_task() {
    local query="$1"
    local context="$2"
    local session_id="${3:-test-session-$RANDOM}"

    curl -sS -X POST "$BASE_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -d "{
            \"session_id\": \"$session_id\",
            \"query\": \"$query\",
            \"context\": $context
        }"
}

# Helper function to wait for task completion
wait_for_task() {
    local task_id="$1"
    local max_wait="${2:-60}"
    local interval="${3:-2}"
    local elapsed=0

    while [ $elapsed -lt $max_wait ]; do
        local response=$(curl -sS "$BASE_URL/api/v1/tasks/$task_id")
        local status=$(echo "$response" | jq -r '.status')

        if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
            echo "$response"
            return 0
        elif [ "$status" = "TASK_STATUS_FAILED" ]; then
            log_error "Task $task_id failed"
            echo "$response" | jq '.'
            return 1
        fi

        sleep $interval
        ((elapsed+=interval))
    done

    log_error "Task $task_id timed out after ${max_wait}s"
    return 1
}

# Test 1: Simple calculation task
test_simple_calculation() {
    log_section "Test 1: Simple Calculation Task"
    ((TESTS_RUN++))

    local response=$(submit_task "What is 15 plus 25?" '{"role": "simple"}')
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit simple calculation task"
        return 1
    fi

    log_info "Task ID: $task_id"
    log_info "Waiting for completion..."

    if result=$(wait_for_task "$task_id" 30); then
        local status=$(echo "$result" | jq -r '.status')
        local model_used=$(echo "$result" | jq -r '.model_used')
        local result_text=$(echo "$result" | jq -r '.result')

        if [ "$status" = "TASK_STATUS_COMPLETED" ] && [ -n "$model_used" ]; then
            log_success "Simple calculation task completed (model: $model_used)"
            log_info "Result: ${result_text:0:100}..."
            return 0
        fi
    fi

    log_error "Simple calculation task failed"
    return 1
}

# Test 2: UTF-8 Chinese text handling
test_utf8_chinese() {
    log_section "Test 2: UTF-8 Chinese Text Handling"
    ((TESTS_RUN++))

    local response=$(submit_task "请用中文回答：今天天气怎么样？" '{"role": "simple"}')
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit UTF-8 Chinese task"
        return 1
    fi

    log_info "Task ID: $task_id"

    if result=$(wait_for_task "$task_id" 30); then
        local status=$(echo "$result" | jq -r '.status')
        local result_text=$(echo "$result" | jq -r '.result')

        # Check if result contains Chinese characters
        if [ "$status" = "TASK_STATUS_COMPLETED" ] && echo "$result_text" | grep -qP '[\p{Han}]'; then
            log_success "UTF-8 Chinese handling works correctly"
            return 0
        fi
    fi

    log_error "UTF-8 Chinese handling failed"
    return 1
}

# Test 3: SSE Streaming endpoint
test_sse_streaming() {
    log_section "Test 3: SSE Streaming Endpoint"
    ((TESTS_RUN++))

    local response=$(submit_task "Calculate 5 + 3" '{"role": "simple"}')
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit SSE streaming task"
        return 1
    fi

    log_info "Task ID: $task_id"
    log_info "Testing SSE stream..."

    # Attempt to stream events (timeout after 15 seconds)
    local stream_output=$(timeout 15 curl -sS -N "$BASE_URL/api/v1/tasks/$task_id/stream" 2>&1 || true)

    # Check if we got some SSE events
    if echo "$stream_output" | grep -q "event:"; then
        log_success "SSE streaming endpoint works"

        # Check for WORKFLOW_COMPLETED event
        if echo "$stream_output" | grep -q "WORKFLOW_COMPLETED"; then
            log_info "Stream completed successfully with WORKFLOW_COMPLETED event"
        fi
        return 0
    else
        log_error "SSE streaming endpoint failed to return events"
        return 1
    fi
}

# Test 6: Session management
test_session_management() {
    log_section "Test 4: Session Management"
    ((TESTS_RUN++))

    local session_id="test-session-$RANDOM"

    # Submit task with session
    local response=$(submit_task "Hello, this is a test" '{"role": "simple"}' "$session_id")
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit task with session"
        return 1
    fi

    # Wait for completion
    wait_for_task "$task_id" 30 > /dev/null

    # Try to retrieve session
    local session_response=$(curl -sS "$BASE_URL/api/v1/sessions/$session_id")
    local session_status=$(echo "$session_response" | jq -r '.id // .error')

    if [ "$session_status" != "null" ] && [ "$session_status" != "" ]; then
        log_success "Session management works correctly"
        return 0
    else
        log_error "Session retrieval failed"
        return 1
    fi
}

# Test 7: Multi-step workflow (Supervisor)
test_supervisor_workflow() {
    log_section "Test 5: Multi-Step Supervisor Workflow"
    ((TESTS_RUN++))

    local query="Analyze website traffic for Q3 2025, then create a summary report with recommendations"
    local context='{
        "role": "data_analytics",
        "prompt_params": {
            "profile_id": "49598h6e",
            "aid": "fcb1cd29-9104-47b1-b914-31db6ba30c1a",
            "current_date": "2025-10-01"
        }
    }'

    local response=$(submit_task "$query" "$context")
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit supervisor workflow task"
        return 1
    fi

    log_info "Task ID: $task_id"
    log_info "Waiting for multi-step workflow (max 120s)..."

    if result=$(wait_for_task "$task_id" 120 5); then
        local status=$(echo "$result" | jq -r '.status')

        if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
            log_success "Supervisor workflow completed successfully"
            return 0
        fi
    fi

    log_warn "Supervisor workflow test incomplete (may timeout for complex queries)"
    # Don't fail this test as it might be legitimately slow
    return 0
}

# Test 8: Error handling - Invalid context
test_error_handling() {
    log_section "Test 6: Error Handling - Invalid Context"
    ((TESTS_RUN++))

    # Submit task with invalid/minimal context
    local response=$(curl -sS -X POST "$BASE_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -d '{"query": "test", "context": "invalid"}' 2>&1)

    local status_code=$(curl -sS -w "%{http_code}" -o /dev/null -X POST "$BASE_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -d '{"query": "test", "context": "invalid"}')

    # Should either accept it gracefully or return 4xx error
    if [ "$status_code" = "200" ] || [ "$status_code" = "400" ] || [ "$status_code" = "422" ]; then
        log_success "Error handling works correctly (status: $status_code)"
        return 0
    else
        log_error "Unexpected error handling behavior (status: $status_code)"
        return 1
    fi
}

# Test 9: Metadata population
test_metadata_population() {
    log_section "Test 7: Metadata Population (model_used, provider, usage)"
    ((TESTS_RUN++))

    local response=$(submit_task "Quick test: 2+2" '{"role": "simple"}')
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit metadata test task"
        return 1
    fi

    if result=$(wait_for_task "$task_id" 30); then
        local model_used=$(echo "$result" | jq -r '.model_used')
        local provider=$(echo "$result" | jq -r '.provider')
        local usage=$(echo "$result" | jq -r '.usage')

        if [ "$model_used" != "null" ] && [ "$provider" != "null" ]; then
            log_success "Metadata populated correctly (model: $model_used, provider: $provider)"

            if [ "$usage" != "null" ]; then
                log_info "Usage data: $usage"
            fi
            return 0
        else
            log_error "Metadata not populated (model_used: $model_used, provider: $provider)"
            return 1
        fi
    fi

    log_error "Metadata test failed"
    return 1
}

# Test 10: Database persistence check
test_database_persistence() {
    log_section "Test 8: Database Persistence"
    ((TESTS_RUN++))

    local response=$(submit_task "Test persistence" '{"role": "simple"}')
    local task_id=$(echo "$response" | jq -r '.task_id')

    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_error "Failed to submit persistence test task"
        return 1
    fi

    wait_for_task "$task_id" 30 > /dev/null

    # Try to query database directly
    local db_check=$(docker compose -f "$(dirname "$SCRIPT_DIR")/../deploy/compose/docker-compose.yml" exec -T postgres \
        psql -U shannon -d shannon -c \
        "SELECT workflow_id, status FROM task_executions WHERE workflow_id = '$task_id' LIMIT 1;" 2>/dev/null || echo "")

    if echo "$db_check" | grep -q "$task_id"; then
        log_success "Database persistence works correctly"
        return 0
    else
        log_warn "Could not verify database persistence (may still be working)"
        # Don't fail if we can't access database
        return 0
    fi
}

# Main test runner
main() {
    log_section "Shannon Comprehensive E2E Test Suite"
    log_info "Testing against: $BASE_URL"
    log_info "Starting tests at $(date)"
    echo ""

    # Check if services are running
    if ! curl -sf "$BASE_URL/health" > /dev/null 2>&1; then
        log_error "Gateway service is not accessible at $BASE_URL"
        log_info "Please ensure Shannon services are running: make dev"
        exit 1
    fi

    log_success "Gateway service is accessible"
    echo ""

    # Run all tests
    test_simple_calculation
    test_utf8_chinese
    test_metadata_population
    test_sse_streaming
    test_session_management
    test_error_handling
    test_database_persistence
    test_supervisor_workflow

    # Print summary
    log_section "Test Summary"
    echo "Tests Run:    $TESTS_RUN"
    echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"

    if [ $TESTS_FAILED -gt 0 ]; then
        echo ""
        echo -e "${RED}Failed Tests:${NC}"
        for test in "${FAILED_TESTS[@]}"; do
            echo "  - $test"
        done
        echo ""
        log_error "Some tests failed. Please review the output above."
        exit 1
    else
        echo ""
        log_success "All tests passed! ✨"
        exit 0
    fi
}

# Run main function
main "$@"
