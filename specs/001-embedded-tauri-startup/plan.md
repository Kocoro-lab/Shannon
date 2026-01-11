# Implementation Plan: Embedded App Startup Readiness

**Branch**: `001-embedded-tauri-startup` | **Date**: 2026-01-10 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-embedded-tauri-startup/spec.md`

## Summary

Ensure the embedded desktop app reaches a ready-to-prompt state by coordinating local data store initialization, embedded service startup with port fallback, UI readiness gating on health checks, and full log visibility in the debug console, validated end-to-end with automated tests.

## Technical Context

**Language/Version**: Rust 1.91.1, TypeScript 5, Next.js 16.1.1  
**Primary Dependencies**: Tauri 2.x, Axum, rusqlite, shannon-api, Next.js, tauri IPC/event system  
**Storage**: Local SQLite (embedded, shared across embedded components)  
**Testing**: cargo test, Vitest (desktop), Tauri integration tests  
**Target Platform**: Desktop (macOS, Windows, Linux)  
**Project Type**: Desktop app with embedded backend + static web renderer  
**Performance Goals**: Ready-to-prompt within 30 seconds on typical developer hardware (95% of launches)  
**Constraints**: Self-contained (no external databases), port fallback 1906-1915, offline-capable, logs retained until UI ready  
**Scale/Scope**: Single-user local instance per desktop app launch

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- Constitution file contains placeholders only; no enforceable gates defined.
- Gate status: PASS (no explicit constraints to validate)
- Re-check after Phase 1: PASS (no explicit constraints to validate)

## Project Structure

### Documentation (this feature)

```text
specs/001-embedded-tauri-startup/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
desktop/
├── app/                 # Next.js app routes and pages
├── components/          # UI components (including debug-console)
├── lib/                 # Frontend helpers, IPC hooks
├── src-tauri/
│   ├── src/             # Tauri host + embedded API startup
│   └── tests/           # Tauri integration tests

rust/
├── shannon-api/         # Embedded API server and health endpoints
└── durable-shannon/     # Embedded workflow engine
```

**Structure Decision**: Use the existing desktop Tauri + Next.js layout with embedded Rust services in `desktop/src-tauri/` and shared API logic in `rust/shannon-api/`.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

None.
