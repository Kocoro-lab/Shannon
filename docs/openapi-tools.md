# OpenAPI Tools Configuration Guide

Complete reference for configuring and using OpenAPI 3.x tools in Shannon.

---

## Table of Contents

1. [Overview](#overview)
2. [Configuration Reference](#configuration-reference)
3. [Authentication Types](#authentication-types)
4. [Advanced Features](#advanced-features)
5. [Troubleshooting](#troubleshooting)
6. [Examples](#examples)

---

## Overview

Shannon can automatically generate tools from OpenAPI 3.x specifications, allowing you to integrate any REST API without writing code. The OpenAPI loader:

- ✅ Parses OpenAPI 3.0/3.1 specs
- ✅ Generates one tool per operation
- ✅ Handles authentication (Bearer, API Key, Basic)
- ✅ Supports path/query/header parameters
- ✅ Includes circuit breaker and rate limiting
- ✅ Validates requests against schema
- ✅ Resolves `$ref` references locally

**Quick Start**: See [Adding Custom Tools Guide](adding-custom-tools.md#adding-openapi-tools)

---

## Configuration Reference

### Basic Configuration

```yaml
# config/shannon.yaml or config/overlays/shannon.myvendor.yaml
openapi_tools:
  tool_collection_name:
    enabled: true | false                   # Enable/disable this tool collection
    spec_path: string                       # Path to OpenAPI spec file
    spec_url: string                        # OR: URL to fetch spec from
    spec_inline: string                     # OR: Inline YAML/JSON spec

    auth_type: none | api_key | bearer | basic
    auth_config: object                     # Auth configuration (see below)

    category: string                        # Tool category (e.g., "analytics", "data")
    base_cost_per_use: float               # Estimated cost per operation (USD)
    rate_limit: integer                     # Requests per minute (default: 30)
    timeout_seconds: float                  # Request timeout (default: 30)
    max_response_bytes: integer             # Max response size (default: 10MB)

    operations: [string]                    # Optional: Filter by operationId
    tags: [string]                          # Optional: Filter by tags
    base_url: string                        # Optional: Override spec's base URL
```

### Field Descriptions

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean | Yes | - | Enable this tool collection |
| `spec_path` | string | One of spec_* | - | Local file path to OpenAPI spec |
| `spec_url` | string | One of spec_* | - | URL to fetch spec from (supports relative server URLs) |
| `spec_inline` | string | One of spec_* | - | Inline YAML/JSON spec content |
| `auth_type` | enum | Yes | `none` | Authentication method |
| `auth_config` | object | If auth ≠ none | `{}` | Auth configuration (varies by type) |
| `category` | string | No | `"api"` | Tool category for organization |
| `base_cost_per_use` | float | No | `0.001` | Estimated cost per invocation (USD) |
| `rate_limit` | integer | No | `30` | Max requests per minute per tool |
| `timeout_seconds` | float | No | `30.0` | HTTP request timeout |
| `max_response_bytes` | integer | No | `10485760` | Max response size (10MB) |
| `operations` | array[string] | No | All | Filter to specific operationIds |
| `tags` | array[string] | No | All | Filter operations by tags |
| `base_url` | string | No | From spec | Override API base URL |

---

## Authentication Types

### 1. No Authentication

```yaml
openapi_tools:
  public_api:
    enabled: true
    spec_url: https://api.example.com/openapi.json
    auth_type: none
```

### 2. Bearer Token

Used by: GitHub, GitLab, most modern APIs

```yaml
openapi_tools:
  github_api:
    enabled: true
    spec_url: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
    auth_type: bearer
    auth_config:
      token: "${GITHUB_TOKEN}"              # From environment variable
      # OR:
      # token: "ghp_xxxxxxxxxxxxx"          # Hardcoded (not recommended)
```

**Environment Variable**:
```bash
GITHUB_TOKEN=ghp_your_token_here
```

**Headers sent**:
```
Authorization: Bearer ghp_your_token_here
```

### 3. API Key in Header

Used by: OpenAI, Anthropic, many SaaS APIs

```yaml
openapi_tools:
  openai_api:
    enabled: true
    spec_url: https://raw.githubusercontent.com/openai/openai-openapi/master/openapi.yaml
    auth_type: api_key
    auth_config:
      api_key_name: Authorization                # Header name
      api_key_location: header                   # "header" or "query"
      api_key_value: "Bearer ${OPENAI_API_KEY}"  # Value with prefix
```

**Environment Variable**:
```bash
OPENAI_API_KEY=sk-your-key-here
```

**Headers sent**:
```
Authorization: Bearer sk-your-key-here
```

### 4. API Key in Query Parameter

Used by: OpenWeather, some legacy APIs

```yaml
openapi_tools:
  weather_api:
    enabled: true
    spec_url: https://api.openweathermap.org/data/3.0/openapi.json
    auth_type: api_key
    auth_config:
      api_key_name: appid                   # Query param name
      api_key_location: query               # "query" not "header"
      api_key_value: "${OPENWEATHER_KEY}"
```

**Request URL**:
```
GET https://api.openweathermap.org/data/3.0/weather?appid=your_key_here&q=London
```

### 5. Basic Authentication

Used by: Legacy APIs, internal services

```yaml
openapi_tools:
  legacy_api:
    enabled: true
    spec_path: config/openapi_specs/legacy_api.yaml
    auth_type: basic
    auth_config:
      username: "${API_USERNAME}"
      password: "${API_PASSWORD}"
```

**Environment Variables**:
```bash
API_USERNAME=admin
API_PASSWORD=secret123
```

**Headers sent**:
```
Authorization: Basic YWRtaW46c2VjcmV0MTIz
```

### 6. Custom Headers

For vendor-specific authentication:

```yaml
openapi_tools:
  custom_api:
    enabled: true
    spec_url: https://api.custom.com/v1/openapi.json
    auth_type: api_key
    auth_config:
      vendor: myvendor                      # Triggers vendor adapter
      api_key_name: X-API-Key
      api_key_location: header
      api_key_value: "${CUSTOM_API_KEY}"
      extra_headers:
        X-Account-ID: "{{body.account_id}}"  # Dynamic from request body
        X-User-ID: "${CUSTOM_USER_ID}"       # Static from environment
        X-Timestamp: "{{timestamp}}"          # Dynamic template
```

**Dynamic Header Templates**:
- `"${ENV_VAR}"` - Resolved from environment
- `"{{body.field}}"` - Resolved from request body at runtime
- Static strings - Used as-is

---

## Advanced Features

### Operation Filtering

**By operationId** (recommended):
```yaml
openapi_tools:
  petstore:
    spec_url: https://petstore3.swagger.io/api/v3/openapi.json
    operations:
      - getPetById              # Only generate tool for this operation
      - findPetsByStatus
      - addPet
```

**By tags**:
```yaml
openapi_tools:
  petstore:
    spec_url: https://petstore3.swagger.io/api/v3/openapi.json
    tags:
      - pet                     # Only operations tagged "pet"
      - user
```

### Base URL Override

Override the base URL from the spec:

```yaml
openapi_tools:
  api_staging:
    spec_url: https://api.example.com/openapi.json
    base_url: https://staging-api.example.com  # Use staging instead
```

**Use cases**:
- Testing against staging/dev environments
- Internal proxies or gateways
- Local development

### Rate Limiting

Protect external APIs from overload:

```yaml
openapi_tools:
  expensive_api:
    spec_url: https://expensive.api.com/openapi.json
    rate_limit: 10              # Only 10 requests per minute
    timeout_seconds: 60         # 60 second timeout for slow operations
```

**Per-tool limits**: Each operation generated from the spec inherits this limit.

**Behavior**:
- Enforced via token bucket algorithm
- Shared across all tool instances (single Shannon instance)
- Returns error if limit exceeded

### Circuit Breaker

Automatic failure protection:

**Configuration** (via environment):
```bash
# Circuit breaker opens after 5 failures
# Stays open for 60 seconds before allowing retry
# (These are defaults, not directly configurable per tool yet)
```

**States**:
1. **Closed** (normal): All requests pass through
2. **Open** (failing): All requests immediately rejected
3. **Half-open** (testing): One trial request allowed

**Behavior**:
- After 5 consecutive failures → opens circuit
- Circuit stays open for 60 seconds
- Then allows one trial request (half-open)
- Success → closes circuit
- Failure → reopens for another 60 seconds

### Response Size Limits

Prevent memory exhaustion:

```yaml
openapi_tools:
  api_with_large_responses:
    spec_url: https://api.example.com/openapi.json
    max_response_bytes: 52428800  # 50MB limit
```

**Behavior**:
- Responses larger than limit are truncated
- Error returned with truncated marker

---

## Troubleshooting

### Tool Not Registered

**Symptom**: Tool doesn't appear in `/tools/list`

**Debug**:
```bash
# 1. Check if spec is valid
curl -X POST http://localhost:8000/tools/openapi/validate \
  -H "Content-Type: application/json" \
  -d '{"spec_url": "https://api.example.com/openapi.json"}'

# 2. Check logs
docker logs shannon-llm-service-1 | grep -i "openapi"

# 3. Verify enabled flag
grep -A 5 "my_tool" config/shannon.yaml | grep enabled
```

**Common causes**:
- `enabled: false` in config
- Invalid OpenAPI spec
- Domain not in `OPENAPI_ALLOWED_DOMAINS`
- Spec fetch timeout
- Circular `$ref` references

### Domain Validation Error

**Symptom**: `URL host 'example.com' not in allowed domains`

**Fix**:
```bash
# Development: Allow all
OPENAPI_ALLOWED_DOMAINS=*

# Production: Specific domains
OPENAPI_ALLOWED_DOMAINS=api.github.com,api.example.com,api.partner.com
```

**In docker-compose.yml**:
```yaml
services:
  llm-service:
    environment:
      - OPENAPI_ALLOWED_DOMAINS=api.example.com
```

### Spec Fetch Timeout

**Symptom**: `Failed to fetch OpenAPI spec: timeout`

**Fix**:
```bash
# Increase timeout (default: 30s)
OPENAPI_FETCH_TIMEOUT=60

# Or use local spec file instead
openapi_tools:
  my_api:
    spec_path: config/openapi_specs/my_api.yaml  # Local file
```

### Circuit Breaker Triggered

**Symptom**: `Circuit breaker open for https://api.example.com`

**Debug**:
```bash
# Check recent errors
docker logs shannon-llm-service-1 --tail 100 | grep -i "circuit\|failure"
```

**Fix**:
- Wait 60 seconds for automatic recovery
- Fix underlying API issues
- Increase timeout if API is slow:
  ```yaml
  timeout_seconds: 60
  ```

### Rate Limit Exceeded

**Symptom**: `Rate limit exceeded for tool my_tool`

**Fix**:
```yaml
# Increase limit
openapi_tools:
  my_api:
    rate_limit: 120  # 120 requests per minute
```

### Authentication Failures

**Symptom**: `401 Unauthorized` or `403 Forbidden`

**Debug**:
```bash
# Check environment variables
env | grep API_KEY

# Test token manually
curl -H "Authorization: Bearer $YOUR_TOKEN" https://api.example.com/endpoint
```

**Common causes**:
- Environment variable not set
- Token expired
- Wrong auth type (should be `bearer` not `api_key`)
- Missing `Bearer` prefix for API key auth

---

## Examples

### Example 1: GitHub API

```yaml
openapi_tools:
  github:
    enabled: true
    spec_url: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
    auth_type: bearer
    auth_config:
      token: "${GITHUB_TOKEN}"
    category: development
    rate_limit: 60
    operations:
      - repos/get
      - repos/list-for-user
      - issues/list-for-repo
```

**Usage**:
```bash
./scripts/submit_task.sh "List all repositories for user octocat"
```

### Example 2: OpenWeather API

```yaml
openapi_tools:
  openweather:
    enabled: true
    spec_url: https://api.openweathermap.org/data/3.0/openapi.json
    auth_type: api_key
    auth_config:
      api_key_name: appid
      api_key_location: query
      api_key_value: "${OPENWEATHER_API_KEY}"
    category: data
    rate_limit: 60
```

**Usage**:
```bash
./scripts/submit_task.sh "What's the weather forecast for Tokyo?"
```

### Example 3: Internal API with Vendor Adapter

```yaml
openapi_tools:
  internal_analytics:
    enabled: true
    spec_path: config/openapi_specs/internal_analytics.yaml
    auth_type: bearer
    auth_config:
      vendor: mycompany                     # Triggers vendor adapter
      token: "${ANALYTICS_API_TOKEN}"
      extra_headers:
        X-Tenant-ID: "{{body.tenant_id}}"
    category: analytics
    base_cost_per_use: 0.005
```

**Vendor Adapter** (`python/llm-service/llm_service/tools/vendor_adapters/mycompany.py`):
```python
class MyCompanyAdapter:
    def transform_body(self, body, operation_id, prompt_params):
        # Add tenant context
        if prompt_params and "tenant_id" in prompt_params:
            body["tenant_id"] = prompt_params["tenant_id"]

        # Transform metric names
        if "metrics" in body:
            body["metrics"] = [f"myco:{m}" for m in body["metrics"]]

        return body
```

---

## Security Best Practices

1. **Never hardcode secrets**
   ```yaml
   # ❌ Bad
   auth_config:
     token: "sk-1234567890"

   # ✅ Good
   auth_config:
     token: "${API_TOKEN}"
   ```

2. **Use domain allowlisting**
   ```bash
   # Production
   OPENAPI_ALLOWED_DOMAINS=api.trusted.com,api.partner.com

   # Not: OPENAPI_ALLOWED_DOMAINS=*
   ```

3. **Set appropriate rate limits**
   ```yaml
   rate_limit: 60  # Match your API provider's limits
   ```

4. **Use HTTPS**
   - Shannon automatically upgrades HTTP to HTTPS for external APIs
   - localhost/127.0.0.1 allowed on HTTP for development

5. **Limit response sizes**
   ```yaml
   max_response_bytes: 10485760  # 10MB default
   ```

---

## See Also

- **[Adding Custom Tools Guide](adding-custom-tools.md)** - Complete tool integration guide
- **[Vendor Adapters Guide](vendor-adapters.md)** - Domain-specific integrations
- **[Extending Shannon](extending-shannon.md)** - Other extension methods
- **OpenAPI Parser Tests**: `python/llm-service/tests/test_openapi_parser.py`
- **OpenAPI Tool Tests**: `python/llm-service/tests/test_openapi_tool.py`

---

## Quick Reference

```bash
# Validate spec
curl -X POST http://localhost:8000/tools/openapi/validate \
  -d '{"spec_url": "https://api.example.com/openapi.json"}' | jq .

# List registered tools
curl http://localhost:8000/tools/list | jq .

# Get tool schema
curl http://localhost:8000/tools/myTool/schema | jq .

# Execute tool
curl -X POST http://localhost:8000/tools/execute \
  -d '{"tool_name":"myTool","parameters":{...}}' | jq .

# Test via workflow
./scripts/submit_task.sh "Your query using the tool"
```

---

**Need Help?**

- Report issues: https://github.com/anthropics/shannon/issues
- Documentation: https://docs.shannon.ai
- Examples: `tests/e2e/06_openapi_petstore_test.sh`
