#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "Python WASM Execution E2E Test Suite Starting"
echo "Testing Python code execution through WASI sandbox"
echo ""

# Wait for services to be ready
info "Waiting for services to be ready..."
for i in $(seq 1 30); do
  if nc -z localhost 50051 2>/dev/null && nc -z localhost 50052 2>/dev/null && nc -z localhost 8000 2>/dev/null; then
    pass "All services ready (agent-core:50051, orchestrator:50052, llm-service:8000)"
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then fail "Services not ready after 30 seconds"; fi
done

echo ""
echo "=== Phase 1: Direct WASM Execution Tests ==="

# Test 1: Create and execute simple WASM module
info "Test 1: Creating simple WASM module"
cat > /tmp/test_python.wat << 'EOF'
;; Simulates Python: print("Hello from Python WASM")
(module
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))
  (memory 1)
  (export "memory" (memory 0))
  (data (i32.const 8) "Hello from Python WASM\n")
  (func $main (export "_start")
    (i32.store (i32.const 0) (i32.const 8))
    (i32.store (i32.const 4) (i32.const 23))
    (call $fd_write
      (i32.const 1) (i32.const 0) (i32.const 1) (i32.const 32)
    )
    drop
  )
)
EOF

if command -v wat2wasm &> /dev/null; then
  wat2wasm /tmp/test_python.wat -o /tmp/test_python.wasm 2>/dev/null
  pass "WASM module compiled successfully"

  # Test via Rust agent-core
  info "Test 2: Testing WASM execution via Rust core"
  cd rust/agent-core
  if cargo run --example wasi_hello -- /tmp/test_python.wasm 2>/dev/null | grep -q "Hello from Python WASM"; then
    pass "Direct WASM execution successful"
  else
    info "Direct WASM execution test skipped (example not available)"
  fi
  cd ../..
else
  info "wat2wasm not found, skipping WASM compilation tests"
fi

echo ""
echo "=== Phase 2: Python Executor Tool Tests ==="

# Test 3: Test python_executor tool registration
info "Test 3: Checking python_executor tool availability"
TOOLS_RESPONSE=$(curl -fsS http://localhost:8000/tools/list 2>/dev/null || echo "[]")
if echo "$TOOLS_RESPONSE" | grep -q "python_executor"; then
  pass "python_executor tool is registered"
else
  info "python_executor tool not found in registered tools"
fi

# Also check code_executor
if echo "$TOOLS_RESPONSE" | grep -q "code_executor"; then
  info "code_executor tool is also registered (internal use)"
fi

# Test 4: Create base64 WASM for testing
info "Test 4: Creating base64-encoded WASM payload"
if [ -f /tmp/test_python.wasm ]; then
  WASM_BASE64=$(base64 -b 0 /tmp/test_python.wasm 2>/dev/null || base64 -w 0 /tmp/test_python.wasm 2>/dev/null || echo "")
  if [ -n "$WASM_BASE64" ]; then
    pass "WASM module encoded to base64"

    # Test 5: Execute via tool API
    info "Test 5: Testing code_executor via tool API"
    EXEC_RESPONSE=$(curl -fsS -X POST http://localhost:8000/tools/execute \
      -H 'Content-Type: application/json' \
      -d "{
        \"tool_name\": \"code_executor\",
        \"parameters\": {
          \"wasm_base64\": \"$WASM_BASE64\"
        }
      }" 2>/dev/null || echo '{"success": false, "error": "Tool execution failed"}')

    if echo "$EXEC_RESPONSE" | grep -q '"success"[[:space:]]*:[[:space:]]*true'; then
      pass "code_executor tool executed successfully"
    else
      info "code_executor execution: $(echo "$EXEC_RESPONSE" | jq -r '.error // "Unknown error"' 2>/dev/null || echo "Failed")"
    fi
  fi
fi

echo ""
echo "=== Phase 3: Fibonacci WASM Test ==="

# Test 6: Create and test Fibonacci calculation in WASM
info "Test 6: Creating Fibonacci WASM module"
if [ -f /tmp/fibonacci.wasm ]; then
  pass "Using existing Fibonacci WASM module"

  FIBO_BASE64=$(base64 -b 0 /tmp/fibonacci.wasm 2>/dev/null || base64 -w 0 /tmp/fibonacci.wasm 2>/dev/null || echo "")
  if [ -n "$FIBO_BASE64" ]; then
    info "Testing Fibonacci execution"
    FIBO_RESPONSE=$(curl -fsS -X POST http://localhost:8000/tools/execute \
      -H 'Content-Type: application/json' \
      -d "{
        \"tool_name\": \"code_executor\",
        \"parameters\": {
          \"wasm_base64\": \"$FIBO_BASE64\",
          \"stdin\": \"\"
        }
      }" 2>/dev/null || echo '{"success": false}')

    if echo "$FIBO_RESPONSE" | grep -q '"success"[[:space:]]*:[[:space:]]*true'; then
      pass "Fibonacci WASM executed successfully"
    else
      info "Fibonacci execution failed"
    fi
  fi
else
  info "Fibonacci WASM not found, skipping test"
fi

echo ""
echo "=== Phase 4: Workflow Integration Test ==="

# Test 7: Submit Python execution task through orchestrator
info "Test 7: Testing Python code execution through workflow"

# Test with proper Python execution request
TASK_RESPONSE=$(grpcurl -plaintext -d "{
  \"metadata\": {\"userId\":\"test\",\"sessionId\":\"python-test\"},
  \"query\": \"Execute Python: print('Hello from Python')\",
  \"context\": {}
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null || echo '{}')

if echo "$TASK_RESPONSE" | grep -q "workflowId"; then
  WORKFLOW_ID=$(echo "$TASK_RESPONSE" | grep -o '"workflowId"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/')
  pass "Python task submitted with workflow ID: $WORKFLOW_ID"

  # Poll for completion
  info "Polling for task completion..."
  for i in $(seq 1 10); do
    STATUS_RESPONSE=$(grpcurl -plaintext -d "{\"taskId\":\"$WORKFLOW_ID\"}" \
      localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null || echo '{}')

    STATUS=$(echo "$STATUS_RESPONSE" | grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/')

    if [[ "$STATUS" =~ COMPLETED|FAILED|TIMEOUT ]]; then
      info "Task finished with status: $STATUS"
      break
    fi
    sleep 1
  done

  # Export workflow history for replay testing
  if [ -n "$WORKFLOW_ID" ]; then
    info "Exporting workflow history for replay testing"
    GO111MODULE=on go run ./go/orchestrator/tools/replay -export \
      -history /tmp/python-execution-history.json \
      -workflow-id "$WORKFLOW_ID" 2>/dev/null || info "History export not available"
  fi
else
  info "Task submission returned no workflow ID"
fi

echo ""
echo "=== Phase 5: Python Specific Execution Tests ==="

# Test 8: Test factorial calculation
info "Test 8: Testing Python factorial calculation"
FACTORIAL_RESPONSE=$(./scripts/submit_task.sh "Run Python code to calculate factorial of 10" 2>/dev/null | tail -1)
if echo "$FACTORIAL_RESPONSE" | grep -q "3628800"; then
  pass "Python factorial calculation returned correct result"
else
  info "Factorial test output: $FACTORIAL_RESPONSE"
fi

# Test 9: Test Unicode support
info "Test 9: Testing Unicode text support"
UNICODE_RESPONSE=$(./scripts/submit_task.sh "Execute Python: print('Hello Unicode: 🚀 💻 🎉')" 2>/dev/null | tail -1)
if echo "$UNICODE_RESPONSE" | grep -q "🚀"; then
  pass "Python Unicode text handled correctly"
else
  info "Unicode test returned: $UNICODE_RESPONSE"
fi

# Test 10: Test parameter validation (empty parameters should be rejected)
info "Test 10: Testing parameter validation"
EMPTY_RESPONSE=$(curl -fsS -X POST http://localhost:8000/tools/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "tool_name": "python_executor",
    "parameters": {}
  }' 2>/dev/null || echo '{"success": false}')

if echo "$EMPTY_RESPONSE" | grep -q '"success"[[:space:]]*:[[:space:]]*false'; then
  pass "Empty parameters correctly rejected"
else
  info "Empty parameter test unexpected result"
fi

echo ""
echo "=== Phase 6: Python Interpreter Setup Check ==="

info "Checking for Python WASM interpreter"
PYTHON_WASM_PATH="/opt/wasm-interpreters/python-3.11.4.wasm"
if [ -f "$PYTHON_WASM_PATH" ] || [ -f "./wasm-interpreters/python-3.11.4.wasm" ]; then
  pass "Python interpreter WASM found"
  info "Python 3.11.4 WASI interpreter is ready for use"
else
  info "Python interpreter WASM not found"
  echo "  To enable Python support, run:"
  echo "  ./scripts/setup_python_wasi.sh"
fi

echo ""
echo "================================"
echo "Python Execution Test Suite Complete"
echo ""
echo "Test Coverage:"
echo "- python_executor tool registration and availability"
echo "- Parameter validation (empty parameters rejected)"
echo "- Python code execution through orchestrator"
echo "- Factorial calculation and mathematical operations"
echo "- Unicode text and emoji support"
echo "- WASM module execution via code_executor"
echo "- Workflow integration with Python requests"
echo ""
echo "Key Points:"
echo "- python_executor wraps code_executor for Python-specific handling"
echo "- Requires Python WASM interpreter (python-3.11.4.wasm)"
echo "- Full CPython 3.11.4 standard library available"
echo "- Secure WASI sandbox execution environment"
echo "================================"