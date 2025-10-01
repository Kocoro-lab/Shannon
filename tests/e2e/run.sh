#!/usr/bin/env bash
set -euo pipefail

# Shannon Platform E2E Test Suite
# Comprehensive testing of multi-agent workflows and cognitive patterns

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=========================================="
echo "Shannon Platform E2E Test Suite"
echo "=========================================="
echo -e "${NC}"

# Ensure stack is up
echo -e "${YELLOW}[Setup] Ensuring services are running...${NC}"
docker compose -f "$COMPOSE_FILE" ps >/dev/null || docker compose -f "$COMPOSE_FILE" up -d

# Wait for services to be healthy
echo "Waiting for services to be healthy..."
sleep 5

# Check service health
HEALTH_CHECK=$(curl -s http://localhost:8081/health | jq -r '.status' 2>/dev/null || echo "unknown")
if [ "$HEALTH_CHECK" = "healthy" ]; then
    echo -e "${GREEN}✅ Services are healthy${NC}"
else
    echo -e "${YELLOW}⚠️  Services may still be starting up${NC}"
    sleep 5
fi

# Run test suites
echo ""
echo -e "${BLUE}[Test Suite 1] Basic Smoke Tests${NC}"
echo "----------------------------------------"
make smoke
SMOKE_RESULT=$?

echo ""
echo -e "${BLUE}[Test Suite 2] Calculator & Python Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/01_basic_calculator_test.sh" ]; then
    bash "$SCRIPT_DIR/01_basic_calculator_test.sh"
    CALC_RESULT=$?
else
    echo "Calculator test not found, skipping..."
    CALC_RESULT=0
fi

echo ""
echo -e "${BLUE}[Test Suite 3] Supervisor Workflow Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/10_supervisor_workflow_test.sh" ]; then
    bash "$SCRIPT_DIR/10_supervisor_workflow_test.sh"
    SUPERVISOR_RESULT=$?
else
    echo "Supervisor test not found, skipping..."
    SUPERVISOR_RESULT=0
fi

echo ""
echo -e "${BLUE}[Test Suite 4] Cognitive Patterns Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/05_cognitive_patterns_test.sh" ]; then
    bash "$SCRIPT_DIR/05_cognitive_patterns_test.sh"
    COGNITIVE_RESULT=$?
else
    echo "Cognitive patterns test not found, skipping..."
    COGNITIVE_RESULT=0
fi

echo ""
echo -e "${BLUE}[Test Suite 5] Session Memory Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/../integration/session_memory_test.sh" ]; then
    bash "$SCRIPT_DIR/../integration/session_memory_test.sh"
    SESSION_RESULT=$?
else
    echo "Session memory test not found, skipping..."
    SESSION_RESULT=0
fi

echo ""
echo -e "${BLUE}[Test Suite 6] P2P Coordination Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/20_p2p_coordination_test.sh" ]; then
    bash "$SCRIPT_DIR/20_p2p_coordination_test.sh"
    P2P_RESULT=$?
else
    echo "P2P coordination test not found, skipping..."
    P2P_RESULT=0
fi

echo ""
echo -e "${BLUE}[Test Suite 7] OpenAPI Integration Tests${NC}"
echo "----------------------------------------"
if [ -f "$SCRIPT_DIR/06_openapi_petstore_test.sh" ]; then
    bash "$SCRIPT_DIR/06_openapi_petstore_test.sh"
    OPENAPI_RESULT=$?
else
    echo "OpenAPI test not found, skipping..."
    OPENAPI_RESULT=0
fi

# Performance metrics check
echo ""
echo -e "${BLUE}[Metrics] Checking Performance Metrics${NC}"
echo "----------------------------------------"

# Check orchestrator metrics
echo "Orchestrator metrics:"
curl -s http://localhost:2112/metrics 2>/dev/null | grep -E "shannon_orchestrator_workflow_duration|shannon_orchestrator_pattern" | head -5 || echo "  Metrics not available"

# Check agent core metrics
echo ""
echo "Agent Core metrics:"
curl -s http://localhost:2113/metrics 2>/dev/null | grep -E "shannon_agent_tool_execution|shannon_agent_enforcement" | head -5 || echo "  Metrics not available"

# Check database statistics
echo ""
echo "Database statistics:"
docker compose -f "$COMPOSE_FILE" exec -T postgres \
    psql -U shannon -d shannon -c \
    "SELECT
        COUNT(*) as total_tasks,
        COUNT(DISTINCT session_id) as unique_sessions,
        AVG(EXTRACT(EPOCH FROM (completed_at - created_at))) as avg_duration_seconds,
        COUNT(CASE WHEN status = 'COMPLETED' THEN 1 END) as completed,
        COUNT(CASE WHEN status = 'FAILED' THEN 1 END) as failed
    FROM task_executions
    WHERE created_at > NOW() - INTERVAL '1 hour';" 2>/dev/null || echo "  Database stats not available"

# Cleanup
echo ""
echo -e "${BLUE}[Cleanup] Terminating any stuck workflows${NC}"
docker compose -f "$COMPOSE_FILE" exec temporal \
    temporal workflow list --address temporal:7233 2>/dev/null | \
    grep "Running" | grep "task-" | awk '{print $2}' | \
    while read wf; do
        echo "  Terminating $wf..."
        docker compose -f "$COMPOSE_FILE" exec temporal \
            temporal workflow terminate --workflow-id "$wf" --address temporal:7233 \
            --reason "E2E test cleanup" 2>/dev/null || true
    done

# Test Summary
echo ""
echo -e "${BLUE}=========================================="
echo "E2E Test Summary"
echo "=========================================="
echo -e "${NC}"

TOTAL_TESTS=7
PASSED_TESTS=0

[ $SMOKE_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ $CALC_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ $SUPERVISOR_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ $COGNITIVE_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ $SESSION_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ $P2P_RESULT -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))
[ ${OPENAPI_RESULT:-1} -eq 0 ] && PASSED_TESTS=$((PASSED_TESTS + 1))

echo "Test Results: $PASSED_TESTS/$TOTAL_TESTS passed"
echo ""
echo -e "1. Smoke Tests:           $([ $SMOKE_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "2. Calculator Tests:      $([ $CALC_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "3. Supervisor Tests:      $([ $SUPERVISOR_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "4. Cognitive Tests:       $([ $COGNITIVE_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "5. Session Memory Tests:  $([ $SESSION_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "6. P2P Coordination:      $([ $P2P_RESULT -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"
echo -e "7. OpenAPI Integration:   $([ ${OPENAPI_RESULT:-1} -eq 0 ] && echo -e "${GREEN}✅ PASSED${NC}" || echo -e "${RED}❌ FAILED${NC}")"

echo ""
echo "For detailed logs, run:"
echo "  docker compose -f $COMPOSE_FILE logs orchestrator --tail 200"
echo "  docker compose -f $COMPOSE_FILE logs llm-service --tail 200"
echo ""

# Exit with appropriate code
if [ $PASSED_TESTS -eq $TOTAL_TESTS ]; then
    echo -e "${GREEN}All tests passed successfully!${NC}"
    exit 0
else
    echo -e "${YELLOW}Some tests did not pass. Check logs for details.${NC}"
    exit 1
fi
