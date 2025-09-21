# Shannon Gateway API

A unified HTTP gateway for the Shannon multi-agent AI platform, providing REST API access to the orchestrator's gRPC services.

## Features

- **REST API** - Clean HTTP/JSON interface for task submission and status checking
- **Authentication** - API key validation using existing auth service
- **Rate Limiting** - Per-API-key token bucket rate limiting
- **Idempotency** - Support for idempotent requests with `Idempotency-Key` header
- **SSE Streaming** - Real-time event streaming via Server-Sent Events
- **OpenAPI Spec** - Self-documenting API with OpenAPI 3.0 specification
- **Distributed Tracing** - Trace context propagation for debugging

## Quick Start (No Auth Required!)

By default, the gateway runs with `GATEWAY_SKIP_AUTH=1` for easy open-source adoption. You can start using Shannon immediately without setting up authentication.

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

- `POST /api/v1/tasks` - Submit a new task
- `GET /api/v1/tasks` - List tasks (limit, offset, status, session_id)
- `GET /api/v1/tasks/{id}` - Get task status (includes query/session_id/mode)
- `GET /api/v1/tasks/{id}/events` - Get persisted event history (from Postgres)
- `GET /api/v1/tasks/{id}/timeline` - Build human‑readable timeline from Temporal history (summary/full, persist)
- `GET /api/v1/tasks/{id}/stream` - Stream task events (SSE)
- `GET /api/v1/stream/sse` - Direct SSE stream
- `GET /api/v1/stream/ws` - WebSocket stream

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

Notes:
- SSE events are stored in Redis Streams (~24h TTL) for live viewing.
- Persistent event history is stored in Postgres `event_logs`.
- Timeline API derives a human‑readable history from Temporal’s canonical event store and persists it asynchronously when `persist=true`.

### Authentication

**🚀 Open Source Default**: Authentication is **disabled by default** (`GATEWAY_SKIP_AUTH=1`) for easy getting started.

**For Production**: Enable authentication by setting `GATEWAY_SKIP_AUTH=0` and using API keys:

```bash
# Enable authentication for production
export GATEWAY_SKIP_AUTH=0
docker compose up -d gateway

# Then use API keys
curl -H "X-API-Key: sk_test_123456" http://localhost:8080/api/v1/tasks

# Or via query parameter (for SSE connections)
curl "http://localhost:8080/api/v1/stream/sse?api_key=sk_test_123456&workflow_id=xxx"
```

### Task Submission

```bash
# Default mode (no auth required)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is 2+2?",
    "session_id": "optional-session-id",
    "mode": "simple"
  }'

# With authentication enabled (production)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk_test_123456" \
  -d '{"query": "What is 2+2?"}'
```

Response:
```json
{
  "task_id": "task-00000000-0000-0000-0000-000000000001",
  "status": "submitted",
  "created_at": "2025-01-20T10:00:00Z"
}
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

The gateway enforces per-API-key rate limits:

- Default: 60 requests per minute
- Headers returned: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- When exceeded: HTTP 429 with `Retry-After` header

### SSE Streaming

Stream real-time events for a task:

```bash
# Direct task streaming
curl -N -H "X-API-Key: sk_test_123456" \
  "http://localhost:8080/api/v1/tasks/{task_id}/stream"

# Or use the general SSE endpoint
curl -N "http://localhost:8080/api/v1/stream/sse?api_key=sk_test_123456&workflow_id={task_id}"
```

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
| `JWT_SECRET` | `your-secret-key` | JWT signing secret |
| `GATEWAY_SKIP_AUTH` | `1` | Skip authentication (1=disabled for easy start, 0=enabled for production) |

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

# Integration tests
./scripts/test_gateway.sh
```

### Adding New Endpoints

1. Add handler in `internal/handlers/`
2. Register route in `main.go`
3. Update OpenAPI spec in `internal/handlers/openapi.go`
4. Add tests

## Architecture

The gateway acts as a thin HTTP translation layer:

```
Client → Gateway → Orchestrator (gRPC)
         ↓
         Admin Server (SSE proxy)
```

Key design decisions:
- Lives inside orchestrator module for internal package access
- Direct function calls to auth service (not RPC)
- Manual HTTP handlers for v0.1 (no grpc-gateway)
- Reverse proxy for SSE to reuse existing streaming

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

Clear rate limit for a key:
```bash
docker compose exec redis redis-cli DEL "ratelimit:apikey:YOUR_KEY_ID"
```

## Security

- API keys are validated using the existing auth service
- Rate limiting prevents abuse
- Idempotency keys prevent replay attacks
- CORS headers for development (configure for production)
- All database queries use prepared statements
- Secrets never logged

## License

Copyright (c) 2025 Shannon AI Platform
