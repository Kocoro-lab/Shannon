#!/bin/bash

# Pre-push verification script to catch CI failures locally
# Usage: ./scripts/pre-push-check.sh

set -e

echo "🔍 Running pre-push verification checks..."
echo "==========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track overall status
ERRORS=0
WARNINGS=0

# Function to check command exists
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo -e "${RED}❌ $1 is not installed${NC}"
        return 1
    fi
    return 0
}

# Check required tools
echo "📋 Checking required tools..."
check_command go || ERRORS=$((ERRORS + 1))
check_command cargo || ERRORS=$((ERRORS + 1))
check_command python3 || ERRORS=$((ERRORS + 1))

if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}Missing required tools. Please install them first.${NC}"
    exit 1
fi

# 1. Go Build Check
echo ""
echo "🔨 Building Go orchestrator..."
if cd go/orchestrator && go build ./... 2>&1; then
    echo -e "${GREEN}✅ Go build successful${NC}"
else
    echo -e "${RED}❌ Go build failed${NC}"
    ERRORS=$((ERRORS + 1))
fi

# 2. Go Tests (without race detection for speed, just compilation check)
echo ""
echo "🧪 Running Go tests (compilation check)..."
if go test -c ./... 2>&1 | grep -v "no test files" > /dev/null; then
    echo -e "${GREEN}✅ Go tests compile${NC}"
else
    echo -e "${RED}❌ Go test compilation failed${NC}"
    ERRORS=$((ERRORS + 1))
fi

# 3. Rust Build Check
echo ""
echo "🦀 Building Rust agent-core..."
if cd ../../rust/agent-core && RUSTFLAGS="-A warnings" cargo build 2>&1 | grep -v "warning:" > /dev/null; then
    echo -e "${GREEN}✅ Rust build successful${NC}"
else
    echo -e "${RED}❌ Rust build failed${NC}"
    ERRORS=$((ERRORS + 1))
fi

# 4. Rust Tests (compile only for speed)
echo ""
echo "🧪 Compiling Rust tests..."
if RUSTFLAGS="-A warnings" cargo test --no-run 2>&1 | grep -v "warning:" > /dev/null; then
    echo -e "${GREEN}✅ Rust tests compile${NC}"
else
    echo -e "${YELLOW}⚠️  Rust test compilation issues${NC}"
    WARNINGS=$((WARNINGS + 1))
fi

# 5. Python Linting
echo ""
echo "🐍 Checking Python code..."
cd ../../python/llm-service
if command -v ruff &> /dev/null; then
    if ruff check . 2>&1 | grep -E "^\[" > /dev/null; then
        echo -e "${YELLOW}⚠️  Python linting issues found (non-fatal)${NC}"
        WARNINGS=$((WARNINGS + 1))
    else
        echo -e "${GREEN}✅ Python linting clean${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  ruff not installed, skipping Python linting${NC}"
    WARNINGS=$((WARNINGS + 1))
fi

# 6. Check for common issues
echo ""
echo "🔍 Checking for common issues..."

# Check for undefined variables in Go source files only
cd ../../go/orchestrator
if find . -name "*.go" -type f | xargs grep "undefined:" 2>/dev/null | grep -v "vendor"; then
    echo -e "${RED}❌ Found undefined variables in Go code${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✅ No undefined variables${NC}"
fi

# Check for double slashes in Makefile (excluding URLs and Makefile variables)
cd ../..
DOUBLE_SLASH_COUNT=$(grep -E "//[^/]" Makefile | grep -v "http://" | grep -v "https://" | grep -v '\$\$' | wc -l)
if [ "$DOUBLE_SLASH_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Found double slashes in Makefile (may cause issues)${NC}"
    grep -E "//[^/]" Makefile | grep -v "http://" | grep -v "https://" | grep -v '\$\$' | head -3
    WARNINGS=$((WARNINGS + 1))
else
    echo -e "${GREEN}✅ No problematic double slashes in Makefile${NC}"
fi

# Summary
echo ""
echo "==========================================="
echo "📊 Summary:"
echo "  Errors: $ERRORS"
echo "  Warnings: $WARNINGS"

if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}❌ Pre-push check FAILED - fix errors before pushing${NC}"
    exit 1
elif [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Pre-push check passed with warnings${NC}"
    echo "Consider fixing warnings before pushing"
    exit 0
else
    echo -e "${GREEN}✅ All pre-push checks passed!${NC}"
    exit 0
fi