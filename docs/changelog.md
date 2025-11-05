# Changelog

## 2025-11-05

- fix(llm-service): Route GPT‑5 models to Responses API and prefer direct `output_text` extraction. Avoids empty results when chat returns structured parts. (See providers.md)
- fix(llm-service): Defensive content normalization for Chat API results in OpenAI and OpenAI‑compatible providers. If `message.content` is a list of parts, extract `.text` and join.
- fix(agent-core → llm-service): Align request JSON to `allowed_tools` (was `tools`). Clarifies tool gating semantics: `[]` disables tools; omitting field allows role presets.
- fix(orchestrator): Avoid stale overwrite in session update. Append assistant message directly to `sess.History` and persist once; add result length logging.
- docs: Add troubleshooting for “tokens > 0 but empty result”, GPT‑5 routing notes, and API reference for `/agent/query`.

