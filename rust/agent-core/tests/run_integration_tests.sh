#!/bin/bash

# Integration test runner for Python-Rust contract
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$SCRIPT_DIR/.."
MOCK_SERVER_PID=""
USE_MOCK=false
PORT=8000

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --mock)
            USE_MOCK=true
            shift
            ;;
        --port)
            PORT="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--mock] [--port PORT]"
            exit 1
            ;;
    esac
done

cleanup() {
    if [ ! -z "$MOCK_SERVER_PID" ]; then
        echo -e "${YELLOW}Stopping mock Python server (PID: $MOCK_SERVER_PID)...${NC}"
        kill $MOCK_SERVER_PID 2>/dev/null || true
        wait $MOCK_SERVER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

echo -e "${GREEN}🧪 Python-Rust Integration Test Suite${NC}"
echo "==========================================="

# Check if Python service is already running
if curl -s -f "http://localhost:$PORT/health" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Python service detected at http://localhost:$PORT${NC}"
    export LLM_SERVICE_URL="http://localhost:$PORT"
elif [ "$USE_MOCK" = true ]; then
    echo -e "${YELLOW}Starting mock Python server on port $PORT...${NC}"
    
    # Check Python dependencies
    if ! python3 -c "import flask" 2>/dev/null; then
        echo -e "${YELLOW}Installing Flask for mock server...${NC}"
        pip3 install flask --user
    fi
    
    # Start mock server in background
    python3 "$SCRIPT_DIR/mock_python_server.py" $PORT > /tmp/mock_server.log 2>&1 &
    MOCK_SERVER_PID=$!
    
    # Wait for server to start
    echo -n "Waiting for mock server to start"
    for i in {1..30}; do
        if curl -s -f "http://localhost:$PORT/health" > /dev/null 2>&1; then
            echo -e " ${GREEN}✓${NC}"
            break
        fi
        echo -n "."
        sleep 0.5
    done
    
    if ! curl -s -f "http://localhost:$PORT/health" > /dev/null 2>&1; then
        echo -e " ${RED}✗${NC}"
        echo -e "${RED}Failed to start mock server. Check /tmp/mock_server.log${NC}"
        exit 1
    fi
    
    export LLM_SERVICE_URL="http://localhost:$PORT"
else
    echo -e "${YELLOW}⚠️  No Python service detected at http://localhost:$PORT${NC}"
    echo ""
    echo "You can either:"
    echo "  1. Start the real Python service:"
    echo "     cd python/llm-service && python3 main.py"
    echo ""
    echo "  2. Run with mock server:"
    echo "     $0 --mock"
    echo ""
    exit 1
fi

echo ""
echo "Running integration tests..."
echo "-----------------------------"

cd "$PROJECT_DIR"

# Run specific integration tests
echo -e "${YELLOW}Test 1: Tool Selection Contract${NC}"
cargo test --test integration_python_rust test_tool_selection_contract -- --nocapture

echo -e "${YELLOW}Test 2: Tool Execution Contract${NC}"
cargo test --test integration_python_rust test_tool_execution_contract -- --nocapture

echo -e "${YELLOW}Test 3: Tool Discovery Integration${NC}"
cargo test --test integration_python_rust test_tool_discovery_integration -- --nocapture

echo -e "${YELLOW}Test 4: Cache Integration${NC}"
cargo test --test integration_python_rust test_tool_cache_with_python -- --nocapture

echo -e "${YELLOW}Test 5: Error Handling${NC}"
cargo test --test integration_python_rust test_error_handling_contract -- --nocapture

echo -e "${YELLOW}Test 6: Full Contract Suite${NC}"
cargo test --test integration_python_rust test_full_python_rust_contract_suite -- --nocapture

echo ""
echo -e "${GREEN}✅ All integration tests completed!${NC}"

if [ "$USE_MOCK" = true ]; then
    echo ""
    echo -e "${YELLOW}Note: Tests ran against mock server. For full validation, run against real Python service.${NC}"
fi