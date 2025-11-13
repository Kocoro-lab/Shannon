# Shannon Deep Research Agent — Overview and Guide

## Executive Summary

Shannon’s Deep Research agent (v2) runs a multi‑stage workflow to produce high‑quality, cited research outputs with strong cost accounting. It combines query refinement, plan/decomposition, execution via patterns (React/Parallel/Hybrid/Sequential), citation collection, LLM‑based synthesis, optional reflection, and verification. Token usage is recorded once per step with guards to avoid duplicates, both for budgeted and non‑budgeted runs.

---

## Workflow at a Glance

Query → Memory Retrieval → Refine → Decompose → Execute → Entity Filter → Cite → Gap Fill → Synthesize → Reflect → Verify → Result

- **Memory Retrieval**: hierarchical (recent + semantic) or session memory injection
- **Context Compression**: automatic compression for long conversations (>20 messages)
- **Refine**: expand vague queries; detect entity (canonical name, exact queries, domains)
- **Decompose**: create subtasks + model‑aware strategy selection
- **Execute**: React/Parallel/Hybrid/Sequential patterns (with optional react_per_task for deep research)
- **Entity Filter**: prune off-entity tool results using canonical name
- **Cite**: extract/deduplicate/score citations from tool outputs
- **Gap Fill**: detect undercovered areas and trigger targeted re-search
- **Synthesize**: LLM synthesis with continuation logic and coverage checks
- **Reflect**: evaluate quality; re‑synthesize with feedback when needed
- **Verify** (optional): claim verification vs. citations

Key file: `go/orchestrator/internal/workflows/strategies/research.go`

---

## Architecture Diagram

```
Query → Decomposition → Information Gathering (Parallel)
                        ├─ web_search + quality scoring
                        ├─ web_fetch (deep content)
                        └─ Source diversity enforcement
                        ↓
                     Citation Collection
                        ├─ Deduplicate & normalize URLs/DOIs
                        ├─ Score quality & credibility
                        └─ Enforce diversity (max 3/domain)
                        ↓
                     Verification Layer
                        ├─ Extract claims
                        ├─ Cross‑reference against sources (with snippets)
                        └─ Calculate confidence scores
                        ↓
                     Synthesis
                        ├─ Inline citations [1], [2] when available
                        ├─ Structured report format
                        └─ Language matching
                        ↓
                     Reflection + Report Generation
```

---

## Stage Details and Code Pointers

### Memory Retrieval (Step -1)

**Purpose**: Inject conversational context from previous interactions to enable coherent multi-turn research.

**Memory Types** (version-gated for determinism):
1. **Hierarchical Memory** (priority): Combines recent + semantic retrieval
   - Recent memory: Last 5 messages from session (temporal context)
   - Semantic memory: Top 5 relevant past interactions (similarity ≥ 0.75)
   - Sources tracked: `recent`, `semantic`, or `both`
   - Activity: `FetchHierarchicalMemory`

2. **Session Memory** (fallback): Simple chronological retrieval
   - Top 20 recent messages from session
   - Activity: `FetchSessionMemory`

**Injection**:
- Memory items stored in `baseContext["agent_memory"]`
- Available to all downstream agents (refine, decompose, execution patterns)
- Empty memory → workflow proceeds without context

**Files**:
- `go/orchestrator/internal/workflows/strategies/research.go:237-279`
- `go/orchestrator/internal/activities/memory.go`

---

### Context Compression (Step -0.5)

**Purpose**: Automatically compress long conversation histories to prevent context window overflow.

**Triggers** (version-gated as `context_compress_v1`):
- Session has >20 messages
- Estimated tokens exceed model tier window threshold
- Rate-limited to prevent excessive compression calls

**Process**:
1. `CheckCompressionNeeded`: Evaluates message count + token estimates against model capacity
2. `CompressAndStoreContext`: LLM summarizes conversation (target: 37.5% of window)
3. `UpdateCompressionState`: Records compression timestamp in session metadata

**Files**:
- `go/orchestrator/internal/workflows/strategies/research.go:281-329`
- `go/orchestrator/internal/activities/compression.go`

---

### 1) Refine (Step 0)

- Activity: `RefineResearchQuery`
- Output: refined_query, research_areas, canonical_name, exact_queries, official_domains, disambiguation_terms
- **Memory Context**: Uses injected memory for query expansion based on conversation history
- Injects context for downstream execution and entity filtering
- Files: `go/orchestrator/internal/activities/research_refine.go`

### 2) Decompose (Step 1)
- Activity: `DecomposeTask`
- Output: `DecompositionResult{Subtasks[], ComplexityScore}`
- Strategy routing:
  - complexity < 0.5 → React
  - has dependencies → Hybrid
  - else → Parallel
- **Forced Tool Injection**: Research workflows automatically inject `web_search` into all subtasks to ensure citation collection, even when decomposition doesn't suggest tools
- Files: `go/orchestrator/internal/workflows/strategies/research.go`

### 3) Execute (Step 2)

**Pattern Selection** based on complexity and dependencies:
- **React** (complexity < 0.5): Iterative reason→act→observe for simpler research
- **Parallel** (no dependencies): Concurrent subtasks with web_search injection
- **Hybrid** (has dependencies): Dependency-aware fan-in/fan-out + topological sort
- **Sequential** (explicit ordering): Ordered subtasks with result passing

**React Per Task** (deep research enhancement, version-gated):
- **Trigger conditions**:
  1. Manual: `context.react_per_task = true`
  2. Auto-enable: complexity > 0.7 AND strategy ∈ {deep, academic}

- **Behavior**: Replaces simple agents with mini ReAct loops (reason→act→observe) for each subtask
  - Enables iterative refinement per research area
  - Configurable max iterations via `context.react_max_iterations` (2–8, default 5)
  - Parallel execution: ReAct loops run concurrently across subtasks
  - Sequential execution: ReAct loops respect dependencies via topological sort

- **When to use**:
  - High-complexity research requiring iterative exploration per area
  - Academic research where each subtask needs deep investigation
  - When decomposition produces nuanced subtasks needing multi-step reasoning

**Files**:
- Main routing: `go/orchestrator/internal/workflows/strategies/research.go:534-722`
- React pattern: `go/orchestrator/internal/workflows/patterns/react.go`
- Parallel/Hybrid/Sequential: `go/orchestrator/internal/workflows/patterns/execution/*.go`

---

### 4) Entity Filter (Step 2.5)
- Filters off‑entity tool results using canonical name and official domains
- Keeps reasoning‑only outputs; prunes off‑entity tool outputs
- File: `go/orchestrator/internal/workflows/strategies/research.go`

### 5) Cite (Step 3)

- Extract, sanitize, deduplicate, enforce diversity, score, and rank
- **Entity Filtering** (when canonical_name detected):
  - Scoring: domain match +0.6, alias in URL +0.4, text match +0.4
  - Threshold: 0.3 (any single match passes)
  - Safety floor: minKeep=8 citations (backfilled by quality×credibility)
  - Official domains always preserved (bypass threshold)
- Output: top N citations with quality/credibility stats
- File: `go/orchestrator/internal/metadata/citations.go`

---

### 6) Gap Filling (Step 3.5)

**Purpose**: Detect and fill undercovered research areas through targeted re-search.

**Version-gated** as `gap_filling_v1` (max 2 iterations to prevent runaway loops).

**Gap Detection** (`analyzeGaps`):
- Missing section headings (`### Area Name`)
- Gap indicator phrases (e.g., "limited information", "insufficient data", "未找到足够信息")
- Low citation density (< 2 inline citations per section)

**Gap Resolution**:
1. Build targeted queries: `"Find detailed information about: {area} (related to: {original_query})"`
2. Execute focused ReAct loops (max 3 iterations per gap)
3. Re-collect citations from gap results (global deduplication with original citations)
4. Re-synthesize with combined evidence using large tier

**Iteration Tracking**: `context.gap_iteration` prevents infinite loops

**Files**:
- Main logic: `go/orchestrator/internal/workflows/strategies/research.go:1090-1246`
- Gap analysis: `research.go:1503-1555`
- Query builder: `research.go:1558-1567`

---

### 7) Synthesize (Step 4)

- Activity: `SynthesizeResultsLLM` (LLM‑first, fallback to simple)
- **Model Tier**:
  - Initial synthesis: Defaults to `medium` tier (gpt-5-mini) for cost efficiency
  - Gap-filling re-synthesis: Uses `large` tier (gpt-4.1/claude-opus-4-1/gemini-2.5-pro) for highest quality
  - Override with `context.synthesis_model_tier` parameter to explicitly control tier selection
- Prompt directives:
  - Language matching (respond in query language)
  - Mandatory research area coverage (subsection per area)
  - Output structure (comprehensive vs. concise)
  - Available citations list (formatted as `[n] Title (URL) - Source, Date`)

**Continuation Logic** (handles model output truncation):
- **Trigger**: `finish_reason="stop"` AND `!looksComplete()` AND remaining tokens < adaptive margin
- **Adaptive margin**: min(25% of effective_max_completion, 300 tokens)
- **Continuation prompt**: "Continue from last sentence; maintain headings and citation style; no preamble"

**looksComplete() Validation** (comprehensive checks):
1. **Sentence ending**: Must end with `.`, `!`, `?`, or `。` (CJK period)
2. **No dangling phrases**: Rejects trailing conjunctions (`and, or, but, however, therefore, thus, additionally, moreover, furthermore, meanwhile, consequently, subsequently`)
3. **Per-area coverage** (strict validation):
   - Extract each research area's section via `### {Area Name}` heading
   - Measure section content until next heading (`### ` or `## `)
   - Requirements per section:
     - ≥600 characters (substantive content, not placeholder text)
     - ≥2 unique inline citations (`[1]`, `[2]`, etc.)
   - **Fails if ANY area is undercovered**

**Files**:
- Synthesis + continuation: `go/orchestrator/internal/activities/synthesis.go:748-940`
- Tier override: `go/orchestrator/internal/workflows/strategies/research.go:1030, 1204`
- Tier propagation: `go/orchestrator/internal/activities/synthesis.go:480-487`

---

### 8) Reflect (Step 5)
- Evaluate quality; if below threshold, re‑synthesize with feedback
- Files: `go/orchestrator/internal/workflows/patterns/reflection.go`

### 9) Verify (Step 6, optional)

- **VerifyClaims** activity validates assertions vs. citations
- Sends per-citation snippets for grounded confidence scoring
- Pluggable; disabled by default (enable via `context.enable_verification = true`)

---

## Token Accounting and Cost Accuracy

Budgeted runs
- Per‑agent usage recorded inside `ExecuteAgentWithBudget` (activity)
- Patterns skip `RecordTokenUsageActivity` to avoid duplicates

Non‑budgeted runs
- Patterns record per‑agent usage once via `RecordTokenUsageActivity` with 60/40 split fallback when only totals are available

Patterns covered (non‑budgeted recording enabled)
- Parallel, React (reason/action/synth), Sequential, Chain‑of‑Thought (main + clarify), Debate (initial + rounds), Tree‑of‑Thoughts (branch generation), Reflection (initial + re‑synthesis). Hybrid inherits Parallel.

Safety nets
- Server aggregates token_usage if final task cost is zero
- Model/provider fallbacks fill missing fields for accurate pricing

### Forced Tool Executions (Zero-Token Optimization)

Some agents execute tools without calling the LLM, resulting in 0 token usage records:

**What happens:**
1. Decomposition phase pre-computes exact tool calls with parameters
2. Parallel agents receive `tool_parameters` with forced tool specification
3. Agent-core detects forced tools → bypasses LLM → executes tool directly
4. Returns raw tool output (search results, fetched content, etc.)
5. Records 0 tokens in token_usage table (no LLM call occurred)

**Example from logs:**
```
[INFO] ENFORCING tool execution from orchestration parameters - bypassing LLM choice
[INFO] Executing LLM-suggested tool directly
[INFO] ExecuteTaskResponse (direct tool): token_usage=None, tool=web_search, ms=428
```

**Why this is beneficial:**
- Saves 500-1,500 tokens per agent (no LLM reasoning or synthesis)
- Total savings: 5,000-10,000 tokens per research task with 4-6 parallel agents
- Raw tool outputs are synthesized once at the end by a single LLM call
- More efficient than having each agent independently synthesize partial results

**Database behavior:**
- `agent_executions` table: agent completes successfully with large output (20-30KB)
- `token_usage` table: 0 tokens recorded (empty model/provider fields)
- This is **by design** and not a bug

**When this occurs:**
- Parallel execution phase with pre-computed tool calls
- Decomposition identified simple information gathering tasks
- Tools like `web_search`, `web_fetch` with specific queries

### Forced Web Search Injection (Citation Guarantee)

Research workflows automatically inject `web_search` into all subtasks to ensure citation collection, regardless of what the decomposition model suggests.

**What happens:**
1. Decomposition may return subtasks with empty `suggested_tools` arrays for conceptual queries
2. Before execution, research workflow inspects all Parallel/Hybrid tasks
3. If `web_search` is missing from `SuggestedTools`, it's automatically appended
4. Agents now have access to web search even for conceptual/theoretical questions

**Why this is necessary:**
- Research workflows require external citations for credibility
- Decomposition models (especially smaller tiers) may not suggest tools for conceptual queries like "What is machine learning?"
- Without web searches, agents rely solely on internal knowledge → no citations collected
- This guarantee ensures every research report has citations from authoritative sources

**Implementation:**
- File: `go/orchestrator/internal/workflows/strategies/research.go`
- Injection logic for Parallel and Hybrid execution patterns
- ReAct pattern doesn't need injection (has access to all tools by default)

**Example:**
```go
// Before injection (from decomposition):
subtask.SuggestedTools = []  // empty

// After injection:
parallelTask.SuggestedTools = ["web_search"]
```

---

## Developer Notes and Navigation

- Main workflow orchestration: `go/orchestrator/internal/workflows/strategies/research.go`
- Synthesis continuation and coverage checks: `go/orchestrator/internal/activities/synthesis.go`
- Patterns:
  - React: `go/orchestrator/internal/workflows/patterns/react.go`
  - Parallel: `go/orchestrator/internal/workflows/patterns/execution/parallel.go`
  - Hybrid: `go/orchestrator/internal/workflows/patterns/execution/hybrid.go`
  - Sequential: `go/orchestrator/internal/workflows/patterns/execution/sequential.go`
  - Chain‑of‑Thought: `go/orchestrator/internal/workflows/patterns/chain_of_thought.go`
  - Debate: `go/orchestrator/internal/workflows/patterns/debate.go`
  - Tree‑of‑Thoughts: `go/orchestrator/internal/workflows/patterns/tree_of_thoughts.go`
  - Reflection: `go/orchestrator/internal/workflows/patterns/reflection.go`, `patterns/wrappers.go`
- Citations pipeline: `go/orchestrator/internal/metadata/citations.go`

---

## Behavior Guarantees (Concise)

- **Memory**: Hierarchical memory (recent + semantic) injected when available
- **Compression**: Automatic for sessions >20 messages to prevent context overflow
- **Coverage**: Each research area has dedicated subsection (###) with ≥600 chars and ≥2 inline citations
- **Gap Filling**: Auto-detects and re-searches undercovered areas (max 2 iterations)
- **Language**: Response matches user query language
- **Cost**: One token_usage row per agent step; no duplicates with budgets; final task cost equals sum of recorded usage (or server fallback)
- **Continuation**: Triggers only when synthesis is incomplete and capacity nearly exhausted
- **Entity Focus**: When entity detected, filters citations and prunes off-entity tool results

---

## Features Status

### ✅ Implemented

- **Native tools**: web_search, web_fetch with SSRF‑safe fetch and metadata
- **Citation pipeline**: Extract from tool outputs, normalize/dedup URLs/DOIs, score (relevance×0.7 + recency×0.3), enforce diversity (max 3/domain)
- **Entity filtering**: Canonical name detection, citation filtering (0.3 threshold, minKeep=8), off-entity result pruning
- **Coverage enforcement**: Per‑area subsections (###), each ≥600 chars and ≥2 inline citations; minimum 6 inline citations per report (floor 3)
- **Gap filling** (v1): Auto-detect undercovered areas, trigger targeted re-search (max 2 iterations), re-synthesize with combined evidence
- **Synthesis continuation**: Trigger when model stops early with incomplete output (adaptive margin, per-area validation)
- **Memory retrieval**: Hierarchical (recent + semantic, threshold 0.75) with fallback to session memory
- **Context compression**: Automatic for sessions >20 messages (rate-limited, target 37.5% of window)
- **React per task**: Mini ReAct loops per subtask for deep research (auto-enable when complexity > 0.7 + {deep|academic} strategy)
- **Verification layer**: Optional claim extraction and cross‑reference against citations via VerifyClaimsActivity
- **Language matching**: Detect from query, instruct response in same language
- **Strategy presets**: research_strategy (quick/standard/deep/academic) with overrides (max_concurrent_agents, react_max_iterations), toggles (enable_verification, react_per_task)
- **Token accounting**: Budgeted vs non‑budgeted guards across patterns; single write per step
- **Gap filling**: Automatic quality improvement for incomplete research areas (configurable per strategy)

### Gap Filling Configuration

Automatic iterative refinement for undercovered research areas:

| Strategy | Enabled | Max Gaps | Citation Check | Max Iterations |
|----------|---------|----------|----------------|----------------|
| **quick** | No | - | - | - |
| **standard** | Yes | 3 | No | 2 |
| **deep** | Yes | 5 | Yes (≥2 per section) | 2 |
| **academic** | Yes | 10 | Yes (≥2 per section) | 2 |

**How it works:**
1. After initial synthesis, analyzes each research area for coverage gaps
2. Detects gaps via: missing sections, explicit gap phrases ("insufficient data"), or low citation density
3. Triggers targeted ReAct loops to fill identified gaps
4. Re-synthesizes with combined original + gap-fill results
5. Repeats up to max iterations if gaps still exist

**Override via context:**
- `gap_filling_enabled` (bool) - Enable/disable gap filling
- `gap_filling_max_gaps` (int) - Maximum areas to refine per iteration
- `gap_filling_max_iterations` (int) - Maximum re-synthesis attempts
- `gap_filling_check_citations` (bool) - Require ≥2 inline citations per section

**Example:**
```json
{
  "query": "...",
  "research_strategy": "deep",
  "context": {
    "gap_filling_max_gaps": 8,
    "gap_filling_max_iterations": 3
  }
}
```

**Note:** Gap-fill synthesis inherits model tier from initial synthesis (defaults to medium). For higher quality gap filling, explicitly set `context.synthesis_model_tier: "large"`.

### 🔜 Future Enhancements

- Extend citation collection and per‑area checks beyond ResearchWorkflow to generic DAG/Supervisor flows (unified behavior when `force_research` is not set)
- API layer hot‑reload for research presets (gateway) beyond static validation
- Adaptive gap-filling retry when coverage remains below target after first iteration
- Citation quality feedback loop (use reflection scores to adjust credibility weights)

---

## Citations & Verification Behavior

- Citation collection: from `web_search` and `web_fetch` tool outputs with normalization (URL/DOI), dedup (keeps best scores), diversity enforcement, and combined score ranking.
- Synthesis: citation requirements are conditional; when no citations are available, the model is instructed not to fabricate sources and to label unsupported claims as "unverified".
- Verification: the orchestrator sends per‑citation snippets so the verifier can ground checks and produce meaningful confidence scores.
- Toggle (non‑research flows):
  - `context.enable_citations`: React (opt‑in), DAG (opt‑out; enabled by default). Research always manages citations internally.


## Quick Usage (HTTP)

Notes
- Per‑agent budgets are opt‑in (set `context.budget_agent_max`). With budgets on, patterns skip duplicate recording; costs remain accurate.
- For fully comprehensive reports, set `context.synthesis_style = "comprehensive"`. Research workflows set this by default.

Auth defaults
- Docker Compose: authentication is disabled by default (`GATEWAY_SKIP_AUTH=1`).
- Local builds: authentication is enabled by default (`config/features.yaml` has `gateway.skip_auth: false`). Set `GATEWAY_SKIP_AUTH=1` to disable, or provide an API key header `-H "X-API-Key: $API_KEY"`.

HTTP Gateway (REST) — if enabled
- Base URL: `http://localhost:8080` (Docker Compose default). Auth may be disabled by `GATEWAY_SKIP_AUTH=1`; otherwise set `-H "X-API-Key: $API_KEY"`.

Basic
```
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is quantum computing?",
    "context": {"force_research": true},
    "research_strategy": "quick"
  }'
```

Deep with overrides (including react_per_task)
```
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Compare LangChain and AutoGen frameworks",
    "context": {
      "force_research": true,
      "react_per_task": true,
      "react_max_iterations": 6
    },
    "research_strategy": "deep",
    "enable_verification": true
  }'
```

Academic
```
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Latest research on transformer architectures",
    "context": {"force_research": true},
    "research_strategy": "academic"
  }'
```

Minimal
```
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Explain neural networks",
    "context": {"force_research": true}
  }'
```

Stream events (SSE)
```
curl -N "http://localhost:8080/api/v1/stream/sse?workflow_id=<workflow_id>"
```

Task status
```
curl -s "http://localhost:8080/api/v1/tasks/<workflow_id>"
```

Status response notes
- Usage fields include `total_tokens`, and when available `input_tokens` and `output_tokens`. Cost may appear as `estimated_cost` in this HTTP response; workflows also compute `cost_usd` in metadata when available.
- `created_at` / `updated_at` in this endpoint reflect response generation time. The authoritative run timing and totals are persisted in the database (and visible via events/timeline APIs).

Approvals (if enabled)
```
curl -X POST http://localhost:8080/api/v1/approvals/decision \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_id": "<workflow_id>",
    "approval_id": "<approval_id>",
    "approved": true,
    "feedback": "Looks good"
  }'
```

Parameters (through gateway)
- `context.force_research` (boolean, required to route to ResearchWorkflow)
- `context.react_per_task` (boolean, enables mini ReAct loops per subtask; auto-enabled for complexity > 0.7 + deep/academic strategy)
- `context.react_max_iterations` (2–8, default 5, controls ReAct loop depth)
- `context.budget_agent_max` (int, optional per-agent token budget with enforcement)
- `research_strategy` (`quick|standard|deep|academic`)
- `max_concurrent_agents` (1–20, controls parallel task concurrency)
- `enable_verification` (boolean, enables claim verification against citations)
