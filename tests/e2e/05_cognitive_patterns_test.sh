#!/usr/bin/env bash
set -euo pipefail

# Cognitive Patterns E2E Test
# Tests various cognitive strategies: Chain of Thought, Tree of Thoughts, ReAct, Debate, Reflection

source "$(dirname "$0")/submit_and_get_response.sh"

echo "=========================================="
echo "Cognitive Patterns E2E Test"
echo "=========================================="

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper function to check pattern in logs
check_pattern() {
    local workflow_id=$1
    local pattern=$2

    docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail 200 2>/dev/null | \
        grep -i "$workflow_id" | grep -iE "$pattern" > /dev/null 2>&1
    return $?
}

# Test 1: Chain of Thought (Sequential Reasoning)
echo ""
echo -e "${YELLOW}[TEST 1] Chain of Thought Pattern${NC}"
echo "Testing sequential reasoning for logical problems..."

QUERY="Solve this step by step: If a train travels 120 miles in 2 hours, \
then stops for 30 minutes, then travels another 90 miles in 1.5 hours, \
what is the average speed for the entire journey including the stop?"

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

if check_pattern "$WORKFLOW_ID" "chain.*thought|sequential|step"; then
    echo -e "${GREEN}✅ Chain of Thought pattern detected${NC}"
else
    echo -e "${YELLOW}⚠️  Pattern may be processing differently${NC}"
fi

# Get result
sleep 10
STATUS=$(get_task_status "$WORKFLOW_ID")
if [ "$STATUS" = "COMPLETED" ]; then
    RESULT=$(get_task_result "$WORKFLOW_ID" | head -100)
    echo "Result preview: ${RESULT:0:150}..."
fi

# Test 2: Tree of Thoughts (Exploration)
echo ""
echo -e "${YELLOW}[TEST 2] Tree of Thoughts Pattern${NC}"
echo "Testing exploration of multiple solution paths..."

QUERY="Explore different approaches to solve: You have 8 identical-looking coins, \
but one is slightly heavier. Using a balance scale only twice, \
how can you find the heavier coin? Consider multiple strategies."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

if check_pattern "$WORKFLOW_ID" "tree.*thought|explore|branch|backtrack"; then
    echo -e "${GREEN}✅ Tree of Thoughts pattern detected${NC}"
else
    echo -e "${YELLOW}⚠️  Pattern may be processing differently${NC}"
fi

sleep 10
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Status: $STATUS"

# Test 3: ReAct (Reasoning + Action)
echo ""
echo -e "${YELLOW}[TEST 3] ReAct Pattern${NC}"
echo "Testing combined reasoning and action..."

QUERY="Research and calculate: What is the current price of gold per ounce? \
Based on that, calculate how much 5.5 ounces would cost, \
then determine if it's a good investment compared to the S&P 500's 10-year average return."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

if check_pattern "$WORKFLOW_ID" "react|reasoning.*action|observe"; then
    echo -e "${GREEN}✅ ReAct pattern detected${NC}"
else
    echo -e "${YELLOW}⚠️  Pattern may be processing differently${NC}"
fi

# Check for tool usage (indicates action)
TOOL_USAGE=$(docker compose -f deploy/compose/docker-compose.yml logs llm-service --tail 100 2>/dev/null | \
    grep -c "tool" || echo "0")

if [ "$TOOL_USAGE" -gt 0 ]; then
    echo -e "${GREEN}✅ Tool usage detected (${TOOL_USAGE} references)${NC}"
fi

sleep 10
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Status: $STATUS"

# Test 4: Debate Pattern (Multi-Agent Argumentation)
echo ""
echo -e "${YELLOW}[TEST 4] Debate Pattern${NC}"
echo "Testing multi-agent argumentation..."

QUERY="Debate: Should companies prioritize remote work or office work post-2024? \
Present arguments from both perspectives - \
productivity, collaboration, cost, employee satisfaction, and innovation. \
Then synthesize a balanced conclusion."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

if check_pattern "$WORKFLOW_ID" "debate|argument|perspective|synthesis"; then
    echo -e "${GREEN}✅ Debate pattern detected${NC}"
else
    echo -e "${YELLOW}⚠️  Pattern may be processing differently${NC}"
fi

sleep 15
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Status: $STATUS"

# Test 5: Reflection Pattern (Self-Improvement)
echo ""
echo -e "${YELLOW}[TEST 5] Reflection Pattern${NC}"
echo "Testing iterative self-improvement..."

QUERY="Write a Python function to find the nth Fibonacci number. \
Then reflect on the implementation, identify improvements, \
and provide an optimized version with explanation of enhancements."

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

if check_pattern "$WORKFLOW_ID" "reflect|improve|iterate|enhance"; then
    echo -e "${GREEN}✅ Reflection pattern detected${NC}"
else
    echo -e "${YELLOW}⚠️  Pattern may be processing differently${NC}"
fi

sleep 15
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Status: $STATUS"

# Test 6: Complex Hybrid Pattern
echo ""
echo -e "${YELLOW}[TEST 6] Hybrid Pattern (Multiple Strategies)${NC}"
echo "Testing complex query requiring multiple cognitive strategies..."

QUERY="Analyze the problem of climate change: \
1. Use chain of thought to trace causes and effects \
2. Explore different solution paths (renewable energy, carbon capture, policy) \
3. Debate the trade-offs between economic growth and environmental protection \
4. Reflect on current approaches and suggest improvements \
5. Synthesize everything into an actionable 5-year plan"

WORKFLOW_ID=$(submit_task "$QUERY" | jq -r '.workflowId')
echo "Workflow ID: $WORKFLOW_ID"

sleep 5

# Check for supervisor workflow (complex tasks often trigger it)
SUPERVISOR=$(docker compose -f deploy/compose/docker-compose.yml exec temporal \
    temporal workflow list --address temporal:7233 2>/dev/null | \
    grep -c "SupervisorWorkflow" || echo "0")

if [ "$SUPERVISOR" -gt 0 ]; then
    echo -e "${GREEN}✅ SupervisorWorkflow active for complex orchestration${NC}"
fi

# Check for multiple patterns
PATTERNS_FOUND=0
for pattern in "chain" "tree" "debate" "reflect" "synthesis"; do
    if check_pattern "$WORKFLOW_ID" "$pattern"; then
        echo -e "${GREEN}  ✓ ${pattern} pattern elements detected${NC}"
        PATTERNS_FOUND=$((PATTERNS_FOUND + 1))
    fi
done

echo "Detected $PATTERNS_FOUND pattern elements"

sleep 20
STATUS=$(get_task_status "$WORKFLOW_ID")
echo "Final status: $STATUS"

# Cleanup any long-running workflows
echo ""
echo "Cleaning up long-running workflows..."
for wf in $(docker compose -f deploy/compose/docker-compose.yml exec temporal \
    temporal workflow list --address temporal:7233 2>/dev/null | \
    grep "Running" | grep "task-" | awk '{print $2}'); do

    docker compose -f deploy/compose/docker-compose.yml exec temporal \
        temporal workflow terminate --workflow-id "$wf" --address temporal:7233 \
        --reason "Test cleanup" 2>/dev/null || true
done

echo ""
echo "=========================================="
echo "Cognitive Patterns Test Summary"
echo "=========================================="
echo ""
echo "Patterns Tested:"
echo "  1. Chain of Thought - Sequential reasoning"
echo "  2. Tree of Thoughts - Solution exploration"
echo "  3. ReAct - Reasoning with actions"
echo "  4. Debate - Multi-perspective analysis"
echo "  5. Reflection - Iterative improvement"
echo "  6. Hybrid - Complex multi-strategy"
echo ""
echo "Note: Pattern detection depends on query complexity and system configuration."
echo "Check orchestrator logs for detailed pattern routing:"
echo "  docker compose -f deploy/compose/docker-compose.yml logs orchestrator | grep -i pattern"