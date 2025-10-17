#!/usr/bin/env python3
"""
配置模块的单元测试
"""

import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from config import safe_percentile, validate_config, SIMULATION_DELAYS
except ImportError:
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.config import safe_percentile, validate_config, SIMULATION_DELAYS


class TestSafePercentile(unittest.TestCase):
    """测试safe_percentile函数的边界情况"""
    
    def test_empty_list(self):
        """测试空列表"""
        result = safe_percentile([], 0.50)
        self.assertIsNone(result)
        
        result = safe_percentile([], 0.95)
        self.assertIsNone(result)
    
    def test_single_element(self):
        """测试单个元素"""
        result = safe_percentile([5], 0.50)
        self.assertEqual(result, 5)
        
        result = safe_percentile([5], 0.95)
        self.assertEqual(result, 5)
        
        result = safe_percentile([5], 0.99)
        self.assertEqual(result, 5)
    
    def test_two_elements(self):
        """测试两个元素"""
        result = safe_percentile([1, 2], 0.50)
        self.assertIn(result, [1, 2])
        
        result = safe_percentile([1, 2], 0.95)
        self.assertEqual(result, 2)  # 应该取第二个
    
    def test_normal_list(self):
        """测试正常列表"""
        data = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
        
        # P50应该接近中位数
        p50 = safe_percentile(data, 0.50)
        self.assertGreaterEqual(p50, 5)
        self.assertLessEqual(p50, 6)
        
        # P95应该接近高位
        p95 = safe_percentile(data, 0.95)
        self.assertGreaterEqual(p95, 9)
        
        # P99应该是最大值或接近最大值
        p99 = safe_percentile(data, 0.99)
        self.assertEqual(p99, 10)
    
    def test_percentile_zero(self):
        """测试P0（最小值）"""
        data = [1, 2, 3, 4, 5]
        result = safe_percentile(data, 0.0)
        self.assertEqual(result, 1)
    
    def test_percentile_one(self):
        """测试P100（最大值）"""
        data = [1, 2, 3, 4, 5]
        result = safe_percentile(data, 1.0)
        self.assertEqual(result, 5)
    
    def test_unsorted_list(self):
        """测试未排序列表（函数假设已排序）"""
        # 注意：safe_percentile假设列表已排序
        # 这个测试验证在已排序列表上的行为
        data = sorted([5, 1, 3, 2, 4])
        result = safe_percentile(data, 0.50)
        self.assertEqual(result, 3)
    
    def test_duplicate_values(self):
        """测试重复值"""
        data = [1, 1, 1, 2, 2]
        result = safe_percentile(data, 0.50)
        self.assertIn(result, [1, 2])
    
    def test_large_list(self):
        """测试大列表"""
        data = list(range(1000))
        
        p50 = safe_percentile(data, 0.50)
        self.assertAlmostEqual(p50, 500, delta=50)
        
        p95 = safe_percentile(data, 0.95)
        self.assertGreaterEqual(p95, 950)
        
        p99 = safe_percentile(data, 0.99)
        self.assertGreaterEqual(p99, 990)


class TestConfigValidation(unittest.TestCase):
    """测试配置验证"""
    
    def test_validate_config_succeeds(self):
        """测试配置验证通过"""
        # 应该不抛出异常
        try:
            validate_config()
            success = True
        except ValueError:
            success = False
        
        self.assertTrue(success)
    
    def test_simulation_delays_positive(self):
        """测试所有模拟延迟为正数"""
        for key, delay in SIMULATION_DELAYS.items():
            with self.subTest(pattern=key):
                self.assertGreaterEqual(delay, 0, 
                    f"Delay for {key} should be non-negative, got {delay}")


class TestEdgeCases(unittest.TestCase):
    """测试其他边界情况"""
    
    def test_safe_percentile_float_values(self):
        """测试浮点数值"""
        data = [0.1, 0.2, 0.3, 0.4, 0.5]
        result = safe_percentile(data, 0.50)
        self.assertIsInstance(result, float)
        self.assertAlmostEqual(result, 0.3, delta=0.1)
    
    def test_safe_percentile_negative_values(self):
        """测试负数值"""
        data = [-5, -3, -1, 0, 2]
        result = safe_percentile(data, 0.50)
        self.assertLessEqual(result, 0)
    
    def test_safe_percentile_mixed_types_raises(self):
        """测试混合类型（应该在实际使用中避免）"""
        # 这个测试验证函数在混合类型下的行为
        # 实际使用中应该避免这种情况
        data = [1, 2.0, 3]  # 整数和浮点数混合
        result = safe_percentile(data, 0.50)
        # 应该能正常工作（Python会处理类型转换）
        self.assertIsNotNone(result)


if __name__ == '__main__':
    unittest.main()


