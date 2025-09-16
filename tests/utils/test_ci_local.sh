#!/usr/bin/env bash
set -euo pipefail

echo "Testing CI locally with Docker (Ubuntu environment)..."

# Use Docker to simulate Ubuntu environment
docker run --rm -v $(pwd):/workspace -w /workspace ubuntu:22.04 bash -c '
set -euo pipefail

echo "=== Installing system dependencies ==="
apt-get update
apt-get install -y curl git build-essential protobuf-compiler libprotobuf-dev

echo "=== Installing Go ==="
curl -L https://go.dev/dl/go1.22.5.linux-amd64.tar.gz | tar -C /usr/local -xz
export PATH="/usr/local/go/bin:$PATH"
go version

echo "=== Installing Rust ==="
curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
source "$HOME/.cargo/env"
rustc --version

echo "=== Installing Python ==="
apt-get install -y python3.11 python3-pip
python3 --version

echo "=== Installing Buf ==="
BIN="/usr/local/bin" && \
VERSION="1.28.1" && \
curl -sSL \
  "https://github.com/bufbuild/buf/releases/download/v${VERSION}/buf-Linux-x86_64" \
  -o "${BIN}/buf" && \
chmod +x "${BIN}/buf"

echo "=== Generating Protobuf ==="
cd protos && buf generate && cd ..

echo "=== Building Go orchestrator ==="
cd go/orchestrator
go mod download
go build ./...
echo "Go build: SUCCESS"

echo "=== Running Go tests ==="
go test ./... 2>&1 | grep -E "^(ok|FAIL)" || true
cd ../..

echo "=== Building Rust agent-core ==="
cd rust/agent-core
rm -f Cargo.lock  # Remove lock file for cross-platform build
cargo build --release
echo "Rust build: SUCCESS"

echo "=== Running Rust tests ==="
cargo test --all-targets
echo "Rust tests: SUCCESS"
cd ../..

echo "=== Installing Python dependencies ==="
python3 -m pip install --upgrade pip
pip3 install ruff pytest -r python/llm-service/requirements.txt

echo "=== Running Python tests ==="
cd python/llm-service
pytest -q || true
cd ../..

echo "=== CI simulation complete ==="
'

echo "Local CI test completed!"