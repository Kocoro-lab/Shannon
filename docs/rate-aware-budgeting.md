# Rate-Aware Budgeting and Control

## Overview

Shannon's rate-aware budgeting system provides intelligent rate limit management across different LLM providers and tiers, ensuring optimal throughput while respecting provider quotas.

## Architecture

### Rate Control System

```
┌─────────────────────────────────────────────────────────────┐
│                    Workflow Execution                        │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│              Middleware Budget Controller                    │
│  - Detects provider and tier from context                   │
│  - Calculates required sleep based on RPM/TPM               │
│  - Applies deterministic delays via workflow.Sleep()        │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│              Rate Control Helper                             │
│  - Loads provider limits from config                        │
│  - Tracks token and request counts                          │
│  - Computes optimal sleep durations                         │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

### Provider Rate Limits

Rate limits are configured in `config/models.yaml` under the `rate_limits` section. The Go orchestrator reads this file directly (see `go/orchestrator/internal/ratecontrol/ratecontrol.go`).

Canonical schema:

```yaml
rate_limits:
  default_rpm: 60      # global default requests per minute
  default_tpm: 100000  # global default tokens per minute

  # Optional: per-tier overrides
  tier_overrides:
    small:  { rpm: 120, tpm: 200000 }
    medium: { rpm: 60,  tpm: 100000 }
    large:  { rpm: 30,  tpm: 50000 }

  # Optional: per-provider overrides
  provider_overrides:
    openai:    { rpm: 30,  tpm: 60000 }
    anthropic: { rpm: 20,  tpm: 40000 }
    google:    { rpm: 40,  tpm: 80000 }
```

Notes:
- Tier and provider overrides are combined by taking the most constraining RPM/TPM.
- If an override is missing, the corresponding default is used.

### Default Limits

If limits are not specified in `config/models.yaml`, the system applies conservative built‑in defaults (override recommended):

| Provider | Built‑in RPM | Built‑in TPM |
|----------|--------------|--------------|
| OpenAI   | 30           | 60,000       |
| Anthropic| 20           | 40,000       |
| Google   | 40           | 80,000       |
| Others   | 45           | 90,000       |

## Implementation

### Rate Control Helper

The rate control utility lives in `go/orchestrator/internal/ratecontrol/ratecontrol.go` and exposes a single entry point:

```go
// DelayForRequest combines tier + provider limits (RPM/TPM)
// and returns a deterministic sleep duration for the request.
delay := ratecontrol.DelayForRequest(provider, tier, estimatedTokens)
```

### Middleware Integration

The budget middleware (`go/orchestrator/internal/workflows/middleware_budget.go`) calculates the delay and sleeps deterministically:

```go
// Inside BudgetPreflight or a similar pre-check
version := workflow.GetVersion(ctx, "provider_rate_control_v1", workflow.DefaultVersion, 1)
if version >= 1 {
    provider := resolveProviderFromContext(input.Context)
    tier := deriveModelTier(input.Context)
    delay := ratecontrol.DelayForRequest(provider, tier, estimatedTokens)
    if delay > 0 {
        _ = workflow.Sleep(ctx, delay)
    }
}
```

## Usage

### Automatic Rate Control

Rate control is automatically applied to all workflows when enabled:

```yaml
# In task context
context:
  model_tier: "large"        # Determines rate limits
  provider: "openai"         # Optional, inferred from model
  respect_rate_limits: true  # Enable rate control

#### Set tier via HTTP API

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Process batch data", "model_tier": "medium"}'
```
```

### Template-Based Control

Templates can specify rate control parameters:

```yaml
name: high_volume_analysis
defaults:
  model_tier: small  # Use small tier for higher RPM
  budget_agent_max: 5000

nodes:
  - id: batch_process
    type: dag
    metadata:
      rate_control:
        burst_allowed: false  # Strict rate limiting
        buffer_factor: 0.8    # Use 80% of limit for safety
```

### Manual Override

For specific tasks, override rate limits:

```go
input := TaskInput{
    Query: "Process batch data",
    Context: map[string]interface{}{
        "rate_override": map[string]interface{}{
            "rpm": 100,  // Custom RPM
            "tpm": 50000, // Custom TPM
        },
    },
}
```

## Monitoring

### Metrics

The system exposes Prometheus metrics for rate limiting:

```
# Rate limit delays applied
shannon_rate_limit_delay_seconds{provider="openai",tier="large"} 0.5

# Rate limit violations
shannon_rate_limit_exceeded_total{provider="anthropic",tier="medium"} 3

# Current rate usage
shannon_rate_usage_ratio{provider="openai",type="rpm"} 0.75
shannon_rate_usage_ratio{provider="openai",type="tpm"} 0.82
```

### Logging

Rate control events are logged for debugging:

```json
{
  "level": "info",
  "msg": "Rate limit sleep applied",
  "provider": "openai",
  "tier": "large",
  "sleep_ms": 500,
  "reason": "TPM limit",
  "token_count": 3500,
  "request_count": 2
}
```

## Best Practices

### 1. Tier Selection

Choose appropriate tiers based on workload:

- **Small tier**: High-volume, simple tasks
- **Medium tier**: Balanced performance
- **Large tier**: Complex reasoning, lower volume

### 2. Burst Management

Configure burst handling for different scenarios:

```yaml
# Steady processing
metadata:
  rate_control:
    burst_allowed: false
    smoothing: true

# Batch jobs
metadata:
  rate_control:
    burst_allowed: true
    burst_window: "5m"
```

### 3. Provider Distribution

Distribute load across providers:

```yaml
# Primary provider
- id: primary_task
  metadata:
    provider: "openai"

# Fallback provider
- id: fallback_task
  metadata:
    provider: "anthropic"
  on_fail:
    provider: "google"
```

### 4. Buffer Management

Leave headroom for rate limits:

```yaml
defaults:
  metadata:
    rate_control:
      buffer_factor: 0.9  # Use 90% of limit
```

## Integration with Budget System

### Token Budgets

Rate control works with token budgets:

```go
// Budget enforced first
if task.BudgetMax > 0 && tokensUsed >= task.BudgetMax {
    return ErrBudgetExceeded
}

// Then rate control
if sleepDuration := rc.CalculateSleep(); sleepDuration > 0 {
    workflow.Sleep(ctx, sleepDuration)
}
```

### Cost Optimization

The system optimizes for both cost and rate limits:

1. **Pattern degradation**: Reduces tokens when approaching limits
2. **Tier selection**: Chooses optimal tier for cost/performance
3. **Provider routing**: Routes to available providers

## Advanced Features

### Adaptive Rate Control

The system learns optimal rates over time:

```go
type AdaptiveController struct {
    baseRPM     int
    actualRPM   float64
    errorRate   float64
    adjustments int
}

func (ac *AdaptiveController) AdjustLimits() {
    if ac.errorRate > 0.05 { // >5% errors
        ac.baseRPM = int(float64(ac.baseRPM) * 0.9)
    } else if ac.errorRate < 0.01 { // <1% errors
        ac.baseRPM = int(float64(ac.baseRPM) * 1.05)
    }
}
```

### Priority Queues

High-priority tasks bypass rate limiting:

```yaml
context:
  priority: "critical"
  rate_control:
    bypass: true  # Skip rate limiting
```

### Rate Pooling

Share rate limits across sessions:

```yaml
context:
  rate_pool: "organization_xyz"  # Share limits
  rate_pool_weight: 0.2          # Use 20% of pool
```

## Troubleshooting

### Common Issues

#### High Latency
- Check rate limit configuration
- Verify tier selection
- Monitor actual vs configured limits

#### Rate Limit Errors
```bash
# Check current usage
curl http://localhost:2112/metrics | grep rate_usage

# View rate control logs
docker logs orchestrator | grep "rate_limit"
```

#### Configuration Issues
```bash
# Validate models.yaml
go run cmd/validate/main.go -config config/models.yaml

# Test rate calculation
go test ./internal/ratecontrol/... -v
```

## Migration Guide

### Enabling Rate Control

1. Update docker-compose.yml:
```yaml
environment:
  - ENABLE_RATE_CONTROL=true
```

2. Configure provider limits in models.yaml

3. Restart orchestrator:
```bash
docker-compose restart orchestrator
```

### Backward Compatibility

Rate control is version-gated and won't affect existing workflows:

```go
// Old workflows continue without rate control
version := workflow.GetVersion(ctx, "provider_rate_control_v1",
    workflow.DefaultVersion, 1)
if version < 1 {
    return nil // Skip rate control
}
```
