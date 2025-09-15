# Shannon Policy Engine - Production Metrics Dashboard

## Overview

This comprehensive dashboard provides actionable insights for monitoring Shannon's OPA policy engine in production, with a focus on canary deployment safety, SLO tracking, and security incident response.

## Dashboard Import

1. Open Grafana → Dashboards → Import
2. Upload `grafana/dashboards/policy.json`
3. Ensure Prometheus is configured to scrape Shannon metrics from `:2112/metrics`

## Key Metrics & Alerts

### 🔥 Critical SLO Metrics

| Metric | Target | Alert Threshold | Description |
|--------|--------|----------------|-------------|
| **Error Rate** | <5% | >5% | Policy evaluation failures that could indicate system issues |
| **P50 Latency** | <1ms | >1ms | 50th percentile evaluation time (cached) |
| **P95 Latency** | <5ms | >5ms | 95th percentile evaluation time (includes cache misses) |
| **Cache Hit Rate** | >80% | <80% | Policy decision cache effectiveness |

### 📊 Canary Deployment Tracking

**Canary Rollout Status Panel** - Shows distribution between:
- `dry-run`: Requests that would be evaluated but always allowed
- `enforce`: Requests where policy decisions are actually enforced

**Routing Breakdown Table** - Shows why requests are routed to each mode:
- `percentage_rollout`: Normal canary percentage routing
- `explicit_enforce_user`: Users in the enforce allowlist
- `explicit_dry_run_user`: Users forced to dry-run mode
- `emergency_kill_switch`: All requests forced to dry-run (emergency state)

### 🚨 Security & Incident Response

**Top Deny Reasons** - Most frequent policy denials:
- Monitor for unusual patterns or attack vectors
- Track effectiveness of security rules
- Identify common user pain points

**Dry-Run vs Enforce Decisions** - Comparison analysis:
- Shows what decisions would be made in enforce mode vs current dry-run
- Critical for understanding impact before increasing canary percentage
- Alerts on significant divergence between modes

## Alerting Rules

### Critical Alerts (PagerDuty)

```yaml
# Error Rate SLO Breach
- alert: PolicyErrorRateHigh
  expr: rate(shannon_policy_slo_errors_total[5m]) / rate(shannon_policy_evaluations_total[5m]) * 100 > 5
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Policy engine error rate exceeds SLO"

# Latency SLO Breach
- alert: PolicyLatencyHigh
  expr: histogram_quantile(0.95, rate(shannon_policy_latency_slo_seconds_bucket[5m])) > 0.005
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Policy engine P95 latency exceeds 5ms SLO"

# Emergency Kill Switch Activated
- alert: PolicyEmergencyKillSwitch
  expr: sum(rate(shannon_policy_canary_decisions_total{routing_reason="emergency_kill_switch"}[5m])) > 0
  for: 0m
  labels:
    severity: critical
  annotations:
    summary: "Policy engine emergency kill switch is active - all requests in dry-run mode"
```

### Warning Alerts (Slack)

```yaml
# Cache Hit Rate Low
- alert: PolicyCacheHitRateLow
  expr: rate(shannon_policy_cache_hits_total[5m]) / (rate(shannon_policy_cache_hits_total[5m]) + rate(shannon_policy_cache_misses_total[5m])) * 100 < 80
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Policy cache hit rate below 80%"

# High Denial Rate
- alert: PolicyDenialRateHigh
  expr: rate(shannon_policy_evaluations_total{decision="deny"}[5m]) / rate(shannon_policy_evaluations_total[5m]) * 100 > 20
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Policy denial rate exceeds 20% - possible attack or misconfiguration"
```

## Canary Deployment Playbook

### Phase 1: Initial Canary (5% enforce)
1. **Set canary percentage**: `./scripts/policy-killswitch.sh set-canary 5`
2. **Monitor for 24h**: Watch error rate, latency, deny patterns
3. **Check divergence**: Compare dry-run vs enforce decisions
4. **SLO validation**: Ensure all metrics within bounds

### Phase 2: Gradual Rollout (5% → 25% → 50%)
1. **Increase percentage** every 24-48h if SLOs maintained
2. **Monitor user feedback** and support tickets
3. **Track top deny reasons** for new policy impacts
4. **Validate cache performance** as load increases

### Phase 3: Full Enforcement (100%)
1. **Final rollout** only after 50% stable for 1 week
2. **Disable canary mode** once confident
3. **Keep monitoring** - policies can still cause issues
4. **Document lessons learned** for next policy update

### Emergency Procedures

**Immediate Rollback (Kill Switch)**:
```bash
./scripts/policy-killswitch.sh enable-killswitch
```

**Gradual Rollback**:
```bash
./scripts/policy-killswitch.sh set-canary 0  # Move to 0% enforce
```

**Add Problem User to Dry-Run**:
```bash
./scripts/policy-killswitch.sh add-dry-run-user problematic_user_id
```

## Metric Details

### Core Metrics
- `shannon_policy_evaluations_total` - Total evaluations by decision/mode/reason
- `shannon_policy_evaluation_duration_seconds` - Latency histogram by mode
- `shannon_policy_slo_errors_total` - Errors for SLO calculation by category
- `shannon_policy_latency_slo_seconds` - Enhanced latency tracking with cache labels

### Canary Metrics
- `shannon_policy_canary_decisions_total` - Canary routing decisions by reason/mode
- `shannon_policy_mode_comparison_total` - Dry-run vs enforce comparison
- `shannon_policy_cache_hits_total` / `shannon_policy_cache_misses_total` - Cache performance

### Insights Metrics
- `shannon_policy_deny_reasons_total` - Top denial reasons (hashed for cardinality)
- `shannon_policy_version_info` - Policy version tracking
- `shannon_policy_cache_entries` - Current cache size

## Dashboard Usage Tips

1. **Set appropriate time ranges** - Use 1h for real-time, 24h for trends
2. **Filter by effective_mode** - Compare dry-run vs enforce behavior
3. **Use alerts for proactive monitoring** - Don't rely on manual dashboard checking
4. **Correlate with application metrics** - Policy issues often show up in app performance
5. **Review weekly** - Look for trends in denial patterns and performance

## Troubleshooting Common Issues

### High Latency
- Check cache hit rate - low cache performance indicates too much variation in requests
- Look at policy complexity - complex rules slow down evaluation
- Review query patterns - some patterns may be expensive to evaluate

### High Error Rate
- Check policy syntax - .rego file errors cause evaluation failures
- Validate input format - malformed inputs cause conversion errors
- Review logs - error details in orchestrator logs

### High Denial Rate
- Compare with security events - may indicate actual attacks
- Check policy changes - new rules may be too restrictive
- Review user patterns - legitimate usage patterns may have changed

### Low Cache Hit Rate
- High variation in requests (users, queries, contexts)
- Cache size too small for workload
- TTL too short for request patterns

## Integration with Other Systems

### Security Information and Event Management (SIEM)
- Export high denial rate events
- Correlate with authentication failures
- Track policy bypass attempts

### Application Performance Monitoring (APM)
- Correlate policy latency with application response times
- Track policy evaluation as part of request traces
- Monitor policy impact on user experience

### Incident Response
- Policy metrics often first indicators of attacks
- Use dashboard during incident investigation
- Historical data helps with forensic analysis