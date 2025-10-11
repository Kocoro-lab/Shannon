#!/usr/bin/env python3
"""
Shannon 工作流性能基准测试
"""

import time
import argparse
import statistics
from concurrent.futures import ThreadPoolExecutor, as_completed
import sys
import json

try:
    import grpc
    from google.protobuf import struct_pb2
    sys.path.insert(0, './clients/python/src')
    from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc, common_pb2
    GRPC_AVAILABLE = True
except ImportError as e:
    print(f"⚠️  Warning: gRPC imports failed: {e}")
    print("   Running in simulation mode. Install: pip install -e clients/python")
    GRPC_AVAILABLE = False

class WorkflowBenchmark:
    """工作流性能测试"""
    
    def __init__(self, endpoint="localhost:50052", api_key="test-key", use_simulation=False):
        self.endpoint = endpoint
        self.api_key = api_key
        self.use_simulation = use_simulation or not GRPC_AVAILABLE
        
        if not self.use_simulation:
            try:
                self.channel = grpc.insecure_channel(endpoint)
                self.client = orchestrator_pb2_grpc.OrchestratorServiceStub(self.channel)
                print(f"✅ Connected to orchestrator at {endpoint}")
            except Exception as e:
                print(f"⚠️  Failed to connect: {e}. Using simulation mode.")
                self.use_simulation = True
    
    def _get_metadata(self):
        """Get gRPC metadata for authentication"""
        return [('x-api-key', self.api_key)]
        
    def run_simple_task(self, task_id):
        """运行简单任务"""
        start = time.time()
        
        try:
            if self.use_simulation:
                time.sleep(0.5)  # 模拟延迟
                success = True
            else:
                # 真实 gRPC 调用
                request = orchestrator_pb2.TaskRequest(
                    query="Calculate the factorial of 20",
                    user_id="benchmark-user",
                    mode=common_pb2.EXECUTION_MODE_STANDARD
                )
                
                response = self.client.SubmitTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=30.0
                )
                success = response.status == "completed"
            
            duration = time.time() - start
            return {
                "task_id": task_id,
                "duration": duration,
                "success": success
            }
        except Exception as e:
            duration = time.time() - start
            return {
                "task_id": task_id,
                "duration": duration,
                "success": False,
                "error": str(e)
            }
    
    def run_dag_workflow(self, num_subtasks, task_id):
        """运行 DAG 工作流"""
        start = time.time()
        
        query = f"""
        Complete the following {num_subtasks} tasks:
        {chr(10).join([f'{i+1}. Calculate {i+1} * {i+1}' for i in range(num_subtasks)])}
        """
        
        try:
            if self.use_simulation:
                time.sleep(num_subtasks * 0.3)  # 模拟延迟
                success = True
            else:
                # 真实 gRPC 调用
                context = struct_pb2.Struct()
                context['workflow_type'] = 'dag'
                context['num_subtasks'] = num_subtasks
                
                request = orchestrator_pb2.TaskRequest(
                    query=query,
                    user_id="benchmark-user",
                    mode=common_pb2.EXECUTION_MODE_STANDARD,
                    context=context
                )
                
                response = self.client.SubmitTask(
                    request,
                    metadata=self._get_metadata(),
                    timeout=60.0
                )
                success = response.status == "completed"
            
            duration = time.time() - start
            return {
                "task_id": task_id,
                "num_subtasks": num_subtasks,
                "duration": duration,
                "success": success
            }
        except Exception as e:
            duration = time.time() - start
            return {
                "task_id": task_id,
                "num_subtasks": num_subtasks,
                "duration": duration,
                "success": False,
                "error": str(e)
            }
    
    def benchmark_simple_tasks(self, num_requests=100, concurrency=10):
        """基准测试简单任务"""
        print(f"\n📊 简单任务基准测试 ({num_requests} 请求, {concurrency} 并发)")
        print("-" * 60)
        
        results = []
        with ThreadPoolExecutor(max_workers=concurrency) as executor:
            futures = [
                executor.submit(self.run_simple_task, i) 
                for i in range(num_requests)
            ]
            
            for future in as_completed(futures):
                try:
                    result = future.result()
                    results.append(result)
                except Exception as e:
                    print(f"❌ 任务失败: {e}")
        
        self.print_statistics("简单任务", results)
        return results
    
    def benchmark_dag_workflows(self, num_requests=20, num_subtasks=5):
        """基准测试 DAG 工作流"""
        print(f"\n📊 DAG 工作流基准测试 ({num_requests} 请求, {num_subtasks} 子任务)")
        print("-" * 60)
        
        results = []
        for i in range(num_requests):
            try:
                result = self.run_dag_workflow(num_subtasks, i)
                results.append(result)
                print(f"  完成 {i+1}/{num_requests}")
            except Exception as e:
                print(f"❌ 工作流失败: {e}")
        
        self.print_statistics("DAG 工作流", results)
        return results
    
    def print_statistics(self, name, results):
        """打印统计信息"""
        if not results:
            print("⚠️  无结果")
            return
        
        durations = [r["duration"] for r in results if r.get("success")]
        success_rate = len(durations) / len(results) * 100
        
        print(f"\n{name} 统计:")
        print(f"  总请求数: {len(results)}")
        print(f"  成功率: {success_rate:.1f}%")
        print(f"  平均耗时: {statistics.mean(durations):.3f}s")
        print(f"  中位数: {statistics.median(durations):.3f}s")
        print(f"  最小值: {min(durations):.3f}s")
        print(f"  最大值: {max(durations):.3f}s")
        
        if len(durations) > 1:
            print(f"  标准差: {statistics.stdev(durations):.3f}s")
        
        # 百分位数
        sorted_durations = sorted(durations)
        p50 = sorted_durations[len(sorted_durations) // 2]
        p95 = sorted_durations[int(len(sorted_durations) * 0.95)]
        p99 = sorted_durations[int(len(sorted_durations) * 0.99)]
        
        print(f"\n  P50: {p50:.3f}s")
        print(f"  P95: {p95:.3f}s")
        print(f"  P99: {p99:.3f}s")
        
        # 吞吐量
        total_time = max(durations)
        throughput = len(results) / total_time
        print(f"\n  吞吐量: {throughput:.1f} req/s")

def main():
    parser = argparse.ArgumentParser(description="Shannon 工作流基准测试")
    parser.add_argument("--test", choices=["simple", "dag", "parallel"], 
                        default="simple", help="测试类型")
    parser.add_argument("--requests", type=int, default=100, 
                        help="请求数量")
    parser.add_argument("--subtasks", type=int, default=5, 
                        help="DAG 子任务数")
    parser.add_argument("--concurrency", type=int, default=10, 
                        help="并发数")
    parser.add_argument("--endpoint", default="localhost:50052", 
                        help="gRPC 端点")
    parser.add_argument("--api-key", default="test-key", 
                        help="API Key")
    parser.add_argument("--simulate", action="store_true",
                        help="使用模拟模式")
    parser.add_argument("--output", type=str,
                        help="JSON 输出文件路径")
    
    args = parser.parse_args()
    
    bench = WorkflowBenchmark(
        endpoint=args.endpoint,
        api_key=args.api_key,
        use_simulation=args.simulate
    )
    
    results = None
    if args.test == "simple":
        results = bench.benchmark_simple_tasks(args.requests, args.concurrency)
    elif args.test == "dag":
        results = bench.benchmark_dag_workflows(args.requests, args.subtasks)
    elif args.test == "parallel":
        print("并行测试尚未实现")
    
    if args.output and results:
        with open(args.output, 'w') as f:
            json.dump(results, f, indent=2)
        print(f"\n✅ 结果已保存到 {args.output}")

if __name__ == "__main__":
    main()


