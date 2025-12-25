#!/bin/bash
set -e

# Shannon Quick Install Script
# Downloads production config and helps setup environment

SHANNON_VERSION="${SHANNON_VERSION:-v0.1.0}"
GITHUB_RAW="https://raw.githubusercontent.com/Kocoro-lab/Shannon/${SHANNON_VERSION}"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Shannon AI Platform - Quick Installer"
echo "  Version: ${SHANNON_VERSION}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check prerequisites
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first:"
    echo "   https://docs.docker.com/get-docker/"
    exit 1
fi

if ! command -v docker compose &> /dev/null; then
    echo "❌ Docker Compose is not installed."
    exit 1
fi

echo "✅ Docker found: $(docker --version)"
echo ""

# Create installation directory
INSTALL_DIR="${INSTALL_DIR:-$HOME/shannon}"
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

echo "📦 Installing to: $INSTALL_DIR"
echo ""

# Download docker-compose.release.yml
echo "⬇️  Downloading docker-compose.release.yml..."
curl -fsSL "${GITHUB_RAW}/deploy/compose/docker-compose.release.yml" -o docker-compose.release.yml
echo "✅ Downloaded docker-compose.release.yml"

# Download grafana compose and config files (required by include directive)
echo "⬇️  Downloading Grafana/Prometheus config..."
mkdir -p grafana/config/provisioning/dashboards grafana/scripts grafana/data/prometheus-data grafana/data/grafana-data
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/docker-compose-grafana-prometheus.yml" -o grafana/docker-compose-grafana-prometheus.yml
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/config/prometheus.yml" -o grafana/config/prometheus.yml
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/config/grafana.ini" -o grafana/config/grafana.ini
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/scripts/import-dashboards.sh" -o grafana/scripts/import-dashboards.sh
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/config/provisioning/dashboards/node-exporter-1860.json" -o grafana/config/provisioning/dashboards/node-exporter-1860.json
curl -fsSL "${GITHUB_RAW}/deploy/compose/grafana/config/provisioning/dashboards/tier-drift-monitoring.json" -o grafana/config/provisioning/dashboards/tier-drift-monitoring.json
echo "✅ Downloaded Grafana config"

# Download .env.example
echo "⬇️  Downloading .env.example..."
curl -fsSL "${GITHUB_RAW}/.env.example" -o .env.example
echo "✅ Downloaded .env.example"

# Download config files
echo "⬇️  Downloading configuration files..."
mkdir -p config/templates/synthesis
curl -fsSL "${GITHUB_RAW}/config/features.yaml" -o config/features.yaml
curl -fsSL "${GITHUB_RAW}/config/models.yaml" -o config/models.yaml
curl -fsSL "${GITHUB_RAW}/config/research_strategies.yaml" -o config/research_strategies.yaml
curl -fsSL "${GITHUB_RAW}/config/templates/synthesis/_base.tmpl" -o config/templates/synthesis/_base.tmpl
curl -fsSL "${GITHUB_RAW}/config/templates/synthesis/normal_default.tmpl" -o config/templates/synthesis/normal_default.tmpl
curl -fsSL "${GITHUB_RAW}/config/templates/synthesis/research_comprehensive.tmpl" -o config/templates/synthesis/research_comprehensive.tmpl
curl -fsSL "${GITHUB_RAW}/config/templates/synthesis/research_concise.tmpl" -o config/templates/synthesis/research_concise.tmpl
echo "✅ Downloaded config files"

# Download Python WASM interpreter
echo "⬇️  Downloading Python WASM interpreter (~20MB)..."
mkdir -p wasm-interpreters
WASM_URL="https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm"
curl -fsSL "$WASM_URL" -o wasm-interpreters/python-3.11.4.wasm
echo "✅ Downloaded Python WASM interpreter"

# Create .env if it doesn't exist
if [ ! -f .env ]; then
    cp .env.example .env
    echo "✅ Created .env file"
else
    echo "⚠️  .env already exists, skipping"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Configuration Required"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Please configure your API keys in .env:"
echo ""
echo "Required (choose one):"
echo "  • OpenAI:    OPENAI_API_KEY=sk-..."
echo "  • Anthropic: ANTHROPIC_API_KEY=sk-ant-..."
echo ""
echo "Optional but recommended:"
echo "  • Web Search: SERPAPI_API_KEY=... (get key at serpapi.com)"
echo ""

# Ask if user wants to edit now
read -p "Would you like to edit .env now? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    ${EDITOR:-nano} .env
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Starting Shannon"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Pull images
echo "📥 Pulling Docker images..."
docker compose -f docker-compose.release.yml pull

# Start services
echo "🚀 Starting services..."
docker compose -f docker-compose.release.yml up -d

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ✅ Shannon is running!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Services:"
echo "  • Gateway API:  http://localhost:8080"
echo "  • Temporal UI:  http://localhost:8088"
echo "  • Grafana:      http://localhost:3030"
echo ""
echo "Quick test:"
echo '  curl -X POST http://localhost:8080/api/v1/tasks \'
echo '    -H "Content-Type: application/json" \'
echo '    -d '\''{"query": "What is 2+2?", "session_id": "test"}'\'''
echo ""
echo "Manage services:"
echo "  cd $INSTALL_DIR"
echo "  docker compose -f docker-compose.release.yml ps      # Check status"
echo "  docker compose -f docker-compose.release.yml logs -f # View logs"
echo "  docker compose -f docker-compose.release.yml down    # Stop services"
echo ""
echo "Documentation: https://docs.shannon.run"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
