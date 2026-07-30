# Shannon LLM Service (Python)

The LLM Service is Shannon's AI provider gateway, managing LLM interactions, tool execution, and MCP integration with support for multiple providers and intelligent tool selection.

## ⚠️ Important Setup Note

**Before building or running the service**, you must generate the protobuf files:

```bash
# From repository root
./scripts/generate_protos_local.sh
```

This creates the `python/llm-service/llm_service/grpc_gen` directory with protobuf v5-compatible files required for the Python WASI executor and other gRPC communication.

## 🎯 Core Responsibilities

- **Multi-Provider LLM Gateway** - Unified interface for OpenAI and Anthropic (additional providers available via the internal LLM manager library)
- **Tool Management** - MCP tool registration, validation, and execution
- **Intelligent Tool Selection** - Automatic tool selection based on task requirements
- **Web Search Integration** - Multiple search providers (Exa, Perplexity, Brave, DuckDuckGo)
- **Embeddings & Chunking** - Document processing for vector storage
- **Cost Tracking** - Token usage and cost calculation per provider

## 🏗️ Architecture

```
HTTP API (:8000)
    ↓
FastAPI Application
    ├── LLM Router → Provider Selection
    │   ├── OpenAI (GPT‑5 family)
    │   ├── Anthropic (Claude 4)
    │   └── (Additional providers via library)
    ├── Tools Router → MCP Integration
    │   ├── Tool Registry
    │   ├── Tool Validation
    │   └── Tool Execution
    └── Search Router → Web Search
        ├── Exa Search
        ├── Perplexity
        ├── Brave Search
        └── DuckDuckGo
```

## 📁 Project Structure

```
python/llm-service/
├── main.py                      # FastAPI application entry
├── Dockerfile                   # Container configuration
├── requirements.txt             # Python dependencies
├── llm_service/
│   ├── api/                    # API endpoints
│   │   ├── llm.py              # LLM completion endpoints
│   │   ├── tools.py            # Tool management endpoints
│   │   └── health.py           # Health check endpoints
│   ├── core/                   # Core functionality
│   │   ├── config.py           # Configuration management
│   │   ├── models.py           # Pydantic models
│   │   └── metrics.py          # Prometheus metrics
│   ├── tools/                  # Tool system
│   │   ├── manager.py          # Tool registry & execution
│   │   ├── selector.py         # Intelligent tool selection
│   │   ├── mcp_tools.py        # MCP tool integration
│   │   └── builtin/            # Built-in tools
│   └── web_search/             # Search providers
│       ├── manager.py          # Search orchestration
│       └── providers/          # Individual providers
├── llm_provider/               # LLM provider implementations
│   ├── base.py                # Abstract provider interface
│   ├── openai_provider.py     # OpenAI implementation
│   ├── anthropic_provider.py  # Anthropic implementation
│   ├── google_provider.py     # Google Gemini
│   └── groq_provider.py       # Groq implementation
├── integrations/               # External integrations
│   └── mcp/                   # MCP protocol implementation
└── tests/                      # Test suite
    ├── test_providers.py       # Provider tests
    ├── test_tools.py           # Tool system tests
    └── test_web_search.py      # Search tests
```

## 🚀 Quick Start

### Prerequisites
- Python 3.11+
- At least one LLM API key (OpenAI, Anthropic, etc.)
- Docker (for containerized deployment)

### Development Setup

```bash
# Create virtual environment
python3 -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate

# Install dependencies
pip install -r requirements.txt

# Set environment variables
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="..."

# Run locally
python main.py

# Service will be available at http://localhost:8000
```

### Docker Deployment

```bash
# Build image
docker build -t shannon-llm-service .

# Run with compose (recommended)
make dev  # From repository root

# Or run standalone
docker run -p 8000:8000 \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  shannon-llm-service
```

## 🔌 API Endpoints

### LLM Completion
```bash
# Generate completion
curl -X POST http://localhost:8000/llm/completion \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "openai",
    "model": "gpt-5-2025-08-07",
    "messages": [{"role": "user", "content": "Hello"}],
    "temperature": 0.7
  }'
```

### Tool Management
```bash
# List available tools
curl http://localhost:8000/tools/list

# Execute a tool
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "calculator",
    "parameters": {"expression": "2+2"}
  }'

# Auto-select tools for task
curl -X POST http://localhost:8000/tools/select \
  -H "Content-Type: application/json" \
  -d '{
    "task": "Get weather in Beijing and convert temperature",
    "max_tools": 3
  }'
```

### Web Search
```bash
# Search the web
curl -X POST http://localhost:8000/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "latest AI developments",
    "provider": "exa",
    "max_results": 5
  }'
```

### Health & Metrics
```bash
# Health check
curl http://localhost:8000/health

# Prometheus metrics
curl http://localhost:8000/metrics
```

## ⚙️ Configuration

### Environment Variables

```bash
# LLM Providers (at least one required)
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=...

# Search Providers (optional)
EXA_API_KEY=...
PERPLEXITY_API_KEY=...
BRAVE_API_KEY=...

# Tool Configuration
ENABLE_TOOL_SELECTION=1      # Auto-select tools
TOOL_PARALLELISM=4           # Parallel tool execution
MCP_REGISTER_TOKEN=...       # MCP registration auth

# Service Configuration
PORT=8000
LOG_LEVEL=INFO
METRICS_ENABLED=true
```

### Optional Xquik Search

`xquik_search` reads public X posts through Xquik. Each call is a paid read.
Review current Xquik pricing before enabling it. Set a small `limit` for each
search. The API can return fewer results than requested.

The tool is disabled by default. Enable it only for operators who accept paid
reads:

```bash
XQUIK_SEARCH_ENABLED=true
XQUIK_API_KEY=xq_...
XQUIK_SEARCH_RATE_LIMIT=30
XQUIK_SEARCH_TIMEOUT=30
```

Restart the LLM service after changing these values. The tool is marked
dangerous. Default lists, schemas, and automatic selection exclude it. Add
`xquik_search` to an agent's explicit `allowed_tools` list or call it directly.
It never creates a checkout or top-up after a `402` response.

Xquik is an independent third-party service. Not affiliated with X Corp.
"Twitter" and "X" are trademarks of X Corp.

### Provider Models

Each provider supports different models. See [providers-models.md](../../docs/providers-models.md) for the complete list.

Common models:
- **OpenAI**: gpt-5-2025-08-07, gpt-5-pro-2025-10-06, gpt-5-nano-2025-08-07, gpt-5-mini-2025-08-07
- **Anthropic**: claude-opus-4-1-20250805, claude-sonnet-4-5-20250929, claude-haiku-4-5-20251001

## 🧪 Testing

### Unit Tests
```bash
# Run all tests
pytest

# Run with coverage
pytest --cov=. --cov-report=html

# Run specific test file
pytest tests/test_providers.py
```

### Integration Tests
```bash
# Test LLM providers
python -m pytest tests/test_providers.py -k "test_openai"

# Test tool system
python -m pytest tests/test_tools.py

# Test web search
python -m pytest tests/test_web_search.py
```

### Manual Testing
```bash
# Test tool execution
curl -X POST http://localhost:8000/tools/execute \
  -d '{"tool_name": "calculator", "parameters": {"expression": "2+2"}}'

# Test LLM completion
curl -X POST http://localhost:8000/llm/completion \
  -d '{"provider": "openai", "model": "gpt-5-2025-08-07", "messages": [{"role": "user", "content": "Hi"}]}'
```

## 🔧 Key Features

### Multi-Provider Support

The service abstracts provider differences:
- Unified API across all providers
- Automatic retry and fallback
- Provider-specific optimizations
- Cost tracking per provider

### MCP Tool Integration

Model Context Protocol support:
- Dynamic tool registration
- Tool validation and sandboxing
- Parallel tool execution
- Tool cost tracking

### Intelligent Tool Selection

Automatic tool selection based on:
- Task analysis
- Tool capabilities matching
- Cost optimization
- Execution time estimates

### Web Search Integration

Multiple search providers with:
- Result deduplication
- Source credibility scoring
- Content extraction
- Caching for efficiency

## 📊 Observability

### Metrics
- **Endpoint**: `:8000/metrics` (Prometheus format)
- LLM token usage and costs
- Tool execution counts and latency
- Provider availability and errors
- Search query performance

### Logging
- Structured JSON logging
- Request/response tracing
- Error tracking with context
- Performance profiling

### Health Checks
- `/health` - Basic health status
- `/health/ready` - Readiness probe
- `/health/live` - Liveness probe

## 🚨 Common Issues

### Provider Authentication
- Ensure API keys are set in environment
- Check key format and validity
- Verify provider-specific requirements

### Tool Registration
- MCP tools require valid endpoints
- Check tool parameter schemas
- Verify MCP_REGISTER_TOKEN for dynamic registration

### Memory Usage
- Large context windows can consume memory
- Use streaming for long responses
- Monitor embedding batch sizes

## 📚 Further Documentation

- [MCP Integration Guide](../../docs/mcp-integration.md)
- [Provider Models Reference](../../docs/providers-models.md)
- [Web Search Configuration](../../docs/web-search-configuration.md)
- [Main README](../../README.md)
