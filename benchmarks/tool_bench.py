#!/usr/bin/env python3
"""
Shannon 工具执行性能基准测试
测试各种工具的执行性能和开销
"""

import time
import argparse
import statistics
import json
import sys
from typing import List, Dict, Any
from concurrent.futures import ThreadPoolExecutor, as_completed

try:
    import grpc
    from google.protobuf import struct_pb2
    sys.path.insert(0, './clients/python/src')
    from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc, common_pb2
    GRPC_AVAILABLE = True
except ImportError as e:
    print(f"⚠️  Warning: gRPC imports failed: {e}")
    print("   Running in simulation mode.")
    GRPC_AVAILABLE = False


class ToolBenchmark:
    """工具性能基准测试"""
    
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
        return [('x-api-key', self.api_key)]
    
    def benchmark_python_wasi(self, cold_start=5, hot_start=20):
        """Python WASI 执行性能测试"""
        print(f"\n📊 Python WASI 性能测试")
        print(f"   冷启动: {cold_start} 次, 热启动: {hot_start} 次")
        print("-" * 60)
        
        python_code = """
def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n-1) + fibonacci(n-2)

result = fibonacci(15)
print(f"Fibonacci(15) = {result}")
"""
        
        cold_results = []
        hot_results = []
        
        # Cold start tests - 每次都是新的环境
        print("\n冷启动测试...")
        for i in range(cold_start):
            start = time.time()
            
            if self.use_simulation:
                time.sleep(0.48)  # 模拟 480ms 冷启动
                success = True
            else:
                try:
                    query = f"Execute this Python code: {python_code}"
                    context = struct_pb2.Struct()
                    context['tool'] = 'python_wasi'
                    context['cold_start'] = True
                    
                    request = orchestrator_pb2.TaskRequest(
                        query=query,
                        user_id="benchmark-user",
                        mode=common_pb2.EXECUTION_MODE_STANDARD,
                        context=context
                    )
                    
                    response = self.client.SubmitTask(request, metadata=self._get_metadata(), timeout=30.0)
                    success = response.status == "completed"
                except Exception as e:
                    print(f"  ❌ 错误: {e}")
                    success = False
            
            duration = time.time() - start
            cold_results.append({"duration": duration, "success": success, "type": "cold"})
            print(f"  冷启动 {i+1}/{cold_start}: {duration*1000:.0f}ms")
        
        # Hot start tests - 复用环境
        print("\n热启动测试...")
        for i in range(hot_start):
            start = time.time()
            
            if self.use_simulation:
                time.sleep(0.045)  # 模拟 45ms 热启动
                success = True
            else:
                try:
                    query = f"Execute this Python code: {python_code}"
                    context = struct_pb2.Struct()
                    context['tool'] = 'python_wasi'
                    context['cold_start'] = False
                    
                    request = orchestrator_pb2.TaskRequest(
                        query=query,
                        user_id="benchmark-user",
                        mode=common_pb2.EXECUTION_MODE_STANDARD,
                        context=context
                    )
                    
                    response = self.client.SubmitTask(request, metadata=self._get_metadata(), timeout=30.0)
                    success = response.status == "completed"
                except Exception as e:
                    success = False
            
            duration = time.time() - start
            hot_results.append({"duration": duration, "success": success, "type": "hot"})
            if (i + 1) % 5 == 0:
                print(f"  热启动 {i+1}/{hot_start}: {duration*1000:.0f}ms")
        
        # Print statistics
        self._print_startup_stats("Python WASI", cold_results, hot_results)
        
        return {"cold": cold_results, "hot": hot_results}
    
    def benchmark_web_search(self, num_requests=10):
        """Web 搜索性能测试"""
        print(f"\n📊 Web Search 性能测试 ({num_requests} 请求)")
        print("-" * 60)
        
        queries = [
            "What is the weather in San Francisco?",
            "Latest AI news 2025",
            "Python programming best practices",
            "Machine learning tutorials",
            "Docker container optimization"
        ]
        
        results = []
        for i in range(num_requests):
            query = queries[i % len(queries)]
            start = time.time()
            
            if self.use_simulation:
                time.sleep(0.8 + (i % 3) * 0.1)  # 模拟 800-1000ms
                success = True
                response_size = 2500
            else:
                try:
                    context = struct_pb2.Struct()
                    context['tool'] = 'web_search'
                    
                    request = orchestrator_pb2.TaskRequest(
                        query=f"Search the web: {query}",
                        user_id="benchmark-user",
                        mode=common_pb2.EXECUTION_MODE_STANDARD,
                        context=context
                    )
                    
                    response = self.client.SubmitTask(request, metadata=self._get_metadata(), timeout=30.0)
                    success = response.status == "completed"
                    response_size = len(response.output) if hasattr(response, 'output') else 0
                except Exception as e:
                    success = False
                    response_size = 0
            
            duration = time.time() - start
            results.append({
                "duration": duration,
                "success": success,
                "response_size": response_size,
                "query": query
            })
            print(f"  请求 {i+1}/{num_requests}: {duration*1000:.0f}ms")
        
        self._print_tool_stats("Web Search", results)
        return results
    
    def benchmark_file_operations(self, num_requests=20):
        """文件系统操作性能测试"""
        print(f"\n📊 文件操作性能测试 ({num_requests} 请求)")
        print("-" * 60)
        
        operations = ['read', 'write', 'list', 'stat']
        results = []
        
        for i in range(num_requests):
            op = operations[i % len(operations)]
            start = time.time()
            
            if self.use_simulation:
                delays = {'read': 0.015, 'write': 0.025, 'list': 0.020, 'stat': 0.010}
                time.sleep(delays[op])
                success = True
            else:
                # 文件操作通过工具执行
                success = True  # Placeholder
            
            duration = time.time() - start
            results.append({
                "duration": duration,
                "success": success,
                "operation": op
            })
        
        # Group by operation type
        for op in operations:
            op_results = [r for r in results if r['operation'] == op]
            if op_results:
                durations = [r['duration'] for r in op_results]
                print(f"\n  {op.upper()} 操作:")
                print(f"    平均: {statistics.mean(durations)*1000:.2f}ms")
                print(f"    P95: {sorted(durations)[int(len(durations)*0.95)]*1000:.2f}ms")
        
        return results
    
    def benchmark_mcp_tools(self, num_requests=10):
        """MCP 工具性能测试"""
        print(f"\n📊 MCP 工具性能测试 ({num_requests} 请求)")
        print("-" * 60)
        
        # Test various MCP tool calls
        tools = ['list_resources', 'read_resource', 'call_tool']
        results = []
        
        for i in range(num_requests):
            tool = tools[i % len(tools)]
            start = time.time()
            
            if self.use_simulation:
                delays = {'list_resources': 0.050, 'read_resource': 0.080, 'call_tool': 0.120}
                time.sleep(delays[tool])
                success = True
            else:
                success = True  # Placeholder for real MCP integration
            
            duration = time.time() - start
            results.append({
                "duration": duration,
                "success": success,
                "tool": tool
            })
            print(f"  {tool} {i+1}: {duration*1000:.0f}ms")
        
        self._print_tool_stats("MCP Tools", results)
        return results
    
    def _print_startup_stats(self, name: str, cold_results: List[Dict], hot_results: List[Dict]):
        """打印启动性能统计"""
        print(f"\n{name} 启动性能:")
        
        cold_durations = [r['duration'] for r in cold_results if r['success']]
        hot_durations = [r['duration'] for r in hot_results if r['success']]
        
        if cold_durations:
            print(f"\n  冷启动:")
            print(f"    平均: {statistics.mean(cold_durations)*1000:.0f}ms")
            print(f"    中位数: {statistics.median(cold_durations)*1000:.0f}ms")
            print(f"    P95: {sorted(cold_durations)[int(len(cold_durations)*0.95)]*1000:.0f}ms")
            print(f"    最小: {min(cold_durations)*1000:.0f}ms")
            print(f"    最大: {max(cold_durations)*1000:.0f}ms")
        
        if hot_durations:
            print(f"\n  热启动:")
            print(f"    平均: {statistics.mean(hot_durations)*1000:.0f}ms")
            print(f"    中位数: {statistics.median(hot_durations)*1000:.0f}ms")
            print(f"    P95: {sorted(hot_durations)[int(len(hot_durations)*0.95)]*1000:.0f}ms")
            print(f"    最小: {min(hot_durations)*1000:.0f}ms")
            print(f"    最大: {max(hot_durations)*1000:.0f}ms")
        
        if cold_durations and hot_durations:
            speedup = statistics.mean(cold_durations) / statistics.mean(hot_durations)
            print(f"\n  热启动加速比: {speedup:.1f}x")
    
    def _print_tool_stats(self, name: str, results: List[Dict]):
        """打印工具性能统计"""
        successful = [r for r in results if r['success']]
        if not successful:
            print("  ⚠️  无成功请求")
            return
        
        durations = [r['duration'] for r in successful]
        success_rate = len(successful) / len(results) * 100
        
        print(f"\n{name} 性能统计:")
        print(f"  成功率: {success_rate:.1f}%")
        print(f"  平均延迟: {statistics.mean(durations)*1000:.0f}ms")
        print(f"  中位数: {statistics.median(durations)*1000:.0f}ms")
        print(f"  P95: {sorted(durations)[int(len(durations)*0.95)]*1000:.0f}ms")
        print(f"  P99: {sorted(durations)[int(len(durations)*0.99)]*1000:.0f}ms")
        
        # Throughput
        total_time = sum(durations)
        throughput = len(results) / total_time if total_time > 0 else 0
        print(f"  吞吐量: {throughput:.1f} req/s")


def main():
    parser = argparse.ArgumentParser(description="Shannon 工具性能基准测试")
    parser.add_argument("--tool", 
                        choices=["python", "web_search", "file_ops", "mcp", "all"], 
                        default="all",
                        help="测试工具类型")
    parser.add_argument("--cold-start", type=int, default=5,
                        help="冷启动测试次数")
    parser.add_argument("--hot-start", type=int, default=20,
                        help="热启动测试次数")
    parser.add_argument("--requests", type=int, default=10,
                        help="请求数量")
    parser.add_argument("--endpoint", default="localhost:50052",
                        help="gRPC 端点")
    parser.add_argument("--api-key", default="test-key",
                        help="API Key")
    parser.add_argument("--simulate", action="store_true",
                        help="模拟模式")
    parser.add_argument("--output", type=str,
                        help="JSON 输出文件")
    
    args = parser.parse_args()
    
    bench = ToolBenchmark(
        endpoint=args.endpoint,
        api_key=args.api_key,
        use_simulation=args.simulate
    )
    
    all_results = {}
    
    if args.tool == "all":
        print("\n" + "=" * 60)
        print("工具性能综合测试")
        print("=" * 60)
        
        all_results['python_wasi'] = bench.benchmark_python_wasi(args.cold_start, args.hot_start)
        all_results['web_search'] = bench.benchmark_web_search(args.requests)
        all_results['file_ops'] = bench.benchmark_file_operations(args.requests)
        all_results['mcp'] = bench.benchmark_mcp_tools(args.requests)
    elif args.tool == "python":
        all_results['python_wasi'] = bench.benchmark_python_wasi(args.cold_start, args.hot_start)
    elif args.tool == "web_search":
        all_results['web_search'] = bench.benchmark_web_search(args.requests)
    elif args.tool == "file_ops":
        all_results['file_ops'] = bench.benchmark_file_operations(args.requests)
    elif args.tool == "mcp":
        all_results['mcp'] = bench.benchmark_mcp_tools(args.requests)
    
    if args.output:
        with open(args.output, 'w') as f:
            json.dump(all_results, f, indent=2)
        print(f"\n✅ 结果已保存到 {args.output}")


if __name__ == "__main__":
    main()

