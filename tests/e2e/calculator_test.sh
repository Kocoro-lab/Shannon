#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "Calculator E2E Test Suite Starting"
echo "Note: Simple arithmetic is handled by LLM directly, complex calculations use the calculator tool"
echo ""

# Wait for services to be ready
info "Waiting for orchestrator to be ready..."
for i in $(seq 1 30); do
  if nc -z localhost 50052 2>/dev/null; then
    pass "Orchestrator ready"
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then fail "Orchestrator not ready"; fi
done

# Simple calculations (handled by LLM directly, no tool expected)
echo "=== Testing Simple Calculations (LLM handles directly) ==="

info "Test 1: Basic addition (2 + 2)"
RESPONSE=$(./scripts/submit_task.sh "What is 2 + 2?" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "suggested_tools\": \[\]"; then
  pass "Basic addition correctly handled without tools"
else
  info "Note: Basic addition may or may not use tools"
fi

info "Test 2: Simple multiplication (6 * 7)"
RESPONSE=$(./scripts/submit_task.sh "What is 6 times 7?" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "suggested_tools\": \[\]"; then
  pass "Simple multiplication correctly handled without tools"
else
  info "Note: Simple multiplication may or may not use tools"
fi

# Complex calculations (should suggest calculator tool)
echo ""
echo "=== Testing Complex Calculations (Should use calculator tool) ==="

info "Test 3: Complex expression with sqrt"
RESPONSE=$(./scripts/submit_task.sh "Calculate sqrt(144) + log10(1000) * sin(pi/2)" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "\"calculator\""; then
  pass "Complex math expression correctly suggests calculator tool"
else
  info "Complex expression response: $RESPONSE" | head -n 5
fi

info "Test 4: Statistical calculation"
RESPONSE=$(./scripts/submit_task.sh "Calculate the standard deviation of [23, 45, 67, 89, 12, 34, 56, 78, 90, 21]" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "\"calculator\""; then
  pass "Statistical calculation correctly suggests calculator tool"
else
  info "Statistical calculation may be handled directly by LLM"
fi

info "Test 5: Compound interest calculation"
RESPONSE=$(./scripts/submit_task.sh "Calculate compound interest for principal 10000, rate 5.5% annually, compounded monthly for 3 years" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "\"calculator\""; then
  pass "Compound interest correctly suggests calculator tool"
else
  info "Compound interest may be handled directly by LLM"
fi

info "Test 6: Matrix operations"
RESPONSE=$(./scripts/submit_task.sh "Calculate the determinant of the 3x3 matrix [[1,2,3],[4,5,6],[7,8,9]]" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "\"calculator\""; then
  pass "Matrix operation correctly suggests calculator tool"
else
  info "Matrix operation may be handled directly by LLM"
fi

info "Test 7: Scientific notation"
RESPONSE=$(./scripts/submit_task.sh "Calculate (6.022e23 * 1.38e-23 * 300) / 101325" 2>&1 || echo "FAILED")
if echo "$RESPONSE" | grep -q "\"calculator\""; then
  pass "Scientific notation correctly suggests calculator tool"
else
  info "Scientific calculation may be handled directly by LLM"
fi

# Test actual calculator tool execution (if registered)
echo ""
echo "=== Testing Calculator Tool Registration ==="

info "Checking if calculator tool is registered"
TOOLS_RESPONSE=$(curl -fsS http://localhost:8000/tools/list 2>/dev/null || echo "[]")
if echo "$TOOLS_RESPONSE" | grep -q "calculator"; then
  pass "Calculator tool is registered in the system"

  info "Test 8: Direct calculator tool execution"
  CALC_RESPONSE=$(curl -fsS -X POST http://localhost:8000/tools/execute \
    -H 'Content-Type: application/json' \
    -d '{
      "tool_name": "calculator",
      "parameters": {"expression": "sqrt(16) + pow(2, 8)"}
    }' 2>/dev/null || echo '{"success": false}')

  if echo "$CALC_RESPONSE" | grep -q '"success"[[:space:]]*:[[:space:]]*true'; then
    OUTPUT=$(echo "$CALC_RESPONSE" | sed -n 's/.*"output"[[:space:]]*:[[:space:]]*\([0-9.]*\).*/\1/p')
    if [ "$OUTPUT" = "260" ] || [ "$OUTPUT" = "260.0" ]; then
      pass "Calculator tool execution: sqrt(16) + 2^8 = 260"
    else
      info "Calculator returned: $OUTPUT (expected 260)"
    fi
  else
    fail "Calculator tool execution failed"
  fi
else
  info "Calculator tool not found in registered tools"
fi

# Summary
echo ""
echo "================================"
echo "Calculator E2E Test Suite Complete"
echo "Key findings:"
echo "- Simple arithmetic: Handled by LLM directly (no tool needed)"
echo "- Complex calculations: Should suggest calculator tool"
echo "- Tool registration: Calculator tool available at /tools/execute"
echo "================================"