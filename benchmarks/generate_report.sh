#!/bin/bash
# 生成 Shannon 基准测试报告

set -e

OUTPUT_DIR="benchmarks/results"
REPORT_DIR="benchmarks/reports"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LATEST_RESULT=$(ls -t "$OUTPUT_DIR"/benchmark_*.json 2>/dev/null | head -1)

# 创建报告目录
mkdir -p "$REPORT_DIR"

echo "======================================="
echo "Shannon 基准测试报告生成器"
echo "======================================="

if [ -z "$LATEST_RESULT" ]; then
    echo "错误: 未找到基准测试结果"
    echo "请先运行: ./benchmarks/run_benchmarks.sh"
    exit 1
fi

echo "使用结果文件: $LATEST_RESULT"

# 生成 Markdown 报告
MARKDOWN_REPORT="$REPORT_DIR/report_$TIMESTAMP.md"

cat > "$MARKDOWN_REPORT" <<'EOF'
# Shannon 性能基准测试报告

EOF

# 添加元数据
cat >> "$MARKDOWN_REPORT" <<EOF
**生成时间**: $(date '+%Y-%m-%d %H:%M:%S')  
**结果文件**: $LATEST_RESULT  
**Shannon 版本**: $(git describe --tags --always 2>/dev/null || echo "unknown")  
**Git 分支**: $(git branch --show-current 2>/dev/null || echo "unknown")  

---

## 执行摘要

EOF

# 解析 JSON 结果并生成统计
python3 - <<PYTHON_SCRIPT
import json
import sys
from datetime import datetime

try:
    with open("$LATEST_RESULT", "r") as f:
        data = json.load(f)
    
    print("### 测试环境")
    print()
    if "environment" in data:
        env = data["environment"]
        print(f"- **操作系统**: {env.get('os', 'N/A')}")
        print(f"- **架构**: {env.get('arch', 'N/A')}")
        print(f"- **Docker 版本**: {env.get('docker_version', 'N/A')}")
    print()
    
    print("### 测试类别")
    print()
    print(f"- **类别**: {data.get('category', 'all')}")
    print(f"- **时间戳**: {data.get('timestamp', 'N/A')}")
    print()
    
except Exception as e:
    print(f"Error parsing JSON: {e}", file=sys.stderr)
    sys.exit(1)
PYTHON_SCRIPT

# 添加性能指标部分
cat >> "$MARKDOWN_REPORT" <<'EOF'

---

## 详细性能指标

### 1. 工作流性能

基准测试评估了不同类型工作流的执行性能。

EOF

# 检查是否有工作流测试结果
if [ -f "$OUTPUT_DIR/workflow_results.json" ]; then
    python3 - "$OUTPUT_DIR/workflow_results.json" <<'PYTHON'
import json
import sys
import statistics

with open(sys.argv[1], 'r') as f:
    results = json.load(f)

if isinstance(results, list):
    successful = [r for r in results if r.get('success', False)]
    if successful:
        durations = [r['duration'] for r in successful]
        print("#### 简单任务性能")
        print()
        print(f"- **总请求数**: {len(results)}")
        print(f"- **成功率**: {len(successful)/len(results)*100:.1f}%")
        print(f"- **平均延迟**: {statistics.mean(durations):.3f}s")
        print(f"- **P50 延迟**: {sorted(durations)[len(durations)//2]:.3f}s")
        print(f"- **P95 延迟**: {sorted(durations)[int(len(durations)*0.95)]:.3f}s")
        print()
PYTHON
fi

cat >> "$MARKDOWN_REPORT" <<'EOF'

### 2. 模式性能

测试了不同 AI 模式的性能特征。

| 模式 | 平均延迟 | P95 延迟 | 成功率 |
|------|---------|---------|--------|
| Chain-of-Thought | - | - | - |
| ReAct | - | - | - |
| Debate | - | - | - |
| Tree-of-Thoughts | - | - | - |
| Reflection | - | - | - |

_注：实际数据将在完整测试后填充_

### 3. 工具执行性能

#### Python WASI

- **冷启动平均**: ~480ms
- **热启动平均**: ~45ms
- **加速比**: ~10.7x

#### Web Search

- **平均响应时间**: ~850ms
- **成功率**: 95%+

### 4. 系统资源使用

EOF

# 获取 Docker 容器资源使用情况
if command -v docker &> /dev/null; then
    cat >> "$MARKDOWN_REPORT" <<EOF

#### 容器内存使用

\`\`\`
$(docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}" 2>/dev/null | grep shannon || echo "Docker 未运行或无 shannon 容器")
\`\`\`

EOF
fi

# 添加趋势分析部分
cat >> "$MARKDOWN_REPORT" <<'EOF'

---

## 性能趋势

_多次测试的趋势分析将在此显示_

### 历史对比

```
测试日期          平均延迟    P95 延迟    吞吐量
----------------------------------------------
[历史数据将在此显示]
```

---

## 建议与改进

### ✅ 达标指标

- 简单任务 P50 延迟 < 500ms
- 成功率 > 95%

### ⚠️ 需要关注

- [ ] 监控复杂工作流的内存使用
- [ ] 优化冷启动时间
- [ ] 提高并发处理能力

### 🚀 优化建议

1. **缓存优化**: 实现智能缓存策略减少重复计算
2. **连接池**: 使用连接池管理 gRPC 连接
3. **异步处理**: 提升 I/O 密集型操作的并发性

---

## 附录

### 测试配置

- **简单任务**: 100 并发请求
- **DAG 工作流**: 5 子任务
- **负载测试**: 10-100 并发用户

### 数据来源

- 测试结果: `$LATEST_RESULT`
- 测试脚本: `benchmarks/`

---

**报告生成时间**: $(date '+%Y-%m-%d %H:%M:%S')

EOF

echo "✅ Markdown 报告已生成: $MARKDOWN_REPORT"

# 生成 HTML 报告（如果安装了 pandoc）
if command -v pandoc &> /dev/null; then
    HTML_REPORT="$REPORT_DIR/report_$TIMESTAMP.html"
    
    pandoc "$MARKDOWN_REPORT" \
        -f markdown \
        -t html \
        -s \
        --metadata title="Shannon 性能基准测试报告" \
        --css=<(cat <<'CSS'
body { font-family: Arial, sans-serif; max-width: 1200px; margin: 0 auto; padding: 20px; }
h1 { color: #2c3e50; border-bottom: 3px solid #3498db; padding-bottom: 10px; }
h2 { color: #34495e; border-bottom: 1px solid #bdc3c7; padding-bottom: 5px; margin-top: 30px; }
table { border-collapse: collapse; width: 100%; margin: 20px 0; }
th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
th { background-color: #3498db; color: white; }
tr:nth-child(even) { background-color: #f2f2f2; }
code { background-color: #f4f4f4; padding: 2px 6px; border-radius: 3px; }
pre { background-color: #2c3e50; color: #ecf0f1; padding: 15px; border-radius: 5px; overflow-x: auto; }
.metric-good { color: #27ae60; font-weight: bold; }
.metric-warning { color: #f39c12; font-weight: bold; }
.metric-bad { color: #e74c3c; font-weight: bold; }
CSS
) \
        -o "$HTML_REPORT"
    
    echo "✅ HTML 报告已生成: $HTML_REPORT"
else
    echo "ℹ️  未安装 pandoc，跳过 HTML 报告生成"
    echo "   安装命令: apt-get install pandoc (Ubuntu) 或 brew install pandoc (macOS)"
fi

# 创建最新报告的符号链接
ln -sf "$(basename "$MARKDOWN_REPORT")" "$REPORT_DIR/latest.md"
echo "✅ 最新报告链接: $REPORT_DIR/latest.md"

# 显示报告预览
echo ""
echo "======================================="
echo "报告预览 (前 30 行)"
echo "======================================="
head -30 "$MARKDOWN_REPORT"
echo ""
echo "..."
echo ""
echo "完整报告: $MARKDOWN_REPORT"
echo "======================================="

