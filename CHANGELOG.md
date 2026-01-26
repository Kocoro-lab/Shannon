# Changelog

All notable changes to Shannon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### Skills System
- **Markdown-based Skills**: Define workflows with YAML frontmatter and markdown instructions
- **Skills API**: `GET /api/v1/skills`, `GET /api/v1/skills/{name}` endpoints
- **Skill Categories**: Organize skills by type (development, research, analysis)
- **Version Support**: Request specific skill versions with `name@version` syntax
- **Hot-reload**: Skills loaded at gateway startup from `config/skills/` directories

#### Filesystem Tools
- **Session Workspaces**: Isolated filesystem per session at `/tmp/shannon-sessions/{session_id}/`
- **file_read Tool**: Read files with JSON/YAML auto-parsing, encoding support
- **file_list Tool**: List directory contents with glob patterns, recursive option
- **Path Validation**: Canonical path resolution, symlink protection, allowlist enforcement

#### WASI Sandbox Integration
- **Sandbox Service**: gRPC service for secure file operations in Rust agent-core
- **Safe Commands**: Native Rust implementations of `ls`, `cat`, `head`, `tail`, `grep`, `find`
- **Session Isolation**: Cross-session access prevention with workspace boundaries
- **Audit Logging**: Structured tracing with session_id, operation, path, violation fields
- **Fail-closed Security**: Symlink validation rejects unresolvable targets
- **Shared Volumes**: Docker Compose `shannon-sessions` volume for agent-core and llm-service

### Documentation
- `docs/skills-system.md`: Skills API and custom skill creation guide
- `docs/session-workspaces.md`: Session isolation architecture and file tool usage

## [0.1.0] - 2025-12-25

### Added

#### Desktop Application
- **Pre-built Binaries**: Native desktop apps for macOS, Windows, and Linux
  - macOS: Universal binary (Intel + Apple Silicon) as `.dmg` and `.app.tar.gz`
  - Windows: `.msi` and `.exe` installers
  - Linux: `.AppImage` (portable) and `.deb` packages
- **Web UI Mode**: Run as local web server (`npm run dev` at `http://localhost:3000`)
- **OSS Mode**: Works without authentication - skip login and go directly to run page
- **Session Management**: Conversation history with auto-generated titles
- **Real-time Event Timeline**: Visual execution flow with SSE streaming
- **Multi-model Support**: Switch between LLM providers in the UI
- **Research Agent**: Deep research mode with configurable strategies

#### Core Features
- **OpenAI-compatible API**: Drop-in replacement (`/v1/chat/completions`)
- **Multi-Agent Orchestration**: Temporal-based workflows with DAG, Supervisor, and Strategy patterns
- **SSE Streaming**: Real-time task execution events via Server-Sent Events
- **Multi-tenant Architecture**: Tenant isolation, API key scoping, and rate limiting
- **Scheduled Tasks**: Cron-based recurring task execution with Temporal schedules
- **Token Budget Control**: Hard caps with automatic model fallback

#### Workflows & Research
- **Research Workflow**: Multi-step research with parallel agent execution and synthesis
- **Citation System**: Automatic source tracking with `[n]` format citations
- **Model Tier Override**: Per-activity model tier control (e.g., `synthesis_model_tier`)
- **Synthesis Templates**: Customizable output formatting via templates

#### Infrastructure
- **Agent Core (Rust)**: WASI sandbox, gRPC server, policy enforcement
- **Orchestrator (Go)**: Task routing, budget enforcement, OPA policies
- **LLM Service (Python)**: 15+ LLM providers (OpenAI, Anthropic, Google, DeepSeek, local models)
- **MCP Integration**: Native Model Context Protocol support for custom tools
- **Python SDK**: Official client library with CLI (`pip install shannon-sdk`)
- **Observability**: Prometheus metrics, Grafana dashboards, OpenTelemetry tracing

#### Developer Experience
- **Hot-reload Configuration**: Update `config/shannon.yaml` without restarts
- **Vendor Adapter Pattern**: Clean separation for custom integrations
- **Release Automation**: GitHub Actions builds Docker images + desktop apps on version tags

### Fixed

- **Tool Rate Limiting**: Use `agent_id` fallback to avoid asyncio collision
- **Rate Limit Accuracy**: Return remaining wait time instead of full interval
- **API Key Normalization**: Support both `sk-shannon-xxx` and `sk_xxx` formats
- **Citation Priority**: Let CitationAgent determine source ranking dynamically
- **Race Conditions**: Generate request UUID upfront to avoid concurrent execution issues
- **Research Role Support**: Accept `research_supervisor` in decomposition endpoint

### Security

- **WASI Sandbox**: Secure Python code execution in WebAssembly sandbox
- **JWT Authentication**: Token-based auth with refresh tokens and revocation
- **API Key Hashing**: Store hashed API keys with prefix-based lookup
- **Multi-tenant Isolation**: User/tenant scoping on all operations
- **OPA Policy Governance**: Fine-grained access control rules

### Documentation

- **Platform Guides**: Ubuntu, Rocky Linux, Windows setup instructions
- **API Reference**: OpenAPI spec and endpoint documentation
- **Desktop App Guide**: Development, building, and troubleshooting
- **Vendor Adapters**: Custom integration pattern documentation

### Technical Details

- **Languages**: Go 1.22+, Rust (stable), Python 3.11+
- **Infrastructure**: Temporal, PostgreSQL, Redis, Qdrant
- **Desktop**: Next.js, Tauri 2, React
- **Protocols**: gRPC, HTTP/2, Server-Sent Events

[0.1.0]: https://github.com/Kocoro-lab/Shannon/releases/tag/v0.1.0
