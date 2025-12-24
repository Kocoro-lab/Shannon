# Shannon Gateway API

A unified HTTP gateway for the Shannon multi-agent AI platform, providing REST API access to the orchestrator's gRPC services.

## Features

- **REST API** - Clean HTTP/JSON interface for task submission and status checking
- **Authentication** - API key validation using existing auth service
- **Rate Limiting** - Per-user fixed-window rate limiting (1-minute windows)
- **Idempotency** - Support for idempotent requests with `Idempotency-Key` header
- **SSE Streaming** - Real-time event streaming via Server-Sent Events
- **OpenAPI Spec** - Self-documenting API with OpenAPI 3.0 specification
- **Distributed Tracing** - Trace context propagation for debugging

## Quick Start

**Docker Compose (No Auth Required!)**: The Docker Compose setup defaults to `GATEWAY_SKIP_AUTH=1` for easy open-source adoption.

**Local Builds**: Require either setting `GATEWAY_SKIP_AUTH=1` environment variable or configuring `gateway.skip_auth: true` in `config/features.yaml`.

### Running with Docker Compose

```bash
# Build and start all services (auth disabled by default for easy start)
docker compose -f deploy/compose/docker-compose.yml up -d

# Submit your first task - no API key needed!
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query":"What is 2+2?"}'

# Check gateway health
curl http://localhost:8080/health

# View OpenAPI specification
curl http://localhost:8080/openapi.json | jq
```

### API Endpoints

#### Public Endpoints (No Auth)

- `GET /health` - Health check
- `GET /readiness` - Readiness probe
- `GET /openapi.json` - OpenAPI specification

#### Authenticated Endpoints

**Task Management:**
- `POST /api/v1/tasks` - Submit a new task
- `POST /api/v1/tasks/stream` - Submit task and receive a stream URL (201)
- `GET /api/v1/tasks` - List tasks (limit, offset, status, session_id)
- `GET /api/v1/tasks/{id}` - Get task status (includes query/session_id/mode)
- `POST /api/v1/tasks/{id}/cancel` - Cancel a running or queued task
- `GET /api/v1/tasks/{id}/events` - Get persisted event history (from Postgres)
- `GET /api/v1/tasks/{id}/timeline` - Build human‑readable timeline from Temporal history (summary/full, persist)
- `GET /api/v1/tasks/{id}/stream` - Redirect to SSE stream for this task
- `GET /api/v1/stream/sse?workflow_id={id}` - Stream task events (SSE)
- `GET /api/v1/stream/ws?workflow_id={id}` - WebSocket stream

**Approval Management:**
- `POST /api/v1/approvals/decision` - Submit approval decision for workflows requiring human approval

**Session Management:**
- `GET /api/v1/sessions` - List sessions with pagination (limit, offset)
- `GET /api/v1/sessions/{sessionId}` - Get session details
- `GET /api/v1/sessions/{sessionId}/history` - Get conversation history
- `GET /api/v1/sessions/{sessionId}/events` - Get session events (excludes LLM_PARTIAL)

##### Task Listing

```bash
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks?limit=20&offset=0&status=COMPLETED"
```

##### Event History (persistent)

```bash
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK_ID/events?limit=200"
```

##### Deterministic Timeline (Temporal replay)

```bash
# Persist derived timeline (async, 202)
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK_ID/timeline?mode=summary&persist=true"

# Preview timeline only (no DB writes, 200)
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/tasks/$TASK_ID/timeline?mode=full&include_payloads=false&persist=false" | jq
```

##### Session Management

```bash
# List all sessions for a user
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/sessions?limit=20&offset=0"

# Get session details
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/sessions/$SESSION_ID" | jq

# Get conversation history for a session
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/sessions/$SESSION_ID/history" | jq

# Get session events (excludes partial LLM responses)
curl -s -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/sessions/$SESSION_ID/events?limit=100" | jq
```

Notes:
- SSE events are stored in Redis Streams (~24h TTL) for live viewing.
- Persistent event history is stored in Postgres `event_logs`.
- Timeline API derives a human‑readable history from Temporal's canonical event store and persists it asynchronously when `persist=true`.

### Authentication

**🚀 Docker Compose Default**: Authentication is **disabled by default** in Docker Compose (`GATEWAY_SKIP_AUTH=1`) for easy getting started.

**Local Builds**: Authentication is enabled by default (`config/features.yaml` sets `gateway.skip_auth: false`). Set `GATEWAY_SKIP_AUTH=1` to disable.

**For Production**: Enable authentication by setting `GATEWAY_SKIP_AUTH=0` and using API keys:

```bash
# Enable authentication for production
export GATEWAY_SKIP_AUTH=0
docker compose up -d gateway

# Then use API keys via header (recommended)
curl -H "X-API-Key: sk_test_123456" http://localhost:8080/api/v1/tasks

# For SSE connections, also use headers (query params are not accepted)
curl -N -H "X-API-Key: sk_test_123456" \
  "http://localhost:8080/api/v1/stream/sse?workflow_id=xxx"
```

#### API Key Formats

Shannon accepts API keys in two formats:

| Format | Example | Notes |
|--------|---------|-------|
| Internal | `sk_abc123` | Standard format, used as-is |
| External | `sk-shannon-abc123` | OpenAI-style, auto-normalized to `sk_abc123` |

Both formats work with `X-API-Key` header or `Authorization: Bearer` header:

```bash
# Both of these are equivalent:
curl -H "X-API-Key: sk_test123" ...
curl -H "Authorization: Bearer sk-shannon-test123" ...
```

### Task Submission

```bash
# Default (no auth required in Docker Compose)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is 2+2?",
    "session_id": "optional-session-id"
  }'

# With authentication enabled (production)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk_test_123456" \
  -d '{"query": "What is 2+2?"}'
```

### Strategy Presets

Control research behavior with presets and overrides (mapped into context by the gateway):

```bash
# Quick strategy
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is quantum computing?",
    "research_strategy": "quick"
  }'

# Deep strategy with override
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Compare LangChain and AutoGen frameworks",
    "research_strategy": "deep",
    "max_iterations": 12
  }'

# Academic strategy
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Latest research on transformer architectures",
    "research_strategy": "academic"
  }'
```

Response:
```json
{
  "task_id": "task-00000000-0000-0000-0000-000000000001",
  "status": "QUEUED",
  "created_at": "2025-01-20T10:00:00Z"
}
```

### Submit And Get Stream URL (Recommended DX)

This convenience endpoint returns a ready-to-use SSE URL in one call.

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks/stream \
  -H "Content-Type: application/json" \
  -d '{"query": "What is 2+2?"}' | jq
```

Response (201 Created):
```json
{
  "workflow_id": "task-...",
  "task_id": "task-...",
  "stream_url": "/api/v1/stream/sse?workflow_id=task-..."
}
```

Then connect with EventSource:
```js
const r = await fetch('/api/v1/tasks/stream', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ query: 'What is 2+2?' })});
const { stream_url } = await r.json();
const es = new EventSource(stream_url);
```

### Idempotency

Prevent duplicate submissions with the `Idempotency-Key` header:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk_test_123456" \
  -H "Idempotency-Key: unique-request-id-123" \
  -d '{"query": "Process this once"}'
```

### Rate Limiting

The gateway enforces per-user fixed-window rate limits:

- Default: 60 requests per minute (1-minute fixed windows)
- Keyed by user ID (not API key)
- Headers returned: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- When exceeded: HTTP 429 with `Retry-After` header

### SSE Streaming

Stream real-time events for a task (with optional filters and resume):

```bash
# Unfiltered
curl -N -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/stream/sse?workflow_id={task_id}"

# Filter by event types
curl -N -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/stream/sse?workflow_id={task_id}&types=LLM_OUTPUT,WORKFLOW_COMPLETED"

# Resume using Last-Event-ID (Redis stream ID or numeric seq)
curl -N -H "X-API-Key: $API_KEY" \
  "http://localhost:8080/api/v1/stream/sse?workflow_id={task_id}&last_event_id=1700000000000-0"
```

Notes:
- The gateway forwards filters and accepts any event types; unknown types simply yield no events.
- `last_event_id` supports both Redis stream IDs and numeric sequences.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `ORCHESTRATOR_GRPC` | `orchestrator:50052` | Orchestrator gRPC address |
| `ADMIN_SERVER` | `http://orchestrator:8081` | Admin server for SSE proxy |
| `POSTGRES_HOST` | `postgres` | PostgreSQL host |
| `POSTGRES_PORT` | `5432` | PostgreSQL port |
| `POSTGRES_USER` | `shannon` | Database user |
| `POSTGRES_PASSWORD` | `shannon` | Database password |
| `POSTGRES_DB` | `shannon` | Database name |
| `REDIS_URL` | `redis://redis:6379` | Redis URL for rate limiting |
| `JWT_SECRET` | (required) | JWT signing secret. **Must be changed in production.** Generate with `openssl rand -base64 32` |
| `GATEWAY_SKIP_AUTH` | `0` (repo), `1` (Compose) | Skip authentication (1=disabled for easy start, 0=enabled for production) |

## Development

### Building Locally

```bash
cd go/orchestrator
go build -o gateway cmd/gateway/main.go
./gateway
```

### Running Tests

```bash
# Unit tests
cd go/orchestrator
go test ./cmd/gateway/...

# Full E2E smoke test (includes gateway)
make smoke
```

### Adding New Endpoints

1. Add handler in `cmd/gateway/internal/handlers/`
2. Register route in `cmd/gateway/main.go`
3. Update OpenAPI spec in `cmd/gateway/internal/handlers/openapi.go`
4. Add tests in `cmd/gateway/internal/handlers/`

## Architecture

The gateway acts as a thin HTTP translation layer:

```
Client → Gateway (cmd/gateway/main.go)
         ↓
         ├─→ Orchestrator gRPC (tasks, workflows)
         ├─→ Postgres (sessions, events, auth)
         ├─→ Redis (rate limiting, idempotency)
         └─→ Admin Server (SSE/WebSocket proxy)
```

Key design decisions:
- Lives in `go/orchestrator/cmd/gateway/` as a separate binary
- Uses `cmd/gateway/internal/` for gateway-specific handlers and middleware
- Direct function calls to orchestrator's internal auth service (no separate auth RPC)
- Manual HTTP handlers for precise control (no grpc-gateway auto-generation)
- Reverse proxy for SSE/WebSocket to reuse existing admin server streaming infrastructure

## Monitoring

### Health Checks

```bash
# Basic health
curl http://localhost:8080/health

# Readiness (checks orchestrator connection)
curl http://localhost:8080/readiness
```

### Metrics

The gateway logs all requests with trace IDs. View logs:

```bash
docker compose logs -f gateway
```

### Tracing

The gateway propagates trace context via:
- `traceparent` header (W3C Trace Context)
- `X-Trace-ID` header (custom)
- `X-Workflow-ID` header in responses

## Troubleshooting

### Gateway won't start

Check orchestrator is running:
```bash
docker compose ps orchestrator
curl http://localhost:50052  # Should fail but shows connectivity
```

### Authentication failures

Verify API key exists in database:
```bash
docker compose exec postgres psql -U shannon -d shannon \
  -c "SELECT * FROM auth.api_keys WHERE key_hash = encode(digest('sk_test_123456', 'sha256'), 'hex');"
```

### Rate limiting issues

Check Redis connectivity:
```bash
docker compose exec redis redis-cli ping
```

Clear rate limit for a user:
```bash
# Keys are per user and window (ratelimit:user:<USER_ID>:<window_unix>)
docker compose exec redis sh -lc 'for k in \$(redis-cli --scan --pattern "ratelimit:user:YOUR_USER_ID:*"); do redis-cli DEL "$k"; done'
```

## Security

- API keys are validated using the existing auth service
- Rate limiting prevents abuse
- Idempotency keys prevent replay attacks
- CORS headers for development (configure for production)
- All database queries use parameterized queries
- Secrets never logged

## License

Copyright (c) 2025 Shannon AI Platform
