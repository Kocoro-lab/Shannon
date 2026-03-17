#!/bin/bash
set -e

# Shannon Quick Install Script
# Downloads production config and helps setup environment

SHANNON_VERSION="${SHANNON_VERSION:-v0.3.1}"
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
SYNTHESIS_TEMPLATES=(
    "_base.tmpl"
    "normal_default.tmpl"
    "research_comprehensive.tmpl"
    "research_concise.tmpl"
    "research_with_facts.tmpl"
    "domain_analysis_digest.tmpl"
    "swarm_default.tmpl"
    "test_bullet_summary.tmpl"
)
for tmpl in "${SYNTHESIS_TEMPLATES[@]}"; do
    curl -fsSL "${GITHUB_RAW}/config/templates/synthesis/${tmpl}" -o "config/templates/synthesis/${tmpl}"
done
echo "✅ Downloaded config files"

# Download workflow templates
echo "⬇️  Downloading workflow templates..."
mkdir -p config/workflows/examples config/workflows/user
WORKFLOW_TEMPLATES=(
    "complex_dag.yaml"
    "market_analysis_playbook.yaml"
    "market_analysis.yaml"
    "parallel_dag_example.yaml"
    "parallel_items_example.yaml"
    "research_summary_enterprise.yaml"
    "research_summary.yaml"
    "simple_analysis.yaml"
)
for wf in "${WORKFLOW_TEMPLATES[@]}"; do
    curl -fsSL "${GITHUB_RAW}/config/workflows/examples/${wf}" -o "config/workflows/examples/${wf}"
done
echo "✅ Downloaded workflow templates"

# Download skills config
echo "⬇️  Downloading skills configuration..."
mkdir -p config/skills/core
curl -fsSL "${GITHUB_RAW}/config/skills/README.md" -o config/skills/README.md
curl -fsSL "${GITHUB_RAW}/config/skills/core/code-review.md" -o config/skills/core/code-review.md
curl -fsSL "${GITHUB_RAW}/config/skills/core/debugging.md" -o config/skills/core/debugging.md
curl -fsSL "${GITHUB_RAW}/config/skills/core/test-driven-dev.md" -o config/skills/core/test-driven-dev.md
echo "✅ Downloaded skills config"

# Download Python WASM interpreter
echo "⬇️  Downloading Python WASM interpreter (~20MB)..."
mkdir -p wasm-interpreters
WASM_URL="https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm"
curl -fsSL "$WASM_URL" -o wasm-interpreters/python-3.11.4.wasm
echo "✅ Downloaded Python WASM interpreter"

# Download database migrations
echo "⬇️  Downloading database migrations..."
mkdir -p migrations/postgres migrations/qdrant
MIGRATION_FILES=(
    "001_initial_schema.sql"
    "002_persistence_tables.sql"
    "003_authentication.sql"
    "004_event_logs.sql"
    "005_alter_memory_system.sql"
    "006_supervisor_memory_tables.sql"
    "007_session_soft_delete.sql"
    "008_add_model_provider_to_tasks.sql"
    "009_scheduled_tasks.sql"
    "010_auth_user_link.sql"
    "010_session_context_indexes.sql"
    "011_add_agent_id_to_token_usage.sql"
    "112_add_cache_token_columns.sql"
    "113_channels.sql"
)
for file in "${MIGRATION_FILES[@]}"; do
    curl -fsSL "${GITHUB_RAW}/migrations/postgres/${file}" -o "migrations/postgres/${file}"
done
curl -fsSL "${GITHUB_RAW}/migrations/qdrant/create_collections.py" -o migrations/qdrant/create_collections.py
echo "✅ Downloaded database migrations"

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

# Ask if user wants to edit now (read from terminal, not pipe)
read -p "Would you like to edit .env now? (y/n) " -n 1 -r < /dev/tty
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    ${EDITOR:-nano} .env < /dev/tty
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
