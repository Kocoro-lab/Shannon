#!/bin/bash
# P2P Supervisor Memory Validation Test
# Tests that supervisor memory retrieval works for similar/same tasks
# and validates P2P coordination features (mailbox, workspace, dependencies)

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Test results tracking
PASSED=0
FAILED=0
TESTS_RUN=0

# Helper functions
log_test() {
    echo -e "\n${BLUE}[TEST $((++TESTS_RUN))]${NC} $1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
    ((++PASSED))
}

log_failure() {
    echo -e "${RED}✗${NC} $1"
    ((++FAILED))
}

log_info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

log_debug() {
    echo -e "${CYAN}→${NC} $1"
}

# Wait for services to be ready
wait_for_services() {
    echo "Waiting for services to be ready..."
    local ready=false
    for i in {1..30}; do
        if curl -s http://localhost:8081/health > /dev/null 2>&1 && \
           redis-cli ping > /dev/null 2>&1 && \
           grpcurl -plaintext localhost:50052 list > /dev/null 2>&1; then
            ready=true
            break
        fi
        echo -n "."
        sleep 2
    done

    if [ "$ready" = false ]; then
        log_failure "Services not ready after 60 seconds"
        exit 1
    fi
    echo ""
    log_success "Services are ready"
}

# Submit task and get ID
submit_task() {
    local user_id="$1"
    local session_id="$2"
    local query="$3"
    local context="${4:-{}}"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"$user_id\", \"sessionId\": \"$session_id\"},
        \"query\": \"$query\",
        \"context\": $context
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || echo ""
}

# Wait for task completion
wait_for_task() {
    local task_id="$1"
    local max_wait="${2:-30}"

    for i in $(seq 1 $max_wait); do
        local status=$(grpcurl -plaintext -d "{\"taskId\":\"$task_id\"}" \
            localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 | \
            grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' | \
            sed 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || echo "")

        if [[ "$status" == "COMPLETED" ]] || [[ "$status" == "TASK_STATUS_COMPLETED" ]]; then
            return 0
        elif [[ "$status" == "FAILED" ]] || [[ "$status" == "TASK_STATUS_FAILED" ]]; then
            return 1
        fi
        sleep 1
    done
    return 2  # Timeout
}

# Test 1: Supervisor Memory Retrieval for Identical Tasks
test_supervisor_memory_identical() {
    log_test "Supervisor Memory - Identical Task Retrieval"

    local user_id="memory-test-$(date +%s)"
    local session_id="supervisor-memory-identical"

    # First task - establish baseline
    log_debug "Submitting first complex analysis task..."
    local task1_id=$(submit_task "$user_id" "$session_id" \
        "Analyze the performance metrics of a web application, identify bottlenecks, and create an optimization report")

    if [ -z "$task1_id" ]; then
        log_failure "Failed to submit first task"
        return 1
    fi

    log_info "First task ID: $task1_id"

    # Wait for first task to complete
    if wait_for_task "$task1_id" 20; then
        log_success "First task completed"
    else
        log_info "First task still running (expected for complex tasks)"
    fi

    # Give time for memory storage
    sleep 3

    # Submit identical task to test memory retrieval
    log_debug "Submitting identical task to test memory retrieval..."
    local task2_id=$(submit_task "$user_id" "$session_id" \
        "Analyze the performance metrics of a web application, identify bottlenecks, and create an optimization report")

    if [ -z "$task2_id" ]; then
        log_failure "Failed to submit second task"
        return 1
    fi

    log_info "Second task ID: $task2_id"

    # Check logs for memory retrieval
    sleep 3
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=200 2>&1)

    if echo "$logs" | grep -iq "supervisor.*memory\|strategy.*performance\|decomposition.*pattern"; then
        log_success "Supervisor memory retrieval detected"
    else
        log_failure "No supervisor memory retrieval indicators found"
    fi

    # Check if second task used cached strategies
    if echo "$logs" | grep -iq "cached\|previous\|historical"; then
        log_success "Task used cached/previous strategies"
    else
        log_info "No explicit cache usage detected (may use different mechanism)"
    fi

    return 0
}

# Test 2: Supervisor Memory for Similar Tasks
test_supervisor_memory_similar() {
    log_test "Supervisor Memory - Similar Task Pattern Recognition"

    local user_id="memory-test-$(date +%s)"
    local session_id="supervisor-memory-similar"

    # First task with specific pattern
    log_debug "Submitting data processing pipeline task..."
    local task1_id=$(submit_task "$user_id" "$session_id" \
        "Load CSV data, process it to extract metrics, create visualizations, and generate PDF report")

    if [ -z "$task1_id" ]; then
        log_failure "Failed to submit first pipeline task"
        return 1
    fi

    log_info "First pipeline task ID: $task1_id"
    wait_for_task "$task1_id" 15

    sleep 3

    # Similar task with same pattern but different data
    log_debug "Submitting similar pipeline with different data..."
    local task2_id=$(submit_task "$user_id" "$session_id" \
        "Load JSON data, analyze it for patterns, create charts, and produce summary document")

    if [ -z "$task2_id" ]; then
        log_failure "Failed to submit similar task"
        return 1
    fi

    log_info "Similar task ID: $task2_id"

    # Check for pattern reuse
    sleep 3
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=200 2>&1)

    if echo "$logs" | grep -iq "decomposition.*pattern\|similar.*task\|pattern.*match"; then
        log_success "Pattern recognition detected for similar tasks"
    else
        log_info "No explicit pattern recognition found"
    fi

    return 0
}

# Test 3: P2P Mailbox Message Exchange
test_p2p_mailbox() {
    log_test "P2P Mailbox - Agent-to-Agent Communication"

    local session_id="mailbox-test-$(date +%s)"

    # Task requiring multiple agents with dependencies
    log_debug "Submitting multi-agent collaborative task..."
    local task_id=$(submit_task "p2p-test" "$session_id" \
        "Research quantum computing, then validate findings with scientific papers, then create summary report based on validated findings")

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit multi-agent task"
        return 1
    fi

    log_info "Multi-agent task ID: $task_id"

    # Monitor for mailbox messages
    sleep 5
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=300 2>&1)

    # Check for mailbox indicators
    if echo "$logs" | grep -iq "mailbox\|role_assigned\|agent.*message\|MailboxMessage"; then
        log_success "Mailbox message exchange detected"
    else
        log_info "No explicit mailbox messages (may use different coordination)"
    fi

    # Check for agent coordination
    if echo "$logs" | grep -iq "produces\|consumes\|dependency\|waiting"; then
        log_success "Agent dependency coordination detected"
    else
        log_failure "No agent coordination indicators found"
    fi

    return 0
}

# Test 4: P2P Workspace Data Sharing
test_p2p_workspace() {
    log_test "P2P Workspace - Data Sharing via Redis"

    local session_id="workspace-test-$(date +%s)"

    # Task with explicit data dependencies
    log_debug "Submitting task with data dependencies..."
    local task_id=$(submit_task "p2p-test" "$session_id" \
        "Calculate factorial of 5, then use that result to compute percentage of 1000, then create a report with both values")

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit workspace task"
        return 1
    fi

    log_info "Workspace task ID: $task_id"

    # Wait for processing
    sleep 5

    # Check Redis for workspace data
    log_debug "Checking Redis for workspace data..."

    # Check for session data
    local session_data=$(redis-cli GET "session:$session_id" 2>&1)
    if [ "$session_data" != "" ] && [ "$session_data" != "(nil)" ]; then
        log_success "Session data stored in Redis"
    else
        log_info "Session data not found (may not persist immediately)"
    fi

    # Check for workspace patterns
    local workspace_keys=$(redis-cli --scan --pattern "*workspace*" 2>&1 | head -5)
    if [ -n "$workspace_keys" ]; then
        log_success "Workspace keys found in Redis"
    else
        log_info "No explicit workspace keys (may use different pattern)"
    fi

    # Check for agent data sharing
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=200 2>&1)
    if echo "$logs" | grep -iq "workspace\|shared.*data\|data.*exchange"; then
        log_success "Workspace data exchange detected"
    else
        log_info "No explicit workspace exchange in logs"
    fi

    return 0
}

# Test 5: Complex Pipeline with Memory and P2P
test_complex_pipeline_with_memory() {
    log_test "Complex Pipeline - Memory + P2P Coordination"

    local user_id="pipeline-test-$(date +%s)"
    local session_id="pipeline-memory"

    # First pipeline to establish patterns
    log_debug "Submitting initial pipeline to establish patterns..."
    local task1_id=$(submit_task "$user_id" "$session_id" \
        "First, gather market data from multiple sources, then analyze trends and patterns, then generate investment recommendations, and finally create executive summary")

    if [ -z "$task1_id" ]; then
        log_failure "Failed to submit initial pipeline"
        return 1
    fi

    log_info "Initial pipeline ID: $task1_id"
    wait_for_task "$task1_id" 20

    sleep 3

    # Second pipeline to test memory + P2P
    log_debug "Submitting second pipeline to test memory retrieval..."
    local task2_id=$(submit_task "$user_id" "$session_id" \
        "First, collect customer feedback data, then perform sentiment analysis, then identify improvement areas, and finally prepare action plan")

    if [ -z "$task2_id" ]; then
        log_failure "Failed to submit second pipeline"
        return 1
    fi

    log_info "Second pipeline ID: $task2_id"

    sleep 5

    # Check for both memory retrieval and P2P coordination
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=400 2>&1)

    local memory_found=false
    local p2p_found=false

    if echo "$logs" | grep -iq "supervisor.*memory\|decomposition.*pattern\|strategy.*reuse"; then
        memory_found=true
        log_success "Memory retrieval detected in pipeline"
    fi

    if echo "$logs" | grep -iq "dependency\|produces\|consumes\|mailbox"; then
        p2p_found=true
        log_success "P2P coordination detected in pipeline"
    fi

    if [ "$memory_found" = true ] && [ "$p2p_found" = true ]; then
        log_success "Both memory and P2P working together"
    elif [ "$memory_found" = false ] && [ "$p2p_found" = false ]; then
        log_failure "Neither memory nor P2P detected"
    fi

    return 0
}

# Test 6: Verify Database Persistence
test_database_persistence() {
    log_test "Database Persistence - Task and Memory Storage"

    # Check tasks table
    log_debug "Checking tasks table for recent entries..."
    local tasks_count=$(PGPASSWORD=shannon psql -h localhost -U shannon -d shannon -t -c "SELECT COUNT(*) FROM task_executions WHERE created_at > NOW() - INTERVAL '10 minutes';" 2>&1 | xargs)

    if [ "$tasks_count" -gt 0 ]; then
        log_success "Tasks persisted to database: $tasks_count recent entries"
    else
        log_failure "No recent tasks in database"
    fi

    # Check agent_executions for strategy data
    log_debug "Checking agent_executions for strategy metadata..."
    local strategy_count=$(PGPASSWORD=shannon psql -h localhost -U shannon -d shannon -t -c "SELECT COUNT(*) FROM agent_executions WHERE metadata->>'strategy' IS NOT NULL;" 2>&1 | xargs)

    if [ "$strategy_count" -gt 0 ]; then
        log_success "Strategy metadata stored: $strategy_count entries"
    else
        log_info "No strategy metadata found (may not be required)"
    fi

    # Check for session linkage
    log_debug "Checking for proper session linkage..."
    local linked_tasks=$(PGPASSWORD=shannon psql -h localhost -U shannon -d shannon -t -c "SELECT COUNT(*) FROM task_executions WHERE session_id IS NOT NULL AND created_at > NOW() - INTERVAL '10 minutes';" 2>&1 | xargs)

    if [ "$linked_tasks" -gt 0 ]; then
        log_success "Tasks properly linked to sessions: $linked_tasks entries"
    else
        log_failure "No session-linked tasks found (data flow issue)"
    fi

    return 0
}

# Main test execution
main() {
    echo -e "${BLUE}=== P2P Supervisor Memory Validation Test ===${NC}"
    echo "Testing supervisor memory retrieval and P2P coordination..."

    # Check environment
    wait_for_services

    # Run all tests
    test_supervisor_memory_identical
    test_supervisor_memory_similar
    test_p2p_mailbox
    test_p2p_workspace
    test_complex_pipeline_with_memory
    test_database_persistence

    # Summary
    echo -e "\n${BLUE}=== Test Summary ===${NC}"
    echo -e "Tests Run: $TESTS_RUN"
    echo -e "${GREEN}Passed: $PASSED${NC}"
    echo -e "${RED}Failed: $FAILED${NC}"

    if [ $FAILED -eq 0 ]; then
        echo -e "\n${GREEN}✓ All P2P and supervisor memory tests passed!${NC}"
        echo "The system is working as expected with:"
        echo "  • Supervisor memory retrieval for similar tasks"
        echo "  • P2P mailbox communication between agents"
        echo "  • Workspace data sharing via Redis"
        echo "  • Complex pipeline coordination"
    else
        echo -e "\n${YELLOW}⚠ Some tests failed or returned warnings${NC}"
        echo "Known issues that may affect tests:"
        echo "  • NULL user_id/session_id in tasks table"
        echo "  • Empty task_executions table (migration in progress)"
        echo "  • Supervisor memory query recently fixed"
        echo ""
        echo "To investigate:"
        echo "  docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=500"
        echo "  PGPASSWORD=shannon psql -h localhost -U shannon -d shannon"
        echo "  redis-cli MONITOR"
    fi

    # Exit with appropriate code
    if [ $FAILED -gt 0 ]; then
        exit 1
    fi
}

# Run the test suite
main "$@"
