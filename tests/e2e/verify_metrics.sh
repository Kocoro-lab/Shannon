#!/usr/bin/env bash
set -euo pipefail

# E2E test with metrics counter assertions
# This script verifies that metrics increment correctly after workflow execution

echo "=== Shannon E2E Metrics Verification Test ==="

# Helper functions
check_metric() {
    local url=$1
    local metric=$2
    local min_value=$3
    local actual=$(curl -s "$url" | grep "^$metric" | awk '{print $2}' | head -1)
    
    if [ -z "$actual" ]; then
        echo "❌ Metric $metric not found"
        return 1
    fi
    
    if awk -v a="$actual" -v m="$min_value" 'BEGIN {exit !(a >= m)}'; then
        echo "✅ $metric = $actual (>= $min_value)"
        return 0
    else
        echo "❌ $metric = $actual (expected >= $min_value)"
        return 1
    fi
}

get_metric_value() {
    local url=$1
    local metric=$2
    curl -s "$url" | grep "^$metric" | awk '{print $2}' | head -1
}

# URLs
ORCHESTRATOR_METRICS="http://localhost:2112/metrics"
AGENT_METRICS="http://localhost:2113/metrics"

echo "Step 1: Capture initial metrics"
echo "================================"

# Capture initial orchestrator metrics
INITIAL_WORKFLOWS_STARTED=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_workflows_started_total" || echo "0")
INITIAL_WORKFLOWS_COMPLETED=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_workflows_completed_total" || echo "0")
INITIAL_GRPC_REQUESTS=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_grpc_requests_total" || echo "0")

# Capture initial agent metrics
INITIAL_AGENT_TASKS=$(get_metric_value "$AGENT_METRICS" "agent_core_tasks_total" || echo "0")
INITIAL_AGENT_TOKENS=$(get_metric_value "$AGENT_METRICS" "agent_core_tokens_used_total" || echo "0")

echo "Initial orchestrator workflows started: $INITIAL_WORKFLOWS_STARTED"
echo "Initial orchestrator workflows completed: $INITIAL_WORKFLOWS_COMPLETED"
echo "Initial agent tasks: $INITIAL_AGENT_TASKS"

echo ""
echo "Step 2: Submit test workflow"
echo "============================"

# Submit a task via gRPC
TASK_ID=$(grpcurl -plaintext \
    -d '{"metadata":{"user_id":"e2e-test","session_id":"e2e-session"},"query":"Calculate 5+5 and explain the result","context":{}}' \
    localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask 2>/dev/null | \
    grep '"taskId"' | sed 's/.*"taskId": "\([^"]*\)".*/\1/')

if [ -z "$TASK_ID" ]; then
    echo "❌ Failed to submit task"
    exit 1
fi

echo "✅ Task submitted: $TASK_ID"

echo ""
echo "Step 3: Wait for workflow completion"
echo "===================================="

# Poll for completion
for i in {1..10}; do
    STATUS=$(grpcurl -plaintext \
        -d '{"taskId":"'"$TASK_ID"'"}' \
        localhost:50052 shannon.orchestrator.OrchestratorService/GetTaskStatus 2>/dev/null | \
        grep '"status"' | sed 's/.*"status": "\([^"]*\)".*/\1/')
    
    echo "Attempt $i: Status = $STATUS"
    
    if [[ "$STATUS" == "TASK_STATUS_COMPLETED" ]] || [[ "$STATUS" == "TASK_STATUS_FAILED" ]]; then
        break
    fi
    sleep 1
done

if [[ "$STATUS" != "TASK_STATUS_COMPLETED" ]]; then
    echo "⚠️ Task did not complete successfully (status: $STATUS)"
fi

echo ""
echo "Step 4: Verify metrics incremented"
echo "=================================="

# Give metrics a moment to update
sleep 2

# Get new metric values
NEW_WORKFLOWS_STARTED=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_workflows_started_total" || echo "0")
NEW_WORKFLOWS_COMPLETED=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_workflows_completed_total" || echo "0")
NEW_GRPC_REQUESTS=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_grpc_requests_total" || echo "0")
NEW_AGENT_TASKS=$(get_metric_value "$AGENT_METRICS" "agent_core_tasks_total" || echo "0")

# Calculate deltas
DELTA_STARTED=$(echo "$NEW_WORKFLOWS_STARTED - $INITIAL_WORKFLOWS_STARTED" | bc)
DELTA_COMPLETED=$(echo "$NEW_WORKFLOWS_COMPLETED - $INITIAL_WORKFLOWS_COMPLETED" | bc)
DELTA_GRPC=$(echo "$NEW_GRPC_REQUESTS - $INITIAL_GRPC_REQUESTS" | bc)
DELTA_AGENT=$(echo "$NEW_AGENT_TASKS - $INITIAL_AGENT_TASKS" | bc)

echo "Metric Changes:"
echo "---------------"
echo "Workflows started: +$DELTA_STARTED (was $INITIAL_WORKFLOWS_STARTED, now $NEW_WORKFLOWS_STARTED)"
echo "Workflows completed: +$DELTA_COMPLETED (was $INITIAL_WORKFLOWS_COMPLETED, now $NEW_WORKFLOWS_COMPLETED)"
echo "gRPC requests: +$DELTA_GRPC (was $INITIAL_GRPC_REQUESTS, now $NEW_GRPC_REQUESTS)"
echo "Agent tasks: +$DELTA_AGENT (was $INITIAL_AGENT_TASKS, now $NEW_AGENT_TASKS)"

echo ""
echo "Step 5: Assert expected increments"
echo "=================================="

ERRORS=0

# Workflows should have started
if [ "$DELTA_STARTED" -ge 1 ]; then
    echo "✅ Workflows started incremented by $DELTA_STARTED"
else
    echo "❌ Workflows started did not increment (delta: $DELTA_STARTED)"
    ERRORS=$((ERRORS + 1))
fi

# Workflows should have completed (if status was COMPLETED)
if [[ "$STATUS" == "TASK_STATUS_COMPLETED" ]]; then
    if [ "$DELTA_COMPLETED" -ge 1 ]; then
        echo "✅ Workflows completed incremented by $DELTA_COMPLETED"
    else
        echo "❌ Workflows completed did not increment (delta: $DELTA_COMPLETED)"
        ERRORS=$((ERRORS + 1))
    fi
fi

# gRPC requests should have increased (SubmitTask + GetTaskStatus)
if [ "$DELTA_GRPC" -ge 2 ]; then
    echo "✅ gRPC requests incremented by $DELTA_GRPC"
else
    echo "❌ gRPC requests insufficient increment (delta: $DELTA_GRPC, expected >= 2)"
    ERRORS=$((ERRORS + 1))
fi

# Agent tasks may have increased if agent was invoked
if [ "$DELTA_AGENT" -ge 0 ]; then
    echo "✅ Agent tasks handled: $DELTA_AGENT"
fi

echo ""
echo "Step 6: Check specific metric patterns"
echo "======================================"

# Check for histogram metrics
if curl -s "$ORCHESTRATOR_METRICS" | grep -q "shannon_workflow_duration_seconds_bucket"; then
    echo "✅ Workflow duration histogram present"
else
    echo "⚠️ Workflow duration histogram not found"
fi

if curl -s "$ORCHESTRATOR_METRICS" | grep -q "shannon_grpc_request_duration_seconds"; then
    echo "✅ gRPC request duration metrics present"
else
    echo "⚠️ gRPC request duration metrics not found"
fi

# Check token usage metrics
if curl -s "$ORCHESTRATOR_METRICS" | grep -q "shannon_task_tokens_used"; then
    TOKEN_USAGE=$(get_metric_value "$ORCHESTRATOR_METRICS" "shannon_task_tokens_used")
    echo "✅ Token usage tracked: $TOKEN_USAGE tokens"
else
    echo "⚠️ Token usage metrics not found"
fi

# Check cache metrics
if curl -s "$AGENT_METRICS" | grep -q "shannon_cache_hits_total"; then
    CACHE_HITS=$(get_metric_value "$AGENT_METRICS" "shannon_cache_hits_total" || echo "0")
    CACHE_MISSES=$(get_metric_value "$AGENT_METRICS" "shannon_cache_misses_total" || echo "0")
    echo "✅ Cache metrics: hits=$CACHE_HITS, misses=$CACHE_MISSES"
else
    echo "⚠️ Cache metrics not found"
fi

echo ""
echo "======================================="
if [ $ERRORS -eq 0 ]; then
    echo "✅ E2E METRICS TEST PASSED"
    exit 0
else
    echo "❌ E2E METRICS TEST FAILED ($ERRORS errors)"
    exit 1
fi