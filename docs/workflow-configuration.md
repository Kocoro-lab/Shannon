# Workflow Configuration Reference

**Version**: 1.0  
**File**: `config/shannon.yaml`

## Overview

This guide covers all configuration options for the embedded workflow engine.

## Configuration File Structure

```yaml
embedded_workflow:
  # Engine Settings
  max_concurrent: 10
  wasm_cache_size_mb: 100
  event_buffer_size: 256
  checkpoint_interval_events: 10
  checkpoint_max_age_minutes: 5
  event_retention_days: 7
  
  # Pattern-Specific Configuration
  patterns:
    chain_of_thought:
      max_iterations: 5
      model: "claude-sonnet-4-20250514"
    
    tree_of_thoughts:
      max_branches: 3
      max_depth: 4
      exploration_mode: "breadth_first"
      pruning_threshold: 0.3
    
    research:
      max_iterations: 3
      sources_per_round: 6
      min_sources: 8
      coverage_threshold: 0.8
      enable_verification: false
      enable_fact_extraction: false
    
    react:
      max_iterations: 5
      tools_enabled: ["web_search", "calculator", "web_fetch"]
    
    debate:
      num_agents: 3
      max_rounds: 3
      require_consensus: false
    
    reflection:
      max_iterations: 3
      quality_threshold: 0.8
```

## Engine Settings

### max_concurrent
- **Type**: Integer
- **Default**: 10
- **Range**: 1-20
- **Description**: Maximum concurrent workflows. Set to `min(num_cpus, 10)` for optimal performance.

### wasm_cache_size_mb
- **Type**: Integer
- **Default**: 100
- **Range**: 50-500
- **Description**: LRU cache size for compiled WASM modules.

### event_buffer_size
- **Type**: Integer
- **Default**: 256
- **Range**: 64-1024
- **Description**: Per-workflow event channel capacity. Higher values prevent event loss for slow consumers.

### checkpoint_interval_events
- **Type**: Integer
- **Default**: 10
- **Range**: 5-50
- **Description**: Create checkpoint every N events.

### checkpoint_max_age_minutes
- **Type**: Integer
- **Default**: 5
- **Range**: 1-30
- **Description**: Maximum time between checkpoints.

### event_retention_days
- **Type**: Integer
- **Default**: 7
- **Range**: 1-90
- **Description**: Days to retain completed workflow events.

## Pattern Configuration

### Chain of Thought

```yaml
chain_of_thought:
  max_iterations: 5      # Maximum reasoning steps
  model: "claude-sonnet-4"  # LLM model to use
```

**Performance Impact**:
- Higher iterations → more thorough reasoning → slower
- Faster models → quicker but potentially lower quality

### Tree of Thoughts

```yaml
tree_of_thoughts:
  max_branches: 3                    # Branches per node
  max_depth: 4                       # Tree depth limit
  exploration_mode: "breadth_first"  # or "depth_first"
  pruning_threshold: 0.3             # Drop branches below this score
```

**Performance Impact**:
- More branches/depth → exponential cost increase
- Pruning threshold → quality vs speed trade-off

### Research (Deep Research 2.0)

```yaml
research:
  max_iterations: 3              # Coverage improvement loops
  sources_per_round: 6           # Sources to collect per iteration
  min_sources: 8                 # Minimum total sources
  coverage_threshold: 0.8        # Stop when coverage reaches this
  enable_verification: false     # Fact checking (expensive)
  enable_fact_extraction: false  # Extract structured facts
```

**Performance Impact**:
- max_iterations → duration multiplier (3x iterations = 3x time)
- Verification + extraction → 50% slower but higher quality

### ReAct

```yaml
react:
  max_iterations: 5                           # Reason-Act-Observe cycles
  tools_enabled: ["web_search", "calculator"]  # Available tools
```

**Tool Configuration**:
- `web_search`: Requires Tavily API key
- `calculator`: Built-in, no API needed
- `web_fetch`: HTTP fetching, no API needed

### Debate

```yaml
debate:
  num_agents: 3              # Number of debating agents (2-4)
  max_rounds: 3              # Discussion rounds
  require_consensus: false   # Wait for agreement
```

**Performance Impact**:
- num_agents × max_rounds → LLM calls
- Consensus → may run longer

### Reflection

```yaml
reflection:
  max_iterations: 3        # Critique-improve cycles
  quality_threshold: 0.8   # Stop when quality sufficient
```

**Performance Impact**:
- Iterations → duration and token cost
- Lower threshold → faster but lower quality

## Environment Variables

### Required

```bash
# At least one LLM provider
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
GROQ_API_KEY=gsk_...
XAI_API_KEY=xai-...

# For research pattern
TAVILY_API_KEY=tvly-...
```

### Optional

```bash
# Database paths (defaults to app data directory)
SHANNON_DB_PATH=./shannon.db
SHANNON_EVENTS_DB_PATH=./shannon_events.db

# Logging
RUST_LOG=info

# WASM settings
WASM_MEMORY_LIMIT_MB=512
WASM_FUEL_LIMIT=10000000
```

## Hot Reload

Configuration supports hot reload (changes detected within ~30s):

```bash
# Edit config
vim config/shannon.yaml

# Changes auto-detected
# Check logs for confirmation:
# INFO config: Configuration reloaded
```

## Performance Tuning

### Low-Resource Devices

```yaml
embedded_workflow:
  max_concurrent: 3              # Limit concurrent workflows
  wasm_cache_size_mb: 50         # Smaller cache
  event_buffer_size: 128         # Smaller buffer
  checkpoint_interval_events: 20  # Less frequent checkpoints
```

### High-Performance Servers

```yaml
embedded_workflow:
  max_concurrent: 20            # More concurrent workflows
  wasm_cache_size_mb: 500       # Larger cache
  event_buffer_size: 512        # Larger buffer
  checkpoint_interval_events: 5  # More frequent checkpoints
```

### Cost Optimization

```yaml
patterns:
  chain_of_thought:
    model: "claude-haiku-3-5"  # Cheaper model
    max_iterations: 3          # Fewer iterations
  
  research:
    max_iterations: 1          # Single pass
    sources_per_round: 4       # Fewer sources
    enable_verification: false # Skip expensive verification
```

## Security Settings

### WASM Sandbox

```rust
pub struct SandboxCapabilities {
    pub timeout_ms: 30_000,      // 30s timeout
    pub max_memory_mb: 512,      // Memory limit
    pub allow_network: false,    // Network access
    pub allowed_hosts: vec![],   // Host allow-list
}
```

### Data Encryption

API keys are encrypted at rest using AES-256-GCM:

```rust
// Automatic encryption in database
store.save_api_key(provider, key).await?;
```

## Troubleshooting Configuration

### Validation

```bash
# Check configuration syntax
cargo run --bin shannon-api -- --validate-config

# View effective configuration
cargo run --bin shannon-api -- --dump-config
```

### Common Errors

**"Max concurrent workflows reached"**:
- Increase `max_concurrent`
- Wait for workflows to complete
- Check for stuck workflows

**"Checkpoint corruption"**:
- Verify disk space
- Check `checkpoint_interval_events` not too low
- Review compression settings

**"Event channel full"**:
- Increase `event_buffer_size`
- Improve consumer performance
- Check for slow UI updates

## References

- [Main Documentation](embedded-workflow-engine.md)
- [Pattern Guide](workflow-patterns.md)
- [Debugging Guide](workflow-debugging.md)
- [Testing Strategy](testing-strategy.md)
