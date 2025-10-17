#!/usr/bin/env python3
"""
工作流基准测试的单元测试
"""

import unittest
import sys
import time
from pathlib import Path

# 添加项目路径
sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from workflow_bench import WorkflowBenchmark
except ImportError:
    # Fallback 如果直接运行
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.workflow_bench import WorkflowBenchmark


class TestWorkflowBenchmark(unittest.TestCase):
    """测试工作流基准测试功能"""
    
    def setUp(self):
        """测试初始化"""
        # 始终使用模拟模式进行单元测试
        self.benchmark = WorkflowBenchmark(use_simulation=True)
    
    def test_initialization(self):
        """测试基准测试对象初始化"""
        self.assertTrue(self.benchmark.use_simulation)
        self.assertIsNotNone(self.benchmark.endpoint)
        self.assertIsNotNone(self.benchmark.api_key)
    
    def test_run_simple_task(self):
        """测试简单任务执行"""
        result = self.benchmark.run_simple_task(task_id="test-001")
        
        self.assertIsInstance(result, dict)
        self.assertIn('task_id', result)
        self.assertIn('duration', result)
        self.assertIn('success', result)
        
        self.assertEqual(result['task_id'], "test-001")
        self.assertTrue(result['success'])
        self.assertGreater(result['duration'], 0)
        self.assertLess(result['duration'], 5.0)  # 应该在 5 秒内完成
    
    def test_run_dag_workflow(self):
        """测试 DAG 工作流执行"""
        num_subtasks = 3
        result = self.benchmark.run_dag_workflow(num_subtasks=num_subtasks, task_id="dag-001")
        
        self.assertIsInstance(result, dict)
        self.assertIn('task_id', result)
        self.assertIn('duration', result)
        self.assertIn('success', result)
        self.assertIn('num_subtasks', result)
        
        self.assertEqual(result['task_id'], "dag-001")
        self.assertEqual(result['num_subtasks'], num_subtasks)
        self.assertTrue(result['success'])
    
    def test_parallel_tasks(self):
        """测试并行任务执行"""
        results = self.benchmark.run_parallel_tasks(num_tasks=5, concurrency=3)
        
        self.assertEqual(len(results), 5)
        
        for result in results:
            self.assertIn('duration', result)
            self.assertTrue(result['success'])
        
        # 验证统计信息
        durations = [r['duration'] for r in results]
        self.assertEqual(len(durations), 5)
        self.assertTrue(all(d > 0 for d in durations))
    
    def test_statistics_calculation(self):
        """测试统计计算"""
        results = [
            {"duration": 0.5, "success": True},
            {"duration": 0.7, "success": True},
            {"duration": 0.6, "success": True},
            {"duration": 0.8, "success": True},
            {"duration": 0.9, "success": True}
        ]
        
        stats = self.benchmark.calculate_statistics(results)
        
        self.assertIn('p50', stats)
        self.assertIn('p95', stats)
        self.assertIn('p99', stats)
        self.assertIn('mean', stats)
        self.assertIn('throughput', stats)
        
        # 验证统计值的合理性
        self.assertGreater(stats['p50'], 0)
        self.assertGreater(stats['p95'], stats['p50'])
        self.assertGreater(stats['p99'], stats['p95'])
    
    def test_error_handling(self):
        """测试错误处理"""
        # 模拟失败场景
        result = self.benchmark.run_simple_task(task_id="fail-001")
        
        # 即使在模拟模式下，也应该有完整的结构
        self.assertIn('task_id', result)
        self.assertIn('duration', result)
        self.assertIn('success', result)
    
    def test_output_format(self):
        """测试输出格式"""
        results = self.benchmark.run_parallel_tasks(num_tasks=2, concurrency=1)
        
        # 验证输出可以被 JSON 序列化
        import json
        try:
            json_str = json.dumps(results)
            reloaded = json.loads(json_str)
            self.assertEqual(len(reloaded), len(results))
        except Exception as e:
            self.fail(f"Result should be JSON serializable: {e}")


class TestBenchmarkConfiguration(unittest.TestCase):
    """测试配置和环境"""
    
    def test_custom_endpoint(self):
        """测试自定义端点配置"""
        custom_endpoint = "custom-host:12345"
        benchmark = WorkflowBenchmark(endpoint=custom_endpoint, use_simulation=True)
        self.assertEqual(benchmark.endpoint, custom_endpoint)
    
    def test_custom_api_key(self):
        """测试自定义 API 密钥"""
        custom_key = "my-custom-api-key"
        benchmark = WorkflowBenchmark(api_key=custom_key, use_simulation=True)
        self.assertEqual(benchmark.api_key, custom_key)
    
    def test_simulation_mode_forced(self):
        """测试强制模拟模式"""
        benchmark = WorkflowBenchmark(use_simulation=True)
        self.assertTrue(benchmark.use_simulation)


class TestPerformanceTargets(unittest.TestCase):
    """测试性能目标"""
    
    def setUp(self):
        self.benchmark = WorkflowBenchmark(use_simulation=True)
    
    def test_simple_task_latency(self):
        """测试简单任务延迟是否在目标范围内"""
        result = self.benchmark.run_simple_task(task_id="perf-001")
        
        # 在模拟模式下，简单任务应该在 1 秒内完成
        self.assertLess(result['duration'], 1.0)
    
    def test_dag_workflow_latency(self):
        """测试 DAG 工作流延迟"""
        result = self.benchmark.run_dag_workflow(num_subtasks=5, task_id="dag-perf-001")
        
        # 在模拟模式下，5 个子任务应该在 3 秒内完成
        self.assertLess(result['duration'], 3.0)


if __name__ == '__main__':
    unittest.main()

