#!/usr/bin/env bash
set -euo pipefail

# Shannon Quick Installer
# Downloads pre-built Docker images and config, then starts all services.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/Shannon/main/scripts/install.sh | bash
#   curl -fsSL ... | SHANNON_VERSION=v0.5.1 bash

SHANNON_VERSION="${SHANNON_VERSION:-v0.5.1}"
REPO_RAW="https://raw.githubusercontent.com/Kocoro-lab/Shannon/${SHANNON_VERSION}"
INSTALL_DIR="${INSTALL_DIR:-shannon}"

info()  { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33mWARN:\033[0m %s\n" "$1"; }
error() { printf "\033[1;31mERROR:\033[0m %s\n" "$1" >&2; exit 1; }

# --- Preflight checks ---
command -v docker  >/dev/null 2>&1 || error "docker is required but not installed."
command -v curl    >/dev/null 2>&1 || error "curl is required but not installed."

if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  error "docker compose (v2) or docker-compose is required."
fi

info "Installing Shannon ${SHANNON_VERSION} into ./${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"
cd "${INSTALL_DIR}"

# --- Helper: download a file if it doesn't already exist ---
dl() {
  local dest="$1" url="$2"
  mkdir -p "$(dirname "$dest")"
  if ! curl -fsSL -o "$dest" "$url"; then
    warn "Failed to download ${url} — skipping"
    return 1
  fi
}

# --- Docker Compose file ---
info "Downloading docker-compose.release.yml"
dl docker-compose.yml "${REPO_RAW}/deploy/compose/docker-compose.release.yml"

# --- Environment template ---
info "Downloading .env.example"
dl .env.example "${REPO_RAW}/.env.example"

# --- Config files ---
info "Downloading config files"
CONFIG_FILES=(
  features.yaml
  models.yaml
  research_strategies.yaml
  shannon.yaml
  rate_limits.yaml
)
for f in "${CONFIG_FILES[@]}"; do
  dl "config/${f}" "${REPO_RAW}/config/${f}"
done

# --- Synthesis templates ---
info "Downloading synthesis templates"
TEMPLATES=(
  _base.tmpl
  normal_default.tmpl
  research_comprehensive.tmpl
  research_concise.tmpl
  research_with_facts.tmpl
  swarm_default.tmpl
)
for f in "${TEMPLATES[@]}"; do
  dl "config/templates/synthesis/${f}" "${REPO_RAW}/config/templates/synthesis/${f}"
done

# --- Workflow examples ---
info "Downloading workflow examples"
WORKFLOWS=(
  complex_dag.yaml
  market_analysis.yaml
  market_analysis_playbook.yaml
  parallel_dag_example.yaml
  parallel_items_example.yaml
  research_summary.yaml
  simple_analysis.yaml
)
for f in "${WORKFLOWS[@]}"; do
  dl "config/workflows/examples/${f}" "${REPO_RAW}/config/workflows/examples/${f}"
done

# --- Skills ---
info "Downloading skills"
# Download the skills directory index, then fetch core skills
dl "config/skills/README.md" "${REPO_RAW}/config/skills/README.md"
CORE_SKILLS=(
  code-review.md
  debugging.md
  test-driven-dev.md
)
for f in "${CORE_SKILLS[@]}"; do
  dl "config/skills/core/${f}" "${REPO_RAW}/config/skills/core/${f}" || true
done

# --- WASM interpreter ---
info "Downloading WASM Python interpreter (~20MB, may take a moment)"
WASM_URL="https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python%2F3.11.4%2B20230714-11be424/python-3.11.4.wasm"
dl "wasm-interpreters/python-3.11.4.wasm" "$WASM_URL"

# --- Database migrations ---
info "Downloading database migrations"
MIGRATIONS=(
  001_initial_schema.sql
  002_persistence_tables.sql
  003_authentication.sql
  004_event_logs.sql
  005_alter_memory_system.sql
  006_supervisor_memory_tables.sql
  007_session_soft_delete.sql
  008_add_model_provider_to_tasks.sql
  009_scheduled_tasks.sql
  010_auth_user_link.sql
  010_session_context_indexes.sql
  011_add_agent_id_to_token_usage.sql
  012_workspace_quotas.sql
  112_add_cache_token_columns.sql
  118_performance_indexes.sql
  120_token_usage_call_sequence.sql
)
for f in "${MIGRATIONS[@]}"; do
  dl "migrations/postgres/${f}" "${REPO_RAW}/migrations/postgres/${f}"
done

# --- Create .env from template ---
if [ ! -f .env ]; then
  info "Creating .env from .env.example"
  cp .env.example .env
  warn "Edit .env to add your API keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.)"
else
  info ".env already exists — skipping"
fi

# --- Pull images ---
info "Pulling Docker images (SHANNON_VERSION=${SHANNON_VERSION})"
export SHANNON_VERSION
${COMPOSE} pull

# --- Start services ---
info "Starting Shannon services"
${COMPOSE} up -d

# --- Wait for gateway health ---
info "Waiting for gateway to become healthy..."
for i in $(seq 1 30); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
  printf "\n"
  info "Shannon ${SHANNON_VERSION} is running!"
  printf "\n"
  printf "  Gateway API:   http://localhost:8080\n"
  printf "  Temporal UI:   http://localhost:8088\n"
  printf "  LLM Service:   http://localhost:8000\n"
  printf "\n"
  printf "  Quick test:\n"
  printf "    curl -sS -X POST http://localhost:8080/api/v1/tasks \\\\\n"
  printf "      -H 'Content-Type: application/json' \\\\\n"
  printf "      -d '{\"query\":\"What is 2+2?\",\"session_id\":\"test-1\"}'\n"
  printf "\n"
  info "To stop: cd ${INSTALL_DIR} && ${COMPOSE} down"
else
  warn "Gateway not yet healthy — services may still be starting."
  warn "Check status with: cd ${INSTALL_DIR} && ${COMPOSE} ps"
fi
