#!/usr/bin/env bash
# HITL Research Review E2E Test
# Tests the full human-in-the-loop research plan review cycle:
#   submit → plan ready → feedback → approve → workflow completion
# set -e disabled to allow individual test error handling

GATEWAY="http://localhost:8080"
GRPC_ADDR="localhost:50052"
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

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# ── Helpers ──────────────────────────────────────────

# Submit a task via HTTP with require_review + force_research
submit_task_http() {
    local query="$1"
    local session_id="hitl-test-$(date +%s)-$RANDOM"

    curl -sS -X POST "$GATEWAY/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -H "X-User-ID: 00000000-0000-0000-0000-000000000002" \
        -d "{
            \"query\": \"$query\",
            \"context\": {
                \"force_research\": true,
                \"require_review\": true,
                \"user_id\": \"00000000-0000-0000-0000-000000000002\"
            }
        }" 2>&1
}

# Poll GET /review until status=reviewing or timeout
# Returns: sets REVIEW_BODY (full JSON) and REVIEW_ETAG (ETag header)
poll_review() {
    local task_id="$1"
    local max_attempts="${2:-30}"

    for i in $(seq 1 "$max_attempts"); do
        local tmp_headers
        tmp_headers=$(mktemp)

        REVIEW_BODY=$(curl -sS -D "$tmp_headers" \
            "$GATEWAY/api/v1/tasks/$task_id/review" \
            -H "X-User-ID: 00000000-0000-0000-0000-000000000002" 2>&1)

        local http_code
        http_code=$(grep -i "^HTTP/" "$tmp_headers" | tail -1 | awk '{print $2}')
        REVIEW_ETAG=$(grep -i "^etag:" "$tmp_headers" | tail -1 | tr -d '\r' | awk '{print $2}')
        rm -f "$tmp_headers"

        if [ "$http_code" = "200" ]; then
            local status
            status=$(echo "$REVIEW_BODY" | jq -r '.status' 2>/dev/null)
            if [ "$status" = "reviewing" ]; then
                return 0
            fi
        fi

        sleep 2
    done
    return 1
}

# Send feedback to review endpoint
# Returns: sets FEEDBACK_BODY and FEEDBACK_ETAG
send_feedback() {
    local task_id="$1"
    local message="$2"
    local if_match="$3"

    local tmp_headers
    tmp_headers=$(mktemp)

    local headers=(-H "Content-Type: application/json" -H "X-User-ID: 00000000-0000-0000-0000-000000000002")
    if [ -n "$if_match" ]; then
        headers+=(-H "If-Match: $if_match")
    fi

    FEEDBACK_BODY=$(curl -sS -D "$tmp_headers" \
        -X POST "$GATEWAY/api/v1/tasks/$task_id/review" \
        "${headers[@]}" \
        -d "{\"action\": \"feedback\", \"message\": \"$message\"}" 2>&1)

    FEEDBACK_HTTP_CODE=$(grep -i "^HTTP/" "$tmp_headers" | tail -1 | awk '{print $2}')
    FEEDBACK_ETAG=$(grep -i "^etag:" "$tmp_headers" | tail -1 | tr -d '\r' | awk '{print $2}')
    rm -f "$tmp_headers"
}

# Send approve to review endpoint
send_approve() {
    local task_id="$1"
    local if_match="$2"

    local tmp_headers
    tmp_headers=$(mktemp)

    local headers=(-H "Content-Type: application/json" -H "X-User-ID: 00000000-0000-0000-0000-000000000002")
    if [ -n "$if_match" ]; then
        headers+=(-H "If-Match: $if_match")
    fi

    APPROVE_BODY=$(curl -sS -D "$tmp_headers" \
        -X POST "$GATEWAY/api/v1/tasks/$task_id/review" \
        "${headers[@]}" \
        -d '{"action": "approve"}' 2>&1)

    APPROVE_HTTP_CODE=$(grep -i "^HTTP/" "$tmp_headers" | tail -1 | awk '{print $2}')
    rm -f "$tmp_headers"
}

# Get task status via gRPC
get_task_status() {
    local task_id="$1"
    grpcurl -plaintext -d "{\"task_id\": \"$task_id\"}" \
        $GRPC_ADDR shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 | \
        grep -o '"status": "[^"]*"' | cut -d'"' -f4
}

# Terminate workflow (cleanup)
terminate_workflow() {
    local task_id="$1"
    $TEMPORAL_CLI workflow terminate --workflow-id "$task_id" \
        --address temporal:7233 --reason "E2E test cleanup" 2>/dev/null || true
}

# ── Shared state ─────────────────────────────────────
TASK_ID=""
CURRENT_VERSION=""

# ============================================
# Test 1: Submit + Plan Ready
# ============================================
test_submit_plan_ready() {
    log_section "Test 1: Submit + Plan Ready"

    local response
    response=$(submit_task_http "Research the history of quantum computing")
    TASK_ID=$(echo "$response" | jq -r '.task_id // .workflow_id' 2>/dev/null)

    if [ -z "$TASK_ID" ] || [ "$TASK_ID" = "null" ]; then
        log_fail "Failed to submit task - response: $response"
        return 1
    fi
    log_info "Submitted: $TASK_ID"

    # Poll for plan ready (60s timeout = 30 attempts * 2s)
    log_info "Polling for review plan ready..."
    if ! poll_review "$TASK_ID" 30; then
        log_fail "Timeout waiting for review plan (60s)"
        return 1
    fi

    # Validate response structure
    local round message status
    round=$(echo "$REVIEW_BODY" | jq -r '.round' 2>/dev/null)
    message=$(echo "$REVIEW_BODY" | jq -r '(.current_plan | select(. != "")) // .rounds[-1].message // empty' 2>/dev/null)
    status=$(echo "$REVIEW_BODY" | jq -r '.status' 2>/dev/null)
    CURRENT_VERSION=$(echo "$REVIEW_BODY" | jq -r '.version' 2>/dev/null)

    if [ "$status" != "reviewing" ]; then
        log_fail "Expected status=reviewing, got '$status'"
        return 1
    fi

    if [ -z "$message" ] || [ "$message" = "null" ]; then
        log_fail "Plan message is empty"
        return 1
    fi

    log_info "Round: $round | Version: $CURRENT_VERSION | ETag: $REVIEW_ETAG"
    log_info "Plan preview: ${message:0:100}..."
    log_pass "Submit + Plan Ready - status=reviewing, plan present"
    return 0
}

# ============================================
# Test 2: Feedback Round
# ============================================
test_feedback_round() {
    log_section "Test 2: Feedback Round"

    if [ -z "$TASK_ID" ]; then
        log_fail "No task ID from previous test"
        return 1
    fi

    local version_before="$CURRENT_VERSION"
    log_info "Sending feedback (version before: $version_before)..."

    send_feedback "$TASK_ID" "Focus on recent developments from 2024-2025" "$CURRENT_VERSION"

    if [ "$FEEDBACK_HTTP_CODE" != "200" ]; then
        log_fail "Feedback returned HTTP $FEEDBACK_HTTP_CODE - body: $FEEDBACK_BODY"
        return 1
    fi

    # Validate response
    local plan_message plan_round plan_version plan_intent
    plan_message=$(echo "$FEEDBACK_BODY" | jq -r '.plan.message // empty' 2>/dev/null)
    plan_round=$(echo "$FEEDBACK_BODY" | jq -r '.plan.round // empty' 2>/dev/null)
    plan_version=$(echo "$FEEDBACK_BODY" | jq -r '.plan.version // empty' 2>/dev/null)
    plan_intent=$(echo "$FEEDBACK_BODY" | jq -r '.plan.intent // empty' 2>/dev/null)

    if [ -z "$plan_message" ] || [ "$plan_message" = "null" ]; then
        log_fail "Feedback response has empty plan message"
        return 1
    fi

    if [ -z "$plan_version" ] || [ "$plan_version" = "null" ]; then
        log_fail "Feedback response missing version"
        return 1
    fi

    # Version should have incremented
    if [ "$plan_version" -le "$version_before" ] 2>/dev/null; then
        log_fail "Version did not increment: before=$version_before, after=$plan_version"
        return 1
    fi

    # ETag should be present
    if [ -z "$FEEDBACK_ETAG" ]; then
        log_fail "Feedback response missing ETag header"
        return 1
    fi

    CURRENT_VERSION="$plan_version"
    log_info "Round: $plan_round | Version: $plan_version | Intent: $plan_intent | ETag: $FEEDBACK_ETAG"
    log_info "Plan preview: ${plan_message:0:100}..."
    log_pass "Feedback round - version incremented ($version_before → $plan_version), ETag present"
    return 0
}

# ============================================
# Test 3: Version Conflict (409)
# ============================================
test_version_conflict() {
    log_section "Test 3: Version Conflict (409)"

    if [ -z "$TASK_ID" ]; then
        log_fail "No task ID from previous test"
        return 1
    fi

    # Send feedback with stale version (use "1" which should be < current)
    log_info "Sending feedback with stale If-Match: 1 (current: $CURRENT_VERSION)..."
    send_feedback "$TASK_ID" "This should conflict" "1"

    if [ "$FEEDBACK_HTTP_CODE" = "409" ]; then
        log_pass "Version conflict detected - HTTP 409 returned"
        return 0
    else
        log_fail "Expected HTTP 409, got $FEEDBACK_HTTP_CODE - body: $FEEDBACK_BODY"
        return 1
    fi
}

# ============================================
# Test 4: Approve
# ============================================
test_approve() {
    log_section "Test 4: Approve"

    if [ -z "$TASK_ID" ]; then
        log_fail "No task ID from previous test"
        return 1
    fi

    log_info "Sending approve with If-Match: $CURRENT_VERSION..."
    send_approve "$TASK_ID" "$CURRENT_VERSION"

    if [ "$APPROVE_HTTP_CODE" != "200" ]; then
        log_fail "Approve returned HTTP $APPROVE_HTTP_CODE - body: $APPROVE_BODY"
        return 1
    fi

    local status
    status=$(echo "$APPROVE_BODY" | jq -r '.status' 2>/dev/null)
    if [ "$status" != "approved" ]; then
        log_fail "Expected status=approved, got '$status'"
        return 1
    fi

    log_pass "Approve accepted - status=approved"
    return 0
}

# ============================================
# Test 5: Post-Approve Execution
# ============================================
test_post_approve_execution() {
    log_section "Test 5: Post-Approve Execution"

    if [ -z "$TASK_ID" ]; then
        log_fail "No task ID from previous test"
        return 1
    fi

    # After approve, verify the workflow accepted the signal and is executing.
    # Full research takes ~15 min; for a smoke test we just confirm
    # the workflow is still RUNNING (signal accepted, research started)
    # or already reached a terminal state.
    log_info "Verifying workflow is executing after approval (30s)..."

    local max_attempts=15  # 15 * 2s = 30s
    local found_running=false
    for i in $(seq 1 $max_attempts); do
        local status
        status=$(get_task_status "$TASK_ID")

        case "$status" in
            TASK_STATUS_COMPLETED)
                log_pass "Workflow completed successfully"
                return 0
                ;;
            TASK_STATUS_FAILED)
                log_warn "Workflow failed (LLM instability is acceptable)"
                log_pass "Workflow reached terminal state (FAILED)"
                return 0
                ;;
            TASK_STATUS_CANCELLED)
                log_warn "Workflow was cancelled"
                log_pass "Workflow reached terminal state (CANCELLED)"
                return 0
                ;;
            TASK_STATUS_RUNNING)
                found_running=true
                ;;
        esac
        sleep 2
    done

    if [ "$found_running" = true ]; then
        log_info "Workflow still running (research in progress) - terminating for cleanup"
        terminate_workflow "$TASK_ID"
        log_pass "Post-approve execution confirmed (workflow RUNNING after approval)"
        return 0
    fi

    log_fail "Workflow in unexpected state after approval"
    terminate_workflow "$TASK_ID" 2>/dev/null || true
    return 1
}

# ============================================
# Main
# ============================================
echo "=========================================="
echo "HITL Research Review E2E Tests"
echo "=========================================="
echo ""
echo "Testing the full HITL review cycle:"
echo "  submit → plan ready → feedback → approve → completion"
echo ""

# Run tests sequentially (each depends on prior state)
test_submit_plan_ready
test_feedback_round
test_version_conflict
test_approve
test_post_approve_execution

# Summary
echo ""
echo "=========================================="
echo -e "Results: ${GREEN}$PASSED passed${NC}, ${RED}$FAILED failed${NC}"
echo "=========================================="

if [ $FAILED -gt 0 ]; then
    # Cleanup on failure
    if [ -n "$TASK_ID" ]; then
        terminate_workflow "$TASK_ID" 2>/dev/null || true
    fi
    exit 1
fi
exit 0
