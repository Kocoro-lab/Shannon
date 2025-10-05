# Extending Shannon

This guide outlines simple, supported ways to extend Shannon without forking large subsystems.

## Extend Decomposition (System 2)

- The orchestrator calls the LLM service endpoint `/agent/decompose` for planning.
- To customize planning, add a new endpoint in `python/llm-service/llm_service/api/agent.py` and switch on a feature flag or context key to route to it.
- Keep the response schema compatible with `DecompositionResponse`.

Lightweight option: add heuristics to `go/orchestrator/internal/activities/decompose.go` to pre/post‑process the LLM request/response.

## Add/Customize Templates (System 1)

- Place templates under your own directory and pass it to `InitTemplateRegistry`.
- Use `extends` to compose common defaults; validate with `registry.Finalize()`.
- Use the `ListTemplates` API to discover what is loaded at runtime.

## Add Tools Safely

- Define tools in the Python LLM service registry.
- Expose only the tools you trust via `tools_allowlist` in templates.
- For experimental integrations, keep them behind config flags.

## Human Approval

- Wire `require_approval` through the SubmitTask request (now supported).
- Approval gates are enforced in the router before execution.

## Feature Flags & Config

- Many knobs are controlled via `config/features.yaml` and env vars, loaded through `GetWorkflowConfig`.
- Example: `TEMPLATE_FALLBACK_ENABLED=1` enables AI fallback if a template fails.

