# Feature Specification: Embedded App Startup Readiness

**Feature Branch**: `001-embedded-tauri-startup`  
**Created**: 2026-01-10  
**Status**: Draft  
**Input**: User description: "I want to ensure the functionality of the tauri application (shannon-desktop) in embedded mode where the application is self contained and uses the following stack for database and vector database, which are variable based on features and for the embedded application should be: 1. rusqlite for the application database in rust, the database behind the embedded api server, and the database that durable uses to back the workflow engine in embedded mode. There should be no surrealdb, lancedb or postgres for this. 2. We need to ensure that the tauri application can start up to completion with the user ready to prompt with the following workflow: - server starts up and ensures that the database is initialized, all migrations run, and the instance of the database is shared by all. - the axum server attempts first to bind to the address 0.0.0.0 at port 1906 after initialization of the database, etc. if that port is not available, it iterates through ports 1907-1915 until a free port is found. when the server finally binds to a port it will send a message that is sent through IPC to let the static web app renderer know which port was selected. However, this may happen before the renderer is loaded up. - when the renderer initializes the next.js static web application, it will attempt to connect to localhost at port 1906 if it has not already received the IPC message that set the port number (which may be missed). The renderer app will attempt to connect to the embedded server starting with port 1906 (or the port number passed to it via IPC). Only after getting a positive return from the health endpoint will the prompt/chat UI be made available and enabled. We need to prove that we can traverse this startup sequence with NO gaps, write integration tests (and NOT unit tests) to cover this entire startup cycle with 100% test coverage, so that will mean creativity in how the live test works. This task is NOT complete until we can prove that the tauri application starts up, the databases get properly initiaized, the axum api server embedded starts up properly with an assigned port, the health check passes, and the web application reaches a full state of readiness. In addition, we need to ensure that there is a way for ALL logs in the Rust side, the axum server, etc. can be seen in the desktop/components/debug-console.tsx. If that means logs from events in the server have to be stored somewhere until the UI starts up so they can be retrieved, then that is what we need, because we need to be able to debug EVERYTHING from that console."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Embedded app reaches ready-to-prompt state (Priority: P1)

As a user launching the desktop app in embedded mode, I want the app to finish startup and show a ready chat experience only after the embedded service is ready, so I can start prompting with confidence that the system is fully initialized.

**Why this priority**: This is the primary value of the app; without it, users cannot begin work.

**Independent Test**: Launch the app in embedded mode and verify that the chat UI becomes enabled only after the embedded service reports ready and startup is complete.

**Acceptance Scenarios**:

1. **Given** the app is started in embedded mode on a clean machine state, **When** startup completes, **Then** the chat UI becomes enabled and accepts prompts.
2. **Given** the embedded service is not yet ready, **When** the desktop UI loads, **Then** the chat UI stays disabled until readiness is confirmed.

---

### User Story 2 - Startup completes even when default port is unavailable (Priority: P2)

As a user launching the app, I want startup to succeed even if the default local port is already in use, so the app still becomes ready without manual troubleshooting.

**Why this priority**: Port conflicts are common and should not block usage.

**Independent Test**: Pre-occupy the default port, launch the app, and verify it selects an alternate port and still reaches ready state.

**Acceptance Scenarios**:

1. **Given** the default local port is occupied, **When** the app starts in embedded mode, **Then** it selects an available port in the configured range and becomes ready.

---

### User Story 3 - Full startup logs are visible in the debug console (Priority: P3)

As a developer or support engineer, I want to view all embedded-service logs in the debug console, including logs emitted before the UI loads, so I can diagnose startup issues from a single place.

**Why this priority**: Debugging and support depend on complete log visibility.

**Independent Test**: Generate logs during early startup, then open the debug console after UI load and verify the full log stream is visible.

**Acceptance Scenarios**:

1. **Given** logs are emitted before the UI is ready, **When** the debug console opens, **Then** those logs are available in chronological order.

---

### Edge Cases

- What happens when no port in the configured range is available?
- How does the app behave when startup readiness never becomes positive?
- What happens when data store initialization or migration fails at startup?
- How does the desktop UI handle missing or delayed port discovery messages?
- What happens when logs exceed in-memory capacity before the UI loads?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The embedded mode MUST start without requiring any external database services.
- **FR-002**: The app MUST initialize the local data store and complete required migrations before declaring the embedded service ready.
- **FR-003**: All embedded components MUST use a shared local data store instance during startup and runtime.
- **FR-004**: The embedded service MUST attempt to bind to a default local port and, if unavailable, select the first available port in a configured fallback range.
- **FR-005**: The desktop UI MUST be able to discover the active embedded service port even if an initial notification is missed.
- **FR-006**: The chat UI MUST remain disabled until the embedded service readiness signal is positive.
- **FR-007**: All startup and runtime logs from embedded components MUST be available in the debug console, including those emitted before the UI is ready.
- **FR-008**: The end-to-end embedded startup flow MUST be covered by automated tests that validate readiness from app launch to chat UI enabled state.

### Key Entities *(include if feature involves data)*

- **Embedded App Instance**: The desktop application running in self-contained mode, including its startup state and readiness status.
- **Local Data Store**: The shared persistent store used by all embedded components.
- **Embedded Service**: The local service that exposes readiness status and supports the chat UI.
- **Port Selection**: The chosen local port and any fallback selection details needed for discovery.
- **Log Entry**: A timestamped message produced by embedded components and made visible in the debug console.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The app reaches a ready-to-prompt state within 30 seconds on a typical developer machine in 95% of launches.
- **SC-002**: The app successfully reaches ready state in 100% of test runs where any single port in the fallback range is available.
- **SC-003**: 100% of defined acceptance scenarios are covered by automated tests.
- **SC-004**: 100% of startup logs emitted before UI readiness are visible in the debug console once it opens.

## Assumptions

- Embedded mode is the default for the desktop app during these tests.
- Local port range for fallback is predefined and consistent across runs.
- The readiness signal represents complete readiness for user prompts.
