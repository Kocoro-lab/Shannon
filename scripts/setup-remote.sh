#!/bin/bash
# Setup script for remote Ubuntu server installation of Shannon stack

set -e

echo "=== Shannon Remote Setup Script ==="
echo ""

# Check if buf is installed
if ! command -v buf &> /dev/null; then
    echo "Installing buf..."
    # Install buf
    curl -sSL https://github.com/bufbuild/buf/releases/latest/download/buf-Linux-x86_64 -o /tmp/buf
    sudo mv /tmp/buf /usr/local/bin/buf
    sudo chmod +x /usr/local/bin/buf
fi

# Check if protoc-gen-go is installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Add Go bin to PATH if not already there
if [[ ":$PATH:" != *":$HOME/go/bin:"* ]]; then
    echo "Adding Go bin to PATH..."
    echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc
    export PATH=$PATH:$HOME/go/bin
fi

# Generate proto files
echo "Generating proto files..."
cd protos
buf generate
cd ..

echo "Proto files generated successfully!"

# Check if proto files were generated
if [ ! -d "go/orchestrator/internal/pb" ]; then
    echo "ERROR: Proto generation failed - go/orchestrator/internal/pb not found"
    exit 1
fi

if [ ! -d "rust/agent-core/src/pb" ]; then
    echo "ERROR: Proto generation failed - rust/agent-core/src/pb not found"
    exit 1
fi

if [ ! -d "python/llm-service/llm_service/pb" ]; then
    echo "ERROR: Proto generation failed - python/llm-service/llm_service/pb not found"
    exit 1
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Proto files have been generated successfully."
echo "You can now run 'make dev' to start the Shannon stack."
echo ""
echo "If you haven't set up your environment variables yet:"
echo "1. Run: make setup-env"
echo "2. Edit .env file with your API keys"
echo ""