# Tier Drift Metrics - Regression Prevention

This document explains the tier drift metrics system for monitoring model tier and provider selection accuracy.

## Purpose

The tier drift metrics help catch regressions in:
1. **Model tier selection** - Ensures requested tiers (small/medium/large) are respected
2. **Provider override** - Ensures `provider_override` context parameter is honored
3. **Fallback behavior** - Tracks when and why fallbacks occur

## Metrics

### Model Tier Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `shannon_model_tier_requested_total` | Counter | `tier` | Times each tier was requested |
| `shannon_model_tier_selected_total` | Counter | `tier`, `provider` | Times each tier+provider was selected |
| `shannon_tier_selection_drift_total` | Counter | `requested_tier`, `selected_tier`, `reason` | Drift events (tier mismatch) |

### Provider Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `shannon_provider_override_requested_total` | Counter | `provider` | Times provider override was requested |
| `shannon_provider_override_respected_total` | Counter | `provider` | Times provider override was honored |
| `shannon_provider_selection_drift_total` | Counter | `requested_provider`, `selected_provider`, `reason` | Provider drift events |

## Grafana Dashboard

Access the **Shannon - Model Tier Drift Monitoring** dashboard at:
```
http://localhost:3000/d/shannon-tier-drift
```

Login credentials (default):
- Username: `shannon`
- Password: `shannon`

### Dashboard Panels

1. **Model Tier: Requested vs Selected** - Line chart comparing tier request/selection rates
2. **Provider Override Respect Rate** - Gauge showing % of provider overrides honored (target: >95%)
3. **Tier Drift Events** - Stat panel showing drift count in last 5 minutes (alert threshold: >10)
4. **Tier Drift Details** - Time series breakdown of drift by tier and reason
5. **Provider Drift Details** - Time series of provider selection drift
6. **Model Tier Distribution** - Pie chart of tier usage by provider
7. **Tier Drift Summary Table** - Sortable table of drift events

## Alerting Rules

### Critical Alerts

**High Tier Drift Rate**
```promql
sum(rate(shannon_tier_selection_drift_total[5m])) > 0.1
```
*Fires when >0.1 drift events/second (6/minute)*

**Low Provider Override Respect Rate**
```promql
(
  sum(rate(shannon_provider_override_respected_total[5m]))
  /
  sum(rate(shannon_provider_override_requested_total[5m]))
) < 0.95
```
*Fires when <95% of provider overrides are honored*

### Warning Alerts

**Tier Drift Spike**
```promql
increase(shannon_tier_selection_drift_total[10m]) > 5
```
*Fires when >5 drift events in 10 minutes*

## Using Metrics for Testing

### Verify No Tier Drift After Changes

```bash
# Run E2E tests
make smoke

# Check tier drift metrics
curl -s http://localhost:9090/api/v1/query?query='sum(increase(shannon_tier_selection_drift_total[5m]))' | jq '.data.result[0].value[1]'

# Expected: "0" (no drift)
```

### Verify Provider Override Respect Rate

```bash
# Check respect rate
curl -s 'http://localhost:9090/api/v1/query?query=(sum(rate(shannon_provider_override_respected_total[5m]))/sum(rate(shannon_provider_override_requested_total[5m])))*100' | jq '.data.result[0].value[1]'

# Expected: "100" (100% respect rate)
```

### Monitor Tier Selection Distribution

```bash
# Get tier selection by provider
curl -s 'http://localhost:9090/api/v1/query?query=sum(increase(shannon_model_tier_selected_total[1h])) by (tier, provider)' | jq '.data.result'
```

## Instrumentation Guide

To emit tier drift metrics in workflow code:

```go
import "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"

// Record tier request
requestedTier := "medium"
metrics.RecordModelTierRequest(requestedTier)

// Record tier selection
selectedTier := "medium"
provider := "openai"
metrics.RecordModelTierSelection(selectedTier, provider)

// Record drift if tiers don't match
if requestedTier != selectedTier {
    metrics.RecordTierDrift(requestedTier, selectedTier, "tier_unavailable")
}

// Record provider override
providerOverride := "anthropic"
respected := (actualProvider == providerOverride)
metrics.RecordProviderOverride(providerOverride, respected)

// Record provider drift
if providerOverride != "" && actualProvider != providerOverride {
    metrics.RecordProviderDrift(providerOverride, actualProvider, "unavailable")
}
```

## Regression Test Checklist

After making changes to tier selection or provider override logic:

- [ ] Run `make smoke` to verify E2E tests pass
- [ ] Check Grafana dashboard for tier drift events
- [ ] Verify provider override respect rate is 100%
- [ ] Check tier drift metrics in Prometheus (`shannon_tier_selection_drift_total` should be 0)
- [ ] Run unit tests: `go test ./internal/workflows/strategies -run TestProvider`
- [ ] Run Python tests: `pytest tests/test_tier_selection.py tests/test_model_locking.py`

## Related Documentation

- **Unit Tests**: See `test_tier_selection.py`, `test_model_locking.py`, `provider_override_test.go`
- **Provider Override**: See `docs/task-submission-api.md#provider-override-precedence`
- **Model Configuration**: See `config/models.yaml` for tier definitions
- **Pricing Logic**: See `go/orchestrator/internal/pricing/pricing.go`

## Prometheus Setup

The metrics are automatically scraped from:
- **Orchestrator**: `http://orchestrator:2112/metrics`
- **Agent Core**: `http://agent-core:2113/metrics`

Scrape configuration is in `deploy/compose/grafana/config/prometheus.yml`.

To reload Prometheus config after changes:
```bash
docker compose -f deploy/compose/docker-compose.yml restart shannon-prometheus-1
```
