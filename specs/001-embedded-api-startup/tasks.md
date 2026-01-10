# Tasks: Embedded API Server Startup with IPC Communication

**Input**: Design documents from `/specs/001-embedded-api-startup/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests are NOT included in this implementation as they were not explicitly requested in the feature specification.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure:
- **Tauri Backend**: `desktop/src-tauri/src/`
- **Frontend Components**: `desktop/components/`
- **Frontend Libraries**: `desktop/lib/`
- **Shannon API**: `rust/shannon-api/src/`
- **Configuration**: `desktop/src-tauri/Cargo.toml`, `desktop/package.json`

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [ ] T001 Update desktop/src-tauri/Cargo.toml to add tokio, networking, and IPC dependencies
- [ ] T002 Update desktop/package.json to add Vitest, React Testing Library, and @tauri-apps/api/mocks dependencies
- [ ] T003 [P] Create IPC event type definitions in desktop/lib/ipc-events.ts based on contracts/ipc-events.json
- [ ] T004 [P] Update desktop/src-tauri/tauri.conf.json with required IPC permissions for server events

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T005 Create embedded API server state management in desktop/src-tauri/src/embedded_api.rs
- [ ] T006 [P] Create IPC event handling system in desktop/src-tauri/src/ipc_events.rs
- [ ] T007 [P] Create real-time log streaming infrastructure in desktop/src-tauri/src/ipc_logger.rs
- [ ] T008 Implement port discovery logic with sequential binding (1906-1915) in desktop/src-tauri/src/embedded_api.rs
- [ ] T009 [P] Add health check endpoints (/health, /ready, /startup) to rust/shannon-api/src/server.rs
- [ ] T010 [P] Create exponential backoff retry manager in desktop/src-tauri/src/embedded_api.rs
- [ ] T011 Register IPC handlers in desktop/src-tauri/src/lib.rs for all embedded API events
- [ ] T012 [P] Add embedded mode configuration support to rust/shannon-api/src/config/deployment.rs

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Application Startup and Connection (Priority: P1) 🎯 MVP

**Goal**: Enable basic application startup with automatic server connection and chat functionality

**Independent Test**: Launch the application and verify the chat interface becomes enabled after successful server startup

### Implementation for User Story 1

- [ ] T013 [P] [US1] Implement server state transitions (Idle → Starting → Port Discovery → Binding → Ready) in desktop/src-tauri/src/embedded_api.rs
- [ ] T014 [P] [US1] Create server connection context provider in desktop/lib/server-context.tsx
- [ ] T015 [US1] Implement automatic server startup on application launch in desktop/src-tauri/src/embedded_api.rs
- [ ] T016 [US1] Add server ready IPC message emission in desktop/src-tauri/src/ipc_events.rs
- [ ] T017 [US1] Create chat interface state management based on server connection in desktop/components/chat-input.tsx
- [ ] T018 [US1] Implement health check verification before enabling chat in desktop/lib/server-context.tsx
- [ ] T019 [US1] Add server status banner component in desktop/components/server-status-banner.tsx
- [ ] T020 [US1] Integrate server status banner in desktop/components/app-layout.tsx
- [ ] T021 [US1] Add 5-second connection timeout and retry logic in desktop/lib/server-context.tsx
- [ ] T022 [US1] Implement proper error handling for all ports unavailable scenario in desktop/src-tauri/src/embedded_api.rs

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Real-time Startup Monitoring (Priority: P2)

**Goal**: Provide developers and power users with real-time visibility into the startup process for debugging

**Independent Test**: Open the debug console during startup and verify all startup events are logged in real-time

### Implementation for User Story 2

- [ ] T023 [P] [US2] Create debug console component in desktop/components/debug-console.tsx
- [ ] T024 [P] [US2] Create log entry display component in desktop/components/log-entry.tsx
- [ ] T025 [P] [US2] Implement useServerLogs hook for real-time log streaming in desktop/lib/use-server-logs.ts
- [ ] T026 [US2] Add log event emission to all server state transitions in desktop/src-tauri/src/embedded_api.rs
- [ ] T027 [US2] Implement circular buffer for log entries (max 1000) in desktop/lib/use-server-logs.ts
- [ ] T028 [US2] Add log level filtering functionality in desktop/components/debug-console.tsx
- [ ] T029 [US2] Add component-based filtering in debug console in desktop/components/debug-console.tsx
- [ ] T030 [US2] Implement search functionality with 300ms debouncing in desktop/components/debug-console.tsx
- [ ] T031 [US2] Add auto-scroll functionality with manual override in desktop/components/debug-console.tsx
- [ ] T032 [US2] Integrate debug console wrapper in desktop/components/debug-console-wrapper.tsx
- [ ] T033 [US2] Add detailed error logging for troubleshooting in desktop/src-tauri/src/embedded_api.rs

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - Graceful Startup Failure Handling (Priority: P3)

**Goal**: Provide clear error messages and recovery options when all preferred ports are unavailable

**Independent Test**: Block ports 1906-1915 and launch the application, verifying appropriate error messages are shown

### Implementation for User Story 3

- [ ] T034 [P] [US3] Create comprehensive error types for port binding failures in desktop/src-tauri/src/embedded_api.rs
- [ ] T035 [P] [US3] Implement automatic restart with exponential backoff (1s, 2s, 4s) in desktop/src-tauri/src/embedded_api.rs
- [ ] T036 [US3] Add restart attempt tracking and maximum attempt enforcement in desktop/src-tauri/src/embedded_api.rs
- [ ] T037 [US3] Implement IPC recovery system for communication failures in desktop/src-tauri/src/ipc_events.rs
- [ ] T038 [US3] Create user-friendly error messages for port unavailability in desktop/components/server-status-banner.tsx
- [ ] T039 [US3] Add diagnostic information display for startup failures in desktop/components/debug-console.tsx
- [ ] T040 [US3] Implement permanent error state after max restart attempts in desktop/src-tauri/src/embedded_api.rs
- [ ] T041 [US3] Add manual restart functionality in server status banner in desktop/components/server-status-banner.tsx
- [ ] T042 [US3] Create graceful initialization restart for IPC failures in desktop/src-tauri/src/embedded_api.rs
- [ ] T043 [US3] Add recovery suggestions in error messages in desktop/components/server-status-banner.tsx

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T044 [P] Add structured logging with tracing-rs throughout all server operations in desktop/src-tauri/src/embedded_api.rs
- [ ] T045 [P] Implement proper error display traits (Debug, Display) for all error types in desktop/src-tauri/src/embedded_api.rs
- [ ] T046 [P] Add performance metrics collection for startup timing in desktop/src-tauri/src/embedded_api.rs
- [ ] T047 Code cleanup and refactoring for consistent error handling patterns across all modules
- [ ] T048 [P] Add comprehensive documentation comments for all public APIs in desktop/src-tauri/src/embedded_api.rs
- [ ] T049 [P] Validate quickstart.md examples work with implemented code
- [ ] T050 Security review for IPC message validation and port binding permissions
- [ ] T051 Performance optimization for circular buffer operations and IPC message throughput

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational (Phase 2) - May integrate with US1 debug visibility but is independently testable
- **User Story 3 (P3)**: Can start after Foundational (Phase 2) - May integrate with US1 error handling but is independently testable

### Within Each User Story

- Core implementation before UI integration
- State management before component creation
- Backend functionality before frontend consumption
- Error handling after core functionality
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel
- All Foundational tasks marked [P] can run in parallel (within Phase 2)
- Once Foundational phase completes, all user stories can start in parallel (if team capacity allows)
- Models within a story marked [P] can run in parallel
- Different user stories can be worked on in parallel by different team members

---

## Parallel Example: User Story 1

```bash
# Launch core server functionality in parallel:
Task: "Implement server state transitions in desktop/src-tauri/src/embedded_api.rs"
Task: "Create server connection context provider in desktop/lib/server-context.tsx"

# Launch UI components in parallel after core functionality:
Task: "Create chat interface state management in desktop/components/chat-input.tsx"
Task: "Add server status banner component in desktop/components/server-status-banner.tsx"
```

---

## Parallel Example: User Story 2

```bash
# Launch debug console components in parallel:
Task: "Create debug console component in desktop/components/debug-console.tsx"
Task: "Create log entry display component in desktop/components/log-entry.tsx"
Task: "Implement useServerLogs hook in desktop/lib/use-server-logs.ts"

# Launch filtering functionality in parallel:
Task: "Add log level filtering in desktop/components/debug-console.tsx"
Task: "Add component-based filtering in desktop/components/debug-console.tsx"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 (Application Startup and Connection)
   - Developer B: User Story 2 (Real-time Startup Monitoring)
   - Developer C: User Story 3 (Graceful Startup Failure Handling)
3. Stories complete and integrate independently

---

## Success Validation Criteria

### User Story 1 (P1) - MVP Success Criteria
- Application launches and embedded API server starts within 10 seconds
- Chat interface becomes enabled after successful server connection
- Port discovery works correctly when 1906 is unavailable
- Health check verification passes before chat enablement

### User Story 2 (P2) - Debug Monitoring Success Criteria
- Debug console displays all startup events in real-time (< 500ms delay)
- Log filtering and search functionality work correctly
- Circular buffer maintains 1000 entries without memory leaks
- Auto-scroll behavior works as expected

### User Story 3 (P3) - Error Handling Success Criteria
- Clear error messages when all ports 1906-1915 are unavailable
- Automatic restart with exponential backoff (3 attempts max)
- IPC recovery system handles communication failures gracefully
- Manual restart functionality works from error state

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
- Follow Shannon Rust coding standards (M-STRONG-TYPES, M-CANONICAL-DOCS, M-ERRORS-CANONICAL-STRUCTS)
- Use structured logging with tracing-rs (M-LOG-STRUCTURED)
- Implement proper async patterns with tokio runtime