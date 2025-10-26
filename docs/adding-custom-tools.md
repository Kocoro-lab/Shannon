# Adding Custom Tools to Shannon

**Complete guide for open-source adopters to extend Shannon with custom tools****

---

## Table of Contents

1. [Overview](#overview)
2. [Quick Start: Adding MCP Tools](#quick-start-adding-mcp-tools)
3. [Adding OpenAPI Tools](#adding-openapi-tools)
4. [Adding Built-in Python Tools](#adding-built-in-python-tools)
5. [Configuration Reference](#configuration-reference)
6. [Testing & Verification](#testing--verification)
7. [Troubleshooting](#troubleshooting)
8. [Security Best Practices](#security-best-practices)

---

## Overview

Shannon supports three ways to add custom tools:

| Method | Best For | Code Changes | Restart Required |
|--------|----------|--------------|------------------|
| **MCP Tools** | External HTTP APIs, rapid prototyping | None | ✅ Service only |
| **OpenAPI Tools** | REST APIs with OpenAPI specs | None | ✅ Service only |
| **Built-in Tools** | Complex logic, database access, performance | Python code | ✅ Service only |

**Key Features:**
- ✅ No Proto/Rust/Go changes required (all tools use generic containers)
- ✅ Dynamic registration via API or YAML config
- ✅ Built-in rate limiting and circuit breakers
- ✅ Domain allowlisting for security
- ✅ Cost tracking and budget enforcement

---

## Quick Start: Adding MCP Tools

MCP (Model Context Protocol) tools let you integrate any HTTP endpoint as a Shannon tool with zero code changes.

### Step 1: Add Tool Definition

Edit `config/shannon.yaml` under the `mcp_tools` section:

```yaml
mcp_tools:
  weather_forecast:
    enabled: true
    url: "https://api.weather.com/v1/forecast"
    func_name: "get_weather"
    description: "Get weather forecast for a location"
    category: "data"
    cost_per_use: 0.001
    parameters:
      - name: "location"
        type: "string"
        required: true
        description: "City name or coordinates"
      - name: "units"
        type: "string"
        required: false
        description: "Temperature units (celsius/fahrenheit)"
        enum: ["celsius", "fahrenheit"]
    headers:
      X-API-Key: "${WEATHER_API_KEY}"  # Resolves from .env
```

**Required Fields:**
- `enabled`: Set to `true` to activate
- `url`: HTTP endpoint (must be POST, accepts JSON)
- `func_name`: Internal function name
- `description`: Clear description shown to LLM
- `category`: Tool category (e.g., `search`, `data`, `analytics`, `code`)
- `cost_per_use`: Estimated cost in USD
- `parameters`: Array of parameter definitions

**Optional Fields:**
- `headers`: HTTP headers for authentication (use `${ENV_VAR}` for secrets)

### Step 2: Configure Domain Access

**For Development (permissive):**

Add to `.env`:
```bash
MCP_ALLOWED_DOMAINS=*  # Wildcard - allows all domains
```

**For Production (recommended):**

```bash
MCP_ALLOWED_DOMAINS=localhost,127.0.0.1,api.weather.com,api.example.com
```

Or set in `deploy/compose/docker-compose.yml`:
```yaml
services:
  llm-service:
    environment:
      - MCP_ALLOWED_DOMAINS=api.weather.com,api.stocks.com
```

### Step 3: Add API Keys

Add your API key to `.env`:

```bash
# MCP Tool API Keys
WEATHER_API_KEY=your_api_key_here
STOCK_API_KEY=your_stock_key_here
```

### Step 4: Restart Service

**Important:** You must **recreate** the service (not just restart):

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --force-recreate llm-service
```

Wait for health check:
```bash
docker inspect shannon-llm-service-1 --format='{{.State.Health.Status}}'
```

### Step 5: Verify Registration

Check logs:
```bash
docker compose logs llm-service | grep "Loaded MCP tool"
```

List tools via API:
```bash
curl http://localhost:8000/tools/list | jq .
```

Get tool schema:
```bash
curl http://localhost:8000/tools/weather_forecast/schema | jq .
```

### Step 6: Test Your Tool

**Direct execution:**
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "weather_forecast",
    "parameters": {"location": "Tokyo", "units": "celsius"}
  }'
```

**Via workflow:**
```bash
SESSION_ID="test-$(date +%s)" ./scripts/submit_task.sh "What's the weather forecast for Tokyo?"
```

### Alternative: Runtime API Registration

For development/testing only (tools lost on restart):

```bash
# Set admin token in .env
MCP_REGISTER_TOKEN=your_secret_token

# Register tool
curl -X POST http://localhost:8000/tools/mcp/register \
  -H "Authorization: Bearer your_secret_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "weather_forecast",
    "url": "https://api.weather.com/v1/forecast",
    "func_name": "get_weather",
    "description": "Get weather forecast",
    "category": "data",
    "parameters": [
      {"name": "location", "type": "string", "required": true},
      {"name": "units", "type": "string", "enum": ["celsius", "fahrenheit"]}
    ]
  }'
```

### MCP Request Convention

Shannon sends POST requests in this format:

```json
{
  "function": "get_weather",
  "args": {
    "location": "Tokyo",
    "units": "celsius"
  }
}
```

Your endpoint should return JSON:
```json
{
  "temperature": 18,
  "condition": "Cloudy",
  "humidity": 65
}
```

---

## Adding OpenAPI Tools

For REST APIs with OpenAPI 3.x specifications, Shannon can automatically generate tools.

> **💡 New in v0.8:** For domain-specific APIs requiring custom transformations, see the [Vendor Adapter Pattern](#vendor-adapter-pattern) section below or the comprehensive [Vendor Adapters Guide](vendor-adapters.md).

### Features

**Supported:**
- ✅ OpenAPI 3.0 and 3.1 specs
- ✅ URL-based or inline spec loading
- ✅ JSON request/response bodies
- ✅ Path and query parameters
- ✅ Bearer, API Key (header/query), Basic auth
- ✅ Operation filtering by operationId or tags
- ✅ Circuit breaker (5 failures → 60s cooldown)
- ✅ Retry logic with exponential backoff (3 retries, configurable via `OPENAPI_RETRIES`)
- ✅ Configurable rate limits and timeouts
- ✅ Relative server URLs (resolved against spec URL)
- ✅ Basic `$ref` resolution (local references to `#/components/schemas/*`)

**Limitations (MVP):**

Shannon OpenAPI integration is production-ready for ~70% of REST APIs (JSON-based with simple auth). The following features are **not yet supported:**

1. **❌ File Upload APIs (multipart/form-data)**
   - Cannot upload files or binary data
   - Workaround: Use base64-encoded files in JSON body
   - Affected APIs: Image generation, file processing, document upload APIs

2. **❌ OAuth-Protected APIs**
   - No OAuth 2.0 flows (Authorization Code, Client Credentials)
   - Can only use Bearer tokens (manually obtained)
   - Affected APIs: Google APIs, GitHub, Slack, Twitter, etc.
   - Workaround: Manually obtain OAuth token and use `bearer` auth_type

3. **❌ Complex Parameter Encoding**
   - No `style`, `explode`, or `deepObject` serialization
   - Only basic path/query parameter substitution
   - Affected APIs: APIs with complex array/object query parameters

4. **❌ Multi-File OpenAPI Specs**
   - No remote `$ref` resolution (e.g., `https://example.com/schemas/Pet.json`)
   - Only local refs (`#/components/...`) supported
   - Workaround: Merge external schemas into single spec file

5. **❌ Advanced Schema Combinators**
   - No `allOf`, `oneOf`, `anyOf` support
   - Only basic type mapping
   - Affected APIs: APIs with polymorphic types or complex validation

6. **❌ Form-Encoded Requests**
   - No `application/x-www-form-urlencoded` content type
   - Only JSON request bodies supported

**What Works Well:**
- ✅ Simple REST APIs with JSON request/response
- ✅ APIs with Bearer/API Key/Basic authentication
- ✅ Read-heavy operations (GET requests)
- ✅ Well-structured specs with local `$ref` references
- ✅ Path and query parameters (primitives)

**Important:** For specs with relative server URLs (e.g., `/api/v3`), you must provide the spec via `spec_url` (not `spec_inline`) so Shannon can resolve the full base URL. Example: PetStore spec has `servers: [{url: "/api/v3"}]`, which resolves to `https://petstore3.swagger.io/api/v3` when loaded from `https://petstore3.swagger.io/api/v3/openapi.json`.

### Step 1: Add Tool Definition

Edit `config/shannon.yaml` under `openapi_tools`:

```yaml
openapi_tools:
  petstore:
    enabled: true
    spec_url: "https://petstore3.swagger.io/api/v3/openapi.json"
    # OR use inline spec:
    # spec_inline: |
    #   <paste OpenAPI JSON/YAML here>

    auth_type: "api_key"  # none|api_key|bearer|basic
    auth_config:
      api_key_name: "X-API-Key"           # Header name or query param name
      api_key_location: "header"          # header|query
      api_key_value: "$PETSTORE_API_KEY"  # Use $ prefix for env vars

    category: "data"
    base_cost_per_use: 0.001
    rate_limit: 30                        # Requests per minute
    timeout_seconds: 30                   # Request timeout
    max_response_bytes: 10485760          # Max response size (10MB)

    # Optional: Filter to specific operations
    operations:
      - "getPetById"
      - "findPetsByStatus"

    # Optional: Filter by tags
    # tags:
    #   - "pet"

    # Optional: Override base URL from spec
    # base_url: "https://custom-petstore.example.com"
```

### Authentication Examples

**Bearer Token (GitHub API):**
```yaml
github:
  enabled: true
  spec_url: "https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json"
  auth_type: "bearer"
  auth_config:
    token: "$GITHUB_TOKEN"
  operations:
    - "repos/get"
    - "repos/list-for-user"
```

**API Key in Query (OpenWeather):**
```yaml
weather:
  enabled: true
  spec_url: "https://api.openweathermap.org/data/3.0/openapi.json"
  auth_type: "api_key"
  auth_config:
    api_key_name: "appid"
    api_key_location: "query"
    api_key_value: "$OPENWEATHER_API_KEY"
```

**Basic Auth:**
```yaml
custom_api:
  enabled: true
  spec_url: "https://api.example.com/openapi.json"
  auth_type: "basic"
  auth_config:
    username: "$API_USERNAME"
    password: "$API_PASSWORD"
```

### Step 2: Configure Environment

Add to `.env`:

```bash
# OpenAPI Security
OPENAPI_ALLOWED_DOMAINS=*                # Use * for dev, specific domains for prod
OPENAPI_MAX_SPEC_SIZE=5242880            # 5MB default
OPENAPI_FETCH_TIMEOUT=30                 # Seconds

# API Keys
PETSTORE_API_KEY=your_key_here
GITHUB_TOKEN=ghp_xxxxxxxxxxxxx
OPENWEATHER_API_KEY=your_key
API_USERNAME=username
API_PASSWORD=password

# Same registration token as MCP
MCP_REGISTER_TOKEN=your_admin_token
```

### Step 3: Restart Service

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --force-recreate llm-service
```

### Step 4: Verify & Test

**Validate spec first:**
```bash
curl -X POST http://localhost:8000/tools/openapi/validate \
  -H "Content-Type: application/json" \
  -d '{"spec_url": "https://petstore3.swagger.io/api/v3/openapi.json"}' | jq .
```

**Response:**
```json
{
  "valid": true,
  "operations_count": 19,
  "operations": [
    {"operation_id": "getPetById", "method": "GET", "path": "/pet/{petId}"},
    {"operation_id": "addPet", "method": "POST", "path": "/pet"}
  ],
  "base_url": "https://petstore3.swagger.io/api/v3"
}
```

**List registered tools:**
```bash
curl http://localhost:8000/tools/list | grep Pet
```

**Execute tool:**
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "getPetById",
    "parameters": {"petId": 1}
  }' | jq .
```

### Alternative: Runtime Registration

```bash
curl -X POST http://localhost:8000/tools/openapi/register \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your_admin_token" \
  -d '{
    "name": "petstore",
    "spec_url": "https://petstore3.swagger.io/api/v3/openapi.json",
    "auth_type": "none",
    "category": "data",
    "operations": ["getPetById", "findPetsByStatus"],
    "rate_limit": 15,
    "timeout_seconds": 10
  }' | jq .
```

**Response includes verification:**
```json
{
  "success": true,
  "collection_name": "petstore",
  "operations_registered": ["getPetById", "findPetsByStatus"],
  "rate_limit": 15,
  "timeout_seconds": 10,
  "max_response_bytes": 10485760
}
```

---

## Adding Built-in Python Tools

For complex logic, database access, or performance-critical operations.

### When to Use Built-in Tools

**Use built-in tools when:**
- Need direct database/Redis access
- Require complex Python libraries (pandas, numpy)
- Performance-critical (avoid HTTP roundtrip)
- Need session state management
- Implement security-sensitive operations

**Use MCP/OpenAPI instead when:**
- Integrating external APIs
- Want no-code deployment
- Prototyping quickly
- Third-party service integration

### Step 1: Create Tool Class

Create file in `python/llm-service/llm_service/tools/builtin/my_custom_tool.py`:

```python
from typing import Any, Dict, List, Optional
from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult

class MyCustomTool(Tool):
    """
    Brief description of what this tool does.
    """

    def _get_metadata(self) -> ToolMetadata:
        """Define tool metadata."""
        return ToolMetadata(
            name="my_custom_tool",
            version="1.0.0",
            description="Clear description for LLM to understand when/how to use this tool",
            category="custom",  # search, data, analytics, code, file, custom
            author="Your Name",
            requires_auth=False,
            timeout_seconds=30,
            memory_limit_mb=128,
            sandboxed=False,
            session_aware=False,  # Set True if tool needs session state
            dangerous=False,      # Set True for file writes, code execution
            cost_per_use=0.001,   # USD per invocation
            rate_limit=60,        # Requests per minute (enforced by base class)
        )

    def _get_parameters(self) -> List[ToolParameter]:
        """Define tool parameters with validation."""
        return [
            ToolParameter(
                name="required_param",
                type=ToolParameterType.STRING,
                description="Description shown to LLM",
                required=True,
            ),
            ToolParameter(
                name="optional_number",
                type=ToolParameterType.INTEGER,
                description="An optional number parameter",
                required=False,
                default=10,
                min_value=1,
                max_value=100,
            ),
            ToolParameter(
                name="choice_param",
                type=ToolParameterType.STRING,
                description="Parameter with predefined choices",
                required=False,
                enum=["option1", "option2", "option3"],
            ),
        ]

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        **kwargs
    ) -> ToolResult:
        """
        Execute the tool logic.

        Args:
            session_context: Session data if session_aware=True
            **kwargs: Tool parameters (validated automatically)

        Returns:
            ToolResult with success/error status
        """
        try:
            # Extract parameters (already validated by base class)
            required_param = kwargs.get("required_param")
            optional_number = kwargs.get("optional_number", 10)
            choice_param = kwargs.get("choice_param")

            # Your tool logic here
            result = self._do_work(required_param, optional_number, choice_param)

            return ToolResult(
                success=True,
                output=result,
                metadata={"processed": True},
                execution_time_ms=50,
            )

        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Tool execution failed: {str(e)}"
            )

    def _do_work(self, param1, param2, param3):
        """Your actual implementation."""
        # Example: Database query, API call, computation
        return {"result": "success", "data": [1, 2, 3]}
```

### Step 2: Register Tool

Edit `python/llm-service/llm_service/api/tools.py` around line 228:

```python
# Add import at top
from ..tools.builtin.my_custom_tool import MyCustomTool

# Add to registration list in startup_event()
@router.on_event("startup")
async def startup_event():
    registry = get_registry()

    tools_to_register = [
        WebSearchTool,
        CalculatorTool,
        FileReadTool,
        FileWriteTool,
        PythonWasiExecutorTool,
        MyCustomTool,  # Add your tool here
    ]

    for tool_class in tools_to_register:
        try:
            registry.register(tool_class)
            logger.info(f"Registered tool: {tool_class.__name__}")
        except Exception as e:
            logger.error(f"Failed to register {tool_class.__name__}: {e}")
```

### Step 3: Restart Service

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --force-recreate llm-service
```

### Step 4: Test Tool

```bash
# Verify registration
curl http://localhost:8000/tools/list | grep my_custom_tool

# Get schema
curl http://localhost:8000/tools/my_custom_tool/schema | jq .

# Execute
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "my_custom_tool",
    "parameters": {
      "required_param": "test",
      "optional_number": 42,
      "choice_param": "option1"
    }
  }' | jq .
```

### Advanced: Session-Aware Tools

For tools that maintain state across executions:

```python
class SessionAwareTool(Tool):
    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="session_tool",
            session_aware=True,  # Enable session context
            ...
        )

    async def _execute_impl(
        self,
        session_context: Optional[Dict] = None,
        **kwargs
    ) -> ToolResult:
        # Access session data
        session_id = session_context.get("session_id") if session_context else None
        user_id = session_context.get("user_id") if session_context else None

        # Store/retrieve session-specific data
        # Example: Redis, database, in-memory cache

        return ToolResult(success=True, output={"session": session_id})
```

---

## Configuration Reference

### MCP Tool Configuration

```yaml
mcp_tools:
  tool_name:
    enabled: true                    # Required: Enable/disable tool
    url: "https://api.example.com"   # Required: HTTP endpoint
    func_name: "function_name"       # Required: Remote function name
    description: "Tool description"  # Required: LLM-visible description
    category: "data"                 # Required: Tool category
    cost_per_use: 0.001             # Required: Cost in USD
    parameters:                      # Required: Parameter definitions
      - name: "param1"
        type: "string"               # string|integer|float|boolean|array|object
        required: true
        description: "Param description"
        enum: ["val1", "val2"]       # Optional: Allowed values
        default: "val1"              # Optional: Default value
    headers:                         # Optional: HTTP headers
      X-API-Key: "${API_KEY_VAR}"   # Use ${} for env vars
```

### OpenAPI Tool Configuration

```yaml
openapi_tools:
  collection_name:
    enabled: true
    spec_url: "https://api.example.com/openapi.json"  # OR spec_inline
    auth_type: "none"                # none|api_key|bearer|basic
    auth_config:                     # Required if auth_type != "none"
      # For api_key:
      api_key_name: "X-API-Key"
      api_key_location: "header"     # header|query
      api_key_value: "$API_KEY"
      # For bearer:
      token: "$BEARER_TOKEN"
      # For basic:
      username: "$USERNAME"
      password: "$PASSWORD"
    category: "api"
    base_cost_per_use: 0.001
    rate_limit: 30                   # Requests per minute
    timeout_seconds: 30              # Request timeout
    max_response_bytes: 10485760     # Max response size (bytes)
    operations:                      # Optional: Filter operations
      - "operationId1"
      - "operationId2"
    tags:                            # Optional: Filter by tags
      - "tag1"
    base_url: "https://override.com" # Optional: Override spec base URL
```

### Environment Variables

**MCP Configuration:**
```bash
# Domain Security
MCP_ALLOWED_DOMAINS=localhost,127.0.0.1,api.example.com  # Or * for dev

# Circuit Breaker
MCP_CB_FAILURES=5                    # Failures before circuit opens
MCP_CB_RECOVERY_SECONDS=60           # Circuit open duration

# Request Limits
MCP_MAX_RESPONSE_BYTES=10485760      # 10MB default
MCP_RETRIES=3                        # Retry attempts
MCP_TIMEOUT_SECONDS=10               # Request timeout

# Registration Security
MCP_REGISTER_TOKEN=your_secret       # API registration protection
```

**OpenAPI Configuration:**
```bash
# Domain Security
OPENAPI_ALLOWED_DOMAINS=*            # Comma-separated or * for dev
OPENAPI_MAX_SPEC_SIZE=5242880        # 5MB spec size limit
OPENAPI_FETCH_TIMEOUT=30             # Spec fetch timeout

# Request Behavior
OPENAPI_RETRIES=3                    # Retry attempts (default: 3, matches MCP)
```

**Tool-Specific API Keys:**
```bash
# Add your tool API keys here
WEATHER_API_KEY=your_key
STOCK_API_KEY=your_key
GITHUB_TOKEN=ghp_xxxxx
PETSTORE_API_KEY=your_key
# ... add more as needed
```

---

## Testing & Verification

### Health Checks

```bash
# Check service health
curl http://localhost:8081/health | jq .

# Check LLM service status
docker inspect shannon-llm-service-1 --format='{{.State.Health.Status}}'
```

### List Tools

```bash
# All tools
curl http://localhost:8000/tools/list | jq .

# By category
curl "http://localhost:8000/tools/list?category=data" | jq .

# Exclude dangerous
curl "http://localhost:8000/tools/list?exclude_dangerous=true" | jq .

# List categories
curl http://localhost:8000/tools/categories | jq .
```

### Get Tool Schema

```bash
# Single tool schema
curl http://localhost:8000/tools/my_tool/schema | jq .

# All schemas
curl http://localhost:8000/tools/schemas | jq .

# Tool metadata
curl http://localhost:8000/tools/my_tool/metadata | jq .
```

### Execute Tools

**Direct execution:**
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "calculator",
    "parameters": {"expression": "sqrt(144) + 2^3"}
  }' | jq .
```

**Batch execution:**
```bash
curl -X POST http://localhost:8000/tools/batch-execute \
  -H "Content-Type: application/json" \
  -d '[
    {"tool_name": "calculator", "parameters": {"expression": "2+2"}},
    {"tool_name": "calculator", "parameters": {"expression": "10*5"}}
  ]' | jq .
```

**Via workflow:**
```bash
SESSION_ID="test-$(date +%s)" ./scripts/submit_task.sh "Calculate 2+2 and then multiply by 5"
```

### Monitor Logs

```bash
# Registration logs
docker compose logs llm-service | grep -i "registered tool"
docker compose logs llm-service | grep -i "loaded.*tools"

# Execution logs
docker compose logs -f llm-service orchestrator agent-core

# Tool-specific logs
docker compose logs llm-service | grep "my_tool"
```

### E2E Tests

```bash
# Run all tests
make smoke

# Run tool-specific tests
./tests/e2e/01_basic_calculator_test.sh
./tests/e2e/06_openapi_petstore_test.sh

# Run with MCP tools
./tests/e2e/run.sh
```

---

## Troubleshooting

### Tool Not Registered

**Symptom:** Tool doesn't appear in `/tools/list`

**Debug steps:**
```bash
# 1. Check YAML syntax
yamllint config/shannon.yaml

# 2. Check logs for errors
docker compose logs llm-service | grep -i error

# 3. Verify enabled flag
grep -A 10 "my_tool" config/shannon.yaml | grep enabled

# 4. Force recreate service
docker compose -f deploy/compose/docker-compose.yml up -d --force-recreate llm-service

# 5. Wait for health
sleep 10
docker inspect shannon-llm-service-1 --format='{{.State.Health.Status}}'
```

### Domain Validation Error

**Symptom:** `URL host 'example.com' not in allowed domains`

**Solutions:**

1. **Development:** Use wildcard
   ```bash
   # .env
   MCP_ALLOWED_DOMAINS=*
   OPENAPI_ALLOWED_DOMAINS=*
   ```

2. **Production:** Add specific domain
   ```bash
   # .env
   MCP_ALLOWED_DOMAINS=localhost,127.0.0.1,api.example.com
   OPENAPI_ALLOWED_DOMAINS=api.example.com,api.github.com
   ```

3. **Docker Compose:** Set in environment
   ```yaml
   services:
     llm-service:
       environment:
         - MCP_ALLOWED_DOMAINS=api.example.com
   ```

### Tool Execution Fails

**Symptom:** `ToolResult { success: false, error: "..." }`

**Debug:**
```bash
# 1. Test tool directly
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"tool_name":"my_tool","parameters":{...}}' | jq .

# 2. Check parameter types
curl http://localhost:8000/tools/my_tool/schema | jq '.parameters'

# 3. Validate parameters match schema
# Required params must be provided
# Types must match (string vs integer)
# Enum values must be in allowed list

# 4. Check agent core logs
docker logs shannon-agent-core-1 | grep "Tool execution error"

# 5. Check LLM service logs
docker logs shannon-llm-service-1 | grep my_tool
```

### Tool Not Selected by LLM

**Symptom:** LLM doesn't use your tool for relevant queries

**Solutions:**

1. **Improve description:** Make it specific and use LLM-friendly keywords
   ```yaml
   # Bad
   description: "Weather tool"

   # Good
   description: "Get real-time weather forecast data for any city including temperature, humidity, and conditions. Use for queries about current or future weather."
   ```

2. **Add to decomposition context:**

   Current limitation: Orchestrator passes empty tools list to decomposition. Tools won't appear in system prompt by default.

   **Quick fix:** Agent service has auto-load fallback (lines 598-609 in `agent.py`)

   **Proper fix:** Update orchestrator workflows to call `fetchAvailableTools()` and pass in `AvailableTools` field:
   - `go/orchestrator/internal/workflows/orchestrator_router.go:80`
   - `go/orchestrator/internal/workflows/supervisor_workflow.go:273`

3. **Test tool selection:**
   ```bash
   curl -X POST http://localhost:8000/tools/select \
     -H "Content-Type: application/json" \
     -d '{
       "task": "Get weather forecast for Tokyo",
       "max_tools": 3
     }' | jq .
   ```

4. **Use explicit mention:**
   ```bash
   ./scripts/submit_task.sh "Use the weather_forecast tool to get weather for Tokyo"
   ```

### Circuit Breaker Triggered

**Symptom:** `Circuit breaker open for <url> (too many failures)`

**Debug:**
```bash
# Check recent errors
docker logs shannon-llm-service-1 --tail 100 | grep -i "circuit\|failure"

# Wait for recovery (default 60s)
sleep 60

# Or restart to reset
docker compose restart llm-service
```

**Prevent:**
- Increase failure threshold: `MCP_CB_FAILURES=10`
- Increase recovery time: `MCP_CB_RECOVERY_SECONDS=120`
- Fix underlying API issues

### Rate Limit Exceeded

**Symptom:** `Rate limit exceeded for tool <name>`

**Solutions:**
```bash
# Increase rate limit in config
# config/shannon.yaml
rate_limit: 120  # Increase from default 60

# Or in tool metadata (built-in tools)
ToolMetadata(
    rate_limit=120,  # Requests per minute
    ...
)
```

---

## Security Best Practices

### Domain Allowlisting

**Development:**
```bash
# Permissive for testing
MCP_ALLOWED_DOMAINS=*
OPENAPI_ALLOWED_DOMAINS=*
```

**Staging:**
```bash
# Specific domains + localhost
MCP_ALLOWED_DOMAINS=localhost,127.0.0.1,staging-api.example.com
```

**Production:**
```bash
# Explicit allowlist only
MCP_ALLOWED_DOMAINS=api.example.com,api.partner.com
OPENAPI_ALLOWED_DOMAINS=api.github.com,api.openweathermap.org
```

**Subdomain Matching:**
- `api.example.com` allows `api.example.com` and `v1.api.example.com`
- Wildcard `*` bypasses all validation (use cautiously!)

### API Key Management

**❌ Never hardcode:**
```yaml
# BAD - Don't do this!
headers:
  X-API-Key: "sk-1234567890abcdef"
```

**✅ Use environment variables:**
```yaml
# GOOD - Reference env vars
headers:
  X-API-Key: "${WEATHER_API_KEY}"
```

**Store in `.env` (not tracked by git):**
```bash
# .env
WEATHER_API_KEY=sk-real-key-here
STOCK_API_KEY=your-stock-key
```

**For production:** Use secrets management
- Docker secrets
- Kubernetes secrets
- HashiCorp Vault
- AWS Secrets Manager

### Rate Limiting

**Per-tool limits:**
```yaml
# MCP tools
mcp_tools:
  expensive_api:
    rate_limit: 10  # Low limit for expensive calls

# OpenAPI tools
openapi_tools:
  github:
    rate_limit: 60  # GitHub's actual limit
```

**Global limits:**
```bash
# .env
MCP_RATE_LIMIT_DEFAULT=60    # Default for all MCP tools
```

### Circuit Breakers

**Configuration:**
```bash
# MCP circuit breaker
MCP_CB_FAILURES=5                 # Open after 5 failures
MCP_CB_RECOVERY_SECONDS=60        # Stay open for 60s

# Built-in per-base_url breakers
# - Prevents cascading failures
# - Isolates misbehaving services
# - Auto-recovery after timeout
```

### Authentication

**MCP Registration API:**
```bash
# Require token for dynamic registration
MCP_REGISTER_TOKEN=your-secure-random-token

# Use in requests
curl -H "Authorization: Bearer your-secure-random-token" ...
# OR
curl -H "X-Admin-Token: your-secure-random-token" ...
```

**Generate secure tokens:**
```bash
openssl rand -hex 32
```

### Dangerous Tools

Mark tools that modify state or access sensitive resources:

```python
ToolMetadata(
    name="file_write",
    dangerous=True,        # Triggers OPA policy checks
    requires_auth=True,    # Requires user authentication
    ...
)
```

**OPA policies can then gate access:**
```rego
# config/opa/policies/tools.rego
package tools

deny[msg] {
    input.tool == "file_write"
    not is_admin(input.user)
    msg := "file_write requires admin role"
}
```

### Response Size Limits

**Prevent DoS via large responses:**
```yaml
# OpenAPI tools
max_response_bytes: 10485760  # 10MB default

# MCP tools (env var)
MCP_MAX_RESPONSE_BYTES=10485760
```

### Timeout Configuration

**Prevent hanging requests:**
```yaml
# OpenAPI tools
timeout_seconds: 30  # Per-request timeout

# MCP tools (env var)
MCP_TIMEOUT_SECONDS=10
```

### HTTPS Enforcement

**For non-localhost URLs, Shannon enforces HTTPS:**
- ✅ `https://api.example.com` - Allowed
- ✅ `http://localhost:8080` - Allowed
- ❌ `http://api.example.com` - Rejected in production

---

## Next Steps

**Explore Advanced Topics:**
- [Tools Implementation Guide](tools-implementation-guide.md) - Architecture deep-dive
- [MCP Integration](mcp-integration.md) - Full MCP specification
- [Python WASI Execution](python-code-execution.md) - Sandboxed code execution

**Example Tools:**
- Built-in tools: `python/llm-service/llm_service/tools/builtin/`
- MCP examples: `config/shannon.yaml` (commented examples)
- OpenAPI examples: `tests/e2e/06_openapi_petstore_test.sh`

**Community:**
- Report issues: https://github.com/anthropics/shannon/issues
- Contribute tools: https://github.com/anthropics/shannon/pulls

---

## Summary

**Three ways to add tools:**

| Method | Command | Config File | Code Changes |
|--------|---------|-------------|--------------|
| **MCP** | `docker compose up -d --force-recreate llm-service` | `config/shannon.yaml` | None |
| **OpenAPI** | `docker compose up -d --force-recreate llm-service` | `config/shannon.yaml` | None |
| **Built-in** | `docker compose up -d --force-recreate llm-service` | `api/tools.py` + new file | Python only |

**Key takeaways:**
- ✅ Zero proto/Rust/Go changes (generic `google.protobuf.Struct` containers)
- ✅ Security built-in (domain allowlisting, rate limiting, circuit breakers)
- ✅ Cost tracking automatic (set `cost_per_use` in metadata)
- ✅ Schema-driven (OpenAI-compatible JSON schemas)

**Quick reference:**
```bash
# List tools
curl http://localhost:8000/tools/list

# Get schema
curl http://localhost:8000/tools/{name}/schema

# Execute
curl -X POST http://localhost:8000/tools/execute \
  -d '{"tool_name":"my_tool","parameters":{...}}'

# Via workflow
./scripts/submit_task.sh "Your query here"
```

Happy tool building! 🛠️

---

## Vendor Adapter Pattern

**For domain-specific APIs and custom agents**

When integrating proprietary or internal APIs that require domain-specific transformations, use the **vendor adapter pattern** to keep vendor logic separate from Shannon's core infrastructure.

### When to Use

Use vendor adapters when your API integration requires:
- Custom field name aliasing (e.g., `users` → `my:unique_users`)
- Request/response transformations
- Dynamic parameter injection from session context
- Domain-specific validation or normalization
- Specialized agent roles with custom system prompts

### Quick Example

**1. Create vendor adapter:**

`python/llm-service/llm_service/tools/vendor_adapters/myvendor.py`:
```python
class MyVendorAdapter:
    def transform_body(self, body, operation_id, prompt_params):
        # Transform field names
        if isinstance(body.get("metrics"), list):
            body["metrics"] = [m.replace("users", "my:users") for m in body["metrics"]]

        # Inject session params
        if prompt_params and "account_id" in prompt_params:
            body["account_id"] = prompt_params["account_id"]

        return body
```

**2. Register adapter:**

`python/llm-service/llm_service/tools/vendor_adapters/__init__.py`:
```python
def get_vendor_adapter(name: str):
    if name.lower() == "myvendor":
        from .myvendor import MyVendorAdapter
        return MyVendorAdapter()
    return None
```

**3. Configure with vendor flag:**

`config/overlays/shannon.myvendor.yaml`:
```yaml
openapi_tools:
  myvendor_api:
    enabled: true
    spec_path: config/openapi_specs/myvendor_api.yaml
    auth_type: bearer
    auth_config:
      vendor: myvendor  # Triggers adapter loading
      token: "${MYVENDOR_API_TOKEN}"
    category: custom
```

**4. Use environment:**
```bash
SHANNON_CONFIG_PATH=config/overlays/shannon.myvendor.yaml
MYVENDOR_API_TOKEN=your_token_here
```

### Benefits

- ✅ **Clean separation:** Vendor code isolated from Shannon core
- ✅ **No core changes:** Shannon infrastructure remains generic
- ✅ **Conditional loading:** Graceful fallback if vendor module unavailable
- ✅ **Easy testing:** Unit test adapters in isolation
- ✅ **Secrets management:** All tokens via environment variables

### Complete Guide

For a comprehensive guide including:
- Custom agent roles for specialized domains
- Session context injection patterns
- Testing strategies
- Best practices and troubleshooting

See: **[Vendor Adapters Guide](vendor-adapters.md)**

---
