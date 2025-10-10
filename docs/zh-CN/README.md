# Shannon 中文文档

欢迎来到 Shannon 的中文文档！本目录包含 Shannon 企业级 AI 代理编排器的完整中文文档。

## 📚 文档目录

### 🚀 入门指南

- [快速开始](快速开始.md) - 快速上手 Shannon
- [常见问题解答](常见问题.md) - 常见问题和解决方案

### 🎯 核心指南

- [模式使用指南](模式使用指南.md) - 了解和使用各种 AI 推理模式
- [添加自定义工具](添加自定义工具.md) - 扩展 Shannon 的工具生态系统
- [多代理工作流架构](多代理工作流架构.md) - 深入了解多代理编排系统
- [Python 代码执行](Python代码执行.md) - WASI 沙箱中的安全 Python 执行
- [流式 API](流式API.md) - 实时事件流接口（gRPC、SSE、WebSocket）
- [身份验证和多租户](身份验证和多租户.md) - 企业级安全和租户隔离

### 📖 其他资源

- [贡献指南](https://github.com/Kocoro-lab/Shannon/blob/main/CONTRIBUTING.md) (英文)
- [主 README](https://github.com/Kocoro-lab/Shannon/blob/main/README.md) (英文)
- [完整英文文档](https://github.com/Kocoro-lab/Shannon/tree/main/docs)

## 🌟 关于 Shannon

Shannon 是一个企业级 AI 代理编排器，提供复杂的多代理工作流、智能模型路由和安全的代码执行环境。

### 主要特性

#### 🤖 多模型支持
- 集成 OpenAI、Anthropic、Google、DeepSeek 等主流 LLM
- 智能模型路由和故障转移
- 统一的 API 接口

#### 🔄 高级推理模式
- **Chain-of-Thought (CoT)**: 逐步推理
- **Debate**: 多代理辩论
- **Tree-of-Thoughts (ToT)**: 系统化探索
- **ReAct**: 推理-行动-观察循环
- **Reflection**: 自我评估和改进

#### 🛡️ 安全执行
- 基于 WASI 的 Python 代码沙箱
- 完全内存隔离
- 资源限制和超时保护
- 无网络访问

#### 🌐 企业级功能
- JWT 身份验证和 API 密钥
- 完整的多租户数据隔离
- 审计日志和合规性
- 实时事件流
- 人工审批工作流

#### 📊 可观测性
- Temporal 工作流可视化
- 实时流式事件
- 全面的日志记录
- 性能指标跟踪

### 架构组件

```
┌─────────────────────────────────────────────────────────┐
│                        用户界面                          │
│            (Dashboard / CLI / API)                      │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                   编排器 (Go)                            │
│  • 任务路由 • 工作流管理 • 身份验证                       │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
┌───────▼──────┐ ┌──▼──────┐ ┌──▼─────────┐
│ LLM 服务     │ │Agent Core│ │  网关      │
│  (Python)    │ │  (Rust)  │ │   (Go)     │
│              │ │          │ │            │
│ • 模型调用   │ │ • WASI   │ │ • HTTP API │
│ • 工具执行   │ │ • 沙箱   │ │ • 流式     │
└──────────────┘ └──────────┘ └────────────┘
        │            │            │
┌───────▼────────────▼────────────▼───────────┐
│              数据层                          │
│  PostgreSQL │ Redis │ Qdrant │ Temporal    │
└─────────────────────────────────────────────┘
```

### 支持的模型

| 提供商 | 模型 | 用途 |
|--------|------|------|
| **OpenAI** | GPT-4o, GPT-4o-mini, o1 | 通用、复杂推理 |
| **Anthropic** | Claude 3.5 Sonnet, Haiku | 长上下文、分析 |
| **Google** | Gemini 1.5 Pro/Flash | 多模态、快速 |
| **DeepSeek** | DeepSeek-V3, R1 | 推理、代码 |
| **自定义** | 本地部署 | 私有环境 |

### 快速链接

- 🏠 [GitHub 仓库](https://github.com/Kocoro-lab/Shannon)
- 📘 [英文文档](https://github.com/Kocoro-lab/Shannon/blob/main/README.md)
- 🐛 [问题反馈](https://github.com/Kocoro-lab/Shannon/issues)
- 💬 [讨论区](https://github.com/Kocoro-lab/Shannon/discussions)

### 开始使用

```bash
# 克隆仓库
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon

# 配置环境
cp .env.example .env
# 编辑 .env 添加你的 API 密钥

# 启动服务
make dev

# 提交任务
./scripts/submit_task.sh "分析 2024 年 AI 发展趋势"
```

### 社区贡献

我们欢迎所有形式的贡献：

- 📝 **文档**：改进文档、添加示例、翻译
- 🐛 **Bug 修复**：报告和修复问题
- ✨ **新功能**：提出和实现新功能
- 🧪 **测试**：添加测试用例、改进覆盖率
- 💡 **想法**：分享你的想法和建议

查看 [贡献指南](https://github.com/Kocoro-lab/Shannon/blob/main/CONTRIBUTING.md) 了解详情。

---

*最后更新：2025 年 1 月*  
*文档维护者：Shannon 中文社区*

