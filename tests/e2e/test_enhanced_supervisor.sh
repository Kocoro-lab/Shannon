#!/bin/bash
# Test script for enhanced supervisor memory system

set -e

echo "==================================="
echo "Testing Enhanced Supervisor Memory"
echo "==================================="

# Use a unique session for this test
SESSION_ID="supervisor-memory-test-$(date +%s)"
echo "Using session: $SESSION_ID"

# 1. Submit a decomposable task
echo -e "\n1. Submitting initial decomposable task..."
TASK1_ID=$(SESSION_ID=$SESSION_ID ./scripts/submit_task.sh "Write a Python function to calculate fibonacci numbers and then optimize it for performance" | grep -oE 'task-[a-z0-9-]+' | head -1)
echo "Task 1 ID: $TASK1_ID"

# Wait for task to complete
sleep 10

# Check if decomposition was recorded
echo -e "\n2. Checking decomposition pattern was recorded..."
docker compose -f deploy/compose/docker-compose.yml exec -T postgres psql -U shannon -d shannon -c "SELECT query_pattern, strategy, success_rate FROM decomposition_patterns WHERE session_id = '$SESSION_ID';" -t

# 2. Submit a similar task to trigger pattern matching
echo -e "\n3. Submitting similar task to trigger pattern matching..."
TASK2_ID=$(SESSION_ID=$SESSION_ID ./scripts/submit_task.sh "Create a Python implementation for computing factorial and then make it more efficient" | grep -oE 'task-[a-z0-9-]+' | head -1)
echo "Task 2 ID: $TASK2_ID"

# Wait for task to complete
sleep 10

# Check strategy performance
echo -e "\n4. Checking strategy performance metrics..."
docker compose -f deploy/compose/docker-compose.yml exec -T postgres psql -U shannon -d shannon -c "SELECT strategy, total_runs, success_rate, avg_duration_ms FROM strategy_performance WHERE user_id IN (SELECT DISTINCT user_id FROM tasks WHERE session_id = '$SESSION_ID');" -t

# Check if failure patterns were identified
echo -e "\n5. Checking failure pattern detection..."
docker compose -f deploy/compose/docker-compose.yml exec -T postgres psql -U shannon -d shannon -c "SELECT pattern_name, mitigation_strategy, occurrence_count FROM failure_patterns WHERE occurrence_count > 0;" -t

# 3. Test rate limit mitigation
echo -e "\n6. Testing rate limit failure pattern detection..."
TASK3_ID=$(SESSION_ID=$SESSION_ID ./scripts/submit_task.sh "Quickly analyze all these files immediately and give me results ASAP" | grep -oE 'task-[a-z0-9-]+' | head -1)
echo "Task 3 ID: $TASK3_ID"

# Wait and check logs for warnings
sleep 5

echo -e "\n7. Checking orchestrator logs for decomposition advice..."
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | tail -50 | grep -E "(Enhanced supervisor memory|decomposition advice|failure pattern)" || echo "No enhanced memory logs found"

# 4. Check user preferences inference
echo -e "\n8. Checking user preferences inference..."
docker compose -f deploy/compose/docker-compose.yml exec -T postgres psql -U shannon -d shannon -c "SELECT user_id, expertise_level, preferred_style, speed_vs_accuracy FROM user_preferences WHERE user_id IN (SELECT DISTINCT user_id FROM tasks WHERE session_id = '$SESSION_ID');" -t

# Summary
echo -e "\n==================================="
echo "Enhanced Supervisor Memory Test Summary"
echo "==================================="
echo "Session ID: $SESSION_ID"
echo ""
echo "Verify the following:"
echo "1. ✓ Decomposition patterns are recorded in database"
echo "2. ✓ Strategy performance is tracked"
echo "3. ✓ Similar queries reuse successful patterns"
echo "4. ✓ Failure patterns trigger warnings"
echo "5. ✓ User preferences are inferred"
echo ""
echo "Check orchestrator logs for:"
echo "  - 'Enhanced supervisor memory loaded'"
echo "  - 'Applying decomposition advice'"
echo "  - 'uses_previous: true' (for second task)"
echo ""
echo "To see full workflow execution:"
echo "docker compose -f deploy/compose/docker-compose.yml logs orchestrator | grep $SESSION_ID"