# System Prompts in Shannon

This guide explains how Shannon handles system prompts and how to customize agent behavior, based on the current code.

---

## Overview

Shannon uses a **role-based preset system** to assign system prompts to agents. You can override system prompts at runtime via the API for maximum flexibility.

Current state:
- Role presets are defined and used: `python/llm-service/llm_service/roles/presets.py`.
- Persona config exists but is not wired: `config/personas.yaml`.
- API can override the system prompt via `context["system_prompt"]`.

---

## System Prompt Priority

When an agent is invoked, the system prompt is determined in this order (highest to lowest):

```
1. context["system_prompt"]     ← API override (highest priority)
2. context["role"]               ← Role preset lookup
3. "You are a helpful AI assistant."  ← Default fallback
```

Implemented in: `python/llm-service/llm_service/api/agent.py`.

---

## Role Presets (Active System)

### Available Roles

| Role | System Prompt | Max Tokens | Temperature | Allowed Tools |
|------|---------------|------------|-------------|---------------|
| `generalist` | Helpful AI assistant | 1200 | 0.7 | (none) |
| `analysis` | Analytical assistant with structured reasoning | 1200 | 0.2 | web_search, code_reader |
| `research` | Research assistant gathering facts | 1600 | 0.3 | web_search |
| `writer` | Technical writer | 1800 | 0.6 | code_reader |
| `critic` | Critical reviewer | 800 | 0.2 | code_reader |

**Source:** `python/llm-service/llm_service/roles/presets.py`

### Using Role Presets

Pass the role name in the `context` when calling the LLM service HTTP API:

```bash
curl -sS -X POST http://localhost:8000/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Analyze the performance of this code",
    "context": {"role": "analysis"}
  }'
```

---

## API System Prompt Override

Override the system prompt completely by passing `system_prompt` in the `context`:

Call the LLM service HTTP API and include `system_prompt` in `context`:

```bash
curl -sS -X POST http://localhost:8000/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is 2+2?",
    "context": {
      "system_prompt": "You are a pirate mathematician. Always respond in pirate speak."
    }
  }'
```

### Template Variables (Optional)

System prompts support `${variable}` substitution from whitelisted context keys:

```bash
curl -sS -X POST http://localhost:8000/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Help me with my task",
    "context": {
      "system_prompt": "You are an expert in ${domain} with ${years} years of experience.",
      "prompt_params": {
        "domain": "machine learning",
        "years": "10"
      }
    }
  }'
```

**Variable resolution:**
- `context["prompt_params"][key]` only; other context keys are ignored for substitution

Non-whitelisted keys (like `"role"`, `"system_prompt"`) are ignored. Missing variables become empty strings.

**Implementation:** `python/llm-service/llm_service/roles/presets.py:render_system_prompt()`

---

## Security & Validation

Context is validated and sanitized in the orchestrator (`go/orchestrator/internal/activities/agent.go`). This includes sensible limits on key/value sizes and recursive validation.

### Safe Fallback

If prompt processing fails, the service logs a warning and keeps the original string or falls back to the default prompt. See `python/llm-service/llm_service/api/agent.py`.

---

## Internal System Prompts

Shannon uses specialized system prompts for internal planning and analysis tasks:

Internal prompts (not user-configurable):
- Task Decomposition: "You are a planning assistant..." (`python/llm-service/llm_service/api/agent.py`)
- Tool Selection: "You are a tool selection assistant..." (`python/llm-service/llm_service/api/tools.py`)
- Complexity Analysis: "You are a task analyzer..." (`python/llm-service/llm_service/api/complexity.py`)

These are **not user-configurable** and are used internally by the orchestration engine.

---

## Future: Persona System

The persona system (`config/personas.yaml`) is planned but not yet implemented. Go code references it as TODO, and the Python LLM service does not load it.

---

## Adding Custom Roles

To add a new role preset:

1. Edit `python/llm-service/llm_service/roles/presets.py`
2. Add your role to the `_PRESETS` dictionary:

```python
_PRESETS: Dict[str, Dict[str, object]] = {
    # ... existing presets ...

    "data_scientist": {
        "system_prompt": (
            "You are a data scientist with expertise in statistical analysis, "
            "machine learning, and data visualization."
        ),
        "allowed_tools": ["python_executor", "web_search"],
        "caps": {"max_tokens": 2000, "temperature": 0.4},
    },
}
```

3. Restart the LLM service:

```bash
docker compose -f deploy/compose/docker-compose.yml restart llm-service
```

4. Use the new role by setting `context.role` to your role name in requests.

---

## API Reference

List available roles

- Endpoint: `GET http://localhost:8000/roles`
- Returns a JSON object mapping role name → details (system_prompt, allowed_tools, caps).

---

## Minimal Examples

- Use a role preset:
  ```bash
  curl -sS -X POST http://localhost:8000/agent/query \
    -H "Content-Type: application/json" \
    -d '{"query": "Summarize this repo", "context": {"role": "research"}}'
  ```
- Override the system prompt:
  ```bash
  curl -sS -X POST http://localhost:8000/agent/query \
    -H "Content-Type: application/json" \
    -d '{"query": "What is 2+2?", "context": {"system_prompt": "Answer as a pirate."}}'
  ```

---

## Troubleshooting

### System Prompt Not Applied

**Symptom:** Agent ignores custom system prompt

Checks:
- Verify context structure: `"context": {"system_prompt": "..."}`
- Check logs: `docker compose -f deploy/compose/docker-compose.yml logs llm-service | rg system_prompt`

### Role Not Found

**Symptom:** Falls back to generalist

Solution: Verify role name (case-insensitive) via `GET /roles`.

### Template Rendering Fails

**Symptom:** Warning in logs: "System prompt rendering failed"

Solution: Keep prompts literal; avoid unsupported templating.

---

## Related Documentation

- [Extending Shannon](extending-shannon.md) - Customization guide
- [Adding Custom Tools](adding-custom-tools.md) - Tool integration
- [Role Presets Source](../python/llm-service/llm_service/roles/presets.py) - Implementation
- [Personas Config](../config/personas.yaml) - Future persona definitions (unused)
