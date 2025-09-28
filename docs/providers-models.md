# Providers Models Endpoint

This page documents the live model registry endpoint exposed by the Python LLM service and how to override models for specific workflow stages.

## Purpose

- Inspect which models are currently available per provider (OpenAI, Anthropic).  
- Verify dynamic discovery (OpenAI) and configured defaults.  
- Quickly debug environment issues (e.g., API keys, connectivity).  

## Endpoint

- URL: `/providers/models`
- Method: `GET`
- Optional query: `tier=small|medium|large` to filter results by logical tier.

Example:

```bash
# All providers and models
curl http://localhost:8000/providers/models | jq

# Filter by tier
curl "http://localhost:8000/providers/models?tier=small" | jq
```

Response (shape):

```json
{
  "openai": [
    {
      "id": "gpt-4o-mini",
      "name": "gpt-4o-mini",
      "tier": "small",
      "context_window": 128000,
      "cost_per_1k_prompt_tokens": 0.0,
      "cost_per_1k_completion_tokens": 0.0,
      "supports_tools": true,
      "supports_streaming": true,
      "available": true
    }
  ],
  "anthropic": [
    {
      "id": "claude-3-5-haiku-latest",
      "name": "claude-3-5-haiku-latest",
      "tier": "small",
      "context_window": 200000,
      "cost_per_1k_prompt_tokens": 0.0,
      "cost_per_1k_completion_tokens": 0.0,
      "supports_tools": true,
      "supports_streaming": true,
      "available": true
    }
  ]
}
```

Notes:
- OpenAI models include both seeded entries and dynamically discovered IDs from `models.list()` at startup (requires `OPENAI_API_KEY`).
- Anthropic models use known modern IDs (Claude 3.5 family).
- **Pricing**: All model costs are centralized in `config/models.yaml`. The LLM service loads pricing from this file for consistent cost tracking across all services.  

## Model Overrides (Per‑Stage)

Set these in the repo root `.env` to override stage‑specific models in the Python service:

```dotenv
# Provider API keys
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=...

# Stage‑specific overrides
COMPLEXITY_MODEL_ID=gpt-4o-mini
DECOMPOSITION_MODEL_ID=gpt-4o
```

- `COMPLEXITY_MODEL_ID`: used by `/complexity/analyze`  
- `DECOMPOSITION_MODEL_ID`: used by `/agent/decompose`  

If unset, the service selects models by tier.  

## Centralized Pricing Configuration

All model pricing is defined in `config/models.yaml` under the `pricing` section:

```yaml
pricing:
  models:
    openai:
      gpt-4o-mini:
        input_per_1k: 0.00015
        output_per_1k: 0.0006
```

This single source of truth is used by:
- **Go Orchestrator**: Budget management and cost tracking
- **Rust Agent Core**: Token cost calculation
- **Python LLM Service**: Provider cost reporting

See [Centralized Pricing Documentation](centralized-pricing.md) for details.

## Requirements

- Run the Python service:

```bash
cd python/llm-service
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
uvicorn main:app --reload
```

- Ensure relevant API keys are present in `.env` before starting.  

## Troubleshooting

- Empty `openai` results: likely missing/invalid `OPENAI_API_KEY`, or network egress blocked.  
- Only seed models shown: dynamic discovery failed; check logs for `OpenAI dynamic model discovery skipped`.  
- Anthropic missing: set `ANTHROPIC_API_KEY`.  

