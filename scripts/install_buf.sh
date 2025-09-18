#!/bin/bash
# Install buf for protobuf management

set -e

echo "Installing buf..."

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    ARCH="x86_64"
elif [ "$ARCH" = "arm64" ] || [ "$ARCH" = "aarch64" ]; then
    ARCH="arm64"
fi

# Download buf
BUF_VERSION="1.28.1"
BUF_URL="https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-${OS}-${ARCH}"

echo "Downloading buf from ${BUF_URL}..."
curl -sSL "${BUF_URL}" -o /tmp/buf
chmod +x /tmp/buf

# Check if user has permission to install to /usr/local/bin
if [ -w /usr/local/bin ]; then
    mv /tmp/buf /usr/local/bin/buf
    echo "buf installed to /usr/local/bin/buf"
else
    mkdir -p "$HOME/.local/bin"
    mv /tmp/buf "$HOME/.local/bin/buf"
    echo "buf installed to $HOME/.local/bin/buf"
    echo "Make sure $HOME/.local/bin is in your PATH"
fi

echo "buf installation complete!"
export PATH="$HOME/.local/bin:$PATH"
"$HOME/.local/bin/buf" --version || true
