# Providers and Routing

## GPT‑5 family routing

- GPT‑5 models are routed to the OpenAI Responses API.
- Prefer `output_text` when present to avoid empty content when the API returns structured blocks.
- A low reasoning effort is used during synthesis to encourage producing final text.

Why:
- Some GPT‑5 chat responses return content as structured parts; using Responses API avoids empty `message.content`.

## Chat content normalization (defense in depth)

- For Chat Completions (OpenAI and OpenAI‑compatible), if `message.content` is a list, extract `.text` from each part and join.
- This provides backward compatibility and protects against provider variations.

## Tool gating semantics

- The LLM service expects `allowed_tools` in `/agent/query`.
- Semantics:
  - Omit field → role presets may enable tools.
  - `[]` → tools disabled.
  - `["name", …]` → only those tools are available (built‑in, OpenAPI, or MCP by registered name).

