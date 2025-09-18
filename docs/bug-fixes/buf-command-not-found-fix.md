# Bug Fix: buf command not found 错误修复记录

## 问题描述
在运行 `./scripts/setup-remote.sh` 脚本时，出现以下错误：
```
./scripts/setup-remote.sh: line 35: buf: command not found
```

## 问题原因分析

1. **PATH 环境变量问题**
   - 虽然脚本在第 10-16 行已经安装了 `buf` 到 `/usr/local/bin/buf`
   - 但是 `/usr/local/bin` 不在当前用户的 PATH 环境变量中
   - 导致直接执行 `buf generate` 命令时找不到 buf 可执行文件

2. **错误的文件路径检查**
   - 脚本在第 46-54 行检查生成的 proto 文件时，使用了错误的路径
   - 实际上 Rust 的 protobuf 文件是由 Rust 构建过程单独处理的
   - Python 的 proto 文件生成在 `protos/gen/python` 而不是 `python/llm-service/llm_service/pb`

## 修复方法

### 1. 修复 buf 命令路径问题
**文件**: `/data/Shannon/scripts/setup-remote.sh`
**修改位置**: 第 35 行

```bash
# 原代码
buf generate

# 修改后
/usr/local/bin/buf generate
```

使用完整路径 `/usr/local/bin/buf` 来确保能够找到 buf 命令。

### 2. 修复 proto 文件路径检查
**文件**: `/data/Shannon/scripts/setup-remote.sh`
**修改位置**: 第 40-54 行

```bash
# 原代码
if [ ! -d "rust/agent-core/src/pb" ]; then
    echo "ERROR: Proto generation failed - rust/agent-core/src/pb not found"
    exit 1
fi

if [ ! -d "python/llm-service/llm_service/pb" ]; then
    echo "ERROR: Proto generation failed - python/llm-service/llm_service/pb not found"
    exit 1
fi

# 修改后
# Python proto files are generated in protos/gen/python
if [ ! -d "protos/gen/python" ]; then
    echo "ERROR: Proto generation failed - protos/gen/python not found"
    exit 1
fi

# Note: Rust protobuf generation is handled separately by the Rust build process
```

## 验证结果
修复后脚本成功运行，输出如下：
```
=== Shannon Remote Setup Script ===

Installing buf...
Generating proto files...
Proto files generated successfully!

=== Setup Complete ===

Proto files have been generated successfully.
You can now run 'make dev' to start the Shannon stack.
```

## 经验总结

1. **始终使用绝对路径**：当安装工具到特定位置后，如果不确定 PATH 是否包含该目录，最好使用绝对路径调用
2. **验证实际生成路径**：检查文件生成位置时，应该先验证实际的输出路径，而不是假设路径
3. **分离关注点**：不同语言的构建过程可能有不同的处理方式（如 Rust 的 protobuf 生成是由 Cargo 构建过程处理的）

## 相关文件
- 脚本文件：`/data/Shannon/scripts/setup-remote.sh`
- Buf 配置：`/data/Shannon/protos/buf.gen.yaml`
- 生成的 Go proto 文件：`/data/Shannon/go/orchestrator/internal/pb/`
- 生成的 Python proto 文件：`/data/Shannon/protos/gen/python/`

## 修复时间
2025-09-18