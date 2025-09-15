#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/compose.yml}"
SEED_FILE="${SEED_FILE:-tests/fixtures/seed_data.sql}"
PGUSER_ENV="${POSTGRES_USER:-shannon}"

if [ ! -f "$SEED_FILE" ]; then
  echo "No seed file at $SEED_FILE (skipping)"
  exit 0
fi

echo "Seeding Postgres with $SEED_FILE..."
docker compose -f "$COMPOSE_FILE" cp "$SEED_FILE" postgres:/seed_data.sql
docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U "$PGUSER_ENV" -d "$POSTGRES_DB" -f /seed_data.sql
echo "Postgres seed complete."
