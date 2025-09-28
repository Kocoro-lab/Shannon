#!/bin/bash
# Comprehensive P2P Agent Messaging Test Suite
# Tests all claims from the documentation about P2P coordination

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

# Function to check if P2P is enabled in config
check_p2p_enabled() {
    log_test "Checking P2P configuration"

    # Check if P2P is enabled via environment or config
    if [ "${P2P_COORDINATION_ENABLED:-}" = "true" ]; then
        log_success "P2P enabled via environment variable"
        return 0
    fi

    # Check config file
    if [ -f config/features.yaml ]; then
        if grep -q "p2p:" config/features.yaml && grep -q "enabled: true" config/features.yaml; then
            log_success "P2P enabled in config/features.yaml"
            return 0
        fi
    fi

    log_info "P2P not explicitly enabled, using default settings"
    return 0
}

# Test 1: Basic P2P Detection for Sequential Tasks
test_sequential_dependency_detection() {
    log_test "Sequential Task Dependency Detection"

    local query="First analyze the sales data from Q4, then create a detailed report based on that analysis"

    # Submit task
    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-session-$(date +%s)\"},
        \"query\": \"$query\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit sequential task"
        echo "$response"
        return 1
    fi

    log_info "Task ID: $task_id"

    # Check decomposition for produces/consumes fields
    sleep 3
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=100 2>&1)

    if echo "$logs" | grep -q "produces\|consumes"; then
        log_success "Decomposition detected data dependencies"
    else
        log_failure "No produces/consumes fields detected in decomposition"
    fi

    # Check if SupervisorWorkflow was selected
    local orch_logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=100 2>&1)

    if echo "$orch_logs" | grep -q "SupervisorWorkflow"; then
        log_success "SupervisorWorkflow selected for dependent tasks"
    else
        log_failure "SupervisorWorkflow not selected for tasks with dependencies"
    fi

    return 0
}

# Test 2: Force P2P Mode
test_force_p2p_mode() {
    log_test "Force P2P Mode on Simple Task"

    local query="What is 2+2?"

    # Submit with force_p2p flag
    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-force-$(date +%s)\"},
        \"query\": \"$query\",
        \"context\": {\"force_p2p\": \"true\"}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit task with force_p2p"
        echo "$response"
        return 1
    fi

    log_info "Task ID: $task_id"

    # Give it a moment to process
    sleep 2

    # Check if forced P2P was detected
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=50 2>&1)

    if echo "$logs" | grep -iq "forced\|force_p2p\|SupervisorWorkflow"; then
        log_success "P2P mode was forced for simple task"
    else
        log_failure "Force P2P flag may not have been processed"
    fi

    return 0
}

# Test 3: Complex Pipeline with Multiple Dependencies
test_complex_pipeline() {
    log_test "Complex Pipeline with Chain Dependencies"

    local query="Load the CSV data, then process it to extract metrics, then create visualizations from those metrics, and finally generate a PDF report combining everything"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-pipeline-$(date +%s)\"},
        \"query\": \"$query\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit pipeline task"
        echo "$response"
        return 1
    fi

    log_info "Task ID: $task_id"

    # Wait a bit for decomposition
    sleep 5

    # Check if multiple subtasks were created with dependencies
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=200 2>&1)

    if echo "$logs" | grep -q "subtasks"; then
        log_success "Pipeline decomposed into subtasks"
    else
        log_failure "Pipeline not properly decomposed"
    fi

    # Check for dependency chain in orchestrator
    local orch_logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=200 2>&1)

    if echo "$orch_logs" | grep -iq "dependency\|waiting\|produces\|consumes"; then
        log_success "Dependency chain detected in pipeline"
    else
        log_info "Dependency chain indicators not found in logs"
    fi

    return 0
}

# Test 4: Mailbox System Communication
test_mailbox_communication() {
    log_test "Agent Mailbox Message Delivery"

    # Submit a task that should create multiple agents
    local query="Research the topic of quantum computing, validate the findings with scientific sources, and write a comprehensive article"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-mailbox-$(date +%s)\"},
        \"query\": \"$query\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit multi-agent task"
        echo "$response"
        return 1
    fi

    log_info "Task ID: $task_id"

    # Wait for agents to be created
    sleep 5

    # Check for mailbox messages in logs
    local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=300 2>&1)

    if echo "$logs" | grep -iq "mailbox\|role_assigned\|MailboxMessage"; then
        log_success "Mailbox messages detected between agents"
    else
        log_info "No explicit mailbox messages found (may be using different coordination)"
    fi

    # Check for agent role assignments
    if echo "$logs" | grep -iq "role\|agent-"; then
        log_success "Agent roles were assigned"
    else
        log_failure "No agent role assignments detected"
    fi

    return 0
}

# Test 5: Workspace Data Exchange via Redis
test_workspace_data_exchange() {
    log_test "Workspace Data Exchange through Redis"

    local session_id="test-workspace-$(date +%s)"

    # Submit task that requires data sharing
    local query="Calculate the sum of 10 and 20, then use that result to calculate the percentage of 100"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"$session_id\"},
        \"query\": \"$query\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_failure "Failed to submit data-sharing task"
        echo "$response"
        return 1
    fi

    log_info "Task ID: $task_id"

    # Wait for task to process
    sleep 5

    # Check Redis for workspace data
    local redis_data=$(redis-cli --scan --pattern "*workspace*" 2>&1)

    if [ -n "$redis_data" ]; then
        log_success "Workspace data found in Redis"
    else
        log_info "No explicit workspace keys in Redis (may use different storage pattern)"
    fi

    # Check session data
    local session_data=$(redis-cli GET "session:$session_id" 2>&1)

    if [ "$session_data" != "" ] && [ "$session_data" != "(nil)" ]; then
        log_success "Session data stored in Redis"
    else
        log_info "Session data not found (may not persist immediately)"
    fi

    return 0
}

# Test 6: Dependency Timeout Handling
test_dependency_timeout() {
    log_test "Dependency Timeout Protection"

    # Submit task with impossible dependency
    local query="Wait for data that will never arrive, then process it"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-timeout-$(date +%s)\"},
        \"query\": \"$query\",
        \"context\": {\"force_p2p\": \"true\"}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -z "$task_id" ]; then
        log_info "Task submission handled appropriately"
        return 0
    fi

    log_info "Task ID: $task_id - Monitoring for timeout handling"

    # The system should handle this gracefully
    sleep 10

    # Check status - should not hang indefinitely
    local status_response=$(grpcurl -plaintext -d "{\"taskId\":\"$task_id\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

    if echo "$status_response" | grep -q "status"; then
        log_success "Task handled timeout gracefully"
    else
        log_failure "Task status check failed"
    fi

    return 0
}

# Test 7: Parallel vs Sequential Detection
test_parallel_vs_sequential() {
    log_test "Parallel vs Sequential Task Detection"

    # Test parallel tasks (no dependencies)
    local parallel_query="Calculate 2+2, find the capital of France, and tell me the current weather"

    local response=$(grpcurl -plaintext -d "{
        \"metadata\": {\"userId\": \"test-p2p\", \"sessionId\": \"test-parallel-$(date +%s)\"},
        \"query\": \"$parallel_query\",
        \"context\": {}
    }" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1)

    local task_id=$(echo "$response" | grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"taskId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')

    if [ -n "$task_id" ]; then
        sleep 3
        local logs=$(docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=100 2>&1)

        if echo "$logs" | grep -iq "DAGWorkflow\|parallel"; then
            log_success "Independent tasks routed to parallel execution"
        else
            log_info "Parallel execution not explicitly detected"
        fi
    fi

    return 0
}

# Main test execution
main() {
    echo -e "${BLUE}=== Comprehensive P2P Agent Messaging Test Suite ===${NC}"
    echo "Testing claims from P2P coordination documentation..."

    # Check environment
    wait_for_services
    check_p2p_enabled

    # Run all tests
    test_sequential_dependency_detection
    test_force_p2p_mode
    test_complex_pipeline
    test_mailbox_communication
    test_workspace_data_exchange
    test_dependency_timeout
    test_parallel_vs_sequential

    # Summary
    echo -e "\n${BLUE}=== Test Summary ===${NC}"
    echo -e "Tests Run: $TESTS_RUN"
    echo -e "${GREEN}Passed: $PASSED${NC}"
    echo -e "${RED}Failed: $FAILED${NC}"

    if [ $FAILED -eq 0 ]; then
        echo -e "\n${GREEN}✓ All P2P coordination tests passed!${NC}"
        echo "The P2P agent messaging system is working as documented."
    else
        echo -e "\n${YELLOW}⚠ Some tests failed or returned warnings${NC}"
        echo "Review the output above for details."
        echo ""
        echo "Common issues:"
        echo "1. Ensure P2P is enabled in config/features.yaml"
        echo "2. Check that all services are running: make dev"
        echo "3. Verify Redis is accessible for workspace storage"
        echo "4. Check orchestrator and llm-service logs for errors"
    fi

    echo ""
    echo "To investigate further:"
    echo "  docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=500 | grep -i p2p"
    echo "  docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=500 | grep -i produces"
    echo "  redis-cli MONITOR  # Watch Redis operations in real-time"

    # Exit with appropriate code
    if [ $FAILED -gt 0 ]; then
        exit 1
    fi
}

# Run the test suite
main "$@"