# 为 Shannon 基准测试做贡献

欢迎为 Shannon 性能基准测试框架做贡献！

## 📋 贡献指南

### 添加新的基准测试

1. **创建测试脚本**
   ```bash
   # 在 benchmarks/ 目录下创建新的 Python 脚本
   touch benchmarks/my_new_bench.py
   chmod +x benchmarks/my_new_bench.py
   ```

2. **遵循现有格式**
   - 使用 argparse 处理命令行参数
   - 提供 `--simulate` 模式用于无服务测试
   - 支持 `--output` 参数保存 JSON 结果
   - 实现清晰的进度显示和统计输出

3. **测试脚本示例结构**
   ```python
   #!/usr/bin/env python3
   """
   Shannon <测试类型> 基准测试
   """
   
   import argparse
   import json
   import time
   from typing import List, Dict
   
   class MyBenchmark:
       def __init__(self, endpoint, api_key, use_simulation):
           self.endpoint = endpoint
           self.api_key = api_key
           self.use_simulation = use_simulation
       
       def run_test(self):
           # 实现测试逻辑
           pass
       
       def print_statistics(self, results):
           # 打印统计信息
           pass
   
   def main():
       parser = argparse.ArgumentParser(description="...")
       parser.add_argument("--endpoint", default="localhost:50052")
       parser.add_argument("--simulate", action="store_true")
       parser.add_argument("--output", type=str)
       args = parser.parse_args()
       
       bench = MyBenchmark(args.endpoint, "test-key", args.simulate)
       results = bench.run_test()
       
       if args.output:
           with open(args.output, 'w') as f:
               json.dump(results, f, indent=2)
   
   if __name__ == "__main__":
       main()
   ```

4. **更新运行器**
   编辑 `benchmarks/run_benchmarks.sh` 添加新的测试类别：
   ```bash
   bench_my_new_test() {
       echo ""
       echo "=== X. My New Test ==="
       echo ""
       python3 benchmarks/my_new_bench.py --requests 10
   }
   ```

5. **添加 Makefile 目标**
   编辑 `Makefile` 添加便捷命令：
   ```makefile
   bench-mynew:
       @echo "[Benchmark] Running my new benchmark..."
       @python3 benchmarks/my_new_bench.py --output benchmarks/results/mynew.json || true
   ```

### 改进可视化

1. **添加新图表类型**
   编辑 `benchmarks/visualize.py` 添加新的绘图函数：
   ```python
   def plot_my_chart(self, results: List[Dict]):
       # 使用 matplotlib 或 plotly 创建图表
       pass
   ```

2. **改进报告生成**
   编辑 `benchmarks/generate_report.sh` 添加新的报告部分

### 提交 PR

1. **运行测试**
   ```bash
   # 在模拟模式下测试
   make bench-simulate
   
   # 生成报告
   make bench-report
   ```

2. **提交变更**
   ```bash
   git add benchmarks/
   git commit -m "feat(benchmark): add <description>"
   git push origin feat/your-benchmark-name
   ```

3. **创建 PR**
   - 描述添加的基准测试类型
   - 包含示例输出或图表
   - 说明如何运行新测试

## 🎯 性能目标

贡献新测试时，请考虑以下性能目标：

### 简单任务
- P50 延迟: < 500ms
- P95 延迟: < 2s
- P99 延迟: < 5s
- 吞吐量: > 100 req/s

### 复杂任务 (DAG)
- P50 延迟: < 5s
- P95 延迟: < 30s
- 吞吐量: > 10 req/s

### Python WASI
- 冷启动: < 500ms
- 热启动: < 50ms
- 执行开销: < 20% vs 本地

### 向量搜索
- 查询延迟: < 100ms
- 索引速度: > 1000 vectors/s

## 📝 测试最佳实践

1. **使用合适的样本量**
   - 快速测试: 10-20 请求
   - 标准测试: 50-100 请求
   - 负载测试: 100-1000 请求

2. **提供进度反馈**
   ```python
   for i in range(num_requests):
       result = run_test()
       if (i + 1) % 10 == 0:
           print(f"  完成 {i+1}/{num_requests}")
   ```

3. **错误处理**
   ```python
   try:
       result = run_test()
   except Exception as e:
       print(f"  ❌ 测试失败: {e}")
       result = {"success": False, "error": str(e)}
   ```

4. **统计输出**
   - 总请求数和成功率
   - 平均、中位数、最小、最大延迟
   - P50, P95, P99 百分位数
   - 吞吐量 (req/s)

## 🔍 代码审查要点

PR 审查时我们会检查：
- [ ] 测试逻辑正确且有意义
- [ ] 支持模拟模式（用于 CI）
- [ ] 错误处理完善
- [ ] 输出格式一致
- [ ] 文档清晰
- [ ] 代码风格符合 PEP 8

## 💡 测试想法

欢迎贡献以下类型的基准测试：

- **新模式测试**: 测试新的 AI 模式（如 Hybrid, Supervisor）
- **规模测试**: 测试不同规模的输入（文档大小、上下文长度）
- **端到端场景**: 模拟真实用户场景
- **资源消耗**: 内存、CPU、网络使用
- **错误恢复**: 测试故障恢复和重试机制
- **并发测试**: 测试不同并发级别
- **长时间运行**: 稳定性和内存泄漏测试

## 📚 参考资源

- [Shannon 文档](../docs/)
- [Python gRPC 指南](https://grpc.io/docs/languages/python/)
- [Matplotlib 文档](https://matplotlib.org/)
- [Plotly 文档](https://plotly.com/python/)

## 🙏 致谢

感谢所有为 Shannon 基准测试框架做出贡献的开发者！

---

有问题？在 [GitHub Issues](https://github.com/Kocoro-lab/Shannon/issues) 提问或加入我们的 Discord 社区。

