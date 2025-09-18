# Shannon Integration Tests

Comprehensive integration tests for validating Shannon's core functionality across all service layers.

## Overview

The integration test suite validates three critical aspects of Shannon's architecture:

1. **Single Agent Flow** - End-to-end task execution through the orchestrator
2. **Session Memory** - Session persistence and context continuity 
3. **Qdrant Vector Database** - Vector operations and similarity search

## Prerequisites

All Shannon services must be running before executing integration tests:

```bash
# Start all services
make dev

# Verify services are healthy  
make smoke
```

Required services:
- Orchestrator (gRPC :50052, metrics :2112)
- Agent Core (gRPC :50051, metrics :2113)  
- LLM Service (HTTP :8000)
- PostgreSQL (:5432)
- Redis (:6379)
- Qdrant (:6333)
- Temporal (:7233, UI :8088)

## Running Tests

### All Integration Tests
```bash
# Run complete integration test suite
make integration-tests
```

### Individual Tests
```bash
# Single agent flow test
make integration-single

# Session memory test  
make integration-session

# Qdrant vector database test
make integration-qdrant
```

### Direct Execution
```bash
# Run master test suite
./tests/integration/run_integration_tests.sh

# Run individual tests
./tests/integration/single_agent_flow_test.sh
./tests/integration/session_memory_test.sh
./tests/integration/qdrant_upsert_test.sh
```

## Test Descriptions

### 1. Single Agent Flow Test (`single_agent_flow_test.sh`)

**Purpose**: Validates the complete execution path for simple tasks using SimpleTaskWorkflow.

**Test Flow**:
1. Submit simple query ("What is 2+2?")
2. Monitor task execution to completion
3. Verify SimpleTaskWorkflow was used
4. Validate database persistence
5. Check agent core interaction
6. Performance validation

**Key Validations**:
- Task submission successful
- Task reaches terminal state (COMPLETED/FAILED)  
- Database persistence working
- Agent-orchestrator communication functional
- Reasonable response time (< 10s for simple tasks)

### 2. Session Memory Test (`session_memory_test.sh`)

**Purpose**: Tests session creation, persistence, and context continuity across multiple tasks.

**Test Flow**:
1. Create session with contextual information
2. Submit follow-up task referencing previous context  
3. Test session continuity with third task
4. Validate session persistence and updates

**Key Validations**:
- Session creation in PostgreSQL
- Multiple task execution in same session
- Database session tracking
- Context retrieval for subsequent tasks
- Redis session caching (if configured)
- Session update activities

### 3. Qdrant Vector Database Test (`qdrant_upsert_test.sh`)

**Purpose**: Validates vector embedding storage, retrieval, and similarity search functionality.

**Test Flow**:
1. Validate collection configurations
2. Test vector upsert through orchestrator
3. Direct vector upsert to Qdrant
4. Vector point retrieval
5. Similarity search operations
6. Payload-based filtering
7. Vector deletion

**Key Validations**:
- Qdrant collections accessible (`tool_results`, `cases`)
- Vector upsert operations successful
- Point retrieval working correctly
- Similarity search functional
- Payload filtering operational
- Vector deletion working
- Collection statistics tracking

## Test Output

Each test provides color-coded output:
- 🟢 **PASS** - Test validation successful
- 🔴 **FAIL** - Test validation failed (stops execution)
- 🔵 **INFO** - Informational messages
- 🟡 **WARN** - Warning (test continues)

Example successful output:
```
======================================
Integration Test: Single Agent Flow
======================================
[INFO] Checking prerequisites...
[PASS] Prerequisites check completed
[INFO] Test 1: Submitting simple task...
[PASS] Task submission successful
[INFO] Task status: COMPLETED (attempt 3/60)
[PASS] Task reached terminal state: COMPLETED
[PASS] ✅ Single Agent Flow Integration Test PASSED
```

## Debugging Failed Tests

### Service Connectivity Issues
```bash
# Check service status
docker compose -f deploy/compose/docker-compose.yml ps

# Check service logs
docker compose -f deploy/compose/docker-compose.yml logs orchestrator
docker compose -f deploy/compose/docker-compose.yml logs agent-core
```

### Database Issues
```bash  
# Check PostgreSQL connectivity
docker compose -f deploy/compose/docker-compose.yml exec postgres psql -U shannon -d shannon -c 'SELECT 1'

# Check recent sessions
docker compose -f deploy/compose/docker-compose.yml exec postgres psql -U shannon -d shannon -c 'SELECT id, user_id, created_at FROM sessions ORDER BY created_at DESC LIMIT 5'
```

### Qdrant Issues
```bash
# Check Qdrant health
curl http://localhost:6333/readyz

# List collections
curl http://localhost:6333/collections

# Check collection stats
curl http://localhost:6333/collections/tool_results
```

### Agent Core Issues
```bash
# Test agent health directly
grpcurl -plaintext -import-path protos -proto agent/agent.proto \
  localhost:50051 shannon.agent.AgentService/HealthCheck
```

## Integration with CI

Integration tests can be added to CI pipelines:

```yaml
# Example GitHub Actions integration
- name: Run Integration Tests
  run: make integration-tests
  env:
    COMPOSE_FILE: deploy/compose/docker-compose.yml
```

For local development, run integration tests after major changes:

```bash
# Development workflow
make ci                    # Build and unit tests
make smoke                # Basic smoke tests  
make integration-tests    # Full integration validation
```

## Test Data Cleanup

Integration tests create test data with unique identifiers:
- User IDs: `test-*-user-{timestamp}`
- Session IDs: `test-*-session-{timestamp}`
- Vector IDs: `test-vector-{timestamp}`

Test data is generally cleaned up automatically, but you can manually clean if needed:

```bash
# Clean test vectors from Qdrant
curl -X POST http://localhost:6333/collections/tool_results/points/delete \
  -H "Content-Type: application/json" \
  -d '{"filter": {"must": [{"key": "test", "match": {"value": true}}]}}'

# Clean test sessions from PostgreSQL
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c "DELETE FROM sessions WHERE user_id LIKE 'test-%';"
```

## Contributing

When adding new integration tests:

1. Follow existing naming patterns (`*_test.sh`)
2. Use consistent output formatting (pass/fail/info/warn functions)
3. Include comprehensive validation steps
4. Add cleanup for test data
5. Update this README with test descriptions
6. Add Makefile targets for new tests