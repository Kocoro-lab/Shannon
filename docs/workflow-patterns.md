# Workflow Patterns Guide

**Version**: 1.0  
**Audience**: Developers and users

## Overview

Shannon's embedded workflow engine supports 6 cognitive patterns, each optimized for different types of tasks. This guide helps you choose the right pattern and configure it effectively.

## Pattern Selection Matrix

| Pattern | Best For | Complexity | Speed | Quality | Cost |
|---------|----------|------------|-------|---------|------|
| **Chain of Thought** | Logical reasoning, math | Low | ★★★★ | ★★★ | $ |
| **Tree of Thoughts** | Planning, exploration | High | ★★ | ★★★★★ | $$$ |
| **Research** | Information gathering | Medium | ★★ | ★★★★ | $$ |
| **ReAct** | Multi-step tool usage | Medium | ★★★ | ★★★★ | $$ |
| **Debate** | Multiple perspectives | High | ★★ | ★★★★★ | $$$ |
| **Reflection** | Quality improvement | Medium | ★★★ | ★★★★ | $$ |

## Pattern Details

### Chain of Thought (CoT)

**When to Use**:
- Logical reasoning tasks
- Mathematical problems
- Step-by-step analysis
- Quick answers needed

**Configuration**:
```yaml
chain_of_thought:
  max_iterations: 5
  model: "claude-sonnet-4"
```

**Example Query**: "What is the derivative of x^2 + 3x + 5?"

**Typical Duration**: 3-7 seconds

---

### Tree of Thoughts (ToT)

**When to Use**:
- Planning tasks
- Exploring multiple solutions
- Complex decision making
- Need best solution from many options

**Configuration**:
```yaml
tree_of_thoughts:
  max_branches: 3
  max_depth: 4
  exploration_mode: "breadth_first"  # or "depth_first"
  pruning_threshold: 0.3
```

**Example Query**: "Plan a 3-day trip to Tokyo with $2000 budget"

**Typical Duration**: 30-60 seconds

---

### Research

**When to Use**:
- Information gathering
- Research tasks
- Fact-finding missions
- Need cited sources

**Configuration**:
```yaml
research:
  max_iterations: 3
  sources_per_round: 6
  min_sources: 8
  coverage_threshold: 0.8
  enable_verification: false
```

**Example Query**: "What are the latest developments in quantum computing?"

**Typical Duration**: 5-15 minutes

---

### ReAct (Reason-Act-Observe)

**When to Use**:
- Multi-step tool usage
- Iterative problem solving
- Need tool results to inform next steps

**Configuration**:
```yaml
react:
  max_iterations: 5
  available_tools:
    - web_search
    - calculator
    - web_fetch
```

**Example Query**: "What's the weather in the capital of France?"

**Typical Duration**: 10-30 seconds

---

### Debate

**When to Use**:
- Controversial topics
- Need multiple perspectives
- Exploring trade-offs
- Decision with pros/cons

**Configuration**:
```yaml
debate:
  num_agents: 3
  max_rounds: 3
  require_consensus: false
```

**Example Query**: "Should AI be regulated? Discuss pros and cons."

**Typical Duration**: 45-90 seconds

---

### Reflection

**When to Use**:
- Quality improvement needed
- Refine initial answer
- Self-critique valuable
- Multiple drafts beneficial

**Configuration**:
```yaml
reflection:
  max_iterations: 3
  quality_threshold: 0.8
```

**Example Query**: "Write a professional email requesting a meeting"

**Typical Duration**: 20-40 seconds

## Automatic Pattern Selection

The router automatically selects patterns based on query complexity:

**Simple (score <0.3)** → Chain of Thought  
**Medium (score 0.3-0.7)** → Research or ReAct  
**Complex (score >0.7)** → Tree of Thoughts or Debate

Override with `mode` or `cognitive_strategy` parameters.

## Pattern Comparison

### Speed vs Quality

```
Quality ↑
        │
    ★★★★│    Debate       ToT
        │
    ★★★ │  Research    Reflection
        │       CoT
    ★★  │         ReAct
        │
    ★   │
        └──────────────────────────→ Speed
           ★   ★★   ★★★   ★★★★
```

### Cost Optimization

**Lowest Cost**:
1. Chain of Thought (1-2 LLM calls)
2. ReAct (2-5 LLM calls)  
3. Reflection (3-6 LLM calls)

**Highest Cost**:
1. Tree of Thoughts (10-20 LLM calls)
2. Debate (9-12 LLM calls)
3. Research with Deep 2.0 (15-30 LLM calls)

## Best Practices

1. **Start Simple**: Try CoT first, upgrade if needed
2. **Use Research for Facts**: Always cite sources
3. **Limit Iterations**: More isn't always better
4. **Monitor Token Usage**: Track costs
5. **Cache Results**: Don't rerun expensive patterns

## Common Patterns by Use Case

**Customer Support**: Reflection (draft responses)  
**Data Analysis**: Chain of Thought (interpret data)  
**Market Research**: Research (gather information)  
**Product Planning**: Tree of Thoughts (explore options)  
**Decision Making**: Debate (weigh perspectives)  
**Automation**: ReAct (use tools)

## References

- [Main Documentation](embedded-workflow-engine.md)
- [Configuration Guide](workflow-configuration.md)
- [Debugging Guide](workflow-debugging.md)
