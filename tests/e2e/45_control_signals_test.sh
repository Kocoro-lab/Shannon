#!/usr/bin/env bash
# Control Signals E2E Test
# Tests pause/resume/cancel functionality with DB status verification
# set -e disabled to allow individual test error handling

GRPC_ADDR="localhost:50052"
GATEWAY="http://localhost:8080"
TEMPORAL_CLI="docker compose -f deploy/compose/docker-compose.yml exec -T temporal temporal"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PASSED=0
FAILED=0

log_section() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

log_pass() {
    ((PASSED++))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

log_fail() {
    ((FAILED++))
    echo -e "${RED}✗ FAIL${NC}: $1"
}

log_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

# Submit a task via gRPC
submit_task() {
    local query="$1"
    local context="$2"
    if [ -z "$context" ]; then
        context='{}'
    fi
    local session_id="control-test-$(date +%s)-$RANDOM"

    local payload="{
        \"metadata\": {
            \"user_id\": \"00000000-0000-0000-0000-000000000002\",
            \"session_id\": \"$session_id\"
        },
        \"query\": \"$query\",
        \"context\": $context
    }"

    local response=$(grpcurl -plaintext -d "$payload" $GRPC_ADDR shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    # Try both workflowId (old) and workflow_id (new) formats
    local wf_id=$(echo "$response" | grep -oE '"(workflowId|workflow_id)": "[^"]*"' | head -1 | cut -d'"' -f4)
    echo "$wf_id"
}

# Get task status from gRPC
get_task_status() {
    local task_id="$1"
    grpcurl -plaintext -d "{\"task_id\": \"$task_id\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 | \
        grep -o '"status": "[^"]*"' | cut -d'"' -f4
}

# Get task status from DB
get_db_status() {
    local task_id="$1"
    PGPASSWORD=shannon psql -h localhost -U shannon -d shannon -t -c \
        "SELECT status FROM task_executions WHERE workflow_id = '$task_id';" 2>/dev/null | tr -d ' \n'
}

# Send pause signal via gRPC (updates DB)
send_pause_grpc() {
    local task_id="$1"
    local response=$(grpcurl -plaintext -d "{\"task_id\": \"$task_id\", \"reason\": \"e2e test\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/PauseTask 2>&1)
    echo "$response" | grep -q '"success": true'
}

# Send resume signal via gRPC (updates DB)
send_resume_grpc() {
    local task_id="$1"
    local response=$(grpcurl -plaintext -d "{\"task_id\": \"$task_id\", \"reason\": \"e2e resume\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/ResumeTask 2>&1)
    echo "$response" | grep -q '"success": true'
}

# Send cancel signal via gRPC (updates DB)
send_cancel_grpc() {
    local task_id="$1"
    local response=$(grpcurl -plaintext -d "{\"task_id\": \"$task_id\", \"reason\": \"e2e cancel\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/CancelTask 2>&1)
    echo "$response" | grep -q '"success": true'
}

# Get control state via gRPC
get_control_state_grpc() {
    local task_id="$1"
    grpcurl -plaintext -d "{\"task_id\": \"$task_id\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/GetControlState 2>&1
}

# Send pause signal via Temporal (direct, no DB update)
send_pause() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name pause_v1 \
        --input '{"reason": "e2e test", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "Signal workflow succeeded"
}

# Send resume signal via Temporal (direct, no DB update)
send_resume() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name resume_v1 \
        --input '{"reason": "e2e resume", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "Signal workflow succeeded"
}

# Send cancel signal via Temporal (direct, no DB update)
send_cancel() {
    local task_id="$1"
    $TEMPORAL_CLI workflow signal --workflow-id "$task_id" --name cancel_v1 \
        --input '{"reason": "e2e cancel", "requested_by": "test"}' \
        --address temporal:7233 2>&1 | grep -q "Signal workflow succeeded"
}

# Get control state via Temporal query
get_control_state() {
    local task_id="$1"
    $TEMPORAL_CLI workflow query --workflow-id "$task_id" --type control_state_v1 \
        --address temporal:7233 2>&1 | grep -o 'QueryResult.*' | sed 's/QueryResult  //'
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
# Test 1: Pause/Resume with DB Status
# ============================================
test_pause_resume_db_status() {
    log_section "Test 1: Pause/Resume with DB Status Verification"

    local task_id=$(submit_task "Research quantum computing history in detail" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit research task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    # Check initial DB status
    local initial_status=$(get_db_status "$task_id")
    log_info "Initial DB status: $initial_status"

    # Send pause via gRPC (updates DB)
    if send_pause_grpc "$task_id"; then
        log_info "Pause signal sent via gRPC"
        sleep 2

        # Verify DB status is PAUSED
        local paused_status=$(get_db_status "$task_id")
        if [ "$paused_status" = "PAUSED" ]; then
            log_info "DB status updated to PAUSED"

            # Verify control state via Temporal
            if is_paused "$task_id"; then
                log_info "Workflow control state is paused"

                # Send resume via gRPC (updates DB)
                if send_resume_grpc "$task_id"; then
                    sleep 2

                    # Verify DB status is RUNNING
                    local resumed_status=$(get_db_status "$task_id")
                    if [ "$resumed_status" = "RUNNING" ]; then
                        log_pass "Pause/Resume with DB status works correctly"
                        return 0
                    else
                        log_fail "DB status after resume is '$resumed_status', expected 'RUNNING'"
                    fi
                fi
            fi
        else
            log_fail "DB status is '$paused_status', expected 'PAUSED'"
        fi
    fi

    log_fail "Pause/Resume DB status test failed"
    return 1
}

# ============================================
# Test 2: Cancel with DB Status
# ============================================
test_cancel_db_status() {
    log_section "Test 2: Cancel with DB Status Verification"

    local task_id=$(submit_task "Analyze the evolution of artificial intelligence comprehensively" '{"force_research": true}')
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit task"
        return 1
    fi
    log_info "Submitted: $task_id"

    sleep 3

    # Send cancel
    if send_cancel "$task_id"; then
        log_info "Cancel signal sent"

        # Wait for workflow to complete with CANCELLED status
        if wait_for_status "$task_id" "TASK_STATUS_CANCELLED" 30; then
            # Verify DB status
            local db_status=$(get_db_status "$task_id")
            if [ "$db_status" = "CANCELLED" ]; then
                log_pass "Cancel with DB status works - status is CANCELLED"
                return 0
            else
                log_fail "DB status is '$db_status', expected 'CANCELLED'"
            fi
        else
            local final_status=$(get_task_status "$task_id")
            log_fail "Task status is '$final_status', expected 'TASK_STATUS_CANCELLED'"
        fi
    fi

    log_fail "Cancel DB status test failed"
    return 1
}

# ============================================
# Test 3: Cancel While Paused
# ============================================
test_cancel_while_paused() {
    log_section "Test 3: Cancel While Paused"

    local task_id=$(submit_task "Deep dive into blockchain technology and its applications" '{"force_research": true}')
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
            log_info "Workflow paused - now cancelling"

            # Then cancel while paused
            if send_cancel "$task_id"; then
                if wait_for_status "$task_id" "TASK_STATUS_CANCELLED" 30; then
                    local db_status=$(get_db_status "$task_id")
                    if [ "$db_status" = "CANCELLED" ]; then
                        log_pass "Cancel while paused works - DB status is CANCELLED"
                        return 0
                    else
                        log_fail "DB status is '$db_status', expected 'CANCELLED'"
                    fi
                fi
            fi
        fi
    fi

    log_fail "Cancel while paused test failed"
    return 1
}

# ============================================
# Test 4: HTTP API Control Endpoints
# ============================================
test_http_api_control() {
    log_section "Test 4: HTTP API Control Endpoints"

    # Submit via HTTP
    local response=$(curl -sS -X POST "$GATEWAY/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -H "X-User-ID: 00000000-0000-0000-0000-000000000002" \
        -d '{
            "query": "Research renewable energy sources comprehensively",
            "context": {"force_research": true}
        }' 2>&1)

    local task_id=$(echo "$response" | jq -r '.task_id // .workflow_id' 2>/dev/null)
    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        log_fail "Failed to submit task via HTTP API"
        return 1
    fi
    log_info "Submitted via HTTP: $task_id"

    sleep 3

    # Test pause via HTTP
    local pause_response=$(curl -sS -X POST "$GATEWAY/api/v1/tasks/$task_id/pause" \
        -H "Content-Type: application/json" \
        -H "X-User-ID: 00000000-0000-0000-0000-000000000002" \
        -d '{"reason": "http api test"}' 2>&1)

    local pause_success=$(echo "$pause_response" | jq -r '.success' 2>/dev/null)
    if [ "$pause_success" = "true" ]; then
        log_info "HTTP pause succeeded"
        sleep 2

        # Test get control state via HTTP
        local state_response=$(curl -sS "$GATEWAY/api/v1/tasks/$task_id/control-state" \
            -H "X-User-ID: 00000000-0000-0000-0000-000000000002" 2>&1)

        local is_paused=$(echo "$state_response" | jq -r '.is_paused' 2>/dev/null)
        if [ "$is_paused" = "true" ]; then
            log_info "HTTP control state shows paused"

            # Test resume via HTTP
            local resume_response=$(curl -sS -X POST "$GATEWAY/api/v1/tasks/$task_id/resume" \
                -H "Content-Type: application/json" \
                -H "X-User-ID: 00000000-0000-0000-0000-000000000002" \
                -d '{"reason": "http resume test"}' 2>&1)

            local resume_success=$(echo "$resume_response" | jq -r '.success' 2>/dev/null)
            if [ "$resume_success" = "true" ]; then
                log_pass "HTTP API control endpoints work correctly"

                # Cancel to cleanup
                curl -sS -X POST "$GATEWAY/api/v1/tasks/$task_id/cancel" \
                    -H "Content-Type: application/json" \
                    -H "X-User-ID: 00000000-0000-0000-0000-000000000002" \
                    -d '{"reason": "cleanup"}' >/dev/null 2>&1 || true

                return 0
            fi
        fi
    fi

    log_fail "HTTP API control test failed"
    return 1
}

# ============================================
# Test 5: Simple Task - Completes Before Pause
# ============================================
test_simple_task_fast_completion() {
    log_section "Test 5: Simple Task (Fast Completion)"

    local task_id=$(submit_task "What is 2+2?")
    if [ -z "$task_id" ]; then
        log_fail "Failed to submit simple task"
        return 1
    fi
    log_info "Submitted: $task_id"

    # Wait for simple task to complete (usually fast)
    if wait_for_status "$task_id" "TASK_STATUS_COMPLETED" 30; then
        log_pass "Simple task completed successfully"
        return 0
    fi

    # Check current status - it might be paused or still running
    local status=$(get_task_status "$task_id")
    if [ "$status" = "TASK_STATUS_RUNNING" ]; then
        # Task still running, try pause/resume
        send_pause "$task_id" || true
        sleep 2
        send_resume "$task_id" || true
        if wait_for_status "$task_id" "TASK_STATUS_COMPLETED" 30; then
            log_pass "Simple task completed after pause/resume"
            return 0
        fi
    fi

    log_fail "Simple task test unexpected status: $status"
    return 1
}

# ============================================
# Main
# ============================================
echo "=========================================="
echo "Control Signals E2E Tests"
echo "=========================================="
echo ""
echo "Testing pause/resume/cancel with:"
echo "- DB status verification"
echo "- Temporal signal delivery"
echo "- HTTP API endpoints"
echo ""

# Run tests
test_pause_resume_db_status
test_cancel_db_status
test_cancel_while_paused
test_http_api_control
test_simple_task_fast_completion

# Summary
echo ""
echo "=========================================="
echo -e "Results: ${GREEN}$PASSED passed${NC}, ${RED}$FAILED failed${NC}"
echo "=========================================="

if [ $FAILED -gt 0 ]; then
    exit 1
fi
exit 0
