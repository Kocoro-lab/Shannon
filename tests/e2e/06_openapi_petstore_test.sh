#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.yml}"

pass() { echo -e "[OK]  $1"; }
fail() { echo -e "[ERR] $1"; exit 1; }
info() { echo -e "[..]  $1"; }

info "OpenAPI (PetStore) E2E Test Starting"
echo "Prereqs: llm-service running, OPENAPI_ALLOWED_DOMAINS allows petstore host"

# Quick health check for llm-service
for i in $(seq 1 30); do
  if nc -z localhost 8000 2>/dev/null; then
    pass "llm-service listening on :8000"
    break
  fi
  sleep 1
  if [ "$i" -eq 30 ]; then fail "llm-service not reachable on :8000"; fi
done

PETSTORE_SPEC_URL=${PETSTORE_SPEC_URL:-"https://petstore3.swagger.io/api/v3/openapi.json"}

echo ""
echo "=== Step 1: Validate OpenAPI spec ==="
VALIDATE_JSON=$(curl -fsS -X POST http://localhost:8000/tools/openapi/validate \
  -H 'Content-Type: application/json' \
  -d '{"spec_url": "'"$PETSTORE_SPEC_URL"'"}' 2>/dev/null || echo '{"valid":false}')

if echo "$VALIDATE_JSON" | jq -e '.valid == true' >/dev/null 2>&1; then
  COUNT=$(echo "$VALIDATE_JSON" | jq -r '.operations_count')
  pass "Spec validated. Operations: $COUNT"
else
  echo "$VALIDATE_JSON" | jq . || true
  fail "OpenAPI spec validation failed"
fi

# Prefer a safe GET list operation if available
OPERATION=$(echo "$VALIDATE_JSON" | jq -r '.operations[]?.operation_id' | \
  grep -E '^findPetsByStatus$|^getPetById$' | head -n1 || true)

if [ -z "${OPERATION:-}" ]; then
  info "Suitable operation not found (findPetsByStatus/getPetById). Using first operation."
  OPERATION=$(echo "$VALIDATE_JSON" | jq -r '.operations[0].operation_id')
fi

if [ -z "${OPERATION:-}" ] || [ "$OPERATION" = "null" ]; then
  fail "No operations available in spec"
fi
pass "Selected operation: $OPERATION"

echo ""
echo "=== Step 2: Register OpenAPI tools (filtered) ==="
# Build curl command with optional auth header
CURL_ARGS=(-fsS -X POST http://localhost:8000/tools/openapi/register -H 'Content-Type: application/json')
if [ -n "${MCP_REGISTER_TOKEN:-}" ]; then
  CURL_ARGS+=(-H "Authorization: Bearer ${MCP_REGISTER_TOKEN}")
fi

REGISTER_JSON=$(curl "${CURL_ARGS[@]}" \
  -d '{
    "name": "petstore_e2e",
    "spec_url": "'"$PETSTORE_SPEC_URL"'",
    "auth_type": "none",
    "operations": ["'"$OPERATION"'"]
  }' 2>/dev/null || echo '{"success":false}')

if echo "$REGISTER_JSON" | jq -e '.success == true' >/dev/null 2>&1; then
  pass "Registered OpenAPI operation(s): $(echo "$REGISTER_JSON" | jq -r '.operations_registered | join(", ")')"
else
  echo "$REGISTER_JSON" | jq . || true
  fail "OpenAPI registration failed"
fi

echo ""
echo "=== Step 3: Verify tool is listed ==="
TOOLS=$(curl -fsS http://localhost:8000/tools/list 2>/dev/null || echo '[]')
if echo "$TOOLS" | grep -q "$OPERATION"; then
  pass "Tool '$OPERATION' appears in /tools/list"
else
  echo "$TOOLS" | head -n 5
  fail "Tool '$OPERATION' not found in /tools/list"
fi

echo ""
echo "=== Step 4: Execute the tool ==="
EXEC_PAYLOAD='{}'
case "$OPERATION" in
  findPetsByStatus)
    EXEC_PAYLOAD='{"tool_name":"findPetsByStatus","parameters":{"status":"available"}}'
    ;;
  getPetById)
    EXEC_PAYLOAD='{"tool_name":"getPetById","parameters":{"petId":1}}'
    ;;
  *)
    # Generic: no params
    EXEC_PAYLOAD='{"tool_name":"'"$OPERATION"'","parameters":{}}'
    ;;
esac

EXEC_JSON=$(curl -fsS -X POST http://localhost:8000/tools/execute \
  -H 'Content-Type: application/json' \
  -d "$EXEC_PAYLOAD" 2>/dev/null || echo '{"success":false}')

if echo "$EXEC_JSON" | jq -e '.success == true' >/dev/null 2>&1; then
  pass "Tool executed successfully"
  echo "$EXEC_JSON" | jq -r '.output' | head -n 3 || true
else
  echo "$EXEC_JSON" | jq . || true
  fail "Tool execution did not succeed"
fi

echo ""
echo "================================"
echo "OpenAPI (PetStore) E2E Test Complete"
echo "- Validated spec"
echo "- Registered 1 operation as a tool"
echo "- Verified listing and executed the tool"
echo "================================"

