# Shannon Observability

This document provides guidance for monitoring and tracing the Shannon platform.

## Tracing

Shannon implements minimal OpenTelemetry tracing with W3C traceparent header propagation.

### Go Orchestrator
- **Service**: `shannon-orchestrator`
- **Spans**: HTTP requests to LLM service, Qdrant operations, gRPC calls to agent-core
- **Headers**: W3C traceparent + X-Workflow-ID/X-Run-ID propagation
- **Export**: OTLP to configurable endpoint

### Python LLM Service
- **Service**: `shannon-llm-service`
- **Instrumentation**: FastAPI + httpx via OpenTelemetry auto-instrumentation
- **Export**: OTLP to configurable endpoint

### Configuration

Enable tracing in `config/shannon.yaml`:
```yaml
tracing:
  enabled: true
  service_name: "shannon-orchestrator"
  otlp_endpoint: "otel-collector:4317"
```

Python service uses environment variables:
```bash
OTEL_SERVICE_NAME=shannon-llm-service
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
```

### Validation

With tracing enabled, you should see:
- Go spans for embedding requests, Qdrant searches, agent execution
- HTTP requests carrying `traceparent` and `X-Workflow-ID` headers
- gRPC metadata with `x-workflow-id` and `x-run-id`
- Python spans continuing traces from Go with same trace ID

## Metrics

Each service exposes Prometheus metrics:
- **Go Orchestrator**: `:2112/metrics`
- **Agent Core**: `:2113/metrics`
- **Python LLM Service**: `:8000/metrics`

## Dashboards

Shannon includes Grafana dashboard configurations for:
- Request latency (p50/p95)
- Error rates by service
- Token usage and costs
- Workflow execution metrics
- Policy enforcement metrics
- Enforcement gateway metrics

Dashboards are stored in `observability/grafana/dashboards/`:
- `enforcement.json` - Agent Core enforcement gateway metrics
- `policy.json` - OPA policy engine metrics and canary deployment

## Alerts

Prometheus alert rules are defined in `observability/prometheus/alerts.yml` covering:
- Policy engine SLO breaches (error rate, latency)
- Service health monitoring
- Token budget violations
- Workflow performance issues

Configure Prometheus with `observability/prometheus/prometheus.yml` to load these alerts.