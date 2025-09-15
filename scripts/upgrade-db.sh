#!/bin/bash
# Database upgrade script for existing Shannon installations
# Run this after pulling new code with schema changes

echo "Shannon Database Upgrade Script"
echo "================================"

# Check if postgres container is running
if ! docker ps | grep -q shannon-postgres-1; then
    echo "Error: PostgreSQL container is not running"
    echo "Please run 'make dev' first"
    exit 1
fi

echo "Applying database migrations..."

# Apply all migration files in order
for migration in migrations/postgres/*.sql; do
    filename=$(basename "$migration")
    echo "Applying $filename..."
    docker exec shannon-postgres-1 psql -U shannon -d shannon -f "/docker-entrypoint-initdb.d/$filename" 2>&1 | grep -E "CREATE|ALTER|INSERT" | head -5
done

echo ""
echo "Verifying schema..."

# Check for tenant_id columns
echo "Checking tenant_id columns:"
docker exec shannon-postgres-1 psql -U shannon -d shannon -c "\d+ sessions" 2>&1 | grep tenant_id > /dev/null && echo "✓ sessions.tenant_id exists" || echo "✗ sessions.tenant_id missing"
docker exec shannon-postgres-1 psql -U shannon -d shannon -c "\d+ tasks" 2>&1 | grep tenant_id > /dev/null && echo "✓ tasks.tenant_id exists" || echo "✗ tasks.tenant_id missing"

# Check for auth tables
echo ""
echo "Checking auth tables:"
docker exec shannon-postgres-1 psql -U shannon -d shannon -c "\dt auth.*" 2>&1 | grep -c "auth\." | xargs -I {} echo "✓ {} auth tables found"

echo ""
echo "Database upgrade complete!"
echo ""
echo "Note: If you had authentication enabled, you may need to restart the orchestrator:"
echo "  docker compose -f deploy/compose/compose.yml restart orchestrator"