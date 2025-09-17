#!/bin/bash

# Setup Python WASI Interpreter for Shannon Platform
# This script downloads and configures the Python WASI interpreter for sandboxed execution

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
WASM_DIR="wasm-interpreters"
PYTHON_VERSION="3.11.4"
PYTHON_WASM_URL="https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm"
PYTHON_WASM_FILE="python-${PYTHON_VERSION}.wasm"
RUSTPYTHON_URL="https://github.com/RustPython/RustPython/releases/latest/download/rustpython.wasm"

echo "=========================================="
echo "Shannon Python WASI Setup"
echo "=========================================="
echo ""

# Check if running from project root
if [ ! -f "Makefile" ] || [ ! -d "rust/agent-core" ]; then
    echo -e "${RED}Error: Please run this script from the Shannon project root directory${NC}"
    exit 1
fi

# Create wasm-interpreters directory
echo -e "${YELLOW}Creating WASM interpreters directory...${NC}"
mkdir -p "$WASM_DIR"

# Function to download file with progress
download_with_progress() {
    local url=$1
    local output=$2
    local name=$3

    echo -e "${YELLOW}Downloading ${name}...${NC}"
    if command -v curl &> /dev/null; then
        curl -L --progress-bar -o "$output" "$url"
    elif command -v wget &> /dev/null; then
        wget --show-progress -O "$output" "$url"
    else
        echo -e "${RED}Error: Neither curl nor wget found. Please install one.${NC}"
        exit 1
    fi
}

# Download Python WASI interpreter
if [ -f "$WASM_DIR/$PYTHON_WASM_FILE" ]; then
    echo -e "${GREEN}Python WASI interpreter already exists at $WASM_DIR/$PYTHON_WASM_FILE${NC}"
    echo "File size: $(ls -lh "$WASM_DIR/$PYTHON_WASM_FILE" | awk '{print $5}')"
    read -p "Do you want to re-download? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Keeping existing file."
    else
        download_with_progress "$PYTHON_WASM_URL" "$WASM_DIR/$PYTHON_WASM_FILE" "Python $PYTHON_VERSION WASI"
    fi
else
    download_with_progress "$PYTHON_WASM_URL" "$WASM_DIR/$PYTHON_WASM_FILE" "Python $PYTHON_VERSION WASI"
fi

# Verify download
if [ ! -f "$WASM_DIR/$PYTHON_WASM_FILE" ]; then
    echo -e "${RED}Error: Failed to download Python WASI interpreter${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Python WASI interpreter downloaded successfully${NC}"
echo "  Location: $WASM_DIR/$PYTHON_WASM_FILE"
echo "  Size: $(ls -lh "$WASM_DIR/$PYTHON_WASM_FILE" | awk '{print $5}')"

# Optional: Download RustPython (lightweight alternative)
echo ""
read -p "Do you also want to download RustPython (lightweight alternative)? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    download_with_progress "$RUSTPYTHON_URL" "$WASM_DIR/rustpython.wasm" "RustPython WASI"
    echo -e "${GREEN}✓ RustPython downloaded successfully${NC}"
fi

# Check if .env file exists
echo ""
echo -e "${YELLOW}Checking environment configuration...${NC}"

if [ -f ".env" ]; then
    # Check if PYTHON_WASI_WASM_PATH is already set
    if grep -q "^PYTHON_WASI_WASM_PATH=" .env; then
        echo -e "${GREEN}✓ PYTHON_WASI_WASM_PATH already configured in .env${NC}"
        current_path=$(grep "^PYTHON_WASI_WASM_PATH=" .env | cut -d'=' -f2)
        echo "  Current path: $current_path"

        read -p "Do you want to update it? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            # Update existing path
            if [[ "$OSTYPE" == "darwin"* ]]; then
                sed -i '' "s|^PYTHON_WASI_WASM_PATH=.*|PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/$PYTHON_WASM_FILE|" .env
            else
                sed -i "s|^PYTHON_WASI_WASM_PATH=.*|PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/$PYTHON_WASM_FILE|" .env
            fi
            echo -e "${GREEN}✓ Updated PYTHON_WASI_WASM_PATH in .env${NC}"
        fi
    else
        # Add the configuration
        echo "" >> .env
        echo "# Python WASI interpreter path" >> .env
        echo "PYTHON_WASI_WASM_PATH=/opt/wasm-interpreters/$PYTHON_WASM_FILE" >> .env
        echo -e "${GREEN}✓ Added PYTHON_WASI_WASM_PATH to .env${NC}"
    fi
else
    echo -e "${YELLOW}No .env file found. Creating from .env.example...${NC}"
    cp .env.example .env
    echo -e "${GREEN}✓ Created .env file${NC}"
    echo -e "${YELLOW}Please add your API keys to .env before starting services${NC}"
fi

# Test the WASM file if wasmtime is available
echo ""
if command -v wasmtime &> /dev/null; then
    echo -e "${YELLOW}Testing Python WASI interpreter...${NC}"
    echo 'print("Hello from Python WASI!")' | wasmtime run \
        --dir=/tmp \
        "$WASM_DIR/$PYTHON_WASM_FILE" -- -c 'import sys; exec(sys.stdin.read())' 2>/dev/null && \
        echo -e "${GREEN}✓ Python WASI interpreter test successful${NC}" || \
        echo -e "${YELLOW}Note: Basic test failed, but this is expected without proper WASI environment${NC}"
elif command -v wat2wasm &> /dev/null; then
    echo -e "${GREEN}✓ WebAssembly tools detected (wat2wasm)${NC}"
else
    echo -e "${YELLOW}Note: Install wasmtime or wabt for local testing${NC}"
    echo "  macOS: brew install wasmtime wabt"
    echo "  Linux: sudo apt-get install wabt"
fi

# Docker compose check
echo ""
if [ -f "deploy/compose/compose.yml" ]; then
    echo -e "${YELLOW}Checking Docker Compose configuration...${NC}"

    # Check if volumes are configured
    if grep -q "wasm-interpreters:/opt/wasm-interpreters" deploy/compose/compose.yml; then
        echo -e "${GREEN}✓ Docker Compose already configured for WASI interpreters${NC}"
    else
        echo -e "${YELLOW}Note: Docker Compose needs to be updated to mount WASI interpreters${NC}"
        echo "  The services will automatically mount ./wasm-interpreters:/opt/wasm-interpreters"
    fi
fi

# Final instructions
echo ""
echo "=========================================="
echo -e "${GREEN}Setup Complete!${NC}"
echo "=========================================="
echo ""
echo "Next steps:"
echo "1. Ensure your .env file has the required API keys"
echo "2. Restart Shannon services:"
echo "   make down && make dev"
echo "3. Test Python execution:"
echo "   ./scripts/submit_task.sh \"Execute Python: print('Hello World')\""
echo ""
echo "For more information, see:"
echo "  - docs/python-wasi-setup.md"
echo "  - docs/python-code-execution.md"
echo ""
echo "Python WASI interpreter location:"
echo "  Local: $(pwd)/$WASM_DIR/$PYTHON_WASM_FILE"
echo "  Docker: /opt/wasm-interpreters/$PYTHON_WASM_FILE"