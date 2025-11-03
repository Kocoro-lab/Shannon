# Migration Guide: GPT-3.5/Claude 3 → GPT-5/Claude 4.5

**Version**: 1.0
**Date**: 2025-11-03
**Scope**: Shannon Platform Model Migration

---

## Overview

This migration guide covers the breaking changes introduced when upgrading from legacy models (GPT-3.5, GPT-4, Claude 3) to the latest generation (GPT-5, GPT-4.1, Claude 4.5).

---

## Breaking Changes

### 1. Model Alias Removal

**BREAKING**: Model aliases have been removed to simplify logic and ensure explicit model selection.

**Before** (aliases supported):
```python
# These aliases worked in the old version
"gpt-5" → "gpt-4o"
"o3-mini" → "gpt-4o-mini"
"claude-sonnet-4-5-20250929" → "claude-3-sonnet"
"claude-opus-4-1-20250805" → "claude-3-opus"
```

**After** (aliases removed):
```python
# You must now use full canonical model IDs
"gpt-5-nano-2025-08-07"  # No aliases
"gpt-5-2025-08-07"
"gpt-5-mini-2025-08-07"
"gpt-4.1-2025-04-14"
"claude-sonnet-4-5-20250929"
"claude-haiku-4-5-20251001"
"claude-opus-4-1-20250805"
```

**Migration Action**:
- Search your codebase for model names and update to full canonical IDs
- Update any configuration files, environment variables, or hardcoded model strings
- Update API calls to use new model IDs

```bash
# Find potential issues
grep -r "gpt-5\"" --include="*.py" --include="*.go" --include="*.yaml"
grep -r "claude-3" --include="*.py" --include="*.go" --include="*.yaml"
```

---

### 2. Default Pricing Changed

**BREAKING**: Default fallback pricing increased from `$0.002/1K` tokens to `$0.005/1K` tokens.

**Impact**:
- Tasks using unknown/unconfigured models will see higher cost estimates
- Budget calculations may trigger warnings sooner

**Migration Action**:
- Ensure all models in use are properly configured in `config/models.yaml`
- Review budget thresholds if you rely on default pricing

---

### 3. Model Configuration Changes

**BREAKING**: `config/models.yaml` structure updated with new models.

**Removed Models**:
```yaml
# These models are NO LONGER in the config
openai:
  - gpt-3.5-turbo
  - gpt-4o
  - gpt-4o-mini
  - gpt-4-turbo
  - o3
  - o1
  - o1-mini

anthropic:
  - claude-3-sonnet
  - claude-3-haiku
  - claude-3-5-sonnet-20241022
  - claude-3-5-haiku-20241022
```

**Added Models**:
```yaml
openai:
  - gpt-5-nano-2025-08-07      # Small tier
  - gpt-5-mini-2025-08-07      # Small tier
  - gpt-5-2025-08-07           # Medium tier
  - gpt-4.1-2025-04-14         # Large tier
  - gpt-5-pro-2025-10-06       # Large tier (Responses API only)

anthropic:
  - claude-haiku-4-5-20251001       # Small tier
  - claude-sonnet-4-5-20250929      # Medium tier
  - claude-opus-4-1-20250805        # Large tier
  - claude-sonnet-4-20250514        # Medium tier
```

**Migration Action**:
- Review tier assignments in `model_tiers` section
- Update any explicit model references to use new model IDs
- Test that your workloads still use appropriate tier models

---

### 4. GPT-5 API Parameter Restrictions

**BREAKING**: GPT-5 chat models have restricted parameter support.

**GPT-5 Chat Models** (excludes `gpt-5-pro`):
```python
# ❌ These parameters are NO LONGER supported
temperature       # Must use default (1.0)
top_p             # Must use default
frequency_penalty # Must use default
presence_penalty  # Must use default
max_tokens        # Replaced by max_completion_tokens

# ✅ Use this instead
max_completion_tokens  # Replaces max_tokens
```

**GPT-4.1 and older models**:
```python
# ✅ These parameters still work
temperature
top_p
frequency_penalty
presence_penalty
max_tokens  # Still supported for GPT-4.1
```

**Migration Action**:
- Update code that sets sampling parameters for GPT-5 models
- Replace `max_tokens` with `max_completion_tokens` for GPT-5
- Test that GPT-5 calls work without custom temperature/top_p

**Example Fix**:
```python
# Before (worked with GPT-3.5/GPT-4)
response = await client.chat.completions.create(
    model="gpt-3.5-turbo",
    messages=[...],
    max_tokens=100,
    temperature=0.7,
    top_p=0.9
)

# After (GPT-5 family)
response = await client.chat.completions.create(
    model="gpt-5-nano-2025-08-07",
    messages=[...],
    max_completion_tokens=100,  # Changed
    # temperature, top_p removed (use defaults)
)

# After (GPT-4.1)
response = await client.chat.completions.create(
    model="gpt-4.1-2025-04-14",
    messages=[...],
    max_tokens=100,  # Still works for GPT-4.1
    temperature=0.7,
    top_p=0.9
)
```

---

### 5. API Response Metadata Fields

**ENHANCEMENT** (non-breaking): All API responses now include execution metadata.

**New Fields Added**:
```json
{
  "task_id": "...",
  "status": "TASK_STATUS_COMPLETED",
  "result": "...",
  // New metadata fields:
  "model_used": "gpt-5-nano-2025-08-07",
  "provider": "openai",
  "usage": {
    "input_tokens": 104,
    "output_tokens": 70,
    "total_tokens": 174,
    "estimated_cost": 0.000087
  }
}
```

**Migration Action**:
- Update any code that parses task responses to handle new fields
- Update tests to verify metadata population
- Use metadata for cost tracking and model selection analysis

---

### 6. Provider Override Precedence

**CLARIFICATION**: Provider override precedence is now strictly enforced.

**Precedence Order**:
1. `provider_override` (highest priority)
2. `provider` (fallback)
3. `llm_provider` (legacy fallback)

**Example**:
```json
// This will use "anthropic" regardless of other fields
{
  "query": "...",
  "provider_override": "anthropic",  // ← Takes precedence
  "provider": "openai",              // ← Ignored
  "llm_provider": "google"           // ← Ignored
}
```

**Migration Action**:
- Review code that sets provider preferences
- Ensure `provider_override` is only used when you truly want to force a provider
- Update any conflicting provider settings

---

### 7. Tier Preference Logic

**CLARIFICATION**: Model tier precedence is now strictly enforced.

**Precedence Order**:
1. Top-level `query.model_tier` (highest priority)
2. `context.model_tier` (fallback)
3. Mode-based default (lowest priority)

**Example**:
```python
# Top-level tier takes precedence
request = {
    "query": "...",
    "model_tier": "large",        # ← Takes precedence
    "context": {
        "model_tier": "small"     # ← Ignored
    }
}
```

**Migration Action**:
- Review code that sets model tiers
- Ensure tier is set at the correct level (top-level for explicit control)
- Test that tier selection behaves as expected

---

### 8. Provider Validation

**NEW**: Provider names are now validated at submission time.

**Valid Providers**:
```
openai, anthropic, google, groq, xai, deepseek, qwen, zai, ollama, mistral, cohere
```

**Example Error**:
```json
// Invalid provider will now fail immediately
{
  "query": "...",
  "provider_override": "invalid_provider"
}

// Response:
{
  "error": "Invalid provider_override: invalid_provider (allowed: openai, anthropic, ...)"
}
```

**Migration Action**:
- Test API calls with provider overrides
- Update error handling to catch validation errors
- Ensure provider names match the allowed list

---

## Database Migrations

### New Columns Added

**Table**: `task_executions`

```sql
-- Migration 008: Add model_used and provider columns
ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS model_used VARCHAR(100);

ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS provider VARCHAR(50);

-- Indexes for analytics queries
CREATE INDEX IF NOT EXISTS idx_task_executions_model ON task_executions(model_used);
CREATE INDEX IF NOT EXISTS idx_task_executions_provider ON task_executions(provider);
```

**Migration Action**:
- Run migration `008_add_model_provider_to_tasks.sql`
- Verify indexes are created
- Update analytics queries to use new columns

---

## Testing Checklist

After migration, verify the following:

### Model Selection
- [ ] Small tier tasks use `gpt-5-nano-2025-08-07` or `claude-haiku-4-5-20251001`
- [ ] Medium tier tasks use `gpt-5-2025-08-07` or `claude-sonnet-4-5-20250929`
- [ ] Large tier tasks use `gpt-4.1-2025-04-14` or `claude-opus-4-1-20250805`

### API Parameters
- [ ] GPT-5 calls use `max_completion_tokens` instead of `max_tokens`
- [ ] GPT-5 calls do not include `temperature`, `top_p`, etc. (unless using gpt-5-pro)
- [ ] GPT-4.1 calls still work with `max_tokens`

### Provider Override
- [ ] `provider_override` is respected
- [ ] Invalid providers are rejected with clear error messages
- [ ] Provider precedence follows documented order

### Metadata
- [ ] API responses include `model_used` field
- [ ] API responses include `provider` field
- [ ] API responses include `usage` breakdown (input_tokens, output_tokens, cost)
- [ ] Database persists metadata correctly

### Cost Estimation
- [ ] Task costs are non-zero
- [ ] Costs use correct model pricing from `models.yaml`
- [ ] Budget warnings trigger at correct thresholds

---

## Rollback Plan

If you need to rollback this migration:

1. **Revert code changes**:
   ```bash
   git revert <commit-sha>
   ```

2. **Restore old models.yaml**:
   ```bash
   git checkout <previous-commit> -- config/models.yaml
   ```

3. **Database rollback** (if migration 008 was applied):
   ```sql
   -- Rollback migration 008
   ALTER TABLE task_executions DROP COLUMN IF EXISTS model_used;
   ALTER TABLE task_executions DROP COLUMN IF EXISTS provider;
   DROP INDEX IF EXISTS idx_task_executions_model;
   DROP INDEX IF EXISTS idx_task_executions_provider;
   ```

4. **Restart services**:
   ```bash
   docker compose -f deploy/compose/docker-compose.yml down
   docker compose -f deploy/compose/docker-compose.yml build --no-cache
   docker compose -f deploy/compose/docker-compose.yml up -d
   ```

---

## Support

If you encounter issues during migration:

1. Check logs: `docker compose logs llm-service orchestrator`
2. Verify configuration: `cat config/models.yaml | grep -A 5 "model_tiers"`
3. Test with simple query: `./scripts/submit_task.sh "What is 2+2?"`
4. Review test results: `bash tests/e2e/44_model_tiers_e2e_test.sh`

For additional help, refer to:
- `docs/testing-strategy.md` - Comprehensive testing guide
- `docs/providers-models.md` - Model configuration reference
- `docs/task-submission-api.md` - API parameter reference

---

**Document Version**: 1.0
**Last Updated**: 2025-11-03
**Next Review**: After production deployment
