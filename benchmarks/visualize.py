#!/usr/bin/env python3
"""
Shannon 性能数据可视化工具
生成性能趋势图表和对比分析
"""

import json
import os
import sys
from pathlib import Path
from typing import List, Dict, Any
from datetime import datetime
import argparse

try:
    import matplotlib
    matplotlib.use('Agg')  # 无头模式
    import matplotlib.pyplot as plt
    import matplotlib.dates as mdates
    MATPLOTLIB_AVAILABLE = True
except ImportError:
    print("⚠️  matplotlib not installed. Install with: pip install matplotlib")
    MATPLOTLIB_AVAILABLE = False

try:
    import pandas as pd
    PANDAS_AVAILABLE = True
except ImportError:
    print("⚠️  pandas not installed. Install with: pip install pandas")
    PANDAS_AVAILABLE = False

try:
    import plotly.graph_objects as go
    import plotly.express as px
    from plotly.subplots import make_subplots
    PLOTLY_AVAILABLE = True
except ImportError:
    print("⚠️  plotly not installed. Install with: pip install plotly")
    PLOTLY_AVAILABLE = False


class BenchmarkVisualizer:
    """基准测试可视化工具"""
    
    def __init__(self, results_dir="benchmarks/results", output_dir="benchmarks/charts"):
        self.results_dir = Path(results_dir)
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        
    def load_results(self, pattern="*.json") -> List[Dict]:
        """加载所有测试结果"""
        results = []
        for file_path in self.results_dir.glob(pattern):
            try:
                with open(file_path, 'r') as f:
                    data = json.load(f)
                    data['_filename'] = file_path.name
                    data['_timestamp'] = file_path.stat().st_mtime
                    results.append(data)
            except Exception as e:
                print(f"⚠️  Failed to load {file_path}: {e}")
        
        # 按时间排序
        results.sort(key=lambda x: x.get('_timestamp', 0))
        return results
    
    def plot_latency_distribution(self, results: List[Dict], test_name: str):
        """绘制延迟分布图"""
        if not MATPLOTLIB_AVAILABLE:
            return
        
        fig, ax = plt.subplots(figsize=(12, 6))
        
        # 提取延迟数据
        latencies = []
        for result in results:
            if isinstance(result, list):
                for item in result:
                    if 'duration' in item and item.get('success', False):
                        latencies.append(item['duration'] * 1000)  # 转换为 ms
        
        if not latencies:
            print(f"⚠️  No latency data found for {test_name}")
            return
        
        # 绘制直方图
        ax.hist(latencies, bins=50, alpha=0.7, color='blue', edgecolor='black')
        ax.set_xlabel('延迟 (ms)', fontsize=12)
        ax.set_ylabel('频次', fontsize=12)
        ax.set_title(f'{test_name} - 延迟分布', fontsize=14, fontweight='bold')
        ax.grid(True, alpha=0.3)
        
        # 添加统计信息
        if PANDAS_AVAILABLE:
            import statistics
            mean_lat = statistics.mean(latencies)
            median_lat = statistics.median(latencies)
            p95_lat = sorted(latencies)[int(len(latencies) * 0.95)]
            
            stats_text = f'平均: {mean_lat:.1f}ms\n中位数: {median_lat:.1f}ms\nP95: {p95_lat:.1f}ms'
            ax.text(0.98, 0.97, stats_text,
                   transform=ax.transAxes,
                   verticalalignment='top',
                   horizontalalignment='right',
                   bbox=dict(boxstyle='round', facecolor='wheat', alpha=0.5),
                   fontsize=10)
        
        output_path = self.output_dir / f'{test_name}_latency_dist.png'
        plt.tight_layout()
        plt.savefig(output_path, dpi=150)
        plt.close()
        
        print(f"✅ 生成延迟分布图: {output_path}")
    
    def plot_trend_over_time(self, results: List[Dict], test_name: str):
        """绘制性能趋势图"""
        if not MATPLOTLIB_AVAILABLE:
            return
        
        # 提取时间序列数据
        timestamps = []
        avg_latencies = []
        p95_latencies = []
        success_rates = []
        
        for result in results:
            if isinstance(result, list):
                successful = [r for r in result if r.get('success', False)]
                if successful:
                    durations = [r['duration'] * 1000 for r in successful]
                    timestamps.append(datetime.fromtimestamp(result[0].get('_timestamp', 0)))
                    avg_latencies.append(sum(durations) / len(durations))
                    p95_latencies.append(sorted(durations)[int(len(durations) * 0.95)])
                    success_rates.append(len(successful) / len(result) * 100)
        
        if not timestamps:
            print(f"⚠️  No trend data found for {test_name}")
            return
        
        fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(14, 10))
        
        # 延迟趋势
        ax1.plot(timestamps, avg_latencies, marker='o', label='平均延迟', linewidth=2)
        ax1.plot(timestamps, p95_latencies, marker='s', label='P95 延迟', linewidth=2)
        ax1.set_ylabel('延迟 (ms)', fontsize=12)
        ax1.set_title(f'{test_name} - 性能趋势', fontsize=14, fontweight='bold')
        ax1.legend()
        ax1.grid(True, alpha=0.3)
        ax1.xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m-%d'))
        plt.setp(ax1.xaxis.get_majorticklabels(), rotation=45)
        
        # 成功率趋势
        ax2.plot(timestamps, success_rates, marker='o', color='green', linewidth=2)
        ax2.set_xlabel('时间', fontsize=12)
        ax2.set_ylabel('成功率 (%)', fontsize=12)
        ax2.set_ylim([0, 105])
        ax2.grid(True, alpha=0.3)
        ax2.xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m-%d'))
        plt.setp(ax2.xaxis.get_majorticklabels(), rotation=45)
        
        output_path = self.output_dir / f'{test_name}_trend.png'
        plt.tight_layout()
        plt.savefig(output_path, dpi=150)
        plt.close()
        
        print(f"✅ 生成趋势图: {output_path}")
    
    def plot_pattern_comparison(self, results: Dict[str, List]):
        """绘制模式对比图"""
        if not MATPLOTLIB_AVAILABLE:
            return
        
        patterns = []
        avg_latencies = []
        p95_latencies = []
        
        for pattern_name, pattern_results in results.items():
            if pattern_results:
                successful = [r for r in pattern_results if r.get('success', False)]
                if successful:
                    durations = [r['duration'] * 1000 for r in successful]
                    patterns.append(pattern_name)
                    avg_latencies.append(sum(durations) / len(durations))
                    p95_latencies.append(sorted(durations)[int(len(durations) * 0.95)])
        
        if not patterns:
            print("⚠️  No pattern comparison data found")
            return
        
        fig, ax = plt.subplots(figsize=(12, 6))
        
        x = range(len(patterns))
        width = 0.35
        
        ax.bar([i - width/2 for i in x], avg_latencies, width, label='平均延迟', alpha=0.8)
        ax.bar([i + width/2 for i in x], p95_latencies, width, label='P95 延迟', alpha=0.8)
        
        ax.set_xlabel('模式', fontsize=12)
        ax.set_ylabel('延迟 (ms)', fontsize=12)
        ax.set_title('AI 模式性能对比', fontsize=14, fontweight='bold')
        ax.set_xticks(x)
        ax.set_xticklabels(patterns, rotation=45, ha='right')
        ax.legend()
        ax.grid(True, alpha=0.3, axis='y')
        
        output_path = self.output_dir / 'pattern_comparison.png'
        plt.tight_layout()
        plt.savefig(output_path, dpi=150)
        plt.close()
        
        print(f"✅ 生成模式对比图: {output_path}")
    
    def create_interactive_dashboard(self, results: List[Dict]):
        """创建交互式仪表板"""
        if not PLOTLY_AVAILABLE:
            print("⚠️  Plotly not available, skipping interactive dashboard")
            return
        
        # 创建子图
        fig = make_subplots(
            rows=2, cols=2,
            subplot_titles=('延迟分布', '吞吐量趋势', '成功率', '资源使用'),
            specs=[[{'type': 'histogram'}, {'type': 'scatter'}],
                   [{'type': 'bar'}, {'type': 'scatter'}]]
        )
        
        # 提取数据
        all_latencies = []
        for result in results:
            if isinstance(result, list):
                for item in result:
                    if 'duration' in item and item.get('success', False):
                        all_latencies.append(item['duration'] * 1000)
        
        # 1. 延迟分布直方图
        fig.add_trace(
            go.Histogram(x=all_latencies, name='延迟分布', nbinsx=50),
            row=1, col=1
        )
        
        # 2. 吞吐量趋势（示例数据）
        timestamps = list(range(len(results)))
        throughput = [100 + i * 5 for i in timestamps]
        
        fig.add_trace(
            go.Scatter(x=timestamps, y=throughput, mode='lines+markers', name='吞吐量'),
            row=1, col=2
        )
        
        # 3. 成功率
        success_rates = []
        for result in results:
            if isinstance(result, list):
                success_rate = sum(1 for r in result if r.get('success', False)) / len(result) * 100
                success_rates.append(success_rate)
        
        fig.add_trace(
            go.Bar(x=list(range(len(success_rates))), y=success_rates, name='成功率'),
            row=2, col=1
        )
        
        # 4. 资源使用（示例）
        memory_usage = [450 + i * 10 for i in range(len(results))]
        
        fig.add_trace(
            go.Scatter(x=timestamps, y=memory_usage, mode='lines', name='内存使用 (MB)'),
            row=2, col=2
        )
        
        # 更新布局
        fig.update_layout(
            title_text="Shannon 性能基准测试仪表板",
            showlegend=True,
            height=800
        )
        
        output_path = self.output_dir / 'dashboard.html'
        fig.write_html(str(output_path))
        
        print(f"✅ 生成交互式仪表板: {output_path}")
    
    def generate_all_visualizations(self):
        """生成所有可视化图表"""
        print("\n" + "=" * 60)
        print("Shannon 性能数据可视化")
        print("=" * 60 + "\n")
        
        # 加载所有结果
        all_results = self.load_results()
        
        if not all_results:
            print("⚠️  未找到测试结果文件")
            return
        
        print(f"加载了 {len(all_results)} 个测试结果文件\n")
        
        # 工作流测试可视化
        workflow_results = [r for r in all_results if 'workflow' in r.get('_filename', '')]
        if workflow_results:
            print("📊 生成工作流性能图表...")
            self.plot_latency_distribution(workflow_results, 'workflow')
            self.plot_trend_over_time(workflow_results, 'workflow')
        
        # 模式测试可视化
        pattern_results = {}
        for result in all_results:
            if 'pattern' in result.get('_filename', ''):
                for pattern_name, data in result.items():
                    if not pattern_name.startswith('_'):
                        pattern_results[pattern_name] = data
        
        if pattern_results:
            print("📊 生成模式性能图表...")
            self.plot_pattern_comparison(pattern_results)
        
        # 工具测试可视化
        tool_results = [r for r in all_results if 'tool' in r.get('_filename', '')]
        if tool_results:
            print("📊 生成工具性能图表...")
            self.plot_latency_distribution(tool_results, 'tools')
        
        # 交互式仪表板
        if PLOTLY_AVAILABLE:
            print("📊 生成交互式仪表板...")
            self.create_interactive_dashboard(all_results)
        
        print("\n" + "=" * 60)
        print(f"✅ 所有图表已生成到: {self.output_dir}")
        print("=" * 60)


def main():
    parser = argparse.ArgumentParser(description="Shannon 性能数据可视化")
    parser.add_argument("--results-dir", default="benchmarks/results",
                        help="测试结果目录")
    parser.add_argument("--output-dir", default="benchmarks/charts",
                        help="图表输出目录")
    
    args = parser.parse_args()
    
    visualizer = BenchmarkVisualizer(args.results_dir, args.output_dir)
    visualizer.generate_all_visualizations()


if __name__ == "__main__":
    main()

