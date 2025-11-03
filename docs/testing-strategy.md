# Shannon Platform - Test Strategy Document
## OpenAI & Anthropic Model Validation

**Version**: 2.0
**Date**: November 2, 2025 (Updated)
**Author**: Claude Code
**Purpose**: Guide for validating Shannon platform after model updates

**⚠️ CRITICAL UPDATE (v2.0)**: All tests must now verify **API response metadata completeness** (model_used, provider, usage tokens, cost). Status code checks alone are insufficient.

---

## Overview

This test strategy outlines the approach for validating the Shannon multi-agent AI platform after major model provider updates, particularly:
- Claude 3 → Claude 4.5 migration
- GPT-5 family integration
- Provider-specific parameter handling updates
- **Metadata population across all workflow types** (v2.0 addition)

---

## Test Pyramid

```
                    ┌─────────────┐
                    │   Manual    │  ← Complex workflows
                    │   E2E       │     Exploratory testing
                    └─────────────┘
                ┌───────────────────┐
                │  Automated E2E    │  ← Provider integration
                │  Tests            │     Model tier selection
                └───────────────────┘
            ┌───────────────────────────┐
            │   Integration Tests       │  ← API validation
            │   (Provider + Gateway)    │     Parameter handling
            └───────────────────────────┘
        ┌───────────────────────────────────┐
        │      Unit Tests                   │  ← Provider logic
        │   (Parameter transforms)          │     Cost calculation
        └───────────────────────────────────┘
```

---

## Test Scope

### 1. Provider Integration Testing

**Objective**: Verify both OpenAI and Anthropic providers work correctly with updated models

**Test Levels**:

#### Level 1: Direct Provider Tests
- Test each model directly with correct parameters
- Verify API response format
- Validate token usage reporting
- Confirm cost estimation

#### Level 2: Tier-Based Selection
- Test small tier (gpt-5-nano-2025-08-07, claude-haiku-4-5-20251001)
- Test medium tier (gpt-5-2025-08-07, claude-sonnet-4-5-20250929)
- Test large tier (gpt-4.1-2025-04-14, claude-opus-4-1-20250805)

#### Level 3: Override Mechanisms
- Direct model override
- Provider override + tier
- Fallback behavior when primary fails

### 2. Parameter Validation

**Critical for GPT-5 Models**:

| Parameter | GPT-5 Models | GPT-4.1 | Claude |
|-----------|--------------|---------|--------|
| max_tokens | ❌ Not supported | ✅ Supported | ✅ Supported |
| max_completion_tokens | ✅ Required | ❌ Not supported | ❌ Not supported |
| temperature | ❌ Default only | ✅ Configurable | ✅ Configurable |
| top_p | ❌ Default only | ✅ Configurable | ✅ Configurable |
| frequency_penalty | ❌ Default only | ✅ Configurable | ✅ Configurable |
| presence_penalty | ❌ Default only | ✅ Configurable | ✅ Configurable |

**Test Approach**:
```python
# Test 1: Verify GPT-5 with max_completion_tokens works
response = await client.chat.completions.create(
    model="gpt-5-nano-2025-08-07",
    messages=[{"role": "user", "content": "test"}],
    max_completion_tokens=50  # Should work
)

# Test 2: Verify GPT-5 with max_tokens fails correctly
try:
    response = await client.chat.completions.create(
        model="gpt-5-nano-2025-08-07",
        messages=[{"role": "user", "content": "test"}],
        max_tokens=50  # Should fail
    )
except Exception as e:
    assert "max_completion_tokens" in str(e)

# Test 3: Verify GPT-4.1 with max_tokens works
response = await client.chat.completions.create(
    model="gpt-4.1-2025-04-14",
    messages=[{"role": "user", "content": "test"}],
    max_tokens=50  # Should work
)
```

### 3. Cost Estimation & Metadata Validation

**Objective**: Verify accurate cost calculation and complete metadata in API responses

**CRITICAL**: All API responses must include execution metadata (model, provider, tokens, cost)

**Test Cases**:
1. Direct model ID cost lookup
2. Alias resolution for cost lookup
3. Fallback cost estimation when usage not reported
4. Multi-tier cost comparison
5. **API response metadata completeness** ⭐

**Example Test** (Updated with metadata verification):
```bash
# Submit task
TASK_ID=$(curl -X POST "http://localhost:8080/api/v1/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"query": "Short test", "model_override": "gpt-5-nano-2025-08-07"}' \
  | jq -r '.task_id')

# Wait for completion
sleep 5

# Get full response
RESPONSE=$(curl -sS "http://localhost:8080/api/v1/tasks/$TASK_ID")

# ✅ CRITICAL: Verify metadata fields in API response
MODEL_USED=$(echo "$RESPONSE" | jq -r '.model_used')
PROVIDER=$(echo "$RESPONSE" | jq -r '.provider')
INPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.input_tokens')
OUTPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.output_tokens')
TOTAL_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.total_tokens')
COST=$(echo "$RESPONSE" | jq -r '.usage.estimated_cost')

# Verify all fields populated
if [ "$MODEL_USED" = "null" ] || [ "$MODEL_USED" = "" ]; then
    echo "❌ FAIL: model_used missing from API response"
    exit 1
fi

if [ "$PROVIDER" = "null" ] || [ "$PROVIDER" = "" ]; then
    echo "❌ FAIL: provider missing from API response"
    exit 1
fi

if [ "$INPUT_TOKENS" = "null" ] || [ "$INPUT_TOKENS" = "0" ]; then
    echo "❌ FAIL: usage.input_tokens missing or zero"
    exit 1
fi

if [ "$OUTPUT_TOKENS" = "null" ] || [ "$OUTPUT_TOKENS" = "0" ]; then
    echo "❌ FAIL: usage.output_tokens missing or zero"
    exit 1
fi

if [ "$COST" = "null" ] || [ "$COST" = "0" ] || [ "$COST" = "0.0" ]; then
    echo "❌ FAIL: usage.estimated_cost missing or zero"
    exit 1
fi

# Verify database persistence
DB_COST=$(PGPASSWORD=shannon psql -h localhost -U shannon -d shannon -t -c \
  "SELECT total_cost_usd FROM task_executions WHERE workflow_id='$TASK_ID';" | xargs)

if [ "$DB_COST" != "$COST" ]; then
    echo "❌ FAIL: Database cost ($DB_COST) doesn't match API ($COST)"
    exit 1
fi

echo "✅ PASS: All metadata verified (model=$MODEL_USED, cost=$COST)"
```

---

## Test Categories

### Category A: Smoke Tests (Quick Validation)
**Duration**: 2-3 minutes  
**Frequency**: After every code change

**Tests**:
1. One test per tier per provider (6 tests total)
2. Verify task submission and completion
3. Check basic result accuracy

**Script**: `/tmp/comprehensive_e2e_test.sh` (simplified version)

### Category B: Full Integration Tests
**Duration**: 15-20 minutes  
**Frequency**: Before merging to main

**Tests**:
1. All tier combinations (small, medium, large)
2. Direct model overrides
3. Provider fallback scenarios
4. Cost estimation accuracy
5. Parameter handling edge cases
6. Streaming responses

### Category C: Real-World Scenario Tests
**Duration**: 30-45 minutes  
**Frequency**: Before release

**Tests**:
1. Calculator tool usage
2. Python code execution
3. Web search and synthesis
4. Multi-agent workflows
5. P2P coordination
6. Memory system integration
7. Context compression
8. Rate limiting behavior

---

## Test Data Strategy

### Arithmetic Tests (Basic Validation)
**Purpose**: Quick validation of model functionality

**Test Data**:
```json
[
  {"query": "15 + 27", "expected": "42", "difficulty": "trivial"},
  {"query": "13 * 8", "expected": "104", "difficulty": "easy"},
  {"query": "60mph * 2.5hrs", "expected": "150", "difficulty": "medium"},
  {"query": "3 items at $12.50 with 20% discount", "expected": "30", "difficulty": "complex"}
]
```

### Logic Tests (Advanced Validation)
**Purpose**: Verify reasoning capabilities

**Test Data**:
```json
[
  {
    "query": "Count words in: The quick brown fox",
    "expected": "5",
    "type": "text_analysis"
  },
  {
    "query": "If all roses are flowers and some flowers fade, do all roses fade?",
    "expected": "no",
    "type": "logical_reasoning"
  }
]
```

---

## Test Environment

### Required Services
- ✅ Temporal (workflow orchestration)
- ✅ PostgreSQL (task storage)
- ✅ Redis (session cache)
- ✅ Qdrant (vector storage)
- ✅ Orchestrator (Go service)
- ✅ LLM Service (Python service)
- ✅ Agent Core (Rust service)

### Environment Variables
```bash
OPENAI_API_KEY=<your-key>
ANTHROPIC_API_KEY=<your-key>
SHANNON_ENV=test
LOG_LEVEL=info
```

### Health Checks
```bash
# Verify all services healthy
docker compose -f deploy/compose/docker-compose.yml ps

# Expected output:
# orchestrator: healthy
# llm-service: healthy
# agent-core: healthy
# temporal: healthy
# postgres: healthy
# redis: healthy
# qdrant: healthy
```

---

## Test Execution Guide

### Pre-Test Checklist
- [ ] All services running and healthy
- [ ] API keys configured
- [ ] Database migrations applied
- [ ] Proto files generated
- [ ] No uncommitted code changes (for baseline tests)

### Test Execution Steps

#### 1. Full Rebuild (Clean Slate)
```bash
# Rebuild all services with no cache
docker compose -f deploy/compose/docker-compose.yml build --no-cache

# Restart all services
docker compose -f deploy/compose/docker-compose.yml up -d

# Wait for health
for i in {1..30}; do
  if docker compose -f deploy/compose/docker-compose.yml ps orchestrator | grep -q "healthy" && \
     docker compose -f deploy/compose/docker-compose.yml ps llm-service | grep -q "healthy"; then
    echo "✅ Services healthy"
    break
  fi
  echo "Waiting... ($i/30)"
  sleep 3
done
```

#### 2. Run Smoke Tests
```bash
bash /tmp/comprehensive_e2e_test.sh
```

Expected output:
```
Total Tests: 8
Passed: 8
Failed: 0
✅ ALL TESTS PASSED!
```

#### 3. Verify API Logs
```bash
# Check OpenAI calls
docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=100 | grep "openai.com.*200 OK"

# Check Anthropic calls
docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=100 | grep "anthropic.com.*200 OK"

# Verify no 400 errors
docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail=100 | grep "400 Bad Request" && echo "❌ Found errors" || echo "✅ No errors"
```

#### 4. Database Verification
```bash
# Check recent tasks
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c \
  "SELECT workflow_id, status, model_used, cost FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

#### 5. Cost Validation
```bash
# Verify non-zero costs
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c \
  "SELECT COUNT(*) FROM tasks WHERE cost > 0;"
```

### Post-Test Cleanup
```bash
# Optional: Clear test data
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c \
  "DELETE FROM tasks WHERE user_id = '00000000-0000-0000-0000-000000000002';"
```

---

## Continuous Integration Strategy

### Git Workflow Integration

```yaml
# .github/workflows/test.yml
name: Shannon Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      
      - name: Setup environment
        run: make setup-env
      
      - name: Build services
        run: docker compose build
      
      - name: Run smoke tests
        run: |
          docker compose up -d
          sleep 30
          bash /tmp/comprehensive_e2e_test.sh
      
      - name: Check logs for errors
        run: |
          docker compose logs llm-service | grep "400 Bad Request" && exit 1 || exit 0
```

### Pre-Merge Requirements
- [ ] All smoke tests passing
- [ ] No 400 errors in logs
- [ ] Cost estimation non-zero for all tasks
- [ ] Both providers (OpenAI + Anthropic) verified

---

## Troubleshooting Guide

### Common Issues

#### Issue 1: GPT-5 Returns 400 Error
**Symptom**:
```
HTTP Request: POST https://api.openai.com/v1/chat/completions "HTTP/1.1 400 Bad Request"
Error: Unsupported parameter: 'max_tokens'
```

**Diagnosis**:
```bash
# Check provider code
grep -n "max_tokens\|max_completion_tokens" python/llm-service/llm_provider/openai_provider.py
```

**Fix**:
Ensure conditional parameter handling is applied (see lines 123-128).

#### Issue 2: Cost Always Zero
**Symptom**:
```sql
SELECT cost FROM tasks WHERE model_used LIKE 'gpt-5%';
-- All return 0.00
```

**Diagnosis**:
```python
# Check if _resolve_alias is used
grep "_resolve_alias" python/llm-service/llm_provider/openai_provider.py
```

**Fix**:
Ensure cost estimation uses `_resolve_alias(model)` instead of `model` directly.

#### Issue 3: Services Not Healthy
**Symptom**:
```
llm-service: starting
orchestrator: starting
```

**Diagnosis**:
```bash
docker compose logs llm-service --tail=50
docker compose logs orchestrator --tail=50
```

**Common Causes**:
- Missing proto files: Run `make proto`
- Port conflicts: Check `lsof -i :8080`
- Dependency issues: Run `go mod tidy` or `pip install -r requirements.txt`

---

## Success Criteria

### Definition of Done
✅ **All tests must meet these criteria**:

1. **Functional**:
   - Task submits successfully
   - Task completes within reasonable time (<30s for simple tasks)
   - Result is mathematically/logically correct

2. **Technical**:
   - API returns 200 OK (not 400, 500)
   - No errors in orchestrator/llm-service logs
   - Correct model used (verify in logs)

3. **Business**:
   - Cost > 0 (not zero)
   - Usage tokens reported
   - Latency acceptable (<5s for simple queries)

4. **⭐ Metadata Completeness** (CRITICAL):
   - API response includes `model_used` (not null/empty)
   - API response includes `provider` (openai/anthropic/etc)
   - API response includes `usage.input_tokens` (>0)
   - API response includes `usage.output_tokens` (>0)
   - API response includes `usage.total_tokens` (>0)
   - API response includes `usage.estimated_cost` (>0)
   - Database `task_executions` has matching metadata
   - Orchestrator logs show expected workflow type executed

### Regression Prevention
**Before each release, verify**:
- [ ] All smoke tests passing
- [ ] No new 400 errors introduced
- [ ] Cost estimation accuracy maintained
- [ ] Both providers working
- [ ] All tiers functional

---

## Future Test Enhancements

### Phase 1: Immediate (Next Sprint)
1. Add GPT-5-pro Responses API test
2. Add streaming response validation
3. Add parameter edge case tests
4. Add cost calculation unit tests

### Phase 2: Medium Term (Next Release)
1. Tool execution tests (calculator, Python)
2. Multi-agent workflow tests
3. P2P coordination validation
4. Memory system integration tests

### Phase 3: Long Term (Future Releases)
1. Performance benchmarking
2. Load testing (concurrent requests)
3. Chaos engineering (service failures)
4. Security testing (injection attacks)

---

## Metrics & Reporting

### Test Metrics to Track
- **Pass Rate**: Target 100% for smoke tests
- **Execution Time**: Target <3 minutes for smoke tests
- **Coverage**: Target 80% code coverage
- **Cost Accuracy**: Target <1% deviation from expected

### Report Template
```markdown
## Test Execution Report

**Date**: YYYY-MM-DD
**Tester**: Name
**Environment**: Test/Staging/Production

### Results
- Total Tests: X
- Passed: Y
- Failed: Z
- Pass Rate: (Y/X * 100)%

### Failed Tests
| Test Name | Expected | Actual | Root Cause |
|-----------|----------|--------|------------|
| ... | ... | ... | ... |

### Actions Required
- [ ] Action 1
- [ ] Action 2
```

---

**Document Version**: 1.0  
**Last Updated**: November 2, 2025  
**Next Review**: Before next major release  
**Owner**: Shannon Platform Team
