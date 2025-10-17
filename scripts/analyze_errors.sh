#!/bin/bash
# Shannon 错误分析脚本
# 用法: ./scripts/analyze_errors.sh [hours] [service]

set -e

HOURS=${1:-24}
SERVICE=${2:-"orchestrator"}

echo "==================================="
echo "Shannon 错误分析报告"
echo "服务: $SERVICE"
echo "时间范围: 最近 $HOURS 小时"
echo "==================================="

# 获取容器名称
CONTAINER=$(docker ps --filter "name=shannon-${SERVICE}" --format "{{.Names}}" | head -1)

if [ -z "$CONTAINER" ]; then
    echo "错误: 未找到 shannon-${SERVICE} 容器"
    exit 1
fi

echo -e "\n📊 错误统计:\n"

# 错误总数
ERROR_COUNT=$(docker logs "$CONTAINER" --since "${HOURS}h" 2>&1 | grep -c 'level":"error"\|ERROR' || echo "0")
echo "  总错误数: $ERROR_COUNT"

# 按错误代码统计
echo -e "\n📌 按错误类型分布:\n"
docker logs "$CONTAINER" --since "${HOURS}h" 2>&1 \
  | grep -E 'level":"error"|ERROR' \
  | grep -oP '"code":"[^"]*"' \
  | sort | uniq -c | sort -rn \
  | head -10 \
  | awk '{printf "  %-40s %s\n", $2, $1}'

# 最近的严重错误
echo -e "\n🚨 最近的 CRITICAL/FATAL 错误:\n"
docker logs "$CONTAINER" --since "${HOURS}h" --timestamps 2>&1 \
  | grep -E 'CRITICAL|FATAL|fatal' \
  | tail -5 \
  | sed 's/^/  /'

# 失败的工作流
echo -e "\n❌ 失败的工作流:\n"
docker logs "$CONTAINER" --since "${HOURS}h" 2>&1 \
  | grep -oP 'workflow_id":"[^"]*".*failed' \
  | sort | uniq \
  | head -10 \
  | sed 's/^/  /'

# 错误趋势（每小时）
echo -e "\n📈 错误趋势（每小时）:\n"
for ((i=$HOURS-1; i>=0; i--)); do
    START_TIME="${i}h"
    END_TIME="$((i))h"
    if [ $i -eq 0 ]; then
        END_TIME="0h"
    fi
    
    COUNT=$(docker logs "$CONTAINER" --since "${START_TIME}" --until "${END_TIME}" 2>&1 \
      | grep -c 'level":"error"\|ERROR' || echo "0")
    
    HOUR=$(date -d "$i hours ago" "+%H:00")
    printf "  %s: " "$HOUR"
    printf '█%.0s' $(seq 1 $((COUNT / 10)))
    printf " (%d)\n" "$COUNT"
done

# 常见错误消息
echo -e "\n💬 最常见的错误消息:\n"
docker logs "$CONTAINER" --since "${HOURS}h" 2>&1 \
  | grep -E 'level":"error"|ERROR' \
  | grep -oP '"msg":"[^"]*"' \
  | sort | uniq -c | sort -rn \
  | head -5 \
  | sed 's/^/  /'

# 建议
echo -e "\n💡 建议:\n"
if [ "$ERROR_COUNT" -gt 100 ]; then
    echo "  ⚠️  错误率较高，建议检查服务健康状况"
fi

if docker logs "$CONTAINER" --since "1h" 2>&1 | grep -q "FATAL"; then
    echo "  🔴 检测到 FATAL 错误，请立即处理"
fi

if docker logs "$CONTAINER" --since "1h" 2>&1 | grep -q "panic"; then
    echo "  🔴 检测到 panic，服务可能不稳定"
fi

echo -e "\n完成分析"
echo "==================================="

# 生成 JSON 报告（可选）
if [ "$3" == "--json" ]; then
    OUTPUT_FILE="error_report_$(date +%Y%m%d_%H%M%S).json"
    echo -e "\n生成 JSON 报告: $OUTPUT_FILE"
    
    {
        echo "{"
        echo "  \"timestamp\": \"$(date -Iseconds)\","
        echo "  \"service\": \"$SERVICE\","
        echo "  \"hours\": $HOURS,"
        echo "  \"total_errors\": $ERROR_COUNT"
        echo "}"
    } > "$OUTPUT_FILE"
fi

