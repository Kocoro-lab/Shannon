# Shannon 项目 Windows 本地环境适配指南

本文档旨在帮助开发人员在 Windows 环境下顺利搭建和运行 Shannon 智能体平台。由于项目原生脚本主要针对 Linux/macOS 环境，在 Windows 上运行时需要进行特定的适配和配置。

## 1. 前置软件准备 (Prerequisites)

在开始之前，请确保安装以下软件。**建议使用 Git Bash 作为主要的终端工具。**

1.  **Git**: [下载链接](https://git-scm.com/download/win) (安装时勾选 Git Bash)
2.  **WSL 2 (Windows Subsystem for Linux)**:
    *   **必要性**: Docker Desktop on Windows 依赖 WSL 2 作为后端引擎来运行 Linux 容器。相比旧的 Hyper-V 后端，WSL 2 启动更快、资源占用更低，且能提供完整的 Linux 内核兼容性，这对运行复杂的微服务架构至关重要。
    *   **安装方法**: 以管理员身份打开 PowerShell，运行 `wsl --install`，然后重启电脑。
    *   **官方指南/手动下载**: [微软官方文档](https://learn.microsoft.com/zh-cn/windows/wsl/install)
3.  **Docker Desktop**: [下载链接](https://www.docker.com/products/docker-desktop/)
    *   安装后在设置中确保勾选 "Use the WSL 2 based engine"。
4.  **Go (Golang)**: [下载链接](https://go.dev/dl/) (建议 1.21+)
4.  **Python**: [下载链接](https://www.python.org/downloads/) (建议 3.10+)
    *   **重要**: 安装时勾选 "Add Python to PATH"。
    *   **避坑**: 在 Windows 设置中搜索“应用执行别名 (App execution aliases)”，**关闭** `python.exe` 和 `python3.exe` 的开关，防止系统调用到微软商店的空壳程序。
5.  **Node.js**: [下载链接](https://nodejs.org/) (建议 LTS 版本，用于桌面端)
6.  **Rust**: [下载链接](https://rustup.rs/) (桌面端 Tauri 依赖)
    *   **前置要求**: Windows 上编译 Rust 需要 C++ 链接器。
    *   **安装步骤**:
        1.  下载并安装 **[Visual Studio Build Tools](https://visualstudio.microsoft.com/visual-cpp-build-tools/)** (或 Visual Studio Community)。
        2.  在安装程序中，务必勾选 **"使用 C++ 的桌面开发" (Desktop development with C++)** 工作负载。
        3.  安装完成后，再运行 `rustup-init.exe` 安装 Rust。
7.  **Protoc (Protocol Buffers Compiler)**: [下载链接](https://github.com/protocolbuffers/protobuf/releases)
    *   **注意**: 下载 `protoc-xx.x-win64.zip` (不要下成 osx 或 linux 版)。
    *   **配置**: 解压到固定目录（如 `D:\Tools\protoc`），将 `bin` 目录添加到系统环境变量 `Path` 中。
    *   **关键**: 确保解压目录中包含 `include` 文件夹，否则生成代码时会报 `google/protobuf/timestamp.proto not found` 错误。

---

## 2. 项目初始化与配置

### 2.1 克隆项目
```bash
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon
```

### 2.2 配置环境变量 (.env)
Windows 下无法直接运行 `make setup` 中的 `ln -sf` 命令，需手动复制。

**两个 .env 文件的适用场景：**
*   **根目录 `.env`**: 供本地开发脚本（如 `scripts/setup_python_wasi.sh`）使用。
*   **`deploy/compose/.env`**: **核心配置**。供 Docker Compose 启动服务时加载环境变量。**必须在此文件中填入 API Key，否则服务无法运行。**

在 **Git Bash** 中执行：
```bash
# 1. 根目录配置
cp .env.example .env

# 2. Docker Compose 配置 (手动复制替代软链接)
cd deploy/compose
cp ../../.env .env
cd ../..
```

**注意**：由于是复制生成的两个独立文件，**修改配置时请务必同步修改这两个文件**（或使用 `mklink /H` 创建硬链接）。

打开 `.env` 文件，填入必要的 Key：
```properties
OPENAI_API_KEY=sk-xxxxxx
# 如果需要 Google 搜索，参考本文第 7 节配置
```

---

## 3. 核心脚本适配 (Proto 生成)

原项目 `scripts/generate_protos_local.sh` 在 Windows 下存在兼容性问题（`python3`/`pip3` 命令缺失、路径问题）。

### 3.1 修改脚本
请使用编辑器打开 `scripts/generate_protos_local.sh`，进行以下两处关键修改：

**1. 在文件开头添加 Python 命令检测逻辑**
在 `set -euo pipefail` 之后，插入以下代码块，用于自动识别 Windows 环境下的 `python` 或 `python3` 命令：

```bash
# Windows compatibility: Determine Python and Pip commands
if command -v python3 &> /dev/null; then
    PYTHON_CMD="python3"
elif command -v python &> /dev/null; then
    PYTHON_CMD="python"
else
    echo "Error: Python not found"
    exit 1
fi

if command -v pip3 &> /dev/null; then
    PIP_CMD="pip3"
elif command -v pip &> /dev/null; then
    PIP_CMD="pip"
else
    # Fallback to python -m pip if pip command is not found
    PIP_CMD="$PYTHON_CMD -m pip"
fi
```

**2. 替换硬编码的命令**
将脚本中所有的 `python3` 替换为 `"$PYTHON_CMD"`，所有的 `pip3` 替换为 `"$PIP_CMD"`。

例如：
*   原代码：`pip3 install grpcio-tools...`
*   修改后：`"$PIP_CMD" install grpcio-tools...`

*   原代码：`python3 -m grpc_tools.protoc...`
*   修改后：`"$PYTHON_CMD" -m grpc_tools.protoc...`

### 3.2 执行生成
在 Git Bash 中运行：
```bash
./scripts/generate_protos_local.sh
```
成功标志：看到输出 `Protobuf generation complete!` 且无报错。

---

## 4. WASI 环境配置

用于 Python 代码沙箱执行。
```bash
./scripts/setup_python_wasi.sh
```
*   提示是否下载 RustPython 时，选 `n` (No)。
*   提示是否更新 .env 时，选 `y` (Yes)。

---

## 5. 启动服务 (Docker Compose)

Windows 下建议显式指定项目名称和文件路径。

### 5.1 启动后端服务
```bash
cd deploy/compose
docker compose -p shannon up -d
```

### 5.2 常见问题：Temporal Not Ready
**现象**：桌面端提交任务报错 `Failed to submit task: Temporal not ready`，Orchestrator 日志显示 `context deadline exceeded`。
**原因**：Temporal 启动较慢，Orchestrator 在其就绪前尝试连接超时。

**解决方案**：
1.  确保 Temporal UI (`http://localhost:8088`) 可以访问。
2.  **注册 Namespace** (如果 UI 中没有 `default` namespace)：
    ```bash
    docker compose -p shannon exec temporal temporal operator namespace register default
    ```
3.  **重启 Orchestrator** (关键步骤)：
    ```bash
    docker compose -p shannon restart orchestrator
    ```

---

## 6. 桌面端启动 (Desktop App)

### 6.1 开发模式启动
```bash
cd desktop
npm install
npm run tauri dev
```
*   首次运行需要编译 Rust 后端，耗时较长，请耐心等待。

### 6.2 构建生产版本
如果你想生成一个 `.exe` 安装包或可执行文件：
```bash
npm run tauri build
```
构建好的文件通常位于 `desktop/src-tauri/target/release/bundle/nsis/` 下。

### 6.3 常见问题排查
*   **Rust 报错**：如果提示找不到 `cargo` 或 `rustc`，请确保你安装了 Rust 并重启了终端。
*   **WebView2 错误**：Tauri 依赖 Edge WebView2，Windows 10/11 通常自带。如果报错，可能需要手动安装 WebView2 Runtime。
*   **API 连接**：桌面应用通常需要连接后端的 API 服务。请确保你之前通过 `docker compose up` 启动的服务（Orchestrator, LLM Service 等）正在运行，并且端口（如 8081, 8000）没有被防火墙阻挡。

---

## 7. 配置 Google 联网搜索 (可选)

要启用 Agent 的联网搜索能力，需配置 Google Custom Search。

1.  **获取 API Key**: [Google Cloud Console](https://console.cloud.google.com/apis/credentials) -> Create Credentials -> API Key。
2.  **获取 Engine ID**: [Programmable Search Engine](https://programmablesearchengine.google.com/) -> Create -> 选择 "Search the entire web"。
3.  **修改 .env**:
    ```properties
    WEB_SEARCH_PROVIDER=google
    GOOGLE_SEARCH_API_KEY=你的API_KEY
    GOOGLE_SEARCH_ENGINE_ID=你的ENGINE_ID
    ```
4.  **重启服务**:
    ```bash
    cd deploy/compose
    docker compose -p shannon up -d
    ```

---

## 8. 常用命令速查表

| 操作 | 命令 (Git Bash) |
| :--- | :--- |
| **启动服务** | `cd deploy/compose && docker compose -p shannon up -d` |
| **停止服务** | `cd deploy/compose && docker compose -p shannon down` |
| **查看状态** | `cd deploy/compose && docker compose -p shannon ps` |
| **查看日志** | `cd deploy/compose && docker logs -f shannon-orchestrator-1` |
| **重启编排器** | `cd deploy/compose && docker compose -p shannon restart orchestrator` |
