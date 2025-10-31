# HTTP Task Submission API

This page documents all parameters for submitting tasks via the HTTP Gateway.

## Endpoints

- POST `/api/v1/tasks` — submit a task
- POST `/api/v1/tasks/stream` — submit and receive a stream URL (201)

Response headers include `X-Workflow-ID` and `X-Session-ID`.

## Top‑Level Body

- `query` (string, required) — user query or command
- `session_id` (string, optional) — session continuity (UUID or custom)
- `context` (object, optional) — execution context (see below)
- `mode` (string, optional) — `simple` | `supervisor` (routes via labels)
- `model_tier` (string, optional) — `small` | `medium` | `large`
  - Injected into `context.model_tier` and honored by services

Example:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Summarize our Q3 results",
    "session_id": "sales-2025-q3",
    "mode": "supervisor",
    "model_tier": "large",
    "context": {
      "role": "analysis",
      "prompt_params": {"profile_id": "49598h6e", "current_date": "2025-10-25"}
    }
  }'
```

## Recognized `context.*` Keys

- `role` — role preset (e.g., `analysis`, `research`, `writer`)
- `system_prompt` — overrides role prompt; supports `${var}` from `prompt_params`
- `prompt_params` — arbitrary params for prompts/tools/adapters
- `model_tier` — fallback when top‑level not provided
- `model_override` — specific model (e.g., `gpt-4.1`)
  - Note: does not force provider selection by itself
- `template` — template name
- `template_version` — template version (string)
- `template_name` — alias for `template` (accepted)
- `disable_ai` — true to require template only (no AI fallback)
- Advanced context window controls:
  - `history_window_size`, `use_case_preset`, `primers_count`, `recents_count`
  - `compression_trigger_ratio`, `compression_target_ratio`

## Common Scenarios

- Force model tier (top‑level wins):

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Complex analysis", "model_tier": "large"}'
```

- Force specific model:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Write a plan", "context": {"model_override": "gpt-4.1"}}'
```

- Template‑only execution:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Weekly research briefing",
    "context": {"template": "research_summary", "template_version": "1.0.0", "disable_ai": true}
  }'
```

- Supervisor mode:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Assess system reliability", "mode": "supervisor"}'
```

## Validation & Priority Rules

- `model_tier`: only `small|medium|large` (400 on invalid)
- `mode`: only `simple|supervisor` (400 on invalid)
- Top‑level `model_tier` overrides `context.model_tier`
- `template_name` is accepted as an alias for `template`

## Notes

- `model_override` selects a concrete model but provider choice follows tier routing.
- Use header `Idempotency-Key` to safely retry submissions; gateway caches 2xx responses for 24h.
- SSE streaming endpoint is returned by `POST /api/v1/tasks/stream`.

