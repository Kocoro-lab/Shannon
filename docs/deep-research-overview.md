# Shannon Deep Research Agent — Overview and Guide

## Executive Summary

Shannon’s Deep Research agent (v2) runs a multi‑stage workflow to produce high‑quality, cited research outputs with strong cost accounting. It combines query refinement, plan/decomposition, execution via patterns (React/Parallel/Hybrid/Sequential), citation collection, LLM‑based synthesis, optional reflection, and verification. Token usage is recorded once per step with guards to avoid duplicates, both for budgeted and non‑budgeted runs.

---

## Workflow at a Glance

Query → Refine → Decompose → Execute → Cite → Synthesize → Reflect → Verify → Result

- Refine: expand vague queries; detect entity (canonical name, exact queries, domains)
- Decompose: create subtasks + model‑aware strategy selection
- Execute: React/Parallel/Hybrid/Sequential patterns
- Cite: extract/deduplicate/score citations from tool outputs
- Synthesize: LLM synthesis with continuation logic and coverage checks
- Reflect: evaluate quality; re‑synthesize with feedback when needed
- Verify (optional): claim verification vs. citations

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

1) Refine (Step 0)
- Activity: `RefineResearchQuery`
- Output: refined_query, research_areas, canonical_name, exact_queries, official_domains
- Injects context for downstream execution and entity filtering
- Files: `go/orchestrator/internal/activities/research_refine.go`

2) Decompose (Step 1)
- Activity: `DecomposeTask`
- Output: `DecompositionResult{Subtasks[], ComplexityScore}`
- Strategy routing:
  - complexity < 0.5 → React
  - has dependencies → Hybrid
  - else → Parallel
- Files: `go/orchestrator/internal/workflows/strategies/research.go`

3) Execute (Step 2)
- Patterns and when they’re used:
  - React: iterative reason→act→observe for simpler cases
  - Parallel: concurrent subtasks
  - Hybrid: dependency‑aware fan‑in/fan‑out on top of Parallel
  - Sequential: ordered subtasks with optional result passing
- Files:
  - React: `go/orchestrator/internal/workflows/patterns/react.go`
  - Parallel/Hybrid/Sequential: `go/orchestrator/internal/workflows/patterns/execution/*.go`

4) Entity Filter (Step 2.5)
- Filters off‑entity tool results using canonical name and official domains
- Keeps reasoning‑only outputs; prunes off‑entity tool outputs
- File: `go/orchestrator/internal/workflows/strategies/research.go`

5) Cite (Step 3)
- Extract, sanitize, deduplicate, enforce diversity, score, and rank
- Output: top N citations with quality/credibility stats
- File: `go/orchestrator/internal/metadata/citations.go`

6) Synthesize (Step 4)
- Activity: `SynthesizeResultsLLM` (LLM‑first, fallback to simple)
- Prompt directives:
  - Language matching (respond in query language)
  - Mandatory research area coverage (subsection per area)
  - Output structure (comprehensive vs. concise)
  - Available citations list
- Continuation logic:
  - Trigger when finish_reason="stop" AND !looksComplete() AND remaining capacity small
  - looksComplete() requires:
    - sentence‑ending punctuation
    - no dangling conjunctions/phrases
    - every research area has a subsection (###) with ≥600 chars and ≥2 inline citations
- File: `go/orchestrator/internal/activities/synthesis.go`

7) Reflect (Step 5)
- Evaluate quality; if below threshold, re‑synthesize with feedback
- Files: `go/orchestrator/internal/workflows/patterns/reflection.go`

8) Verify (Step 6, optional)
- Verifyclaims activity validates assertions vs. citations
- Pluggable; disabled by default

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

- Coverage: each research area has a dedicated subsection (###) with ≥600 chars and ≥2 inline citations
- Language: response matches user query language
- Cost: one token_usage row per agent step; no duplicates with budgets; final task cost equals sum of recorded usage (or server fallback)
- Continuation: triggers only when synthesis is incomplete and capacity was nearly exhausted

---

## Citations Plan Status

Done
- Native tools for research (web_search, web_fetch) with SSRF‑safe fetch and metadata.
- Citation pipeline: extract from tool outputs, normalize/dedup URLs/DOIs, score (relevance×0.7 + recency×0.3), enforce diversity (max 3/domain).
- Synthesis coverage enforcement: per‑area subsections (###), each ≥600 chars and ≥2 inline citations; minimum inline citations per report (default 6, floor 3). Citations are only required when available to avoid fabrication.
- Verification layer: optional claim extraction and cross‑reference against citations via VerifyClaimsActivity and llm‑service `/api/verify_claims`.
- Language matching in synthesis: detect from query and instruct response in the same language.
- Strategy presets via gateway: research_strategy (quick/standard/deep/academic) with overrides (max_iterations, max_concurrent_agents), and toggles (enable_verification, report_mode).
- Token accounting dedup: budgeted vs non‑budgeted guards across patterns; single write per step.

Left To‑Dos
- Extend citation collection and per‑area checks beyond ResearchWorkflow to generic DAG/Supervisor flows (unified behavior when `force_research` is not set).
- Optional: API layer hot‑reload for research presets (gateway) beyond static validation.
- Future enhancement: hierarchical ReAct per parallel subtask (mini‑loops) for high‑complexity plans.

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

Deep with overrides
```
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Compare LangChain and AutoGen frameworks",
    "context": {"force_research": true},
    "research_strategy": "deep",
    "max_iterations": 12,
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
- `research_strategy` (`quick|standard|deep|academic`)
- `max_iterations` (1–50), `max_concurrent_agents` (1–20)
- `enable_verification` (boolean), `report_mode` (boolean)
