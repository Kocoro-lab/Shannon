# Templates Examples

This folder contains small, runnable templates you can load into Shannon.

- basic-report.yaml — tiny 3‑step plan (discover → analyze → finalize)

Quick start

1) Make these templates discoverable by the orchestrator

```bash
export TEMPLATES_PATH="$(pwd)/docs/example-usecases/templates"
make down && make dev
```

2) List loaded templates (optional)

```bash
grpcurl -plaintext -d '{}' localhost:50052 \
  shannon.orchestrator.OrchestratorService/ListTemplates
```

3) Execute a template (example: basic_report)

- Use the Python SDK or the gRPC/HTTP gateway. With the SDK:

```python
from shannon.client import ShannonClient

with ShannonClient() as client:
    handle = client.submit_task(
        "Create a concise product market snapshot for ACME Gadget.",
        session_id="report-demo",
        user_id="analyst-1",
        context={"template": "basic_report", "template_version": "v1"},
        # disable_ai=True,  # optional: require template-only execution
    )
    print(handle.result())
```

Notes
- The compiler prefers `allowed_tools` and falls back to legacy `tools_allowlist` automatically.
- Pattern degradation is budget‑based and automatic; `on_fail` fields are validated but not fully enforced at runtime yet.
- See docs/templates.md for the full templates guide (structure, routing, degradation, best practices).
