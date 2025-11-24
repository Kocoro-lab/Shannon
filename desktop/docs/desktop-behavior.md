# Shannon Desktop Behavior

## Task lifecycle
- Tasks are submitted via the home dialog or the run-detail chat box, hitting `POST /api/v1/tasks`. The home dialog uses fixed default `research_strategy="standard"`. In the chat box, when "Everyday Agent" is selected, no research_strategy is sent; when "Deep Research" is selected, it includes `context.force_research=true` and `research_strategy` (quick/standard/deep/academic, defaulting to "quick"), and also passes `session_id` for follow-ups.
- The run-detail page loads tasks by `id`, prefers `workflow_id` from the API for streaming, and seeds the chat with the user query plus a generating placeholder when the task is running.
- Follow-up questions require a session ID; when starting from a "new" session, the page updates the URL once a real session ID arrives from the backend.

## Streaming and timeline
- An `EventSource` subscribes to `/api/v1/stream/sse?workflow_id=...`; Redux collects `thread.message.*`, tool, agent, and workflow events for both the conversation and timeline, auto-reconnecting with backoff and `last_event_id` to resume.
- Deltas append to the latest assistant message, completions finalize it, and prompts are filtered out of the timeline; both panes auto-scroll to the newest item.
- Timeline filters let you toggle agent/llm/tool/system events.
- When streaming ends, status flips to completed; if no assistant reply is present, the client fetches the final result via `GET /api/v1/tasks/{workflow_id}` as a fallback.

### SSE payloads and completion
- Events arrive under their type names (e.g., `WORKFLOW_STARTED`, `ROLE_ASSIGNED`, `TEAM_RECRUITED/RETIRED/STATUS`, `WAITING`, `ERROR_RECOVERY`, `ERROR_OCCURRED`, `BUDGET_THRESHOLD`, `DEPENDENCY_SATISFIED`, `APPROVAL_REQUESTED/APPROVAL_DECISION`, `MESSAGE_SENT/MESSAGE_RECEIVED`, `WORKSPACE_UPDATED`, `LLM_PROMPT/OUTPUT/PARTIAL`, `TOOL_INVOKED/TOOL_OBSERVATION`, `PROGRESS`, `DELEGATION`, `DATA_PROCESSING`, `STREAM_END`). `thread.message.delta` carries `{delta, workflow_id, agent_id, seq?, stream_id?}`; `thread.message.completed` carries `{response, workflow_id, agent_id, metadata?, seq?, stream_id?}`; other types carry `{workflow_id, type, agent_id, message, payload?, timestamp, seq?, stream_id?}`.
- `WORKFLOW_STARTED` includes `payload.task_context` (force_research, research_strategy, etc.) for immediate badge/strategy rendering. The same context is seeded into `GET /api/v1/tasks/{id}` even while RUNNING, and is persisted again on completion.
- Treat `STREAM_END` (SSE name `done`/`STREAM_END`) as the definitive end of the round; `WORKFLOW_COMPLETED` precedes it. On either, stop spinners and optionally poll `GET /api/v1/tasks/{id}` to hydrate final result/metadata.

## Sessions and navigation
- The runs list links to `run-detail?session_id=...`; session history uses `GET /api/v1/sessions/{session_id}/events` (up to 100 turns) to rebuild past turns and events.
- The summary tab aggregates tokens, durations, and agent badges from turn metadata; event counts come from the live Redux event list.
- Accessing a task directly (`?id=...`) also hydrates session info and updates the URL if the task carries a session ID.

## Agent trace toggle
- The "Show/Hide Agent Trace" button filters intermediate agent messages (generalist, reasoner-*, actor-*, etc.) while always displaying final answer messages (synthesis, simple-agent, assistant).
- Hidden by default for clean UX; useful for debugging multi-agent workflows.
- See [agent-trace-feature.md](./agent-trace-feature.md) for implementation details.

## Citation rendering
- Citations appear as inline `[1]`, `[2]` links with tooltips when both markers and metadata are present.
- **Known inconsistency**: Different workflows (Supervisor/DAG/React/Simple) may produce citations differently, causing inconsistent rendering between SSE and historical sessions.
- See [citation-inconsistency-analysis.md](./citation-inconsistency-analysis.md) for root cause and normalization strategy.

## Error and loading states
- Initial load shows a spinner; failures render an error screen with a back link to `/runs`.
- Stream errors push an `error` event into Redux, trigger reconnect attempts, and surface a banner with "Retry stream" plus "Fetch final output" (manual `GET /api/v1/tasks/{workflow_id}`) when needed.
