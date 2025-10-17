# 数据分析管道示例

这个示例展示如何使用 Shannon 构建一个自动化的数据分析管道。

## 场景

分析电商网站的用户行为数据，生成洞察报告并提供优化建议。

## 工作流程

```
数据收集 → 数据清洗 → 统计分析 → 可视化 → 生成报告 → 提出建议
```

## 使用的 Shannon 功能

- **DAG Workflow**: 处理多步骤数据管道
- **Python Executor**: 数据处理和统计计算
- **Reflection Pattern**: 质量检查和改进
- **多模型路由**: 复杂分析使用 GPT-4o，总结使用 Claude

## 示例代码

### 1. 提交分析任务

```bash
./scripts/submit_task.sh "分析用户购物数据：
1. 计算月度销售趋势
2. 识别高价值客户群
3. 分析产品关联性
4. 预测下季度销量
5. 提供营销优化建议

数据格式：CSV，包含 user_id, product_id, purchase_date, amount"
```

### 2. 使用 Python 执行器处理数据

Shannon 会自动生成类似以下的 Python 代码：

```python
import csv
from datetime import datetime
from collections import defaultdict
import statistics

# 模拟数据读取
data = [
    {"user_id": "u1", "product_id": "p1", "purchase_date": "2024-01-15", "amount": 150},
    {"user_id": "u1", "product_id": "p2", "purchase_date": "2024-02-20", "amount": 200},
    # ... 更多数据
]

# 月度销售趋势
monthly_sales = defaultdict(float)
for record in data:
    month = record["purchase_date"][:7]  # YYYY-MM
    monthly_sales[month] += record["amount"]

print("月度销售趋势:")
for month, total in sorted(monthly_sales.items()):
    print(f"{month}: ${total:,.2f}")

# 高价值客户
user_spending = defaultdict(float)
for record in data:
    user_spending[record["user_id"]] += record["amount"]

high_value_threshold = statistics.mean(user_spending.values()) * 2
high_value_customers = {
    user: amount 
    for user, amount in user_spending.items() 
    if amount > high_value_threshold
}

print(f"\n高价值客户数量: {len(high_value_customers)}")
print(f"平均消费: ${statistics.mean(user_spending.values()):,.2f}")

# 产品关联分析
product_pairs = defaultdict(int)
user_products = defaultdict(set)
for record in data:
    user_products[record["user_id"]].add(record["product_id"])

for user, products in user_products.items():
    products_list = sorted(products)
    for i, p1 in enumerate(products_list):
        for p2 in products_list[i+1:]:
            product_pairs[(p1, p2)] += 1

print("\n最常见的产品组合:")
top_pairs = sorted(product_pairs.items(), key=lambda x: x[1], reverse=True)[:5]
for (p1, p2), count in top_pairs:
    print(f"{p1} + {p2}: {count} 次")
```

### 3. 生成分析报告

Shannon 使用 Chain-of-Thought 模式生成详细分析：

```
分析结果：

## 月度销售趋势
- 2024-01: $45,230
- 2024-02: $52,100 (↑15%)
- 2024-03: $48,950 (↓6%)

趋势：2月销售高峰，可能与春节促销相关

## 高价值客户群
- 数量：125 人（占总用户 8%）
- 贡献：总销售额的 42%
- 平均消费：$850/月

## 产品关联性
最强关联：
1. 笔记本电脑 + 鼠标（85%）
2. 手机 + 保护壳（78%）
3. 相机 + 存储卡（72%）

## 预测与建议
下季度预测销售：$165,000
建议：
1. 针对高价值客户推出会员计划
2. 产品捆绑销售策略
3. 3月销售下滑需关注季节性因素
```

## API 调用示例

使用 gRPC 提交复杂分析任务：

```python
import grpc
from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc

channel = grpc.insecure_channel('localhost:50052')
client = orchestrator_pb2_grpc.OrchestratorServiceStub(channel)

# 提交数据分析任务
task = orchestrator_pb2.TaskRequest(
    query="""
    分析电商数据并生成报告：
    1. 用户留存率分析
    2. 流失预警模型
    3. 个性化推荐策略
    4. ROI 优化建议
    """,
    mode="dag",  # 使用 DAG 工作流
    tools=["python_executor", "web_search"],  # 可以搜索最佳实践
    session_id="analytics_session_001",
    metadata={
        "data_source": "s3://bucket/user-behavior.csv",
        "time_range": "2024-Q1",
        "priority": "high"
    }
)

response = client.SubmitTask(task)
print(f"任务已提交: {response.workflow_id}")

# 流式获取分析进度
stream_request = orchestrator_pb2.StreamRequest(
    workflow_id=response.workflow_id,
    types=["AGENT_STARTED", "AGENT_COMPLETED"]
)

for update in client.StreamTaskExecution(stream_request):
    print(f"[{update.type}] {update.agent_id}: {update.message}")
```

## 配置优化

### 使用 Scientific Workflow

对于需要假设验证的分析：

```yaml
# config/shannon.yaml
workflows:
  scientific:
    max_hypotheses: 5
    max_iterations: 3
    confidence_threshold: 0.85

patterns:
  chain_of_thought:
    max_steps: 10
  debate:
    num_agents: 3
    max_rounds: 2
```

### 模型选择策略

```yaml
models:
  routing:
    # 数据处理：快速模型
    - pattern: "计算|统计|数据清洗"
      model: "gpt-4o-mini"
    
    # 深度分析：强大模型
    - pattern: "预测|建议|策略"
      model: "claude-3-5-sonnet-20241022"
    
    # 报告生成：平衡模型
    - pattern: "报告|总结"
      model: "gpt-4o"
```

## 输出示例

完整的分析报告将包括：

```markdown
# 用户行为分析报告

## 执行摘要
- 分析时间：2024-01-01 至 2024-03-31
- 数据量：50,000 条记录
- 用户数：1,580
- 总销售额：$146,280

## 关键发现

### 1. 用户留存率
- 30 天留存：45%
- 60 天留存：28%
- 90 天留存：18%

### 2. 流失预警
高风险用户特征：
- 最近 30 天无活动
- 单次购买用户
- 客单价 < $50

预测流失数：235 人（15%）

### 3. 个性化推荐策略
基于关联规则：
- 购买 A → 推荐 B（置信度 78%）
- 购买 C → 推荐 D（置信度 65%）

### 4. ROI 优化建议
投资回报最高的渠道：
1. 社交媒体广告：320% ROI
2. 邮件营销：180% ROI
3. 搜索引擎：145% ROI

## 行动计划
1. 立即：实施流失预警系统
2. 本周：优化产品推荐算法
3. 本月：调整营销预算分配
4. 下季度：开发会员忠诚计划
```

## 性能优化技巧

### 1. 使用会话持久化

```bash
# 第一步：加载数据
./scripts/submit_task.sh "加载并预处理用户数据" --session-id "analysis-001"

# 第二步：在同一会话中分析（数据已在内存）
./scripts/submit_task.sh "基于已加载数据进行趋势分析" --session-id "analysis-001"
```

### 2. 并行处理

```python
# Shannon 自动使用 Parallel Execution Pattern
query = """
同时执行以下分析：
1. 销售趋势分析
2. 用户细分
3. 产品性能评估
4. 竞争对手对比
"""
```

### 3. 增量分析

```python
# 只分析新数据
metadata = {
    "incremental": True,
    "last_analysis_date": "2024-03-01",
    "checkpoint_id": "checkpoint_20240301"
}
```

## 故障排除

### 数据量过大

```yaml
# 配置更大的超时和内存
wasi:
  timeout_seconds: 300  # 5 分钟
  memory_limit_mb: 1024  # 1GB
```

### 复杂计算超时

```python
# 分解为子任务
queries = [
    "步骤 1：数据清洗和预处理",
    "步骤 2：描述性统计分析",
    "步骤 3：预测模型构建",
    "步骤 4：生成可视化报告"
]

for i, query in enumerate(queries):
    submit_task(query, session_id=f"pipeline_{i}")
```

## 扩展阅读

- [Python 代码执行文档](../../docs/zh-CN/Python代码执行.md)
- [DAG 工作流指南](../../docs/zh-CN/多代理工作流架构.md)
- [模式使用指南](../../docs/zh-CN/模式使用指南.md)

---

## 前置要求

### 环境要求
- Shannon v0.2.0+
- Docker Compose环境
- 配置好的API密钥

### 数据要求
- CSV格式数据文件
- 包含必要字段（user_id, product_id, purchase_date, amount）
- 数据量建议：<10,000行（单次分析）

### 配置
```bash
# 设置API密钥
export SHANNON_API_KEY="your-api-key-here"

# 准备数据文件
ls data/user_shopping.csv
```

---

## 成本估算

### 单次数据分析

| 项目 | 数值 | 说明 |
|------|------|------|
| **Token使用** | 约15,000 tokens | 包含数据 + 分析 + 报告 |
| **输入Tokens** | ~8,000 | 数据摘要 + 分析要求 + prompt |
| **输出Tokens** | ~7,000 | Python代码 + 分析结果 + 建议 |
| **模型** | GPT-4o (分析) + Claude (总结) | 组合使用 |
| **预计成本** | $0.40-0.60 | 基于当前定价 |
| **时间** | 1-3分钟 | 取决于数据复杂度 |

### 成本优化建议

1. **数据采样**: 大数据集先分析样本（10%）
2. **分步骤**: 先探索性分析，再深入分析
3. **缓存结果**: 相似数据集复用统计信息
4. **模型选择**: 标准分析用Claude Haiku

---

## 常见问题

### Q1: 数据量太大怎么办？

**限制**: 单次处理建议<10,000行

**解决方案**:
```bash
# 方案A: 数据采样
head -1000 data/large_file.csv > data/sample.csv

# 方案B: 分块处理
split -l 5000 data/large_file.csv data/chunk_

# 方案C: 先做聚合
# 在数据库中预先聚合，再分析汇总结果
```

### Q2: Python代码执行失败？

**常见原因**:
- 缺少依赖库（如pandas）
- 数据格式错误
- 内存不足

**解决**:
```bash
# 1. 简化代码（只用标准库）
# 2. 验证数据格式
head -5 data/user_shopping.csv

# 3. 检查日志
docker logs shannon-agent-core-1 --tail 100
```

### Q3: 分析结果不准确？

**原因**: 数据理解不足

**改进**:
```bash
# 提供数据字典
./scripts/submit_task.sh "分析用户购物数据：
...

数据字典：
- user_id: 用户唯一标识符（字符串）
- amount: 购买金额（美元）
- purchase_date: 购买日期（YYYY-MM-DD格式）"
```

### Q4: 生成的图表不美观？

**当前限制**: Shannon主要生成文本分析

**解决方案**:
- 导出数据，使用专业工具（Tableau, PowerBI）
- 或要求生成matplotlib代码，本地执行

### Q5: 如何处理时间序列数据？

**提示**:
```bash
"分析销售数据的时间趋势：
1. 计算每月同比增长率
2. 识别季节性模式
3. 预测未来3个月趋势
请使用移动平均和指数平滑"
```

---

## 故障排查

### 问题: 任务一直处于pending状态

**检查**:
```bash
# 查看任务状态
./scripts/check_task_status.sh task-id

# 查看orchestrator日志
docker logs shannon-orchestrator-1 -f
```

**可能原因**:
- 工作流队列积压
- 依赖服务（Temporal）不可用

### 问题: 数据分析结果为空

**检查**:
- 数据文件是否正确加载
- 字段名是否匹配
- 是否有数据过滤逻辑错误

### 问题: 分析耗时过长（>5分钟）

**优化**:
1. 减少数据量
2. 简化分析逻辑
3. 拆分为多个小任务

---

## 扩展阅读

- [DAG 工作流文档](../../docs/zh-CN/工作流架构.md)
- [Python 代码执行指南](../../docs/zh-CN/Python代码执行.md)
- [多模型路由策略](../../docs/zh-CN/模型路由.md)

---

*示例更新：2025年10月*

