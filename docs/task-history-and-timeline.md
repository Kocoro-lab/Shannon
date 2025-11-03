# Task History & Timeline API

This document explains Shannon's task history APIs, persistent event storage, and the deterministic timeline derived from Temporal history.

## Overview

- Live events: Server‑Sent Events (SSE) for in‑progress tasks.
- Persistent events: human‑readable app events stored in Postgres (`event_logs`).
- Deterministic timeline: a concise, human‑oriented summary built from Temporal's canonical workflow history, optionally persisted to `event_logs`.

## Data Stores

- Temporal history: stored by Temporal in its own DB (not owned by Shannon).
- Redis Streams: live streaming buffer (~24h TTL) for SSE.
- Postgres `event_logs`: long‑term audit/events used by the dashboard and APIs.

## HTTP Endpoints (Gateway)

- `GET /api/v1/tasks` — List tasks
  - Query: `limit`, `offset`, `status` (QUEUED|RUNNING|COMPLETED|FAILED|CANCELLED|TIMEOUT), `session_id`
  - Response: `{ tasks: TaskSummary[], total_count }`
- `GET /api/v1/tasks/{id}` — Task status
  - Includes: `query`, `session_id`, `mode` for replay/continuation
  - Now also includes usage metadata populated by workflows and persisted in DB:
    - `model_used` (string), `provider` (string)
    - `usage` (object): `{ total_tokens, input_tokens?, output_tokens?, estimated_cost? }`

Example response (shape):

```json
{
  "task_id": "task-...",
  "status": "TASK_STATUS_COMPLETED",
  "result": "...",
  "model_used": "gpt-5-mini-2025-08-07",
  "provider": "openai",
  "usage": {
    "total_tokens": 300,
    "input_tokens": 200,
    "output_tokens": 100,
    "estimated_cost": 0.006
  }
}
```
- `GET /api/v1/tasks/{id}/events` — Persistent event history (from `event_logs`)
  - Query: `limit`, `offset`
  - Response: `{ events: [...], count }`
- `GET /api/v1/tasks/{id}/timeline` — Deterministic replay (Temporal → timeline)
  - Query:
    - `run_id` — optional run id
    - `mode` — `summary` (default) or `full`
    - `include_payloads` — default `false`
    - `persist` — default `true`; when true, persist asynchronously and return 202
  - Response:
    - 202 Accepted: `{ status: "accepted", workflow_id, count }` (persisting async)
    - 200 OK: `{ workflow_id, events, stats }` (preview only)

All endpoints require authentication unless `GATEWAY_SKIP_AUTH=1` is set.

## SSE vs Persistent vs Timeline

- SSE (live):
  - `GET /api/v1/stream/sse?workflow_id=...`
  - Ephemeral, resume‑friendly via `Last-Event-ID`, not stored beyond Redis TTL.

- Persistent events (`event_logs`):
  - App‑level readable events (e.g., TOOL_INVOKED, AGENT_THINKING) are written asynchronously during execution.
  - Retrieve via `GET /api/v1/tasks/{id}/events`.

- Deterministic timeline:
  - Derived from Temporal history to ensure completeness even if streaming was interrupted.
  - Built on demand; persisted asynchronously when `persist=true`.
  - Event type prefixes for provenance: `WF_`, `ACT_`, `CHILD_`, `SIG_`, `TIMER_`, `ATTR_`, `MARKER_`.

## Examples

```bash
# List recent tasks
curl -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks?limit=20&offset=0&status=COMPLETED"

# Get persistent events for a task
curl -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK/events?limit=200" | jq

# Build & persist a timeline (async), then read events
curl -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK/timeline?mode=summary&persist=true"

# Preview timeline without persisting
curl -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK/timeline?mode=full&include_payloads=false&persist=false" | jq
```

## Event Semantics

- Summary mode collapses Activity Scheduled/Started/Completed into a single row with duration and redacts large payloads.
- Full mode includes raw markers and more granular steps.
- Badges in the dashboard denote provenance:
  - `stream` — app‑emitted SSE events saved to `event_logs`.
  - `timeline` — derived from Temporal history.

## Performance & Idempotency

- Writes to `event_logs` are done asynchronously and batched to avoid impacting workflow latency.
- The timeline builder fetches Temporal history in pages and can be triggered on demand.
- Optional future enhancement: add a materialization marker to avoid re‑inserting existing timeline segments for repeated builds.

## Schema

`event_logs` (created by `migrations/postgres/004_event_logs.sql`):

```
(id UUID PK, workflow_id TEXT, type TEXT, agent_id TEXT NULL,
 message TEXT, timestamp TIMESTAMPTZ, seq BIGINT NULL, stream_id TEXT NULL,
 created_at TIMESTAMPTZ)
```

## Security

- All endpoints respect tenant/user scoping via gateway authentication.
- Timeline defaults: `mode=summary`, `include_payloads=false` to avoid sensitive data leakage.

## OpenAPI

- The gateway publishes OpenAPI at `GET /openapi.json` including these endpoints.
