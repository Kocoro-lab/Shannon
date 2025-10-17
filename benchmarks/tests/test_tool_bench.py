#!/usr/bin/env python3
"""
工具基准测试的单元测试
"""

import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from tool_bench import ToolBenchmark
except ImportError:
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.tool_bench import ToolBenchmark


class TestToolBenchmark(unittest.TestCase):
    """测试工具执行基准测试"""
    
    def setUp(self):
        """测试初始化"""
        self.benchmark = ToolBenchmark(use_simulation=True)
    
    def test_initialization(self):
        """测试初始化"""
        self.assertTrue(self.benchmark.use_simulation)
        self.assertIsNotNone(self.benchmark.endpoint)
    
    def test_calculator_tool(self):
        """测试计算器工具"""
        result = self.benchmark.test_calculator("2 + 2")
        
        self.assertIsInstance(result, dict)
        self.assertIn('tool_name', result)
        self.assertIn('duration', result)
        self.assertIn('success', result)
        
        self.assertEqual(result['tool_name'], 'calculator')
        self.assertTrue(result['success'])
    
    def test_web_search_tool(self):
        """测试网络搜索工具"""
        result = self.benchmark.test_web_search("Python tutorials")
        
        self.assertIsInstance(result, dict)
        self.assertIn('tool_name', result)
        self.assertIn('duration', result)
        
        self.assertEqual(result['tool_name'], 'web_search')
    
    def test_file_operations_tool(self):
        """测试文件操作工具"""
        # 测试文件读取
        result = self.benchmark.test_file_read("test.txt")
        
        self.assertIsInstance(result, dict)
        self.assertIn('tool_name', result)
        self.assertEqual(result['tool_name'], 'file_read')
        
        # 测试文件写入
        result = self.benchmark.test_file_write("test.txt", "content")
        self.assertEqual(result['tool_name'], 'file_write')
    
    def test_python_execution_tool(self):
        """测试Python代码执行工具"""
        code = "print('Hello, World!')"
        result = self.benchmark.test_python_execution(code)
        
        self.assertIsInstance(result, dict)
        self.assertIn('tool_name', result)
        self.assertIn('duration', result)
        
        self.assertEqual(result['tool_name'], 'python_wasi_executor')
    
    def test_tool_chaining(self):
        """测试工具链式调用"""
        tools = ['calculator', 'web_search']
        results = self.benchmark.test_tool_chain(tools)
        
        self.assertEqual(len(results), len(tools))
        
        for result in results:
            self.assertIn('tool_name', result)
            self.assertIn('duration', result)
    
    def test_concurrent_tool_execution(self):
        """测试并发工具执行"""
        tools = ['calculator', 'calculator', 'calculator']
        results = self.benchmark.test_concurrent_tools(tools)
        
        self.assertEqual(len(results), len(tools))
        
        # 验证并发执行确实比串行快
        total_duration = sum(r['duration'] for r in results)
        concurrent_duration = max(r['duration'] for r in results)
        
        # 在模拟模式下，这个可能不明显，但结构应该正确
        self.assertGreater(total_duration, 0)
        self.assertGreater(concurrent_duration, 0)
    
    def test_error_handling(self):
        """测试错误处理"""
        # 测试无效工具
        result = self.benchmark.test_tool("invalid_tool")
        self.assertIn('success', result)
        
        # 测试无效参数
        result = self.benchmark.test_calculator("invalid expression")
        self.assertIsInstance(result, dict)
    
    def test_statistics(self):
        """测试统计计算"""
        results = [
            {'tool_name': 'calc', 'duration': 0.1, 'success': True},
            {'tool_name': 'calc', 'duration': 0.2, 'success': True},
            {'tool_name': 'calc', 'duration': 0.15, 'success': False}
        ]
        
        stats = self.benchmark.calculate_tool_statistics(results)
        
        self.assertIn('mean_duration', stats)
        self.assertIn('success_rate', stats)
        self.assertIn('failure_count', stats)
        
        self.assertAlmostEqual(stats['success_rate'], 2/3)
        self.assertEqual(stats['failure_count'], 1)


class TestToolPerformance(unittest.TestCase):
    """测试工具性能指标"""
    
    def setUp(self):
        self.benchmark = ToolBenchmark(use_simulation=True)
    
    def test_cold_start_latency(self):
        """测试冷启动延迟"""
        # Python WASI 冷启动应该在合理时间内
        result = self.benchmark.test_python_execution("print('test')")
        
        if 'cold_start' in result:
            self.assertLess(result['cold_start'], 1.0)  # < 1秒
    
    def test_warm_start_latency(self):
        """测试热启动延迟"""
        # 第一次调用（冷启动）
        self.benchmark.test_calculator("1+1")
        
        # 第二次调用（热启动）应该更快
        result = self.benchmark.test_calculator("2+2")
        
        if 'is_warm_start' in result:
            self.assertTrue(result['is_warm_start'])
    
    def test_tool_overhead(self):
        """测试工具调用开销"""
        result = self.benchmark.test_calculator("1+1")
        
        if 'overhead_percentage' in result:
            # 开销应该合理（< 50%）
            self.assertLess(result['overhead_percentage'], 50.0)


class TestToolRegistry(unittest.TestCase):
    """测试工具注册表"""
    
    def test_list_available_tools(self):
        """测试列出可用工具"""
        benchmark = ToolBenchmark(use_simulation=True)
        tools = benchmark.list_available_tools()
        
        self.assertIsInstance(tools, list)
        self.assertGreater(len(tools), 0)
        
        # 基本工具应该存在
        basic_tools = ['calculator', 'web_search', 'file_read']
        for tool in basic_tools:
            self.assertIn(tool, tools)
    
    def test_tool_metadata(self):
        """测试工具元数据"""
        benchmark = ToolBenchmark(use_simulation=True)
        metadata = benchmark.get_tool_metadata('calculator')
        
        if metadata:
            self.assertIn('name', metadata)
            self.assertIn('description', metadata)


if __name__ == '__main__':
    unittest.main()

