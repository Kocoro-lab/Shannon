# Rocky Linux Shannon Development Environment Quick Start Guide

This guide is designed for Rocky Linux system users to quickly deploy and configure the Shannon production-grade AI agent platform development environment.

## Table of Contents

- [System Environment](#system-environment)
- [Dependencies Installation](#dependencies-installation)
- [Project Deployment](#project-deployment)
- [Service Verification](#service-verification)
- [Running Your First Task](#running-your-first-task)
- [Web Interface Access](#web-interface-access)
- [Common Management Commands](#common-management-commands)
- [Troubleshooting](#troubleshooting)

## System Environment

### Tested Environment Specifications

```bash
# Check system version information
cat /etc/os-release
```

**Recommended Configuration**:

```bash
NAME="Rocky Linux"
VERSION="9.6 (Blue Onyx)"
ID="rocky"
ID_LIKE="rhel centos fedora"
VERSION_ID="9.6"
PLATFORM_ID="platform:el9"
PRETTY_NAME="Rocky Linux 9.6 (Blue Onyx)"
ANSI_COLOR="0;32"
LOGO="fedora-logo-icon"
CPE_NAME="cpe:/o:rocky:rocky:9::baseos"
HOME_URL="https://rockylinux.org/"
VENDOR_NAME="RESF"
VENDOR_URL="https://resf.org/"
BUG_REPORT_URL="https://bugs.rockylinux.org/"
SUPPORT_END="2032-05-31"
ROCKY_SUPPORT_PRODUCT="Rocky-Linux-9"
ROCKY_SUPPORT_PRODUCT_VERSION="9.6"
REDHAT_SUPPORT_PRODUCT="Rocky Linux"
REDHAT_SUPPORT_PRODUCT_VERSION="9.6"
```

### Container Environment Versions

```bash
# Docker version requirements
docker -v
# Recommended: Docker version 26.1.3, build b72abbb

# Docker Compose version requirements
docker-compose -v
# Recommended: docker-compose version 1.29.1, build c34c88b2
```

### Recommended Installation Path

```bash
# Project deployment directory (customize as needed)
${SHANNON_BASE_DIR:-/data}

# Code path (relative to base directory)
${SHANNON_BASE_DIR:-/data}/Shannon/
```

## Dependencies Installation

### Development Environment One-Click Installation

```bash
# Development environment package installation (Rocky Linux)
# System basics
dnf update -y && dnf install -y curl wget tar unzip @development-tools

# Development environment
dnf install -y @development-tools gcc gcc-c++ make git go openssl-devel protobuf protobuf-c python3-pip

# Tool suite
dnf install -y net-tools telnet nmap nmap-ncat bind-utils tcpdump htop iotop lsof strace vim nano grep gawk sed tree jq
```

### gRPC Client Tool Installation

**grpcurl** is an essential debugging tool for the Shannon platform, used to test gRPC service interfaces:

Check if grpcurl is available:

```bash
# Check version
grpcurl --version

# Command not found
-bash: grpcurl: command not found
```

Download and install grpcurl:

```bash
# Project Information
# Repository: https://github.com/fullstorydev/grpcurl
# Language: Go
# Purpose: Command-line client for gRPC services (equivalent to curl for HTTP)

# Download latest version
wget https://github.com/fullstorydev/grpcurl/releases/download/v1.8.9/grpcurl_1.8.9_linux_x86_64.tar.gz

# Extract files
tar -xzf grpcurl_1.8.9_linux_x86_64.tar.gz

# Move to system path
mv grpcurl /usr/bin/

# Verify installation
grpcurl --version
# Output: grpcurl v1.8.9
```

### Protocol Buffers Compiler Installation

Check if protoc is available:

```bash
# Check version
protoc --version

# Command not found
-bash: protoc: command not found
```

Install Protocol Buffers compiler:

```bash
# Download official protoc binary
cd /tmp
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v24.4/protoc-24.4-linux-x86_64.zip

# Extract and install
unzip -o protoc-24.4-linux-x86_64.zip
mv bin/protoc /usr/local/bin/
chmod +x /usr/local/bin/protoc
ln -s /usr/local/bin/protoc /usr/bin/

# Copy include files
cp -r /tmp/include/google /usr/local/include/
```

## Project Deployment

### 1. Get Source Code

```bash
# Create and enter project directory
export SHANNON_BASE_DIR=${SHANNON_BASE_DIR:-/data}
sudo mkdir -p $SHANNON_BASE_DIR
cd $SHANNON_BASE_DIR

# Clone Shannon project repository
git clone https://github.com/Kocoro-lab/Shannon.git
cd $SHANNON_BASE_DIR/Shannon/

# Check current directory
pwd
# Expected output: $SHANNON_BASE_DIR/Shannon/
```

### 2. Environment Configuration

```bash
# Copy environment variables template
cp .env.example .env

# Edit configuration file
vi .env
```

**Required Configuration Items**:

```bash
# LLM service provider API keys (configure at least one)
OPENAI_API_KEY=sk-your-openai-key-here
ANTHROPIC_API_KEY=your-anthropic-key-here

# Optional: Search service API key
EXA_API_KEY=your-exa-key-here

# Optional: Web search configuration
GOOGLE_SEARCH_API_KEY=your-google-key
GOOGLE_SEARCH_ENGINE_ID=your-search-engine-id
```

Save and exit the editor.

### 3. Platform Initialization

```bash
# Step 1: Initialize project environment and basic configuration
make setup-env

# Step 2: Remote server specific setup (install tools + generate proto)
./scripts/setup-remote.sh

# Step 3: Generate cross-language gRPC code
make proto

# Step 4: Start the complete Shannon platform
make dev
```

**Service Startup Notes**:

- First startup may take 3-5 minutes (downloading Docker images)
- Platform will automatically start 8 docker instances
- Access addresses will be displayed after all services start

## Service Verification

### Check Service Status

```bash
# View all running containers
make ps

# Check service health status
curl http://localhost:8081/health  # Orchestrator service

# Run end-to-end test verification
make smoke
```

### Service Port Description

| Service | Port | Description |
|---------|------|-------------|
| Orchestrator | 50052 (gRPC), 8081 (HTTP) | Workflow orchestration service |
| LLM Service | 8000 | LLM provider interface service |
| Agent Core | 50051 (gRPC) | Agent core execution service |
| Temporal UI | 8088 | Workflow management interface |
| PostgreSQL | 5432 | Main database |
| Redis | 6379 | Cache service |
| Qdrant | 6333 | Vector database |

## Running Your First Task

### Submit Test Task

```bash
# Use task submission script
./scripts/submit_task.sh "Is the Earth round? How do you prove it? What evidence do you have?"
```

**Expected Output Example**:

```json
{
  "workflowId": "task-00000000-0000-0000-0000-000000000002-1758093116",
  "taskId": "task-00000000-0000-0000-0000-000000000002-1758093116",
  "status": "STATUS_CODE_OK",
  "decomposition": {
    "mode": "EXECUTION_MODE_STANDARD",
    "complexityScore": 0.5
  },
  "message": "Task submitted successfully. Session: 40d7e9b5-df1c-462b-9221-cd6df8e6318b"
}
```

### Task Status Monitoring

```bash
# Script will automatically poll task status
Polling status for task: task-00000000-0000-0000-0000-000000000002-1758093116
  attempt 1: status=TASK_STATUS_RUNNING
  attempt 2: status=TASK_STATUS_RUNNING
  ...
  attempt 10: status=TASK_STATUS_COMPLETED
Done.
```

Seeing `"Task submitted successfully"` indicates the task was submitted successfully and the system is processing it.

## Web Interface Access

### Temporal Workflow Management Interface

```bash
# Access URL
http://your-server-ip:8088
```

**Usage Instructions**:

1. After entering the homepage, click **"Workflows"**
2. View detailed execution flow and status of tasks
3. Supports workflow replay and debugging features

### Service Metrics Monitoring

```bash
# Orchestrator metrics
curl http://localhost:2112/metrics

# Agent core metrics
curl http://localhost:2113/metrics

# LLM service metrics
curl http://localhost:8000/metrics
```

## Common Management Commands

### Service Management

```bash
# Stop all services (preserve data)
make down

# Stop services and remove all data volumes
make clean
```

### Log Viewing

```bash
# View all service logs
make logs
```

## Troubleshooting

### Most Common Issues ⚠️

The following are the most frequently encountered problems during Rocky Linux deployment and their solutions:

### Common Issue Resolution

1. **Go Module Build Failure** ⚠️ Common

   **Error Symptoms**:

   ```bash
   => ERROR [orchestrator builder 7/7] RUN CGO_ENABLED=0 GOOS=linux go build...
   internal/server/session_service.go:14:5: module github.com/Kocoro-lab/Shannon@latest found 
   (v0.0.0-20250829105349-0a1cee10d781), but does not contain package 
   github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/session
   failed to solve: process "/bin/sh -c CGO_ENABLED=0 GOOS=linux go build..." 
   did not complete successfully: exit code: 1
   make: *** [Makefile:34: dev] Error 17
   ```

   **Solution**:

   ```bash
   # Regenerate protobuf files
   make proto
   
   # Then restart services
   make dev
   ```

2. **buf Command Not Found Error** ⚠️ Critical

   **Error Symptoms**:

   ```bash
   ./scripts/setup-remote.sh: line 35: buf: command not found
   ```

   **Root Cause**:
   - buf is installed to `/usr/local/bin/buf` but `/usr/local/bin` is not in PATH
   - Script uses incorrect proto file path checks

   **Solution**:

   ```bash
   # Fix buf command path in setup-remote.sh
   # Edit $SHANNON_BASE_DIR/Shannon/scripts/setup-remote.sh line 35
   # Change: buf generate
   # To: /usr/local/bin/buf generate

   # Or add to PATH temporarily
   export PATH="/usr/local/bin:$PATH"
   ./scripts/setup-remote.sh
   ```

3. **LLM Service Protobuf Module Missing** ⚠️ Critical

   **Error Symptoms**:

   ```bash
   ModuleNotFoundError: No module named 'llm_service.generated'
   ModuleNotFoundError: No module named 'common'
   grpcio-tools 1.68.1 incompatible with protobuf 6.x
   ```

   **Root Cause**:
   - Python service lacks protobuf files generated from `.proto` definitions
   - Rocky Linux dnf packages are incomplete
   - Version incompatibility between grpcio-tools and protobuf

   **Solution**:

   ```bash
   # Step 1: Install Protocol Buffers compiler manually
   cd /tmp
   curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v24.4/protoc-24.4-linux-x86_64.zip
   unzip -o protoc-24.4-linux-x86_64.zip
   mv bin/protoc /usr/local/bin/
   chmod +x /usr/local/bin/protoc
   cp -r /tmp/include/google /usr/local/include/

   # Step 2: Install compatible Python dependencies
   dnf install -y python3-pip
   pip3 install grpcio-tools==1.68.1

   # Step 3: Generate Python protobuf files
   cd $SHANNON_BASE_DIR/Shannon/protos
   mkdir -p ../python/llm-service/llm_service/generated/agent
   mkdir -p ../python/llm-service/llm_service/generated/common

   python3 -m grpc_tools.protoc \
       --python_out=../python/llm-service/llm_service/generated \
       --grpc_python_out=../python/llm-service/llm_service/generated \
       --pyi_out=../python/llm-service/llm_service/generated \
       -I . -I /usr/local/include \
       common/common.proto agent/agent.proto

   # Step 4: Create required __init__.py files
   touch ../python/llm-service/llm_service/generated/__init__.py
   touch ../python/llm-service/llm_service/generated/agent/__init__.py
   touch ../python/llm-service/llm_service/generated/common/__init__.py

   # Step 5: Fix import paths in generated files
   # Edit python/llm-service/llm_service/generated/agent/agent_pb2.py
   # Change: from common import common_pb2 as common_dot_common__pb2
   # To: from ..common import common_pb2 as common_dot_common__pb2

   # Step 6: Rebuild and restart
   cd $SHANNON_BASE_DIR/Shannon
   docker compose build llm-service
   docker compose up -d llm-service
   ```

4. **Smoke Test Keeps Waiting**

   **Error Symptoms**:

   ```bash
   make smoke
   Temporal UI reachable
   Temporal UI
   Agent-Core gRPC health
   healthAgentCore
   ExecuteTaskagent
   Wait for orchestrator gRPC (50052) to be ready
   waiting
   waiting
   # Waits multiple times, then errors out
   ```

   **Root Cause**: System lacks the `netcat` tool, which the smoke test script uses to check port connectivity

   **Solution**:

   ```bash
   # Install netcat tool (Rocky Linux uses nmap-ncat)
   sudo dnf install -y nmap-ncat
   
   # Re-run test
   make smoke
   ```

   **Successful Output Example**:

   ```bash
   [OK]  Temporal UI
   [OK]  Agent-Core health
   [OK]  Agent-Core ExecuteTask
   [OK]  Orchestrator gRPC ready
   [OK]  Orchestrator SubmitTask (reflection)
   [OK]  Orchestrator reached terminal status
   [OK]  Orchestrator persistence verified
   [OK]  Metrics endpoints reachable
   [OK]  LLM ready
   [OK]  LLM health endpoints
   [OK]  MCP tool registered
   [OK]  MCP tool executed
   [OK]  Qdrant collections present
   [OK]  Postgres query
   All smoke checks passed
   ```

5. **Service Startup Failure**

   ```bash
   # Check Docker status
   systemctl status docker
   
   # Restart Docker service (if needed)
   sudo systemctl restart docker
   ```

6. **Port Conflicts**

   ```bash
   # Check port usage
   netstat -tulpn | grep :8081
   
   # Stop conflicting process or modify configuration
   ```

7. **API Key Issues**

   ```bash
   # Verify environment variable loading
   docker compose config | grep -i api_key
   
   # Reconfigure .env file
   vi .env
   ```

8. **Insufficient Memory**

   ```bash
   # Check system resources
   free -h
   df -h
   
   # Clean Docker resources
   docker system prune -f
   ```

### Rocky Linux Specific Issues

1. **SELinux Permission Issues**

   ```bash
   # Check SELinux status
   getenforce
   
   # Temporarily disable SELinux (for debugging)
   sudo setenforce 0
   
   # Permanently disable SELinux (edit configuration file)
   sudo vi /etc/selinux/config
   # Change SELINUX=enforcing to SELINUX=disabled
   ```

2. **Firewall Port Configuration**

   ```bash
   # Open required Shannon ports
   sudo firewall-cmd --permanent --add-port=8081/tcp
   sudo firewall-cmd --permanent --add-port=8088/tcp
   sudo firewall-cmd --permanent --add-port=50051/tcp
   sudo firewall-cmd --permanent --add-port=50052/tcp
   
   # Reload firewall configuration
   sudo firewall-cmd --reload
   
   # List open ports
   sudo firewall-cmd --list-ports
   ```

### General Troubleshooting Workflow

#### 1. Diagnostic Steps

```bash
# Check all Shannon services
docker ps -a | grep shannon

# View container logs
docker logs <container-name> --tail 50

# Filter specific errors
docker logs <container-name> 2>&1 | grep -E "Error|Exception|Failed"

# Check port usage
ss -tulpn | grep -E "8000|8088|50051|50052"
```

#### 2. Quick Health Checks

```bash
# Check if proto files are generated
ls -la $SHANNON_BASE_DIR/Shannon/python/llm-service/llm_service/generated/

# Verify Python dependencies
pip3 list | grep -E "grpc|protobuf"

# Check Docker images
docker images | grep shannon

# Service health check
curl -s http://localhost:8000/health || echo "Service not responding"
curl -s http://localhost:8081/health || echo "Orchestrator not responding"
```

#### 3. Clean and Rebuild Process

```bash
# Clean old containers and images
docker compose down -v
docker system prune -f

# Regenerate proto files
cd $SHANNON_BASE_DIR/Shannon
make proto  # or use manual commands above

# Rebuild all services
docker compose build
docker compose up -d

# Run comprehensive test
make smoke
```


### Best Practices for Rocky Linux Deployment

#### 1. Pre-deployment Verification

```bash
# Verify system compatibility
grep -q "Rocky Linux" /etc/os-release && echo "Rocky Linux detected"

# Check required tools
for tool in docker python3 pip3 make curl; do
    command -v $tool >/dev/null 2>&1 || echo "Missing: $tool"
done

# Verify protoc installation
/usr/local/bin/protoc --version || echo "protoc not found"
```


---

**Note**: Ensure your Rocky Linux system has at least 8GB of memory and 20GB of available disk space for optimal performance.
