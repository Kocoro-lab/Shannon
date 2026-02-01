#!/usr/bin/env bash
# Generate protobuf files locally without BSR dependencies
# IMPORTANT: This script ensures version compatibility across all services

set -euo pipefail

cd "$(dirname "$0")/.."

# Add Go bin to PATH
export PATH="$HOME/go/bin:$PATH"

echo "Generating protobuf files locally..."
echo "Note: Using language-specific protoc to ensure compatibility"

# Check for protoc - REQUIRED for Go proto generation
if ! command -v protoc &> /dev/null; then
    echo ""
    echo "=========================================="
    echo "ERROR: protoc (Protocol Buffer compiler) is not installed!"
    echo "=========================================="
    echo ""
    echo "Go protobuf files cannot be generated without protoc."
    echo "This can cause Docker builds to fail."
    echo ""
    echo "Please install protoc:"
    echo "  macOS:   brew install protobuf"
    echo "  Ubuntu:  sudo apt-get install -y protobuf-compiler"
    echo "  Fedora:  sudo dnf install -y protobuf-compiler"
    echo "  Arch:    sudo pacman -S protobuf"
    echo ""
    echo "After installing, run: make proto-local"
    echo ""
    exit 1
fi
echo "✅ System protoc found: $(protoc --version)"

# Ensure Go protoc plugins are installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing Go protoc plugins..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Ensure Python grpc tools are installed with correct protobuf version
if ! python3 -c "import grpc_tools" 2>/dev/null; then
    echo "Installing Python gRPC tools with protobuf 5.29.2..."
    pip3 install grpcio-tools==1.68.1 protobuf==5.29.2
else
    # Check protobuf version
    PROTOBUF_VERSION=$(python3 -c "import google.protobuf; print(google.protobuf.__version__)" 2>/dev/null || echo "unknown")
    if [[ ! "$PROTOBUF_VERSION" =~ ^5\. ]]; then
        echo "Warning: Protobuf version $PROTOBUF_VERSION detected, expected 5.x"
        echo "Installing correct version..."
        pip3 install --upgrade protobuf==5.29.2 grpcio-tools==1.68.1
    fi
fi

cd protos

# Generate Go files
echo "Generating Go protobuf files..."
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
echo "✅ Go protobuf files generated"

# Generate Python files using grpc_tools.protoc (ensures correct version)
echo "Generating Python protobuf files with grpc_tools.protoc (protobuf 5.x)..."
mkdir -p gen/python
# Use Python's bundled protoc to ensure version compatibility
python3 -m grpc_tools.protoc \
    --python_out=gen/python \
    --grpc_python_out=gen/python \
    --pyi_out=gen/python \
    -I . \
    $(find . -name "*.proto" -type f)

# Copy Python files to llm-service
mkdir -p ../python/llm-service/llm_service/grpc_gen
cp -r gen/python/* ../python/llm-service/llm_service/grpc_gen/ 2>/dev/null || true
echo "✅ Python protobuf files generated"

echo ""
echo "✅ Protobuf generation complete!"
echo "   - Go:     go/orchestrator/internal/pb/"
echo "   - Python: python/llm-service/llm_service/grpc_gen/"
