# Shannon Platform - Rocky Linux 9.6 部署故障排查完整指南

## 系统环境
- **操作系统**: Rocky Linux 9.6 (Blue Onyx)
- **内核版本**: Linux 5.14.0-427.13.1.el9_4.x86_64
- **部署日期**: 2025-09-18
- **平台架构**: x86_64

## 故障问题汇总

本文档整合了在 Rocky Linux 9.6 系统上部署 Shannon 平台时遇到的所有故障及其解决方案。

---

## 故障一：buf 命令找不到

### 问题描述
运行 `./scripts/setup-remote.sh` 脚本时出现错误：
```bash
./scripts/setup-remote.sh: line 35: buf: command not found
```

### 根因分析
1. **PATH 环境变量问题**
   - buf 已安装到 `/usr/local/bin/buf`
   - 但 `/usr/local/bin` 不在当前用户的 PATH 中
   - 导致直接执行 `buf generate` 命令失败

2. **错误的文件路径检查**
   - 脚本检查了错误的 proto 生成路径
   - Python 的 proto 文件实际生成在 `protos/gen/python`

### 解决方案

#### 修复 buf 命令路径
```bash
# 文件：/data/Shannon/scripts/setup-remote.sh
# 第 35 行

# 原代码
buf generate

# 修改为
/usr/local/bin/buf generate
```

#### 修复 proto 文件路径检查
```bash
# 第 40-54 行

# 原代码
if [ ! -d "rust/agent-core/src/pb" ]; then
    echo "ERROR: Proto generation failed - rust/agent-core/src/pb not found"
    exit 1
fi

# 修改为
if [ ! -d "protos/gen/python" ]; then
    echo "ERROR: Proto generation failed - protos/gen/python not found"
    exit 1
fi
```

### 验证方法
```bash
# 重新运行脚本
./scripts/setup-remote.sh

# 预期输出
=== Shannon Remote Setup Script ===
Installing buf...
Generating proto files...
Proto files generated successfully!
=== Setup Complete ===
```

---

## 故障二：LLM Service Protobuf 模块缺失

### 问题描述
LLM 服务容器持续重启，smoke 测试失败：
```
ModuleNotFoundError: No module named 'llm_service.generated'
```

### 错误链
1. 初始错误：`ModuleNotFoundError: No module named 'llm_service.generated'`
2. 修复后错误：`ModuleNotFoundError: No module named 'common'`
3. 版本冲突：`grpcio-tools 1.68.1 incompatible with protobuf 6.x`

### 根因分析
- Python 服务缺少从 `.proto` 定义生成的 protobuf 文件
- Rocky Linux 的包管理器 (dnf) 提供的 protobuf 包不完整
- grpcio-tools 和 protobuf 版本不兼容

### 解决方案

#### 步骤 1：安装 Protobuf 编译器
由于 Rocky Linux 的 dnf 包不完整，需要手动安装：

```bash
# 下载官方 protoc 二进制文件
cd /tmp
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v24.4/protoc-24.4-linux-x86_64.zip

# 解压并安装
unzip -o protoc-24.4-linux-x86_64.zip
mv bin/protoc /usr/local/bin/
chmod +x /usr/local/bin/protoc

# 复制包含文件
cp -r /tmp/include/google /usr/local/include/
```

#### 步骤 2：安装 Python 依赖
```bash
# Rocky Linux 需要先安装 pip
dnf install -y python3-pip

# 安装兼容版本的 grpcio-tools
pip3 install grpcio-tools==1.68.1
```

#### 步骤 3：生成 Protobuf 文件
```bash
cd /data/Shannon/protos

# 创建目标目录
mkdir -p ../python/llm-service/llm_service/generated/agent
mkdir -p ../python/llm-service/llm_service/generated/common

# 生成 Python protobuf 文件
python3 -m grpc_tools.protoc \
    --python_out=../python/llm-service/llm_service/generated \
    --grpc_python_out=../python/llm-service/llm_service/generated \
    --pyi_out=../python/llm-service/llm_service/generated \
    -I . -I /usr/local/include \
    common/common.proto agent/agent.proto

# 创建 Python 包所需的 __init__.py 文件
touch ../python/llm-service/llm_service/generated/__init__.py
touch ../python/llm-service/llm_service/generated/agent/__init__.py
touch ../python/llm-service/llm_service/generated/common/__init__.py
```

#### 步骤 4：修复导入路径
编辑生成的文件 `/data/Shannon/python/llm-service/llm_service/generated/agent/agent_pb2.py`：

```python
# 将
from common import common_pb2 as common_dot_common__pb2

# 改为
from ..common import common_pb2 as common_dot_common__pb2
```

#### 步骤 5：处理版本冲突
更新 `/data/Shannon/python/llm-service/requirements.txt`：

```txt
# gRPC - 注意版本兼容性
grpcio==1.68.1
grpcio-tools==1.68.1
protobuf>=5.26.1,<6.0  # 与 grpcio-tools 1.68.1 兼容
```

#### 步骤 6：临时禁用问题模块（如需要）
如果特定模块仍有问题，可在 `/data/Shannon/python/llm-service/llm_service/tools/builtin/__init__.py` 中临时禁用：

```python
# 临时禁用直到 proto 生成修复
# from .python_wasi_executor import PythonWasiExecutorTool
```

#### 步骤 7：重建并部署
```bash
cd /data/Shannon/deploy/compose

# 重建 Docker 镜像
docker compose build llm-service

# 重启服务
docker compose up -d llm-service

# 验证服务健康状态
sleep 10
docker ps | grep llm-service
```

### 验证方法
```bash
cd /data/Shannon
make smoke

# 预期输出
[OK] All smoke checks passed.
```

---

## Rocky Linux 特定注意事项

### 1. 包管理器差异
Rocky Linux 使用 `dnf` 而不是 `apt-get`：

```bash
# Ubuntu/Debian
apt-get install protobuf-compiler

# Rocky Linux (包可能不完整)
dnf install protobuf protobuf-c  # 不包含 protoc 二进制文件
```

### 2. Python 包管理
```bash
# 需要先安装 pip
dnf install -y python3-pip

# pip 可能需要升级
pip3 install --upgrade pip
```

### 3. 系统库路径
Rocky Linux 的库路径可能不同：
- 标准路径：`/usr/local/bin`、`/usr/local/lib`
- Python 包：`/usr/local/lib64/python3.9/site-packages`

### 4. SELinux 考虑
Rocky Linux 默认启用 SELinux，可能影响文件访问：

```bash
# 检查 SELinux 状态
getenforce

# 临时禁用（仅用于测试）
setenforce 0

# 或配置适当的 SELinux 上下文
chcon -R -t container_file_t /data/Shannon
```

---

## 通用故障排查流程

### 1. 诊断步骤
```bash
# 检查服务状态
docker ps -a | grep shannon

# 查看容器日志
docker logs <container-name> --tail 50

# 过滤特定错误
docker logs <container-name> 2>&1 | grep -E "Error|Exception|Failed"

# 检查端口占用
netstat -tlnp | grep -E "8000|8088|50051|50052"
```

### 2. 常见问题快速检查
```bash
# Proto 文件是否生成
ls -la /data/Shannon/python/llm-service/llm_service/generated/

# Python 依赖是否正确
pip3 list | grep -E "grpc|protobuf"

# Docker 镜像是否最新
docker images | grep shannon

# 服务健康检查
curl -s http://localhost:8000/health || echo "Service not responding"
```

### 3. 清理和重建
```bash
# 清理旧容器和镜像
docker compose down -v
docker system prune -f

# 重新生成 proto 文件
cd /data/Shannon
make proto  # 或使用手动命令

# 重建所有服务
docker compose build
docker compose up -d

# 运行测试
make smoke
```

---

## 预防措施和最佳实践

### 1. 自动化 Proto 生成
添加到 Makefile：

```makefile
proto-python:
	@echo "Generating Python protobuf files..."
	@cd protos && python3 -m grpc_tools.protoc \
		--python_out=../python/llm-service/llm_service/generated \
		--grpc_python_out=../python/llm-service/llm_service/generated \
		--pyi_out=../python/llm-service/llm_service/generated \
		-I . -I /usr/local/include \
		$$(find . -name "*.proto")

build-llm: proto-python
	@docker compose build llm-service
```

### 2. 依赖版本锁定
创建 `constraints.txt`：

```txt
# Rocky Linux 9.6 兼容版本
grpcio==1.68.1
grpcio-tools==1.68.1
protobuf==5.29.5
```

### 3. 部署前检查脚本
创建 `scripts/pre-deploy-check.sh`：

```bash
#!/bin/bash
set -e

echo "=== Pre-deployment Check for Rocky Linux ==="

# 检查系统版本
if ! grep -q "Rocky Linux" /etc/os-release; then
    echo "Warning: This script is optimized for Rocky Linux"
fi

# 检查必要工具
for tool in docker python3 pip3 make; do
    if ! command -v $tool &> /dev/null; then
        echo "ERROR: $tool is not installed"
        exit 1
    fi
done

# 检查 protoc
if ! /usr/local/bin/protoc --version &> /dev/null; then
    echo "ERROR: protoc not found at /usr/local/bin/protoc"
    exit 1
fi

# 检查 Python 包
python3 -c "import grpc_tools" || {
    echo "ERROR: grpcio-tools not installed"
    exit 1
}

echo "=== All checks passed ==="
```

### 4. 环境变量配置
添加到 `.env` 文件：

```bash
# Rocky Linux 特定配置
PROTO_PATH=/usr/local/include
PROTOC_BIN=/usr/local/bin/protoc
PYTHON_VERSION=3.9
```

---

## 快速参考命令集

```bash
# Proto 生成
cd /data/Shannon/protos
python3 -m grpc_tools.protoc \
    --python_out=../python/llm-service/llm_service/generated \
    --grpc_python_out=../python/llm-service/llm_service/generated \
    --pyi_out=../python/llm-service/llm_service/generated \
    -I . -I /usr/local/include \
    common/common.proto agent/agent.proto

# 服务管理
docker compose build llm-service
docker compose up -d llm-service
docker logs shannon-llm-service-1 --tail 50

# 测试
cd /data/Shannon && make smoke

# 故障诊断
docker ps -a | grep shannon
docker logs <container> 2>&1 | grep -E "Error|Exception"
curl -s http://localhost:8000/health
```

---

## 相关文件路径

- **脚本文件**：`/data/Shannon/scripts/setup-remote.sh`
- **Buf 配置**：`/data/Shannon/protos/buf.gen.yaml`
- **Proto 定义**：`/data/Shannon/protos/`
- **生成的 Go 文件**：`/data/Shannon/go/orchestrator/internal/pb/`
- **生成的 Python 文件**：`/data/Shannon/python/llm-service/llm_service/generated/`
- **Docker Compose**：`/data/Shannon/deploy/compose/docker-compose.yml`
- **环境配置**：`/data/Shannon/.env`

---

## 总结

在 Rocky Linux 9.6 上部署 Shannon 平台的主要挑战：

1. **包管理器差异**：dnf vs apt，某些包不完整或命名不同
2. **Proto 生成复杂性**：需要手动安装 protoc 和配置路径
3. **Python 版本和依赖**：需要注意兼容性问题
4. **SELinux**：可能需要额外的安全配置

通过本文档的解决方案，可以成功在 Rocky Linux 9.6 上部署和运行 Shannon 平台的所有组件。

---

**文档维护**
- 创建日期：2025-09-18
- 最后更新：2025-09-18
- 适用版本：Shannon Platform on Rocky Linux 9.6