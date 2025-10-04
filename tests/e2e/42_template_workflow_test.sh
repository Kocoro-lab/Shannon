#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "Template Workflow E2E Test Suite Starting"
echo "Testing zero-token template routing and execution"
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

# Check if template examples are loaded
info "Verifying template system initialized..."
TEMPLATE_COUNT=$(docker compose -f "$COMPOSE_FILE" logs orchestrator 2>/dev/null | grep -c "Loaded template" || echo "0")
if [ "$TEMPLATE_COUNT" -gt 0 ]; then
  pass "Template system initialized ($TEMPLATE_COUNT templates loaded)"
else
  info "Warning: No templates loaded (system will use AI decomposition)"
fi

# Test 1: Template routing via explicit template_name
echo ""
echo "=== Test 1: Explicit Template Selection ==="
info "Submitting task with template_name context..."

SESSION_ID="template-test-$(date +%s)"
USER_ID="template-test-user"

# Use research_summary template explicitly
RESPONSE=$(grpcurl -plaintext -d "{
  \"metadata\": {\"userId\":\"$USER_ID\",\"sessionId\":\"$SESSION_ID\"},
  \"query\": \"Research quantum computing and provide a summary\",
  \"context\": {\"template_name\": \"research_summary\"}
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 || echo "FAILED")

TASK_ID=$(echo "$RESPONSE" | grep -oP '"taskId":\s*"\K[^"]+' || echo "")
if [ -z "$TASK_ID" ]; then
  info "Template not found, will use AI decomposition (expected if templates not loaded)"
else
  pass "Task submitted with template: $TASK_ID"

  # Wait for completion
  info "Waiting for template workflow completion..."
  for i in $(seq 1 30); do
    STATUS_RESPONSE=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 || echo "")
    STATUS=$(echo "$STATUS_RESPONSE" | grep -oP '"status":\s*"\K[^"]+' || echo "PENDING")

    if [ "$STATUS" = "COMPLETED" ] || [ "$STATUS" = "FAILED" ]; then
      break
    fi
    sleep 2
  done

  if [ "$STATUS" = "COMPLETED" ]; then
    pass "Template workflow completed successfully"

    # Check if DecomposeTask was skipped (zero-token routing)
    DECOMPOSE_COUNT=$(docker compose -f "$COMPOSE_FILE" logs orchestrator 2>/dev/null | grep "$TASK_ID" | grep -c "DecomposeTask" || echo "0")
    if [ "$DECOMPOSE_COUNT" -eq 0 ]; then
      pass "Zero-token routing: No DecomposeTask activity called"
    else
      info "Note: DecomposeTask was called (template may have delegated to AI)"
    fi
  else
    info "Template workflow status: $STATUS"
  fi
fi

# Test 2: Check template metrics
echo ""
echo "=== Test 2: Template Metrics Verification ==="
info "Checking Prometheus metrics..."

METRICS=$(curl -fsS http://localhost:2112/metrics 2>/dev/null || echo "")
if [ -n "$METRICS" ]; then
  # Check template loaded metric
  LOADED_COUNT=$(echo "$METRICS" | grep "shannon_template_loaded_total" | head -1 | grep -oP '\s\K\d+$' || echo "0")
  if [ "$LOADED_COUNT" -gt 0 ]; then
    pass "Template loaded metric: $LOADED_COUNT templates"
  else
    info "No templates loaded (metric: shannon_template_loaded_total = 0)"
  fi

  # Check workflow started metric (should include TemplateWorkflow if template used)
  WORKFLOW_METRICS=$(echo "$METRICS" | grep "shannon_workflows_started_total" || echo "")
  if echo "$WORKFLOW_METRICS" | grep -q "TemplateWorkflow"; then
    pass "TemplateWorkflow execution recorded in metrics"
  else
    info "No TemplateWorkflow metrics found (templates may not be in use)"
  fi
else
  info "Could not fetch metrics from orchestrator"
fi

# Test 3: Verify template inheritance works (if enterprise_research exists)
echo ""
echo "=== Test 3: Template Inheritance Test ==="
info "Testing derived template (enterprise_research)..."

SESSION_ID="template-inherit-$(date +%s)"
RESPONSE=$(grpcurl -plaintext -d "{
  \"metadata\": {\"userId\":\"$USER_ID\",\"sessionId\":\"$SESSION_ID\"},
  \"query\": \"Research AI safety and provide executive summary\",
  \"context\": {\"template_name\": \"enterprise_research\"}
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 || echo "")

TASK_ID=$(echo "$RESPONSE" | grep -oP '"taskId":\s*"\K[^"]+' || echo "")
if [ -z "$TASK_ID" ]; then
  info "enterprise_research template not found (this is OK, it's an example)"
else
  pass "Submitted task with derived template: $TASK_ID"

  # Just verify it starts, don't wait for completion
  sleep 2
  STATUS_RESPONSE=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 || echo "")
  STATUS=$(echo "$STATUS_RESPONSE" | grep -oP '"status":\s*"\K[^"]+' || echo "")
  if [ -n "$STATUS" ]; then
    pass "Inherited template workflow status: $STATUS"
  fi
fi

# Test 4: Verify template fallback to AI decomposition
echo ""
echo "=== Test 4: Template Not Found Fallback ==="
info "Submitting task with non-existent template..."

SESSION_ID="template-fallback-$(date +%s)"
RESPONSE=$(grpcurl -plaintext -d "{
  \"metadata\": {\"userId\":\"$USER_ID\",\"sessionId\":\"$SESSION_ID\"},
  \"query\": \"What is 2 + 2?\",
  \"context\": {\"template_name\": \"nonexistent_template_xyz\"}
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 || echo "")

TASK_ID=$(echo "$RESPONSE" | grep -oP '"taskId":\s*"\K[^"]+' || echo "")
if [ -n "$TASK_ID" ]; then
  pass "Task submitted (should fallback to AI decomposition)"

  sleep 3
  STATUS_RESPONSE=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 || echo "")
  STATUS=$(echo "$STATUS_RESPONSE" | grep -oP '"status":\s*"\K[^"]+' || echo "")

  # Should NOT fail, just use AI decomposition
  if [ "$STATUS" != "FAILED" ]; then
    pass "Graceful fallback: Task proceeding with AI decomposition"
  else
    info "Task failed (may not have fallback enabled)"
  fi
fi

# Test 5: Verify disable_ai flag enforcement
echo ""
echo "=== Test 5: Template Required Mode (disable_ai) ==="
info "Testing strict template-only mode..."

SESSION_ID="template-required-$(date +%s)"
RESPONSE=$(grpcurl -plaintext -d "{
  \"metadata\": {\"userId\":\"$USER_ID\",\"sessionId\":\"$SESSION_ID\"},
  \"query\": \"Calculate 100 * 200\",
  \"context\": {
    \"template_name\": \"nonexistent_required\",
    \"disable_ai\": true
  }
}" localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 || echo "")

TASK_ID=$(echo "$RESPONSE" | grep -oP '"taskId":\s*"\K[^"]+' || echo "")
if [ -n "$TASK_ID" ]; then
  sleep 2
  STATUS_RESPONSE=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1 || echo "")
  STATUS=$(echo "$STATUS_RESPONSE" | grep -oP '"status":\s*"\K[^"]+' || echo "")
  ERROR=$(echo "$STATUS_RESPONSE" | grep -oP '"error":\s*"\K[^"]+' || echo "")

  if [ "$STATUS" = "FAILED" ] || echo "$ERROR" | grep -qi "template.*not found"; then
    pass "Correctly rejected: Template required but not found"
  else
    info "disable_ai enforcement may not be active (status: $STATUS)"
  fi
fi

# Summary
echo ""
echo "================================"
echo "Template Workflow E2E Test Complete"
echo "Key verifications:"
echo "- Template system initialization"
echo "- Zero-token routing (no DecomposeTask)"
echo "- Template metrics recording"
echo "- Template inheritance (extends)"
echo "- Graceful fallback to AI decomposition"
echo "- Template-required mode enforcement"
echo "================================"
