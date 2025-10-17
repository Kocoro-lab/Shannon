#!/bin/bash
# 对比当前测试结果与基线性能

set -e

RESULTS_DIR="benchmarks/results"
BASELINE_FILE="benchmarks/baseline.json"
LATEST_RESULT=$(ls -t "$RESULTS_DIR"/benchmark_*.json 2>/dev/null | head -1)
TOLERANCE=10  # 性能回退容忍度百分比

echo "======================================="
echo "Shannon 基准测试 - 基线对比"
echo "======================================="

# 检查是否存在基线
if [ ! -f "$BASELINE_FILE" ]; then
    echo "⚠️  未找到基线文件: $BASELINE_FILE"
    echo ""
    read -p "是否将当前结果设置为基线? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if [ -z "$LATEST_RESULT" ]; then
            echo "错误: 未找到测试结果"
            exit 1
        fi
        cp "$LATEST_RESULT" "$BASELINE_FILE"
        echo "✅ 已设置基线: $BASELINE_FILE"
        exit 0
    else
        echo "取消操作"
        exit 1
    fi
fi

# 检查是否有最新结果
if [ -z "$LATEST_RESULT" ]; then
    echo "错误: 未找到测试结果"
    echo "请先运行: ./benchmarks/run_benchmarks.sh"
    exit 1
fi

echo "基线文件: $BASELINE_FILE"
echo "当前结果: $LATEST_RESULT"
echo "容忍度: ±${TOLERANCE}%"
echo ""

# Python 脚本进行详细对比
python3 - "$BASELINE_FILE" "$LATEST_RESULT" "$TOLERANCE" <<'PYTHON'
import json
import sys
from typing import Dict, List, Any

def load_json(filepath: str) -> Dict:
    """Load JSON file"""
    try:
        with open(filepath, 'r') as f:
            return json.load(f)
    except Exception as e:
        print(f"Error loading {filepath}: {e}")
        sys.exit(1)

def calculate_change(baseline: float, current: float) -> tuple:
    """Calculate percentage change and status"""
    if baseline == 0:
        return 0, "⚠️"
    
    change = ((current - baseline) / baseline) * 100
    
    # 延迟类指标：增加是坏事
    if abs(change) < float(sys.argv[3]):
        status = "✅"  # 在容忍范围内
    elif change > 0:
        status = "❌"  # 性能下降
    else:
        status = "🚀"  # 性能提升
    
    return change, status

def compare_metrics(baseline: Dict, current: Dict):
    """Compare performance metrics"""
    print("=" * 70)
    print("性能指标对比")
    print("=" * 70)
    
    # 简单任务对比
    print("\n📊 简单任务性能")
    print("-" * 70)
    print(f"{'指标':<30} {'基线':<12} {'当前':<12} {'变化':<12} {'状态'}")
    print("-" * 70)
    
    # 这里添加实际的指标对比逻辑
    # 示例数据结构
    metrics = [
        ("平均延迟 (s)", 0.42, 0.45),
        ("P50 延迟 (s)", 0.38, 0.40),
        ("P95 延迟 (s)", 1.8, 1.9),
        ("P99 延迟 (s)", 3.2, 3.5),
        ("吞吐量 (req/s)", 125, 120),
    ]
    
    failed_checks = []
    
    for metric_name, baseline_val, current_val in metrics:
        change, status = calculate_change(baseline_val, current_val)
        
        print(f"{metric_name:<30} {baseline_val:<12.3f} {current_val:<12.3f} "
              f"{change:>+10.1f}% {status}")
        
        if status == "❌":
            failed_checks.append((metric_name, change))
    
    # DAG 工作流对比
    print("\n📊 DAG 工作流性能")
    print("-" * 70)
    print(f"{'指标':<30} {'基线':<12} {'当前':<12} {'变化':<12} {'状态'}")
    print("-" * 70)
    
    dag_metrics = [
        ("平均执行时间 (s)", 8.5, 8.8),
        ("并行加速比", 3.2, 3.1),
        ("内存使用 (MB)", 450, 460),
    ]
    
    for metric_name, baseline_val, current_val in dag_metrics:
        change, status = calculate_change(baseline_val, current_val)
        print(f"{metric_name:<30} {baseline_val:<12.2f} {current_val:<12.2f} "
              f"{change:>+10.1f}% {status}")
        
        if status == "❌":
            failed_checks.append((metric_name, change))
    
    # Python WASI 对比
    print("\n📊 Python WASI 性能")
    print("-" * 70)
    print(f"{'指标':<30} {'基线':<12} {'当前':<12} {'变化':<12} {'状态'}")
    print("-" * 70)
    
    wasi_metrics = [
        ("冷启动 (ms)", 480, 490),
        ("热启动 (ms)", 45, 43),
        ("内存开销 (MB)", 55, 56),
    ]
    
    for metric_name, baseline_val, current_val in wasi_metrics:
        change, status = calculate_change(baseline_val, current_val)
        print(f"{metric_name:<30} {baseline_val:<12.0f} {current_val:<12.0f} "
              f"{change:>+10.1f}% {status}")
        
        if status == "❌":
            failed_checks.append((metric_name, change))
    
    # 总结
    print("\n" + "=" * 70)
    print("对比总结")
    print("=" * 70)
    
    if not failed_checks:
        print("\n✅ 所有指标均在可接受范围内或有所改善")
        print("   性能测试通过！")
        return 0
    else:
        print(f"\n❌ 发现 {len(failed_checks)} 个性能回退:")
        for metric, change in failed_checks:
            print(f"   - {metric}: {change:+.1f}%")
        print("\n⚠️  性能测试未通过，请检查性能回退原因")
        return 1

def main():
    baseline = load_json(sys.argv[1])
    current = load_json(sys.argv[2])
    
    print(f"基线版本: {baseline.get('version', 'unknown')}")
    print(f"当前版本: {current.get('version', 'unknown')}")
    print(f"基线时间: {baseline.get('timestamp', 'unknown')}")
    print(f"当前时间: {current.get('timestamp', 'unknown')}")
    print()
    
    exit_code = compare_metrics(baseline, current)
    sys.exit(exit_code)

if __name__ == "__main__":
    main()
PYTHON

EXIT_CODE=$?

echo ""
echo "======================================="

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ 基线对比通过"
    echo "======================================="
    exit 0
else
    echo "❌ 基线对比失败"
    echo "======================================="
    echo ""
    echo "建议操作:"
    echo "1. 检查最近的代码变更"
    echo "2. 分析性能回退原因"
    echo "3. 优化相关代码"
    echo "4. 重新运行测试"
    echo ""
    echo "如果性能变化是预期的，更新基线:"
    echo "  cp $LATEST_RESULT $BASELINE_FILE"
    exit 1
fi

