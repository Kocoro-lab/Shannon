# Learning Router Enhancements

## Overview

The learning router uses an **epsilon-greedy** algorithm to intelligently select strategies based on historical performance and exploration needs, with contextual pattern matching.

## Algorithm Design

### Epsilon-Greedy Selection

The router uses epsilon-greedy exploration with a 10% exploration rate:
- 90% of the time: Exploit the best-performing strategy based on historical success
- 10% of the time: Explore by trying different strategies

### Strategy Score Calculation

The router calculates a score for each strategy based on historical performance:

```
Score = Success Rate + Contextual Boost - Performance Penalties
```

### Components

#### 1. Success Rate (0.0 - 1.0)
- Base component from historical success/failure ratio
- Weighted by recency (recent outcomes matter more)

```go
successRate := float64(successes) / float64(attempts)
```

#### 2. Exploration (Epsilon-Greedy)
- 10% chance to select random strategy for exploration
- Ensures continuous learning of new patterns

```go
if rand.Float64() < 0.1 {  // 10% exploration
    return selectRandomStrategy()
}
```

#### 3. Penalties

**Latency Penalty** (0.0 - 0.2):
```go
if avgLatency > targetLatency {
    penalty = min(0.2, (avgLatency - targetLatency) / targetLatency * 0.1)
}
```

**Token Efficiency Penalty** (0.0 - 0.15):
```go
if avgTokens > budgetTarget {
    penalty = min(0.15, (avgTokens - budgetTarget) / budgetTarget * 0.1)
}
```

#### 4. Keyword Boost (0.0 - 0.1)
- Boosts strategies matching query patterns
- Based on TF-IDF similarity

```go
if keywordMatch > threshold {
    boost = min(0.1, similarity * 0.2)
}
```

### Final Confidence

Confidence is clamped to [0, 1]:

```go
confidence := max(0.0, min(1.0, ucbScore))
```

## Implementation

### RecommendWorkflowStrategy

```go
func RecommendWorkflowStrategy(ctx context.Context,
    input RecommendStrategyInput) (RecommendStrategyOutput, error) {

    // Fetch historical patterns from supervisor memory
    patterns := SearchDecompositionPatterns(input.SessionID, input.Query)

    // Epsilon-greedy selection
    if rand.Float64() < 0.1 { // 10% exploration
        return selectRandomStrategy()
    }

    // Calculate scores for each strategy
    scores := make(map[StrategyType]float64)
    for strategy, stats := range patterns {
        score := calculateStrategyScore(strategy, stats, input.Query)
        scores[strategy] = score
    }

    // Select highest scoring strategy
    best := selectBestStrategy(scores)

    return RecommendStrategyOutput{
        Strategy:   best.Strategy,
        Confidence: best.Confidence,
        Source:     "epsilon_greedy",
    }, nil
}
```

### Strategy Score Calculation

```go
func calculateStrategyScore(strategy StrategyType,
    stats StrategyStats, query string) float64 {

    // Base success rate from historical data
    successRate := stats.SuccessRate()

    // Latency penalty
    latencyPenalty := 0.0
    if stats.AvgLatencyMs > targetLatencyMs {
        ratio := (stats.AvgLatencyMs - targetLatencyMs) / targetLatencyMs
        latencyPenalty = math.Min(0.2, ratio * 0.1)
    }

    // Token efficiency penalty
    tokenPenalty := 0.0
    if stats.AvgTokens > targetTokens {
        ratio := (stats.AvgTokens - targetTokens) / targetTokens
        tokenPenalty = math.Min(0.15, ratio * 0.1)
    }

    // Contextual boost from query similarity
    contextBoost := calculateContextualBoost(strategy, query)

    // Composite score
    score := successRate - latencyPenalty - tokenPenalty + contextBoost

    // Clamp to [0, 1]
    return math.Max(0.0, math.Min(1.0, score))
}
```

## Strategy Patterns

### Pattern Recognition

The system learns query patterns for each strategy:

```go
type PatternSignature struct {
    Keywords     []string
    Complexity   float64
    ToolsNeeded  []string
    OutputFormat string
}

func matchPattern(query string,
    signature PatternSignature) float64 {
    // TF-IDF similarity
    keywordScore := tfidfSimilarity(query, signature.Keywords)

    // Complexity matching
    queryComplexity := estimateComplexity(query)
    complexityMatch := 1.0 - math.Abs(queryComplexity -
                                      signature.Complexity)

    // Tool requirement matching
    requiredTools := detectRequiredTools(query)
    toolMatch := jaccardSimilarity(requiredTools,
                                   signature.ToolsNeeded)

    // Weighted combination
    return keywordScore*0.5 + complexityMatch*0.3 + toolMatch*0.2
}
```

### Strategy Profiles

Each strategy has a learned profile:

| Strategy | Keywords | Complexity | Typical Tools | Success Domains |
|----------|----------|------------|---------------|-----------------|
| Tree of Thoughts | analyze, explore, compare, evaluate | High | web_search, calculator | Research, analysis |
| Chain of Thought | explain, describe, summarize | Medium | web_search | Explanations, summaries |
| ReAct | find, get, retrieve, calculate | Low | All tools | Data retrieval, calculations |
| Debate | pros/cons, compare, argue | High | web_search | Decision making |
| Reflection | improve, refine, review | Medium | None | Quality improvement |

## Confidence Scoring

### Confidence Levels

| Confidence | Meaning | Action |
|------------|---------|--------|
| 0.9 - 1.0 | Very High | Use recommended strategy |
| 0.7 - 0.89 | High | Use with pattern degradation allowed |
| 0.5 - 0.69 | Medium | Consider alternatives |
| 0.3 - 0.49 | Low | Prefer exploration |
| 0.0 - 0.29 | Very Low | Use default strategy |

### Confidence Factors

Confidence is influenced by:

1. **Sample size**: More data → higher confidence
2. **Recency**: Recent successes → higher confidence
3. **Consistency**: Stable performance → higher confidence
4. **Context match**: Similar queries → higher confidence

```go
func adjustConfidence(baseConfidence float64,
    stats StrategyStats) float64 {

    // Sample size factor
    sampleFactor := math.Min(1.0, float64(stats.Attempts) / 100.0)

    // Recency factor (exponential decay)
    recencyFactor := calculateRecencyWeight(stats.LastSuccess)

    // Consistency factor (low variance = high consistency)
    consistencyFactor := 1.0 - stats.SuccessVariance()

    // Weighted adjustment
    adjusted := baseConfidence *
                (0.4 + sampleFactor*0.2 +
                 recencyFactor*0.2 +
                 consistencyFactor*0.2)

    return math.Max(0.0, math.Min(1.0, adjusted))
}
```

## Storage and Retrieval

### Vector Store Integration

Patterns are stored in Qdrant with embeddings:

```go
type DecompositionPattern struct {
    ID           string
    Query        string
    QueryVector  []float32  // Embedding
    Strategy     string
    Success      bool
    TokensUsed   int
    LatencyMs    int64
    Timestamp    time.Time
    Metadata     map[string]interface{}
}
```

### Similarity Search

Finding similar patterns:

```go
func findSimilarPatterns(query string, limit int) []Pattern {
    // Generate query embedding
    embedding := generateEmbedding(query)

    // Search in Qdrant
    results := qdrant.Search(
        collection: "decomposition_patterns",
        vector: embedding,
        limit: limit,
        threshold: 0.75,  // Minimum similarity
    )

    return results
}
```

## Metrics and Monitoring

### Key Metrics

```prometheus
# Strategy selection distribution
shannon_strategy_selection_total{strategy="tree_of_thoughts"} 142
shannon_strategy_selection_total{strategy="chain_of_thought"} 298
shannon_strategy_selection_total{strategy="react"} 521

# Decomposition patterns recorded
shannon_decomposition_patterns_recorded_total 961

# Learning source breakdown
shannon_strategy_selection_total{source="epsilon_greedy"} 865
shannon_strategy_selection_total{source="exploration"} 96

# Pattern cache performance
shannon_decomposition_pattern_cache_hits_total 432
shannon_decomposition_pattern_cache_misses_total 529
```

### Performance Tracking

```sql
-- Strategy performance over time
SELECT
    strategy,
    DATE(timestamp) as date,
    AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END) as success_rate,
    AVG(tokens_used) as avg_tokens,
    AVG(latency_ms) as avg_latency,
    COUNT(*) as attempts
FROM decomposition_patterns
WHERE timestamp > NOW() - INTERVAL '7 days'
GROUP BY strategy, date
ORDER BY date DESC, strategy;
```

## Configuration

### Tuning Parameters

```yaml
learning_router:
  # Exploration rate (epsilon-greedy)
  epsilon: 0.1

  # UCB parameters
  exploration_weight: 2.0  # C in UCB formula

  # Penalty thresholds
  target_latency_ms: 5000
  target_tokens: 3000

  # Confidence thresholds
  min_confidence: 0.3
  high_confidence: 0.7

  # History window
  pattern_retention_days: 30
  max_patterns_per_session: 1000

  # Keyword matching
  keyword_boost_weight: 0.1
  min_keyword_similarity: 0.6
```

### A/B Testing

Compare strategies with controlled experiments:

```yaml
experiments:
  strategy_comparison:
    enabled: true
    control_strategy: "chain_of_thought"
    test_strategy: "ucb_router"
    traffic_split: 0.5
    metrics:
      - success_rate
      - avg_tokens
      - avg_latency
    duration_hours: 168  # 1 week
```

## Best Practices

### 1. Cold Start Handling

For new sessions without history, use heuristics:

```go
if len(patterns) == 0 {
    // Exploration mode: randomly select to build initial data
    if rand.Float64() < 0.3 {  // 30% exploration during cold start
        return selectRandomStrategy()
    }
    // Otherwise use query complexity heuristic
    complexity := estimateComplexity(query)
    if complexity > 0.7 {
        return "tree_of_thoughts", 0.5
    } else if complexity > 0.4 {
        return "chain_of_thought", 0.5
    }
    return "react", 0.5
}
```

### 2. Strategy Fallback

Always have a fallback chain:

```go
strategies := []StrategyType{
    recommendedStrategy,
    "chain_of_thought",  // Fallback 1
    "react",             // Fallback 2
}
```

### 3. Continuous Learning

Update patterns after each execution:

```go
defer recordPattern(PatternRecord{
    Query:      input.Query,
    Strategy:   selectedStrategy,
    Success:    result.Success,
    TokensUsed: result.TokensUsed,
    LatencyMs:  elapsed.Milliseconds(),
})
```

### 4. Privacy Considerations

- Anonymize patterns before storage
- Use differential privacy for aggregates
- Implement data retention policies
- Allow user opt-out

## Future Enhancements

1. **Thompson Sampling**: Bayesian approach for exploration/exploitation
2. **Contextual Bandits**: Consider additional context features for selection
3. **Deep Learning**: Neural network-based strategy prediction
4. **Transfer Learning**: Share patterns across similar domains
5. **Dynamic Exploration**: Adjust epsilon rate based on performance variance
6. **Hierarchical Strategies**: Multi-level strategy selection

## Algorithm Notes

The current implementation uses **epsilon-greedy** (not UCB) because:
- Simpler to implement and understand
- No need to track visit counts per strategy
- More predictable exploration behavior
- Works well with limited historical data

Future versions may explore UCB variants (UCB1, Thompson Sampling) for more sophisticated exploration strategies.