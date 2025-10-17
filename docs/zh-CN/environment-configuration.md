# 环境配置指南

本指南解释如何为 Shannon 的 Docker Compose 部署正确配置环境变量。

## 目录
- [概述](#概述)
- [环境变量加载](#环境变量加载)
- [必需配置](#必需配置)
- [常见问题和解决方案](#常见问题和解决方案)
- [最佳实践](#最佳实践)

## 概述

Shannon 使用环境变量来配置敏感信息，如 API 密钥、服务端点和功能标志。正确的配置对平台正常运行至关重要。

## 环境变量加载

### Docker Compose 如何加载环境变量

Docker Compose 按以下优先级顺序加载环境变量：

1. **Shell 环境变量**（最高优先级）
2. **docker-compose 目录中的 `.env` 文件**
3. **docker-compose.yml 中的 `env_file` 指令**
4. **docker-compose.yml 中的 `environment` 部分**（最低优先级）

### 关键设置步骤

#### 1. 创建根 `.env` 文件

通过复制示例文件在项目根目录创建 `.env`：

```bash
cp .env.example .env
# 编辑 .env 填入你的实际 API 密钥
```

#### 2. 为 Docker Compose 创建符号链接

Docker Compose 在与 `docker-compose.yml` 文件相同的目录中查找 `.env`：

```bash
cd deploy/compose
ln -sf ../../.env .env
```

这个符号链接确保 Docker Compose 能找到你的环境变量。

#### 3. 验证配置

测试你的环境变量是否正确加载：

```bash
# 从项目根目录
docker compose -f deploy/compose/docker-compose.yml config | grep EXA_API_KEY

# 检查运行中容器内部
docker compose -f deploy/compose/docker-compose.yml exec llm-service env | grep EXA
```

## 必需配置

### 必需的 API 密钥

```bash
# LLM 提供商（至少设置一个）
OPENAI_API_KEY=sk-...
# ANTHROPIC_API_KEY=...
# GOOGLE_API_KEY=...
# XAI_API_KEY=...
# DEEPSEEK_API_KEY=...

# Web 搜索（推荐）
EXA_API_KEY=...
BRAVE_API_KEY=...
# TAVILY_API_KEY=...

# 可选集成
# FIRECRAWL_API_KEY=...
# JINA_API_KEY=...
```

### 服务配置

```bash
# 数据库
POSTGRES_USER=shannon
POSTGRES_PASSWORD=shannon_dev
POSTGRES_DB=shannon
POSTGRES_HOST=postgres
POSTGRES_PORT=5432

# Redis
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=  # 开发环境留空

# Temporal
TEMPORAL_HOST=temporal:7233
TEMPORAL_NAMESPACE=default

# Qdrant 向量存储
QDRANT_HOST=qdrant
QDRANT_PORT=6333
QDRANT_API_KEY=  # 开发环境留空
```

### 服务端点

```bash
# 内部服务通信
ORCHESTRATOR_GRPC=orchestrator:50052
AGENT_CORE_ADDR=agent-core:50051
LLM_SERVICE_ADDR=llm-service:50053

# 外部网关（可选）
GATEWAY_HTTP_PORT=8081
GATEWAY_SKIP_AUTH=1  # 开发环境设为 1
```

### 功能标志

```bash
# 模板系统
TEMPLATE_FALLBACK_ENABLED=1  # 启用 AI 回退
TEMPLATES_DIR=config/workflows/examples

# 预算和速率限制
BUDGET_ENFORCEMENT=true
RATE_LIMITING_ENABLED=true

# 可观测性
ENABLE_METRICS=true
ENABLE_TRACING=true
LOG_LEVEL=info  # debug | info | warn | error

# 安全
OPA_ENABLED=true
OPA_POLICIES_DIR=config/opa/policies
```

## 常见问题和解决方案

### 问题 1: 容器内未设置环境变量

**症状：**
```bash
docker compose exec llm-service env | grep OPENAI_API_KEY
# 无输出
```

**解决方案：**

1. 确认符号链接存在：
   ```bash
   ls -la deploy/compose/.env
   # 应显示: .env -> ../../.env
   ```

2. 如果符号链接不存在，创建它：
   ```bash
   cd deploy/compose
   ln -sf ../../.env .env
   ```

3. 重启服务：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml down
   docker compose -f deploy/compose/docker-compose.yml up -d
   ```

### 问题 2: API 密钥未生效

**症状：**
LLM 服务报告"未配置 API 密钥"

**解决方案：**

1. 验证 `.env` 中的密钥格式：
   ```bash
   cat .env | grep OPENAI_API_KEY
   # 正确格式: OPENAI_API_KEY=sk-proj-xxxxx
   # 错误格式: OPENAI_API_KEY="sk-proj-xxxxx"  # 不要使用引号！
   ```

2. 测试配置：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml config | grep OPENAI_API_KEY
   ```

3. 如果仍然失败，直接在 `docker-compose.yml` 中设置（临时调试）：
   ```yaml
   services:
     llm-service:
       environment:
         OPENAI_API_KEY: ${OPENAI_API_KEY}
   ```

### 问题 3: 服务无法连接到 PostgreSQL/Redis

**症状：**
```
connection refused: postgres:5432
```

**解决方案：**

1. 验证服务正在运行：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml ps
   ```

2. 检查网络连接：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml exec orchestrator nc -zv postgres 5432
   docker compose -f deploy/compose/docker-compose.yml exec orchestrator nc -zv redis 6379
   ```

3. 检查环境变量：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml exec orchestrator env | grep POSTGRES
   ```

4. 等待服务完全启动：
   ```bash
   docker compose -f deploy/compose/docker-compose.yml logs postgres | grep "ready to accept"
   docker compose -f deploy/compose/docker-compose.yml logs redis | grep "Ready to accept"
   ```

### 问题 4: Windows 符号链接问题

**症状：**
在 Windows 上，符号链接可能不工作

**解决方案：**

选项 1 - 复制文件（简单）：
```bash
copy .env deploy\compose\.env
```

选项 2 - 使用管理员权限创建符号链接：
```powershell
# 以管理员身份运行 PowerShell
cd deploy\compose
New-Item -ItemType SymbolicLink -Path .env -Target ..\..\env
```

选项 3 - 使用 WSL（推荐用于开发）：
```bash
wsl
cd /mnt/c/path/to/Shannon
ln -sf ../../.env deploy/compose/.env
```

## 最佳实践

### 1. 使用环境特定的配置

```bash
# 开发
cp .env.example .env.development

# 生产
cp .env.example .env.production

# 根据环境符号链接
ln -sf .env.development .env
```

### 2. 永远不要提交 `.env` 文件

确保 `.gitignore` 包含：

```gitignore
.env
.env.*
!.env.example
deploy/compose/.env
```

### 3. 使用强密码

```bash
# 生成安全的随机密码
openssl rand -base64 32

# 或使用 Python
python3 -c "import secrets; print(secrets.token_urlsafe(32))"
```

### 4. 分离秘密信息

```bash
# .env - 非敏感配置
LOG_LEVEL=info
ENABLE_METRICS=true

# .env.secrets - 敏感信息
OPENAI_API_KEY=sk-...
DATABASE_PASSWORD=...

# 在 docker-compose.yml 中
services:
  llm-service:
    env_file:
      - ../../.env
      - ../../.env.secrets  # 可选，如果存在
```

### 5. 验证必需变量

创建验证脚本：

```bash
#!/bin/bash
# scripts/validate_env.sh

required_vars=(
  "OPENAI_API_KEY"
  "POSTGRES_PASSWORD"
  "REDIS_HOST"
)

missing=()
for var in "${required_vars[@]}"; do
  if [ -z "${!var}" ]; then
    missing+=("$var")
  fi
done

if [ ${#missing[@]} -ne 0 ]; then
  echo "错误: 缺少必需的环境变量:"
  printf '  - %s\n' "${missing[@]}"
  exit 1
fi

echo "✅ 所有必需的环境变量已设置"
```

使用：

```bash
source .env
./scripts/validate_env.sh
```

### 6. 使用 Docker Secrets（生产环境）

对于生产部署，使用 Docker Swarm secrets：

```yaml
# docker-compose.prod.yml
services:
  llm-service:
    secrets:
      - openai_api_key
    environment:
      OPENAI_API_KEY_FILE: /run/secrets/openai_api_key

secrets:
  openai_api_key:
    external: true
```

### 7. 定期轮换密钥

```bash
# 1. 生成新密钥
NEW_KEY=$(openssl rand -base64 32)

# 2. 更新 .env
sed -i "s/REDIS_PASSWORD=.*/REDIS_PASSWORD=$NEW_KEY/" .env

# 3. 重启服务
docker compose -f deploy/compose/docker-compose.yml restart redis

# 4. 验证
docker compose -f deploy/compose/docker-compose.yml exec redis redis-cli AUTH $NEW_KEY PING
```

## 环境变量参考

### 完整列表

有关所有支持的环境变量的完整列表，请参阅：
- [.env.example](../../.env.example) - 带注释的示例
- [docker-compose.yml](../../deploy/compose/docker-compose.yml) - 默认值
- 服务特定文档（如 [llm-service](../../python/llm-service/README.md)）

### 按类别

#### LLM 提供商
- `OPENAI_API_KEY` - OpenAI API 密钥
- `ANTHROPIC_API_KEY` - Anthropic Claude API 密钥
- `GOOGLE_API_KEY` - Google Gemini API 密钥
- `XAI_API_KEY` - xAI Grok API 密钥
- `DEEPSEEK_API_KEY` - DeepSeek API 密钥
- `GROQ_API_KEY` - Groq API 密钥

#### 搜索提供商
- `EXA_API_KEY` - Exa.ai 搜索 API
- `BRAVE_API_KEY` - Brave 搜索 API
- `TAVILY_API_KEY` - Tavily 搜索 API

#### 数据库
- `POSTGRES_*` - PostgreSQL 配置
- `REDIS_*` - Redis 配置
- `QDRANT_*` - Qdrant 向量数据库

#### 服务
- `*_ADDR` / `*_HOST` / `*_PORT` - 服务端点
- `TEMPORAL_*` - Temporal 工作流引擎

#### 功能
- `ENABLE_*` - 功能标志
- `*_ENABLED` - 模块启用/禁用
- `LOG_LEVEL` - 日志详细程度

## 下一步

- 阅读[快速开始指南](../../README.md#quick-start)
- 查看[测试指南](./testing.md)
- 探索[模板工作流](./template-workflows.md)
- 学习[添加自定义工具](./adding-custom-tools.md)

---

需要帮助？在 [GitHub Issues](https://github.com/Kocoro-lab/Shannon/issues) 提问或加入我们的 Discord！

