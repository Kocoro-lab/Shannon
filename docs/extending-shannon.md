# Extending Shannon

This guide outlines simple, supported ways to extend Shannon without forking large subsystems.

---

## Quick Navigation

- [Extend Decomposition (System 2)](#extend-decomposition-system-2)
- [Add/Customize Templates (System 1)](#addcustomize-templates-system-1)
- [Add Tools Safely](#add-tools-safely)
- [**Vendor Extensions (Domain-Specific Integrations)**](#vendor-extensions)
- [Human Approval](#human-approval)
- [Feature Flags & Config](#feature-flags--config)

---

## Extend Decomposition (System 2)

- The orchestrator calls the LLM service endpoint `/agent/decompose` for planning.
- To customize planning, add a new endpoint in `python/llm-service/llm_service/api/agent.py` and switch on a feature flag or context key to route to it.
- Keep the response schema compatible with `DecompositionResponse`.

Lightweight option: add heuristics to `go/orchestrator/internal/activities/decompose.go` to pre/post‑process the LLM request/response.

---

## Add/Customize Templates (System 1)

- Place templates under your own directory and pass it to `InitTemplateRegistry`.
- Use `extends` to compose common defaults; validate with `registry.Finalize()`.
- Use the `ListTemplates` API to discover what is loaded at runtime.

---

## Add Tools Safely

- Define tools in the Python LLM service registry.
- Expose only the tools you trust via `tools_allowlist` in templates.
- For experimental integrations, keep them behind config flags.

**See:** [Adding Custom Tools Guide](adding-custom-tools.md)

---

## Vendor Extensions

**For domain-specific agents and API integrations**

Shannon provides a **vendor adapter pattern** that allows you to integrate proprietary APIs and specialized agents without modifying core Shannon code.

### What Are Vendor Extensions?

Vendor extensions consist of:
1. **Vendor Adapters** - Transform requests/responses for domain-specific APIs
2. **Config Overlays** - Vendor-specific tool configurations
3. **Vendor Roles** - Specialized agent system prompts and tool restrictions

### When to Use

Use vendor extensions when you need:
- Domain-specific API integrations (analytics, CRM, e-commerce)
- Custom field name transformations
- Specialized agent roles with domain knowledge
- Session context injection (account IDs, tenant IDs)
- Private/proprietary tool configurations

### Architecture

```
Shannon Core (Open Source)
├── Generic OpenAPI tool loader
├── Generic role system with conditional imports
└── Generic field mirroring in orchestrator

Vendor Extensions (Private)
├── config/overlays/shannon.myvendor.yaml    # Tool configs
├── config/openapi_specs/myvendor_api.yaml  # API specs
├── tools/vendor_adapters/myvendor.py        # Transformations
└── roles/myvendor/custom_agent.py           # Agent roles
```

### Quick Start

**1. Create a vendor adapter:**

```python
# python/llm-service/llm_service/tools/vendor_adapters/myvendor.py
class MyVendorAdapter:
    def transform_body(self, body, operation_id, prompt_params):
        # Field aliasing
        if "metrics" in body:
            body["metrics"] = [m.replace("users", "mv:users") for m in body["metrics"]]

        # Inject session context
        if prompt_params:
            body.update(prompt_params)

        return body
```

**2. Register the adapter:**

```python
# python/llm-service/llm_service/tools/vendor_adapters/__init__.py
def get_vendor_adapter(name: str):
    if name.lower() == "myvendor":
        from .myvendor import MyVendorAdapter
        return MyVendorAdapter()
    return None
```

**3. Create config overlay:**

```yaml
# config/overlays/shannon.myvendor.yaml
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

**4. (Optional) Create specialized agent role:**

```python
# python/llm-service/llm_service/roles/myvendor/custom_agent.py
CUSTOM_AGENT_PRESET = {
    "name": "myvendor_agent",
    "system_prompt": "You are a specialized agent for...",
    "allowed_tools": ["myvendor_query", "myvendor_analyze"],
    "temperature": 0.7,
}
```

Register in `roles/presets.py`:
```python
try:
    from .myvendor.custom_agent import CUSTOM_AGENT_PRESET
    _PRESETS["myvendor_agent"] = CUSTOM_AGENT_PRESET
except ImportError:
    pass  # Graceful fallback
```

**5. Use via environment:**

```bash
SHANNON_CONFIG_PATH=config/overlays/shannon.myvendor.yaml
MYVENDOR_API_TOKEN=your_token_here
```

### Benefits

- ✅ **Zero Shannon core changes** - All vendor logic isolated
- ✅ **Clean separation** - Generic infrastructure vs. vendor-specific
- ✅ **Conditional loading** - Graceful fallback if vendor module unavailable
- ✅ **Easy to maintain** - Vendor code in separate directories
- ✅ **Testable in isolation** - Unit test adapters independently

### Complete Documentation

For comprehensive guides including:
- Request/response transformation patterns
- Session context injection
- Custom authentication
- Testing strategies
- Best practices and troubleshooting

See: **[Vendor Adapters Guide](vendor-adapters.md)**

---

## Human Approval

- Wire `require_approval` through the SubmitTask request (now supported).
- Approval gates are enforced in the router before execution.

---

## Feature Flags & Config

- Many knobs are controlled via `config/features.yaml` and env vars, loaded through `GetWorkflowConfig`.
- Example: `TEMPLATE_FALLBACK_ENABLED=1` enables AI fallback if a template fails.

---

## Summary

| Extension Type | Complexity | Code Changes | Use Case |
|---------------|------------|--------------|----------|
| **Templates** | Low | YAML only | Repeatable workflows |
| **MCP/OpenAPI Tools** | Low | Config only | External APIs |
| **Built-in Tools** | Medium | Python only | Custom logic |
| **Vendor Adapters** | Medium | Python + Config | Domain-specific integrations |
| **Decomposition** | High | Go + Python | Custom planning logic |

For most use cases, **Templates** and **Vendor Adapters** provide the best balance of power and simplicity.

