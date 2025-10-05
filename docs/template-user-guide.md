# Getting Started with Templates

This short guide shows how to create, load, and run a template‑based workflow (System 1).

## 1) Create a Template

Create a YAML file under `config/workflows/examples/` (or your own folder). Minimal example:

```yaml
name: simple_analysis
version: "1.0.0"
defaults:
  model_tier: medium
  budget_agent_max: 5000
  require_approval: false

nodes:
  - id: analyze
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    budget_max: 500
    depends_on: []
```

Tips:
- `type`: `simple | cognitive | dag | supervisor`
- `strategy`: `react | chain_of_thought | reflection | debate | tree_of_thoughts`
- Set per‑node `budget_max` and `tools_allowlist` to constrain execution.

## 2) Load Templates

Templates are loaded at orchestrator startup via `InitTemplateRegistry` and can come from one or more directories. See `go/orchestrator/internal/workflows/template_catalog.go`.

## 3) List Available Templates

Use the new gRPC API:

```
grpcurl -plaintext -d '{}' localhost:50052 \
  shannon.orchestrator.OrchestratorService/ListTemplates
```

Response contains `name`, `version`, `key`, and `content_hash`.

## 4) Execute a Template

Request template execution by name/version and optionally disable AI:

```
grpcurl -plaintext -d '{
  "query": "Summarize this week's tech news",
  "context": {
    "template": "simple_analysis",
    "template_version": "1.0.0",
    "disable_ai": true
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

Notes:
- When `disable_ai` is true and the template is missing, the request fails fast.
- When `workflows.templates.fallback_to_ai` (or `TEMPLATE_FALLBACK_ENABLED=1`) is enabled, failed template runs can fall back to AI decomposition.

## 5) Best Practices

- Keep nodes small and deterministic; prefer more nodes over large monoliths.
- Restrict tools explicitly per node.
- Set `defaults.require_approval` when human sign‑off is needed.
- Use `extends` to share defaults; verify with `registry.Finalize()`.
