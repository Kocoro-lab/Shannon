# API Reference (LLM Service)

## POST /agent/query

Request body (fields shown are the most relevant):
- `query` (string) – task or question.
- `context` (object, optional) – additional parameters; may include `role`, `model_tier`, `prompt_params`, `history`.
- `agent_id` (string, optional) – identifier for observability.
- `allowed_tools` (array of strings, optional) – explicit tool allowlist.
  - Omit or `null` → role presets may enable tools.
  - `[]` → tools disabled.
  - Non‑empty list → only these tools are available (names must match registered tools: built‑in, OpenAPI, MCP).
- `model_tier` (string, optional) – `small|medium|large`.
- `model_override` (string, optional) – provider‑specific model id.
- `max_tokens` (int, optional) – response limit.
- `temperature` (float, optional) – sampling.

Response body:
- `success` (bool)
- `response` (string) – final answer text.
- `tokens_used` (int) – total tokens (prompt + completion).
- `model_used` (string)
- `provider` (string)
- `metadata` (object) – may include `allowed_tools`, `role`.

Notes:
- GPT‑5 models are routed to the Responses API; the server prefers `output_text` when available.
- Chat providers defensively normalize content by joining text parts when a list is returned.

