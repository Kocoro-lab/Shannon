# Shannon Cloud OpenAI-Compatible API Examples

This directory contains examples for using Shannon Cloud's OpenAI-compatible API with various SDKs and tools.

## Quick Start

1. Get your API key from Shannon Cloud
2. Set the environment variable:
   ```bash
   export SHANNON_API_KEY="sk-shannon-your-api-key"
   ```

3. Run an example:
   ```bash
   # Python
   python python_example.py

   # LangChain
   python langchain_example.py

   # cURL
   ./curl_examples.sh
   ```

## Examples

### Python (`python_example.py`)

Uses the official OpenAI Python SDK to demonstrate:
- Listing available models
- Simple chat completions
- Streaming responses
- Multi-turn conversations
- Deep research queries

**Requirements:**
```bash
pip install openai
```

### LangChain (`langchain_example.py`)

Uses LangChain with Shannon Cloud for:
- Simple invocations
- Message objects
- Prompt templates
- Streaming
- Multi-step chains

**Requirements:**
```bash
pip install langchain-openai langchain
```

### cURL (`curl_examples.sh`)

Shell script demonstrating raw API calls:
- Model listing
- Non-streaming completions
- Streaming with real-time output
- Session management
- Rate limit headers
- Shannon Events (agent thinking, progress)

**Requirements:**
```bash
# jq for JSON parsing (required for this script)
brew install jq  # macOS
apt install jq   # Ubuntu/Debian
```

## Shannon Events Extension

Shannon extends the OpenAI streaming format with agent lifecycle events in the `shannon_events` field:

```json
{
  "choices": [{"delta": {}}],
  "shannon_events": [
    {"type": "AGENT_THINKING", "agent_id": "Ryogoku", "message": "Analyzing..."}
  ]
}
```

### Handling Events in Python

```python
import httpx
import json

async def stream_with_events(message: str):
    async with httpx.AsyncClient() as client:
        async with client.stream(
            "POST",
            "http://localhost:8080/v1/chat/completions",
            headers={"Authorization": "Bearer sk_..."},
            json={"model": "shannon-deep-research", "messages": [{"role": "user", "content": message}], "stream": True}
        ) as response:
            async for line in response.aiter_lines():
                if line.startswith("data: ") and line != "data: [DONE]":
                    chunk = json.loads(line[6:])

                    # Handle content
                    if delta := chunk["choices"][0].get("delta", {}).get("content"):
                        print(delta, end="", flush=True)

                    # Handle Shannon events
                    for event in chunk.get("shannon_events", []):
                        print(f"\n[{event['type']}] {event.get('agent_id', '')}: {event.get('message', '')}")
```

### Event Types

| Type | Description |
|------|-------------|
| `WORKFLOW_STARTED` | Task begins |
| `AGENT_STARTED` | Agent activates |
| `AGENT_THINKING` | Agent reasoning |
| `PROGRESS` | Step updates |
| `TOOL_INVOKED` | Tool called |
| `AGENT_COMPLETED` | Agent done |

## Available Models

| Model | Description | Best For |
|-------|-------------|----------|
| `shannon-chat` | General chat (default) | Conversational AI |
| `shannon-quick-research` | Fast research | Quick fact-finding |
| `shannon-deep-research` | Comprehensive research | In-depth analysis |
| `shannon-ads-research` | Google Ads analysis | Marketing research |
| `shannon-analytics` | Web analytics | Data analysis |

## Configuration

Environment variables:
- `SHANNON_API_KEY` - Your API key (required)
- `SHANNON_BASE_URL` - API base URL (default: `https://api.shannon.run/v1`)

For local development:
```bash
export SHANNON_BASE_URL="http://localhost:8080/v1"
```

## Rate Limits

The API includes rate limit headers in responses:
- `X-RateLimit-Limit-Requests` - Max requests per minute
- `X-RateLimit-Remaining-Requests` - Remaining requests
- `X-RateLimit-Reset-Requests` - Reset timestamp

## Session Management

For multi-turn conversations, include the `X-Session-ID` header:
```python
response = client.chat.completions.create(
    model="shannon-chat",
    messages=[...],
    extra_headers={"X-Session-ID": "my-session-123"}
)
```

## Support

- [API Documentation](../../docs/openai-api-reference.md)
- [Architecture Plan](../../docs/openai-compatible-api-plan.md)
