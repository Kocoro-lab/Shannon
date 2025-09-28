#!/bin/bash

# Memory System Integration Tests Runner
# This script runs integration tests for the Shannon memory system

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Shannon Memory System Integration Tests${NC}"
echo "========================================="
echo ""

# Function to check if a service is running
check_service() {
    local service=$1
    local port=$2

    if nc -z localhost $port 2>/dev/null; then
        echo -e "${GREEN}✓${NC} $service is running on port $port"
        return 0
    else
        echo -e "${RED}✗${NC} $service is not running on port $port"
        return 1
    fi
}

# Function to start services if needed
start_services() {
    echo -e "${YELLOW}Starting required services...${NC}"
    cd "$PROJECT_ROOT"

    docker compose -f deploy/compose/docker-compose.yml up -d \
        postgres redis qdrant temporal llm-service orchestrator agent-core

    echo "Waiting for services to be ready..."
    sleep 10
}

# Check prerequisites
echo "Checking prerequisites..."
echo ""

# Check if services are running
SERVICES_OK=true

check_service "PostgreSQL" 5432 || SERVICES_OK=false
check_service "Redis" 6379 || SERVICES_OK=false
check_service "Qdrant" 6333 || SERVICES_OK=false
check_service "LLM Service" 8000 || SERVICES_OK=false
check_service "Orchestrator" 50052 || SERVICES_OK=false

echo ""

if [ "$SERVICES_OK" = false ]; then
    echo -e "${YELLOW}Some services are not running.${NC}"
    read -p "Do you want to start them? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        start_services
    else
        echo -e "${RED}Cannot run tests without required services.${NC}"
        exit 1
    fi
fi

# Check environment variables
echo "Checking environment variables..."

if [ -z "$OPENAI_API_KEY" ] && [ -z "$ANTHROPIC_API_KEY" ]; then
    echo -e "${YELLOW}⚠ No LLM API key found. Tests may fail.${NC}"
    echo "Set OPENAI_API_KEY or ANTHROPIC_API_KEY in your environment."
fi

echo ""

# Run different test suites
run_test_suite() {
    local suite_name=$1
    local test_pattern=$2

    echo -e "${BLUE}Running $suite_name...${NC}"
    echo "----------------------------------------"

    cd "$PROJECT_ROOT"

    # Set environment for integration tests
    export RUN_INTEGRATION_TESTS=true
    export LLM_SERVICE_URL=http://localhost:8000
    export QDRANT_HOST=localhost
    export REDIS_URL=localhost:6379

    # Run the tests
    if go test -v ./tests/integration -run "$test_pattern" -timeout 5m; then
        echo -e "${GREEN}✓ $suite_name passed${NC}"
        return 0
    else
        echo -e "${RED}✗ $suite_name failed${NC}"
        return 1
    fi
}

# Track test results
FAILED_TESTS=()
PASSED_TESTS=()

# Run test suites
echo -e "${BLUE}Starting test execution...${NC}"
echo ""

# Test Suite 1: Chunking Pipeline
if run_test_suite "Chunking Pipeline" "TestMemoryIntegrationSuite/TestChunkingPipelineIntegration"; then
    PASSED_TESTS+=("Chunking Pipeline")
else
    FAILED_TESTS+=("Chunking Pipeline")
fi
echo ""

# Test Suite 2: Batch Embeddings
if run_test_suite "Batch Embeddings" "TestMemoryIntegrationSuite/TestBatchEmbeddingIntegration"; then
    PASSED_TESTS+=("Batch Embeddings")
else
    FAILED_TESTS+=("Batch Embeddings")
fi
echo ""

# Test Suite 3: MMR Diversity
if run_test_suite "MMR Diversity" "TestMemoryIntegrationSuite/TestMMRDiversityIntegration"; then
    PASSED_TESTS+=("MMR Diversity")
else
    FAILED_TESTS+=("MMR Diversity")
fi
echo ""

# Test Suite 4: Idempotency
if run_test_suite "Idempotency" "TestMemoryIntegrationSuite/TestIdempotencyIntegration"; then
    PASSED_TESTS+=("Idempotency")
else
    FAILED_TESTS+=("Idempotency")
fi
echo ""

# Test Suite 5: Reconstruction Accuracy
if run_test_suite "Reconstruction Accuracy" "TestMemoryIntegrationSuite/TestChunkReconstructionAccuracy"; then
    PASSED_TESTS+=("Reconstruction Accuracy")
else
    FAILED_TESTS+=("Reconstruction Accuracy")
fi
echo ""

# Test Suite 6: Session Isolation
if run_test_suite "Session Isolation" "TestMemoryIntegrationSuite/TestCrossSessionIsolation"; then
    PASSED_TESTS+=("Session Isolation")
else
    FAILED_TESTS+=("Session Isolation")
fi
echo ""

# Test Suite 7: Performance Under Load
if [ "$RUN_PERFORMANCE_TESTS" = "true" ]; then
    if run_test_suite "Performance Under Load" "TestMemoryIntegrationSuite/TestPerformanceUnderLoad"; then
        PASSED_TESTS+=("Performance Under Load")
    else
        FAILED_TESTS+=("Performance Under Load")
    fi
    echo ""
fi

# Test Suite 8: Hierarchical Memory
if run_test_suite "Hierarchical Memory" "TestMemoryIntegrationSuite/TestHierarchicalMemoryIntegration"; then
    PASSED_TESTS+=("Hierarchical Memory")
else
    FAILED_TESTS+=("Hierarchical Memory")
fi
echo ""

# Run all tests at once (optional)
if [ "$RUN_ALL_TOGETHER" = "true" ]; then
    echo -e "${BLUE}Running all tests together...${NC}"
    if go test -v ./tests/integration -run TestMemoryIntegrationSuite -timeout 10m; then
        echo -e "${GREEN}✓ All tests passed together${NC}"
    else
        echo -e "${RED}✗ Some tests failed${NC}"
    fi
fi

# Generate report
echo ""
echo "========================================="
echo -e "${BLUE}📊 Test Results Summary${NC}"
echo "========================================="
echo ""

echo -e "${GREEN}Passed Tests (${#PASSED_TESTS[@]}):${NC}"
for test in "${PASSED_TESTS[@]}"; do
    echo -e "  ${GREEN}✓${NC} $test"
done

if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
    echo ""
    echo -e "${RED}Failed Tests (${#FAILED_TESTS[@]}):${NC}"
    for test in "${FAILED_TESTS[@]}"; do
        echo -e "  ${RED}✗${NC} $test"
    done
fi

echo ""
TOTAL_TESTS=$((${#PASSED_TESTS[@]} + ${#FAILED_TESTS[@]}))
SUCCESS_RATE=$((${#PASSED_TESTS[@]} * 100 / TOTAL_TESTS))

echo "Total: ${#PASSED_TESTS[@]}/${TOTAL_TESTS} passed (${SUCCESS_RATE}%)"

# Generate detailed metrics if requested
if [ "$GENERATE_METRICS" = "true" ]; then
    echo ""
    echo -e "${BLUE}📈 Performance Metrics${NC}"
    echo "----------------------------------------"

    # Query Prometheus for metrics
    curl -s "http://localhost:2112/metrics" | grep shannon_memory | head -20
fi

# Exit with appropriate code
if [ ${#FAILED_TESTS[@]} -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ All integration tests passed!${NC}"
    exit 0
else
    echo ""
    echo -e "${RED}❌ Some tests failed. Please review the output above.${NC}"
    exit 1
fi