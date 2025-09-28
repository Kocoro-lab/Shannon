# Contributing to Shannon

First off, thank you for considering contributing to Shannon! It's people like you that make Shannon such a great tool for the AI community.

## 🎯 Ways to Contribute

There are many ways to contribute to Shannon:

- **Report Bugs** - Help us identify and fix issues
- **Suggest Features** - Share your ideas for new capabilities
- **Improve Documentation** - Help make our docs clearer and more comprehensive
- **Submit Code** - Fix bugs or add new features
- **Answer Questions** - Help other users in discussions
- **Write Tutorials** - Share your Shannon use cases and patterns

## 🚀 Getting Started

### Prerequisites

- Go 1.22+ for orchestrator development
- Rust (stable) for agent core development
- Python 3.11+ for LLM service development
- Docker and Docker Compose
- Make, curl, grpcurl
- protoc (Protocol Buffers compiler)
- Node.js 18+ and npm (for dashboard development)
- An API key for at least one supported LLM provider

### Quick Reference - Service Ports

| Service | Port | Description |
|---------|------|-------------|
| Agent Core (Rust) | 50051 | gRPC service for agent operations |
| Orchestrator (Go) | 50052 | gRPC service for workflow orchestration |
| LLM Service (Python) | 8000 | HTTP service for LLM providers |
| Gateway | 8080 | REST API gateway |
| Dashboard | 3000 | Real-time observability UI |
| PostgreSQL | 5432 | Primary database |
| Redis | 6379 | Session cache and pub/sub |
| Qdrant | 6333 | Vector database |
| Temporal | 7233 | Workflow engine (UI on 8088) |
| Prometheus | 9090 | Metrics collection |
| Grafana | 3001 | Metrics visualization |

### Development Setup

1. **Fork and Clone**
   ```bash
   git clone https://github.com/your-username/shannon.git
   cd shannon
   ```

2. **Set Up Development Environment**
   ```bash
   # One-stop setup: creates .env, generates protobuf files
   make setup

   # Add your LLM API key to .env
   echo "OPENAI_API_KEY=your-key-here" >> .env
   # Or edit manually: vim .env

   # Download Python WASI interpreter for secure code execution (20MB)
   ./scripts/setup_python_wasi.sh
   ```

3. **Start Services for Development**
   ```bash
   # Option 1: Start all services with Docker (recommended)
   make dev
   make smoke  # Verify everything is working

   # Option 2: Run services locally for development
   # Start dependencies first
   docker compose -f deploy/compose/docker-compose.yml up -d postgres redis qdrant temporal

   # Terminal 1: Orchestrator (Go service on port 50052)
   cd go/orchestrator
   go run ./cmd/server

   # Terminal 2: Agent Core (Rust service on port 50051)
   cd rust/agent-core
   cargo run

   # Terminal 3: LLM Service (Python service on port 8000)
   cd python/llm-service
   python -m uvicorn main:app --reload

   # Terminal 4: Gateway (REST API on port 8080)
   cd go/gateway
   go run ./cmd/server

   # Terminal 5: Dashboard (UI on port 3000)
   cd dashboard
   npm install && npm run dev
   ```

## 🔨 Development Workflow

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-description
```

### 2. Make Your Changes

#### Code Style Guidelines

**Go (Orchestrator)**
- Follow standard Go formatting (`gofmt`)
- Use meaningful variable names
- Add comments for exported functions
- Run `go mod tidy` after adding dependencies

**Rust (Agent Core)**
- Follow Rust formatting (`cargo fmt`)
- Use `clippy` for linting (`cargo clippy`)
- Prefer `Result` over panics
- Document public APIs

**Python (LLM Service)**
- Follow PEP 8 style guide
- Use type hints
- Format with `black`
- Sort imports with `isort`

**Protocol Buffers**
- After modifying `.proto` files:
  ```bash
  make proto
  docker compose build  # Rebuild services if running in Docker
  docker compose up -d  # Restart with new proto definitions
  ```

### 3. Write Tests

All code changes should include tests:

```bash
# Go tests
cd go/orchestrator
go test -race ./...

# Rust tests
cd rust/agent-core
cargo test

# Python tests
cd python/llm-service
python -m pytest
```

### 4. Run CI Checks Locally

Before submitting, ensure all checks pass:

```bash
# Run all CI checks
make ci

# Individual checks
make lint
make test
make build
```

### 5. Commit Your Changes

Write clear, descriptive commit messages:

```bash
git add .
git commit -m "feat: add new pattern for recursive agents

- Implement RecursivePattern in orchestrator
- Add tests for edge cases
- Update documentation"
```

Commit message format:
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `style:` Code style changes
- `refactor:` Code refactoring
- `test:` Test additions/changes
- `chore:` Maintenance tasks

### 6. Push and Create Pull Request

```bash
git push origin feature/your-feature-name
```

Then open a Pull Request on GitHub with:
- Clear title and description
- Link to any related issues
- Screenshots/logs if applicable
- Test results

## 📋 Pull Request Checklist

- [ ] Code follows the project's style guidelines
- [ ] Self-review completed
- [ ] Tests added/updated and passing
- [ ] Documentation updated if needed
- [ ] `make ci` passes locally
- [ ] No new warnings or errors
- [ ] Commits are logical and atomic
- [ ] PR description explains the changes

## 🧪 Testing Guidelines

### Unit Tests
- Test individual functions and methods
- Mock external dependencies
- Aim for >80% code coverage

### Integration Tests
- Test component interactions
- Use test containers for dependencies
- Cover critical paths

### E2E Tests
- Test complete workflows
- Verify system behavior
- Test error scenarios

### Running Specific Tests

```bash
# Run E2E smoke tests
make smoke

# Test a specific workflow (time-travel debugging)
./scripts/submit_task.sh "test query"
# Note the workflow_id from output, then:
make replay-export WORKFLOW_ID=task-dev-1234567890 OUT=test.json
make replay HISTORY=test.json

# Run integration tests
export RUN_INTEGRATION_TESTS=true
go test -tags integration ./internal/activities

# Test with specific model config
export MODELS_CONFIG_PATH=$PWD/config/models.yaml
go test ./internal/pricing -v
```

## 🐛 Reporting Issues

### Before Submitting an Issue

1. Check existing issues to avoid duplicates
2. Try the latest version
3. Collect relevant information:
   - Shannon version
   - OS and environment
   - Error messages and logs
   - Steps to reproduce

### Issue Template

```markdown
**Description**
Clear description of the issue

**Steps to Reproduce**
1. Run command X
2. See error Y

**Expected Behavior**
What should happen

**Actual Behavior**
What actually happens

**Environment**
- Shannon version:
- OS:
- Go/Rust/Python version:

**Logs**
```
Relevant log output
```
```

## 🏗️ Architecture Overview

### Key Components

1. **Orchestrator (Go)** - Brain of the system
   - Temporal workflow orchestration
   - Complexity analysis and task decomposition
   - Budget management and token tracking
   - Session and memory management (character-based chunking with MMR diversity)
   - Centralized pricing from `config/models.yaml`

2. **Agent Core (Rust)** - Secure execution layer
   - WASI sandbox for Python code execution
   - Tool execution and caching
   - gRPC service for agent operations
   - Circuit breakers and rate limiting

3. **LLM Service (Python)** - AI provider interface
   - Multi-provider support (OpenAI, Anthropic, Google, etc.)
   - MCP tool implementations
   - Prompt management and optimization

4. **Gateway (Go)** - REST API layer
   - HTTP/REST interface for clients
   - Authentication and authorization
   - Request routing and load balancing

5. **Dashboard (React)** - Real-time UI
   - Task submission and monitoring
   - Event streaming visualization
   - Metrics and system health

### Project Structure

Understanding the codebase:

```
shannon/
├── go/orchestrator/      # Temporal workflows and orchestration (port 50052)
│   ├── internal/        # Core orchestrator logic
│   │   ├── workflows/  # Workflow patterns (DAG, supervisor, streaming)
│   │   ├── activities/ # Memory, budget, complexity analysis
│   │   └── pricing/    # Model pricing from config/models.yaml
│   └── cmd/            # Entry points
├── rust/agent-core/     # WASI runtime and tool execution (port 50051)
│   ├── src/            # Rust source code
│   │   ├── wasi_sandbox.rs  # Secure Python execution
│   │   └── tools.rs         # Built-in tool implementations
│   └── tests/          # Rust tests
├── python/llm-service/  # LLM providers and MCP tools (port 8000)
│   ├── providers/      # LLM provider implementations
│   ├── tools/          # MCP tool implementations
│   └── tests/          # Python tests
├── go/gateway/         # REST API gateway (port 8080)
├── dashboard/          # Real-time UI (port 2111)
├── protos/             # Protocol buffer definitions
├── config/             # Configuration files
│   ├── models.yaml     # Centralized model pricing
│   └── shannon.yaml    # System configuration
├── scripts/            # Utility scripts
└── docs/               # Documentation
```

## ⚙️ Important Configuration

### Model Pricing
All model pricing is centralized in `config/models.yaml`. When adding new models:
1. Add pricing under the `pricing.models` section
2. Specify `input_per_1k` and `output_per_1k` in USD
3. Update tier assignments in `model_tiers` if needed

### Memory System
- Character-based chunking (4 chars ≈ 1 token)
- MMR (Maximal Marginal Relevance) for diversity
- Configurable thresholds in `config/shannon.yaml`

### Reflection Gating
- Uses configurable complexity thresholds
- Default: >0.5 triggers reflection
- Configured via `ComplexityMediumThreshold`

## 🔧 Debugging Tips

### Enable Debug Logging

```bash
# Set log levels
export RUST_LOG=debug
export LOG_LEVEL=debug

# View service logs
docker compose logs -f orchestrator
docker compose logs -f agent-core
docker compose logs -f llm-service
```

### Common Issues

**Proto changes not reflected:**
```bash
make proto
docker compose build
docker compose up -d
```

**Temporal workflow issues:**
```bash
# Check workflow status
temporal workflow describe --workflow-id <id> --address localhost:7233

# Or via Docker
docker compose exec temporal temporal workflow describe --workflow-id <id> --address temporal:7233

# View Temporal UI
open http://localhost:8088
```

**Database queries:**
```bash
# Connect to database
docker compose exec postgres psql -U shannon -d shannon

# Common queries
SELECT workflow_id, status, created_at FROM task_executions ORDER BY created_at DESC LIMIT 5;
SELECT id, user_id, created_at FROM sessions WHERE id = 'session-id';

# Check Redis session data
redis-cli GET session:SESSION_ID | jq '.total_tokens_used'
```

## 🛠️ Common Development Tasks

### Adding a New LLM Provider
1. Implement provider in `python/llm-service/providers/`
2. Add pricing to `config/models.yaml`
3. Update tier assignments if needed
4. Add tests for the provider

### Adding a New Tool
1. Define tool in `python/llm-service/tools/`
2. Register with MCP if external
3. Add tool description for LLM
4. Include `print()` statements for Python tools (WASI requirement)
5. Write integration tests

### Adding a New Workflow Pattern
1. Create pattern in `go/orchestrator/internal/workflows/strategies/`
2. Register in workflow router
3. Add complexity analysis logic
4. Test with replay functionality

### Updating Protocol Buffers
1. Modify `.proto` files in `protos/`
2. Run `make proto` to regenerate
3. Update all three services if interfaces changed
4. Rebuild and test: `docker compose build && docker compose up -d`

### Performance Optimization
1. Check metrics at `http://localhost:9090` (Prometheus)
2. Review dashboard at `http://localhost:2111` (Shannon Dashboard)
3. Use `make replay` for deterministic testing
4. Profile with service-specific tools

## 💬 Communication

- **GitHub Issues**: Bug reports and feature requests
- **GitHub Discussions**: General questions and ideas
- **Discord**: Real-time chat (coming soon)
- **Pull Requests**: Code contributions

## 🎓 Learning Resources

- [Architecture Overview](docs/multi-agent-workflow-architecture.md)
- [Pattern Guide](docs/pattern-usage-guide.md)
- [API Documentation](docs/agent-core-api.md)
- [Testing Guide](docs/testing.md)
- [Python Code Execution Guide](docs/python-code-execution.md)
- [Streaming API Guide](docs/streaming-api.md)

## 📜 Code of Conduct

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms.

## 🙏 Recognition

Contributors are recognized in:
- The README.md contributors section
- Release notes
- Our website (coming soon)

## Questions?

Feel free to open an issue with the `question` label or start a discussion!

---

Thank you for contributing to Shannon! Together we're building the future of AI agents. 🚀