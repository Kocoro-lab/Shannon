# Tasks: Embedded App Startup Readiness

**Input**: Design documents from `/specs/001-embedded-tauri-startup/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/embedded-api.yaml

## Phase 1: Setup (Shared Infrastructure)

- [X] T001 Create UI integration test harness notes in desktop/tests/integration/README.md
- [X] T002 Create shared Rust test helpers module in desktop/src-tauri/tests/support/mod.rs
- [X] T003 [P] Add embedded API contract reference in desktop/src-tauri/tests/fixtures/embedded-api.yaml

---

## Phase 2: Foundational (Blocking Prerequisites)

- [X] T004 Implement shared startup state container in desktop/src-tauri/src/embedded_api.rs
- [X] T005 Add port selection helper module in desktop/src-tauri/src/embedded_port.rs
- [X] T006 [P] Add IPC event names for port discovery and readiness in desktop/src-tauri/src/ipc_events.rs
- [X] T007 [P] Add log buffer store and drain API in desktop/src-tauri/src/ipc_logger.rs
- [X] T008 Implement renderer port discovery state in desktop/lib/server-context.tsx
- [X] T009 Implement readiness polling helper in desktop/lib/shannon/api.ts

**Checkpoint**: Foundation ready - user story implementation can now begin.

---

## Phase 3: User Story 1 - Embedded app reaches ready-to-prompt state (Priority: P1) 🎯 MVP

**Goal**: Users can prompt only after embedded startup completes and readiness is confirmed.

**Independent Test**: Launch in embedded mode and verify chat UI enables only after readiness is positive.

### Tests for User Story 1

- [X] T010 [P] [US1] Add embedded startup integration test in desktop/src-tauri/tests/embedded_startup.rs
- [X] T011 [P] [US1] Add UI readiness gating test in desktop/tests/integration/ready_gate.test.tsx

### Implementation for User Story 1

- [X] T012 [US1] Ensure local data store initialization and migration gating in desktop/src-tauri/src/embedded_api.rs
- [X] T013 [US1] Wire readiness signal emission after health checks in desktop/src-tauri/src/embedded_api.rs
- [X] T014 [US1] Disable chat UI until readiness in desktop/components/chat-input.tsx
- [X] T015 [US1] Expose readiness state to UI in desktop/lib/server-context.tsx

**Checkpoint**: User Story 1 is fully functional and testable independently.

---

## Phase 4: User Story 2 - Startup completes even when default port is unavailable (Priority: P2)

**Goal**: Startup succeeds when port 1906 is occupied by selecting the first available port in range.

**Independent Test**: Pre-occupy 1906 and verify the app binds to an available port and reaches ready state.

### Tests for User Story 2

- [X] T016 [P] [US2] Add port fallback integration test in desktop/src-tauri/tests/embedded_port_fallback.rs
- [X] T017 [P] [US2] Add UI fallback connect test in desktop/tests/integration/port_fallback.test.tsx

### Implementation for User Story 2

- [X] T018 [US2] Implement deterministic port scan in desktop/src-tauri/src/embedded_port.rs
- [X] T019 [US2] Emit selected port to UI via IPC in desktop/src-tauri/src/embedded_api.rs
- [X] T020 [US2] Implement UI fallback connection loop in desktop/lib/shannon/api.ts
- [X] T021 [US2] Persist selected port for renderer use in desktop/lib/server-context.tsx

**Checkpoint**: User Story 2 is fully functional and testable independently.

---

## Phase 5: User Story 3 - Full startup logs are visible in the debug console (Priority: P3)

**Goal**: All embedded logs, including pre-UI logs, are visible in the debug console.

**Independent Test**: Emit early startup logs and verify they appear in the debug console after UI load.

### Tests for User Story 3

- [X] T022 [P] [US3] Add log buffering integration test in desktop/src-tauri/tests/log_buffering.rs
- [X] T023 [P] [US3] Add debug console log replay test in desktop/tests/integration/debug_console_logs.test.tsx

### Implementation for User Story 3

- [X] T024 [US3] Buffer startup logs and expose drain API in desktop/src-tauri/src/ipc_logger.rs
- [X] T025 [US3] Flush buffered logs on renderer ready in desktop/src-tauri/src/ipc_events.rs
- [X] T026 [US3] Consume buffered logs in UI hook in desktop/lib/use-server-logs.ts
- [X] T027 [US3] Render buffered logs in debug console in desktop/components/debug-console.tsx

**Checkpoint**: User Story 3 is fully functional and testable independently.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T028 [P] Update debug console documentation in desktop/DEBUG_GUIDE.md
- [X] T029 [P] Update embedded startup documentation in desktop/EMBEDDED_API_STARTUP.md
- [X] T030 Run quickstart validation steps in specs/001-embedded-tauri-startup/quickstart.md

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
- **Polish (Phase 6)**: Depends on all desired user stories being complete

### User Story Dependencies

- **US1 (P1)**: Can start after Foundational - no dependency on US2/US3
- **US2 (P2)**: Can start after Foundational - independent of US1/US3
- **US3 (P3)**: Can start after Foundational - independent of US1/US2

### Parallel Opportunities

- Setup: T003 can run in parallel with T001-T002
- Foundational: T006 and T007 can run in parallel
- US1: T010 and T011 can run in parallel
- US2: T016 and T017 can run in parallel
- US3: T022 and T023 can run in parallel

---

## Parallel Example: User Story 1

```bash
Task: "Add embedded startup integration test in desktop/src-tauri/tests/embedded_startup.rs"
Task: "Add UI readiness gating test in desktop/tests/integration/ready_gate.test.tsx"
```

## Parallel Example: User Story 2

```bash
Task: "Add port fallback integration test in desktop/src-tauri/tests/embedded_port_fallback.rs"
Task: "Add UI fallback connect test in desktop/tests/integration/port_fallback.test.tsx"
```

## Parallel Example: User Story 3

```bash
Task: "Add log buffering integration test in desktop/src-tauri/tests/log_buffering.rs"
Task: "Add debug console log replay test in desktop/tests/integration/debug_console_logs.test.tsx"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: User Story 1
4. Validate readiness gating independently

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → readiness gating validated
3. US2 → port fallback validated
4. US3 → debug console log visibility validated
