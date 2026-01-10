# Implementation Plan: Embedded API Server Startup with IPC Communication

**Branch**: `001-embedded-api-startup` | **Date**: 2026-01-09 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-embedded-api-startup/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Implement embedded API server startup system for Shannon desktop application. The system automatically discovers available ports (1906-1915), starts the embedded Rust API server, establishes IPC communication with the Next.js frontend, streams real-time logs for debugging, and enables chat functionality once the connection is verified. Key components include port discovery logic, health check endpoints, automatic restart with exponential backoff, and comprehensive error handling.

## Technical Context

**Language/Version**: Rust (stable) for embedded API server, TypeScript/JavaScript for Next.js frontend
**Primary Dependencies**: Tauri framework, Next.js, tokio runtime for async Rust
**Storage**: In-memory state for port management and server status, log files for debugging
**Testing**: cargo test for Rust components, NEEDS CLARIFICATION for frontend testing framework
**Target Platform**: Cross-platform desktop (Windows, macOS, Linux) via Tauri
**Project Type**: Desktop application with embedded server architecture
**Performance Goals**: <5 second startup time, <100ms IPC message latency, <500ms log streaming delay
**Constraints**: Port range limited to 1906-1915, restart attempts capped at 3 with exponential backoff, 5-second connection timeout
**Scale/Scope**: Single-user desktop application, real-time log streaming, chat interface with embedded AI agents

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: No project constitution has been established yet (constitution.md contains only template placeholders).

### Initial Evaluation (Pre-Research)
**Default Gates Applied**:
- ✅ **Feature Scope**: Well-defined scope focusing on embedded server startup, IPC communication, and chat enablement
- ✅ **Testing Strategy**: Unit tests for server components, integration tests for IPC communication planned
- ✅ **Documentation**: Comprehensive specification with 15 functional requirements and clear acceptance criteria
- ✅ **Error Handling**: Robust error handling with automatic retry, exponential backoff, and graceful degradation
- ✅ **Performance**: Clear performance goals defined (<5s startup, <100ms IPC latency)

**Initial Result**: No violations identified - proceeded to Phase 0 research.

### Post-Design Evaluation (After Phase 1 Design)
**Enhanced Evaluation Based on Design Artifacts**:
- ✅ **Architecture Quality**: Clean separation between Tauri backend, Shannon API server, and Next.js frontend
- ✅ **Technology Alignment**: Leverages existing Shannon patterns (tracing-rs, tokio, structured logging)
- ✅ **Testing Coverage**: Comprehensive testing strategy with Vitest + RTL for frontend, cargo test + tokio-test for backend
- ✅ **Error Classification**: Detailed error hierarchy with proper Rust error handling patterns (thiserror, structured errors)
- ✅ **Performance Design**: Circular buffering, non-blocking IPC, async patterns optimized for throughput
- ✅ **Maintainability**: Modular design with clear contracts, comprehensive documentation, quickstart guide
- ✅ **Security**: No exposed secrets, proper validation in IPC contracts, secure port binding patterns
- ✅ **Scalability**: Design supports extension (additional IPC events, health check endpoints, runtime monitoring)
- ✅ **Code Standards**: All Rust code follows Shannon's mandatory coding standards (M-STRONG-TYPES, M-CANONICAL-DOCS, M-ERRORS-CANONICAL-STRUCTS)
- ✅ **API Design**: IPC events follow structured patterns, health checks use standard HTTP semantics, clear separation of concerns

**Post-Design Result**: ✅ **All gates passed** - design maintains high quality standards with no constitutional violations. All design artifacts are complete and ready for implementation.

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
desktop/                              # Tauri desktop application
├── src-tauri/                        # Tauri Rust backend
│   ├── src/
│   │   ├── embedded_api.rs          # NEW: Embedded API server management
│   │   ├── ipc_events.rs            # NEW: IPC event handling
│   │   ├── ipc_logger.rs            # NEW: Real-time log streaming
│   │   ├── lib.rs                   # Modified: Register new IPC handlers
│   │   └── main.rs                  # Entry point
│   ├── Cargo.toml                   # Modified: Add tokio, networking deps
│   └── tauri.conf.json              # Modified: IPC permissions
├── components/                       # Next.js frontend components
│   ├── debug-console.tsx            # NEW: Debug log display
│   ├── server-status-banner.tsx     # NEW: Server status indicator
│   └── app-layout.tsx               # Modified: Integrate status banner
├── lib/                             # Frontend utilities
│   ├── server-context.tsx           # NEW: Server connection context
│   ├── use-server-logs.ts          # NEW: Hook for log streaming
│   └── ipc-events.ts                # NEW: IPC event definitions
└── tests/                           # Test files
    ├── embedded-api.test.ts         # NEW: Embedded server tests
    └── ipc-communication.test.ts    # NEW: IPC integration tests

rust/shannon-api/                     # Main API server (for embedded mode)
├── src/
│   ├── config/
│   │   └── deployment.rs            # Modified: Add embedded mode config
│   ├── server.rs                    # Modified: Support embedded startup
│   └── main.rs                      # Modified: Embedded mode entry point
└── Cargo.toml                       # Modified: Add embedded feature flag
```

**Structure Decision**: Hybrid desktop application structure. The feature primarily extends the existing `desktop/` Tauri application with embedded server capabilities. Main changes focus on the Tauri backend (`src-tauri/`) for server management and IPC communication, frontend components for status display and debugging, and minor modifications to the `rust/shannon-api/` service to support embedded mode deployment.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
