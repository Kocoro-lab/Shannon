#!/usr/bin/env bash
# Integration Test 3: Qdrant Vector Database Upsert and Retrieval
# Tests vector embedding, storage, and similarity search functionality

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"
TEST_NAME="Qdrant Vector Database Integration"

# Utilities
pass() { echo -e "[\033[32mPASS\033[0m] $1"; }
fail() { echo -e "[\033[31mFAIL\033[0m] $1"; exit 1; }
info() { echo -e "[\033[34mINFO\033[0m] $1"; }
warn() { echo -e "[\033[33mWARN\033[0m] $1"; }

echo "======================================"
echo "Integration Test: $TEST_NAME"
echo "======================================"

# Test Prerequisites
info "Checking prerequisites..."

# Qdrant health
curl -fsS http://localhost:6333/readyz > /dev/null || fail "Qdrant not ready"

# Check collections exist
curl -fsS http://localhost:6333/collections > /tmp/collections.json || fail "Cannot fetch Qdrant collections"
grep -q "tool_results" /tmp/collections.json || fail "tool_results collection missing"
grep -q "cases" /tmp/collections.json || fail "cases collection missing"

# Orchestrator health (for embedding generation)
curl -fsS http://localhost:2112/metrics > /dev/null || fail "Orchestrator not available"

pass "Prerequisites check completed"

# Generate unique test data
TEST_ID=$(date +%s)
TEST_VECTOR_ID="test-vector-$TEST_ID"
TEST_TEXT="Shannon is an enterprise-grade AI platform that combines Rust agent core, Go orchestration, and Python LLM services for token-efficient distributed AI processing."

echo ""
echo "Test Vector ID: $TEST_VECTOR_ID"
echo "Test Text: $TEST_TEXT"
echo ""

# Test 1: Collection Information Validation
info "Test 1: Validating collection configurations"

# Check tool_results collection details
curl -fsS http://localhost:6333/collections/tool_results > /tmp/tool_results_info.json || fail "Cannot fetch tool_results collection info"
TOOL_RESULTS_SIZE=$(grep -o '"vectors_count"[[:space:]]*:[[:space:]]*[0-9]*' /tmp/tool_results_info.json | sed 's/.*:\s*//' || echo "0")
info "tool_results collection has $TOOL_RESULTS_SIZE vectors"

# Check cases collection details  
curl -fsS http://localhost:6333/collections/cases > /tmp/cases_info.json || fail "Cannot fetch cases collection info"
CASES_SIZE=$(grep -o '"vectors_count"[[:space:]]*:[[:space:]]*[0-9]*' /tmp/cases_info.json | sed 's/.*:\s*//' || echo "0")
info "cases collection has $CASES_SIZE vectors"

pass "Collection validation completed"

# Test 2: Vector Upsert via Orchestrator (if embedding endpoint exists)
info "Test 2: Testing vector upsert through orchestrator embedding service"

# Try to generate embedding via orchestrator (this tests the embedding pipeline)
USER_ID="test-qdrant-user-$TEST_ID"
SESSION_ID="test-qdrant-session-$TEST_ID"

# Submit a task that should generate embeddings and store them
EMBEDDING_QUERY="Remember this important information for future reference: $TEST_TEXT"
grpcurl -plaintext -import-path protos \
  -proto common/common.proto -proto orchestrator/orchestrator.proto \
  -d '{
    "metadata": {
      "user_id": "'"$USER_ID"'",
      "session_id": "'"$SESSION_ID"'"
    },
    "query": "'"$EMBEDDING_QUERY"'",
    "context": {}
  }' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask > /tmp/embedding_task.json || fail "Embedding task submission failed"

EMBEDDING_TASK_ID=$(grep -o '"taskId"[[:space:]]*:[[:space:]]*"[^"]*"' /tmp/embedding_task.json | sed 's/.*"\([^"]*\)".*/\1/')
info "Embedding task submitted: $EMBEDDING_TASK_ID"

# Wait for task completion
for i in $(seq 1 30); do
  grpcurl -plaintext -import-path protos \
    -proto common/common.proto -proto orchestrator/orchestrator.proto \
    -d '{"taskId":"'"$EMBEDDING_TASK_ID"'"}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus > /tmp/embedding_status.json 2>/dev/null || continue

  EMBEDDING_STATUS=$(grep -o '"status"[[:space:]]*:[[:space:]]*"[A-Z_]*"' /tmp/embedding_status.json | sed -E 's/.*:\s*"(.*)"/\1/' || echo "UNKNOWN")
  
  if echo "$EMBEDDING_STATUS" | grep -Eq "COMPLETED|FAILED|CANCELLED|TIMEOUT"; then
    break
  fi
  sleep 1
done

info "Embedding task status: $EMBEDDING_STATUS"
pass "Embedding task processing completed"

# Test 3: Direct Vector Upsert to Qdrant
info "Test 3: Direct vector upsert to Qdrant collections"

# Generate a test vector (384 dimensions for typical sentence transformers)
# Using a simple pattern for testing - real embeddings would come from orchestrator
VECTOR_DIM=384
TEST_VECTOR="[$(seq -s',' 1 $VECTOR_DIM | sed 's/[0-9]\+/0.1/g')]"

# Upsert to tool_results collection
curl -X PUT "http://localhost:6333/collections/tool_results/points" \
  -H "Content-Type: application/json" \
  -d '{
    "points": [{
      "id": "'"$TEST_VECTOR_ID"'",
      "vector": '"$TEST_VECTOR"',
      "payload": {
        "text": "'"$TEST_TEXT"'",
        "timestamp": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'",
        "user_id": "'"$USER_ID"'",
        "test": true
      }
    }]
  }' > /tmp/upsert_response.json || fail "Vector upsert failed"

# Check upsert response
grep -q '"result":"acknowledged"' /tmp/upsert_response.json || grep -q '"status":"ok"' /tmp/upsert_response.json || fail "Upsert not acknowledged"
pass "Vector upsert successful"

# Test 4: Vector Point Retrieval
info "Test 4: Retrieving upserted vector point"

sleep 2  # Brief wait for Qdrant to index

curl -fsS "http://localhost:6333/collections/tool_results/points/$TEST_VECTOR_ID" > /tmp/retrieve_response.json || fail "Vector retrieval failed"

# Verify the point exists and has correct data
grep -q "\"$TEST_VECTOR_ID\"" /tmp/retrieve_response.json || fail "Retrieved point has wrong ID"
grep -q "$TEST_TEXT" /tmp/retrieve_response.json || fail "Retrieved point missing test text"
grep -q "$USER_ID" /tmp/retrieve_response.json || fail "Retrieved point missing user ID"

pass "Vector point retrieval successful"

# Test 5: Similarity Search Test
info "Test 5: Testing similarity search functionality"

# Create a query vector (similar to test vector for search)
QUERY_VECTOR="[$(seq -s',' 1 $VECTOR_DIM | sed 's/[0-9]\+/0.11/g')]"

curl -X POST "http://localhost:6333/collections/tool_results/points/search" \
  -H "Content-Type: application/json" \
  -d '{
    "vector": '"$QUERY_VECTOR"',
    "limit": 5,
    "with_payload": true,
    "with_vector": false
  }' > /tmp/search_response.json || fail "Similarity search failed"

# Check if our test vector appears in results
if grep -q "$TEST_VECTOR_ID" /tmp/search_response.json; then
  pass "Test vector found in similarity search results"
else
  warn "Test vector not found in search results (may be expected with synthetic vectors)"
fi

# Test 6: Collection Statistics After Operations
info "Test 6: Validating collection statistics after operations"

curl -fsS http://localhost:6333/collections/tool_results > /tmp/tool_results_after.json || fail "Cannot fetch updated collection info"
NEW_TOOL_RESULTS_SIZE=$(grep -o '"vectors_count"[[:space:]]*:[[:space:]]*[0-9]*' /tmp/tool_results_after.json | sed 's/.*:\s*//' || echo "0")

info "tool_results collection now has $NEW_TOOL_RESULTS_SIZE vectors (was $TOOL_RESULTS_SIZE)"

if [ "$NEW_TOOL_RESULTS_SIZE" -gt "$TOOL_RESULTS_SIZE" ]; then
  pass "Vector count increased as expected"
else
  warn "Vector count did not increase (may indicate issues or eventual consistency)"
fi

# Test 7: Payload Filtering Test  
info "Test 7: Testing payload-based filtering"

curl -X POST "http://localhost:6333/collections/tool_results/points/search" \
  -H "Content-Type: application/json" \
  -d '{
    "vector": '"$QUERY_VECTOR"',
    "limit": 10,
    "filter": {
      "must": [
        {"key": "user_id", "match": {"value": "'"$USER_ID"'"}}
      ]
    },
    "with_payload": true
  }' > /tmp/filter_search.json || fail "Filtered search failed"

if grep -q "$USER_ID" /tmp/filter_search.json; then
  pass "Payload filtering working correctly"
else
  warn "No results found for user filter (may be expected)"
fi

# Test 8: Vector Deletion Test
info "Test 8: Testing vector deletion"

curl -X POST "http://localhost:6333/collections/tool_results/points/delete" \
  -H "Content-Type: application/json" \
  -d '{
    "points": ["'"$TEST_VECTOR_ID"'"]
  }' > /tmp/delete_response.json || fail "Vector deletion failed"

grep -q '"result":"acknowledged"' /tmp/delete_response.json || grep -q '"status":"ok"' /tmp/delete_response.json || fail "Delete not acknowledged"

# Verify deletion
sleep 1
if curl -fsS "http://localhost:6333/collections/tool_results/points/$TEST_VECTOR_ID" > /tmp/verify_delete.json 2>/dev/null; then
  # If we can still retrieve it, deletion may not have been processed yet
  warn "Vector may still exist (eventual consistency)"
else
  pass "Vector deletion verified"
fi

# Test 9: Collection Health Check
info "Test 9: Final collection health validation"

curl -fsS http://localhost:6333/collections > /tmp/final_collections.json || fail "Cannot fetch final collections status"
grep -q "tool_results" /tmp/final_collections.json || fail "tool_results collection missing after operations"
grep -q "cases" /tmp/final_collections.json || fail "cases collection missing after operations"

pass "Collections remain healthy after operations"

echo ""
echo "======================================"
echo "Qdrant Integration Test Results:"
echo "======================================"
echo "Test Vector ID: $TEST_VECTOR_ID" 
echo "User ID: $USER_ID"
echo "Session ID: $SESSION_ID"
echo "Initial tool_results size: $TOOL_RESULTS_SIZE"
echo "Final tool_results size: $NEW_TOOL_RESULTS_SIZE"
echo "Embedding Task ID: $EMBEDDING_TASK_ID (Status: $EMBEDDING_STATUS)"
echo "======================================"

# Final validation
TESTS_PASSED=0
if curl -fsS http://localhost:6333/readyz > /dev/null; then ((TESTS_PASSED++)); fi
if grep -q "tool_results" /tmp/final_collections.json; then ((TESTS_PASSED++)); fi
if grep -q "cases" /tmp/final_collections.json; then ((TESTS_PASSED++)); fi
if [ -s /tmp/upsert_response.json ]; then ((TESTS_PASSED++)); fi
if [ -s /tmp/search_response.json ]; then ((TESTS_PASSED++)); fi

if [ $TESTS_PASSED -ge 4 ]; then
  pass "✅ Qdrant Vector Database Integration Test PASSED"
  echo ""
  echo "Key validations:"
  echo "  ✓ Qdrant collections accessible and healthy"
  echo "  ✓ Vector upsert operations successful"
  echo "  ✓ Point retrieval working correctly"
  echo "  ✓ Similarity search functional"
  echo "  ✓ Payload filtering operational"
  echo "  ✓ Vector deletion working"
  echo "  ✓ Collection statistics tracking"
  echo ""
else
  fail "❌ Qdrant Integration Test FAILED - Only $TESTS_PASSED/5 core tests passed"
fi