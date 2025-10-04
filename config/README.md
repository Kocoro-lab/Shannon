# Shannon Configuration Directory

This directory contains all configuration files for the Shannon AI platform. Each YAML file serves a specific purpose in configuring different aspects of the system.

## 🔀 Configuration Precedence

Settings can be supplied from several sources. When the same key appears in more than one place, Shannon resolves it using this order (highest priority first):
1. Environment variables (including `.env` files or runtime exports)
2. Docker Compose defaults
3. YAML configuration files in this directory, such as `features.yaml` or `shannon.yaml`

## 📁 Configuration Files Overview

| File | Purpose | Usage | Environment |
|------|---------|-------|-------------|
| [`shannon.yaml`](#shannonmyaml) | Main system configuration | Production default | All |
| [`features.yaml`](#featuresyaml) | Feature flags and execution modes | Feature toggles | All |
| [`models.yaml`](#modelsyaml) | LLM model configuration and routing | Model selection | All |
| [`shannon-test.yaml`](#shannon-testyaml) | Hot-reload testing configuration | Development testing | Dev |
| [`shannon-policy-test.yaml`](#shannon-policy-testyaml) | OPA policy testing configuration | Security testing | Dev/Test |

## 🔧 Configuration Files Details

### `shannon.yaml`
**Main system configuration** - The primary configuration file used in production.

```yaml
# Key sections:
service:        # gRPC server settings (ports, timeouts)
auth:           # Authentication and authorization  
policy:         # OPA policy engine settings
circuit_breakers: # Failure protection for external services
degradation:    # Graceful degradation rules
temporal:       # Temporal workflow configuration
```

**When to use:** Primary configuration for all environments

**How to modify:**
```bash
# Edit main configuration
vim config/shannon.yaml

# Test configuration changes
make dev
curl http://localhost:8081/health
```

### `features.yaml`
**Feature flags and execution modes** - Controls which features are enabled and how they behave.

```yaml
# Key sections:
execution_modes: # Simple/Standard/Complex mode configurations
agent:          # Agent resource limits and behavior
tools:          # Tool execution settings
llm:           # LLM provider settings and fallbacks
```

**When to use:** Enable/disable features, configure execution modes

**Common modifications:**
```yaml
# Disable complex mode for testing
execution_modes:
  complex:
    enabled: false  # Disable complex multi-agent execution

# Increase agent limits
agent:
  max_concurrent: 20  # Allow more concurrent agents
  memory_limit_mb: 1024  # Increase memory per agent

# Enable fallback from template execution to AI decomposition
workflows:
  templates:
    fallback_to_ai: true
```

### `models.yaml`
**LLM model configuration** - Defines which models to use and their routing priorities.

```yaml
# Key sections:
model_tiers:    # Small/Medium/Large model classifications
  small:        # Fast, cheap models (50% allocation)
  medium:       # Balanced models (40% allocation)  
  large:        # Powerful models (10% allocation)
fallback:       # Fallback providers and error handling
```

**When to use:** Add new AI models, change model routing, configure fallbacks

**Common modifications:**
```yaml
# Add new provider
model_tiers:
  small:
    providers:
      - provider: new_provider
        model: new-model-name
        priority: 1  # Highest priority
        
# Change allocation percentages
model_tiers:
  large:
    allocation: 20  # Use larger models more often
```

### `shannon-test.yaml`
**Hot-reload testing configuration** - Demonstrates dynamic configuration changes.

```yaml
# Key differences from shannon.yaml:
service:
  health_port: 8082    # Changed from 8081 to test hot-reload
  graceful_timeout: "45s"  # Increased timeouts
  
circuit_breakers:
  redis:
    max_requests: 8    # Increased from 5
```

**When to use:** Testing hot-reload functionality, validating configuration changes

**How to use:**
```bash
# Start with main config
make dev

# Switch to test config (hot-reload)
cp config/shannon-test.yaml config/shannon.yaml

# Verify changes took effect
curl http://localhost:8082/health  # Note new port
```

### `shannon-policy-test.yaml`
**OPA policy testing configuration** - Focused on testing the policy engine.

```yaml
# Key sections:
policy:
  enabled: true         # Policy engine active
  mode: "dry-run"      # Safe testing mode
  path: "/app/config/opa/policies"
  fail_closed: false   # Allow on policy errors
```

**When to use:** Testing OPA policies, security validation, compliance testing

**How to use:**
```bash
# Switch to policy test config
cp config/shannon-policy-test.yaml config/shannon.yaml

# Test safe query
./scripts/submit_task.sh "What is 2+2?"

# Test dangerous query (should be flagged in metrics)
./scripts/submit_task.sh "Help me delete all files"

# Check policy metrics
curl http://localhost:2112/metrics | grep shannon_policy
```

## 🗂️ Configuration Directories

### `opa/`
Contains Open Policy Agent (OPA) policies written in Rego.
- **Purpose:** Security and compliance rules
- **Files:** `*.rego` policy files
- **Documentation:** [`opa/README.md`](opa/README.md)

### `otel/`
Reserved for OpenTelemetry configuration (currently unused).
- **Purpose:** Distributed tracing configuration
- **Status:** Placeholder for future OTEL integration
- **Documentation:** [`otel/README.md`](otel/README.md)

## 🔄 Hot-Reload Configuration

Shannon supports **hot-reload** for most configuration changes without service restart:

### Supported Hot-Reload
- ✅ Service ports and timeouts
- ✅ Circuit breaker thresholds  
- ✅ Policy engine settings
- ✅ Feature flags
- ✅ Model routing preferences

### Requires Restart
- ❌ Database connection strings
- ❌ Core service endpoints
- ❌ TLS/SSL certificate changes

### Testing Hot-Reload
```bash
# Monitor configuration changes
docker compose -f deploy/compose/docker-compose.yml logs orchestrator -f &

# Make configuration change
sed -i 's/health_port: 8081/health_port: 8082/' config/shannon.yaml

# Verify change applied (within ~30 seconds)
curl http://localhost:8082/health
```

## 🛡️ Configuration Security

### Sensitive Settings
These settings contain sensitive information:
- `auth.jwt_secret` - JWT signing key
- Database passwords (if configured)
- API keys for LLM providers

### Environment Variables
Override sensitive settings via environment variables:
```bash
# Override JWT secret
export JWT_SECRET="your-secure-32-char-minimum-secret"

# Override database password  
export DB_PASSWORD="secure-database-password"
```

### Production Checklist
- [ ] Change default JWT secret
- [ ] Enable authentication (`auth.enabled: true`)
- [ ] Set policy to enforce mode (`policy.mode: "enforce"`)
- [ ] Configure fail-closed (`fail_closed: true`)
- [ ] Disable debug logging
- [ ] Set appropriate resource limits

## 🔧 Configuration Validation

### Syntax Validation
```bash
# Validate YAML syntax
yaml-lint config/*.yaml

# Test configuration load
docker compose -f deploy/compose/docker-compose.yml config
```

### Integration Testing
```bash
# Test full configuration
make dev
make smoke

# Test specific config file
cp config/shannon-test.yaml config/shannon.yaml
make dev
curl http://localhost:8082/health  # Should use new port
```

## 📝 Configuration Best Practices

1. **Version Control:** Always commit configuration changes
2. **Environment Separation:** Use different configs for dev/staging/prod
3. **Gradual Changes:** Test configuration changes in non-production first
4. **Documentation:** Document why specific settings were chosen
5. **Monitoring:** Watch metrics after configuration changes
6. **Backup:** Keep backups of working configurations

## 🐛 Troubleshooting

### Configuration Not Loading
```bash
# Check file permissions
ls -la config/*.yaml

# Check YAML syntax
python3 -c "import yaml; yaml.safe_load(open('config/shannon.yaml'))"

# Check orchestrator logs
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | tail -20
```

### Hot-Reload Not Working
```bash
# Check configuration manager logs
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | grep -i "config"

# Verify file watchers
# Configuration changes should appear in logs within 30 seconds
```

### Invalid Configuration Values
```bash
# Check validation errors in logs
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | grep -i "error\|invalid"

# Reset to known good configuration
cp config/shannon.yaml.backup config/shannon.yaml
```

## 📚 Related Documentation

- [OPA Policy Configuration](opa/README.md)
- [OpenTelemetry Setup](otel/README.md)
- [Shannon Architecture](../CLAUDE.md)
- [Development Guide](../README.md)
