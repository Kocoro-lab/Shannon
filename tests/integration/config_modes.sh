#!/bin/bash
# ============================================================================
# Shannon Configuration Mode Tests
# ============================================================================
# Validates that all configuration combinations work correctly
# and that invalid combinations fail with helpful error messages.
#
# Usage:
#   ./tests/integration/config_modes.sh
#   ./tests/integration/config_modes.sh ./target/release/shannon-api
# ============================================================================

set -e

# Default binary path (can be overridden via first argument)
BINARY="${1:-./target/release/shannon-api}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test counters
PASSED=0
FAILED=0

echo "============================================"
echo "Shannon Configuration Mode Tests"
echo "============================================"
echo ""

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo -e "${YELLOW}Binary not found at $BINARY${NC}"
    echo "Building embedded mode binary..."
    cargo build -p shannon-api --no-default-features --features "embedded,gateway" --release
fi

# Helper function to run a test
run_test() {
    local name="$1"
    local expected_result="$2"  # "success" or "failure"
    local expected_pattern="$3"  # Pattern to match in output (for failures)
    shift 3
    
    echo -n "Testing: $name... "
    
    # Set up environment
    for var in "$@"; do
        export $var
    done
    
    # Run the binary with a short timeout
    set +e
    OUTPUT=$(timeout 5 "$BINARY" 2>&1)
    EXIT_CODE=$?
    set -e
    
    # Clean up environment
    for var in "$@"; do
        unset "${var%%=*}"
    done
    
    if [ "$expected_result" == "success" ]; then
        # For success tests, we expect the server to start (timeout = success)
        if [ $EXIT_CODE -eq 124 ]; then
            echo -e "${GREEN}PASSED${NC}"
            ((PASSED++))
            return 0
        else
            echo -e "${RED}FAILED${NC} (exit code: $EXIT_CODE)"
            echo "Output: $OUTPUT"
            ((FAILED++))
            return 1
        fi
    else
        # For failure tests, check that it fails with the right message
        if [ $EXIT_CODE -ne 0 ] && [ $EXIT_CODE -ne 124 ]; then
            if echo "$OUTPUT" | grep -qi "$expected_pattern"; then
                echo -e "${GREEN}PASSED${NC}"
                ((PASSED++))
                return 0
            else
                echo -e "${RED}FAILED${NC} (wrong error message)"
                echo "Expected pattern: $expected_pattern"
                echo "Got: $OUTPUT"
                ((FAILED++))
                return 1
            fi
        else
            echo -e "${RED}FAILED${NC} (should have failed but didn't)"
            ((FAILED++))
            return 1
        fi
    fi
}

# Create temp data directory
TEMP_DATA=$(mktemp -d)
trap "rm -rf $TEMP_DATA" EXIT

echo "--- Valid Configuration Tests ---"
echo ""

# Test 1: Local mode (embedded + durable + surrealdb)
run_test "Embedded + Durable + SurrealDB (local mode)" "success" "" \
    "SHANNON_MODE=embedded" \
    "WORKFLOW_ENGINE=durable" \
    "DATABASE_DRIVER=surrealdb" \
    "SURREALDB_PATH=$TEMP_DATA/test.db" \
    "ANTHROPIC_API_KEY=test-key-12345"

echo ""
echo "--- Invalid Configuration Tests ---"
echo ""

# Test 2: Embedded mode cannot use Temporal
run_test "Embedded + Temporal = INVALID" "failure" "Embedded.*temporal\|WORKFLOW_ENGINE=durable" \
    "SHANNON_MODE=embedded" \
    "WORKFLOW_ENGINE=temporal" \
    "DATABASE_DRIVER=surrealdb" \
    "SURREALDB_PATH=$TEMP_DATA/test.db" \
    "ANTHROPIC_API_KEY=test-key-12345"

# Test 3: Cloud mode cannot use SurrealDB
run_test "Cloud + SurrealDB = INVALID" "failure" "Cloud.*surrealdb\|PostgreSQL" \
    "SHANNON_MODE=cloud" \
    "WORKFLOW_ENGINE=temporal" \
    "DATABASE_DRIVER=surrealdb" \
    "ANTHROPIC_API_KEY=test-key-12345"

# Test 4: Cloud mode cannot use Durable
run_test "Cloud + Durable = INVALID" "failure" "Cloud.*durable\|Temporal" \
    "SHANNON_MODE=cloud" \
    "WORKFLOW_ENGINE=durable" \
    "DATABASE_DRIVER=postgresql" \
    "DATABASE_URL=postgres://localhost/test" \
    "ANTHROPIC_API_KEY=test-key-12345"

# Test 5: Embedded mode cannot use PostgreSQL
run_test "Embedded + PostgreSQL = INVALID" "failure" "Embedded.*postgresql\|surrealdb" \
    "SHANNON_MODE=embedded" \
    "WORKFLOW_ENGINE=durable" \
    "DATABASE_DRIVER=postgresql" \
    "DATABASE_URL=postgres://localhost/test" \
    "ANTHROPIC_API_KEY=test-key-12345"

echo ""
echo "--- Missing Required Configuration Tests ---"
echo ""

# Test 6: Missing API key produces helpful error
run_test "Missing LLM API key = helpful error" "failure" "ANTHROPIC_API_KEY\|OPENAI_API_KEY\|LLM API Key" \
    "SHANNON_MODE=embedded" \
    "WORKFLOW_ENGINE=durable" \
    "DATABASE_DRIVER=surrealdb" \
    "SURREALDB_PATH=$TEMP_DATA/test.db"

echo ""
echo "============================================"
echo "Test Results"
echo "============================================"
echo -e "Passed: ${GREEN}$PASSED${NC}"
echo -e "Failed: ${RED}$FAILED${NC}"
echo ""

if [ $FAILED -gt 0 ]; then
    echo -e "${RED}Some tests failed!${NC}"
    exit 1
else
    echo -e "${GREEN}All configuration tests passed!${NC}"
    exit 0
fi
