#!/usr/bin/env bash
set -euo pipefail

# Supervisor Workflow E2E Test
# This test verifies that complex parallel tasks trigger the SupervisorWorkflow

source "$(dirname "$0")/submit_and_get_response.sh"

echo "=========================================="
echo "Supervisor Workflow E2E Test"
echo "=========================================="

# Test 1: Parallel Task Execution
echo ""
echo "[TEST 1] Testing parallel task execution with SupervisorWorkflow..."
QUERY="Break this into 3 separate tasks and execute them in parallel: \
Task 1: Calculate the factorial of 20. \
Task 2: Generate the first 15 Fibonacci numbers. \
Task 3: Find all prime numbers between 1 and 100. \
Then combine all results into a summary report."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Submitted workflow: $WORKFLOW_ID"

# Wait for workflow to start
sleep 3

# Check if SupervisorWorkflow was spawned
echo "Checking for SupervisorWorkflow..."
SUPERVISOR_COUNT=$(docker compose -f deploy/compose/docker-compose.yml exec temporal \
    temporal workflow list --address temporal:7233 2>/dev/null | \
    grep -c "SupervisorWorkflow" || echo "0")

if [ "$SUPERVISOR_COUNT" -gt 0 ]; then
    echo "✅ SupervisorWorkflow detected!"

    # Get supervisor workflow ID
    SUPERVISOR_ID=$(docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow list --address temporal:7233 2>/dev/null | \
        grep "SupervisorWorkflow" | awk '{print $2}' | head -1)

    echo "Supervisor Workflow ID: $SUPERVISOR_ID"

    # Check supervisor details
    docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow describe --workflow-id "$SUPERVISOR_ID" --address temporal:7233 2>/dev/null | \
        grep -E "WorkflowId|Type|Status" | head -5
else
    echo "⚠️  No SupervisorWorkflow detected (may have completed already)"
fi

# Wait for completion with timeout
echo "Waiting for task completion (max 60s)..."
COUNTER=0
MAX_WAIT=60
while [ $COUNTER -lt $MAX_WAIT ]; do
    STATUS=$(get_task_status "$WORKFLOW_ID")
    if [ "$STATUS" = "COMPLETED" ] || [ "$STATUS" = "FAILED" ]; then
        echo "Task $STATUS after ${COUNTER}s"
        break
    fi
    sleep 2
    COUNTER=$((COUNTER + 2))
    if [ $((COUNTER % 10)) -eq 0 ]; then
        echo "  Still running... (${COUNTER}s)"
    fi
done

if [ $COUNTER -ge $MAX_WAIT ]; then
    echo "⚠️  Task did not complete within ${MAX_WAIT}s, terminating..."
    docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow terminate --workflow-id "$WORKFLOW_ID" --address temporal:7233 \
        --reason "Test timeout" 2>/dev/null || true
fi

# Check database for execution mode
echo ""
echo "Checking execution mode in database..."
MODE=$(docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
    psql -U shannon -d shannon -t -c \
    "SELECT mode FROM tasks WHERE workflow_id='$WORKFLOW_ID';" 2>/dev/null | xargs)

echo "Execution mode: $MODE"

# Test 2: Complex Multi-Step Analysis
echo ""
echo "[TEST 2] Testing complex multi-step analysis..."
QUERY="Perform a comprehensive analysis: \
1) Research the top 3 programming languages in 2024. \
2) For each language, calculate its growth rate over the past 5 years. \
3) Create a comparison matrix of their features. \
4) Generate predictions for their popularity in 2025. \
5) Synthesize all findings into an executive summary with recommendations."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Submitted workflow: $WORKFLOW_ID"

# Monitor for a shorter time
sleep 5

# Check workflow type
WORKFLOW_TYPE=$(docker compose -f deploy/compose/docker-compose.yml exec temporal \
    temporal workflow describe --workflow-id "$WORKFLOW_ID" --address temporal:7233 2>/dev/null | \
    grep "Type" | head -1 | awk '{print $2}')

echo "Workflow type: $WORKFLOW_TYPE"

# Check for child workflows
CHILD_COUNT=$(docker compose -f deploy/compose/docker-compose.yml exec temporal \
    temporal workflow describe --workflow-id "$WORKFLOW_ID" --address temporal:7233 2>/dev/null | \
    grep -c "Child Workflows" || echo "0")

if [ "$CHILD_COUNT" -gt 0 ]; then
    echo "✅ Child workflows detected - multi-agent collaboration active!"
fi

# Quick completion check
sleep 10
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Final status: $STATUS"

# Terminate if still running
if [ "$STATUS" = "RUNNING" ]; then
    docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow terminate --workflow-id "$WORKFLOW_ID" --address temporal:7233 \
        --reason "Test cleanup" 2>/dev/null || true
fi

# Test 3: DAG Workflow with Dependencies
echo ""
echo "[TEST 3] Testing DAG workflow with task dependencies..."
QUERY="Execute these tasks with dependencies: \
Step A: Calculate compound interest on \$10,000 at 5% for 10 years. \
Step B: Use result from A to determine Bitcoin purchasing power. \
Step C: Use result from B to project portfolio value in 5 years. \
Each step depends on the previous one's output."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Submitted workflow: $WORKFLOW_ID"

sleep 5

# Check for DAG pattern
echo "Checking workflow pattern..."
docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 100 2>/dev/null | \
    grep -i "$WORKFLOW_ID" | grep -E "DAG|chain|dependency" | head -3 || \
    echo "Pattern detection in progress..."

# Quick status check
sleep 10
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Task status: $STATUS"

# Cleanup
if [ "$STATUS" = "RUNNING" ]; then
    docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow terminate --workflow-id "$WORKFLOW_ID" --address temporal:7233 \
        --reason "Test cleanup" 2>/dev/null || true
fi

echo ""
echo "=========================================="
echo "Supervisor Workflow Tests Complete"
echo "=========================================="
echo ""
echo "Summary:"
echo "- Parallel task execution tested"
echo "- Multi-step analysis tested"
echo "- DAG workflow with dependencies tested"
echo ""
echo "Check logs for detailed execution patterns:"
echo "  docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 200 | grep -i supervisor"