# Shannon 中文文档

欢迎使用 Shannon 中文文档！

Shannon 是一个生产级 AI 智能体编排平台，提供企业级安全、成本控制和供应商灵活性。

## 📚 文档导航

### 快速开始
- [模板使用指南](./template-user-guide.md) - 创建和运行模板工作流
- [环境配置指南](./environment-configuration.md) - 配置环境变量和 API 密钥
- [测试指南](./testing.md) - 单元测试、集成测试和端到端测试

### 核心概念
- [添加自定义工具](./adding-custom-tools.md) - 扩展 Shannon 的工具能力
- [Python 代码执行](./python-code-execution.md) - WASI 沙箱安全执行
- [模式使用指南](./pattern-usage-guide.md) - ReAct, CoT, Debate 等 AI 模式
- [多智能体工作流架构](./multi-agent-workflow-architecture.md) - DAG、并行、顺序模式

### 高级主题
- [内存系统架构](./memory-system-architecture.md) - 会话内存和向量存储
- [上下文窗口管理](./context-window-management.md) - Token 管理和优化
- [学习路由器增强](./learning-router-enhancements.md) - 智能策略选择
- [流式 API](./streaming-api.md) - WebSocket 和 SSE 实时通信

### 开发指南
- [扩展 Shannon](./extending-shannon.md) - 添加新功能和集成
- [API 参考](./agent-core-api.md) - Agent Core 服务接口
- [测试热重载配置](./testing-hot-reload-config.md) - 开发时配置热重载

### 运维指南
- [身份验证与多租户](./authentication-and-multitenancy.md) - 安全和隔离
- [集中定价管理](./centralized-pricing.md) - 模型定价配置
- [速率感知预算](./rate-aware-budgeting.md) - 防止 API 限流

## 🌟 特性亮点

### 🚀 快速发货
- **零 Token 模板** - YAML 工作流消除常见模式的 LLM 调用
- **学习路由器** - UCB 算法选择最优策略，节省高达 85-95% 的 token
- **速率感知执行** - 供应商特定的 RPM/TPM 限制防止限流
- 自动多智能体编排 - 描述目标，Shannon 自动分解任务

### 🔒 生产就绪
- **WASI 沙箱** - CPython 3.11 在 WASI 沙箱中执行（标准库，无网络，只读文件系统）
- **Token 预算控制** - 硬性的每智能体/每任务预算，实时使用跟踪
- **策略引擎 (OPA)** - 细粒度的工具、模型和数据规则
- **多租户** - 租户作用域的认证、会话、内存和工作流

### 📈 可扩展
- **成本优化** - 缓存、会话持久化、上下文优化和预算感知路由
- **供应商支持** - OpenAI, Anthropic, Google (Gemini), Groq, DeepSeek, Qwen, Ollama
- **默认可观测** - 实时仪表板、Prometheus 指标、OpenTelemetry 追踪
- **分布式设计** - Temporal 支持的工作流，水平扩展

## 🎯 与其他框架对比

| 挑战 | Shannon | LangGraph | AutoGen | CrewAI |
|------|---------|-----------|---------|---------|
| **多智能体编排** | ✅ DAG/图工作流 | ✅ 状态图 | ✅ 群聊 | ✅ Crew/角色 |
| **智能体通信** | ✅ 消息传递 | ✅ 工具调用 | ✅ 对话 | ✅ 委派 |
| **内存与上下文** | ✅ 分块存储 (基于字符), MMR 多样性, 模式学习 | ✅ 多种类型 | ✅ 对话历史 | ✅ 共享内存 |
| **调试生产问题** | ✅ 重放任何工作流 | ❌ 有限调试 | ❌ 基本日志 | ❌ |
| **Token 成本控制** | ✅ 硬预算限制 | ❌ | ❌ | ❌ |
| **安全沙箱** | ✅ WASI 隔离 | ❌ | ❌ | ❌ |
| **策略控制 (OPA)** | ✅ 细粒度规则 | ❌ | ❌ | ❌ |
| **确定性重放** | ✅ 时间旅行调试 | ❌ | ❌ | ❌ |

## 💬 社区与支持

- **Discord**: 加入我们的 Discord
- **Twitter/X**: @shannon_agents
- **GitHub Issues**: [报告问题](https://github.com/Kocoro-lab/Shannon/issues)
- **贡献指南**: [CONTRIBUTING.md](../../CONTRIBUTING.md)

## 📖 其他资源

- [主 README (英文)](../../README.md)
- [API 文档](https://docs.shannon.ai) _(即将推出)_
- [示例用例](./example-usecases/)

---

**不再调试 AI 故障。开始发布可靠的智能体。**

如果 Shannon 为你节省了时间或金钱，请告诉我们！我们喜欢成功故事。

