# LLM Service Protobuf Generation Issue - Troubleshooting Guide

## Issue Summary
**Date**: 2025-09-18
**Service**: shannon-llm-service
**Error**: `ModuleNotFoundError: No module named 'llm_service.generated'`
**Impact**: LLM service container continuously restarting, smoke tests failing

## Root Cause Analysis

### Problem Description
The LLM service failed to start due to missing generated protobuf files. The Python service was trying to import gRPC/protobuf modules that hadn't been generated from the `.proto` definitions.

### Error Chain
1. Initial error: `ModuleNotFoundError: No module named 'llm_service.generated'`
2. After initial fix attempt: `ModuleNotFoundError: No module named 'common'`
3. Version conflict: grpcio-tools 1.68.1 incompatible with protobuf 6.x

## Troubleshooting Steps

### 1. Initial Diagnosis
```bash
# Check service status
docker ps | grep llm

# View container logs
docker logs shannon-llm-service-1 --tail 30

# Identify the specific error
docker logs shannon-llm-service-1 2>&1 | grep -E "Error|Exception|ModuleNotFoundError"
```

### 2. Identify Missing Dependencies
- Protobuf compiler (`protoc`) not installed on host
- Python gRPC tools not installed
- Generated protobuf files missing from Python service directory

### 3. Environment Analysis
```bash
# Check OS version (Rocky Linux 9.6)
cat /etc/os-release

# Search for available packages
dnf search protobuf
```

## Solution Implementation

### Step 1: Install Protobuf Compiler
```bash
# Download official protoc binary (dnf package incomplete)
cd /tmp
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v24.4/protoc-24.4-linux-x86_64.zip
unzip -o protoc-24.4-linux-x86_64.zip
mv bin/protoc /usr/local/bin/
chmod +x /usr/local/bin/protoc

# Copy include files for proto imports
cp -r /tmp/include/google /usr/local/include/
```

### Step 2: Install Python Dependencies
```bash
# Install pip if missing
dnf install -y python3-pip

# Install grpcio-tools with compatible version
pip3 install grpcio-tools==1.68.1
```

### Step 3: Generate Protobuf Files
```bash
cd /data/Shannon/protos

# Create target directories
mkdir -p ../python/llm-service/llm_service/generated/agent
mkdir -p ../python/llm-service/llm_service/generated/common

# Generate Python protobuf files
python3 -m grpc_tools.protoc \
    --python_out=../python/llm-service/llm_service/generated \
    --grpc_python_out=../python/llm-service/llm_service/generated \
    --pyi_out=../python/llm-service/llm_service/generated \
    -I . -I /usr/local/include \
    common/common.proto agent/agent.proto

# Create __init__.py files for Python packages
touch ../python/llm-service/llm_service/generated/__init__.py
touch ../python/llm-service/llm_service/generated/agent/__init__.py
touch ../python/llm-service/llm_service/generated/common/__init__.py
```

### Step 4: Fix Import Paths
Edit `/data/Shannon/python/llm-service/llm_service/generated/agent/agent_pb2.py`:
```python
# Change from:
from common import common_pb2 as common_dot_common__pb2

# To:
from ..common import common_pb2 as common_dot_common__pb2
```

### Step 5: Handle Version Conflicts
Update `/data/Shannon/python/llm-service/requirements.txt`:
```txt
# gRPC
grpcio==1.68.1
grpcio-tools==1.68.1
protobuf>=5.26.1,<6.0  # Compatible with grpcio-tools 1.68.1
```

### Step 6: Temporary Workaround
If specific modules still fail, temporarily disable them in `/data/Shannon/python/llm-service/llm_service/tools/builtin/__init__.py`:
```python
# Temporarily disabled until proto generation is fixed
# from .python_wasi_executor import PythonWasiExecutorTool
```

### Step 7: Rebuild and Deploy
```bash
cd /data/Shannon/deploy/compose

# Rebuild the Docker image
docker compose build llm-service

# Restart the service
docker compose up -d llm-service

# Verify service is healthy
sleep 10
docker ps | grep llm-service
```

### Step 8: Verify Fix
```bash
cd /data/Shannon
make smoke
```

## Key Lessons Learned

### 1. Dependency Management
- **Issue**: Protobuf version conflicts between grpcio-tools and protobuf packages
- **Solution**: Pin compatible versions in requirements.txt
- **Best Practice**: Always specify version ranges for critical dependencies

### 2. Proto Generation Process
- **Issue**: Generated files missing from Docker build context
- **Solution**: Generate protos before building Docker images
- **Best Practice**: Include proto generation in CI/CD pipeline

### 3. Import Path Resolution
- **Issue**: Generated Python files use incorrect import paths
- **Solution**: Manually fix or configure protoc to use correct Python module paths
- **Best Practice**: Use proper protoc flags or post-processing scripts

### 4. Docker Build Context
- **Issue**: Generated files not included in Docker image
- **Solution**: Ensure proto generation happens before Docker build
- **Best Practice**: Add proto generation to Makefile targets

## Prevention Measures

### 1. Automated Proto Generation
Add to Makefile:
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

### 2. Version Pinning
Create `constraints.txt` for consistent builds:
```txt
grpcio==1.68.1
grpcio-tools==1.68.1
protobuf==5.29.5
```

### 3. Health Check Enhancement
Add detailed health checks in Docker Compose:
```yaml
healthcheck:
  test: ["CMD", "python", "-c", "import grpc; from llm_service.generated.agent import agent_pb2"]
  interval: 30s
  timeout: 10s
  retries: 3
```

### 4. Documentation
- Document the proto generation process in README
- Add troubleshooting section for common protobuf issues
- Include version compatibility matrix

## Quick Reference Commands

```bash
# Check service logs
docker logs shannon-llm-service-1 --tail 50

# Regenerate protos
cd /data/Shannon/protos && \
python3 -m grpc_tools.protoc \
    --python_out=../python/llm-service/llm_service/generated \
    --grpc_python_out=../python/llm-service/llm_service/generated \
    --pyi_out=../python/llm-service/llm_service/generated \
    -I . -I /usr/local/include \
    common/common.proto agent/agent.proto

# Rebuild and restart
docker compose build llm-service && docker compose up -d llm-service

# Run smoke test
cd /data/Shannon && make smoke
```

## Related Issues
- Proto generation automation: Consider using buf.build for consistent proto management
- Python package structure: Review import paths and module organization
- Docker multi-stage builds: Optimize build process to include proto generation

## References
- [Protocol Buffers Python Tutorial](https://protobuf.dev/getting-started/pythontutorial/)
- [gRPC Python Quick Start](https://grpc.io/docs/languages/python/quickstart/)
- [Docker Multi-stage Builds](https://docs.docker.com/build/building/multi-stage/)