# Changelog

## 2025-12-03

- fix(streaming): TOOL_OBSERVATION messages now use 80 char limit via `MsgToolCompleted()` helper in gRPC path
- fix(streaming): React pattern reasoning message changed from "Analyzing the problem" to "Analyzing the progress"
- fix(streaming): Fixed "agent-undefined" in SSE events by adding proper AgentID to 8 event emissions
- refactor: Removed `observability/dashboard` web UI, replaced with native `desktop/` app (Tauri + Next.js for macOS/iOS)
- fix(decompose): Clamp Haiku max_token bug in decompose endpoint
- docs: Updated README.md, CONTRIBUTING.md, docker-compose.yml for dashboard→desktop transition

## 2025-12-01

- feat(research): Implement Deep Research 2.0 with multi-stage workflow
- fix(token-tracking): Complete synthesis recording and deprecate agent_usages field
- fix(streaming): Handle GPT-5 response formats in stream_complete
- fix(config): Resolve thread safety bug in ReloadSourceTypes
- config: Prioritize Anthropic models for small/medium tiers

## 2025-11-27 — 2025-11-29

- feat(web_fetch): Add pure-Python BFS crawler for subpages with SSRF protection
- feat(orchestrator): Add synthesis template system for output customization
- fix(web_fetch): Security and quality improvements per code review
- fix(llm-service): Align web_fetch default to Exa API; fix web_search category validation
- fix(llm-service): Fix invalid tool references in role presets
- refactor: Improve synthesis quality and citation extraction
- test(orchestrator): Add unit tests for synthesis templates

## 2025-11-22 — 2025-11-26

- feat(streaming): Implement Phase 2A multi-agent coordination events
- feat(streaming): Optimize PostgreSQL writes with event type filtering
- feat(orchestrator): Improve synthesis quality and security filtering
- feat(orchestrator): Enhance workflows with memory, citations, and token tracking
- fix(streaming): Correct workflow ID routing for forced tool SSE events
- fix(streaming): Complete SSE streaming enhancements and cleanup
- perf(orchestrator): Optimize citation sorting in research workflow
- chore(grafana): Change port from 3000 to 3030 to avoid React dev conflict

## 2025-11-17 — 2025-11-21

- fix(streaming): Restore safe streaming with usage metadata and tool events
- fix(research): Move session update after usage report to fix cost discrepancies
- fix(orchestrator): Separate timeouts in watchAndPersist to prevent persistence failures
- fix: Resolve RecordQuery race condition in workflows
- fix: UTF-8 character handling in streaming events
- fix: Resolve 2048 token limit in Deep Research sub-tasks
- refactor: Replace gpt-5-2025-08-07 with gpt-5.1; adjust xAI models to grok-3-mini/grok-4
- orchestrator(roles): Skip auto tool selection for role agents
- feat(orchestrator): Add consistent metadata output across activities

## 2025-11-10 — 2025-11-15

- feat(research): Comprehensive workflow improvements and model updates
- feat(research): Improve deep research workflow performance and quality
- fix(orchestrator): Auto-persistence, per-agent costs, and token tracking
- fix(orchestrator): Add 5-minute timeout and unit tests per PR review
- fix(research): Complete token aggregation and limit adjustments
- fix(dashboard): Repair SSE streams and radar
- add gpt-5.1 support
- chore(sdk): Bump version to 0.2.2 for research strategy CLI features

## 2025-11-06 — 2025-11-09

- feat(llm-service): Add web_fetch tool with SSRF protection and enhance web_search
- feat(citations): Entity-aware citation filtering for research workflows
- feat(orchestrator): Research strategy presets and citation collection enhancements
- feat: SSE event stream correlation and citation infrastructure
- fix(orchestrator): Prevent duplicate token recordings with budgeted execution
- fix(research): Token budget optimization, language detection, and entity filtering
- fix(sdk): Support websockets 15.x API (additional_headers)

## 2025-11-05

- fix(llm-service): Route GPT‑5 models to Responses API and prefer direct `output_text` extraction. Avoids empty results when chat returns structured parts. (See providers.md)
- fix(llm-service): Defensive content normalization for Chat API results in OpenAI and OpenAI‑compatible providers. If `message.content` is a list of parts, extract `.text` and join.
- fix(agent-core → llm-service): Align request JSON to `allowed_tools` (was `tools`). Clarifies tool gating semantics: `[]` disables tools; omitting field allows role presets.
- fix(orchestrator): Avoid stale overwrite in session update. Append assistant message directly to `sess.History` and persist once; add result length logging.
- docs: Add troubleshooting for “tokens > 0 but empty result”, GPT‑5 routing notes, and API reference for `/agent/query`.

