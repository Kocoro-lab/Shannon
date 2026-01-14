# Shannon Default Timeout Configuration

Reference values aligned with the current codebase. Defaults noted come from code; sample `.env` and docker‑compose overrides are called out where relevant.

## Agent & Task Execution

| Configuration   | Default (code)   | Env Var                 | Location                                            | Description                                                       |
| --------------- | ---------------- | ----------------------- | --------------------------------------------------- | ----------------------------------------------------------------- |
| Agent Execution | 30s              | AGENT_TIMEOUT_SECONDS   | go/orchestrator/internal/activities/agent.go        | Max runtime per agent execution (set via env; compose defaults 600s) |
| Enforce Timeout | 30s              | ENFORCE_TIMEOUT_SECONDS | rust/agent-core/src/config.rs                       | Agent-core per-request enforcement timeout (sample .env sets 120s) |
| WASI Timeout    | 30s              | WASI_TIMEOUT_SECONDS    | rust/agent-core/src/config.rs                       | Agent-core WASI execution timeout (sample .env sets 60s)          |

## Orchestrator Activities

| Activity             | Default      | Env Var                   | Location                                         | Description                                              |
| -------------------- | ------------ | ------------------------- | ------------------------------------------------ | -------------------------------------------------------- |
| ResearchWorkflow Activities | 480s (8 min) | N/A | go/orchestrator/internal/workflows/strategies/research.go | Default Activity timeout for research workflow (SynthesizeResultsLLM can take 5+ min for deep research) |
| DecomposeTask        | 30s          | DECOMPOSE_TIMEOUT_SECONDS | go/orchestrator/internal/activities/decompose.go | HTTP timeout for task decomposition                      |
| Synthesis (Standard) | 180s (3 min) | N/A                       | go/orchestrator/internal/activities/synthesis.go | Non-research synthesis timeout                           |
| Synthesis (Research) | 300s (5 min) | N/A                       | go/orchestrator/internal/activities/synthesis.go | Research synthesis timeout                               |
| Research Refinement  | 300s (5 min) | N/A                       | go/orchestrator/internal/activities/research_refine.go | Research query refinement timeout                  |
| Session Title        | 15s          | N/A                       | go/orchestrator/internal/activities/session_title.go   | Generate session title timeout                       |
| Verify Activity      | 120s (2 min) | N/A                       | go/orchestrator/internal/activities/verify.go    | Verification HTTP client timeout                         |
| Context Compression  | 8s           | N/A                       | go/orchestrator/internal/activities/context_compress.go | Context compression HTTP timeout                    |

## HTTP Client Timeouts (Internal)

| Client                    | Default             | Location                                    | Purpose                                                     |
| ------------------------- | ------------------- | ------------------------------------------- | ----------------------------------------------------------- |
| Tool Metadata Fetch       | 2s                  | go/orchestrator/internal/activities/agent.go | Best-effort tool metadata HTTP fetch                        |
| Tools List                | 5s                  | go/orchestrator/internal/activities/agent.go | LLM service: list available tools                           |
| Tools Select              | 5s                  | go/orchestrator/internal/activities/agent.go | LLM service: tool selection                                 |
| Agent gRPC                | Agent timeout + 30s | go/orchestrator/internal/activities/agent.go | gRPC call with buffer (e.g., 630s if agent timeout is 600s) |
| Agent Query (forced tool) | 2 min (120s)        | go/orchestrator/internal/activities/agent.go | HTTP client for /agent/query (forced tool path)             |
| Dial Context              | 3s                  | go/orchestrator/internal/activities/agent.go | gRPC connection establishment                               |

## LLM Provider Timeouts (config/models.yaml)

| Provider   | Default | Location          | Notes                                            |
| ---------- | ------- | ----------------- | ------------------------------------------------ |
| OpenAI     | 300s    | config/models.yaml | Long timeout for potentially slow responses      |
| Anthropic  | 300s    | config/models.yaml | Increased from 60s in current config             |
| Google     | 300s    | config/models.yaml | Increased from 60s in current config             |
| Bedrock    | 90s     | config/models.yaml | AWS Bedrock                                      |
| Ollama     | 120s    | config/models.yaml | Local model serving                              |
| Meta/Llama | 90s     | config/models.yaml | Via Together AI                                  |
| DeepSeek   | 60s     | config/models.yaml |                                                  |
| Qwen       | 60s     | config/models.yaml |                                                  |
| XAI        | 60s     | config/models.yaml | Grok models                                      |
| ZAI        | 60s     | config/models.yaml | Custom provider                                  |
| Mistral    | 60s     | config/models.yaml |                                                  |
| Cohere     | 60s     | config/models.yaml |                                                  |

Default provider fallback: 60s (python/llm-service/llm_provider/base.py) if not specified.

## Workflow Configuration (via config/features.yaml)

| Setting            | Default      | Location                                          | Description                     |
| ------------------ | ------------ | ------------------------------------------------- | ------------------------------- |
| Reflection Timeout | 5000ms (5s)  | go/orchestrator/internal/activities/config.go     | Reflection activity timeout     |
| Hybrid Dependency  | 360s (6 min) | go/orchestrator/internal/activities/config.go     | Hybrid pattern dependency wait  |
| P2P Coordination   | 360s (6 min) | go/orchestrator/internal/activities/config.go     | Peer-to-peer agent coordination |
| LLM Timeout        | 30s          | rust/agent-core/src/config.rs (.env example 120s) | Agent-core LLM request timeout  |

## Security & Integration

| Configuration       | Default         | Env Var                     | Location                                      | Description                          |
| ------------------- | --------------- | --------------------------- | --------------------------------------------- | ------------------------------------ |
| Approval Timeout    | 1800s (30 min)  | N/A (per-request)           | go/orchestrator/internal/server/service.go    | Human approval wait; override via API |
| OpenAPI Fetch       | 30s             | OPENAPI_FETCH_TIMEOUT       | python/llm-service/llm_service/tools/openapi_tool.py | Fetching OpenAPI specs               |
| MCP Timeout         | 10s             | MCP_TIMEOUT_SECONDS         | python/llm-service/llm_service/mcp_client.py  | MCP tool server calls                |
| Python WASI Session | 3600s (1 hour)  | PYTHON_WASI_SESSION_TIMEOUT | python/llm-service/llm_service/tools/builtin/python_wasi_executor.py | WASI interpreter session lifetime    |

## Circuit Breaker & Resilience

| Setting           | Default | Location                                               | Description                                  |
| ----------------- | ------- | ------------------------------------------------------ | -------------------------------------------- |
| Circuit Reset     | 30s     | go/orchestrator/internal/activities/circuit_breaker.go | Time before attempting to close open circuit |
| Provider Recovery | 60s     | python/llm-service/llm_provider/manager.py            | Provider circuit breaker recovery window     |

## Key Insights

1. Longest defaults: Provider timeouts (OpenAI/Anthropic/Google) at 300s; approval wait at 1800s by default.
2. Shortest defaults: Tool metadata fetch (2s), Dial context (3s), Reflection (5s).
3. Notes:
   - Agent execution default in code is 30s; compose sets 600s by default via env.
   - DecomposeTask default is 30s (override via DECOMPOSE_TIMEOUT_SECONDS).
4. Provider variance: Several major providers (OpenAI, Anthropic, Google) use 300s; most others use 60–120s.

## Recommendations

- Slow LLM APIs: prefer providers configured at 300s where appropriate (OpenAI/Anthropic/Google).
- Complex workflows: set `AGENT_TIMEOUT_SECONDS` to ~600s.
- Production: monitor DecomposeTask latency; keep default 30s unless evidence supports increase.
- Local models (Ollama): 120s default is typically sufficient.

