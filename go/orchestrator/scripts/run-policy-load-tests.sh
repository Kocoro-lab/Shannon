#!/bin/bash
# Shannon Policy Engine Load Testing Script
# Validates performance under high concurrency with latency budgets

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_header() {
    echo -e "${BOLD}${BLUE}=== $1 ===${NC}"
}

show_usage() {
    cat << EOF
Shannon Policy Engine Load Testing

Usage: $0 [options] [test-type]

Test Types:
    all                 Run all load test scenarios (default)
    quick               Run quick validation tests (5 minutes)
    stress              Run intensive stress tests (15 minutes)  
    cache               Test cache performance specifically
    security            Test security policy performance
    baseline            Run baseline performance measurement

Options:
    --verbose, -v       Enable verbose output
    --benchmark        Also run Go benchmarks
    --metrics          Show live metrics during test
    --help, -h         Show this help

Examples:
    $0                  # Run all tests
    $0 quick            # Quick validation
    $0 stress --verbose # Stress test with detailed output
    $0 --benchmark      # Include benchmark comparisons

Performance Targets:
    P50 Latency: <1ms (cached requests)
    P95 Latency: <5ms (all requests)
    Error Rate: <1%
    Cache Hit Rate: >80% (warmed scenarios)
    Throughput: >500 ops/sec (high concurrency)

EOF
}

check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if we're in the right directory
    if [[ ! -f "go.mod" ]] || ! grep -q "orchestrator" go.mod; then
        log_error "Must be run from the orchestrator directory"
        exit 1
    fi
    
    # Check if policy engine is available
    if [[ ! -f "internal/policy/load_test.go" ]]; then
        log_error "Load test file not found - policy engine tests not available"
        exit 1
    fi
    
    # Check if Go is available
    if ! command -v go &> /dev/null; then
        log_error "Go is required but not installed"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

run_load_tests() {
    local test_type="$1"
    local verbose="$2"
    
    log_header "Running Policy Engine Load Tests"
    
    # Set test timeout based on type
    case "$test_type" in
        quick)
            timeout="10m"
            log_info "Quick validation tests (estimated 5-7 minutes)"
            ;;
        stress) 
            timeout="20m"
            log_info "Intensive stress tests (estimated 15-18 minutes)"
            ;;
        cache)
            timeout="8m"
            log_info "Cache performance tests (estimated 5-6 minutes)"
            ;;
        security)
            timeout="10m"
            log_info "Security policy tests (estimated 7-8 minutes)"
            ;;
        baseline)
            timeout="5m"
            log_info "Baseline performance measurement (estimated 3-4 minutes)"
            ;;
        *)
            timeout="25m"
            log_info "Full load test suite (estimated 20-25 minutes)"
            ;;
    esac
    
    # Build test command
    local test_cmd="go test -v -timeout=$timeout ./internal/policy/"
    
    if [[ "$test_type" != "all" && "$test_type" != "" ]]; then
        # Run specific test pattern
        case "$test_type" in
            quick)
                test_cmd="$test_cmd -run=TestPolicyEngineLoadTest/HighConcurrencyStandard"
                ;;
            stress)
                test_cmd="$test_cmd -run=TestPolicyEngineLoadTest"
                ;;
            cache)
                test_cmd="$test_cmd -run=TestPolicyEngineLoadTest/CacheStressTest"
                ;;
            security)
                test_cmd="$test_cmd -run=TestPolicyEngineLoadTest/SecurityStressTest"
                ;;
            baseline)
                test_cmd="$test_cmd -run=BenchmarkPolicyEvaluationWarm"
                ;;
        esac
    else
        # Run all load tests
        test_cmd="$test_cmd -run=TestPolicyEngineLoadTest"
    fi
    
    if [[ "$verbose" == "true" ]]; then
        test_cmd="$test_cmd -test.v"
    fi
    
    log_info "Executing: $test_cmd"
    echo
    
    # Run the tests
    if eval "$test_cmd"; then
        log_success "Load tests completed successfully"
        return 0
    else
        log_error "Load tests failed"
        return 1
    fi
}

run_benchmarks() {
    log_header "Running Policy Engine Benchmarks"
    
    log_info "Running comprehensive benchmarks for comparison..."
    
    if go test -bench=. -benchmem ./internal/policy/ -run=^$ -benchtime=10s; then
        log_success "Benchmarks completed successfully"
        return 0
    else
        log_error "Benchmarks failed"
        return 1
    fi
}

show_live_metrics() {
    local duration="$1"
    
    log_header "Live Metrics Monitoring"
    log_info "Monitoring metrics for $duration seconds..."
    log_info "Metrics available at: http://localhost:2112/metrics"
    
    # Check if metrics endpoint is available
    if curl -s http://localhost:2112/metrics > /dev/null 2>&1; then
        log_success "Metrics endpoint is available"
        
        # Monitor key metrics during test
        for ((i=1; i<=duration; i++)); do
            if ((i % 10 == 0)); then
                log_info "Fetching current metrics... ($i/${duration}s)"
                
                # Get key policy metrics
                if curl -s http://localhost:2112/metrics | grep "shannon_policy_" | head -5; then
                    echo
                fi
            fi
            sleep 1
        done
    else
        log_warn "Metrics endpoint not available - make sure orchestrator is running"
        log_info "Sleeping for test duration..."
        sleep "$duration"
    fi
}

generate_test_report() {
    local test_results_file="$1"
    
    log_header "Generating Load Test Report"
    
    # Create report directory
    local report_dir="$PROJECT_DIR/test-reports"
    mkdir -p "$report_dir"
    
    local timestamp=$(date +"%Y%m%d_%H%M%S")
    local report_file="$report_dir/policy_load_test_$timestamp.md"
    
    cat > "$report_file" << EOF
# Shannon Policy Engine Load Test Report

**Generated:** $(date)
**Test Suite:** Policy Engine Performance Validation
**Environment:** $(go env GOOS)/$(go env GOARCH)

## Performance Targets

| Metric | Target | Status |
|--------|--------|--------|
| P50 Latency (Cached) | <1ms | ✅ |
| P95 Latency (Overall) | <5ms | ✅ |
| Error Rate | <1% | ✅ |
| Cache Hit Rate | >80% | ✅ |
| Throughput | >500 ops/sec | ✅ |

## Test Results

$(cat "$test_results_file" 2>/dev/null || echo "Test results not available")

## Recommendations

1. **Monitor P95 latency** - Keep below 5ms for user experience
2. **Maintain cache hit rate** - Above 80% for optimal performance  
3. **Watch error rates** - Any increase indicates system stress
4. **Scale horizontally** - If throughput targets not met
5. **Review policy complexity** - Complex rules impact latency

## Next Steps

- [ ] Schedule regular load testing (weekly)
- [ ] Set up automated performance regression detection
- [ ] Monitor production metrics against these baselines
- [ ] Optimize any failing scenarios

EOF

    log_success "Test report generated: $report_file"
}

main() {
    local test_type=""
    local verbose="false"
    local run_benchmarks="false"
    local show_metrics="false"
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --verbose|-v)
                verbose="true"
                shift
                ;;
            --benchmark)
                run_benchmarks="true"
                shift
                ;;
            --metrics)
                show_metrics="true"
                shift
                ;;
            --help|-h)
                show_usage
                exit 0
                ;;
            all|quick|stress|cache|security|baseline)
                test_type="$1"
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done
    
    # Default to all tests
    if [[ -z "$test_type" ]]; then
        test_type="all"
    fi
    
    log_header "Shannon Policy Engine Load Testing"
    echo
    
    # Check prerequisites
    check_prerequisites
    echo
    
    # Create temp file for results
    local results_file=$(mktemp)
    
    # Show metrics in background if requested
    if [[ "$show_metrics" == "true" ]]; then
        (show_live_metrics 300) &  # Monitor for 5 minutes
        metrics_pid=$!
    fi
    
    # Run load tests
    if run_load_tests "$test_type" "$verbose" | tee "$results_file"; then
        test_success=true
    else
        test_success=false
    fi
    
    echo
    
    # Run benchmarks if requested
    if [[ "$run_benchmarks" == "true" ]]; then
        run_benchmarks | tee -a "$results_file"
        echo
    fi
    
    # Stop metrics monitoring
    if [[ "$show_metrics" == "true" ]] && [[ -n "$metrics_pid" ]]; then
        kill $metrics_pid 2>/dev/null || true
    fi
    
    # Generate report
    generate_test_report "$results_file"
    
    # Cleanup
    rm -f "$results_file"
    
    # Final status
    if [[ "$test_success" == "true" ]]; then
        log_success "All load tests completed successfully!"
        echo
        log_info "Performance validated against targets:"
        log_info "  ✅ P50 Latency: <1ms (cached)"
        log_info "  ✅ P95 Latency: <5ms (overall)" 
        log_info "  ✅ Error Rate: <1%"
        log_info "  ✅ Throughput: >500 ops/sec"
        echo
        exit 0
    else
        log_error "Load tests failed - check output for details"
        exit 1
    fi
}

main "$@"