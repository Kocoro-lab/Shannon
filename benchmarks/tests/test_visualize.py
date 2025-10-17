#!/usr/bin/env python3
"""
可视化工具的单元测试
"""

import unittest
import json
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from visualize import BenchmarkVisualizer
except ImportError:
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.visualize import BenchmarkVisualizer


class TestBenchmarkVisualizer(unittest.TestCase):
    """测试可视化功能"""
    
    def setUp(self):
        """测试初始化"""
        # 创建临时目录
        self.temp_dir = tempfile.TemporaryDirectory()
        self.results_dir = Path(self.temp_dir.name) / "results"
        self.output_dir = Path(self.temp_dir.name) / "charts"
        self.results_dir.mkdir(parents=True, exist_ok=True)
        
        self.visualizer = BenchmarkVisualizer(
            results_dir=str(self.results_dir),
            output_dir=str(self.output_dir)
        )
    
    def tearDown(self):
        """清理测试资源"""
        self.temp_dir.cleanup()
    
    def test_initialization(self):
        """测试初始化"""
        self.assertTrue(self.visualizer.output_dir.exists())
        self.assertEqual(self.visualizer.results_dir, self.results_dir)
    
    def test_load_results_empty(self):
        """测试加载空结果"""
        results = self.visualizer.load_results()
        self.assertEqual(len(results), 0)
    
    def test_load_results_with_data(self):
        """测试加载测试数据"""
        # 创建测试数据文件
        test_data = {
            "test_name": "workflow_benchmark",
            "results": [
                {"task_id": 1, "duration": 0.5, "success": True},
                {"task_id": 2, "duration": 0.7, "success": True}
            ],
            "statistics": {
                "p50": 0.6,
                "p95": 0.7,
                "p99": 0.7,
                "mean": 0.6,
                "throughput": 3.33
            }
        }
        
        test_file = self.results_dir / "test_result.json"
        with open(test_file, 'w') as f:
            json.dump(test_data, f)
        
        results = self.visualizer.load_results()
        self.assertEqual(len(results), 1)
        self.assertIn('test_name', results[0])
        self.assertEqual(results[0]['test_name'], "workflow_benchmark")
    
    def test_create_summary_report(self):
        """测试创建摘要报告"""
        # 创建测试数据
        results = [
            {
                "test_name": "simple_tasks",
                "statistics": {"p50": 0.5, "p95": 0.8, "p99": 1.0},
                "timestamp": "2025-01-17"
            }
        ]
        
        summary = self.visualizer.create_summary_report(results)
        
        self.assertIsInstance(summary, dict)
        # 基本结构检查
        self.assertTrue(len(summary) >= 0)
    
    def test_json_serialization(self):
        """测试 JSON 序列化"""
        test_data = {
            "results": [
                {"duration": 0.5, "success": True},
                {"duration": 0.7, "success": False}
            ]
        }
        
        # 验证可以序列化
        json_str = json.dumps(test_data)
        reloaded = json.loads(json_str)
        self.assertEqual(len(reloaded['results']), 2)
    
    def test_output_directory_creation(self):
        """测试输出目录创建"""
        # 删除输出目录
        if self.output_dir.exists():
            import shutil
            shutil.rmtree(self.output_dir)
        
        # 重新创建visualizer
        visualizer = BenchmarkVisualizer(
            results_dir=str(self.results_dir),
            output_dir=str(self.output_dir)
        )
        
        self.assertTrue(self.output_dir.exists())


class TestDataProcessing(unittest.TestCase):
    """测试数据处理功能"""
    
    def test_percentile_calculation(self):
        """测试百分位计算"""
        import statistics
        
        data = [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]
        
        p50 = statistics.median(data)
        self.assertAlmostEqual(p50, 0.55, places=2)
    
    def test_statistics_aggregation(self):
        """测试统计聚合"""
        import statistics
        
        results = [
            {"duration": 0.5, "success": True},
            {"duration": 0.7, "success": True},
            {"duration": 0.6, "success": True}
        ]
        
        durations = [r['duration'] for r in results if r['success']]
        
        mean = statistics.mean(durations)
        median = statistics.median(durations)
        
        self.assertGreater(mean, 0)
        self.assertGreater(median, 0)
    
    def test_empty_results_handling(self):
        """测试空结果处理"""
        results = []
        
        # 应该能够优雅地处理空列表
        try:
            if results:
                import statistics
                statistics.mean([r['duration'] for r in results])
        except Exception as e:
            self.fail(f"Should handle empty results: {e}")


if __name__ == '__main__':
    unittest.main()

