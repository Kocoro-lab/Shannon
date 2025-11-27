# Extending Shannon

This guide outlines simple, supported ways to extend Shannon without forking large subsystems.

---

## Quick Navigation

- [Extend Decomposition (System 2)](#extend-decomposition-system-2)
- [Add/Customize Templates (System 1)](#addcustomize-templates-system-1)
- [**Synthesis Templates (Output Customization)**](#synthesis-templates-output-customization)
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

## Synthesis Templates (Output Customization)

Shannon's synthesis layer combines multi-agent results into final answers. You can customize the output format without modifying core code.

### Two Customization Methods

| Method | Use Case | Precedence |
|--------|----------|------------|
| `synthesis_template` | Reusable named templates (`.tmpl` files) | Medium |
| `synthesis_template_override` | One-time verbatim prompt text | **Highest** |

### Method 1: Named Templates (`synthesis_template`)

Use pre-defined template files for consistent, reusable output formats.

**Example: Using the bullet summary template**

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What are the benefits of daily exercise?",
    "session_id": "my-session",
    "context": {
      "synthesis_template": "test_bullet_summary",
      "force_research": true
    }
  }'
```

- Template name is the filename without `.tmpl` extension
- Templates live in `config/templates/synthesis/`
- Named templates include the protected `_base.tmpl` contract (citation format `[n]`)

**Creating custom templates:**

```bash
# 1. Create your template file
cat > config/templates/synthesis/my_format.tmpl << 'EOF'
{{- template "system_contract" . -}}

# My Custom Format

{{ template "language_instruction" . }}

## Summary
Provide a 3-sentence summary.

## Key Points
List the top 5 findings as bullet points.

{{ template "citation_list" . }}
EOF

# 2. Use it via context
# context: { "synthesis_template": "my_format" }
```

### Method 2: Verbatim Override (`synthesis_template_override`)

For one-time custom formats, pass the complete synthesis prompt as text.

**Important**: This bypasses `_base.tmpl` entirely. You must:
1. Restate the citation rules (`[n]` format, matching citations array)
2. Specify not to add a `## Sources` section (system appends automatically)

**Example: Custom market research format (Chinese)**

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "日本鞋垫市场调研",
    "session_id": "market-research",
    "context": {
      "force_research": true,
      "synthesis_template_override": "你是市场分析专家...\n\n## 引用规则\n- 使用 [n] 格式引用\n- 不要添加参考来源章节\n\n## 报告结构\n### 1. 市场格局\n### 2. 价格定位\n..."
    }
  }'
```

### Override Precedence

```
synthesis_template_override  (verbatim text - HIGHEST)
    ↓
synthesis_template  (named template file)
    ↓
Auto-selected based on workflow signals
    ↓
Hardcoded fallback
```

### Auto-Selection Logic

When no explicit template is specified:

| Condition | Template Selected |
|-----------|-------------------|
| `workflow_type == "research"` | `research_comprehensive.tmpl` |
| `force_research == true` | `research_comprehensive.tmpl` |
| `synthesis_style == "comprehensive"` | `research_comprehensive.tmpl` |
| `research_areas` present | `research_comprehensive.tmpl` |
| `synthesis_style == "concise"` | `research_concise.tmpl` |
| Default | `normal_default.tmpl` |

### Available Templates

| Template | Description |
|----------|-------------|
| `_base.tmpl` | **Protected** - Citation contract (do not modify) |
| `research_comprehensive.tmpl` | Deep research with Executive Summary, Detailed Findings, Limitations |
| `research_concise.tmpl` | Shorter research format |
| `normal_default.tmpl` | Simple tasks - concise, direct answers |
| `test_bullet_summary.tmpl` | Example custom format - bullet points |

### Continuation Behavior Control

Custom templates with unusual length requirements may trigger unwanted continuation attempts. Override the minimum length threshold via `synthesis_min_length`:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "...",
    "context": {
      "synthesis_template": "test_bullet_summary",
      "synthesis_min_length": 300
    }
  }'
```

This tells the system that a 300-character response is "complete" for this template, preventing unnecessary continuation. Default thresholds: 1000 (normal), 3000 (comprehensive), 500 (concise).

### Limitation

The system automatically appends a `## Sources` section in English at the end. Your template/override should instruct the model not to create its own references section, but the system-appended heading cannot be localized without deeper changes.

**Full documentation:** See `config/templates/synthesis/README.md`

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

