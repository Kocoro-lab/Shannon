# Template Workflows Documentation

## Overview

Shannon's template workflow system enables zero-token routing for common patterns through YAML-defined workflows. This dual-system architecture combines deterministic template execution (System 1) with intelligent AI-driven decomposition (System 2), achieving 85-95% token savings on repeated tasks.

## Architecture

### Dual-System Design

```
┌─────────────────────────────────────────────────────────────┐
│                     User Query                               │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│              Orchestrator Router                             │
│  1. Check template registry for exact match                  │
│  2. Consult learning router for learned patterns             │
│  3. Fall back to AI decomposition if needed                  │
└────────────────┬────────────────────────────────────────────┘
                 │
        ┌────────┴────────┬─────────────┐
        ▼                 ▼             ▼
┌───────────────┐ ┌──────────────┐ ┌──────────────┐
│  Template     │ │   Learning   │ │     AI       │
│  Workflow     │ │   Router     │ │ Decomposition│
│ (0 tokens)    │ │ (0 tokens)   │ │ (full cost)  │
└───────────────┘ └──────────────┘ └──────────────┘
```

### System 1: Template-Based Execution
- **Zero-token routing**: Direct template matching without LLM calls
- **Deterministic execution**: Predictable, repeatable workflows
- **Version-gated**: Temporal workflow versioning ensures backward compatibility
- **Budget-aware**: Per-node token limits with automatic degradation

### System 2: Learning Router
- **Pattern recognition**: Learns successful decomposition patterns
- **Epsilon-greedy strategy**: 90% exploit known patterns, 10% explore new approaches
- **Supervisor memory**: Stores and retrieves proven strategies
- **Continuous improvement**: Updates strategy effectiveness based on outcomes

## Template Structure

### Basic Template Format

```yaml
name: template_name           # Unique identifier
description: "What this does"  # Human-readable description
version: "1.0.0"              # Semantic version
defaults:
  budget_agent_max: 10000     # Default per-node agent budget
  require_approval: false     # Optional: gate on human approval

nodes:
  - id: node_1
    type: simple              # Node type (simple/cognitive/dag/supervisor)
    strategy: react           # Override strategy for this node
    budget_max: 2000          # Node-specific budget
    tools_allowlist:          # Allowed tools for this node
      - web_search
      - calculator
    depends_on: []            # Node dependencies (for DAG ordering)

  - id: node_2
    type: cognitive
    strategy: tree_of_thoughts
    budget_max: 5000
    depends_on: ["node_1"]    # Reference previous outputs via depends_on

edges:
  - from: node_1
    to: node_2
```

### Node Types

#### Simple Node
- Single-task execution
- Direct tool invocation
- Minimal context required

```yaml
- id: fetch_data
  type: simple
  strategy: react
  tools_allowlist: [weather_api]
  budget_max: 500
  metadata:
    query: "Fetch current weather data"
```

#### Cognitive Node
- Complex reasoning tasks
- Multi-step analysis
- Budget-aware automatic degradation (no extra field needed)

```yaml
- id: analyze
  type: cognitive
  strategy: chain_of_thought
  budget_max: 3000
  metadata:
    query: "Analyze trends and provide insights"
  # Note: Degradation due to budget happens automatically; no pattern_degrade flag.
```

#### DAG Node
- Parallel execution branches
- Complex dependency graphs
- Optimized for concurrent operations

```yaml
- id: parallel_research
  type: dag
  depends_on: [previous_node]
  budget_max: 4000
  metadata:
    tasks:
      - id: search_1
        query: "Research topic A"
        tools: [web_search]
      - id: search_2
        query: "Research topic B"
        depends_on: [search_1]  # Dependencies within the DAG
        tools: [web_search, web_fetch]
```

**Note**: DAG sub-tasks are defined in `metadata.tasks`, not as a top-level `sub_nodes` field. Each task can have its own `depends_on` to create complex dependency graphs within the parallel execution.

#### Supervisor Node
- Hierarchical task decomposition
- Sub-agent coordination
- Adaptive execution planning

```yaml
- id: complex_project
  type: supervisor
  strategy: reflection
  budget_max: 8000
  metadata:
    query: "Coordinate multi-phase project"
    sub_agents: ["research_agent", "analysis_agent", "synthesis_agent"]
```

### Template Inheritance
- Reuse established templates and override only what changes
- Specify parents via `extends` (order matters when merging)
- Inherit defaults, nodes, edges, and metadata from the parent templates

```yaml
name: research_summary_enterprise
extends:
  - research_summary

defaults:
  model_tier: large
  require_approval: true

nodes:
  - id: explore
    budget_max: 4000
    metadata:
      exploration_depth: deep
  - id: finalize
    metadata:
      summary_style: executive
```

Multiple parents are allowed:

```yaml
extends:
  - research_summary
  - research_summary_data_appendix
```

Parents are applied in listed order, and the derived template is merged last. Parent edges remain unless the derived template provides its own edge list.

### Strategy Types

#### ReAct (Reasoning + Acting)
- **Token usage**: ~500-1500 per cycle
- **Best for**: Simple tool-based tasks
- **Degradation**: Final fallback option

```yaml
strategy: react
```

#### Chain of Thought (CoT)
- **Token usage**: ~1500-3000 per analysis
- **Best for**: Step-by-step reasoning
- **Degradation**: From ToT when budget constrained

```yaml
strategy: chain_of_thought
```

#### Tree of Thoughts (ToT)
- **Token usage**: ~3000-8000 per exploration
- **Best for**: Complex problem-solving
- **Degradation**: To CoT, then ReAct

```yaml
strategy: tree_of_thoughts
metadata:
  branches: 3
  depth: 2
```

## Pattern Degradation

### Automatic Budget Management

The system automatically degrades execution patterns when budget constraints are detected:

```
Tree of Thoughts (8000 tokens)
       ↓ (budget < 5000)
Chain of Thought (3000 tokens)
       ↓ (budget < 2000)
ReAct (1000 tokens)
```

### Configuration

Degradation is controlled per-node via the `on_fail` field:

```yaml
nodes:
  - id: analyze
    type: cognitive
    strategy: tree_of_thoughts
    budget_max: 5000
    on_fail:
      degrade_to: chain_of_thought  # Fallback strategy
      retry: 1                       # Number of retries
```

## Learning Router

### Strategy Recommendation

The learning router maintains a history of successful patterns:

```go
type StrategyRecommendation struct {
    Pattern     string        // Query pattern signature
    Strategy    StrategyType  // Recommended strategy
    Confidence  float64       // Success rate (0-1)
    TokenSaved  int          // Average tokens saved
}
```

### Epsilon-Greedy Selection

```yaml
learning_router:
  enabled: true
  epsilon: 0.1               # 10% exploration rate
  min_confidence: 0.7        # Minimum confidence to use learned pattern
  history_size: 1000         # Number of patterns to remember
```

## Registry Management

### Template Loading

Templates are loaded at startup from configured directories:

```bash
# Default locations
/app/config/workflows/
/app/config/workflows/examples/

# Custom via environment
export TEMPLATES_PATH="/custom/templates:/shared/templates"
```

### Hot Reload (Future)

```yaml
registry:
  hot_reload: true           # Enable file watching
  reload_interval: 30s       # Check interval
  validation: strict         # Validation level
```

## Validation

### Template Validation Rules

1. **Structure validation**: Required fields, valid YAML
2. **DAG validation**: No cycles, valid dependencies
3. **Budget validation**: Node budgets ≤ total budget
4. **Tool validation**: Tools exist and are available
5. **Reference validation**: Variable references resolve

### Validation Errors

```go
type ValidationIssue struct {
    Code    string
    Message string
}

type ValidationError struct {
    Issues []ValidationIssue
}
```

## Examples

### Research Summary Template

```yaml
name: research_summary
version: "1.0.0"
description: "Research a topic and provide a structured summary"

defaults:
  budget_agent_max: 10000
  require_approval: false

nodes:
  - id: discover
    type: simple
    strategy: react
    tools_allowlist: [web_search]
    budget_max: 1500

  - id: expand
    type: cognitive
    strategy: chain_of_thought
    tools_allowlist: [web_search, web_fetch]
    budget_max: 3000
    depends_on: ["discover"]

  - id: synthesize
    type: cognitive
    strategy: tree_of_thoughts
    budget_max: 5000
    depends_on: ["expand"]

edges:
  - from: discover
    to: expand
  - from: expand
    to: synthesize
```

### Complex DAG Workflow

```yaml
name: market_analysis
version: "1.0.0"
description: "Parallel market research and analysis"

defaults:
  budget_agent_max: 15000
  require_approval: false

nodes:
  - id: competitors
    type: dag
    budget_max: 6000
    metadata:
      tasks:
        - id: competitor_1
          query: "Analyze competitor A"
          tools: [web_search]
        - id: competitor_2
          query: "Analyze competitor B"
          tools: [web_search]
        - id: competitor_3
          query: "Analyze competitor C"
          tools: [web_search]

  - id: market_trends
    type: cognitive
    strategy: chain_of_thought
    budget_max: 3000

  - id: synthesis
    type: supervisor
    budget_max: 6000
    depends_on: ["competitors", "market_trends"]
```

### Derived Template: Executive Research

```yaml
name: research_summary_enterprise
description: "Enterprise version with executive brief output"
extends:
  - research_summary

defaults:
  model_tier: large
  budget_agent_max: 18000
  require_approval: true

nodes:
  - id: explore
    budget_max: 4000
    metadata:
      exploration_depth: deep
  - id: finalize
    metadata:
      summary_style: executive_brief
      include_risks: true
```

### Playbook Workflow with Composition

```yaml
name: market_analysis_playbook
description: "Market analysis with compliance and templated reporting"
extends:
  - market_analysis

defaults:
  require_approval: true

nodes:
  - id: compliance_review
    type: simple
    strategy: react
    depends_on: [synthesize]
    tools_allowlist: [policy_checker]
    budget_max: 1200
  - id: report
    metadata:
      report_template: playbook_v1

edges:
  - from: synthesize
    to: compliance_review
  - from: compliance_review
    to: report
```

## Best Practices

### Template Design

1. **Start simple**: Begin with basic templates and add complexity gradually
2. **Set realistic budgets**: Monitor actual usage and adjust
3. **Use dependencies wisely**: Minimize sequential dependencies for better parallelism
4. **Enable degradation**: Allow patterns to degrade for budget flexibility
5. **Version carefully**: Use semantic versioning for breaking changes

### Performance Optimization

1. **Parallel execution**: Use DAG nodes for independent tasks
2. **Tool allowlists**: Restrict tools to necessary ones only
3. **Reference minimization**: Pass only required data between nodes
4. **Budget allocation**: Give more budget to complex reasoning nodes

### Monitoring

1. **Token usage**: Track actual vs. budgeted usage
2. **Degradation frequency**: Monitor pattern degradation rates
3. **Success rates**: Measure template completion rates
4. **Learning effectiveness**: Track router prediction accuracy

## API Reference

### Template Registration

```go
// Load templates from directory
registry, err := workflows.InitTemplateRegistry(logger, "/path/to/templates")

// Get specific template
entry, found := registry.Get("research_summary@1.0.0")

// List all templates
summaries := registry.List()
```

### Execution

Use the OrchestratorService SubmitTask API and specify the template in the request context. The router will execute TemplateWorkflow deterministically:

```
grpcurl -plaintext -d '{
  "query": "Summarize quantum computing",
  "context": { "template": "research_summary", "template_version": "1.0.0" }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### Monitoring

Operational metrics (execution counts, tokens, success rates) are exported via Prometheus. See service dashboards and metrics naming in the codebase (e.g., internal/metrics).

## Migration Guide

### Converting Existing Workflows

1. **Identify patterns**: Look for repeated task structures
2. **Extract templates**: Create YAML definitions
3. **Test locally**: Validate templates before deployment
4. **Monitor performance**: Compare token usage before/after
5. **Iterate**: Refine based on actual usage

### Rollback Strategy

Templates are version-gated in Temporal workflows:

```go
version := workflow.GetVersion(ctx, "template_router_v1",
    workflow.DefaultVersion, 1)
if version >= 1 {
    // Use template system
} else {
    // Use legacy routing
}
```

## Troubleshooting

### Common Issues

#### Templates Not Loading
- Check `TEMPLATES_PATH` environment variable
- Verify YAML syntax with validators
- Check container logs for validation errors

#### Budget Exceeded
- Review node budget allocations
- Enable pattern degradation
- Check for infinite loops in DAG

#### Poor Learning Router Performance
- Increase exploration rate temporarily
- Clear stale patterns from memory
- Review confidence thresholds

### Debug Logging

```bash
# Enable debug logging
export DEBUG_TEMPLATES=true
export LOG_LEVEL=debug

# View template loading
docker compose logs orchestrator | grep -i template

# Monitor execution
docker compose exec orchestrator cat /var/log/templates.log
```

## Future Enhancements

### Planned Features

1. **Hot reload**: Dynamic template updates without restart
2. **Template marketplace**: Share templates across organizations
3. **Visual editor**: Web-based template designer
4. **A/B testing**: Automatic performance comparison
5. **Cost optimization**: ML-based budget allocation

### Experimental Features

1. **Adaptive budgets**: Dynamic budget adjustment based on complexity
2. **Template composition**: Combine templates into larger workflows
3. **Conditional branching**: Complex if/then/else logic
4. **External triggers**: Event-driven template execution
