#!/bin/bash
# ============================================================================
# Shannon Local Mode Runner
# ============================================================================
# Starts Shannon in embedded mode with no external dependencies.
# Perfect for local development and desktop applications.
#
# Usage:
#   ./scripts/run-local.sh
#
# Prerequisites:
#   - Rust toolchain installed
#   - At least one LLM API key (ANTHROPIC_API_KEY or OPENAI_API_KEY)
# ============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}============================================${NC}"
echo -e "${BLUE}Shannon Local Mode${NC}"
echo -e "${BLUE}============================================${NC}"
echo ""

# Check for API key
if [ -z "$ANTHROPIC_API_KEY" ] && [ -z "$OPENAI_API_KEY" ] && \
   [ -z "$GOOGLE_API_KEY" ] && [ -z "$GROQ_API_KEY" ] && [ -z "$XAI_API_KEY" ]; then
    echo -e "${RED}ERROR: No LLM API key found${NC}"
    echo ""
    echo "Please set at least one of the following environment variables:"
    echo -e "  ${YELLOW}export ANTHROPIC_API_KEY=sk-ant-...${NC}  (recommended)"
    echo -e "  ${YELLOW}export OPENAI_API_KEY=sk-...${NC}"
    echo -e "  ${YELLOW}export GOOGLE_API_KEY=...${NC}"
    echo -e "  ${YELLOW}export GROQ_API_KEY=...${NC}"
    echo -e "  ${YELLOW}export XAI_API_KEY=...${NC}"
    echo ""
    echo "Or source the local environment file:"
    echo -e "  ${YELLOW}source config/env/local.env${NC}"
    exit 1
fi

# Get script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Create data directory
DATA_DIR="${PROJECT_ROOT}/data"
mkdir -p "$DATA_DIR"

# Set environment variables for embedded mode
export SHANNON_MODE=embedded
export WORKFLOW_ENGINE=durable
export DATABASE_DRIVER=surrealdb
export SURREALDB_PATH="${DATA_DIR}/shannon.db"
export SHANNON_HOST="${SHANNON_HOST:-127.0.0.1}"
export SHANNON_PORT="${SHANNON_PORT:-8765}"
export RUST_LOG="${RUST_LOG:-info,shannon_api=debug}"

echo -e "${GREEN}Configuration:${NC}"
echo "  Mode:       $SHANNON_MODE"
echo "  Workflow:   $WORKFLOW_ENGINE"
echo "  Database:   $DATABASE_DRIVER"
echo "  Data path:  $SURREALDB_PATH"
echo ""
echo -e "${GREEN}LLM Providers:${NC}"
[ -n "$ANTHROPIC_API_KEY" ] && echo "  ✓ Anthropic (Claude)"
[ -n "$OPENAI_API_KEY" ] && echo "  ✓ OpenAI (GPT)"
[ -n "$GOOGLE_API_KEY" ] && echo "  ✓ Google (Gemini)"
[ -n "$GROQ_API_KEY" ] && echo "  ✓ Groq"
[ -n "$XAI_API_KEY" ] && echo "  ✓ xAI (Grok)"
echo ""
echo -e "${GREEN}Starting server at http://${SHANNON_HOST}:${SHANNON_PORT}${NC}"
echo ""

# Change to project root
cd "$PROJECT_ROOT"

# Build and run
echo -e "${YELLOW}Building Shannon API with embedded features...${NC}"
cargo build -p shannon-api --no-default-features --features "embedded,gateway"

echo ""
echo -e "${GREEN}Starting Shannon API...${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop${NC}"
echo ""

cargo run -p shannon-api --no-default-features --features "embedded,gateway"
