# Shannon Examples

This directory contains practical examples demonstrating Shannon's capabilities.

## Getting Started

### Prerequisites

```bash
# 1. Install Shannon Python client
cd clients/python
pip install -e .

# 2. Start Shannon services
cd ../..
make setup
make dev

# 3. Wait for services to be ready
make smoke
```

### Environment Variables

Create a `.env` file with your API keys:

```bash
OPENAI_API_KEY=sk-...
# Or other providers
ANTHROPIC_API_KEY=...
GOOGLE_API_KEY=...
```

## Examples

### 1. Quick Start (`quick_start.py`)

Comprehensive examples covering all basic Shannon features:

```bash
python examples/quick_start.py
```

**What it demonstrates:**
- ✅ Simple task submission
- ✅ Streaming task progress
- ✅ Multi-turn conversations with memory
- ✅ Template-based workflows
- ✅ Tool usage (web search, etc.)
- ✅ Multi-agent DAG workflows

### 2. Advanced Patterns (`advanced_patterns.py`)

Advanced AI patterns and strategies:

```bash
python examples/advanced_patterns.py
```

**Patterns covered:**
- Chain-of-Thought reasoning
- Tree-of-Thoughts exploration
- Debate and consensus
- Reflection and self-improvement
- ReAct (Reasoning + Acting)

### 3. Custom Tools (`custom_tools_demo.py`)

How to add and use custom tools:

```bash
python examples/custom_tools_demo.py
```

**Topics:**
- MCP tool integration
- OpenAPI REST API tools
- Python native tools
- Tool composition

### 4. Multi-Agent Coordination (`multi_agent_demo.py`)

Complex multi-agent orchestration:

```bash
python examples/multi_agent_demo.py
```

**Features:**
- DAG workflows with dependencies
- Parallel agent execution
- Supervisor-worker hierarchies
- Agent-to-agent communication

### 5. Memory and Context (`memory_demo.py`)

Working with session memory and context:

```bash
python examples/memory_demo.py
```

**Covers:**
- Session memory management
- Vector memory (Qdrant)
- Context window optimization
- Memory retrieval strategies

### 6. Production Patterns (`production_demo.py`)

Production-ready patterns and best practices:

```bash
python examples/production_demo.py
```

**Topics:**
- Error handling and retries
- Budget management
- Rate limiting
- Monitoring and metrics
- Workflow replay/debugging

## Example Templates

### Simple Analysis Template

```yaml
# config/workflows/examples/simple_analysis.yaml
name: simple_analysis
version: "1.0.0"

defaults:
  model_tier: medium
  budget_agent_max: 5000
  require_approval: false

nodes:
  - id: analyze
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    budget_max: 500
    depends_on: []
```

Usage:

```python
task = await client.submit_task(
    query="Analyze recent AI developments",
    template_name="simple_analysis",
    template_version="1.0.0"
)
```

### Research Pipeline Template

```yaml
# config/workflows/examples/research_pipeline.yaml
name: research_pipeline
version: "1.0.0"

nodes:
  - id: research
    type: simple
    strategy: react
    tools_allowlist: ["web_search"]
    depends_on: []

  - id: analyze
    type: cognitive
    strategy: chain_of_thought
    depends_on: [research]

  - id: report
    type: simple
    strategy: reflection
    depends_on: [analyze]
```

## Common Use Cases

### 1. Web Research

```python
task = await client.submit_task(
    query="Research the latest developments in quantum computing",
    context={
        "allowed_tools": ["web_search"],
        "cognitive_strategy": "react"
    }
)
```

### 2. Code Generation

```python
task = await client.submit_task(
    query="Write a Python function to calculate fibonacci numbers",
    context={
        "allowed_tools": ["python_execute"],
        "cognitive_strategy": "chain_of_thought"
    }
)
```

### 3. Data Analysis

```python
task = await client.submit_task(
    query="Analyze the sales data and provide insights",
    context={
        "allowed_tools": ["file_read", "python_execute"],
        "data_path": "/data/sales.csv"
    }
)
```

### 4. Multi-Turn Conversation

```python
session_id = "user-session-123"

# Turn 1
task1 = await client.submit_task(
    query="What is machine learning?",
    session_id=session_id
)

# Turn 2 - remembers context
task2 = await client.submit_task(
    query="Give me an example of it",
    session_id=session_id
)
```

### 5. Parallel Task Execution

```python
task = await client.submit_task(
    query="""
    Perform these tasks in parallel:
    1. Summarize recent tech news
    2. Check weather forecast
    3. Get stock market summary
    """,
    context={"decompose": True}
)
```

## Testing Examples

### Run with Simulation

```bash
# Test without real API calls
export SKIP_LLM_CALLS=true
python examples/quick_start.py
```

### Run Specific Example

```python
# In quick_start.py, comment out unwanted examples
if __name__ == "__main__":
    # Run only example 1
    asyncio.run(example_1_simple_task())
```

## Troubleshooting

### Connection Refused

```bash
# Check if services are running
docker-compose -f deploy/compose/docker-compose.yml ps

# Restart services
make dev
```

### Authentication Error

```bash
# Verify API key is set
echo $OPENAI_API_KEY

# Or use test key
export SHANNON_API_KEY=sk_test_123456
```

### Timeout Errors

```python
# Increase timeout
result = await task.get_result(timeout=120.0)

# Or stream instead
async for event in task.stream_events():
    # Handle events
    pass
```

## Best Practices

### 1. Always Use Try-Finally for Client Cleanup

```python
client = AsyncShannonClient(...)
try:
    task = await client.submit_task(...)
    result = await task.get_result()
finally:
    await client.close()
```

### 2. Use Context Managers

```python
async with AsyncShannonClient(...) as client:
    task = await client.submit_task(...)
    result = await task.get_result()
# Auto-closes
```

### 3. Handle Errors Gracefully

```python
try:
    result = await task.get_result(timeout=30.0)
except TimeoutError:
    print("Task timed out")
except Exception as e:
    print(f"Error: {e}")
```

### 4. Monitor Token Usage

```python
result = await task.get_result()
if result.cost:
    print(f"Cost: ${result.cost:.4f}")
    print(f"Tokens: {result.tokens_used}")
```

### 5. Use Templates for Common Patterns

Instead of:
```python
task = await client.submit_task(query=long_query_with_instructions)
```

Use:
```python
task = await client.submit_task(
    query=short_query,
    template_name="research_pipeline"
)
```

## Contributing Examples

Want to add a new example? Follow these steps:

1. Create your example script in `examples/`
2. Add documentation to this README
3. Test it thoroughly
4. Submit a PR

Example template:

```python
#!/usr/bin/env python3
"""
Your Example Title

Description of what this example demonstrates.

Usage:
    python examples/your_example.py
"""

import asyncio
from shannon.client import AsyncShannonClient

async def main():
    client = AsyncShannonClient(grpc_endpoint="localhost:50052")
    try:
        # Your example code
        task = await client.submit_task(...)
        result = await task.get_result()
        print(result.output)
    finally:
        await client.close()

if __name__ == "__main__":
    asyncio.run(main())
```

## Next Steps

- Read the [User Guide](../docs/template-user-guide.md)
- Explore [Multi-Agent Workflows](../docs/multi-agent-workflow-architecture.md)
- Learn about [Custom Tools](../docs/adding-custom-tools.md)
- Check out [Testing Guide](../docs/testing.md)

---

Need help? Join our [Discord](https://discord.gg/shannon) or open an [issue](https://github.com/Kocoro-lab/Shannon/issues)!

