# Workflow Pricing Coverage

## Overview

This document tracks which Shannon workflows have true per-model cost calculation vs approximate/default pricing.

**Last Updated**: September 2025

## Coverage Status

| Workflow | True Model Cost | Split Tokens | Implementation Status | Notes |
|----------|----------------|--------------|----------------------|--------|
| **Simple** | ✅ | ❌ | Production Ready | Uses `ModelUsed` for accurate per-model pricing |
| **DAG v2** | ✅ | ✅ | Production Ready | Full per-agent model + split token tracking |
| **Supervisor** | ✅ | ✅ | Production Ready | Tracks per-child model + split tokens |
| **React** | ✅ | ✅ | Production Ready | Captures all agent executions with model/tokens |
| **Streaming** | ✅ | ✅ | Production Ready | Executes agent once for real token counts |
| **ParallelStreaming** | ✅ | ✅ | Production Ready | Per-stream model + token tracking |
| **Exploratory** | ❌ | ❌ | Uses Defaults | Aggregate tokens only, no model tracking |
| **Scientific** | ❌ | ❌ | Uses Defaults | Aggregate tokens only, no model tracking |

## Implementation Details

### Fully Implemented (True Cost)

**Simple Workflow**
- Passes `ModelUsed` from agent execution to session update
- File: `go/orchestrator/internal/workflows/simple_workflow.go`

**DAG v2 Workflow**
- Collects `AgentExecutionResult` for each agent
- Builds `AgentUsage[]` with model + split tokens
- Files: `go/orchestrator/internal/workflows/strategies/dag.go`

**Supervisor Workflow**
- Tracks per-child `AgentExecutionResult`
- Passes `AgentUsage[]` with `InputTokens`/`OutputTokens` when available
- File: `go/orchestrator/internal/workflows/supervisor_workflow.go`

**React Pattern**
- `ReactLoopResult` includes `AgentResults []AgentExecutionResult`
- Captures reasoning and action agent executions
- Files: 
  - `go/orchestrator/internal/workflows/patterns/react.go`
  - `go/orchestrator/internal/workflows/strategies/react.go`

**Streaming Workflows**
- `StreamExecute` executes agent once via `executeAgentCore` for real `TokenUsage`
- `BatchStreamExecute` returns `[]AgentExecutionResult` with actual tokens
- Both single and parallel streaming now report true costs
- Files: 
  - `go/orchestrator/internal/activities/streaming.go`
  - `go/orchestrator/internal/workflows/streaming_workflow.go`

### Using Default Pricing

**Exploratory Workflow**
- Uses Tree-of-Thoughts pattern which returns aggregate `TotalTokens`
- No per-agent model tracking
- File: `go/orchestrator/internal/workflows/strategies/exploratory.go`

**Scientific Workflow**
- Uses hypothesis testing patterns with aggregate token counts
- No per-model cost breakdown
- File: `go/orchestrator/internal/workflows/strategies/scientific.go`

## Token Split Support

When available from the LLM provider, the following information is captured:
- `InputTokens` (prompt tokens)
- `OutputTokens` (completion tokens)
- Mapped from `resp.Metrics.TokenUsage.PromptTokens/CompletionTokens`
- File: `go/orchestrator/internal/activities/agent.go`

## Pricing Calculation Hierarchy

1. **Most Accurate**: Per-agent with model + split tokens
   - Used by: DAG, Supervisor, React, Streaming
   
2. **Accurate**: Single model + total tokens
   - Used by: Simple workflow
   
3. **Approximate**: Default pricing with total tokens
   - Used by: Exploratory, Scientific
   - Logs warning and increments `shannon_pricing_fallback_total` metric

## Monitoring

Use the `shannon_pricing_fallback_total` metric to track workflows using default pricing:
```promql
# Check fallback usage by reason
sum by (reason) (rate(shannon_pricing_fallback_total[5m]))

# Alert on high fallback rate
rate(shannon_pricing_fallback_total[5m]) > 0.1
```

## Future Improvements

### Low Priority (Defer)
- **Exploratory/Scientific Patterns**: Would require significant refactoring of Tree-of-Thoughts, Debate, Chain-of-Thought patterns to surface individual agent results
- **Recommendation**: Monitor usage metrics first; implement if these patterns show significant production usage

### Completed
- ✅ Simple workflow model tracking
- ✅ DAG v2 per-agent costs
- ✅ Supervisor child tracking
- ✅ React pattern integration
- ✅ Streaming true token costs
- ✅ Input/output token split
- ✅ Fallback metrics
- ✅ Hot-reload pricing config
