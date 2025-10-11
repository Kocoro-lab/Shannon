# Shannon 测试指南

Shannon 平台的全面测试指南，从单元测试到端到端场景。

## 测试类型

### 单元测试
- **Go**: 与代码位于同一位置的 `_test.go` 文件
- **Rust**: crate 内的内联 `#[cfg(test)]` 模块
- **Python**: 位于 `python/llm-service/tests/`

### 集成测试
- **Temporal 工作流**: 使用 Temporal 测试套件进行内存执行，带有模拟活动
- **冒烟测试**: 基本的跨服务连接、持久化、指标验证
- **端到端 (E2E)**: `tests/` 下的多服务场景，使用 Docker Compose

## 快速开始

```bash
# 1. 运行所有单元测试
make test

# 2. 启动完整堆栈
make dev

# 3. 运行冒烟测试（健康检查、gRPC、持久化、指标）
make smoke

# 4. 运行 E2E 场景
tests/e2e/run.sh
```

## 详细测试命令

### 按语言进行单元测试

```bash
# Go 测试 + 竞态检测
cd go/orchestrator && go test -race ./...

# Rust 测试 + 输出
cd rust/agent-core && cargo test -- --nocapture

# Python 测试 + 覆盖率
cd python/llm-service && python3 -m pytest --cov

# WASI 沙箱测试
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm
cd rust/agent-core && cargo run --example wasi_hello -- /tmp/hello-wasi.wasm
```

### Temporal 工作流测试

```bash
# 导出工作流历史
make replay-export WORKFLOW_ID=task-dev-XXX OUT=history.json

# 测试确定性
make replay HISTORY=history.json

# 运行所有重放测试
make ci-replay
```

### 冒烟测试详情

冒烟测试 (`make smoke`) 验证：
- Temporal UI 可达性 (http://localhost:8088)
- Agent-Core gRPC 健康检查和 ExecuteTask
- Orchestrator SubmitTask + GetTaskStatus 进度
- LLM 服务 health/live/ready 端点
- Qdrant 就绪和必需的集合
- PostgreSQL 连接和迁移
- Prometheus 指标端点 (:2112, :2113)

### 手动服务验证

```bash
# Agent-Core 健康检查
grpcurl -plaintext -d '{"service":"agent-core"}' localhost:50051 grpc.health.v1.Health/Check

# Orchestrator SubmitTask
grpcurl -plaintext -d '{
  "query": "Calculate 2+2",
  "user_id": "test-user"
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask

# LLM 服务健康
curl http://localhost:50053/health

# Qdrant 集合
curl http://localhost:6333/collections

# PostgreSQL 连接
docker-compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon -c '\dt'

# Prometheus 指标
curl http://localhost:2112/metrics | grep shannon
curl http://localhost:2113/metrics | grep agent
```

## 测试覆盖率

### Go 覆盖率

```bash
# 生成覆盖率报告
make coverage-go

# 查看 HTML 报告
cd go/orchestrator && go tool cover -html=coverage.out
```

当前 Go 覆盖率目标：**50%+**

### Python 覆盖率

```bash
# 生成覆盖率报告
make coverage-python

# 查看详细报告
cd python/llm-service && . .venv/bin/activate && coverage report -m
```

当前 Python 覆盖率基线：**20%**（目标：**70%**）

### 综合覆盖率

```bash
# 运行所有覆盖率测试并应用门限
make coverage-gate
```

## 端到端测试

### 运行所有 E2E 测试

```bash
cd tests/e2e
./run.sh
```

### 运行特定场景

```bash
# 单智能体流程
./tests/e2e/single_agent_test.sh

# 多智能体协作
./tests/e2e/multi_agent_test.sh

# 会话记忆
./tests/e2e/session_memory_test.sh

# 工作流重放
./tests/e2e/replay_test.sh

# Python 代码执行
./tests/e2e/python_exec_test.sh

# Web 搜索集成
./tests/e2e/web_search_test.sh
```

## 集成测试

### 运行所有集成测试

```bash
make integration-tests
```

### 单独的集成测试

```bash
# 单智能体流程测试
make integration-single

# 会话记忆测试
make integration-session

# Qdrant 向量数据库测试
make integration-qdrant
```

## 性能测试

### 运行基准测试

```bash
# 运行所有基准测试
make bench

# 仅工作流基准测试
make bench-workflow

# 仅模式基准测试
make bench-pattern

# 仅工具基准测试
make bench-tool

# 负载测试
make bench-load
```

详见 [benchmarks/README.md](../../benchmarks/README.md)

### 快速性能检查

```bash
# 模拟模式下的快速基准测试（无需服务）
make bench-simulate

# 快速基准测试（较少请求）
make bench-quick
```

## 调试技巧

### 查看日志

```bash
# 所有服务
make logs

# 特定服务
docker-compose -f deploy/compose/docker-compose.yml logs orchestrator -f
docker-compose -f deploy/compose/docker-compose.yml logs agent-core -f
docker-compose -f deploy/compose/docker-compose.yml logs llm-service -f

# 搜索错误
docker-compose -f deploy/compose/docker-compose.yml logs | grep ERROR
```

### 交互式调试

```bash
# 进入容器
docker-compose -f deploy/compose/docker-compose.yml exec orchestrator sh
docker-compose -f deploy/compose/docker-compose.yml exec agent-core sh
docker-compose -f deploy/compose/docker-compose.yml exec llm-service bash

# 检查进程
docker-compose -f deploy/compose/docker-compose.yml exec orchestrator ps aux

# 检查网络
docker-compose -f deploy/compose/docker-compose.yml exec orchestrator netstat -tlnp
```

### 数据库检查

```bash
# PostgreSQL
docker-compose -f deploy/compose/docker-compose.yml exec postgres \
  psql -U shannon -d shannon

# 常用查询
SELECT * FROM sessions LIMIT 10;
SELECT * FROM tasks ORDER BY created_at DESC LIMIT 10;
SELECT * FROM agent_executions WHERE status = 'failed';

# Qdrant 集合
curl http://localhost:6333/collections | jq

# Redis 键
docker-compose -f deploy/compose/docker-compose.yml exec redis redis-cli KEYS "*"
```

### Temporal 工作流调试

```bash
# 列出工作流
docker-compose -f deploy/compose/docker-compose.yml exec temporal \
  temporal workflow list

# 查看特定工作流
docker-compose -f deploy/compose/docker-compose.yml exec temporal \
  temporal workflow show --workflow-id task-dev-xxx

# 导出历史用于重放
make replay-export WORKFLOW_ID=task-dev-xxx OUT=debug_history.json
```

## 测试数据

### 创建测试 API 密钥

```bash
# 为开发/测试创建测试 API 密钥
make seed-api-key

# 使用 sk_test_123456 进行测试
```

### 填充测试数据

```bash
# 填充 PostgreSQL 数据
./scripts/seed_postgres.sh

# 初始化 Qdrant 集合
./scripts/bootstrap_qdrant.sh

# 运行完整的填充
make seed
```

## 持续集成

### GitHub Actions 工作流

项目包含多个 CI 工作流：

- **ci.yml** - 主要 CI 管道（构建、测试、linting）
- **benchmark.yml** - 自动化性能基准测试
- **claude-code-review.yml** - AI 辅助代码审查

### 本地运行 CI 检查

```bash
# 模拟 CI 环境
make ci

# 包含覆盖率门限
make ci-with-coverage

# 重放测试（确保确定性）
make ci-replay
```

## 测试最佳实践

### 编写好的测试

1. **测试行为，而非实现**
   ```go
   // 好
   func TestSubmitTask_ValidInput_ReturnsTaskID(t *testing.T) { ... }
   
   // 差
   func TestSubmitTask_CallsDatabase(t *testing.T) { ... }
   ```

2. **使用描述性测试名称**
   - Go: `TestFunctionName_Scenario_ExpectedResult`
   - Python: `test_function_name_scenario_expected_result`
   - Rust: `test_function_name_scenario_expected_result`

3. **隔离测试**
   - 不依赖测试执行顺序
   - 清理测试数据
   - 使用测试专用数据库/集合

4. **使用表驱动测试（Go）**
   ```go
   tests := []struct {
       name     string
       input    string
       expected string
   }{
       {"empty input", "", ""},
       {"valid input", "hello", "HELLO"},
   }
   
   for _, tt := range tests {
       t.Run(tt.name, func(t *testing.T) {
           result := ToUpper(tt.input)
           assert.Equal(t, tt.expected, result)
       })
   }
   ```

5. **模拟外部依赖**
   - 使用接口进行依赖注入
   - 在单元测试中模拟 LLM 调用
   - 在集成测试中使用真实服务

### 测试环境变量

```bash
# 为测试设置环境
export SHANNON_ENV=test
export LOG_LEVEL=debug
export SKIP_LLM_CALLS=true
export TEST_API_KEY=sk_test_123456

# 或创建 .env.test
cp .env.example .env.test
# 编辑 .env.test 用于测试特定配置
```

### 测试数据管理

```bash
# 在测试之间清理
docker-compose -f deploy/compose/docker-compose.yml down -v
docker-compose -f deploy/compose/docker-compose.yml up -d

# 或使用测试专用配置
docker-compose -f deploy/compose/docker-compose.test.yml up -d
```

## 故障排查

### 测试失败

```bash
# 增加日志详细程度
export LOG_LEVEL=debug

# 运行单个测试
cd go/orchestrator && go test -v -run TestSpecificTest ./...

# 显示所有输出
cd rust/agent-core && cargo test -- --nocapture --test-threads=1

# Python 详细模式
cd python/llm-service && pytest -v -s tests/test_specific.py
```

### 端口冲突

```bash
# 检查使用的端口
netstat -tlnp | grep -E '5005[0-9]|6333|5432|6379|8088'

# 杀死进程
kill -9 $(lsof -t -i:50052)

# 或更改 docker-compose.yml 中的端口
```

### Docker 问题

```bash
# 清理一切
make clean

# 重建镜像
docker-compose -f deploy/compose/docker-compose.yml build --no-cache

# 检查磁盘空间
docker system df

# 清理未使用的资源
docker system prune -a
```

## 下一步

- 阅读[贡献指南](../../CONTRIBUTING.md)
- 探索[示例测试](../../tests/)
- 查看 [CI 工作流](../../.github/workflows/)
- 学习[调试技术](./debugging.md) _(即将推出)_

---

需要帮助？在 [GitHub Issues](https://github.com/Kocoro-lab/Shannon/issues) 提问或加入我们的 Discord！

