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

- Go 1.24+ for orchestrator development
- Rust (stable) for agent core development
- Python 3.11+ for LLM service development
- Docker and Docker Compose
- protoc (Protocol Buffers compiler)

### Development Setup

1. **Fork and Clone**
   ```bash
   git clone https://github.com/your-username/shannon.git
   cd shannon
   ```

2. **Set Up Development Environment**
   ```bash
   # Install dependencies
   make setup-dev

   # Copy and configure environment
   make setup-env
   vim .env  # Add your API keys
   ```

3. **Start Services for Development**
   ```bash
   # Start all dependencies (DB, Redis, etc.)
   docker compose -f deploy/compose/compose.yml up -d postgres redis qdrant temporal

   # Run services locally for development
   # Terminal 1: Orchestrator
   cd go/orchestrator
   go run ./cmd/server

   # Terminal 2: Agent Core
   cd rust/agent-core
   cargo run

   # Terminal 3: LLM Service
   cd python/llm-service
   python -m uvicorn main:app --reload
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
# Test a specific workflow
./scripts/submit_task.sh "test query" | jq .workflow_id
make replay-export WORKFLOW_ID=<id> OUT=test.json
make replay HISTORY=test.json

# Run E2E smoke tests
make smoke
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

## 🏗️ Project Structure

Understanding the codebase:

```
shannon/
├── go/orchestrator/      # Temporal workflows and orchestration
│   ├── internal/        # Core orchestrator logic
│   ├── cmd/            # Entry points
│   └── tests/          # Go tests
├── rust/agent-core/     # WASI runtime and tool execution
│   ├── src/            # Rust source code
│   └── tests/          # Rust tests
├── python/llm-service/  # LLM providers and MCP tools
│   ├── providers/      # LLM provider implementations
│   ├── tools/          # MCP tool implementations
│   └── tests/          # Python tests
├── protos/             # Protocol buffer definitions
├── config/             # Configuration files
├── scripts/            # Utility scripts
└── docs/               # Documentation
```

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
temporal workflow describe --workflow-id <id> --address localhost:7233
```

**Database queries:**
```bash
docker compose exec postgres psql -U shannon -d shannon
```

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