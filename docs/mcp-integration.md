# MCP Integration (Developer Preview)

This document describes Shannon's current MCP (Model Context Protocol) integration, the registration API, and a production‑hardening checklist.

## Quick Start: Adding a New MCP Tool

### Method 1: Config File (Production)

1. **Edit `config/shannon.yaml`**:
```yaml
mcp_tools:
  weather_api:
    enabled: true
    url: "https://api.weather.com/mcp"
    func_name: "get_weather"
    description: "Get weather for a city"
    category: "weather"
    cost_per_use: 0.001
    parameters:
      - name: "city"
        type: "string"
        required: true
        description: "City name"
    headers:
      X-API-Key: "${WEATHER_API_KEY}"  # From .env
```

2. **Add API key to `.env`**:
```bash
WEATHER_API_KEY=your_api_key_here
```

3. **Restart service**:
```bash
docker compose -f deploy/compose/docker-compose.yml restart llm-service
```

4. **Use the tool**:
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool_name": "weather_api", "parameters": {"city": "Beijing"}}'
```

### Method 2: API Registration (Development Only)

```bash
# Set auth token in .env
MCP_REGISTER_TOKEN=your_admin_token

# Register tool
curl -X POST http://localhost:8000/tools/mcp/register \
  -H "Authorization: Bearer your_admin_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "weather_api",
    "url": "https://api.weather.com/mcp",
    "func_name": "get_weather",
    "parameters": [{"name": "city", "type": "string", "required": true}]
  }'
```

**Note**: API method is in-memory only - tool disappears on restart. Use config file for permanent tools.

## What’s Implemented

- Stateless MCP HTTP client in LLM service: `llm_service.mcp_client.HttpStatelessClient`
- Dynamic Tool wrapper for MCP functions: `llm_service.tools.mcp.create_mcp_tool_class`
- Runtime registration API: `POST /tools/mcp/register` to expose a remote MCP function as a local Tool
- Smoke coverage using an in‑service mock endpoint: `POST /mcp/mock` (dev only)

The design is intentionally minimal: a single HTTP POST with `{\"function\": <name>, \"args\": {...}}` and a JSON response. It is suitable for development and staging; additional controls are needed for production.

## Quickstart

1) Register a remote function as a tool

```
POST /tools/mcp/register
{
  "name": "gaode_maps",
  "url": "https://mcp.example.com/mcp",
  "func_name": "maps_geo",
  "description": "Remote maps geocoding via MCP",
  "parameters": [
    {"name": "address", "type": "string", "required": true},
    {"name": "city", "type": "string"}
  ]
}
```

2) Execute it like any other tool

```
POST /tools/execute
{
  "tool_name": "gaode_maps",
  "parameters": {"address": "Tiananmen Square", "city": "Beijing"}
}
```

If `parameters` are omitted during registration, the tool accepts a single OBJECT parameter `args` and forwards it as MCP args.

## Production‑Hardening Checklist

- Authentication & Authorization
  - Restrict `/tools/mcp/register` to admins (gateway token or service‑side auth). Set `MCP_REGISTER_TOKEN` and send `Authorization: Bearer <token>` (or `X-Admin-Token`).
  - Role‑based rules for who can invoke which MCP tools

- Network & Secrets
  - Allowlist MCP endpoints (domain/IP), deny by default via `MCP_ALLOWED_DOMAINS` (comma‑separated; defaults to `localhost,127.0.0.1`)
  - Enforce TLS; consider mTLS for sensitive backends
  - Store API keys/tokens in secrets (env/vault), not in code

- Safety Limits
  - Request timeout (`MCP_TIMEOUT_SECONDS`), response size limits (`MCP_MAX_RESPONSE_BYTES`), arg validation
  - Retry policy with backoff (`MCP_RETRIES`) + circuit breaker (`MCP_CB_FAILURES`, `MCP_CB_RECOVERY_SECONDS`) to isolate failing MCPs
  - Per‑tool rate limiting (`MCP_RATE_LIMIT_DEFAULT` requests/min)
  - Rate limits per tenant/user/tool

- Policy & Budgets
  - OPA policy checks before MCP calls (dangerous capability gates)
  - Token/cost budgets enforced, with cut‑offs and approvals when needed

- Observability
  - OTLP tracing available via httpx instrumentation
  - Prometheus metrics: `llm_mcp_requests_total`, `llm_mcp_request_duration_seconds`

- Multi‑Tenancy
  - Per‑tenant tool registries or namespacing
  - Configurable headers per tenant (e.g., API keys), never shared across tenants

- Schema & Validation
  - Optionally fetch schema from MCP server; validate input rigorously
  - Sanitize/validate outputs before feeding downstream

- Testing & Rollout
  - Unit/e2e tests for registration, invocation, error paths
  - Canary new MCP tools and monitor before full rollout

## Persistence Options

### Config-Based (Recommended for Production)

Define MCP tools in `config/shannon.yaml`:

```yaml
mcp_tools:
  weather_api:
    enabled: true
    url: "https://api.weather.com/mcp"
    func_name: "get_weather"
    description: "Weather service API"
    category: "weather"
    cost_per_use: 0.001
    parameters:
      - name: "city"
        type: "string"
        required: true
    headers:
      X-API-Key: "${WEATHER_API_KEY}"  # Expands from env
```

Benefits:
- Version controlled
- Hot-reload on changes (restart service to apply)
- No database dependency
- Secrets via environment variables

### Dynamic Registration (Development)

Use the `/tools/mcp/register` API with Bearer token authentication:
- Tools stored in-memory only
- Lost on restart
- Good for testing and development

## Status

- Current status: Developer Preview with production-ready hardening
- Config-based persistence available for production deployments
- Circuit breaker and rate limiting protect against misbehaving MCP endpoints

Refer to `archive-historical/implementing-parallel-tools-preemption-mcp.md` for implementation details and `scripts/smoke_e2e.sh` for testing.
