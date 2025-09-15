#!/bin/bash
# Script to run docker-compose with environment variables from .env file

# Change to the project root directory
cd "$(dirname "$0")/.." || exit 1

# Check if .env file exists
if [ ! -f .env ]; then
    echo "Error: .env file not found in project root"
    exit 1
fi

# Export all variables from .env file
set -a
source .env
set +a

# Run docker-compose with all arguments passed to this script
docker compose -f deploy/compose/compose.yml "$@"