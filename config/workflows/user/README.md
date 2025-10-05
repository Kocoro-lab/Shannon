# User-Defined Workflow Templates

This directory is for **custom workflow templates** specific to your deployment or organization. Templates placed here are loaded alongside the built-in examples.

## Purpose

- ✅ Add custom workflows without modifying Shannon core
- ✅ Override built-in templates by using the same `name@version`
- ✅ Keep private/proprietary workflows separate from the codebase
- ✅ Enable per-customer customization in multi-tenant deployments

## Usage

1. **Create a template file** (e.g., `my_workflow.yaml`):

```yaml
name: my_custom_workflow
version: "1.0"
description: "My organization's specific workflow"

defaults:
  model_tier: medium
  budget_agent_max: 5000

nodes:
  - id: step1
    type: simple
    strategy: react
    budget_max: 2000

  - id: step2
    type: cognitive
    strategy: chain_of_thought
    depends_on: [step1]
    budget_max: 3000

edges:
  - from: step1
    to: step2
```

2. **Templates are auto-loaded** on orchestrator startup
3. **Use in requests**:

```bash
grpcurl -plaintext -d '{
  "metadata": {"userId":"user","sessionId":"session"},
  "query": "Analyze this codebase",
  "context": {"template_name": "my_custom_workflow"}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

## Directory Structure

```
config/workflows/
├── examples/          # Built-in Shannon templates (version controlled)
│   ├── research_summary.yaml
│   ├── market_analysis.yaml
│   └── ...
└── user/              # Your custom templates (gitignored)
    ├── custom_code_review.yaml (sample)
    └── my_workflow.yaml
```

## Template Schema

See `examples/` for reference templates. Key fields:

- `name` (required): Unique template identifier
- `version` (optional): Template version for overrides
- `description` (optional): What this template does
- `defaults`: Default model tier, budget, approval settings
- `nodes`: Workflow steps with strategy and dependencies
- `edges`: Execution flow between nodes

## Validation

Validate your templates before deployment:

```bash
./scripts/validate-templates.sh config/workflows
```

## Best Practices

1. **Use semantic versioning**: `name@1.0`, `name@1.1`, `name@2.0`
2. **Test with small budgets first**: Avoid token waste during development
3. **Document metadata**: Add author, tags, category for discoverability
4. **Version control separately**: Keep user/ templates in a private repo if needed
5. **Monitor template metrics**: Check `shannon_template_executions_total` in Prometheus

## Example: Custom Code Review (Included)

See `custom_code_review.yaml` for a complete example that demonstrates:
- Multi-stage workflow with parallel paths
- Security and performance analysis
- Debate pattern for quality review
- Reflection for report generation

## Notes

- This directory is **gitignored** (except `.gitkeep` and `README.md`)
- Templates override by `name@version` - last loaded wins
- Empty `version` matches any version during lookup
- Invalid YAML files are logged but won't prevent startup
