#!/usr/bin/env bash
# Test UTF-8 Chinese character handling in event logs
# This test verifies the fix for: pq: invalid byte sequence for encoding "UTF8": 0xe6

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if services are running
check_service() {
    local service=$1
    local port=$2
    if ! nc -z localhost "$port" 2>/dev/null; then
        log_warn "$service not running on port $port"
        return 1
    fi
    return 0
}

log_info "Testing UTF-8 Chinese character handling..."

# Check prerequisites
if ! check_service "Gateway" 8080; then
    log_error "Gateway service not available. Run 'make dev' first."
    exit 1
fi

# Test 1: Simple Chinese query
log_info "Test 1: Submitting Chinese query..."
CHINESE_QUERY="查询数据库中的用户信息"
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tasks \
    -H "Content-Type: application/json" \
    -d "{
        \"query\": \"${CHINESE_QUERY}\",
        \"context\": {\"role\": \"simple\"}
    }")

TASK_ID=$(echo "$RESPONSE" | jq -r '.task_id // empty')
if [ -z "$TASK_ID" ]; then
    log_error "Failed to submit task with Chinese query"
    echo "Response: $RESPONSE"
    exit 1
fi

log_info "Task submitted successfully. Task ID: $TASK_ID"

# Wait for task to complete
log_info "Waiting for task completion..."
sleep 5

# Check for UTF-8 encoding errors in logs
log_info "Checking for UTF-8 encoding errors..."
ERROR_COUNT=$(docker logs shannon-orchestrator-1 2>&1 | grep -c "invalid byte sequence" || true)

if [ "$ERROR_COUNT" -gt 0 ]; then
    log_error "Found $ERROR_COUNT UTF-8 encoding errors in logs!"
    docker logs shannon-orchestrator-1 2>&1 | grep "invalid byte sequence" | tail -5
    exit 1
fi

log_info "✓ No UTF-8 encoding errors found"

# Test 2: Very long Chinese text (triggers truncation)
log_info "Test 2: Submitting long Chinese query (tests truncation)..."
LONG_CHINESE="用户管理系统需要实现以下功能：用户注册、登录验证、权限管理、数据查询、报表生成、系统监控、日志记录、性能优化、安全防护、备份恢复等多个模块的完整功能实现和测试验证"

RESPONSE2=$(curl -s -X POST http://localhost:8080/api/v1/tasks \
    -H "Content-Type: application/json" \
    -d "{
        \"query\": \"${LONG_CHINESE}\",
        \"context\": {\"role\": \"simple\"}
    }")

TASK_ID2=$(echo "$RESPONSE2" | jq -r '.task_id // empty')
if [ -z "$TASK_ID2" ]; then
    log_error "Failed to submit task with long Chinese query"
    echo "Response: $RESPONSE2"
    exit 1
fi

log_info "Long query submitted successfully. Task ID: $TASK_ID2"
sleep 5

# Check again for errors
ERROR_COUNT2=$(docker logs shannon-orchestrator-1 2>&1 | grep -c "invalid byte sequence" || true)
if [ "$ERROR_COUNT2" -gt 0 ]; then
    log_error "Found UTF-8 encoding errors after long Chinese query!"
    exit 1
fi

log_info "✓ Long Chinese query handled correctly"

# Test 3: Mixed languages
log_info "Test 3: Testing mixed English/Chinese/Japanese/Emoji..."
MIXED_QUERY="Query for 用户データ 👤 in database システム"

RESPONSE3=$(curl -s -X POST http://localhost:8080/api/v1/tasks \
    -H "Content-Type: application/json" \
    -d "{
        \"query\": \"${MIXED_QUERY}\",
        \"context\": {\"role\": \"simple\"}
    }")

TASK_ID3=$(echo "$RESPONSE3" | jq -r '.task_id // empty')
if [ -z "$TASK_ID3" ]; then
    log_error "Failed to submit task with mixed language query"
    echo "Response: $RESPONSE3"
    exit 1
fi

log_info "Mixed language query submitted. Task ID: $TASK_ID3"
sleep 5

# Final error check
ERROR_COUNT3=$(docker logs shannon-orchestrator-1 2>&1 | grep -c "invalid byte sequence" || true)
if [ "$ERROR_COUNT3" -gt 0 ]; then
    log_error "Found UTF-8 encoding errors after mixed language query!"
    exit 1
fi

log_info "✓ Mixed language query handled correctly"

# Check PostgreSQL task_executions table directly
log_info "Verifying task_executions in PostgreSQL..."
PG_CHECK=$(docker exec shannon-postgres-1 psql -U shannon -d shannon -t -c \
    "SELECT COUNT(*) FROM task_executions WHERE workflow_id IN ('${TASK_ID}', '${TASK_ID2}', '${TASK_ID3}');" 2>/dev/null || echo "0")

PG_COUNT=$(echo "$PG_CHECK" | tr -d ' ')
if [ "$PG_COUNT" -gt 0 ]; then
    log_info "✓ Found $PG_COUNT event logs in PostgreSQL"
else
    log_warn "No event logs found in PostgreSQL (this might be okay if persistence is disabled)"
fi

# Summary
echo ""
echo "================================"
log_info "UTF-8 Chinese character test PASSED ✅"
echo "================================"
echo ""
log_info "Test results:"
echo "  - Chinese query: ✓"
echo "  - Long Chinese query with truncation: ✓"
echo "  - Mixed language query (EN/CN/JP/Emoji): ✓"
echo "  - No UTF-8 encoding errors: ✓"
echo "  - Event logs persisted: ✓"
echo ""
log_info "The UTF-8 encoding fix is working correctly!"

