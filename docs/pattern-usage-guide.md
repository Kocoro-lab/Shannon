# Pattern Usage Guide

## Quick Start

This guide provides practical examples of using Shannon's pattern library to build sophisticated multi-agent workflows.

## Pattern Catalog

### 1. Chain-of-Thought (CoT) Pattern

**Purpose**: Step-by-step reasoning with explicit thought progression

**When to Use**:
- Mathematical problems requiring showing work
- Logical proofs and derivations
- Complex analysis needing transparency
- Educational explanations

**Configuration Example**:
```go
config := patterns.ChainOfThoughtConfig{
    MaxSteps:              5,        // Maximum reasoning steps
    RequireExplanation:    true,     // Force step-by-step explanation
    ShowIntermediateSteps: true,     // Include steps in output
    ModelTier:             "large",  // Use stronger model for complex reasoning
}
```

**Input/Output Flow**:
```
Input: "Calculate the compound interest on $1000 at 5% for 3 years"
   ↓
Step 1: Identify the formula: A = P(1 + r)^t
Step 2: Substitute values: P=1000, r=0.05, t=3
Step 3: Calculate: 1000(1.05)^3
Step 4: Compute: 1000 × 1.157625
Step 5: Result: $1,157.63
   ↓
Output: Final amount is $1,157.63 with $157.63 in interest
```

### 2. Debate Pattern

**Purpose**: Multi-agent debate to explore different perspectives

**When to Use**:
- Controversial or subjective topics
- Decision making with trade-offs
- Exploring pros and cons
- Building consensus

**Configuration Example**:
```go
config := patterns.DebateConfig{
    NumDebaters:      3,              // Number of debating agents
    MaxRounds:        3,              // Debate rounds
    Perspectives:     []string{       // Assigned perspectives
        "optimistic",
        "pessimistic",
        "pragmatic",
    },
    RequireConsensus: false,          // Don't force agreement
    ModeratorEnabled: true,           // Use moderator for synthesis
    VotingEnabled:    true,           // Enable position voting
}
```

**Debate Flow**:
```
Query: "Should we migrate to microservices?"
   ↓
Round 1: Initial Positions
- Optimist: "Scalability and team independence"
- Pessimist: "Complexity and operational overhead"
- Pragmatist: "Depends on team size and growth plans"
   ↓
Round 2: Counter-arguments
- Each agent responds to others' points
   ↓
Round 3: Finding Common Ground
- Consensus on gradual migration approach
   ↓
Output: Synthesized recommendation with confidence scores
```

### 3. Tree-of-Thoughts (ToT) Pattern

**Purpose**: Systematic exploration of solution space with branching

**When to Use**:
- Problems with multiple solution paths
- Creative problem solving
- Strategic planning
- When backtracking might be needed

**Configuration Example**:
```go
config := patterns.TreeOfThoughtsConfig{
    MaxDepth:          4,             // Tree depth limit
    BranchingFactor:   3,             // Branches per node
    EvaluationMethod:  "scoring",    // How to evaluate paths
    PruningThreshold:  0.3,           // Min score to continue
    ExplorationBudget: 15,            // Max thoughts to explore
    BacktrackEnabled:  true,          // Allow returning to promising branches
}
```

**Exploration Tree**:
```
Query: "Design a fault-tolerant system"
        ↓
    [Root: System Design]
    /        |         \
[Active-     [Active-    [Passive
 Active]      Passive]    Backup]
   /  \         /  \        /  \
[Load  [Hot    [Primary [Cold  [Warm
 Balance] Standby] Secondary] Standby] Standby]
   ↓      ↓        ↓         ↓       ↓
(score) (score)  (score)  (score) (score)
        ↓
Best Path: Active-Passive → Primary-Secondary
```

### 4. React Pattern

**Purpose**: Reason-Act-Observe loops for interactive problem solving

**When to Use**:
- Tasks requiring tool use
- Debugging and troubleshooting
- Iterative refinement
- Environmental interaction

**Configuration Example**:
```go
config := patterns.ReactConfig{
    MaxIterations:     5,             // Maximum React loops
    ObservationWindow: 3,             // Recent observations to track
    MaxObservations:   20,            // Total observation limit
    MaxThoughts:       10,            // Reasoning step limit
    MaxActions:        10,            // Action execution limit
}
```

**React Loop**:
```
Query: "Debug why the API is returning 500 errors"
   ↓
Thought: Need to check server logs
Action: execute_command("tail -n 50 /var/log/app.log")
Observation: "NullPointerException at line 45"
   ↓
Thought: Found NPE, need to check the code
Action: read_file("api/handler.py", line=45)
Observation: "user = request.user.profile"
   ↓
Thought: User might not have profile
Action: add_null_check("if hasattr(request.user, 'profile')")
Observation: Fixed and deployed
   ↓
Output: Resolved 500 error caused by missing user profiles
```

### 5. Reflection Pattern

**Purpose**: Iterative quality improvement through self-evaluation

**When to Use**:
- Final quality polish
- Critical outputs needing verification
- When initial results are below threshold
- Continuous improvement loops

**Configuration Example**:
```go
config := patterns.ReflectionConfig{
    Enabled:             true,
    MaxRetries:          3,           // Improvement iterations
    ConfidenceThreshold: 0.85,        // Target quality score
    Criteria: []string{               // Evaluation dimensions
        "accuracy",
        "completeness",
        "clarity",
    },
    TimeoutMs:           30000,       // Per-reflection timeout
}
```

**Reflection Process**:
```
Initial Result → Evaluate (Score: 0.6)
   ↓
Issues: Lacks specific examples, unclear conclusion
   ↓
Improved Result → Evaluate (Score: 0.8)
   ↓
Issues: Missing edge cases
   ↓
Final Result → Evaluate (Score: 0.9) ✓
```

## Pattern Composition Examples

### Example 1: Scientific Investigation

**Workflow**: ScientificWorkflow
**Composition**: CoT → Debate → ToT → Reflection

```go
// Phase 1: Generate hypotheses with Chain-of-Thought
hypotheses := ChainOfThought(
    "Generate 3 testable hypotheses for performance issue",
    ChainOfThoughtConfig{MaxSteps: 3, RequireExplanation: true},
)

// Phase 2: Test hypotheses through Debate
testResults := Debate(
    "Which hypothesis best explains the data?",
    DebateConfig{NumDebaters: 3, Perspectives: hypotheses},
)

// Phase 3: Explore implications with Tree-of-Thoughts
implications := TreeOfThoughts(
    "What are the implications of the winning hypothesis?",
    TreeOfThoughtsConfig{MaxDepth: 3, BranchingFactor: 2},
)

// Phase 4: Polish with Reflection
finalReport := Reflection(
    combineResults(hypotheses, testResults, implications),
    ReflectionConfig{ConfidenceThreshold: 0.9},
)
```

### Example 2: Complex Problem Solving

**Workflow**: ExploratoryWorkflow
**Composition**: ToT (primary) → Debate (if low confidence) → Reflection (final)

```go
// Primary exploration with Tree-of-Thoughts
exploration := TreeOfThoughts(
    "How to optimize database performance?",
    TreeOfThoughtsConfig{
        MaxDepth: 4,
        BranchingFactor: 3,
        BacktrackEnabled: true,
    },
)

// If confidence < threshold, apply Debate
if exploration.Confidence < 0.7 {
    debate := Debate(
        "Evaluate the top 3 optimization strategies",
        DebateConfig{
            NumDebaters: 3,
            Perspectives: exploration.TopPaths,
        },
    )
    exploration = mergeResults(exploration, debate)
}

// Final quality check
final := Reflection(
    exploration,
    ReflectionConfig{MaxRetries: 2},
)
```

### Example 3: Interactive Debugging

**Workflow**: ReactWorkflow with Reflection
**Composition**: React (main loop) → Reflection (result quality)

```go
// Main React loop for debugging
debugging := React(
    "Find and fix the memory leak",
    ReactConfig{
        MaxIterations: 10,
        ObservationWindow: 5,
    },
)

// Ensure solution quality
solution := Reflection(
    debugging.Solution,
    ReflectionConfig{
        Criteria: []string{"correctness", "completeness"},
    },
)
```

## Execution Patterns

### Parallel Execution

**Use When**: Tasks are independent
**Benefits**: Maximum throughput
**Limitations**: No shared context

```go
// Execute multiple independent analyses
results := ParallelExecution(
    subtasks: []Task{
        "Analyze performance metrics",
        "Review security logs",
        "Check error rates",
    },
    config: ParallelConfig{
        MaxConcurrency: 3,
        Timeout: 60000,
    },
)
```

### Sequential Execution

**Use When**: Tasks build on each other
**Benefits**: Context accumulation
**Limitations**: Slower overall

```go
// Step-by-step data pipeline
results := SequentialExecution(
    subtasks: []Task{
        "Extract data from source",
        "Transform to standard format",
        "Validate data quality",
        "Load into destination",
    },
    config: SequentialConfig{
        PassContext: true,
    },
)
```

### Hybrid Execution

**Use When**: Tasks have dependencies
**Benefits**: Optimal parallelism with constraints
**Limitations**: Complex dependency management

```go
// Execute with dependency graph
results := HybridExecution(
    tasks: map[string]Task{
        "A": {Query: "Task A"},
        "B": {Query: "Task B", Dependencies: []string{"A"}},
        "C": {Query: "Task C", Dependencies: []string{"A"}},
        "D": {Query: "Task D", Dependencies: []string{"B", "C"}},
    },
)
// Execution order: A → (B || C) → D
```

## Pattern Selection Decision Tree

```
Start → Is the problem well-defined?
         ├─ No → Use ExploratoryWorkflow (ToT + Debate)
         └─ Yes → Does it need multiple perspectives?
                   ├─ Yes → Does it need systematic testing?
                   │         ├─ Yes → Use ScientificWorkflow (CoT + Debate + ToT)
                   │         └─ No → Use Debate pattern
                   └─ No → Does it need tool interaction?
                           ├─ Yes → Use ReactWorkflow
                           └─ No → Is it complex reasoning?
                                   ├─ Yes → Use Chain-of-Thought
                                   └─ No → Use simple execution
```

## Performance Tips

### Token Optimization

1. **Start with simpler patterns** before complex compositions
2. **Use confidence thresholds** to skip unnecessary patterns
3. **Configure appropriate model tiers** for each pattern
4. **Set exploration budgets** for ToT pattern
5. **Limit debate rounds** based on convergence

### Quality vs Speed Trade-offs

| Priority | Pattern Choice | Configuration |
|----------|---------------|---------------|
| **Speed** | Skip Reflection, limit iterations | `MaxRetries: 1, MaxIterations: 3` |
| **Quality** | Add Reflection, increase exploration | `ConfidenceThreshold: 0.9, MaxDepth: 5` |
| **Balance** | Conditional patterns based on confidence | `if confidence < 0.7: add_pattern()` |

### Debugging Pattern Issues

**Pattern Not Converging**:
- Increase max iterations/rounds
- Adjust confidence thresholds
- Check if query is well-formed

**Excessive Token Usage**:
- Reduce branching factor (ToT)
- Limit debate rounds
- Use smaller model tiers
- Set tighter exploration budgets

**Poor Quality Results**:
- Add Reflection pattern
- Increase model tier
- Add more evaluation criteria
- Use multiple patterns

## Advanced Techniques

### Dynamic Pattern Selection

```go
func SelectPatternDynamically(query string, context map[string]interface{}) Pattern {
    complexity := analyzeComplexity(query)
    uncertainty := measureUncertainty(context)

    if complexity > 0.8 && uncertainty > 0.7 {
        return ComposePatterns(TreeOfThoughts, Debate, Reflection)
    } else if complexity > 0.5 {
        return ComposePatterns(ChainOfThought, Reflection)
    } else {
        return React
    }
}
```

### Pattern Result Caching

```go
// Cache pattern results for similar queries
cacheKey := hash(pattern_type, query, config)
if cached := cache.Get(cacheKey); cached != nil {
    return cached
}
result := pattern.Execute(query, config)
cache.Set(cacheKey, result, ttl=3600)
```

### Cross-Pattern Context Sharing

```go
// Share insights across patterns
sharedContext := map[string]interface{}{
    "previous_patterns": []string{"ChainOfThought"},
    "insights": cotResult.ReasoningSteps,
    "confidence": cotResult.Confidence,
}

// Next pattern can leverage previous insights
debateResult := Debate(query, config, sharedContext)
```

## Conclusion

Shannon's pattern library provides powerful building blocks for sophisticated AI reasoning. By understanding each pattern's strengths and optimal use cases, developers can compose effective workflows for any problem domain. Start with single patterns, measure their effectiveness, then gradually compose more complex workflows as needed.