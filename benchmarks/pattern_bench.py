#!/usr/bin/env python3
"""
Shannon 模式性能基准测试
测试不同 AI 模式的性能表现
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
    # Import generated protobuf stubs
    # Note: These should be generated from protos/orchestrator/orchestrator.proto
    sys.path.insert(0, './clients/python/src')
    from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc, common_pb2
    GRPC_AVAILABLE = True
except ImportError as e:
    print(f"⚠️  Warning: gRPC imports failed: {e}")
    print("   Running in simulation mode. Install shannon client: pip install -e clients/python")
    GRPC_AVAILABLE = False


class PatternBenchmark:
    """模式性能基准测试"""
    
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
                print(f"⚠️  Failed to connect to gRPC: {e}")
                print("   Falling back to simulation mode")
                self.use_simulation = True
    
    def _get_metadata(self):
        """Get gRPC metadata for authentication"""
        return [('x-api-key', self.api_key)]
    
    def run_pattern_task(self, pattern: str, query: str, task_id: int, **kwargs) -> Dict[str, Any]:
        """运行指定模式的任务"""
        start = time.time()
        
        try:
            if self.use_simulation:
                # Simulation mode - estimate based on pattern complexity
                delays = {
                    'cot': 1.5,      # Chain-of-Thought
                    'react': 2.0,    # ReAct
                    'debate': 4.5,   # Debate (multiple agents)
                    'tot': 3.5,      # Tree-of-Thoughts
                    'reflection': 2.5 # Reflection
                }
                time.sleep(delays.get(pattern, 2.0))
                success = True
                output = f"Simulated {pattern} output"
                token_usage = {'input': 1000, 'output': 500}
            else:
                # Real gRPC call
                metadata = self._get_metadata()
                
                # Build context with pattern specification
                context = struct_pb2.Struct()
                context['cognitive_strategy'] = pattern
                context.update(kwargs)
                
                request = orchestrator_pb2.TaskRequest(
                    query=query,
                    user_id="benchmark-user",
                    mode=common_pb2.EXECUTION_MODE_STANDARD,
                    context=context
                )
                
                response = self.client.SubmitTask(request, metadata=metadata, timeout=120.0)
                success = response.status == "completed"
                output = response.output[:100] if hasattr(response, 'output') else ""
                
                # Extract token usage from response
                token_usage = {
                    'input': getattr(response, 'tokens_used_input', 0),
                    'output': getattr(response, 'tokens_used_output', 0)
                }
            
            duration = time.time() - start
            
            return {
                "task_id": task_id,
                "pattern": pattern,
                "duration": duration,
                "success": success,
                "output_preview": output,
                "token_usage": token_usage,
                "total_tokens": token_usage['input'] + token_usage['output']
            }
            
        except Exception as e:
            duration = time.time() - start
            return {
                "task_id": task_id,
                "pattern": pattern,
                "duration": duration,
                "success": False,
                "error": str(e),
                "token_usage": {"input": 0, "output": 0},
                "total_tokens": 0
            }
    
    def benchmark_chain_of_thought(self, num_requests=10):
        """Chain-of-Thought 模式基准测试"""
        print(f"\n📊 Chain-of-Thought 基准测试 ({num_requests} 请求)")
        print("-" * 60)
        
        query = "Calculate the result of (123 + 456) * 789 and explain your reasoning step by step."
        
        results = []
        for i in range(num_requests):
            result = self.run_pattern_task('cot', query, i)
            results.append(result)
            print(f"  完成 {i+1}/{num_requests} - {result['duration']:.2f}s")
        
        self.print_statistics("Chain-of-Thought", results)
        return results
    
    def benchmark_react(self, num_requests=10):
        """ReAct 模式基准测试"""
        print(f"\n📊 ReAct 基准测试 ({num_requests} 请求)")
        print("-" * 60)
        
        query = "Search for the current weather in San Francisco and analyze if it's good for outdoor activities."
        
        results = []
        for i in range(num_requests):
            result = self.run_pattern_task('react', query, i)
            results.append(result)
            print(f"  完成 {i+1}/{num_requests} - {result['duration']:.2f}s")
        
        self.print_statistics("ReAct", results)
        return results
    
    def benchmark_debate(self, num_requests=5, num_agents=3):
        """Debate 模式基准测试"""
        print(f"\n📊 Debate 模式基准测试 ({num_requests} 请求, {num_agents} agents)")
        print("-" * 60)
        
        query = "Should AI systems be regulated by government? Discuss pros and cons."
        
        results = []
        for i in range(num_requests):
            result = self.run_pattern_task(
                'debate', 
                query, 
                i,
                num_agents=num_agents,
                debate_rounds=2
            )
            results.append(result)
            print(f"  完成 {i+1}/{num_requests} - {result['duration']:.2f}s")
        
        self.print_statistics(f"Debate ({num_agents} agents)", results)
        return results
    
    def benchmark_tree_of_thoughts(self, num_requests=5):
        """Tree-of-Thoughts 模式基准测试"""
        print(f"\n📊 Tree-of-Thoughts 基准测试 ({num_requests} 请求)")
        print("-" * 60)
        
        query = "Design an algorithm to solve the traveling salesman problem for 10 cities."
        
        results = []
        for i in range(num_requests):
            result = self.run_pattern_task(
                'tot', 
                query, 
                i,
                exploration_depth=3,
                branches_per_level=3
            )
            results.append(result)
            print(f"  完成 {i+1}/{num_requests} - {result['duration']:.2f}s")
        
        self.print_statistics("Tree-of-Thoughts", results)
        return results
    
    def benchmark_reflection(self, num_requests=10):
        """Reflection 模式基准测试"""
        print(f"\n📊 Reflection 基准测试 ({num_requests} 请求)")
        print("-" * 60)
        
        query = "Write a Python function to check if a string is a palindrome. Review and improve it."
        
        results = []
        for i in range(num_requests):
            result = self.run_pattern_task(
                'reflection', 
                query, 
                i,
                reflection_rounds=2
            )
            results.append(result)
            print(f"  完成 {i+1}/{num_requests} - {result['duration']:.2f}s")
        
        self.print_statistics("Reflection", results)
        return results
    
    def print_statistics(self, name: str, results: List[Dict]):
        """打印统计信息"""
        if not results:
            print("⚠️  无结果")
            return
        
        successful = [r for r in results if r.get("success")]
        durations = [r["duration"] for r in successful]
        tokens = [r["total_tokens"] for r in successful]
        success_rate = len(successful) / len(results) * 100
        
        print(f"\n{name} 统计:")
        print(f"  总请求数: {len(results)}")
        print(f"  成功率: {success_rate:.1f}%")
        
        if not durations:
            print("  ❌ 无成功请求")
            return
        
        print(f"\n  延迟指标:")
        print(f"    平均耗时: {statistics.mean(durations):.3f}s")
        print(f"    中位数: {statistics.median(durations):.3f}s")
        print(f"    最小值: {min(durations):.3f}s")
        print(f"    最大值: {max(durations):.3f}s")
        
        if len(durations) > 1:
            print(f"    标准差: {statistics.stdev(durations):.3f}s")
        
        # 百分位数
        sorted_durations = sorted(durations)
        p50 = sorted_durations[len(sorted_durations) // 2]
        p95 = sorted_durations[min(int(len(sorted_durations) * 0.95), len(sorted_durations) - 1)]
        p99 = sorted_durations[min(int(len(sorted_durations) * 0.99), len(sorted_durations) - 1)]
        
        print(f"\n  百分位数:")
        print(f"    P50: {p50:.3f}s")
        print(f"    P95: {p95:.3f}s")
        print(f"    P99: {p99:.3f}s")
        
        # Token 使用情况
        if tokens:
            print(f"\n  Token 使用:")
            print(f"    平均: {statistics.mean(tokens):.0f} tokens")
            print(f"    总计: {sum(tokens)} tokens")
            
            # 估算成本 (假设 $0.01 per 1K tokens)
            total_cost = sum(tokens) * 0.00001
            print(f"    估算成本: ${total_cost:.4f}")
    
    def run_comparison(self, requests_per_pattern=5):
        """对比所有模式的性能"""
        print("\n" + "=" * 60)
        print("模式性能对比测试")
        print("=" * 60)
        
        all_results = {}
        
        # Run all pattern benchmarks
        patterns = [
            ('cot', self.benchmark_chain_of_thought),
            ('react', self.benchmark_react),
            ('debate', lambda n: self.benchmark_debate(n, 3)),
            ('tot', self.benchmark_tree_of_thoughts),
            ('reflection', self.benchmark_reflection)
        ]
        
        for pattern_name, benchmark_func in patterns:
            try:
                results = benchmark_func(requests_per_pattern)
                all_results[pattern_name] = results
            except Exception as e:
                print(f"❌ {pattern_name} 测试失败: {e}")
        
        # Print comparison summary
        print("\n" + "=" * 60)
        print("性能对比总结")
        print("=" * 60)
        print(f"\n{'模式':<20} {'平均延迟':<12} {'P95':<12} {'平均Tokens':<15} {'成功率'}")
        print("-" * 60)
        
        for pattern_name, results in all_results.items():
            successful = [r for r in results if r.get("success")]
            if not successful:
                continue
            
            durations = [r["duration"] for r in successful]
            tokens = [r["total_tokens"] for r in successful]
            success_rate = len(successful) / len(results) * 100
            
            sorted_durations = sorted(durations)
            p95 = sorted_durations[min(int(len(sorted_durations) * 0.95), len(sorted_durations) - 1)]
            
            print(f"{pattern_name:<20} {statistics.mean(durations):>10.2f}s  "
                  f"{p95:>10.2f}s  {statistics.mean(tokens):>13.0f}  "
                  f"{success_rate:>6.1f}%")
        
        return all_results


def main():
    parser = argparse.ArgumentParser(description="Shannon 模式性能基准测试")
    parser.add_argument("--pattern", 
                        choices=["cot", "react", "debate", "tot", "reflection", "all"], 
                        default="all", 
                        help="测试模式")
    parser.add_argument("--requests", type=int, default=5, 
                        help="每个模式的请求数")
    parser.add_argument("--agents", type=int, default=3, 
                        help="Debate 模式的 agent 数量")
    parser.add_argument("--endpoint", default="localhost:50052", 
                        help="gRPC 端点")
    parser.add_argument("--api-key", default="test-key", 
                        help="API Key")
    parser.add_argument("--simulate", action="store_true",
                        help="使用模拟模式（不连接真实服务）")
    parser.add_argument("--output", type=str,
                        help="JSON 输出文件路径")
    
    args = parser.parse_args()
    
    bench = PatternBenchmark(
        endpoint=args.endpoint, 
        api_key=args.api_key,
        use_simulation=args.simulate
    )
    
    all_results = {}
    
    if args.pattern == "all":
        all_results = bench.run_comparison(args.requests)
    elif args.pattern == "cot":
        all_results['cot'] = bench.benchmark_chain_of_thought(args.requests)
    elif args.pattern == "react":
        all_results['react'] = bench.benchmark_react(args.requests)
    elif args.pattern == "debate":
        all_results['debate'] = bench.benchmark_debate(args.requests, args.agents)
    elif args.pattern == "tot":
        all_results['tot'] = bench.benchmark_tree_of_thoughts(args.requests)
    elif args.pattern == "reflection":
        all_results['reflection'] = bench.benchmark_reflection(args.requests)
    
    # Save to JSON if output specified
    if args.output:
        with open(args.output, 'w') as f:
            json.dump(all_results, f, indent=2)
        print(f"\n✅ 结果已保存到 {args.output}")


if __name__ == "__main__":
    main()

