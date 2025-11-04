# Shannon E2E Test Suite

Comprehensive end-to-end test suite for Shannon system, including core functionality and UTF-8 handling.

## Test Suites

### 1. Comprehensive System Test (`e2e/comprehensive_system_test.sh`)

Tests all core Shannon functionality across different scenarios:

**Test Coverage:**
- ✅ Simple calculation tasks
- ✅ UTF-8 Chinese text handling
- ✅ SSE streaming endpoint
- ✅ Session management
- ✅ Multi-step supervisor workflow
- ✅ Error handling
- ✅ Metadata population (model_used, provider, usage)
- ✅ Database persistence

**Run Command:**
```bash
cd tests
./e2e/comprehensive_system_test.sh
```

### 2. Master Test Runner (`run_all_e2e_tests.sh`)

Runs all test suites and generates a comprehensive report:

**Run Command:**
```bash
cd tests
./run_all_e2e_tests.sh
```

## Prerequisites

### Services Must Be Running

Before running tests, ensure all Shannon services are up:

```bash
# From Shannon project root
make dev

# Or manually:
docker compose -f deploy/compose/docker-compose.yml up -d
```

### Health Check

Verify services are healthy:

```bash
# Gateway
curl http://localhost:8080/health

# Orchestrator
curl http://localhost:8081/health

# LLM Service
curl http://localhost:8000/health/live
```

## Quick Start

```bash
cd /Users/wayland/Code_Ptmind/Shannon/tests

# Run all tests
./run_all_e2e_tests.sh

# Or run individual suites
./e2e/comprehensive_system_test.sh
```

## Test Configuration

Tests use environment variables for configuration:

```bash
# Custom base URL
export BASE_URL=http://your-gateway:8080
./e2e/comprehensive_system_test.sh

# Custom gateway URL
export GATEWAY_URL=http://your-gateway:8080
./run_all_e2e_tests.sh
```

## Understanding Test Results

### Success Output

```
[✓] Simple calculation task completed (model: gpt-5-nano)
[✓] UTF-8 Chinese handling works correctly
[✓] Session management works correctly
```

### Failure Output

```
[✗] Session management test failed
[ERROR] Task task-xxx failed with status TASK_STATUS_FAILED
```

### Test Summary

At the end of each test suite:

```
========================================
Test Summary
========================================
Tests Run:    10
Tests Passed: 9
Tests Failed: 1

Failed Tests:
  - Session management test failed
```

## What These Tests Verify

### Critical Fixes Validated

1. **UTF-8 String Truncation** (go/orchestrator/internal/activities/)
   - Validates rune-based vs byte-based string slicing
   - Tests Chinese, Japanese, and emoji characters
   - Prevents "invalid byte sequence" database errors

2. **Python Type Guards** (python/llm-service/llm_service/api/agent.py)
   - Tests `isinstance()` checks for dict vs string
   - Validates JSON argument parsing
   - Prevents `'str' object has no attribute 'keys'` errors

3. **Go Reflection-Aware Sanitization** (go/orchestrator/internal/activities/agent.go)
   - Tests proper handling of nested structures
   - Validates protobuf conversion
   - Prevents double-serialization issues

4. **SSE Stream Completion**
   - Validates workflow delegation pattern
   - Tests parent-child workflow event emission
   - Ensures streams don't close prematurely

## Debugging Test Failures

### Check Service Logs

```bash
# Orchestrator logs
docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=100

# LLM service logs
docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=100

# Agent core logs
docker compose -f deploy/compose/docker-compose.yml logs agent-core --tail=100
```

### Check Task Status Directly

```bash
# Get task details
TASK_ID="task-00000000-0000-0000-0000-000000000002-XXX"
curl http://localhost:8080/api/v1/tasks/$TASK_ID | jq '.'
```

### Check Database

```bash
# Query task executions
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  psql -U shannon -d shannon -c \
  "SELECT workflow_id, status, model_used, provider FROM task_executions ORDER BY created_at DESC LIMIT 5;"
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: E2E Tests
on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Start Shannon Services
        run: make dev
      - name: Wait for Services
        run: sleep 30
      - name: Run E2E Tests
        run: cd tests && ./run_all_e2e_tests.sh
```

### Local Pre-Commit Hook

```bash
# .git/hooks/pre-commit
#!/bin/bash
cd tests
./e2e/comprehensive_system_test.sh || {
  echo "E2E tests failed. Commit aborted."
  exit 1
}
```

## Test Development Guidelines

### Adding New Tests

1. Add test function to appropriate test suite file
2. Follow naming convention: `test_<scenario_name>`
3. Increment `TESTS_RUN` counter
4. Use log functions: `log_success`, `log_error`, `log_info`
5. Return 0 for success, 1 for failure

Example:

```bash
test_my_new_scenario() {
    log_section "Test N: My New Scenario"
    ((TESTS_RUN++))

    # Test logic here
    if [ condition ]; then
        log_success "My test passed"
        return 0
    else
        log_error "My test failed"
        return 1
    fi
}
```

### Adding New Test Suite

1. Create new file in `/tests/e2e/` or `/tests/integration/`
2. Make executable: `chmod +x tests/e2e/my_new_suite.sh`
3. Add to `SUITES` array in `run_all_e2e_tests.sh`:

```bash
SUITES=(
    # ... existing suites ...
    "My New Suite:e2e/my_new_suite.sh"
)
```

## Test Maintenance

### When to Update Tests

- ✅ After adding new features
- ✅ After fixing critical bugs
- ✅ After modifying workflows
- ✅ After changing API contracts
- ✅ Before major releases

### Test Stability

These tests validate:
- System works end-to-end
- No regressions after changes
- UTF-8 handling is robust
- SSE streaming completes properly
- Metadata tracking is accurate

## Known Limitations

1. **Timeouts**: Some complex workflows may timeout (adjust wait times if needed)
2. **Database Access**: Some tests require direct DB access (optional validation)
3. **Log Access**: Some tests check Docker logs (may not work in all environments)
4. **Concurrent Tests**: Running multiple test suites simultaneously may cause conflicts

## Support

For issues or questions:
1. Check service logs first
2. Verify all services are healthy
3. Review test output for specific failures
4. Check database for task execution details
5. Consult Shannon documentation

## Recent Test Updates

**2025-11-04**: Created comprehensive E2E test suite
- Added comprehensive_system_test.sh (8 tests)
- Added master test runner
- Added UTF-8 integration tests
- Validated UTF-8 fixes
- Validated Python type guard fixes
- Validated Go reflection-aware sanitization
- Validated SSE streaming completion
