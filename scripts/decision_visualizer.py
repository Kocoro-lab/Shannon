#!/usr/bin/env python3
"""
Shannon 决策可视化工具
将工程决策、风险评估和技术债务可视化
"""

import json
import sys
from pathlib import Path
from typing import Dict, List, Any
from datetime import datetime

try:
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    import matplotlib.patches as mpatches
    MATPLOTLIB_AVAILABLE = True
except ImportError:
    print("⚠️  matplotlib 未安装。运行: pip install matplotlib")
    MATPLOTLIB_AVAILABLE = False

try:
    import pandas as pd
    PANDAS_AVAILABLE = True
except ImportError:
    print("⚠️  pandas 未安装。运行: pip install pandas")
    PANDAS_AVAILABLE = False


class DecisionVisualizer:
    """决策可视化工具"""
    
    def __init__(self, output_dir="docs/visualizations"):
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.colors = {
            'high': '#d32f2f',      # 红色
            'medium': '#ffa000',    # 橙色
            'low': '#388e3c',       # 绿色
            'critical': '#b71c1c',  # 深红色
            'info': '#1976d2'       # 蓝色
        }
    
    def create_risk_matrix(self, risks: List[Dict]):
        """创建风险矩阵（影响 vs 概率）"""
        if not MATPLOTLIB_AVAILABLE:
            print("❌ 需要 matplotlib 来生成风险矩阵")
            return
        
        fig, ax = plt.subplots(figsize=(10, 8))
        
        # 影响和概率映射
        impact_map = {'高': 3, '中': 2, '低': 1}
        probability_map = {'高': 3, '中': 2, '低': 1}
        
        for risk in risks:
            impact = impact_map.get(risk.get('影响', '中'), 2)
            prob = probability_map.get(risk.get('概率', '中'), 2)
            
            # 根据风险级别选择颜色
            severity = impact * prob
            if severity >= 8:
                color = self.colors['critical']
                size = 300
            elif severity >= 5:
                color = self.colors['high']
                size = 200
            else:
                color = self.colors['low']
                size = 100
            
            ax.scatter(prob, impact, s=size, c=color, alpha=0.6, edgecolors='black')
            ax.annotate(
                risk.get('ID', ''),
                (prob, impact),
                fontsize=9,
                ha='center',
                va='center'
            )
        
        ax.set_xlabel('概率', fontsize=12, fontweight='bold')
        ax.set_ylabel('影响', fontsize=12, fontweight='bold')
        ax.set_title('风险热力矩阵', fontsize=14, fontweight='bold', pad=20)
        ax.set_xlim(0.5, 3.5)
        ax.set_ylim(0.5, 3.5)
        ax.set_xticks([1, 2, 3])
        ax.set_yticks([1, 2, 3])
        ax.set_xticklabels(['低', '中', '高'])
        ax.set_yticklabels(['低', '中', '高'])
        ax.grid(True, alpha=0.3)
        
        # 添加区域着色
        ax.axhspan(2.5, 3.5, 2.5/3.5, 1, alpha=0.1, color='red')  # 高风险区
        ax.axhspan(1.5, 2.5, 1.5/3.5, 2.5/3.5, alpha=0.1, color='orange')  # 中风险区
        
        # 图例
        legend_elements = [
            mpatches.Patch(color=self.colors['critical'], label='严重风险 (>8)'),
            mpatches.Patch(color=self.colors['high'], label='高风险 (5-8)'),
            mpatches.Patch(color=self.colors['low'], label='低风险 (<5)')
        ]
        ax.legend(handles=legend_elements, loc='upper left')
        
        plt.tight_layout()
        output_path = self.output_dir / 'risk_matrix.png'
        plt.savefig(output_path, dpi=300, bbox_inches='tight')
        plt.close()
        
        print(f"✅ 风险矩阵已保存到: {output_path}")
    
    def create_technical_debt_chart(self, debts: List[Dict]):
        """创建技术债务优先级图表"""
        if not MATPLOTLIB_AVAILABLE:
            print("❌ 需要 matplotlib 来生成技术债务图表")
            return
        
        fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(16, 6))
        
        # 按优先级分组
        priority_counts = {'高': 0, '中': 0, '低': 0}
        for debt in debts:
            priority = debt.get('优先级', '中')
            priority_counts[priority] = priority_counts.get(priority, 0) + 1
        
        # 图表1: 优先级分布（饼图）
        priorities = list(priority_counts.keys())
        counts = list(priority_counts.values())
        colors = [self.colors['high'], self.colors['medium'], self.colors['low']]
        
        wedges, texts, autotexts = ax1.pie(
            counts,
            labels=priorities,
            colors=colors,
            autopct='%1.1f%%',
            startangle=90
        )
        ax1.set_title('技术债务优先级分布', fontsize=14, fontweight='bold')
        
        # 图表2: 债务影响 vs 工作量矩阵
        impact_map = {'高': 3, '中': 2, '低': 1}
        effort_map = {'1天': 1, '2天': 2, '3天': 3, '5天': 5, '1周': 7}
        
        for debt in debts:
            impact = impact_map.get(debt.get('影响', '中'), 2)
            effort_str = debt.get('预计工作量', '2天')
            effort = effort_map.get(effort_str, 2)
            
            priority = debt.get('优先级', '中')
            if priority == '高':
                color = self.colors['high']
                size = 300
            elif priority == '中':
                color = self.colors['medium']
                size = 200
            else:
                color = self.colors['low']
                size = 100
            
            ax2.scatter(effort, impact, s=size, c=color, alpha=0.6, edgecolors='black')
            ax2.annotate(
                debt.get('ID', ''),
                (effort, impact),
                fontsize=9,
                ha='center',
                va='center'
            )
        
        ax2.set_xlabel('预计工作量（天）', fontsize=12, fontweight='bold')
        ax2.set_ylabel('业务影响', fontsize=12, fontweight='bold')
        ax2.set_title('技术债务影响-工作量矩阵', fontsize=14, fontweight='bold')
        ax2.grid(True, alpha=0.3)
        
        # 添加象限线
        ax2.axvline(x=3, color='gray', linestyle='--', alpha=0.5)
        ax2.axhline(y=2, color='gray', linestyle='--', alpha=0.5)
        
        # 添加象限标签
        ax2.text(1, 2.8, 'Quick Wins\n(优先)', ha='center', fontsize=10, color='green', fontweight='bold')
        ax2.text(5, 2.8, 'Major Projects\n(规划)', ha='center', fontsize=10, color='orange', fontweight='bold')
        ax2.text(1, 1.2, 'Fill Ins\n(顺手)', ha='center', fontsize=10, color='blue', fontweight='bold')
        ax2.text(5, 1.2, 'Ignore\n(推迟)', ha='center', fontsize=10, color='gray', fontweight='bold')
        
        plt.tight_layout()
        output_path = self.output_dir / 'technical_debt.png'
        plt.savefig(output_path, dpi=300, bbox_inches='tight')
        plt.close()
        
        print(f"✅ 技术债务图表已保存到: {output_path}")
    
    def create_decision_tree(self, decisions: List[Dict]):
        """创建决策流程图"""
        if not MATPLOTLIB_AVAILABLE:
            print("❌ 需要 matplotlib 来生成决策树")
            return
        
        fig, ax = plt.subplots(figsize=(14, 10))
        ax.axis('off')
        
        # 简化的决策树可视化
        y_start = 0.9
        y_step = 0.15
        
        for i, decision in enumerate(decisions):
            y = y_start - i * y_step
            
            # 决策框
            rect = mpatches.FancyBboxPatch(
                (0.1, y - 0.05),
                0.8,
                0.1,
                boxstyle="round,pad=0.01",
                linewidth=2,
                edgecolor='black',
                facecolor='lightblue',
                alpha=0.7
            )
            ax.add_patch(rect)
            
            # 决策文本
            decision_text = f"{decision.get('ID', 'ADR-X')}: {decision.get('标题', 'Decision')}"
            ax.text(0.5, y, decision_text, ha='center', va='center', fontsize=11, fontweight='bold')
            
            # 选项
            chosen = decision.get('选定方案', 'N/A')
            rejected = decision.get('拒绝方案', [])
            
            ax.text(
                0.5,
                y - 0.06,
                f"✅ 选定: {chosen}",
                ha='center',
                va='top',
                fontsize=9,
                color='green'
            )
            
            if rejected:
                ax.text(
                    0.5,
                    y - 0.09,
                    f"❌ 拒绝: {', '.join(rejected)}",
                    ha='center',
                    va='top',
                    fontsize=9,
                    color='red'
                )
        
        ax.set_title('架构决策记录 (ADR) 时间线', fontsize=16, fontweight='bold', pad=20)
        ax.set_xlim(0, 1)
        ax.set_ylim(0, 1)
        
        plt.tight_layout()
        output_path = self.output_dir / 'decision_tree.png'
        plt.savefig(output_path, dpi=300, bbox_inches='tight')
        plt.close()
        
        print(f"✅ 决策树已保存到: {output_path}")
    
    def create_cost_benefit_analysis(self, options: List[Dict], criteria: List[str]):
        """创建成本收益分析表"""
        if not MATPLOTLIB_AVAILABLE:
            print("❌ 需要 matplotlib 来生成成本收益分析")
            return
        
        fig, ax = plt.subplots(figsize=(12, 6))
        ax.axis('off')
        
        # 创建表格数据
        table_data = []
        header = ['方案'] + criteria + ['总分']
        
        for option in options:
            row = [option.get('名称', 'N/A')]
            total_score = 0
            
            for criterion in criteria:
                score = option.get('评分', {}).get(criterion, 0)
                row.append(f"{score}")
                total_score += score
            
            row.append(f"{total_score}")
            table_data.append(row)
        
        # 创建表格
        table = ax.table(
            cellText=table_data,
            colLabels=header,
            cellLoc='center',
            loc='center',
            colWidths=[0.2] + [0.15] * len(criteria) + [0.1]
        )
        
        table.auto_set_font_size(False)
        table.set_fontsize(10)
        table.scale(1, 2)
        
        # 样式化表头
        for i in range(len(header)):
            cell = table[(0, i)]
            cell.set_facecolor('#4CAF50')
            cell.set_text_props(weight='bold', color='white')
        
        # 高亮最佳选项
        if table_data:
            max_score = max(float(row[-1]) for row in table_data)
            for i, row in enumerate(table_data):
                if float(row[-1]) == max_score:
                    for j in range(len(header)):
                        cell = table[(i+1, j)]
                        cell.set_facecolor('#c8e6c9')
        
        ax.set_title('方案对比分析矩阵', fontsize=14, fontweight='bold', pad=20)
        
        plt.tight_layout()
        output_path = self.output_dir / 'cost_benefit_analysis.png'
        plt.savefig(output_path, dpi=300, bbox_inches='tight')
        plt.close()
        
        print(f"✅ 成本收益分析已保存到: {output_path}")
    
    def create_quality_metrics_dashboard(self, metrics: Dict):
        """创建质量指标仪表板"""
        if not MATPLOTLIB_AVAILABLE:
            print("❌ 需要 matplotlib 来生成仪表板")
            return
        
        fig, axes = plt.subplots(2, 2, figsize=(14, 10))
        fig.suptitle('Shannon 项目质量指标仪表板', fontsize=16, fontweight='bold')
        
        # 图表1: 测试覆盖率
        coverage = metrics.get('test_coverage', 70)
        self._create_gauge(axes[0, 0], coverage, '测试覆盖率', '%', target=85)
        
        # 图表2: 代码质量评分
        quality_score = metrics.get('code_quality', 8.5)
        self._create_gauge(axes[0, 1], quality_score, '代码质量评分', '/10', target=8.0, max_value=10)
        
        # 图表3: 自动化检查通过率
        check_pass_rate = metrics.get('check_pass_rate', 95)
        self._create_gauge(axes[1, 0], check_pass_rate, '自动化检查通过率', '%', target=90)
        
        # 图表4: 技术债务趋势
        debt_trend = metrics.get('debt_trend', [5, 4, 4, 3, 2])
        axes[1, 1].plot(debt_trend, marker='o', linewidth=2, markersize=8, color='#4CAF50')
        axes[1, 1].set_title('技术债务数量趋势', fontweight='bold')
        axes[1, 1].set_xlabel('迭代')
        axes[1, 1].set_ylabel('债务数量')
        axes[1, 1].grid(True, alpha=0.3)
        axes[1, 1].fill_between(range(len(debt_trend)), debt_trend, alpha=0.3, color='#4CAF50')
        
        plt.tight_layout()
        output_path = self.output_dir / 'quality_dashboard.png'
        plt.savefig(output_path, dpi=300, bbox_inches='tight')
        plt.close()
        
        print(f"✅ 质量指标仪表板已保存到: {output_path}")
    
    def _create_gauge(self, ax, value, title, unit, target=80, max_value=100):
        """创建仪表盘样式的图表"""
        # 确定颜色
        if value >= target:
            color = '#4CAF50'  # 绿色
        elif value >= target * 0.8:
            color = '#FFC107'  # 黄色
        else:
            color = '#F44336'  # 红色
        
        # 绘制半圆
        theta = (value / max_value) * 180
        
        wedge = mpatches.Wedge(
            (0.5, 0),
            0.4,
            0,
            180,
            facecolor='#E0E0E0',
            edgecolor='black',
            linewidth=2
        )
        ax.add_patch(wedge)
        
        wedge_filled = mpatches.Wedge(
            (0.5, 0),
            0.4,
            0,
            theta,
            facecolor=color,
            edgecolor='black',
            linewidth=2
        )
        ax.add_patch(wedge_filled)
        
        # 添加数值
        ax.text(0.5, 0.1, f"{value}{unit}", ha='center', va='center', fontsize=24, fontweight='bold')
        ax.text(0.5, -0.05, f"目标: {target}{unit}", ha='center', va='top', fontsize=10, color='gray')
        
        ax.set_xlim(0, 1)
        ax.set_ylim(-0.1, 0.5)
        ax.set_aspect('equal')
        ax.axis('off')
        ax.set_title(title, fontweight='bold', pad=10)
    
    def generate_all_visualizations(self):
        """生成所有可视化"""
        print("🎨 开始生成决策可视化...")
        print()
        
        # 示例数据
        risks = [
            {'ID': 'R1', '影响': '高', '概率': '中'},
            {'ID': 'R2', '影响': '高', '概率': '中'},
            {'ID': 'R3', '影响': '中', '概率': '低'},
            {'ID': 'R4', '影响': '低', '概率': '低'},
        ]
        
        debts = [
            {'ID': 'TD-001', '优先级': '中', '影响': '中', '预计工作量': '2天'},
            {'ID': 'TD-002', '优先级': '高', '影响': '高', '预计工作量': '1天'},
            {'ID': 'TD-003', '优先级': '低', '影响': '低', '预计工作量': '3天'},
            {'ID': 'TD-004', '优先级': '高', '影响': '中', '预计工作量': '2天'},
        ]
        
        decisions = [
            {
                'ID': 'ADR-001',
                '标题': '基准测试框架选择',
                '选定方案': '自研框架',
                '拒绝方案': ['第三方APM', '手动测试']
            },
            {
                'ID': 'ADR-002',
                '标题': 'Docker Registry 选择',
                '选定方案': 'GHCR',
                '拒绝方案': ['Docker Hub', '自托管']
            },
            {
                'ID': 'ADR-003',
                '标题': '文档维护策略',
                '选定方案': '人工翻译',
                '拒绝方案': ['机器翻译']
            }
        ]
        
        options = [
            {
                '名称': '方案A: 自研',
                '评分': {'开发成本': 7, '性能': 9, '维护性': 8, '安全性': 8}
            },
            {
                '名称': '方案B: 第三方',
                '评分': {'开发成本': 9, '性能': 7, '维护性': 6, '安全性': 7}
            }
        ]
        
        metrics = {
            'test_coverage': 85,
            'code_quality': 8.8,
            'check_pass_rate': 95,
            'debt_trend': [5, 4, 4, 3, 2]
        }
        
        self.create_risk_matrix(risks)
        self.create_technical_debt_chart(debts)
        self.create_decision_tree(decisions)
        self.create_cost_benefit_analysis(options, ['开发成本', '性能', '维护性', '安全性'])
        self.create_quality_metrics_dashboard(metrics)
        
        print()
        print("✅ 所有可视化已生成完毕！")
        print(f"📁 输出目录: {self.output_dir.absolute()}")


def main():
    """主函数"""
    visualizer = DecisionVisualizer()
    visualizer.generate_all_visualizations()


if __name__ == '__main__':
    main()

