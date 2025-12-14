#!/bin/bash
# E2E tests for pause/resume/cancel across all workflow types
# Don't set -e since we handle errors in individual tests

TEMPORAL_CLI="docker compose -f deploy/compose/docker-compose.yml exec -T temporal temporal"
GRPC_ADDR="localhost:50052"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

passed=0
failed=0

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((passed++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((failed++))
}

log_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

# Submit a task and return the workflow ID
submit_task() {
    local query="$1"
    local context="$2"

    if [ -z "$context" ]; then
        context='{}'
    fi

    local session_id="test-session-$(date +%s)"
    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {
            \"user_id\": \"00000000-0000-0000-0000-000000000002\",
            \"session_id\": \"$session_id\"
        },
        \"query\": \"$query\",
        \"context\": $context
    }" $GRPC_ADDR shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    echo "$response" | grep -o '"workflowId": "[^"]*"' | cut -d'"' -f4
}

# Send pause signal
send_pause() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name pause_v1 \
        --input '{"reason": "e2e test", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "succeeded"
}

# Send resume signal
send_resume() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name resume_v1 \
        --input '{"reason": "e2e resume", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "succeeded"
}

# Send cancel signal
send_cancel() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name cancel_v1 \
        --input '{"reason": "e2e cancel", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "succeeded"
}

# Get control state
get_control_state() {
    local task_id="$1"
    $TEMPORAL_CLI workflow query --workflow-id "$task_id" --type control_state_v1 \
        --address temporal:7233 2>&1 | grep -o 'QueryResult.*' | sed 's/QueryResult  //'
}

# Get task status
get_task_status() {
    local task_id="$1"
    grpcurl -plaintext -d "{\"task_id\": \"$task_id\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 | \
        grep -o '"status": "[^"]*"' | cut -d'"' -f4
}

# Check if workflow is paused
is_paused() {
    local task_id="$1"
    local state=$(get_control_state "$task_id")
    echo "$state" | grep -q '"is_paused":true'
}

# Wait for task to reach a status
wait_for_status() {
    local task_id="$1"
    local expected="$2"
    local max_attempts="${3:-30}"

    for i in $(seq 1 $max_attempts); do
        local status=$(get_task_status "$task_id")
        if [ "$status" = "$expected" ]; then
            return 0
        fi
        sleep 2
    done
    return 1
}

# ============================================
# Test 1: Simple Query - Pause/Resume
# ============================================
test_simple_pause_resume() {
    log_info "Test 1: Simple Query - Pause/Resume"

    local task_id=$(submit_task "What is 2+2?")
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit simple task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 1

    # Try to pause (may complete before pause)
    if send_pause "$task_id"; then
        sleep 2
        if is_paused "$task_id"; then
            log_info "Paused successfully"

            # Resume
            if send_resume "$task_id"; then
                sleep 2
                if ! is_paused "$task_id"; then
                    log_pass "Simple task pause/resume works"
                    return 0
                fi
            fi
        fi
    fi

    # Task may have completed too fast - that's OK for simple queries
    local status=$(get_task_status "$task_id")
    if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
        log_pass "Simple task completed (too fast to pause - OK)"
        return 0
    fi

    log_fail "Simple task pause/resume failed"
    return 1
}

# ============================================
# Test 2: Research Workflow - Pause/Resume
# ============================================
test_research_pause_resume() {
    log_info "Test 2: Research Workflow - Pause/Resume"

    local task_id=$(submit_task "Research the history of quantum computing" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit research task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    if send_pause "$task_id"; then
        sleep 3
        if is_paused "$task_id"; then
            log_info "Research workflow paused"

            # Verify it stays paused
            sleep 5
            if is_paused "$task_id"; then
                log_info "Still paused after 5s"

                # Resume (don't wait for completion - just verify it resumed)
                if send_resume "$task_id"; then
                    sleep 3
                    if ! is_paused "$task_id"; then
                        log_pass "Research workflow pause/resume works"
                        return 0
                    fi
                fi
            fi
        fi
    fi

    log_fail "Research workflow pause/resume failed"
    return 1
}

# ============================================
# Test 3: Supervisor Workflow - Pause/Resume
# ============================================
test_supervisor_pause_resume() {
    log_info "Test 3: Supervisor Workflow (complex task) - Pause/Resume"

    # Complex query that triggers SupervisorWorkflow (>5 subtasks)
    local task_id=$(submit_task "Compare and contrast 6 different programming languages: Python, JavaScript, Go, Rust, Java, and C++. For each, describe syntax, use cases, performance, and ecosystem.")
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit supervisor task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    if send_pause "$task_id"; then
        sleep 3
        if is_paused "$task_id"; then
            log_info "Supervisor workflow paused"

            # Resume
            if send_resume "$task_id"; then
                sleep 3
                if ! is_paused "$task_id"; then
                    log_pass "Supervisor workflow pause/resume works"
                    return 0
                fi
            fi
        fi
    fi

    log_fail "Supervisor workflow pause/resume failed"
    return 1
}

# ============================================
# Test 4: Cancel - Simple Query
# ============================================
test_simple_cancel() {
    log_info "Test 4: Simple Query - Cancel"

    local task_id=$(submit_task "Explain machine learning in great detail with many examples")
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit task for cancel"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 2

    if send_cancel "$task_id"; then
        sleep 5
        local status=$(get_task_status "$task_id")
        if [ "$status" = "TASK_STATUS_CANCELLED" ]; then
            log_pass "Simple task cancellation works - status CANCELLED"
            return 0
        else
            log_fail "Expected CANCELLED, got $status"
            return 1
        fi
    fi

    log_fail "Cancel signal failed"
    return 1
}

# ============================================
# Test 5: Cancel - Research Workflow
# ============================================
test_research_cancel() {
    log_info "Test 5: Research Workflow - Cancel"

    local task_id=$(submit_task "Deep dive into blockchain technology and its applications" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit research task for cancel"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    if send_cancel "$task_id"; then
        # Research workflows take longer to process cancellation
        if wait_for_status "$task_id" "TASK_STATUS_CANCELLED" 30; then
            log_pass "Research workflow cancellation works - status CANCELLED"
            return 0
        else
            local status=$(get_task_status "$task_id")
            log_fail "Expected CANCELLED, got $status"
            return 1
        fi
    fi

    log_fail "Cancel signal failed"
    return 1
}

# ============================================
# Test 6: Cancel While Paused
# ============================================
test_cancel_while_paused() {
    log_info "Test 6: Cancel While Paused"

    local task_id=$(submit_task "Analyze the evolution of artificial intelligence" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    # First pause
    if send_pause "$task_id"; then
        sleep 3
        if is_paused "$task_id"; then
            log_info "Paused - now cancelling"

            # Then cancel while paused
            if send_cancel "$task_id"; then
                # Wait for cancellation to propagate
                if wait_for_status "$task_id" "TASK_STATUS_CANCELLED" 30; then
                    log_pass "Cancel while paused works - status CANCELLED"
                    return 0
                else
                    local status=$(get_task_status "$task_id")
                    log_fail "Expected CANCELLED, got $status"
                    return 1
                fi
            fi
        fi
    fi

    log_fail "Cancel while paused failed"
    return 1
}

# ============================================
# Test 7: Multiple Pause/Resume Cycles
# ============================================
test_multiple_pause_resume() {
    log_info "Test 7: Multiple Pause/Resume Cycles"

    local task_id=$(submit_task "Research renewable energy sources comprehensively" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit task"
        return 1
    fi
    log_info "Submitted: $task_id"

    local cycles=0
    for i in 1 2 3; do
        sleep 3

        if send_pause "$task_id"; then
            sleep 2
            if is_paused "$task_id"; then
                log_info "Cycle $i: Paused"

                if send_resume "$task_id"; then
                    sleep 2
                    if ! is_paused "$task_id"; then
                        log_info "Cycle $i: Resumed"
                        ((cycles++))
                    fi
                fi
            fi
        else
            # Task may have completed
            local status=$(get_task_status "$task_id")
            if [ "$status" = "TASK_STATUS_COMPLETED" ]; then
                break
            fi
        fi
    done

    if [ $cycles -ge 1 ]; then
        log_pass "Multiple pause/resume cycles work ($cycles cycles)"
        return 0
    fi

    log_fail "Multiple pause/resume failed"
    return 1
}

# ============================================
# Main
# ============================================
echo "========================================"
echo "Control Signals E2E Tests"
echo "========================================"
echo ""

# Run all tests
test_simple_pause_resume
test_research_pause_resume
test_supervisor_pause_resume
test_simple_cancel
test_research_cancel
test_cancel_while_paused
test_multiple_pause_resume

echo ""
echo "========================================"
echo "Results: $passed passed, $failed failed"
echo "========================================"

if [ $failed -gt 0 ]; then
    exit 1
fi
exit 0
