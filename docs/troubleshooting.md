# Troubleshooting

## Tokens count > 0 but result is empty

Symptoms:
- Database shows `completion_tokens` > 0 but `result` is an empty string.
- Temporal/Agent‑Core logs report large token counts.
- Session history may be missing the assistant message.

Causes and fixes:
- GPT‑5 chat responses can return content as structured parts, not a plain string. Fix by routing GPT‑5 to the Responses API and preferring `output_text` (PR #67). Defensive parsing was also added for Chat API paths to join content parts.
- Cached empty response from a previous buggy parse. Clear llm‑service cache or restart the service after applying the fixes.
- Orchestrator session stale‑overwrite (historical): A save after `AddMessage` could clobber history. Fixed by appending the assistant message directly to `sess.History` and saving once.

How to verify:
- Run a complex prompt (multi‑paragraph). Ensure `result` length > 0 and session history includes the assistant entry.
- For GPT‑5 models, confirm the provider path uses Responses API.

## Tools unexpectedly enabled or disabled

Symptoms:
- The model selects or executes tools you did not intend; or it never calls tools when expected.

Cause:
- The LLM service expects `allowed_tools` in the request. Previously, Agent‑Core sent a `tools` field (ignored by the API), allowing role presets to influence tools.

Fix:
- Agent‑Core now sends `allowed_tools`. Semantics:
  - Omit field (or `null`) → role preset may allow tools.
  - Empty list `[]` → tools disabled for this request.
  - Non‑empty list → only those tools are available.

## Session result not visible in history

Cause:
- Historical stale‑overwrite during session update.

Fix:
- Update happens on a single in‑memory session struct: append assistant message, set context/metadata, then save once.

