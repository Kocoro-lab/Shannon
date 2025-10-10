# Shannon 中的 Python 代码执行

## 概述

Shannon 通过 WebAssembly 系统接口（WASI）提供安全的 Python 代码执行，确保完整的沙箱隔离和资源管理。本文档涵盖 Python 执行系统的设置、使用和架构。

## 关键设置要求 ⚠️

### 前提条件 - 启动前必须完成

#### 1. 下载 Python WASI 解释器（必需）

```bash
# 在启动服务之前必须运行此命令
./scripts/setup_python_wasi.sh

# 验证文件已下载（应约 20MB）
ls -lh wasm-interpreters/python-3.11.4.wasm
```

#### 2. 理解 WebAssembly 表限制

**重要提示**："表限制"是 WebAssembly 运行时概念：

- **WebAssembly 表**：存储 WASM 模块中函数引用的内存结构
- **Python 需要大表**：CPython 有数千个内部函数（5413+ 条目）
- **默认限制**：默认应超过 5000+，Python 需要 10000+

#### 3. 必需的配置更改

必须在 `rust/agent-core/src/wasi_sandbox.rs` 中配置以下内容：

```rust
// 在 WasiSandbox::with_config() 中
execution_timeout: app_config.wasi_timeout(),
// Python WASM 需要更大的表限制（5413+ 元素）
table_elements_limit: 10000,  // 关键：默认 1024 太小！
instances_limit: 10,           // 需要多个 WASM 实例
tables_limit: 10,              // 多个函数表
memories_limit: 4,             // 内存区域
```

#### 4. 构建并启动服务

```bash
# 使用配置更改重新构建 agent-core
docker compose -f deploy/compose/docker-compose.yml build --no-cache agent-core

# 重新构建 llm-service 以包含 Python 执行器
docker compose -f deploy/compose/docker-compose.yml build llm-service

# 启动所有服务
make dev
```

## 快速开始（完成设置后）

### 2. 执行 Python 代码

```bash
# 简单执行
./scripts/submit_task.sh "执行 Python: print('你好，Shannon！')"

# 数学计算
./scripts/submit_task.sh "执行 Python 代码计算 10 的阶乘"
# 或更直接：
./scripts/submit_task.sh "运行 Python 代码计算 10 的阶乘"

# 数据处理
./scripts/submit_task.sh "使用 Python 生成前 20 个斐波那契数"
```

## 架构

```
┌──────────────────────────────────────────────────────────┐
│                     用户请求                              │
│           "执行 Python: print(2+2)"                       │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│                 编排器 (Go)                               │
│    根据复杂度路由到 LLM 服务                              │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│                LLM 服务 (Python)                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │         PythonWasiExecutorTool                   │    │
│  │  • 检测 Python 执行需求                          │    │
│  │  • 准备执行上下文                                │    │
│  │  • 管理会话状态（可选）                          │    │
│  │  • 缓存解释器以提高性能                          │    │
│  └────────────────┬─────────────────────────────────┘    │
└──────────────────┼────────────────────────────────────────┘
                   ▼
┌──────────────────────────────────────────────────────────┐
│               Agent Core (Rust)                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │          WASI 沙箱 (Wasmtime)                    │    │
│  │  ┌────────────────────────────────────────────┐  │    │
│  │  │    Python.wasm (CPython 3.11.4)            │  │    │
│  │  │  • 完整标准库                              │  │    │
│  │  │  • 内存限制：256MB                         │  │    │
│  │  │  • CPU 限制：可配置                        │  │    │
│  │  │  • 超时：30 秒（最大 60 秒）               │  │    │
│  │  │  • 无网络访问                              │  │    │
│  │  │  • 只读文件系统                            │  │    │
│  │  └────────────────────────────────────────────┘  │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

## 功能特性

### ✅ 生产级功能

- **完整的 Python 标准库**：完整的 CPython 3.11.4 及所有内置模块
- **会话持久化**：使用会话 ID 跨执行维护变量
- **性能优化**：缓存解释器将启动时间从 500ms 减少到 50ms
- **资源限制**：可配置的内存、CPU 和超时限制
- **安全隔离**：通过 WASI 实现真正的沙箱 - 无网络、无文件系统写入
- **输出流式传输**：长时间运行计算的渐进式输出
- **错误处理**：全面的错误消息和超时保护

### 🚀 高级能力

- **持久会话**：变量和导入在执行间持久化
- **自定义超时**：每次执行可从 1 到 60 秒调整
- **标准输入支持**：向 Python 脚本提供输入数据
- **性能指标**：执行时间跟踪和报告

## 使用示例

### 基本 Python 执行

```python
# 请求
"执行 Python: print('你好，世界！')"

# 输出
你好，世界！
```

### 数学计算

```python
# 请求
"执行 Python 代码计算 10 的阶乘"

# 生成的代码
import math
result = math.factorial(10)
print(f"10! = {result}")

# 输出
10! = 3628800
```

### 数据处理

```python
# 请求
"使用 Python 生成前 20 个斐波那契数"

# 生成的代码
def fibonacci(n):
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[-1] + fib[-2])
    return fib

result = fibonacci(20)
print(f"前 20 个斐波那契数: {result}")

# 输出
前 20 个斐波那契数: [0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597, 2584, 4181]
```

### 会话持久化

```python
# 请求 1（使用 session_id: "data-analysis"）
"执行 Python: data = [1, 2, 3, 4, 5]"

# 请求 2（相同 session_id）
"执行 Python: import statistics; print(statistics.mean(data))"

# 输出
3
```

## API 集成

### 直接 API 调用

```bash
curl -X POST http://localhost:8000/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "执行 Python: print(sum(range(100)))",
    "tools": ["python_executor"],
    "mode": "standard"
  }'
```

### 工具参数

```json
{
  "tool": "python_executor",
  "parameters": {
    "code": "print('你好')",
    "session_id": "可选的会话ID",
    "timeout_seconds": 30,
    "stdin": "可选的输入数据"
  }
}
```

## 配置

### 环境变量

```bash
# Python WASI 解释器路径（必需）
PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm

# Agent Core 地址（用于 gRPC 通信）
AGENT_CORE_ADDR=agent-core:50051
```

### 资源限制配置

#### 在代码中（rust/agent-core/src/wasi_sandbox.rs）

```rust
// 关键：这些值必须设置才能使 Python WASM 工作！
pub fn with_config(app_config: &Config) -> Result<Self> {
    // ...
    Ok(Self {
        // ...
        // Python WASM 需要更大的表限制（5413+ 元素）
        table_elements_limit: 10000,  // 必须 ≥ 5413 才能运行 Python
        instances_limit: 10,           // 必须 ≥ 4
        tables_limit: 10,              // 必须 ≥ 4
        memories_limit: 4,             // 必须 ≥ 2
    })
}
```

#### 环境变量（docker-compose.yml）

```yaml
agent-core:
  environment:
    - WASI_MEMORY_LIMIT_MB=512      # Python 执行的内存
    - WASI_TIMEOUT_SECONDS=60        # 最大执行时间
    - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm

llm-service:
  environment:
    - PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/python-3.11.4.wasm
    - AGENT_CORE_ADDR=agent-core:50051
```

## 安全模型

### 沙箱保证

| 功能              | 状态      | 描述                                |
| ----------------- | --------- | ----------------------------------- |
| **内存隔离**      | ✅ 强制执行 | 每次执行的独立线性内存空间           |
| **网络访问**      | ❌ 阻止    | 未授予网络功能                       |
| **文件系统**      | 🔒 只读    | 限制为白名单路径                     |
| **进程生成**      | ❌ 阻止    | 无法创建子进程                       |
| **资源限制**      | ✅ 强制执行 | 内存、CPU 和时间限制                 |
| **系统调用**      | ❌ 阻止    | 无法访问主机系统调用                 |

### 为什么选择 WASI？

1. **真正隔离**：WebAssembly 提供硬件级隔离
2. **确定性**：相同代码在跨平台产生相同结果
3. **资源控制**：对内存和 CPU 使用的细粒度控制
4. **无法逃逸**：即使使用恶意代码也无法突破沙箱
5. **行业标准**：被 Cloudflare Workers、Fastly 等使用

## 支持的 Python 功能

### ✅ 完全支持

- 所有 Python 3.11 语法和功能
- 标准库模块：
  - `math`、`statistics`、`random`
  - `json`、`csv`、`xml`
  - `datetime`、`calendar`、`time`
  - `re`、`string`、`textwrap`
  - `collections`、`itertools`、`functools`
  - `hashlib`、`base64`、`binascii`
  - `decimal`、`fractions`
  - `pathlib`、`os.path`（只读）

### ⚠️ 有限支持

- `io`：仅内存操作
- `sqlite3`：仅内存数据库
- 文件操作：对 `/tmp` 的只读访问

### ❌ 不支持

- 网络操作（`requests`、`urllib`、`socket`）
- 包安装（`pip install`）
- 原生扩展（C 模块）
- GUI 库（`tkinter`、`pygame`）
- 多进程/多线程
- 系统操作（`subprocess`、`os.system`）

## 性能特征

| 操作                        | 时间   | 内存 |
| --------------------------- | ------ | ---- |
| 解释器加载（首次）          | ~500ms | 20MB |
| 解释器加载（缓存）          | ~50ms  | 0MB  |
| 简单打印                    | ~100ms | 50MB |
| 阶乘(1000)                  | ~150ms | 55MB |
| 排序 10,000 个数字          | ~200ms | 60MB |
| 处理 1MB JSON               | ~300ms | 80MB |

## 故障排除

### 常见问题和解决方案

#### 1. "Failed to instantiate WASM module: table minimum size of 5413 elements exceeds table limits"

**原因**：WebAssembly 表限制对于 Python WASM 太小。

**解决方案**：更新 `rust/agent-core/src/wasi_sandbox.rs`：

```rust
table_elements_limit: 10000,  // 从默认 1024 增加
```

然后重新构建：`docker compose build agent-core`

#### 2. "Python WASI interpreter not found"

**原因**：未下载 Python WASM 文件。

**解决方案**：

```bash
# 下载解释器
./scripts/setup_python_wasi.sh

# 验证其存在（应约 20MB）
ls -lh wasm-interpreters/python-3.11.4.wasm
```

#### 3. "can't open file '//import sys; exec(sys.stdin.read())': [Errno 44] No such file or directory"

**原因**：Python WASM 的 argv 格式不正确。

**解决方案**：确保 `python_wasi_executor.py` 使用正确的 argv：

```python
"argv": ["python", "-c", "import sys; exec(sys.stdin.read())"],  # 注意：argv[0] 必须是 "python"
```

#### 4. "Execution timeout"

**原因**：Python 代码执行时间超过超时限制。

**解决方案**：

```python
# 在请求中增加超时（最大 60 秒）
{
  "code": "long_running_code()",
  "timeout_seconds": 60
}
```

#### 5. "Memory limit exceeded"

**原因**：Python 代码使用的内存超过分配的内存。

**解决方案**：在环境变量中增加内存限制：

```bash
# 在 docker-compose.yml 的 agent-core 中
environment:
  - WASI_MEMORY_LIMIT_MB=512  # 从 256 增加
```

#### 6. "Module not found"

**原因**：尝试导入外部包（numpy、pandas 等）。

**解决方案**：仅 Python 标准库可用。外部包必须使用 stdlib 重新实现。

#### 7. "WASI execution error" 无详细信息

**原因**：各种问题 - 检查 agent-core 日志。

**解决方案**：

```bash
# 检查详细日志
docker compose logs agent-core --tail 100 | grep -E "WASI|Python|code_executor"

# 在 docker-compose.yml 中启用调试日志
environment:
  - RUST_LOG=debug,shannon_agent_core::wasi_sandbox=debug
```

## 最佳实践

1. **保持代码简单**：复杂操作会增加执行时间
2. **明智使用会话**：会话会消耗内存 - 完成后清理
3. **处理超时**：长时间计算应分解为步骤
4. **最小化导入**：每次导入都会增加开销
5. **批量操作**：分块处理数据以获得更好的性能

## 与替代方案的比较

| 功能              | Shannon WASI | Docker 容器  | AWS Lambda  | Google Colab |
| ----------------- | ------------ | ------------ | ----------- | ------------ |
| **安全性**        | ⭐⭐⭐⭐⭐        | ⭐⭐⭐          | ⭐⭐⭐⭐        | ⭐⭐⭐          |
| **启动时间**      | 50-100ms     | 1-5s         | 100-500ms   | 5-10s        |
| **内存开销**      | 50MB         | 200MB+       | 128MB+      | 1GB+         |
| **包支持**        | 仅标准库     | 完整         | 完整        | 完整         |
| **网络访问**      | 否           | 是           | 是          | 是           |
| **成本**          | 最小         | 中等         | 按请求计费  | 免费/付费    |
| **确定性**        | 是           | 否           | 大部分      | 否           |

## 未来路线图

### 计划的增强功能

1. **包支持**：通过 Pyodide 预编译 NumPy、Pandas
2. **多语言**：JavaScript（QuickJS）、Ruby（ruby.wasm）
3. **调试**：逐步调试支持
4. **可视化**：通过 base64 图像输出 Matplotlib
5. **分布式执行**：用于大型任务的多节点执行

## 技术细节

### 解释器：CPython 3.11.4

- **来源**：[VMware WebAssembly Language Runtimes](https://github.com/vmware-labs/webassembly-language-runtimes)
- **大小**：20MB 压缩
- **Python 版本**：3.11.4
- **构建日期**：2023-07-14
- **兼容性**：100% CPython 兼容

### WASI 运行时：Wasmtime

- **版本**：最新稳定版
- **功能**：WASI Preview 1
- **安全性**：基于能力的安全模型
- **性能**：接近原生执行速度

## 获取帮助

### 资源

- [WebAssembly 规范](https://webassembly.github.io/spec/)
- [WASI 文档](https://wasi.dev/)
- [Python WASM 指南](https://github.com/vmware-labs/webassembly-language-runtimes/tree/main/python)
- [Shannon 文档](https://github.com/Kocoro-lab/Shannon)

### 支持

- GitHub Issues：[shannon/issues](https://github.com/Kocoro-lab/shannon/issues)
- 文档：[docs/](../docs/)

---

*最后更新：2025 年 1 月*

