#!/usr/bin/env python3
"""
模式基准测试的单元测试
"""

import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from pattern_bench import PatternBenchmark
except ImportError:
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.pattern_bench import PatternBenchmark


class TestPatternBenchmark(unittest.TestCase):
    """测试模式基准测试功能"""
    
    def setUp(self):
        """测试初始化"""
        self.benchmark = PatternBenchmark(use_simulation=True)
    
    def test_initialization(self):
        """测试初始化"""
        self.assertTrue(self.benchmark.use_simulation)
        self.assertIsNotNone(self.benchmark.endpoint)
    
    def test_chain_of_thought_pattern(self):
        """测试 Chain-of-Thought 模式"""
        result = self.benchmark.test_chain_of_thought("What is 2+2?")
        
        self.assertIsInstance(result, dict)
        self.assertIn('pattern', result)
        self.assertIn('duration', result)
        self.assertIn('success', result)
        
        self.assertEqual(result['pattern'], 'chain_of_thought')
        self.assertTrue(result['success'])
        self.assertGreater(result['duration'], 0)
    
    def test_tree_of_thoughts_pattern(self):
        """测试 Tree-of-Thoughts 模式"""
        result = self.benchmark.test_tree_of_thoughts("Solve the puzzle")
        
        self.assertIsInstance(result, dict)
        self.assertIn('pattern', result)
        self.assertIn('duration', result)
        self.assertIn('branches_explored', result)
        
        self.assertEqual(result['pattern'], 'tree_of_thoughts')
        self.assertGreater(result['branches_explored'], 0)
    
    def test_debate_pattern(self):
        """测试 Debate 模式"""
        result = self.benchmark.test_debate("Should we adopt AI?")
        
        self.assertIsInstance(result, dict)
        self.assertIn('pattern', result)
        self.assertIn('rounds', result)
        self.assertIn('duration', result)
        
        self.assertEqual(result['pattern'], 'debate')
        self.assertGreater(result['rounds'], 0)
    
    def test_reflection_pattern(self):
        """测试 Reflection 模式"""
        result = self.benchmark.test_reflection("Initial answer")
        
        self.assertIsInstance(result, dict)
        self.assertIn('pattern', result)
        self.assertIn('iterations', result)
        
        self.assertEqual(result['pattern'], 'reflection')
    
    def test_pattern_comparison(self):
        """测试模式对比"""
        patterns = ['chain_of_thought', 'tree_of_thoughts', 'debate']
        results = self.benchmark.compare_patterns("Test query", patterns)
        
        self.assertEqual(len(results), len(patterns))
        
        for result in results:
            self.assertIn('pattern', result)
            self.assertIn('duration', result)
            self.assertIn('success', result)
    
    def test_error_handling(self):
        """测试错误处理"""
        # 测试空查询
        result = self.benchmark.test_chain_of_thought("")
        self.assertIn('success', result)
        
        # 测试无效模式
        result = self.benchmark.test_pattern("invalid_pattern", "query")
        self.assertIsInstance(result, dict)
    
    def test_statistics_calculation(self):
        """测试统计计算"""
        results = [
            {'pattern': 'cot', 'duration': 1.0, 'success': True},
            {'pattern': 'cot', 'duration': 1.5, 'success': True},
            {'pattern': 'cot', 'duration': 1.2, 'success': True}
        ]
        
        stats = self.benchmark.calculate_pattern_statistics(results)
        
        self.assertIn('mean_duration', stats)
        self.assertIn('median_duration', stats)
        self.assertIn('success_rate', stats)
        
        self.assertAlmostEqual(stats['success_rate'], 1.0)
    
    def test_performance_targets(self):
        """测试性能目标"""
        # Chain-of-Thought 应该在合理时间内完成
        result = self.benchmark.test_chain_of_thought("Quick test")
        self.assertLess(result['duration'], 5.0)  # 5秒内


class TestPatternConfiguration(unittest.TestCase):
    """测试模式配置"""
    
    def test_custom_parameters(self):
        """测试自定义参数"""
        benchmark = PatternBenchmark(
            endpoint="custom:12345",
            use_simulation=True
        )
        self.assertEqual(benchmark.endpoint, "custom:12345")
    
    def test_pattern_registry(self):
        """测试模式注册表"""
        benchmark = PatternBenchmark(use_simulation=True)
        patterns = benchmark.list_available_patterns()
        
        self.assertIsInstance(patterns, list)
        self.assertGreater(len(patterns), 0)
        self.assertIn('chain_of_thought', patterns)


class TestPatternMetrics(unittest.TestCase):
    """测试模式度量"""
    
    def setUp(self):
        self.benchmark = PatternBenchmark(use_simulation=True)
    
    def test_quality_metrics(self):
        """测试质量度量"""
        result = self.benchmark.test_chain_of_thought("Test query")
        
        if 'quality_score' in result:
            self.assertGreaterEqual(result['quality_score'], 0)
            self.assertLessEqual(result['quality_score'], 1.0)
    
    def test_cost_tracking(self):
        """测试成本追踪"""
        result = self.benchmark.test_chain_of_thought("Test")
        
        if 'estimated_cost' in result:
            self.assertGreaterEqual(result['estimated_cost'], 0)
    
    def test_token_usage(self):
        """测试token使用统计"""
        result = self.benchmark.test_chain_of_thought("Test")
        
        if 'tokens_used' in result:
            self.assertGreater(result['tokens_used'], 0)


if __name__ == '__main__':
    unittest.main()

