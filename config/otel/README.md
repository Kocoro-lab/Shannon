# OpenTelemetry Configuration

This directory is reserved for OpenTelemetry Collector configuration files.

## Current Status

OpenTelemetry tracing is **currently disabled** in Shannon (`tracing.enabled: false` in `config/shannon.yaml`).

## When to Use This Directory

Enable OpenTelemetry when you need:
- **Distributed tracing** across Go → Rust → Python services
- **Performance debugging** of cross-service calls
- **Request flow visualization** through Shannon's architecture
- **Integration with external observability platforms** (Jaeger, Zipkin, etc.)

## Configuration Files (When Enabled)

```yaml
# Example: otel-collector.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:

exporters:
  jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [jaeger]
```

## Enabling OpenTelemetry

1. **Update configuration:**
   ```yaml
   # config/shannon.yaml
   tracing:
     enabled: true
     service_name: "shannon-orchestrator"
     otlp_endpoint: "otel-collector:4317"
   ```

2. **Add collector to Docker Compose:**
   ```yaml
   otel-collector:
     image: otel/opentelemetry-collector-contrib:latest
     volumes:
       - ./config/otel/otel-collector.yaml:/etc/otel-collector-config.yaml
     command: ["--config=/etc/otel-collector-config.yaml"]
     ports:
       - "4317:4317"  # OTLP gRPC receiver
       - "4318:4318"  # OTLP HTTP receiver
   ```

3. **Environment variables:**
   ```bash
   OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
   OTEL_SERVICE_NAME=shannon-orchestrator
   ```

## Current Observability

Without OpenTelemetry, Shannon still provides:
- **Prometheus metrics:** ports 2112 (orchestrator), 2113 (agent-core)
- **Grafana dashboards:** `/observability/grafana/`
- **Health checks:** `curl http://localhost:8081/health`
- **Temporal UI:** http://localhost:8088

## See Also

- `/observability/README.md` - Current observability setup
- `config/shannon.yaml` - Main configuration file
- Shannon's distributed tracing spans Go → Rust → Python services