# Shannon 性能基准测试

本目录包含 Shannon 的性能基准测试工具和结果。

## 测试类别

### 1. 工作流性能测试
- 简单任务吞吐量
- DAG 工作流执行时间
- 并行代理效率
- 内存使用情况

### 2. 模式性能测试
- Chain-of-Thought 延迟
- Debate 模式开销
- Tree-of-Thoughts 探索效率
- Reflection 质量vs性能权衡

### 3. 工具执行性能
- Python WASI 启动时间
- Python 代码执行速度
- Web 搜索响应时间
- 文件系统操作延迟

### 4. 数据层性能
- PostgreSQL 查询性能
- Redis 缓存命中率
- Qdrant 向量搜索速度
- Temporal 工作流吞吐量

## 快速开始

```bash
# 运行所有基准测试
make bench

# 运行特定类别
./benchmarks/run_benchmarks.sh workflow
./benchmarks/run_benchmarks.sh patterns
./benchmarks/run_benchmarks.sh tools

# 生成报告
./benchmarks/generate_report.sh
```

## 基准测试工具

### 1. `run_benchmarks.sh`
主测试运行器，协调所有测试

### 2. `workflow_bench.py`
工作流级别的性能测试

### 3. `pattern_bench.py`
模式性能比较测试

### 4. `tool_bench.py`
工具执行性能测试

### 5. `load_test.py`
负载测试和压力测试

## 性能目标

### 简单任务
- **P50 延迟**: < 500ms
- **P95 延迟**: < 2s
- **P99 延迟**: < 5s
- **吞吐量**: > 100 req/s

### 复杂任务 (DAG)
- **P50 延迟**: < 5s
- **P95 延迟**: < 30s
- **P99 延迟**: < 60s
- **吞吐量**: > 10 req/s

### Python 执行
- **冷启动**: < 500ms
- **热启动**: < 50ms
- **执行开销**: < 20% vs 本地

### 向量搜索
- **查询延迟**: < 100ms
- **索引速度**: > 1000 vectors/s

## 结果示例

```
=== Shannon 性能基准测试报告 ===
日期: 2025-01-10
版本: v0.2.0
环境: Docker Compose (4 CPU, 8GB RAM)

1. 简单任务性能
   - P50: 420ms
   - P95: 1.8s
   - P99: 3.2s
   - 吞吐量: 125 req/s
   - 状态: ✅ 达标

2. DAG 工作流 (5 个子任务)
   - 平均执行时间: 8.5s
   - 并行加速比: 3.2x
   - 内存使用: 450MB
   - 状态: ✅ 达标

3. Python WASI 执行
   - 冷启动: 480ms
   - 热启动: 45ms
   - 内存开销: 55MB
   - 状态: ✅ 达标

4. 向量搜索 (Qdrant)
   - 查询延迟: 85ms
   - 精确率@10: 0.95
   - 状态: ✅ 达标
```

## 持续监控

基准测试集成到 CI/CD 中，每次提交都会运行基准测试并与基线比较。

```yaml
# .github/workflows/benchmark.yml
name: Performance Benchmarks
on: [push, pull_request]
jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Run benchmarks
        run: make bench
      - name: Compare with baseline
        run: ./benchmarks/compare_baseline.sh
```

## 贡献

添加新的基准测试：

1. 在 `benchmarks/` 目录创建测试脚本
2. 遵循现有测试的格式
3. 更新 `run_benchmarks.sh` 包含新测试
4. 提交 PR 并附上测试结果

---

*最后更新：2025年1月*


