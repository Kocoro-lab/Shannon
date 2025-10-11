# 模板使用入门指南

本指南展示如何创建、加载和运行基于模板的工作流（System 1）。

## 1) 创建模板

在 `config/workflows/examples/`（或你自己的文件夹）下创建一个 YAML 文件。最简示例：

```yaml
name: simple_analysis
version: "1.0.0"
defaults:
  model_tier: medium
  budget_agent_max: 5000
  require_approval: false

nodes:
  - id: analyze
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    budget_max: 500
    depends_on: []
```

提示：
- `type`: `simple | cognitive | dag | supervisor`
- `strategy`: `react | chain_of_thought | reflection | debate | tree_of_thoughts`
- 设置每个节点的 `budget_max` 和 `tools_allowlist` 来约束执行

## 2) 加载模板

模板在编排器启动时通过 `InitTemplateRegistry` 加载，可以来自一个或多个目录。参见 `go/orchestrator/internal/workflows/template_catalog.go`。

## 3) 列出可用模板

使用新的 gRPC API：

```bash
grpcurl -plaintext -d '{}' localhost:50052 \
  shannon.orchestrator.OrchestratorService/ListTemplates
```

响应包含 `name`、`version`、`key` 和 `content_hash`。

## 4) 执行模板

通过名称/版本请求执行模板，可选禁用 AI：

```bash
grpcurl -plaintext -d '{
  "query": "总结本周的科技新闻",
  "context": {
    "template": "simple_analysis",
    "template_version": "1.0.0",
    "disable_ai": true
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

注意：
- 当 `disable_ai` 为 true 且模板缺失时，请求快速失败
- 当 `workflows.templates.fallback_to_ai`（或 `TEMPLATE_FALLBACK_ENABLED=1`）启用时，失败的模板运行可以回退到 AI 分解

## 5) 最佳实践

- 保持节点小而确定；优先选择更多节点而非大型单体
- 明确限制每个节点的工具
- 当需要人工签核时设置 `defaults.require_approval`
- 使用模板继承来重用常见配置
- 在 `defaults` 中设置合理的预算限制
- 定期版本化和测试你的模板

## 6) 高级功能

### 模板继承

```yaml
name: advanced_analysis
version: "1.0.0"
extends: simple_analysis  # 继承基础模板

defaults:
  budget_agent_max: 10000  # 覆盖父模板设置

nodes:
  - id: deep_analysis
    type: cognitive
    strategy: chain_of_thought
    depends_on: [analyze]  # 依赖于父模板的节点
```

### DAG 工作流

```yaml
name: parallel_research
version: "1.0.0"

nodes:
  - id: search_tech
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    depends_on: []

  - id: search_business
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    depends_on: []

  - id: synthesize
    type: cognitive
    strategy: chain_of_thought
    depends_on: [search_tech, search_business]  # 等待两个并行搜索完成
```

### Supervisor 模式

```yaml
name: team_workflow
version: "1.0.0"

nodes:
  - id: supervisor
    type: supervisor
    strategy: react
    members:
      - id: researcher
        role: "研究和收集信息"
        tools: ["web_search"]
      - id: analyst
        role: "分析和总结发现"
        tools: []
      - id: writer
        role: "撰写最终报告"
        tools: []
```

### 条件执行

```yaml
nodes:
  - id: check_quality
    type: simple
    strategy: reflection
    depends_on: [generate]
    conditions:
      - field: output_quality
        operator: gte
        value: 0.8
        on_fail: retry  # retry | skip | fail
```

## 7) 调试技巧

### 查看模板执行日志

```bash
# 查看编排器日志
docker-compose -f deploy/compose/docker-compose.yml logs orchestrator -f

# 搜索特定模板
docker-compose -f deploy/compose/docker-compose.yml logs orchestrator | grep "template=simple_analysis"
```

### 验证模板语法

```bash
# 使用验证脚本
./scripts/validate-templates.sh config/workflows/examples/

# 或手动验证单个文件
yq eval '.' config/workflows/examples/simple_analysis.yaml
```

### 测试模板

```bash
# 在测试模式下运行
grpcurl -plaintext -d '{
  "query": "测试查询",
  "context": {
    "template": "simple_analysis",
    "template_version": "1.0.0",
    "test_mode": true
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### 导出和重放工作流

```bash
# 导出工作流历史
make replay-export WORKFLOW_ID=task-dev-xxx OUT=history.json

# 重放以调试
make replay HISTORY=history.json
```

## 8) 性能优化

### 减少 Token 使用

1. **使用模板而非 AI 分解** - 节省高达 85-95% 的 token
2. **限制工具访问** - 减少不必要的工具描述
3. **设置严格预算** - 防止失控的 token 消耗
4. **使用缓存** - 重用相似查询的结果

```yaml
defaults:
  budget_agent_max: 3000      # 每个智能体最大 token
  budget_task_max: 10000      # 整个任务最大 token
  enable_caching: true        # 启用结果缓存
  cache_ttl_seconds: 3600     # 缓存 1 小时
```

### 优化并行执行

```yaml
# 最大化并行性
nodes:
  - id: task1
    depends_on: []  # 立即开始

  - id: task2
    depends_on: []  # 与 task1 并行

  - id: task3
    depends_on: []  # 与 task1, task2 并行

  - id: final
    depends_on: [task1, task2, task3]  # 等待所有完成
```

### 使用合适的模型层级

```yaml
nodes:
  - id: simple_task
    model_tier: fast    # 使用快速、便宜的模型

  - id: complex_task
    model_tier: smart   # 使用更强大的模型
```

## 9) 常见模式

### 研究 → 分析 → 报告

```yaml
name: research_pipeline
version: "1.0.0"

nodes:
  - id: research
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    depends_on: []

  - id: analyze
    type: cognitive
    strategy: chain_of_thought
    depends_on: [research]

  - id: report
    type: simple
    strategy: reflection
    depends_on: [analyze]
```

### 多角度辩论

```yaml
name: debate_decision
version: "1.0.0"

nodes:
  - id: debate
    type: cognitive
    strategy: debate
    debate_config:
      num_agents: 3
      rounds: 2
      roles:
        - "支持者"
        - "反对者"
        - "中立分析者"
    depends_on: []
```

### 树形搜索探索

```yaml
name: solution_exploration
version: "1.0.0"

nodes:
  - id: explore
    type: cognitive
    strategy: tree_of_thoughts
    tot_config:
      depth: 3
      branches_per_level: 3
      evaluation_strategy: "vote"
    depends_on: []
```

## 10) 故障排查

### 模板未找到

```bash
# 检查模板是否已加载
grpcurl -plaintext -d '{}' localhost:50052 \
  shannon.orchestrator.OrchestratorService/ListTemplates | grep "simple_analysis"

# 重启编排器以重新加载模板
docker-compose -f deploy/compose/docker-compose.yml restart orchestrator
```

### 预算超限

```yaml
# 增加预算限制
defaults:
  budget_agent_max: 10000
  budget_task_max: 50000

# 或在节点级别覆盖
nodes:
  - id: expensive_task
    budget_max: 15000
```

### 工具权限被拒

```yaml
# 确保工具在允许列表中
nodes:
  - id: task
    tools_allowlist: ["web_search", "python_execute", "file_read"]
```

### 依赖循环

```bash
# 验证脚本会检测循环
./scripts/validate-templates.sh config/workflows/examples/

# 修复：确保 depends_on 不形成循环
# 错误: A -> B -> C -> A
# 正确: A -> B -> C
```

## 11) 下一步

- 阅读[模板工作流完整指南](./template-workflows.md)
- 学习如何[添加自定义工具](./adding-custom-tools.md)
- 探索[多智能体工作流架构](./multi-agent-workflow-architecture.md)
- 查看[示例用例](./example-usecases/)

---

需要帮助？在 [GitHub Issues](https://github.com/Kocoro-lab/Shannon/issues) 提问或加入我们的 Discord！

