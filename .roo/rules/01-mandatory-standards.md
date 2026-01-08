+++
title = "Mandatory Rust Coding Standards"
description = "MANDATORY Rust coding guidelines from docs/coding-standards/RUST.md"
priority = "critical"
applies_to = "rust"
+++

# Shannon (Prometheus) AI Platform - Mandatory Rust Coding Standards

## MANDATORY: Rust Coding Standards

ALL Rust code generation MUST follow the comprehensive guidelines in:
@docs/coding-standards/RUST.md

Key standards to always apply:
- Use strong types and avoid primitive obsession (M-STRONG-TYPES)
- Implement comprehensive documentation with canonical sections (M-CANONICAL-DOCS) 
- Use #[expect] for lint overrides instead of #[allow] (M-LINT-OVERRIDE-EXPECT)
- Implement proper error handling with canonical error structs (M-ERRORS-CANONICAL-STRUCTS)
- Ensure all public types implement Debug and Display where appropriate
- Use structured logging with message templates (M-LOG-STRUCTURED)
- Design APIs to be mockable for testing (M-MOCKABLE-SYSCALLS)
- Prefer smaller crates over large monoliths (M-SMALLER-CRATES)
- Use mimalloc as global allocator for applications (M-MIMALLOC-APPS)

IMPORTANT: When generating any Rust code, always reference and apply the full coding standards document.

## Architecture Priority

Shannon is transitioning to a unified Rust architecture:

**Core Services:**
- Shannon API (Rust) - rust/shannon-api/ ⭐ PRIMARY API (unified Gateway + LLM service)
- Orchestrator (Go) - go/orchestrator/ (workflow management, Temporal)
- Agent Core (Rust) - rust/agent-core/ (WASI sandbox, agent execution)

**Legacy Services (DEPRECATED):**
- Gateway (Go) - go/orchestrator/cmd/gateway/ ⚠️ Use Shannon API instead
- LLM Service (Python) - python/llm-service/ ⚠️ Use Shannon API instead

ALWAYS prefer Shannon API (Rust) development over legacy services.