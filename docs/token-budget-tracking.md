# Token Budget and Cost Tracking

## Overview

Shannon implements a dual-path token tracking system that ensures accurate cost reporting across all workflow patterns while preventing duplicate recordings when budgets are enabled.

**Key Principle**: Every agent execution records token usage **exactly once**, through either the budgeted activity (when budgets are enabled) or the workflow pattern (when budgets are disabled).

## Architecture

### Recording Paths

```
┌─────────────────────────────────────────────────────────────────┐
│                     Workflow Execution                          │
└────────────────┬────────────────────────────────────────────────┘
                 │
                 ├── Budget Enabled? ──────┐
                 │                          │
        ┌────────▼─────────┐     ┌─────────▼──────────┐
        │   YES (Budget)   │     │  NO (Non-Budget)   │
        └────────┬─────────┘     └──────────┬─────────┘
                 │                           │
                 ▼                           ▼
    ┌────────────────────────┐   ┌──────────────────────────┐
    │ ExecuteAgentWithBudget │   │    ExecuteAgent          │
    │    (activity)          │   │    (activity)            │
    └────────┬───────────────┘   └──────────┬───────────────┘
             │                               │
             ▼                               │
    ┌────────────────────────┐              │
    │ Records token usage    │              │
    │ in activity (budget.go)│              │
    └────────────────────────┘              │
                                             ▼
                                  ┌──────────────────────────┐
                                  │ Pattern records tokens   │
                                  │ (parallel/react/etc.)    │
                                  └──────────────────────────┘
                 │                           │
                 └───────────┬───────────────┘
                             ▼
                ┌────────────────────────────┐
                │    token_usage table       │
                │  (one record per agent)    │
                └────────────────────────────┘
```

## Core Components

### 1. Budgeted Execution Activity

**File**: `go/orchestrator/internal/activities/budget.go`

**Function**: `ExecuteAgentWithBudget`

**Responsibilities**:
- Enforces token budget limits per agent
- Calls LLM service with token constraints
- **Records token usage immediately** (line 346-356)
- Returns execution result to workflow

**Recording Logic**:
```go
// Line 346-356: Records usage inside the activity
err = b.budgetManager.RecordUsage(ctx, &budget.BudgetTokenUsage{
    UserID:         input.UserID,
    SessionID:      input.AgentInput.SessionID,
    TaskID:         input.TaskID,
    AgentID:        input.AgentInput.AgentID,
    Model:          actualModel,
    Provider:       actualProvider,
    InputTokens:    inputTokens,
    OutputTokens:   outputTokens,
    IdempotencyKey: idempotencyKey,
})
```

**When Used**:
- `budgetPerAgent > 0` (Parallel, Sequential, Hybrid patterns)
- `opts.BudgetAgentMax > 0` (React, Chain-of-Thought, Debate, Tree-of-Thoughts)
- SupervisorWorkflow with budget constraints

---

### 2. Non-Budgeted Execution Activity

**File**: `go/orchestrator/internal/activities/agent.go`

**Function**: `ExecuteAgent`

**Responsibilities**:
- Executes agent without budget constraints
- Calls LLM service
- **Does NOT record token usage** (pattern does it)
- Returns execution result to workflow

**When Used**:
- SimpleTaskWorkflow
- Workflows without budget constraints
- Decompose/Refine/Synthesis phases (always non-budgeted)

---

### 3. Workflow Pattern Recording

All workflow patterns implement conditional token recording to avoid duplicates:

#### Guard Pattern

```go
// Budgeted path: Activity records, pattern skips
if budgetPerAgent <= 0 {  // or opts.BudgetAgentMax <= 0
    _ = workflow.ExecuteActivity(ctx, constants.RecordTokenUsageActivity, ...)
}
```

#### Patterns with Recording

| Pattern | File | Guard Location | Agent IDs |
|---------|------|----------------|-----------|
| **Parallel** | `patterns/execution/parallel.go` | Line 267 | Result AgentID |
| **Sequential** | `patterns/execution/sequential.go` | Line 289 | Result AgentID |
| **React** | `patterns/react.go` | Lines 154, 367, 493 | reasoner, action, synthesizer |
| **Chain-of-Thought** | `patterns/chain_of_thought.go` | Lines 145, 236 | cot-reasoner, cot-clarifier |
| **Debate** | `patterns/debate.go` | Lines 201, 354 | debater AgentIDs |
| **Tree-of-Thoughts** | `patterns/tree_of_thoughts.go` | Lines 272-350 | tot-generator-{id} |
| **Reflection** | `patterns/wrappers.go`, `patterns/reflection.go` | Lines 212-245 (initial), 123-159 (synth) | reflection-initial, reflection-synth |

#### Patterns without Recording (Delegates)

| Pattern | File | Delegation Target |
|---------|------|-------------------|
| **Hybrid** | `patterns/execution/hybrid.go` | Delegates to Parallel (line 275) |

---

### 4. Workflow-Level Recording (Always Records)

Some workflow phases always record usage (no budget constraints apply):

**OrchestratorWorkflow - Decompose**
- File: `internal/workflows/orchestrator_router.go:218`
- AgentID: `"decompose"`
- Metadata: `{"phase": "decompose"}`

**SupervisorWorkflow - Decompose**
- File: `internal/workflows/supervisor_workflow.go:337`
- AgentID: `"decompose"`
- Metadata: `{"phase": "decompose"}`

**ResearchWorkflow - Phases**
- Refine: `strategies/research.go:204` → AgentID: `"research-refiner"`
- Decompose: `strategies/research.go:265` → AgentID: `"decompose"`
- Synthesis: `strategies/research.go:826` → AgentID: `"synthesis"`

**SimpleTaskWorkflow**
- File: `internal/workflows/simple_workflow.go:487`
- AgentID: `"simple-agent"`
- Always records (single agent, no budget splitting)

---

## Token Usage Recording

### Database Schema

**Table**: `token_usage`

```sql
CREATE TABLE token_usage (
    id                uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id           uuid,
    provider          varchar(50)  NOT NULL,
    model             varchar(255) NOT NULL,
    prompt_tokens     integer      NOT NULL,
    completion_tokens integer      NOT NULL,
    total_tokens      integer      NOT NULL,
    cost_usd          numeric(10,6) NOT NULL,
    created_at        timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    task_id           uuid REFERENCES task_executions(id) ON DELETE CASCADE
);
```

**Indexes**:
- Primary key on `id`
- Foreign key on `task_id` → `task_executions(id)`
- Index on `user_id` for user-level cost queries
- Index on `provider`, `model` for provider/model analytics
- Index on `created_at` for time-series queries

---

### Recording Flow

```
┌──────────────────────────────────────────────────────────────────┐
│ 1. LLM Provider Returns Response                                │
│    - model_used: "gpt-5-2025-08-07"                             │
│    - provider: "openai"                                         │
│    - input_tokens: 732                                          │
│    - output_tokens: 1464                                        │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 2. Activity Captures Token Counts                               │
│    - AgentExecutionResult.InputTokens = 732                     │
│    - AgentExecutionResult.OutputTokens = 1464                   │
│    - AgentExecutionResult.ModelUsed = "gpt-5-2025-08-07"        │
│    - AgentExecutionResult.Provider = "openai"                   │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 3. Recording Decision (Budgeted vs Non-Budgeted)                │
│                                                                  │
│  IF budgetPerAgent > 0:                                         │
│    → ExecuteAgentWithBudget records in activity (budget.go:346) │
│    → Workflow pattern SKIPS recording (guard prevents duplicate)│
│                                                                  │
│  ELSE (budgetPerAgent <= 0):                                    │
│    → ExecuteAgent does NOT record                               │
│    → Workflow pattern records via RecordTokenUsageActivity      │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 4. RecordTokenUsageActivity (activities/session.go)             │
│    - Resolves task_id from workflow_id                          │
│    - Calculates cost: pricing.CostForSplit(model, in, out)      │
│    - Inserts into token_usage table                             │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 5. Database Persistence (PostgreSQL)                            │
│    INSERT INTO token_usage (                                    │
│      user_id, provider, model,                                  │
│      prompt_tokens, completion_tokens, total_tokens,            │
│      cost_usd, task_id                                          │
│    ) VALUES (...)                                               │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 6. Workflow Aggregation (metadata/aggregate.go)                 │
│    - Sums all token_usage records for task                      │
│    - Calculates total cost                                      │
│    - Populates TaskResult.Metadata:                             │
│      {                                                           │
│        "model_used": "gpt-5-2025-08-07",                        │
│        "provider": "openai",                                    │
│        "input_tokens": 4362,                                    │
│        "output_tokens": 8727,                                   │
│        "total_tokens": 13089,                                   │
│        "cost_usd": 0.183258                                     │
│      }                                                           │
└────────────────────┬─────────────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ 7. API Response (GET /api/v1/tasks/{id})                        │
│    {                                                             │
│      "task_id": "task-...",                                     │
│      "status": "TASK_STATUS_COMPLETED",                         │
│      "usage": {                                                 │
│        "total_tokens": 13089,                                   │
│        "prompt_tokens": 4362,                                   │
│        "completion_tokens": 8727,                               │
│        "cost_usd": 0.183258                                     │
│      },                                                          │
│      "metadata": {                                              │
│        "model_used": "gpt-5-2025-08-07",                        │
│        "provider": "openai"                                     │
│      }                                                           │
│    }                                                             │
└──────────────────────────────────────────────────────────────────┘
```

---

## Zero-Token Executions

### Tool-Only Paths

Some agent executions return **0 LLM tokens** by design when no LLM inference occurs:

**Common Scenarios**:
- **Forced tool calls**: Agent uses tools without LLM reasoning (`forced_tools` mode)
- **Cache hits**: Response served entirely from cache (future feature)
- **Tool-only workflows**: Workflows that only execute tools without text generation

**Example** (from ResearchWorkflow):
```go
// Tool-only web search execution
result := executeWebSearch(ctx, query)
// Returns: InputTokens=0, OutputTokens=0, ToolExecutions=[...]
```

See [Deep Research Overview](deep-research-overview.md#gap-filling-and-convergence) for details on gap-filling tool execution.

---

### Recording Behavior

**Default**: Zero-token executions are **NOT recorded** to `token_usage` table

**Rationale**:
- No LLM cost to track
- No billable token consumption
- Reduces database writes for non-LLM operations

**Implementation**:
```go
// Pattern recording guards (parallel.go, sequential.go)
if result.TokensUsed == 0 && result.InputTokens == 0 {
    if recordZeroToken, ok := context["record_zero_token"].(bool); !ok || !recordZeroToken {
        logger.Warn("Skipping zero-token recording (no LLM inference occurred)")
        return nil  // Skip recording
    }
}
```

**Locations**:
- `go/orchestrator/internal/workflows/patterns/execution/sequential.go:316-380`
- `go/orchestrator/internal/workflows/patterns/execution/parallel.go:295-320`

---

### Enabling Zero-Token Recording

To record zero-token executions for **audit trail purposes**:

**Via API Context**:
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Task with tool-only execution",
    "context": {
      "record_zero_token": true
    }
  }'
```

**Via Workflow Context**:
```go
baseContext := map[string]interface{}{
    "record_zero_token": true,  // Enable zero-token recording
}
```

**When Enabled**:
- Zero-token records inserted into `token_usage` table
- `cost_usd` = 0.0
- Metadata can distinguish tool-only vs. LLM executions

**Use Cases**:
- Compliance/audit requirements (track all agent executions)
- Debugging tool execution workflows
- Analytics on tool-only vs. LLM paths

---

### Distinguishing Zero-Token Types

When reviewing `token_usage` records with 0 tokens:

```sql
-- Find zero-token executions
SELECT
    tu.model,
    tu.provider,
    tu.total_tokens,
    tu.cost_usd,
    te.metadata->>'tool_executions' as tools_used
FROM token_usage tu
JOIN task_executions te ON tu.task_id = te.id
WHERE tu.total_tokens = 0
ORDER BY tu.created_at DESC;
```

**Interpretation**:
- `tools_used IS NOT NULL` → Forced tool call (expected zero)
- `tools_used IS NULL` → Potential issue (LLM should have returned tokens)

---

## Preventing Duplicate Recordings

### The Problem (Before Fix)

**Issue**: Budgeted execution recorded usage twice:
1. **Activity recording**: `budget.go:346` (inside ExecuteAgentWithBudget)
2. **Pattern recording**: `parallel.go:289` (workflow-level)

**Impact**: 2× token count, 2× cost calculation, duplicate database rows

**Example**:
```
Actual usage: 2,196 tokens ($0.030744)
Recorded:     4,392 tokens ($0.061488)  ← 2× overcount
```

---

### The Solution (Option B - Conditional Guards)

**Strategy**: Prevent pattern-level recording when budgets are enabled

**Implementation**: Guard all pattern-level recording with budget checks

```go
// BEFORE (duplicates)
_ = workflow.ExecuteActivity(ctx, constants.RecordTokenUsageActivity, ...)

// AFTER (no duplicates)
if budgetPerAgent <= 0 {  // Only record if NOT budgeted
    _ = workflow.ExecuteActivity(ctx, constants.RecordTokenUsageActivity, ...)
}
```

**Guard Variations**:
- Parallel/Sequential/Hybrid: `if budgetPerAgent <= 0`
- React/CoT/Debate: `if opts.BudgetAgentMax <= 0`
- Tree-of-Thoughts: `if tokenBudget > 0 { budgeted } else { record }`

---

### Verification

**Test Query**:
```sql
-- Check for duplicate recordings (should return 0 rows)
SELECT
  prompt_tokens,
  completion_tokens,
  model,
  COUNT(*) as record_count
FROM token_usage tu
JOIN task_executions te ON tu.task_id = te.id
WHERE te.workflow_id = 'task-...'
GROUP BY prompt_tokens, completion_tokens, model
HAVING COUNT(*) > 1;
```

**Expected Result**: 0 rows (no duplicates)

---

## Budget Configuration

### Setting Budgets

**Via API**:
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Complex research task",
    "context": {
      "budget_agent_max": 50000,  # Max tokens per agent
      "budget_total_max": 200000  # Max tokens for entire workflow
    }
  }'
```

**Via Template**:
```yaml
name: research_workflow
defaults:
  budget_agent_max: 30000
  budget_total_max: 150000
```

**Via Environment**:
```bash
# Default budgets in .env
BUDGET_AGENT_MAX=40000
BUDGET_TOTAL_MAX=200000
```

---

### Budget Enforcement

**SupervisorWorkflow Example**:
```go
// Determine budget per agent
agentMaxTokens := 0
if v, ok := baseContext["budget_agent_max"].(int); ok {
    agentMaxTokens = v
}

// Calculate per-agent budget
budgetPerAgent := 0
if agentMaxTokens > 0 && len(tasks) > 0 {
    budgetPerAgent = agentMaxTokens / len(tasks)
}

// Execute with budget
if budgetPerAgent > 0 {
    // Budgeted path: activity records
    err := workflow.ExecuteActivity(ctx,
        constants.ExecuteAgentWithBudgetActivity,
        activities.BudgetedAgentInput{
            AgentInput: agentInput,
            MaxTokens:  budgetPerAgent,
            UserID:     input.UserID,
            TaskID:     workflowID,
            ModelTier:  modelTier,
        }).Get(ctx, &result)
} else {
    // Non-budgeted path: pattern records
    err := workflow.ExecuteActivity(ctx,
        activities.ExecuteAgent,
        agentInput).Get(ctx, &result)
}
```

---

## Cost Calculation

### Pricing Configuration

**File**: `config/models.yaml`

```yaml
pricing:
  defaults:
    combined_per_1k: 0.005
  models:
    openai:
      gpt-5-2025-08-07:
        input_per_1k: 0.0060   # $0.006 per 1K input tokens
        output_per_1k: 0.0180  # $0.018 per 1K output tokens
```

See [centralized-pricing.md](centralized-pricing.md) for details.

---

### Calculation Logic

**File**: `go/orchestrator/internal/pricing/pricing.go`

**Function**: `CostForSplit`

```go
func CostForSplit(model string, inputTokens, outputTokens int) float64 {
    provider := DetectProvider(model)

    // Lookup pricing config
    inputPrice := getInputPricePerToken(provider, model)
    outputPrice := getOutputPricePerToken(provider, model)

    // Calculate cost
    inputCost := float64(inputTokens) * inputPrice
    outputCost := float64(outputTokens) * outputPrice

    return inputCost + outputCost
}
```

**Example**:
```
Model: gpt-5-2025-08-07
Input: 732 tokens × $0.000006 = $0.004392
Output: 1464 tokens × $0.000018 = $0.026352
Total: $0.030744
```

---

## Metadata Aggregation

### Aggregation Logic

**File**: `go/orchestrator/internal/metadata/aggregate.go`

**Function**: `AggregateAgentMetadata`

```go
func AggregateAgentMetadata(results []activities.AgentExecutionResult) map[string]interface{} {
    totalTokens := 0
    totalInputTokens := 0
    totalOutputTokens := 0
    totalCost := 0.0
    primaryModel := ""
    primaryProvider := ""

    for _, result := range results {
        totalTokens += result.TokensUsed
        totalInputTokens += result.InputTokens
        totalOutputTokens += result.OutputTokens

        // Cost calculation
        if result.InputTokens > 0 && result.OutputTokens > 0 {
            cost := pricing.CostForSplit(result.ModelUsed,
                                         result.InputTokens,
                                         result.OutputTokens)
            totalCost += cost
        }

        // Track primary model (first non-empty)
        if primaryModel == "" && result.ModelUsed != "" {
            primaryModel = result.ModelUsed
            primaryProvider = result.Provider
        }
    }

    return map[string]interface{}{
        "total_tokens":      totalTokens,
        "input_tokens":      totalInputTokens,
        "output_tokens":     totalOutputTokens,
        "cost_usd":          totalCost,
        "model_used":        primaryModel,
        "provider":          primaryProvider,
    }
}
```

---

### Workflow Usage

**All workflows** call aggregation before returning:

```go
// SimpleTaskWorkflow
metadata := map[string]interface{}{
    "model_used":    result.ModelUsed,
    "provider":      result.Provider,
    "input_tokens":  result.InputTokens,
    "output_tokens": result.OutputTokens,
    "total_tokens":  result.TokensUsed,
    "cost_usd":      pricing.CostForSplit(result.ModelUsed, ...),
}

// SupervisorWorkflow
metadata := metadata.AggregateAgentMetadata(allResults)

// ResearchWorkflow
metadata := metadata.AggregateAgentMetadata(agentResults)
```

---

## Monitoring and Debugging

### Database Queries

**Check token usage for a task**:
```sql
SELECT
    tu.model,
    tu.provider,
    tu.prompt_tokens,
    tu.completion_tokens,
    tu.total_tokens,
    tu.cost_usd,
    tu.created_at
FROM token_usage tu
JOIN task_executions te ON tu.task_id = te.id
WHERE te.workflow_id = 'task-...'
ORDER BY tu.created_at;
```

**Aggregate costs by task**:
```sql
SELECT
    te.workflow_id,
    COUNT(tu.id) as agent_count,
    SUM(tu.prompt_tokens) as total_input,
    SUM(tu.completion_tokens) as total_output,
    SUM(tu.total_tokens) as total_tokens,
    SUM(tu.cost_usd) as total_cost
FROM task_executions te
LEFT JOIN token_usage tu ON tu.task_id = te.id
WHERE te.user_id = '...'
GROUP BY te.workflow_id
ORDER BY total_cost DESC
LIMIT 10;
```

**Detect duplicate recordings**:
```sql
SELECT
    prompt_tokens,
    completion_tokens,
    model,
    COUNT(*) as duplicates
FROM token_usage
WHERE task_id = (
    SELECT id FROM task_executions WHERE workflow_id = 'task-...'
)
GROUP BY prompt_tokens, completion_tokens, model
HAVING COUNT(*) > 1;
```

---

### Orchestrator Logs

**Budget tracking**:
```bash
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | \
  grep "Budget used"
```

**Token recording**:
```bash
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | \
  grep "RecordTokenUsage"
```

**Cost calculation**:
```bash
docker compose -f deploy/compose/docker-compose.yml logs orchestrator | \
  grep "cost_usd"
```

---

### Temporal Workflow History

**View workflow events**:
```bash
docker compose exec temporal temporal workflow describe \
  --workflow-id task-... \
  --address temporal:7233
```

**Check activity executions**:
```bash
docker compose exec temporal temporal workflow show \
  --workflow-id task-... \
  --address temporal:7233 | \
  grep -A 5 "RecordTokenUsageActivity"
```

---

## Best Practices

### 1. Always Use Split Token Counts

**Preferred**:
```go
cost := pricing.CostForSplit(model, inputTokens, outputTokens)
```

**Avoid**:
```go
cost := pricing.CostForTokens(model, totalTokens)  // Less accurate
```

**Reason**: Input and output tokens have different prices

---

### 2. Propagate Model Information

**Required in AgentExecutionResult**:
- `ModelUsed` (e.g., "gpt-5-2025-08-07")
- `Provider` (e.g., "openai")
- `InputTokens` (prompt tokens)
- `OutputTokens` (completion tokens)

**Example**:
```go
result := activities.AgentExecutionResult{
    Response:      llmResponse.Content,
    TokensUsed:    llmResponse.Usage.TotalTokens,
    InputTokens:   llmResponse.Usage.PromptTokens,
    OutputTokens:  llmResponse.Usage.CompletionTokens,
    ModelUsed:     llmResponse.Model,
    Provider:      "openai",
    AgentID:       agentID,
}
```

---

### 3. Guard Pattern-Level Recording

**When adding new patterns**, always guard recording:

```go
// Check if budgeted execution is active
if budgetPerAgent <= 0 {  // or opts.BudgetAgentMax <= 0
    // Record token usage
    _ = workflow.ExecuteActivity(ctx,
        constants.RecordTokenUsageActivity,
        activities.TokenUsageInput{
            UserID:       userID,
            SessionID:    sessionID,
            TaskID:       workflowID,
            AgentID:      agentID,
            Model:        result.ModelUsed,
            Provider:     result.Provider,
            InputTokens:  result.InputTokens,
            OutputTokens: result.OutputTokens,
            Metadata:     map[string]interface{}{"phase": "pattern_name"},
        }).Get(ctx, nil)
}
```

---

### 4. Include Phase Metadata

**Purpose**: Distinguish recording sources in analytics

**Examples**:
- `{"phase": "parallel"}`
- `{"phase": "react-reasoner"}`
- `{"phase": "decompose"}`
- `{"phase": "synthesis"}`

**Usage**:
```sql
-- Analyze token usage by phase
SELECT
    metadata->>'phase' as phase,
    COUNT(*) as executions,
    AVG(total_tokens) as avg_tokens,
    SUM(cost_usd) as total_cost
FROM token_usage
GROUP BY metadata->>'phase'
ORDER BY total_cost DESC;
```

---

### 5. Verify No Duplicates After Changes

**After modifying token recording logic**:

1. Run a test workflow with budgets enabled
2. Query for duplicates:
```sql
SELECT COUNT(*) FROM (
    SELECT prompt_tokens, completion_tokens, model, COUNT(*)
    FROM token_usage
    WHERE task_id IN (
        SELECT id FROM task_executions
        WHERE workflow_id = 'test-task-...'
    )
    GROUP BY prompt_tokens, completion_tokens, model
    HAVING COUNT(*) > 1
) duplicates;
```
3. Expected result: `0`

---

## Related Documentation

- [Centralized Pricing Configuration](centralized-pricing.md) - Pricing configuration and cost calculation
- [Rate-Aware Budgeting](rate-aware-budgeting.md) - Rate limit management and control
- [Multi-Agent Workflow Architecture](multi-agent-workflow-architecture.md) - Workflow patterns and execution
- [Pattern Usage Guide](pattern-usage-guide.md) - When to use each workflow pattern
- [API Reference](api-reference.md) - REST API endpoints for task submission and status

---

## Troubleshooting

### Missing Token Counts

**Symptom**: `input_tokens` and `output_tokens` are 0 in API response

**Causes**:
1. LLM provider not returning token counts
2. Agent activity not capturing token counts
3. Fallback to `TokensUsed` (total only)

**Solution**:
```go
// Ensure LLM response includes token counts
if result.InputTokens == 0 && result.OutputTokens == 0 {
    if result.TokensUsed > 0 {
        // Approximate split (60/40)
        result.InputTokens = result.TokensUsed * 6 / 10
        result.OutputTokens = result.TokensUsed - result.InputTokens
    }
}
```

---

### Duplicate Recordings

**Symptom**: Token counts are 2× expected value

**Causes**:
1. Pattern-level recording not guarded
2. Budget check missing or incorrect
3. Both activity and pattern recording

**Solution**:
1. Add budget guard to pattern recording
2. Verify guard condition: `if budgetPerAgent <= 0`
3. Run duplicate detection query
4. Review Temporal workflow history for duplicate activities

---

### Incorrect Costs

**Symptom**: Cost doesn't match expected value

**Causes**:
1. Wrong pricing configuration in `models.yaml`
2. Model name mismatch (e.g., "gpt-5" vs "gpt-5-2025-08-07")
3. Provider detection failure
4. Using combined pricing instead of split pricing

**Solution**:
1. Verify pricing in `config/models.yaml`
2. Check model name matches exactly: `SELECT DISTINCT model FROM token_usage`
3. Force provider: `result.Provider = "openai"`
4. Use `CostForSplit` instead of `CostForTokens`

---

### Budget Not Enforced

**Symptom**: Agent exceeds budget without error

**Causes**:
1. Budget not passed to activity
2. Budget check disabled
3. Using `ExecuteAgent` instead of `ExecuteAgentWithBudget`

**Solution**:
1. Verify budget in context: `budget_agent_max`
2. Check activity call:
```go
// Should use ExecuteAgentWithBudget
err := workflow.ExecuteActivity(ctx,
    constants.ExecuteAgentWithBudgetActivity,
    activities.BudgetedAgentInput{
        AgentInput: agentInput,
        MaxTokens:  budgetPerAgent,  // ← Ensure > 0
        ...
    })
```
3. Review budget manager logs: `grep "Budget exceeded"`
