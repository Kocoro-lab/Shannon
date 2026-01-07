#!/usr/bin/env bash
# Generate protobuf files locally without BSR dependencies
# IMPORTANT: This script ensures version compatibility across all services

set -euo pipefail

cd "$(dirname "$0")/.."

# Use Python 3.12 if available (3.14 has compatibility issues with grpcio-tools)
if command -v python3.12 &> /dev/null; then
    PYTHON_CMD="python3.12"
elif command -v python3.13 &> /dev/null; then
    PYTHON_CMD="python3.13"
elif command -v python3.11 &> /dev/null; then
    PYTHON_CMD="python3.11"
else
    PYTHON_CMD="python3"
fi
echo "Using Python: $($PYTHON_CMD --version)"

# Add Go bin to PATH
export PATH="$HOME/go/bin:$PATH"

echo "Generating protobuf files locally..."
echo "Note: Using language-specific protoc to ensure compatibility"

# For Go - protoc is compatible across versions for Go
if command -v protoc &> /dev/null; then
    echo "System protoc found: $(protoc --version)"
else
    echo "Warning: protoc is not installed for Go generation."
    echo "On macOS: brew install protobuf"
    echo "On Ubuntu: apt-get install protobuf-compiler"
fi

# Ensure Go protoc plugins are installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing Go protoc plugins..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Ensure Python grpc tools are installed with correct protobuf version
if ! $PYTHON_CMD -c "import grpc_tools" 2>/dev/null; then
    echo "Installing Python gRPC tools with protobuf 5.29.2..."
    $PYTHON_CMD -m pip install --break-system-packages grpcio-tools==1.68.1 protobuf==5.29.2
else
    # Check protobuf version
    PROTOBUF_VERSION=$($PYTHON_CMD -c "import google.protobuf; print(google.protobuf.__version__)" 2>/dev/null || echo "unknown")
    if [[ ! "$PROTOBUF_VERSION" =~ ^5\. ]]; then
        echo "Warning: Protobuf version $PROTOBUF_VERSION detected, expected 5.x"
        echo "Installing correct version..."
        $PYTHON_CMD -m pip install --break-system-packages --upgrade protobuf==5.29.2 grpcio-tools==1.68.1
    fi
fi

cd protos

# Generate Go files (Go is flexible with protoc versions)
if command -v protoc &> /dev/null; then
    echo "Generating Go protobuf files with system protoc..."
    mkdir -p ../go/orchestrator/internal/pb
    for proto in $(find . -name "*.proto" -type f); do
        protoc \
            --go_out=../go/orchestrator/internal/pb \
            --go_opt=paths=source_relative \
            --go-grpc_out=../go/orchestrator/internal/pb \
            --go-grpc_opt=paths=source_relative \
            --go-grpc_opt=require_unimplemented_servers=false \
            -I . \
            "$proto"
    done
else
    echo "Skipping Go generation - protoc not installed"
fi

# Generate Python files using grpc_tools.protoc (ensures correct version)
echo "Generating Python protobuf files with grpc_tools.protoc (protobuf 5.x)..."
mkdir -p gen/python
# Use Python's bundled protoc to ensure version compatibility
$PYTHON_CMD -m grpc_tools.protoc \
    --python_out=gen/python \
    --grpc_python_out=gen/python \
    --pyi_out=gen/python \
    -I . \
    $(find . -name "*.proto" -type f)

# Copy Python files to llm-service
echo "Copying Python protobuf files to llm-service..."
mkdir -p ../python/llm-service/llm_service/grpc_gen
cp -r gen/python/* ../python/llm-service/llm_service/grpc_gen/ 2>/dev/null || true

echo "Protobuf generation complete!"