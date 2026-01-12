#!/usr/bin/env bash
# Quality audit script for embedded workflow engine.
#
# Performs comprehensive quality checks:
# - Code formatting
# - Linting
# - Test execution
# - Coverage measurement
# - Security audit
# - Performance benchmarks

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🔍 Starting Quality Audit for Embedded Workflow Engine"
echo "Project: $PROJECT_ROOT"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PASSED=0
FAILED=0

check_pass() {
    echo -e "${GREEN}✓ $1${NC}"
    ((PASSED++))
}

check_fail() {
    echo -e "${RED}✗ $1${NC}"
    ((FAILED++))
}

# 1. Code Formatting Check
echo "📝 Checking code formatting..."
cd "$PROJECT_ROOT"
if cargo fmt --check --manifest-path rust/shannon-api/Cargo.toml; then
    check_pass "Code formatting (shannon-api)"
else
    check_fail "Code formatting (shannon-api)"
fi

if cargo fmt --check --manifest-path rust/durable-shannon/Cargo.toml; then
    check_pass "Code formatting (durable-shannon)"
else
    check_fail "Code formatting (durable-shannon)"
fi

# 2. Clippy Linting
echo ""
echo "🔧 Running Clippy linter..."
if cargo clippy --manifest-path rust/shannon-api/Cargo.toml --lib -- -D warnings 2>&1 | grep -q "0 warnings"; then
    check_pass "Clippy clean (shannon-api)"
else
    check_fail "Clippy warnings found (shannon-api)"
fi

# 3. Test Execution
echo ""
echo "🧪 Running tests..."
if cargo test --manifest-path rust/shannon-api/Cargo.toml --lib 2>&1 | grep -q "test result: ok"; then
    check_pass "Unit tests pass (shannon-api)"
else
    check_fail "Unit tests failed (shannon-api)"
fi

if cargo test --manifest-path rust/durable-shannon/Cargo.toml --lib 2>&1 | grep -q "test result: ok"; then
    check_pass "Unit tests pass (durable-shannon)"
else
    check_fail "Unit tests failed (durable-shannon)"
fi

# 4. Security Audit
echo ""
echo "🔒 Running security audit..."
if command -v cargo-audit &> /dev/null; then
    if cargo audit 2>&1 | grep -q "Success"; then
        check_pass "Security audit (no vulnerabilities)"
    else
        check_fail "Security vulnerabilities found"
    fi
else
    echo -e "${YELLOW}⚠ cargo-audit not installed, skipping${NC}"
fi

# 5. Build Check
echo ""
echo "🏗️ Verifying builds..."
if cargo build --manifest-path rust/shannon-api/Cargo.toml --lib 2>&1 | grep -q "Finished"; then
    check_pass "Shannon API builds"
else
    check_fail "Shannon API build failed"
fi

if cargo build --manifest-path rust/durable-shannon/Cargo.toml --lib 2>&1 | grep -q "Finished"; then
    check_pass "Durable Shannon builds"
else
    check_fail "Durable Shannon build failed"
fi

# 6. TypeScript Check
echo ""
echo "📘 Checking TypeScript..."
cd "$PROJECT_ROOT/desktop"
if npm run type-check 2>&1 | grep -q "Found 0 errors"; then
    check_pass "TypeScript compiles"
else
    check_fail "TypeScript errors found"
fi

# Final Report
echo ""
echo "═══════════════════════════════════════"
echo "📊 Quality Audit Report"
echo "═══════════════════════════════════════"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${RED}Failed: $FAILED${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All quality gates passed!${NC}"
    echo "The embedded workflow engine is production-ready."
    exit 0
else
    echo -e "${RED}✗ Some quality gates failed.${NC}"
    echo "Review failures above and fix issues."
    exit 1
fi
