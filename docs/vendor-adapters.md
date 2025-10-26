# Vendor Adapters for Custom Agent Integration

**Complete guide for integrating custom agents and domain-specific tools into Shannon using the vendor adapter pattern**

---

## Table of Contents

1. [Overview](#overview)
2. [When to Use Vendor Adapters](#when-to-use-vendor-adapters)
3. [Architecture](#architecture)
4. [Quick Start Example](#quick-start-example)
5. [Component Guide](#component-guide)
6. [Best Practices](#best-practices)
7. [Testing & Verification](#testing--verification)
8. [Troubleshooting](#troubleshooting)

---

## Overview

The **vendor adapter pattern** allows you to integrate domain-specific agents and tools into Shannon without polluting the core codebase. This pattern maintains clean separation between:

- **Generic Shannon infrastructure** (committed to open source)
- **Vendor-specific implementations** (kept private or in separate repositories)

**Key Benefits:**
- ✅ Zero changes to core Shannon code
- ✅ Clean separation of concerns
- ✅ Easy to maintain vendor-specific logic
- ✅ No secrets in codebase
- ✅ Conditional loading with graceful fallback
- ✅ Fully testable in isolation

---

## When to Use Vendor Adapters

**Use vendor adapters when:**
- Integrating proprietary/internal APIs with domain-specific requirements
- Need custom request/response transformations for OpenAPI tools
- Building specialized agents for specific business domains
- Field naming conventions differ from your internal systems
- Require dynamic parameter injection from session context
- Need custom authentication or header logic

**Example use cases:**
- Analytics platforms (metrics aliasing, time range normalization)
- E-commerce systems (product field mapping, SKU transformations)
- CRM integrations (contact field normalization)
- Internal microservices (custom auth tokens, tenant IDs)
- Domain-specific data validation

---

## Architecture

### File Structure

```
Shannon/
├── config/
│   ├── shannon.yaml                          # Base config (generic, committed)
│   └── overlays/
│       └── shannon.myvendor.yaml             # Vendor overlay (not committed)
├── config/openapi_specs/
│   └── myvendor_api.yaml                     # Vendor API spec (not committed)
├── python/llm-service/llm_service/
│   ├── roles/
│   │   ├── presets.py                        # Generic roles + conditional import
│   │   └── myvendor/                         # Vendor role module (not committed)
│   │       ├── __init__.py
│   │       └── custom_agent.py               # Specialized agent role
│   └── tools/
│       ├── openapi_tool.py                   # Generic OpenAPI loader (committed)
│       └── vendor_adapters/                  # Vendor adapters (not committed)
│           ├── __init__.py                   # Adapter registry
│           └── myvendor.py                   # Vendor-specific transformations
```

### Component Responsibilities

| Component | Responsibility | Committed to OSS |
|-----------|---------------|------------------|
| **Config Overlay** | Vendor-specific tool configurations | ❌ No |
| **OpenAPI Spec** | API schema definition | ❌ No |
| **Vendor Adapter** | Request/response transformations | ❌ No |
| **Vendor Role** | Specialized agent system prompts | ❌ No |
| **Generic Infrastructure** | Core OpenAPI/role system | ✅ Yes |

---

## Quick Start Example

Let's create a complete vendor integration for a fictional analytics platform called "DataInsight".

### Step 1: Create Vendor Adapter

Create `python/llm-service/llm_service/tools/vendor_adapters/datainsight.py`:

```python
"""Vendor adapter for DataInsight Analytics API."""
from typing import Any, Dict, Optional


class DataInsightAdapter:
    """Transforms requests for DataInsight API conventions."""

    def transform_body(
        self,
        body: Dict[str, Any],
        operation_id: str,
        prompt_params: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Transform request body for DataInsight API.

        Args:
            body: Original request body from LLM
            operation_id: OpenAPI operation ID
            prompt_params: Session context parameters (injected by orchestrator)

        Returns:
            Transformed body matching DataInsight API expectations
        """
        if not isinstance(body, dict):
            return body

        # Inject session context (account_id, user_id from prompt_params)
        if prompt_params and isinstance(prompt_params, dict):
            if "account_id" in prompt_params and "account_id" not in body:
                body["account_id"] = prompt_params["account_id"]
            if "user_id" in prompt_params and "user_id" not in body:
                body["user_id"] = prompt_params["user_id"]

        # Operation-specific transformations
        if operation_id == "queryMetrics":
            body = self._transform_query_metrics(body)
        elif operation_id == "getDimensionValues":
            body = self._transform_dimension_values(body)

        return body

    def _transform_query_metrics(self, body: Dict) -> Dict:
        """Transform metric query requests."""
        # Normalize metric names (support shorthand)
        metric_aliases = {
            "users": "di:unique_users",
            "sessions": "di:total_sessions",
            "pageviews": "di:page_views",
            "bounce_rate": "di:bounce_rate",
        }

        if isinstance(body.get("metrics"), list):
            body["metrics"] = [
                metric_aliases.get(m, m) for m in body["metrics"]
            ]

        # Normalize time range format
        if "timeRange" in body and isinstance(body["timeRange"], dict):
            tr = body["timeRange"]
            # Ensure startTime/endTime (not start/end)
            if "start" in tr:
                tr["startTime"] = tr.pop("start")
            if "end" in tr:
                tr["endTime"] = tr.pop("end")

        # Convert sort to expected format
        if isinstance(body.get("sort"), dict):
            field = body["sort"].get("field")
            order = body["sort"].get("order", "DESC").upper()
            body["sort"] = {"field": field, "direction": order}

        return body

    def _transform_dimension_values(self, body: Dict) -> Dict:
        """Transform dimension value requests."""
        dimension_aliases = {
            "country": "di:geo_country",
            "device": "di:device_type",
            "source": "di:traffic_source",
        }

        if "dimension" in body:
            body["dimension"] = dimension_aliases.get(
                body["dimension"], body["dimension"]
            )

        return body
```

### Step 2: Register Adapter

Edit `python/llm-service/llm_service/tools/vendor_adapters/__init__.py`:

```python
from typing import Optional


def get_vendor_adapter(name: str):
    """Return a vendor adapter instance by name, or None if not available."""
    if not name:
        return None
    try:
        if name.lower() == "datainsight":
            from .datainsight import DataInsightAdapter
            return DataInsightAdapter()
        # Add more vendors here
        # elif name.lower() == "othervendor":
        #     from .othervendor import OtherVendorAdapter
        #     return OtherVendorAdapter()
    except Exception:
        return None
    return None
```

### Step 3: Create Config Overlay

Create `config/overlays/shannon.datainsight.yaml`:

```yaml
# DataInsight Analytics Integration
# Usage: SHANNON_CONFIG_PATH=config/overlays/shannon.datainsight.yaml

openapi_tools:
  datainsight_analytics:
    enabled: true
    spec_path: config/openapi_specs/datainsight_api.yaml
    auth_type: bearer
    auth_config:
      vendor: datainsight  # This triggers adapter loading
      token: "${DATAINSIGHT_API_TOKEN}"
      extra_headers:
        X-Account-ID: "{{body.account_id}}"  # Dynamic from request body
        X-User-ID: "${DATAINSIGHT_USER_ID}"   # Static from env
    category: analytics
    base_cost_per_use: 0.002
    rate_limit: 60
    timeout_seconds: 30
    operations:
      - queryMetrics
      - getDimensionValues
```

### Step 4: Create Vendor Role (Optional)

Create `python/llm-service/llm_service/roles/datainsight/analytics_agent.py`:

```python
"""DataInsight Analytics Agent role preset."""

ANALYTICS_AGENT_PRESET = {
    "name": "datainsight_analytics",
    "system_prompt": """You are a specialized data analytics agent with access to DataInsight Analytics API.

Your mission: Provide actionable insights from web analytics data.

## Available Tools
- queryMetrics: Retrieve metrics like users, sessions, pageviews, bounce rate
- getDimensionValues: Get dimension values (countries, devices, traffic sources)

## Output Format
Always structure your response as:
1. **dataResult** block (JSON) - for visualization
2. **Summary** section - key findings
3. **Insights** section - actionable recommendations

## Best Practices
- Always include time range in queries
- Use metric aliases: "users", "sessions", "pageviews" (auto-converted)
- Request relevant dimensions for context
- Provide comparative analysis when possible
- Highlight anomalies and trends

Remember: Your goal is to help users understand their data and make better decisions.""",

    "allowed_tools": [
        "queryMetrics",
        "getDimensionValues",
    ],

    "temperature": 0.7,
    "response_format": None,
}
```

Register in `python/llm-service/llm_service/roles/presets.py`:

```python
# At the end of _load_presets() function:
try:
    from .datainsight.analytics_agent import ANALYTICS_AGENT_PRESET
    _PRESETS["datainsight_analytics"] = ANALYTICS_AGENT_PRESET
except ImportError:
    pass  # Graceful fallback if vendor module not available
```

### Step 5: Add Environment Variables

Add to `.env`:

```bash
# DataInsight Configuration
SHANNON_CONFIG_PATH=config/overlays/shannon.datainsight.yaml
DATAINSIGHT_API_TOKEN=your_bearer_token_here
DATAINSIGHT_USER_ID=your_user_id_here

# Domain allowlist (dev: use *, prod: specific domains)
OPENAPI_ALLOWED_DOMAINS=api.datainsight.com
```

### Step 6: Test Integration

```bash
# Rebuild services
docker compose -f deploy/compose/docker-compose.yml build --no-cache llm-service orchestrator
docker compose -f deploy/compose/docker-compose.yml up -d

# Wait for health
sleep 10

# Test via gRPC
SESSION_ID="test-$(date +%s)"
grpcurl -plaintext -d '{
  "metadata": {
    "user_id": "test-user",
    "session_id": "'$SESSION_ID'"
  },
  "query": "Show me user growth trends for the past 30 days",
  "context": {
    "role": "datainsight_analytics",
    "prompt_params": {
      "account_id": "acct_12345",
      "user_id": "user_67890"
    }
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

---

## Component Guide

### 1. Vendor Adapter Class

**Purpose:** Transform requests/responses for vendor-specific API conventions

**Template:**
```python
class MyVendorAdapter:
    """Brief description of what this vendor does."""

    def transform_body(
        self,
        body: Dict[str, Any],
        operation_id: str,
        prompt_params: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """Transform request body."""
        # 1. Validate input
        if not isinstance(body, dict):
            return body

        # 2. Inject session context
        if prompt_params:
            body = self._inject_session_params(body, prompt_params)

        # 3. Apply operation-specific transformations
        if operation_id == "operation1":
            body = self._transform_operation1(body)

        # 4. Return transformed body
        return body

    def _inject_session_params(self, body: Dict, params: Dict) -> Dict:
        """Inject session context into body."""
        for key, value in params.items():
            if key not in body or not body[key]:
                body[key] = value
        return body

    def _transform_operation1(self, body: Dict) -> Dict:
        """Operation-specific transformations."""
        # Field aliasing, normalization, validation
        return body
```

**Common transformation patterns:**
- **Field aliasing**: `revenue` → `total_revenue`
- **Metric prefixing**: `users` → `my:users`
- **Time range normalization**: `{start, end}` → `{startTime, endTime}`
- **Sort format conversion**: `{field, order}` → `{column, direction}`
- **Filter structure reshaping**: list → object with logic operators
- **Default injection**: Add missing required fields from session context

### 2. Config Overlay

**Purpose:** Define vendor-specific tool configurations without modifying base config

**Template:**
```yaml
# config/overlays/shannon.myvendor.yaml

openapi_tools:
  myvendor_api:
    enabled: true
    spec_path: config/openapi_specs/myvendor_api.yaml
    auth_type: bearer | api_key | basic | none
    auth_config:
      vendor: myvendor  # Triggers adapter loading
      # Bearer auth:
      token: "${MYVENDOR_API_TOKEN}"
      # API key auth:
      # api_key_name: X-API-Key
      # api_key_location: header
      # api_key_value: "${MYVENDOR_API_KEY}"
      # Extra headers (support templates):
      extra_headers:
        X-Tenant-ID: "{{body.tenant_id}}"
        X-Custom-Header: "${STATIC_VALUE}"
    category: custom
    base_cost_per_use: 0.001
    rate_limit: 60
    timeout_seconds: 30
    max_response_bytes: 10485760
    operations:  # Optional: filter operations
      - operation1
      - operation2
    # tags:  # Optional: filter by tags
    #   - tag1
```

**Dynamic header templating:**
- `"${ENV_VAR}"` - Resolved from environment variables
- `"{{body.field}}"` - Resolved from request body at runtime
- Static strings - Used as-is

### 3. Vendor Role

**Purpose:** Specialized agent with domain-specific knowledge and tool restrictions

**Template:**
```python
"""Vendor-specific agent role."""

MY_AGENT_PRESET = {
    "name": "my_agent",

    "system_prompt": """You are a specialized agent for [domain].

Your mission: [clear objective]

## Available Tools
- tool1: [description]
- tool2: [description]

## Output Format
[specific format requirements]

## Best Practices
- [guideline 1]
- [guideline 2]

Remember: [key instruction]""",

    "allowed_tools": [
        "tool1",
        "tool2",
    ],

    "temperature": 0.7,
    "response_format": None,  # or {"type": "json_object"}

    # Optional overrides:
    # "max_tokens": 4000,
    # "top_p": 0.9,
}
```

**Registration in presets.py:**
```python
# Add to _load_presets() function:
try:
    from .myvendor.my_agent import MY_AGENT_PRESET
    _PRESETS["my_agent"] = MY_AGENT_PRESET
except ImportError:
    pass  # Vendor module not installed
```

---

## Best Practices

### 1. Keep Adapters Generic
**✅ Good:** Transform field names, inject defaults
```python
def transform_body(self, body, operation_id, prompt_params):
    # Generic field normalization
    if "start_date" in body and "startTime" not in body:
        body["startTime"] = body.pop("start_date")
    return body
```

**❌ Bad:** Business logic in adapter
```python
def transform_body(self, body, operation_id, prompt_params):
    # Don't do complex business logic here
    if body["revenue"] > 1000000:
        body["alert"] = "high_revenue"  # Belongs in application layer
```

### 2. Use Graceful Fallback
```python
try:
    from .myvendor import MyVendorAdapter
    return MyVendorAdapter()
except ImportError:
    pass  # Shannon works without vendor module
except Exception as e:
    logger.warning(f"Failed to load vendor adapter: {e}")
return None
```

### 3. Document Transformations
```python
def transform_body(self, body, operation_id, prompt_params):
    """
    Transform body for MyVendor API.

    Transformations:
    - Metric names: "users" → "mv:unique_users"
    - Time range: {start, end} → {startTime, endTime}
    - Sort format: {field, order} → {column, direction}
    - Inject tenant_id from prompt_params
    """
```

### 4. Validate Before Transforming
```python
def transform_body(self, body, operation_id, prompt_params):
    if not isinstance(body, dict):
        return body  # Don't transform non-dict

    # Validate required fields
    if operation_id == "queryData" and "metrics" not in body:
        return body  # Let API return validation error
```

### 5. Keep Secrets in Environment
**✅ Good:**
```yaml
auth_config:
  token: "${MYVENDOR_TOKEN}"
```

**❌ Bad:**
```yaml
auth_config:
  token: "sk-1234567890abcdef"  # Never hardcode!
```

### 6. Test in Isolation
```python
# tests/test_myvendor_adapter.py
def test_metric_aliasing():
    adapter = MyVendorAdapter()
    body = {"metrics": ["users", "sessions"]}
    result = adapter.transform_body(body, "queryMetrics", None)
    assert result["metrics"] == ["mv:unique_users", "mv:total_sessions"]
```

---

## Testing & Verification

### 1. Unit Test Adapter

```python
# tests/vendor/test_datainsight_adapter.py
import pytest
from llm_service.tools.vendor_adapters.datainsight import DataInsightAdapter


def test_metric_aliasing():
    adapter = DataInsightAdapter()
    body = {"metrics": ["users", "pageviews"]}
    result = adapter.transform_body(body, "queryMetrics", None)
    assert result["metrics"] == ["di:unique_users", "di:page_views"]


def test_session_param_injection():
    adapter = DataInsightAdapter()
    body = {}
    prompt_params = {"account_id": "acct_123", "user_id": "user_456"}
    result = adapter.transform_body(body, "queryMetrics", prompt_params)
    assert result["account_id"] == "acct_123"
    assert result["user_id"] == "user_456"


def test_time_range_normalization():
    adapter = DataInsightAdapter()
    body = {"timeRange": {"start": "2025-01-01", "end": "2025-01-31"}}
    result = adapter.transform_body(body, "queryMetrics", None)
    assert result["timeRange"]["startTime"] == "2025-01-01"
    assert result["timeRange"]["endTime"] == "2025-01-31"
    assert "start" not in result["timeRange"]
```

### 2. Integration Test

```bash
#!/bin/bash
# tests/e2e/test_datainsight_integration.sh

SESSION_ID="test-datainsight-$(date +%s)"

# Submit test query
grpcurl -plaintext -d '{
  "metadata": {"user_id": "test", "session_id": "'$SESSION_ID'"},
  "query": "Show user growth for the past week",
  "context": {
    "role": "datainsight_analytics",
    "prompt_params": {
      "account_id": "test_account",
      "user_id": "test_user"
    }
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask

# Poll for completion
# Check logs for adapter application
docker logs shannon-llm-service-1 --tail 100 | grep "datainsight"
```

### 3. Verify Adapter Loading

```bash
# Check logs for adapter registration
docker logs shannon-llm-service-1 | grep -i "vendor adapter"

# Should see:
# "Vendor adapter 'datainsight' applied to body for queryMetrics"
```

### 4. Test Tool Availability

```bash
# List tools
curl http://localhost:8000/tools/list | jq '.[] | select(.name | contains("query"))'

# Check schema
curl http://localhost:8000/tools/queryMetrics/schema | jq .
```

---

## Troubleshooting

### Adapter Not Loading

**Symptom:** Logs show "Vendor adapter '' applied" (empty string)

**Fix:**
```yaml
# Ensure vendor name is set in config overlay
auth_config:
  vendor: myvendor  # Must match adapter name
```

### Imports Failing

**Symptom:** `ImportError: No module named 'myvendor'`

**Fix:**
```python
# Use try/except in __init__.py
try:
    from .myvendor import MyVendorAdapter
    return MyVendorAdapter()
except ImportError as e:
    logger.warning(f"Vendor adapter not available: {e}")
    return None
```

### Transformations Not Applied

**Symptom:** API receives original body, not transformed

**Debug:**
```python
# Add logging in adapter
def transform_body(self, body, operation_id, prompt_params):
    logger.info(f"BEFORE transform: {body}")
    # ... transformations ...
    logger.info(f"AFTER transform: {body}")
    return body
```

**Check:**
1. Adapter registered in `__init__.py`
2. Vendor name matches in config
3. `auth_config.vendor` field present
4. Adapter returns modified dict (not None)

### Session Params Not Injected

**Symptom:** `prompt_params` is None in adapter

**Cause:** Orchestrator not sending session context

**Fix:** Ensure context sent in gRPC request:
```json
{
  "context": {
    "role": "my_agent",
    "prompt_params": {
      "account_id": "123",
      "user_id": "456"
    }
  }
}
```

---

## Summary

**Vendor adapter pattern provides:**
- ✅ Clean separation: generic code vs. vendor-specific
- ✅ No Shannon core changes required
- ✅ Conditional loading with graceful fallback
- ✅ Environment-based secrets management
- ✅ Testable in isolation
- ✅ Easy to maintain and extend

**Three components:**
1. **Vendor Adapter** - Request/response transformations
2. **Config Overlay** - Tool configurations
3. **Vendor Role** - Specialized agent (optional)

**Quick reference:**
```bash
# Structure
config/overlays/shannon.myvendor.yaml
config/openapi_specs/myvendor_api.yaml
python/llm-service/llm_service/tools/vendor_adapters/myvendor.py
python/llm-service/llm_service/roles/myvendor/my_agent.py

# Environment
SHANNON_CONFIG_PATH=config/overlays/shannon.myvendor.yaml
MYVENDOR_API_TOKEN=your_token

# Test
docker compose build --no-cache llm-service orchestrator
docker compose up -d
./scripts/submit_task.sh "Your query here"
```

Happy integrating! 🔌
