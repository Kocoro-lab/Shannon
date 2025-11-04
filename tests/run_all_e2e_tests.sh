#!/bin/bash
# Master E2E Test Runner
# Runs all comprehensive test suites and generates a report

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_URL="${BASE_URL:-http://localhost:8080}"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Test suites to run
SUITES=(
    "Comprehensive System Test:e2e/comprehensive_system_test.sh"
    "UTF-8 Integration Test:integration/test_chinese_utf8.sh"
)

# Results tracking
TOTAL_SUITES=${#SUITES[@]}
SUITES_PASSED=0
SUITES_FAILED=0
FAILED_SUITES=()

log_header() {
    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║                                                                ║${NC}"
    echo -e "${CYAN}║           Shannon E2E Test Suite Runner                        ║${NC}"
    echo -e "${CYAN}║                                                                ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

log_section() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[✓]${NC} $1"; }
log_error() { echo -e "${RED}[✗]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# Check if services are running
check_services() {
    log_section "Pre-Flight Checks"

    # Check Gateway
    log_info "Checking Gateway service..."
    if curl -sf "$BASE_URL/health" > /dev/null 2>&1; then
        log_success "Gateway is accessible at $BASE_URL"
    else
        log_error "Gateway is not accessible at $BASE_URL"
        log_info "Please start services: make dev"
        exit 1
    fi

    # Check Orchestrator
    log_info "Checking Orchestrator service..."
    if curl -sf "http://localhost:8081/health" > /dev/null 2>&1; then
        log_success "Orchestrator is healthy"
    else
        log_warn "Orchestrator health check failed"
    fi

    # Check LLM Service
    log_info "Checking LLM service..."
    if curl -sf "http://localhost:8000/health/live" > /dev/null 2>&1; then
        log_success "LLM service is healthy"
    else
        log_warn "LLM service health check failed"
    fi

    echo ""
}

# Run a single test suite
run_test_suite() {
    local suite_name="$1"
    local suite_path="$2"
    local full_path="$SCRIPT_DIR/$suite_path"

    log_section "Running: $suite_name"

    if [ ! -f "$full_path" ]; then
        log_error "Test suite not found: $full_path"
        ((SUITES_FAILED++))
        FAILED_SUITES+=("$suite_name: File not found")
        return 1
    fi

    # Make executable
    chmod +x "$full_path"

    # Run the test suite
    log_info "Executing: $suite_path"
    echo ""

    local start_time=$(date +%s)

    if bash "$full_path"; then
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        echo ""
        log_success "$suite_name completed successfully (${duration}s)"
        ((SUITES_PASSED++))
        return 0
    else
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        echo ""
        log_error "$suite_name failed (${duration}s)"
        ((SUITES_FAILED++))
        FAILED_SUITES+=("$suite_name")
        return 1
    fi
}

# Generate final report
generate_report() {
    log_section "Final Test Report"

    echo ""
    echo "┌─────────────────────────────────────────────┐"
    echo "│  Test Execution Summary                     │"
    echo "├─────────────────────────────────────────────┤"
    echo "│  Total Suites:    $TOTAL_SUITES                           │"
    echo -e "│  Passed:          ${GREEN}$SUITES_PASSED${NC}                           │"
    echo -e "│  Failed:          ${RED}$SUITES_FAILED${NC}                           │"
    echo "└─────────────────────────────────────────────┘"
    echo ""

    if [ $SUITES_FAILED -gt 0 ]; then
        echo -e "${RED}Failed Test Suites:${NC}"
        for suite in "${FAILED_SUITES[@]}"; do
            echo "  ✗ $suite"
        done
        echo ""
    fi

    # Overall status
    if [ $SUITES_FAILED -eq 0 ]; then
        echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
        echo -e "${GREEN}║                                        ║${NC}"
        echo -e "${GREEN}║     ✨ ALL TESTS PASSED! ✨           ║${NC}"
        echo -e "${GREEN}║                                        ║${NC}"
        echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
        echo ""
        return 0
    else
        echo -e "${RED}╔════════════════════════════════════════╗${NC}"
        echo -e "${RED}║                                        ║${NC}"
        echo -e "${RED}║     ⚠️  SOME TESTS FAILED ⚠️          ║${NC}"
        echo -e "${RED}║                                        ║${NC}"
        echo -e "${RED}╚════════════════════════════════════════╝${NC}"
        echo ""
        return 1
    fi
}

# Main execution
main() {
    local start_time=$(date +%s)

    log_header

    log_info "Test Environment: $BASE_URL"
    log_info "Started at: $(date)"
    log_info "Total test suites: $TOTAL_SUITES"

    # Run pre-flight checks
    check_services

    # Run each test suite
    for suite_info in "${SUITES[@]}"; do
        IFS=':' read -r suite_name suite_path <<< "$suite_info"
        run_test_suite "$suite_name" "$suite_path" || true
        echo ""
    done

    # Calculate total duration
    local end_time=$(date +%s)
    local total_duration=$((end_time - start_time))
    local minutes=$((total_duration / 60))
    local seconds=$((total_duration % 60))

    # Generate final report
    generate_report

    log_info "Total execution time: ${minutes}m ${seconds}s"
    log_info "Completed at: $(date)"

    # Exit with appropriate code
    if [ $SUITES_FAILED -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Handle interrupts
trap 'echo ""; log_warn "Tests interrupted by user"; exit 130' INT TERM

# Run main function
main "$@"
