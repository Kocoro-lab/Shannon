#!/bin/bash
# Shannon 性能基准测试运行器

set -e

CATEGORY=${1:-"all"}
OUTPUT_DIR="benchmarks/results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
REPORT_FILE="$OUTPUT_DIR/benchmark_$TIMESTAMP.json"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

echo "======================================="
echo "Shannon 性能基准测试"
echo "类别: $CATEGORY"
echo "时间: $(date)"
echo "======================================="

# 检查服务是否运行
check_services() {
    echo "检查 Shannon 服务状态..."
    
    if ! docker ps | grep -q "shannon-orchestrator"; then
        echo "错误: Shannon 服务未运行"
        echo "请先启动服务: make dev"
        exit 1
    fi
    
    echo "✅ 服务检查通过"
}

# 工作流性能测试
bench_workflows() {
    echo ""
    echo "=== 1. 工作流性能测试 ==="
    echo ""
    
    # 简单任务吞吐量
    echo "测试简单任务吞吐量..."
    python3 benchmarks/workflow_bench.py --test simple --requests 100
    
    # DAG 工作流
    echo "测试 DAG 工作流性能..."
    python3 benchmarks/workflow_bench.py --test dag --subtasks 5 --requests 20
    
    # 并行执行
    echo "测试并行执行效率..."
    python3 benchmarks/workflow_bench.py --test parallel --agents 10 --requests 10
}

# 模式性能测试
bench_patterns() {
    echo ""
    echo "=== 2. 模式性能测试 ==="
    echo ""
    
    # Chain-of-Thought
    echo "测试 Chain-of-Thought 性能..."
    python3 benchmarks/pattern_bench.py --pattern cot --requests 10
    
    # Debate
    echo "测试 Debate 模式性能..."
    python3 benchmarks/pattern_bench.py --pattern debate --agents 3 --requests 5
    
    # Reflection
    echo "测试 Reflection 性能..."
    python3 benchmarks/pattern_bench.py --pattern reflection --requests 10
}

# 工具性能测试
bench_tools() {
    echo ""
    echo "=== 3. 工具性能测试 ==="
    echo ""
    
    # Python WASI
    echo "测试 Python WASI 性能..."
    python3 benchmarks/tool_bench.py --tool python --cold-start 5 --hot-start 20
    
    # Web Search
    echo "测试 Web Search 性能..."
    python3 benchmarks/tool_bench.py --tool web_search --requests 10
}

# 数据层性能测试
bench_data() {
    echo ""
    echo "=== 4. 数据层性能测试 ==="
    echo ""
    
    # PostgreSQL
    echo "测试 PostgreSQL 性能..."
    docker exec shannon-postgres-1 bash -c \
        "echo \"SELECT COUNT(*) FROM sessions; SELECT pg_database_size('shannon');\" | psql -U shannon shannon"
    
    # Redis
    echo "测试 Redis 性能..."
    docker exec shannon-redis-1 redis-cli --stat -i 1 | head -5
    
    # Qdrant
    echo "测试 Qdrant 性能..."
    curl -s http://localhost:6333/collections | jq -r '.result.collections[] | 
        "\(.name): \(.points_count) points, \(.vectors_count) vectors"'
}

# 负载测试
bench_load() {
    echo ""
    echo "=== 5. 负载测试 ==="
    echo ""
    
    echo "运行负载测试 (100 并发用户，持续 60 秒)..."
    python3 benchmarks/load_test.py \
        --users 100 \
        --duration 60 \
        --ramp-up 10
}

# 内存分析
bench_memory() {
    echo ""
    echo "=== 6. 内存使用分析 ==="
    echo ""
    
    docker stats --no-stream --format \
        "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}" \
        | grep shannon
}

# 生成 JSON 报告
generate_json_report() {
    echo ""
    echo "生成 JSON 报告: $REPORT_FILE"
    
    cat > "$REPORT_FILE" <<EOF
{
  "timestamp": "$(date -Iseconds)",
  "version": "$(git describe --tags --always)",
  "category": "$CATEGORY",
  "environment": {
    "os": "$(uname -s)",
    "arch": "$(uname -m)",
    "docker_version": "$(docker --version | awk '{print $3}')"
  },
  "results": {}
}
EOF
    
    echo "✅ 报告已生成"
}

# 主函数
main() {
    check_services
    
    case "$CATEGORY" in
        all)
            bench_workflows
            bench_patterns
            bench_tools
            bench_data
            bench_load
            bench_memory
            ;;
        workflow|workflows)
            bench_workflows
            ;;
        pattern|patterns)
            bench_patterns
            ;;
        tool|tools)
            bench_tools
            ;;
        data)
            bench_data
            ;;
        load)
            bench_load
            ;;
        memory)
            bench_memory
            ;;
        *)
            echo "错误: 未知类别 '$CATEGORY'"
            echo "用法: $0 [all|workflow|pattern|tool|data|load|memory]"
            exit 1
            ;;
    esac
    
    generate_json_report
    
    echo ""
    echo "======================================="
    echo "基准测试完成!"
    echo "结果保存在: $OUTPUT_DIR"
    echo "======================================="
}

main


