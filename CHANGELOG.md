# Changelog

All notable changes to Shannon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-02-13

### Added
- Add swarm multi-agent workflow with P2P messaging
- Add Human-in-the-Loop research plan review system with Redis lock for feedback/approve race conditions
- Add skills system, filesystem tools, WASI sandbox integration, and session workspace mounting
- Add citation pipeline V2/V3 with placement-based validation, soft attribution guidance, and fallback logic
- Add deep research domain analysis, OODA loop, domain discovery, prefetch optimization, and multilingual enhancement
- Add research workflow tiered model architecture with quick_research_agent role and forced parallel execution
- Add multi-engine web search with localization, Finance API, and search-first strategy
- Add parallel_by template expansion and cron scheduling support for template workflows
- Add trading agent roles and template workflows
- Add browser_use tool migration from Enterprise to OSS
- Add temporal awareness to synthesis and search prompts
- Add DAG cycle detection, tool filtering, and idempotency TTL cleanup
- Add hybrid Python sandbox scaffolding (WASI + Firecracker)

### Fixed
- Fix duplicate tool execution after forced_tool_calls
- Fix synthesis "large" model_tier bleeding into gap-filling agents
- Fix session_id propagation to Python tools via ensureSessionContext helper
- Fix SSE streaming reliability for long-running tasks
- Fix citation snippet pollution with three-tier LLM signal detection and fallback-only filtering
- Fix research relationship identification to prevent vendor/customer misclassification
- Fix Chinese vs Japanese language detection in refine pass
- Fix web-subpage-fetch MAP_TIMEOUT from 15s to 45s
- Fix deep research output quality and completeness across multiple iterations
- Fix template_results truncation and warning-level import logging
- Fix non-research roles using general output format in interpretation pass
- Fix workspace creation race condition
- Fix protoc missing detection in proto generation script
- Fix domain prefetch for comparative query type

### Security
- Add SSRF hardening, BM25 guard, URL sanitization, and alias scoring
- Remove WASI /tmp symlink bypass and unify ModelTier enum
- Block absolute path writes and env secret leaks in sandbox
- Fix session ID validation, timeouts, and skill auth from code review

### Changed
- Skip title generation for non-first tasks in session (performance)
- Restrict Chinese sources to Chinese-company queries only
- Rename citation V3 to placement-based naming convention
- Translate Chinese comments to English in web_fetch, web_search, verify
- Update default COMPLEXITY_MODEL_ID to gpt-5
- Update architecture diagram, model names, and versions in README
- Sync docker-compose.release.yml and install.sh with Phase 4 features

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

[0.2.0]: https://github.com/Kocoro-lab/Shannon/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Kocoro-lab/Shannon/releases/tag/v0.1.0
