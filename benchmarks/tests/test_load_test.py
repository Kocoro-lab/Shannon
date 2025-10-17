#!/usr/bin/env python3
"""
负载测试的单元测试
"""

import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

try:
    from load_test import LoadTest
except ImportError:
    sys.path.insert(0, str(Path(__file__).parent.parent.parent))
    from benchmarks.load_test import LoadTest


class TestLoadTest(unittest.TestCase):
    """测试负载测试功能"""
    
    def setUp(self):
        """测试初始化"""
        self.load_tester = LoadTest(use_simulation=True)
    
    def test_initialization(self):
        """测试初始化"""
        self.assertTrue(self.load_tester.use_simulation)
        self.assertIsNotNone(self.load_tester.endpoint)
    
    def test_constant_load(self):
        """测试恒定负载"""
        result = self.load_tester.run_constant_load(
            requests_per_second=10,
            duration_seconds=5
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('total_requests', result)
        self.assertIn('successful_requests', result)
        self.assertIn('failed_requests', result)
        self.assertIn('mean_latency', result)
        
        # 验证基本统计
        self.assertGreaterEqual(result['total_requests'], 0)
        self.assertEqual(
            result['total_requests'],
            result['successful_requests'] + result['failed_requests']
        )
    
    def test_ramp_up_load(self):
        """测试递增负载"""
        result = self.load_tester.run_ramp_up_load(
            start_rps=1,
            end_rps=10,
            duration_seconds=10
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('phases', result)
        self.assertIn('summary', result)
        
        # 验证各阶段
        if 'phases' in result:
            self.assertGreater(len(result['phases']), 0)
            
            for phase in result['phases']:
                self.assertIn('rps', phase)
                self.assertIn('latency', phase)
    
    def test_spike_load(self):
        """测试突发负载"""
        result = self.load_tester.run_spike_load(
            baseline_rps=5,
            spike_rps=50,
            spike_duration=2
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('baseline_phase', result)
        self.assertIn('spike_phase', result)
        self.assertIn('recovery_phase', result)
        
        # 验证突发阶段的请求率更高
        if 'spike_phase' in result and 'baseline_phase' in result:
            spike_rps = result['spike_phase'].get('actual_rps', 0)
            baseline_rps = result['baseline_phase'].get('actual_rps', 0)
            self.assertGreater(spike_rps, baseline_rps)
    
    def test_stress_test(self):
        """测试压力测试"""
        result = self.load_tester.run_stress_test(
            max_rps=100,
            step=10,
            step_duration=5
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('breaking_point', result)
        self.assertIn('max_stable_rps', result)
        
        # 验证找到了断点
        if result.get('breaking_point'):
            self.assertIsInstance(result['breaking_point'], dict)
    
    def test_endurance_test(self):
        """测试耐久性测试"""
        result = self.load_tester.run_endurance_test(
            rps=5,
            duration_minutes=0.1  # 6秒，用于快速测试
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('total_duration', result)
        self.assertIn('memory_leak_detected', result)
        self.assertIn('performance_degradation', result)
    
    def test_concurrent_users(self):
        """测试并发用户模拟"""
        result = self.load_tester.simulate_concurrent_users(
            num_users=10,
            actions_per_user=3
        )
        
        self.assertIsInstance(result, dict)
        self.assertIn('total_users', result)
        self.assertIn('completed_users', result)
        
        self.assertEqual(result['total_users'], 10)
    
    def test_error_rate_calculation(self):
        """测试错误率计算"""
        results = [
            {'success': True},
            {'success': True},
            {'success': False},
            {'success': True},
            {'success': False}
        ]
        
        error_rate = self.load_tester.calculate_error_rate(results)
        
        self.assertAlmostEqual(error_rate, 0.4)  # 2/5 = 40%
    
    def test_latency_percentiles(self):
        """测试延迟百分位计算"""
        latencies = [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]
        
        percentiles = self.load_tester.calculate_percentiles(latencies)
        
        self.assertIn('p50', percentiles)
        self.assertIn('p95', percentiles)
        self.assertIn('p99', percentiles)
        
        self.assertGreater(percentiles['p95'], percentiles['p50'])
        self.assertGreater(percentiles['p99'], percentiles['p95'])


class TestLoadTestMetrics(unittest.TestCase):
    """测试负载测试度量"""
    
    def setUp(self):
        self.load_tester = LoadTester(use_simulation=True)
    
    def test_throughput_calculation(self):
        """测试吞吐量计算"""
        requests = 1000
        duration = 10  # 秒
        
        throughput = self.load_tester.calculate_throughput(requests, duration)
        
        self.assertEqual(throughput, 100)  # 1000/10 = 100 RPS
    
    def test_resource_utilization_tracking(self):
        """测试资源利用率追踪"""
        result = self.load_tester.run_constant_load(
            requests_per_second=5,
            duration_seconds=5,
            track_resources=True
        )
        
        if 'resource_utilization' in result:
            self.assertIn('cpu_percent', result['resource_utilization'])
            self.assertIn('memory_mb', result['resource_utilization'])
    
    def test_response_time_distribution(self):
        """测试响应时间分布"""
        latencies = [0.1] * 50 + [0.5] * 30 + [1.0] * 20
        
        distribution = self.load_tester.analyze_latency_distribution(latencies)
        
        self.assertIn('histogram', distribution)
        self.assertIn('statistics', distribution)


class TestLoadTestConfiguration(unittest.TestCase):
    """测试负载测试配置"""
    
    def test_custom_endpoint(self):
        """测试自定义端点"""
        tester = LoadTester(endpoint="custom:9999", use_simulation=True)
        self.assertEqual(tester.endpoint, "custom:9999")
    
    def test_timeout_configuration(self):
        """测试超时配置"""
        tester = LoadTester(timeout_seconds=30, use_simulation=True)
        self.assertEqual(tester.timeout_seconds, 30)
    
    def test_error_threshold(self):
        """测试错误阈值配置"""
        tester = LoadTester(error_threshold=0.05, use_simulation=True)
        self.assertEqual(tester.error_threshold, 0.05)


if __name__ == '__main__':
    unittest.main()

