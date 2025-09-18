#!/usr/bin/env bash
# Generate protobuf files locally without BSR dependencies

set -euo pipefail

cd "$(dirname "$0")/.."

# Add Go bin to PATH
export PATH="$HOME/go/bin:$PATH"

echo "Generating protobuf files locally..."

# Ensure protoc is installed
if ! command -v protoc &> /dev/null; then
    echo "Error: protoc is not installed. Please install protobuf compiler."
    echo "On macOS: brew install protobuf"
    echo "On Ubuntu: apt-get install protobuf-compiler"
    exit 1
fi

# Ensure Go protoc plugins are installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing Go protoc plugins..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Ensure Python grpc tools are installed
if ! python3 -c "import grpc_tools" 2>/dev/null; then
    echo "Installing Python gRPC tools..."
    pip3 install grpcio-tools
fi

cd protos

# Generate Go files
echo "Generating Go protobuf files..."
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

# Generate Python files
echo "Generating Python protobuf files..."
mkdir -p gen/python
python3 -m grpc_tools.protoc \
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