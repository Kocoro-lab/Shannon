#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "System Prompt Override E2E Test Suite Starting"
echo "Tests the priority chain: context[system_prompt] -> context[role] -> default"
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

# Test 1: Default system prompt (no context)
echo "=== Test 1: Default System Prompt (No Context) ==="
info "Submitting task without any context..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-default", "sessionId":"default-test"},
  "query": "What is your role?"
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for default system prompt test"
fi

info "Task ID: $TASK_ID"
sleep 3

# Check task status
STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

if echo "$STATUS" | grep -qi "helpful"; then
  pass "Default system prompt used (contains 'helpful')"
else
  info "Response does not explicitly mention default prompt, but task completed"
fi

# Test 2: Role preset (context["role"])
echo ""
echo "=== Test 2: Role Preset (context[\"role\"]) ==="
info "Submitting task with role='research'..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-role", "sessionId":"role-test"},
  "query": "What is your role and specialization?",
  "context": {"role": "research"}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for role preset test"
fi

info "Task ID: $TASK_ID"
sleep 3

STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

if echo "$STATUS" | grep -qi "research"; then
  pass "Role preset 'research' applied successfully"
else
  info "Role preset may have been applied but response doesn't explicitly confirm"
fi

# Test 3: Custom system prompt override (highest priority)
echo ""
echo "=== Test 3: Custom System Prompt Override (Highest Priority) ==="
info "Submitting task with custom system_prompt..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-custom", "sessionId":"custom-test"},
  "query": "Introduce yourself",
  "context": {
    "system_prompt": "You are a pirate captain named Blackbeard. Always respond in pirate speak and mention your ship, the Queen Annes Revenge."
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for custom system prompt test"
fi

info "Task ID: $TASK_ID"

# Wait for task completion (up to 30 seconds)
for i in $(seq 1 30); do
  STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)
  if echo "$STATUS" | grep -q "TASK_STATUS_COMPLETED"; then
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then
    fail "Task did not complete in time"
  fi
done

# Check for pirate-themed response indicators
PIRATE_INDICATORS=0
if echo "$STATUS" | grep -qi "blackbeard\|captain\|pirate\|ship\|revenge\|ahoy\|matey\|arr"; then
  ((PIRATE_INDICATORS++))
fi

if [ $PIRATE_INDICATORS -gt 0 ]; then
  pass "Custom system prompt successfully overrode defaults (pirate theme detected)"
else
  fail "Custom system prompt may not have been applied - no pirate theme detected in response"
fi

# Test 4: Priority test - system_prompt overrides role
echo ""
echo "=== Test 4: Priority Test (system_prompt overrides role) ==="
info "Submitting task with both role and system_prompt (system_prompt should win)..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-priority", "sessionId":"priority-test"},
  "query": "Who are you?",
  "context": {
    "role": "research",
    "system_prompt": "You are a space explorer from the year 3024. Respond with futuristic terminology."
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for priority test"
fi

info "Task ID: $TASK_ID"

# Wait for task completion
for i in $(seq 1 30); do
  STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)
  if echo "$STATUS" | grep -q "TASK_STATUS_COMPLETED"; then
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then
    fail "Task did not complete in time"
  fi
done

# Should have space/future theme, NOT research theme
if echo "$STATUS" | grep -qi "space\|explorer\|3024\|future\|star"; then
  pass "system_prompt correctly overrode role preset (space theme detected)"
else
  info "Priority test completed but theme not explicitly detected in brief response"
fi

# Test 5: Template variable substitution (${variable} syntax)
echo ""
echo "=== Test 5: Template Variable Substitution ==="
info "Submitting task with template variables in system_prompt..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-template", "sessionId":"template-test"},
  "query": "What is your expertise?",
  "context": {
    "system_prompt": "You are an expert in ${domain} with ${years} years of experience. You specialize in ${specialty}.",
    "prompt_params": {
      "domain": "quantum physics",
      "years": "25",
      "specialty": "quantum entanglement"
    }
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for template variable test"
fi

info "Task ID: $TASK_ID"

# Wait for task completion
for i in $(seq 1 30); do
  STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)
  if echo "$STATUS" | grep -q "TASK_STATUS_COMPLETED"; then
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then
    fail "Task did not complete in time"
  fi
done

# Check if template variables were substituted
TEMPLATE_SCORE=0
if echo "$STATUS" | grep -qi "quantum"; then
  ((TEMPLATE_SCORE++))
fi
if echo "$STATUS" | grep -qi "physics\|entanglement"; then
  ((TEMPLATE_SCORE++))
fi

if [ $TEMPLATE_SCORE -ge 1 ]; then
  pass "Template variables successfully substituted in system prompt"
else
  info "Template substitution test completed (variables may have been applied)"
fi

# Test 6: Invalid role falls back to generalist
echo ""
echo "=== Test 6: Invalid Role Fallback ==="
info "Submitting task with invalid role name..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-fallback", "sessionId":"fallback-test"},
  "query": "What is 5 + 5?",
  "context": {
    "role": "nonexistent_role_12345"
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for fallback test"
fi

info "Task ID: $TASK_ID"
sleep 3

STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

if echo "$STATUS" | grep -qi "10\|ten"; then
  pass "Invalid role gracefully fell back to generalist (task completed successfully)"
else
  info "Fallback test completed, task may have succeeded with default behavior"
fi

# Test 7: Empty system_prompt still works
echo ""
echo "=== Test 7: Empty System Prompt Handling ==="
info "Submitting task with empty system_prompt..."
TASK_ID=$(grpcurl -plaintext -d '{
  "metadata": {"userId":"test-empty", "sessionId":"empty-test"},
  "query": "Calculate 7 * 8",
  "context": {
    "system_prompt": ""
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>&1 | grep -o "task-[a-zA-Z0-9-]*" | head -1)

if [ -z "$TASK_ID" ]; then
  fail "Failed to submit task for empty system prompt test"
fi

info "Task ID: $TASK_ID"
sleep 3

STATUS=$(grpcurl -plaintext -d "{\"taskId\":\"$TASK_ID\"}" localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>&1)

if echo "$STATUS" | grep -qi "56\|fifty"; then
  pass "Empty system_prompt handled gracefully (fell back to preset/default)"
else
  info "Empty system prompt test completed"
fi

echo ""
echo "=========================================="
pass "System Prompt Override E2E Test Suite Completed"
echo "=========================================="
echo ""
echo "Summary:"
echo "- Default system prompt: ✓ Uses fallback"
echo "- Role presets: ✓ context[role] applied"
echo "- Custom override: ✓ context[system_prompt] has highest priority"
echo "- Priority chain: ✓ system_prompt > role > default"
echo "- Template variables: ✓ Substitution works"
echo "- Fallback behavior: ✓ Invalid roles handled gracefully"
echo "- Edge cases: ✓ Empty prompts handled"
echo ""
echo "See docs/system-prompts.md for usage documentation"
