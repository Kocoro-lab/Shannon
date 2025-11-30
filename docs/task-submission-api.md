# HTTP Task Submission API

This page documents all parameters for submitting tasks via the HTTP Gateway.

## Endpoints

- POST `/api/v1/tasks` — submit a task
- POST `/api/v1/tasks/stream` — submit and receive a stream URL (201)

Response headers include `X-Workflow-ID` and `X-Session-ID`.

## Top‑Level Body

- `query` (string, required) — user query or command
- `session_id` (string, optional) — session continuity (UUID or custom)
- `context` (object, optional) — execution context (see below)
- `mode` (string, optional) — `simple` | `standard` | `complex` | `supervisor` — workflow routing
  - `simple`: Direct to SimpleTaskWorkflow (no decomposition)
  - `standard`: Router with standard complexity hint
  - `complex`: Router with high complexity hint
  - `supervisor`: Direct to SupervisorWorkflow (multi-agent)
  - Default: Auto-detect based on query complexity
- `model_tier` (string, optional) — `small` | `medium` | `large`
  - Injected into `context.model_tier` and honored by services
- `model_override` (string, optional) — specific model name (e.g., `gpt-5-2025-08-07`, `gpt-5-pro-2025-10-06`, `claude-sonnet-4-5-20250929`)
  - Top-level alternative to `context.model_override`
- `provider_override` (string, optional) — force specific provider (e.g., `openai`, `anthropic`)
  - Top-level alternative to `context.provider_override`

### Research Strategy Controls (mapped into context)

These optional fields are validated by the Gateway and then added to the workflow `context`:

- `research_strategy` — `quick | standard | deep | academic`
- `max_concurrent_agents` — integer (1..20)
- `enable_verification` — boolean (enables claim verification when citations exist)

#### Model Tier Architecture (Cost Optimization)

Research strategies use a tiered model architecture for cost efficiency:

| Activity Type | Model Tier | Rationale |
|--------------|------------|-----------|
| Utility activities (coverage eval, fact extraction, subquery gen) | small | Structured output tasks |
| Agent execution (quick) | small | Fast, cheap research |
| Agent execution (standard) | medium | Balanced quality/cost |
| Agent execution (deep) | medium | Iterative refinement compensates |
| Agent execution (academic) | medium | Iterative refinement compensates |
| Final synthesis | large | User-facing quality critical |

This tiered approach reduces costs by 50-70% while maintaining output quality. See `config/research_strategies.yaml` for configuration.

**Note**: The `max_iterations` parameter is accepted by the gateway for backward compatibility but is not used by current workflows. Use `context.react_max_iterations` to control ReAct loop depth instead.

### Deep Research 2.0 Controls

When `context.force_research: true` is set, Deep Research 2.0 is **enabled by default**:

- `context.iterative_research_enabled` — boolean (default: `true`) — Enable/disable iterative coverage loop
- `context.iterative_max_iterations` — integer (1-5, default: `3`) — Max iterations for coverage improvement
- `context.enable_fact_extraction` — boolean (default: `false`) — Extract structured facts into metadata

```bash
# Basic Deep Research 2.0 (default settings)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "AI trends 2025", "context": {"force_research": true}}'

# Custom iterations
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "Compare LLMs", "context": {"force_research": true, "iterative_max_iterations": 2}}'

# Disable 2.0 (use legacy)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"query": "Explain ML", "context": {"force_research": true, "iterative_research_enabled": false}}'
```

### Research Strategy Presets

Apply preset configurations for research workflows:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Weekly research briefing",
    "research_strategy": "deep",
    "max_concurrent_agents": 7,
    "enable_verification": true,
    "context": {
      "react_max_iterations": 4
    }
  }'
```

### Citations (optional, mapped into context)

- `context.enable_citations` — boolean toggle for citation collection/integration in non‑research workflows
  - ReactWorkflow: opt‑in. When true, collects citations from tool outputs (e.g., web_search, web_fetch) and appends a Sources section to the final report.
  - DAGWorkflow: opt‑out. Enabled by default; set to false to skip citation collection and Sources section.
  - ResearchWorkflow: unchanged; always manages citations internally.

Example (enable citations in React):

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Explain XKCD-style encryption best practices",
    "mode": "standard",
    "context": {"enable_citations": true}
  }'
```

### Full Context Example

Combining session, mode, model tier, and custom parameters:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Summarize our Q3 results",
    "session_id": "sales-2025-q3",
    "mode": "supervisor",
    "model_tier": "large",
    "context": {
      "role": "analysis",
      "prompt_params": {"profile_id": "49598h6e", "current_date": "2025-10-25"}
    }
  }'
```

## Recognized `context.*` Keys

- `role` — role preset (e.g., `analysis`, `research`, `writer`). When present, the orchestrator bypasses `/agent/decompose` and creates a single-subtask plan, letting the role-specific agent handle any internal multi-step/tool logic.
- `system_prompt` — overrides role prompt; supports `${var}` from `prompt_params`
- `prompt_params` — arbitrary params for prompts/tools/adapters
- `model_tier` — fallback when top‑level not provided
- `model_override` — specific model (e.g., `gpt-5-2025-08-07`, `gpt-5-nano-2025-08-07`, `gpt-5-pro-2025-10-06`)
  - Can be specified at top-level or in context
  - Falls back to next provider if model unavailable on primary provider
- `provider_override` — force specific provider (e.g., `openai`, `anthropic`, `google`)
  - Can be specified at top-level or in context
  - Short-circuits provider selection logic
  - Falls back to tier-based selection if provider fails
- `template` — template name
- `template_version` — template version (string)
- `template_name` — alias for `template` (accepted)
- `disable_ai` — true to require template only (no AI fallback)
  - ⚠️ Cannot be combined with `model_tier`, `model_override`, or `provider_override` (returns 400)
- Advanced context window controls:
  - `history_window_size`, `use_case_preset`, `primers_count`, `recents_count`
  - `compression_trigger_ratio`, `compression_target_ratio`
- Deep Research 2.0 controls (when `force_research: true`):
  - `iterative_research_enabled` — Enable/disable iterative loop (default: `true`)
  - `iterative_max_iterations` — Max iterations 1-5 (default: `3`)
  - `enable_fact_extraction` — Extract structured facts (default: `false`)

## Common Scenarios

### Full-Featured AI Execution

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Analyze August website traffic trends",
    "session_id": "analytics-session-123",
    "mode": "supervisor",
    "model_tier": "large",
    "context": {
      "role": "data_analytics",
      "system_prompt": "You are a data analyst specializing in web analytics.",
      "prompt_params": {
        "profile_id": "49598h6e",
        "aid": "fcb1cd29-9104-47b1-b914-31db6ba30c1a",
        "current_date": "2025-10-31"
      },
      "history_window_size": 75,
      "primers_count": 3,
      "recents_count": 20,
      "compression_trigger_ratio": 0.75,
      "compression_target_ratio": 0.375
    }
  }'
```

**Parameter Annotations:**
- `query` (REQUIRED) — Task to execute
- `session_id` (OPTIONAL) — Session ID for multi-turn conversations (auto-generated if omitted)
- `mode` (OPTIONAL) — Execution mode: "simple" or "supervisor" (default: based on complexity)
- `model_tier` (OPTIONAL) — Model size: "small", "medium", or "large" (default: "small")
- `context.role` (OPTIONAL) — Role preset name (e.g., "analysis", "research", "writer")
- `context.system_prompt` (OPTIONAL) — Custom system prompt (overrides role preset)
- `context.prompt_params` (OPTIONAL) — Arbitrary key-value pairs passed to tools/adapters
  - `profile_id`, `aid`, `current_date` are EXAMPLES — use any keys you need
- `context.history_window_size` (OPTIONAL) — Max conversation history messages (default: 50)
- `context.primers_count` (OPTIONAL) — Early messages to keep (default: 5)
- `context.recents_count` (OPTIONAL) — Recent messages to keep (default: 15)
- `context.compression_trigger_ratio` (OPTIONAL) — Trigger at % of window (default: 0.8)
- `context.compression_target_ratio` (OPTIONAL) — Compress to % of window (default: 0.5)

### Force Model Tier

Top-level `model_tier` wins over `context.model_tier`:

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Complex analysis", "model_tier": "large"}'
```

### Force Specific Model

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Write a plan", "context": {"model_override": "gpt-5-2025-08-07"}}'
```

### Template-Only Execution (No AI)

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Weekly research briefing",
    "context": {
      "template": "research_summary",
      "template_version": "1.0.0",
      "disable_ai": true,
      "prompt_params": {"week": "2025-W44"}
    }
  }'
```

### Supervisor Mode

```bash
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Assess system reliability", "mode": "supervisor"}'
```

### Override Model (Top-Level)

```bash
# Force specific model with top-level override
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Write a plan", "model_override": "gpt-5-2025-08-07"}'

# Example with a specific GPT‑5 model
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Analyze data", "model_override": "gpt-5-2025-08-07"}'
```

### Override Provider

```bash
# Force OpenAI provider
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Count to 5", "provider_override": "openai"}'

# Force Anthropic provider via context
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"query": "Explain quantum computing", "context": {"provider_override": "anthropic"}}'
```

### Complex Mode with Overrides

```bash
# Combine mode, model, and provider overrides
curl -sS -X POST http://localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Comprehensive market analysis",
    "mode": "complex",
    "model_override": "gpt-5-pro-2025-10-06",
    "provider_override": "openai"
  }'
```

## Validation & Priority Rules

- `model_tier`: only `small|medium|large` (400 on invalid)
- `mode`: only `simple|standard|complex|supervisor` (400 on invalid)
- Top‑level `model_tier` overrides `context.model_tier`
- Top‑level `model_override` overrides `context.model_override`
- Top‑level `provider_override` overrides `context.provider_override`
- `template_name` is accepted as an alias for `template`
- **Conflict Validation**: `disable_ai: true` cannot be combined with:
  - `model_tier` (top-level or context)
  - `model_override` (top-level or context)
  - `provider_override` (top-level or context)
  - Gateway returns 400 with error message when conflicts detected

## Task Status Response Example

After submitting a task, poll `GET /api/v1/tasks/{id}`. Typical response shape when completed:

```json
{
  "task_id": "task-...",
  "status": "TASK_STATUS_COMPLETED",
  "result": "...",
  "model_used": "gpt-5-mini-2025-08-07",
  "provider": "openai",
  "usage": {
    "total_tokens": 300,
    "input_tokens": 200,
    "output_tokens": 100,
    "estimated_cost": 0.006
  }
}
```

## ⚠️ Common Mistakes to Avoid

**1. Don't use both `template` and `template_name`**
```json
// ❌ BAD: Redundant aliases
{"context": {"template": "research_summary", "template_name": "research_summary"}}

// ✅ GOOD: Use template only
{"context": {"template": "research_summary"}}
```

**2. Don't combine `disable_ai: true` with model parameters (Gateway returns 400)**
```json
// ❌ BAD: Conflict - gateway rejects with HTTP 400
{"context": {"disable_ai": true, "model_tier": "large"}}
{"model_tier": "large", "context": {"disable_ai": true}}
{"model_override": "gpt-5-2025-08-07", "context": {"disable_ai": true}}
{"context": {"disable_ai": true, "provider_override": "openai"}}

// ✅ GOOD: Template-only execution (no model params)
{"context": {"template": "summary", "disable_ai": true}}

// ✅ GOOD: AI execution with model control (no disable_ai)
{"model_tier": "large", "model_override": "gpt-5-2025-08-07"}
{"context": {"provider_override": "openai"}}
```

**3. Don't duplicate `model_tier` (top-level wins)**
```json
// ❌ BAD: Confusing duplicate (context.model_tier ignored)
{"model_tier": "large", "context": {"model_tier": "small"}}

// ✅ GOOD: Specify once at top level
{"model_tier": "large"}

// ✅ GOOD: Or only in context (as fallback)
{"context": {"model_tier": "large"}}
```

## Notes

### Model Selection Behavior
- `model_override` selects a specific model by name
- `provider_override` forces provider selection (short-circuits tier-based routing)
- When both specified: use specified model from specified provider
- Falls back to next provider if primary fails

### Model Naming
Use canonical model names only (no legacy aliases). If a model is unavailable on the chosen provider, tier-based fallback applies.

### Fallback Behavior
- If specified model unavailable on requested provider → tries next provider
- If specified provider fails → falls back to tier-based selection
- System prioritizes task completion over strict parameter adherence

### Additional Features
- Use header `Idempotency-Key` to safely retry submissions; gateway caches 2xx responses for 24h.
- SSE streaming endpoint is returned by `POST /api/v1/tasks/stream`.
- All context parameters are optional; defaults are applied when not specified.

### Response Format
- **Gateway API Response**: `/api/v1/tasks/{task_id}` returns a `result` field containing the raw LLM response
  - The `result` field contains plain text or JSON string responses
  - An optional `response` field contains parsed JSON if the result is valid JSON (for backward compatibility)
