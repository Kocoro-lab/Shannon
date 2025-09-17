# Centralized Pricing Configuration

## Overview

The Shannon platform now has a centralized pricing configuration system that manages model costs across all services (Go orchestrator, Rust agent-core, Python llm-service). All pricing data is maintained in a single source of truth: `config/models.yaml`.

## Configuration Structure

The pricing configuration is defined in `config/models.yaml` under the `pricing` section:

```yaml
pricing:
  defaults:
    combined_per_1k: 0.002  # Default cost per 1K tokens when model is unknown
  models:
    <provider>:
      <model_id>:
        input_per_1k: 0.0005   # Cost per 1K input tokens
        output_per_1k: 0.0015  # Cost per 1K output tokens
        combined_per_1k: 0.002 # Optional: Used when only total tokens are known
```

## Implementation Details

### Go Orchestrator

**Location**: `go/orchestrator/internal/pricing/pricing.go`

- Loads pricing configuration from `config/models.yaml` (or `MODELS_CONFIG_PATH` env var)
- Provides functions:
  - `DefaultPerToken()`: Returns default cost per token
  - `PricePerTokenForModel(model string)`: Returns model-specific cost per token
  - `CostForTokens(model string, tokens int)`: Calculates total cost
  - `CostForSplit(model string, inputTokens, outputTokens int)`: Accurate split pricing
  - `ModifiedTime()`: Returns config file modification time
- Used in:
  - `internal/server/service.go`: Calculates workflow execution costs
  - `internal/activities/session.go`: Updates session cost tracking

Hot reload & validation:
- `models.yaml` is watched by the config manager; on change, pricing is reloaded
- Basic validation ensures no negative values under `pricing` section

Fallback metrics:
- `shannon_pricing_fallback_total{reason="missing_model|unknown_model"}`
  increments whenever defaults are used (missing or unknown model name)

### Rust Agent-Core

**Location**: `rust/agent-core/src/llm_client.rs`

- Function `pricing_cost_per_1k(model)` reads pricing from same config file
- Function `calculate_cost(model, tokens)` uses centralized pricing with fallback
- Attempts to read from:
  1. `MODELS_CONFIG_PATH` environment variable
  2. `/app/config/models.yaml` (Docker container path)
  3. `./config/models.yaml` (local development)
- Falls back to hardcoded heuristics if config unavailable

### Python LLM Service

**Location**: `python/llm-service/llm_provider/manager.py`

- `LLMManager` loads pricing overrides after initializing providers
- Function `_load_and_apply_pricing_overrides()`:
  - Reads from `config/models.yaml` (or `MODELS_CONFIG_PATH`)
  - Applies pricing to provider models matching by model_id or key
- Overrides provider-specific pricing with centralized values
- Maintains compatibility with existing provider configurations

## Cost Calculation Logic

1. **When input/output tokens are known separately**:
   - Uses `input_per_1k` and `output_per_1k` for precise calculation

2. **When only total tokens are known**:
   - Uses `combined_per_1k` if specified
   - Otherwise averages: `(input_per_1k + output_per_1k) / 2`

3. **When model is unknown**:
   - Uses `defaults.combined_per_1k`
   - Falls back to `0.002` per 1K tokens (GPT-3.5 equivalent)
    - Increments `shannon_pricing_fallback_total`

## Environment Variables

- `MODELS_CONFIG_PATH`: Override default config file location
  - Used by all services (Go, Rust, Python)
  - Takes precedence over default paths

## Default Paths

Services look for configuration in this order:
1. `$MODELS_CONFIG_PATH` (if set)
2. `/app/config/models.yaml` (Docker containers)
3. `./config/models.yaml` (local development)

## Testing

### Go Testing
```bash
cd go/orchestrator
go test -v ./internal/pricing/...
```

### Manual Verification
The implementation was verified to:
- Load pricing configuration correctly from `config/models.yaml`
- Calculate costs accurately for known models
- Fall back to defaults for unknown models
- Apply pricing overrides in Python LLM service

## Migration Notes

### What Changed
- **Go**: Service uses `pricing.CostForTokens()` / `CostForSplit()` instead of inline heuristics
- **Rust**: `calculate_cost()` checks centralized config before fallback
- **Python**: Manager applies pricing overrides after provider initialization
- **Config**: Added `pricing` section to `config/models.yaml`

### Backward Compatibility
- All services maintain fallback logic for missing configuration
- Existing provider-specific pricing is preserved unless overridden
- No changes to activity/proto signatures required

## Workflow Coverage

For detailed information on which workflows have true per-model costs vs approximations, see [workflow-pricing-coverage.md](workflow-pricing-coverage.md).

### Summary
- **Production Ready** (true costs): Simple, DAG v2, Supervisor, React, Streaming (single & parallel)
- **Using Defaults**: Exploratory, Scientific patterns

## Future Enhancements

### Completed ✅
1. **Split token tracking**: Workflows now track input/output tokens separately
2. **Price validation**: Non-negative validation implemented
3. **Hot-reload support**: Pricing reloads on `models.yaml` changes
4. **Model threading**: Production workflows pass model names for accurate costs
5. **Fallback metrics**: `shannon_pricing_fallback_total` tracks coverage

### Potential Improvements
1. **Cost alerts**: Implement threshold notifications when costs exceed limits
2. **Usage analytics**: Track model-specific usage patterns and costs
3. **Pattern extensions**: Add per-agent tracking to Exploratory/Scientific if usage warrants

## Configuration Example

Current pricing for common models:

```yaml
pricing:
  defaults:
    combined_per_1k: 0.002
  models:
    openai:
      gpt-3.5-turbo:
        input_per_1k: 0.0005
        output_per_1k: 0.0015
      gpt-4-turbo:
        input_per_1k: 0.0100
        output_per_1k: 0.0300
    anthropic:
      claude-3-sonnet:
        input_per_1k: 0.0030
        output_per_1k: 0.0150
      claude-3-haiku:
        input_per_1k: 0.00025
        output_per_1k: 0.00125
    deepseek:
      deepseek-chat:
        input_per_1k: 0.0001
        output_per_1k: 0.0002
```

## Workflow Coverage (True Cost vs. Approximate)

Streaming now reports true per‑model costs (including token splits where available).

- Simple: True cost (per‑model, total tokens)
- DAG v2: True cost (per‑agent, input/output split)
- Supervisor: True cost (per‑agent, input/output split)
- React: True cost (per‑agent, input/output split)
- Streaming (single/parallel): True cost (per‑agent, total tokens; split when provided)
- Exploratory (ToT/Debate/Reflect): Approximate (aggregate totals)
- Scientific (CoT/Debate/ToT/Reflect): Approximate (aggregate totals)

Note: We will upgrade Exploratory/Scientific to true per‑agent costs when these patterns surface model IDs and token splits from their internal agent calls.
