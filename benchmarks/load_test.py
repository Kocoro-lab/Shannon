#!/usr/bin/env python3
"""
Shannon 负载测试和压力测试
模拟真实用户负载，测试系统在高并发下的表现
"""

import time
import argparse
import statistics
import json
import sys
import random
from typing import List, Dict, Any
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, asdict
from datetime import datetime

try:
    import grpc
    from google.protobuf import struct_pb2
    sys.path.insert(0, './clients/python/src')
    from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc, common_pb2
    GRPC_AVAILABLE = True
except ImportError as e:
    print(f"⚠️  Warning: gRPC imports failed: {e}")
    GRPC_AVAILABLE = False


@dataclass
class RequestResult:
    """单个请求的结果"""
    timestamp: float
    duration: float
    success: bool
    status_code: str = ""
    error: str = ""
    request_type: str = "task"


class LoadTest:
    """负载测试工具"""
    
    def __init__(self, endpoint="localhost:50052", api_key="test-key", use_simulation=False):
        self.endpoint = endpoint
        self.api_key = api_key
        self.use_simulation = use_simulation or not GRPC_AVAILABLE
        self.results: List[RequestResult] = []
        
    def _get_metadata(self):
        return [('x-api-key', self.api_key)]
    
    def _create_channel(self):
        """为每个线程创建独立的 gRPC channel"""
        if self.use_simulation:
            return None
        return grpc.insecure_channel(self.endpoint)
    
    def _send_request(self, user_id: int, request_num: int) -> RequestResult:
        """发送单个请求"""
        start = time.time()
        timestamp = start
        
        # Random query selection for realistic load
        queries = [
            "Calculate the factorial of 20",
            "Search for recent AI developments",
            "Write a Python function to sort a list",
            "Explain quantum computing in simple terms",
            "What's the weather like today?",
            "Debug this code: def foo(): return x + 1",
            "Summarize the latest tech news",
            "Create a TODO list for project planning",
        ]
        
        query = random.choice(queries)
        
        try:
            if self.use_simulation:
                # Simulate varying response times
                base_latency = 0.5
                variation = random.gauss(0, 0.15)  # 正态分布变化
                latency = max(0.1, base_latency + variation)
                time.sleep(latency)
                
                # Simulate occasional failures (5% failure rate)
                success = random.random() > 0.05
                status = "completed" if success else "failed"
                error = "" if success else "Simulated timeout"
            else:
                channel = self._create_channel()
                client = orchestrator_pb2_grpc.OrchestratorServiceStub(channel)
                
                request = orchestrator_pb2.TaskRequest(
                    query=query,
                    user_id=f"load-test-user-{user_id}",
                    mode=common_pb2.EXECUTION_MODE_STANDARD,
                )
                
                response = client.SubmitTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=30.0
                )
                
                success = response.status == "completed"
                status = response.status
                error = ""
                
                if channel:
                    channel.close()
        
        except Exception as e:
            success = False
            status = "error"
            error = str(e)
        
        duration = time.time() - start
        
        return RequestResult(
            timestamp=timestamp,
            duration=duration,
            success=success,
            status_code=status,
            error=error
        )
    
    def run_constant_load(self, users: int, duration_seconds: int, requests_per_user: int = None):
        """恒定负载测试"""
        print(f"\n📊 恒定负载测试")
        print(f"   并发用户: {users}, 持续时间: {duration_seconds}s")
        print("-" * 60)
        
        start_time = time.time()
        end_time = start_time + duration_seconds
        
        completed = 0
        with ThreadPoolExecutor(max_workers=users) as executor:
            futures = []
            user_requests = [0] * users
            
            while time.time() < end_time:
                # 为每个用户提交请求
                for user_id in range(users):
                    if requests_per_user and user_requests[user_id] >= requests_per_user:
                        continue
                    
                    future = executor.submit(self._send_request, user_id, user_requests[user_id])
                    futures.append(future)
                    user_requests[user_id] += 1
                
                # 收集完成的请求
                done_futures = [f for f in futures if f.done()]
                for future in done_futures:
                    try:
                        result = future.result()
                        self.results.append(result)
                        completed += 1
                        
                        if completed % 50 == 0:
                            elapsed = time.time() - start_time
                            rate = completed / elapsed
                            print(f"  已完成: {completed} 请求, 速率: {rate:.1f} req/s")
                    except Exception as e:
                        print(f"  ❌ 请求失败: {e}")
                    
                    futures.remove(future)
                
                time.sleep(0.1)  # 避免过度轮询
            
            # 等待剩余请求完成
            print("\n  等待剩余请求完成...")
            for future in as_completed(futures):
                try:
                    result = future.result()
                    self.results.append(result)
                    completed += 1
                except Exception as e:
                    print(f"  ❌ 请求失败: {e}")
        
        actual_duration = time.time() - start_time
        print(f"\n✅ 测试完成: {completed} 请求, 实际用时: {actual_duration:.1f}s")
        
        # 计算统计数据
        successful = [r for r in self.results if r.success]
        failed = [r for r in self.results if not r.success]
        
        mean_latency = statistics.mean([r.duration for r in successful]) if successful else 0
        
        return {
            'total_requests': len(self.results),
            'successful_requests': len(successful),
            'failed_requests': len(failed),
            'mean_latency': mean_latency,
            'actual_duration': actual_duration,
        }
    
    def run_ramp_up_load(self, max_users: int, ramp_up_seconds: int, hold_seconds: int):
        """渐进式负载测试"""
        print(f"\n📊 渐进式负载测试")
        print(f"   最大用户: {max_users}, 爬坡时间: {ramp_up_seconds}s, 保持时间: {hold_seconds}s")
        print("-" * 60)
        
        start_time = time.time()
        completed = 0
        
        with ThreadPoolExecutor(max_workers=max_users) as executor:
            futures = []
            current_users = 0
            
            # Phase 1: Ramp up
            print("\n[Phase 1] 逐步增加负载...")
            ramp_end = start_time + ramp_up_seconds
            
            while time.time() < ramp_end:
                elapsed = time.time() - start_time
                target_users = int((elapsed / ramp_up_seconds) * max_users)
                
                # 增加用户数
                while current_users < target_users:
                    user_id = current_users
                    future = executor.submit(self._send_request, user_id, 0)
                    futures.append(future)
                    current_users += 1
                
                # 收集结果
                done_futures = [f for f in futures if f.done()]
                for future in done_futures:
                    try:
                        result = future.result()
                        self.results.append(result)
                        completed += 1
                    except:
                        pass
                    futures.remove(future)
                
                if completed > 0 and completed % 20 == 0:
                    print(f"  当前用户: {current_users}/{max_users}, 已完成: {completed}")
                
                time.sleep(0.1)
            
            # Phase 2: Hold at max load
            print(f"\n[Phase 2] 保持最大负载 {max_users} 用户...")
            hold_end = time.time() + hold_seconds
            
            while time.time() < hold_end:
                # 为每个用户提交新请求
                for user_id in range(max_users):
                    future = executor.submit(self._send_request, user_id, completed)
                    futures.append(future)
                
                # 收集结果
                done_futures = [f for f in futures if f.done()]
                for future in done_futures:
                    try:
                        result = future.result()
                        self.results.append(result)
                        completed += 1
                    except:
                        pass
                    futures.remove(future)
                
                if completed % 50 == 0:
                    rate = len([r for r in self.results if r.timestamp > time.time() - 1]) / 1.0
                    print(f"  已完成: {completed}, 当前速率: {rate:.1f} req/s")
                
                time.sleep(0.5)
            
            # 等待剩余请求
            print("\n  等待剩余请求完成...")
            for future in as_completed(futures):
                try:
                    result = future.result()
                    self.results.append(result)
                    completed += 1
                except:
                    pass
        
        total_duration = time.time() - start_time
        print(f"\n✅ 测试完成: {completed} 请求, 总用时: {total_duration:.1f}s")
    
    def run_spike_test(self, normal_users: int, spike_users: int, duration: int):
        """峰值冲击测试"""
        print(f"\n📊 峰值冲击测试")
        print(f"   正常负载: {normal_users} 用户, 峰值负载: {spike_users} 用户")
        print("-" * 60)
        
        start_time = time.time()
        
        with ThreadPoolExecutor(max_workers=spike_users) as executor:
            # Phase 1: Normal load
            print("\n[Phase 1] 正常负载...")
            for _ in range(duration // 3):
                futures = [executor.submit(self._send_request, i, 0) for i in range(normal_users)]
                for future in as_completed(futures):
                    try:
                        self.results.append(future.result())
                    except:
                        pass
                time.sleep(1)
            
            # Phase 2: Spike
            print(f"\n[Phase 2] 负载峰值冲击! ({spike_users} 用户)")
            spike_futures = [executor.submit(self._send_request, i, 0) for i in range(spike_users)]
            for future in as_completed(spike_futures):
                try:
                    self.results.append(future.result())
                except:
                    pass
            
            # Phase 3: Back to normal
            print("\n[Phase 3] 恢复正常负载...")
            for _ in range(duration // 3):
                futures = [executor.submit(self._send_request, i, 0) for i in range(normal_users)]
                for future in as_completed(futures):
                    try:
                        self.results.append(future.result())
                    except:
                        pass
                time.sleep(1)
        
        total_duration = time.time() - start_time
        print(f"\n✅ 峰值测试完成, 总用时: {total_duration:.1f}s")
        
        # 返回各阶段结果
        return {
            'baseline_phase': {'users': normal_users},
            'spike_phase': {'users': spike_users, 'duration': duration},
            'recovery_phase': {'users': normal_users},
            'total_duration': total_duration
        }
    
    def simulate_concurrent_users(self, num_users: int, actions_per_user: int):
        """模拟并发用户测试"""
        print(f"\n📊 并发用户模拟测试")
        print(f"   用户数: {num_users}, 每用户操作: {actions_per_user}")
        print("-" * 60)
        
        completed = 0
        with ThreadPoolExecutor(max_workers=num_users) as executor:
            futures = []
            
            # 为每个用户提交操作
            for user_id in range(num_users):
                for action_num in range(actions_per_user):
                    future = executor.submit(self._send_request, user_id, action_num)
                    futures.append(future)
            
            # 收集结果
            for future in as_completed(futures):
                try:
                    result = future.result()
                    self.results.append(result)
                    completed += 1
                except Exception as e:
                    print(f"  ❌ 请求失败: {e}")
        
        print(f"\n✅ 并发用户测试完成: {completed} 个操作")
        return {
            'total_requests': completed,
            'total_users': num_users,
            'completed_users': num_users,  # 在模拟模式下都完成
            'actions_per_user': actions_per_user,
            'results': self.results
        }
    
    def run_endurance_test(self, rps: int, duration_minutes: float):
        """耐久性测试（长时间运行）"""
        duration_seconds = int(duration_minutes * 60)
        print(f"\n📊 耐久性测试")
        print(f"   目标速率: {rps} req/s, 持续时间: {duration_minutes} 分钟")
        print("-" * 60)
        
        # 使用run_constant_load
        users = max(1, rps // 2)  # 估算并发用户数
        requests_per_user = max(1, (rps * duration_seconds) // users)
        
        self.run_constant_load(users, duration_seconds, requests_per_user)
        print(f"\n✅ 耐久性测试完成")
        
        # 返回耐久性测试特定的结果格式
        return {
            'duration_minutes': duration_minutes,
            'target_rps': rps,
            'total_requests': len(self.results),
            'successful_requests': sum(1 for r in self.results if r.success),
            'error_rate': self.calculate_error_rate(self.results)
        }
    
    def run_stress_test(self, max_rps: int, step: int, step_duration: int):
        """压力测试 - 逐步增加负载直到失败"""
        print(f"\n📊 压力测试")
        print(f"   最大RPS: {max_rps}, 步长: {step}, 每步时长: {step_duration}s")
        print("-" * 60)
        
        current_rps = step
        stress_results = []
        
        while current_rps <= max_rps:
            print(f"\n[压力级别 {current_rps} RPS]")
            self.results = []  # 重置结果
            
            users = max(1, current_rps // 2)
            self.run_constant_load(users, step_duration, None)
            
            error_rate = self.calculate_error_rate(self.results)
            avg_latency = statistics.mean([r.duration for r in self.results if r.success]) if self.results else 0
            
            stress_results.append({
                'rps': current_rps,
                'error_rate': error_rate,
                'avg_latency': avg_latency,
                'total_requests': len(self.results)
            })
            
            print(f"  错误率: {error_rate*100:.1f}%, 平均延迟: {avg_latency*1000:.0f}ms")
            
            # 如果错误率超过10%，停止测试
            if error_rate > 0.1:
                print(f"\n⚠️  达到系统极限（错误率>10%），停止测试")
                break
            
            current_rps += step
        
        print(f"\n✅ 压力测试完成")
        return stress_results
    
    def calculate_error_rate(self, results: List) -> float:
        """计算错误率"""
        if not results:
            return 0.0
        
        # 处理不同类型的结果
        if isinstance(results[0], dict):
            failed = sum(1 for r in results if not r.get('success', True))
        else:
            failed = sum(1 for r in results if not r.success)
        
        return failed / len(results)
    
    def calculate_percentiles(self, latencies: List[float]) -> Dict[str, float]:
        """计算延迟百分位数"""
        if not latencies:
            return {'p50': 0, 'p95': 0, 'p99': 0}
        
        sorted_latencies = sorted(latencies)
        
        # 导入safe_percentile
        import sys
        from pathlib import Path
        sys.path.insert(0, str(Path(__file__).parent))
        from config import safe_percentile
        
        return {
            'p50': safe_percentile(sorted_latencies, 0.50) or 0,
            'p95': safe_percentile(sorted_latencies, 0.95) or 0,
            'p99': safe_percentile(sorted_latencies, 0.99) or 0,
        }
    
    def print_summary(self):
        """打印测试摘要"""
        if not self.results:
            print("\n⚠️  无测试结果")
            return
        
        successful = [r for r in self.results if r.success]
        failed = [r for r in self.results if not r.success]
        
        durations = [r.duration for r in successful]
        
        print("\n" + "=" * 60)
        print("负载测试总结")
        print("=" * 60)
        
        print(f"\n请求统计:")
        print(f"  总请求数: {len(self.results)}")
        print(f"  成功: {len(successful)} ({len(successful)/len(self.results)*100:.1f}%)")
        print(f"  失败: {len(failed)} ({len(failed)/len(self.results)*100:.1f}%)")
        
        if durations:
            sorted_durations = sorted(durations)
            p50 = sorted_durations[len(sorted_durations) // 2]
            p90 = sorted_durations[int(len(sorted_durations) * 0.90)]
            p95 = sorted_durations[int(len(sorted_durations) * 0.95)]
            p99 = sorted_durations[min(int(len(sorted_durations) * 0.99), len(sorted_durations) - 1)]
            
            print(f"\n响应时间:")
            print(f"  平均: {statistics.mean(durations):.3f}s")
            print(f"  中位数: {statistics.median(durations):.3f}s")
            print(f"  最小: {min(durations):.3f}s")
            print(f"  最大: {max(durations):.3f}s")
            print(f"\n  百分位数:")
            print(f"    P50: {p50:.3f}s")
            print(f"    P90: {p90:.3f}s")
            print(f"    P95: {p95:.3f}s")
            print(f"    P99: {p99:.3f}s")
        
        # Calculate throughput over time
        if self.results:
            start = min(r.timestamp for r in self.results)
            end = max(r.timestamp + r.duration for r in self.results)
            total_time = end - start
            throughput = len(self.results) / total_time if total_time > 0 else 0
            
            print(f"\n吞吐量:")
            print(f"  平均: {throughput:.1f} req/s")
            print(f"  测试时长: {total_time:.1f}s")
        
        # Error analysis
        if failed:
            print(f"\n错误分析:")
            error_types = {}
            for r in failed:
                error_key = r.error[:50] if r.error else r.status_code
                error_types[error_key] = error_types.get(error_key, 0) + 1
            
            for error, count in sorted(error_types.items(), key=lambda x: x[1], reverse=True)[:5]:
                print(f"  {error}: {count} 次")


def main():
    parser = argparse.ArgumentParser(description="Shannon 负载测试")
    parser.add_argument("--test-type",
                        choices=["constant", "ramp", "spike"],
                        default="constant",
                        help="测试类型")
    parser.add_argument("--users", type=int, default=10,
                        help="并发用户数")
    parser.add_argument("--duration", type=int, default=60,
                        help="测试持续时间（秒）")
    parser.add_argument("--ramp-up", type=int, default=10,
                        help="渐进测试的爬坡时间（秒）")
    parser.add_argument("--spike-users", type=int, default=100,
                        help="峰值测试的峰值用户数")
    parser.add_argument("--endpoint", default="localhost:50052",
                        help="gRPC 端点")
    parser.add_argument("--api-key", default="test-key",
                        help="API Key")
    parser.add_argument("--simulate", action="store_true",
                        help="模拟模式")
    parser.add_argument("--output", type=str,
                        help="JSON 输出文件")
    
    args = parser.parse_args()
    
    load_test = LoadTest(
        endpoint=args.endpoint,
        api_key=args.api_key,
        use_simulation=args.simulate
    )
    
    print("\n" + "=" * 60)
    print("Shannon 负载测试")
    print("=" * 60)
    print(f"开始时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"测试模式: {args.test_type}")
    print(f"模拟模式: {'是' if args.simulate else '否'}")
    
    try:
        if args.test_type == "constant":
            load_test.run_constant_load(args.users, args.duration)
        elif args.test_type == "ramp":
            load_test.run_ramp_up_load(args.users, args.ramp_up, args.duration)
        elif args.test_type == "spike":
            load_test.run_spike_test(args.users // 2, args.spike_users, args.duration)
        
        load_test.print_summary()
        
        if args.output:
            results_dict = {
                "test_type": args.test_type,
                "config": vars(args),
                "results": [asdict(r) for r in load_test.results],
                "timestamp": datetime.now().isoformat()
            }
            with open(args.output, 'w') as f:
                json.dump(results_dict, f, indent=2)
            print(f"\n✅ 详细结果已保存到 {args.output}")
    
    except KeyboardInterrupt:
        print("\n\n⚠️  测试被中断")
        load_test.print_summary()


if __name__ == "__main__":
    main()

