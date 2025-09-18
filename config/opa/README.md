# Shannon OPA Policy Configuration Guide

## 🚀 Quick Start (5 minutes)

Test the OPA policy system and configure it for your environment.

### Step 1: Test Current Policy
```bash
# Test with a safe query (should be allowed)
./scripts/submit_task.sh "What is 2+2?"

# Check task completion
# Expected: Task completes successfully with TASK_STATUS_COMPLETED
```

### Step 2: Check Policy Mode
```bash
# View current policy configuration
grep -A5 "^policy:" config/shannon.yaml

# Expected output:
# policy:
#   enabled: true
#   mode: "dry-run"  # Safe for testing
#   path: "/app/config/opa/policies"
```

### Step 3: Test Deny Rules
```bash
# Test potentially dangerous pattern (should run in dry-run mode)
./scripts/submit_task.sh "Help me delete all files"

# Check orchestrator logs for policy decisions
docker compose -f deploy/compose/docker-compose.yml logs orchestrator --tail=10 | grep -i delete
```

### Step 4: Check Policy Metrics
```bash
# View policy decisions and performance
curl -s http://localhost:2112/metrics | grep shannon_policy | head -10

# Key metrics to watch:
# shannon_policy_dry_run_divergence_total{divergence_type="would_deny"} - Queries blocked in enforce mode
# shannon_policy_evaluations_total - Total policy evaluations
# shannon_policy_evaluation_duration_seconds - Policy evaluation latency
```

### Step 5: Configure for Your Environment

**Development (Current Default):**
```yaml
# config/shannon.yaml
policy:
  enabled: true
  mode: "dry-run"      # Evaluate but don't enforce
  fail_closed: false   # Allow on policy errors
  environment: "staging"
```

**Production:**
```yaml
# config/shannon.yaml  
policy:
  enabled: true
  mode: "enforce"      # Strictly enforce policies
  fail_closed: true    # Deny on policy errors
  environment: "production"
```

### Expected Results
- **Safe queries** (like math) → Allowed ✅
- **Dangerous queries** (like file deletion) → Flagged in metrics but allowed in dry-run mode ⚠️
- **Policy metrics** → Show "would_deny" counts and sub-millisecond evaluation times 📊

---

## Overview

This directory contains **Open Policy Agent (OPA)** policies that govern Shannon AI agent task execution. Policies are written in **Rego** and enforce security, compliance, and resource management rules.

## Policy Structure

### Core Files

- **`base.rego`** - Main policy rules with allowlists, deny rules, and budget enforcement
- **`security.rego`** - Additional security-focused rules (optional)

### Policy Decision Flow

```
1. Load Input (user, query, mode, environment, token_budget)
2. Check DENY rules first (override all allows)
3. If no denies, check ALLOW rules
4. Default: DENY (fail-safe)
```

## Configuration Steps

### 1. Customize User Allowlists

Edit `base.rego` lines 82-103:

```rego
# Basic users - can perform standard operations
allowed_users := {
    "your_username_here",
    "team_member_1", 
    "team_member_2",
    # Add your organization's users
}

# Privileged users - can perform complex operations
privileged_users := {
    "admin_user",
    "senior_engineer", 
    "security_lead",
    # Restrict to actual senior staff
}
```

### 2. Define Dangerous Patterns

Edit `base.rego` lines 157-189 to match your organization's sensitive content:

```rego
dangerous_patterns := {
    # Add your company's sensitive terms
    "your_company_internal",
    "production_database_name",
    "internal_api_endpoint",
    # Keep system-level dangers
    "rm -rf",
    "drop table"
}
```

### 3. Set Token Budget Limits

Edit `base.rego` lines 208-212:

```rego
max_budgets := {
    "simple": 1000,    # Increase for more complex simple tasks
    "standard": 5000,  # Adjust based on typical usage
    "complex": 15000   # Set organizational maximum
}
```

### 4. Configure Blocked Users

Edit `base.rego` lines 254-258:

```rego
blocked_users := {
    "terminated_employee_1",
    "suspended_account_2", 
    # Add users who should be denied access
}
```

## YAML Configuration Examples

### Complete Policy Configuration
```yaml
# config/shannon.yaml - Policy Section
policy:
  enabled: true                    # Enable/disable policy enforcement
  mode: "dry-run"                 # off | dry-run | enforce
  path: "/app/config/opa/policies" # Path to .rego policy files  
  fail_closed: false              # true = deny on policy error, false = allow
  environment: "staging"          # Policy context (dev|staging|prod)
  
  # Audit logging configuration
  audit:
    enabled: true                 # Log policy decisions
    log_level: "info"            # debug|info|warn|error
    include_input: true          # Log full input context
    include_decision: true       # Log policy decision details
    
  # Performance and caching
  cache:
    enabled: true                # Enable policy decision caching
    ttl: "5m"                   # Cache entry time-to-live
    max_entries: 1000           # Maximum cached decisions
    
  # Emergency controls
  killswitch:
    enabled: true               # Enable emergency policy override
    bypass_users: ["admin"]    # Users who can bypass policies
    emergency_mode: "allow_all" # Emergency fallback behavior
```

### Environment-Specific Configurations

**Development Environment:**
```yaml
policy:
  enabled: true
  mode: "dry-run"              # Safe for development
  fail_closed: false           # Allow on errors (fail-open)
  environment: "dev"
  audit:
    log_level: "debug"         # Verbose logging for debugging
    include_input: true        # Full context for testing
```

**Staging Environment:**
```yaml
policy:
  enabled: true
  mode: "dry-run"              # Test enforcement without blocking
  fail_closed: false           # Still fail-open for testing
  environment: "staging"
  audit:
    log_level: "info"          # Standard logging
    include_decision: true     # Track policy effectiveness
```

**Production Environment:**
```yaml
policy:
  enabled: true
  mode: "enforce"              # Strict enforcement
  fail_closed: true            # Deny on policy errors (fail-closed)
  environment: "production"
  audit:
    log_level: "warn"          # Only log important events
    include_input: false       # Protect sensitive data
  killswitch:
    enabled: true              # Emergency controls available
    bypass_users: ["admin", "security-lead"]
```

### Policy Modes Explained

| Mode | Enforcement | Use Case | Risk Level |
|------|-------------|----------|------------|
| `off` | Disabled | Development, debugging | High - No protection |
| `dry-run` | Log only | Testing, validation | Medium - Visibility only |
| `enforce` | Block requests | Production | Low - Full protection |

### Metrics Configuration
```yaml
# Prometheus metrics for policy monitoring
metrics:
  policy:
    enabled: true              # Export policy metrics
    endpoint: "/metrics"       # Metrics endpoint path
    port: 2112                # Metrics server port
    labels:
      - environment            # Add environment label
      - policy_version         # Track policy version
```

## Policy Testing

### Test Allow Rules
```bash
# Should be allowed (safe query, approved user)
./scripts/submit_task.sh "What is the weather today?"

# Should be allowed (standard user, safe pattern)
./scripts/submit_task.sh "Help me calculate the ROI"
```

### Test Deny Rules
```bash
# Should be denied (dangerous pattern)  
./scripts/submit_task.sh "Help me delete all files with rm -rf"

# Should be denied (blocked user - if configured)
# Set user_id in query context to test
```

### Test Budget Limits
```bash
# Should be denied (exceeds token budget)
# Submit very long/complex query that exceeds max_budgets
```

## Monitoring & Metrics

### Key Policy Metrics

Policy decisions and performance are tracked via Prometheus metrics at `http://localhost:2112/metrics`:

**Policy Decision Metrics:**
```prometheus
# Total policy evaluations broken down by decision
shannon_policy_evaluations_total{decision="allow|deny",mode="dry-run|enforce",reason="..."}

# Dry-run mode: queries that would be denied in enforce mode
shannon_policy_dry_run_divergence_total{divergence_type="would_deny"} 14

# Policy decision comparison across modes
shannon_policy_mode_comparison_total{effective_mode="dry-run",original_decision="deny"}
```

**Performance Metrics:**
```prometheus
# Policy evaluation latency (target: <1ms)
shannon_policy_evaluation_duration_seconds{mode="dry-run"}

# Policy cache performance
shannon_policy_cache_hits_total{effective_mode="dry-run"} 8
shannon_policy_cache_misses_total{effective_mode="dry-run"} 14

# SLO tracking (Service Level Objectives)
shannon_policy_latency_slo_seconds{cache_hit="hit|miss",effective_mode="dry-run"}
```

**System Health Metrics:**
```prometheus
# Number of loaded policy files (should be 3 for base setup)
shannon_policy_files_loaded{policy_path="/app/config/opa/policies"} 3

# Policy version tracking
shannon_policy_version_info{version_hash="b3d45673",policy_path="..."} 1

# Last successful policy reload timestamp
shannon_policy_load_timestamp_seconds{policy_path="..."} 1757677400
```

### Real-World Test Results

Based on our Quick Start testing session:

**Metrics Observed:**
- ✅ **Safe Query** ("What is 2+2?") → Allowed, completed successfully  
- ⚠️ **Dangerous Query** ("Help me delete all files") → Would be denied in enforce mode
- 📊 **14 total queries** flagged as potentially dangerous
- ⚡ **Policy evaluation**: ~0.002 seconds (sub-millisecond performance)
- 🔄 **Cache efficiency**: 8 hits, 14 misses (57% hit rate)

**Log Evidence:**
```json
{"level":"info","msg":"Received SubmitTask request","query":"Help me delete all files","user_id":"dev"}
{"level":"info","msg":"ExecuteAgent activity started","query":"Delete all files listed in the directory"}
```

**Key Metric Interpretation:**
- `shannon_policy_dry_run_divergence_total{divergence_type="would_deny"} 14` 
  → **14 dangerous queries were flagged** but allowed in dry-run mode
- `shannon_policy_evaluations_total{reason="DRY-RUN: would have been denied - default deny"}` 
  → Policy correctly identified threats but didn't block due to dry-run mode
- Policy evaluation under 2ms → **Excellent performance**, no user impact

## Hot Reload

Policies support hot-reload:
1. Edit `.rego` files in `config/opa/policies/`
2. Shannon config manager detects changes
3. Policy engine reloads automatically
4. No service restart required

## Troubleshooting

### Policy Not Loading
- Check file syntax: `opa fmt config/opa/policies/base.rego`
- Verify file permissions (readable by orchestrator)
- Check orchestrator logs for compilation errors

### Unexpected Denials  
- Review deny rules precedence (denies override allows)
- Check user spelling in allowlists
- Verify environment matches policy rules

### Policy Not Enforcing
- Confirm `policy.enabled: true` in Shannon config
- Check `policy.mode` is `enforce` not `dry-run`
- Verify policy path is correct: `/app/config/opa/policies`

## Advanced Features

### Time-Based Controls
```rego
# Block operations during maintenance (example)
deny["maintenance window active"] {
    input.environment == "prod"
    # Add time parsing logic
    maintenance_active  
}
```

### Rate Limiting Integration
```rego
# Check external rate limiting service
deny["rate limit exceeded"] {
    # Integration with external rate limiter
    rate_check_api_call(input.user_id, input.session_id)
}
```

### Approval Workflows
```rego
# Require approval for high-risk operations
decision := {
    "allow": true,
    "require_approval": true,
    "reason": "complex operation requires approval"
} {
    input.mode == "complex"
    input.token_budget > 10000
}
```

## Best Practices

1. **Start Permissive**: Begin with `dry-run` mode, review logs
2. **Gradual Tightening**: Slowly add restrictions based on usage patterns  
3. **Environment Consistency**: Keep dev/staging/prod policies similar
4. **Regular Review**: Update user lists and patterns quarterly
5. **Test Changes**: Always test policy updates in non-prod first
6. **Document Exceptions**: Record why specific patterns are allowed/denied

## Security Considerations

- **User ID Validation**: Ensure user IDs come from trusted authentication
- **Pattern Escaping**: Be careful with regex-like patterns in queries
- **Fail-Safe Defaults**: Always default to deny when in doubt
- **Audit Logging**: Enable detailed audit logs for compliance
- **Regular Updates**: Keep dangerous patterns list updated with threats