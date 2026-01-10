# Feature Specification: Embedded API Server Startup with IPC Communication

**Feature Branch**: `001-embedded-api-startup`
**Created**: 2026-01-09
**Status**: Draft
**Input**: User description: "I need to ensure that the shannon-desktop Tauri application is properly set up to run in the following way: 1. Process starts and the embedded API server attempts to start at port 1906. 2. If port 1906 is not available, then startup attempts to increase the port by 1 until a free port is found before 1916. 3. When the embedded server starts successfully, an IPC message is sent to the Next.js static app notifying it of the port number to use to set up the connection for the app. 4. During this process IPC is used to report ALL logs and state do the app, so it can be seen in the debugger wrapper view. 5. Once the app gets the IPC message, the app connects and pulls information from the embedded server. 6. The app enables the chat functions, so the user can start interacting with the agents and send prompts to the embedded api server."

## Clarifications

### Session 2026-01-09

- Q: How should the frontend verify that the embedded server is ready to handle chat requests? → A: Health check endpoint - Frontend calls a dedicated `/health` or `/ready` endpoint
- Q: How should the system respond when the embedded server crashes after successful startup? → A: Automatic restart - Attempt to restart the embedded server automatically while notifying user
- Q: How many times should the system attempt to restart a crashed embedded server before giving up? → A: Three restart attempts with exponential backoff (1s, 2s, 4s delays) then show permanent error
- Q: How long should the frontend wait for the embedded server to respond before timing out? → A: 5 seconds with retry
- Q: How should the system respond when IPC communication fails during startup? → A: Fallback with restart - Attempt IPC recovery, then gracefully restart initialization process

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Application Startup and Connection (Priority: P1)

A user launches the Shannon desktop application and expects it to start up successfully, automatically connecting to the backend services so they can begin using the chat functionality.

**Why this priority**: This is the core functionality that enables all other features. Without successful startup and connection, the application is unusable.

**Independent Test**: Can be fully tested by launching the application and verifying the chat interface becomes enabled, delivering immediate value as a working chat application.

**Acceptance Scenarios**:

1. **Given** the Shannon desktop application is not running, **When** the user launches the application, **Then** the embedded API server starts on port 1906 and the chat interface becomes available
2. **Given** port 1906 is already in use by another service, **When** the user launches the application, **Then** the embedded API server starts on the next available port (1907-1915) and the chat interface becomes available
3. **Given** the application has started successfully, **When** the user opens the chat interface, **Then** they can send messages to the embedded API server and receive responses

---

### User Story 2 - Real-time Startup Monitoring (Priority: P2)

A developer or power user wants to monitor the application startup process to understand what's happening during initialization and troubleshoot any issues.

**Why this priority**: Essential for debugging and support, but not required for basic functionality. Improves maintainability and user confidence.

**Independent Test**: Can be tested independently by opening the debug console during startup and verifying that all startup events are logged in real-time.

**Acceptance Scenarios**:

1. **Given** the application is starting up, **When** the developer opens the debug console, **Then** they can see all startup logs including port discovery attempts and server initialization status
2. **Given** the embedded server encounters an error during startup, **When** viewing the debug console, **Then** the error details are displayed with sufficient information for troubleshooting
3. **Given** the embedded server successfully starts, **When** viewing the debug console, **Then** the final port number and connection status are clearly displayed

---

### User Story 3 - Graceful Startup Failure Handling (Priority: P3)

A user attempts to start the application when all preferred ports (1906-1915) are unavailable, and the system should provide clear feedback about the issue.

**Why this priority**: Important for robustness but represents an edge case. Most users won't encounter this scenario in normal usage.

**Independent Test**: Can be tested by blocking ports 1906-1915 and launching the application, verifying appropriate error messages are shown.

**Acceptance Scenarios**:

1. **Given** all ports from 1906 to 1915 are unavailable, **When** the user launches the application, **Then** a clear error message explains the port availability issue and suggests solutions
2. **Given** the embedded server fails to start after multiple port attempts, **When** the failure occurs, **Then** the application displays diagnostic information to help the user resolve the issue

---

### Edge Cases

- When the embedded API server crashes after startup, the system automatically attempts to restart it up to three times with exponential backoff delays (1s, 2s, 4s) before showing a permanent error
- When the embedded server takes longer than 5 seconds to respond, the frontend retries the connection before considering it failed
- When IPC communication fails during startup, the system attempts to recover the IPC channel and then gracefully restarts the initialization process if recovery fails
- How does the system handle network connectivity issues between the frontend and embedded server?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST attempt to start the embedded API server on port 1906 when the application launches
- **FR-002**: System MUST automatically try the next available port (1907, 1908, etc.) if the preferred port is unavailable, up to port 1915
- **FR-003**: System MUST send an IPC message to the frontend containing the successful port number when the embedded server starts successfully
- **FR-004**: System MUST stream all startup logs and state changes to the frontend via IPC for display in the debug console
- **FR-005**: Frontend MUST wait for the IPC server-ready message before attempting to connect to the embedded API server
- **FR-006**: Frontend MUST establish a connection to the embedded server using the port number received via IPC
- **FR-007**: System MUST disable chat functionality until the connection to the embedded server is established and verified
- **FR-008**: System MUST enable chat functionality once the frontend successfully connects to the embedded server and receives a successful response from the server's health check endpoint
- **FR-009**: Embedded API server MUST provide a health check endpoint (such as `/health` or `/ready`) that confirms server readiness to handle chat requests
- **FR-010**: System MUST provide error handling and user feedback when all ports in the range (1906-1915) are unavailable
- **FR-011**: System MUST maintain IPC communication throughout the application lifecycle for ongoing log streaming
- **FR-012**: System MUST automatically attempt to restart the embedded API server when it crashes, with a maximum of three restart attempts using exponential backoff delays (1 second, 2 seconds, 4 seconds)
- **FR-013**: System MUST display a permanent error message and disable automatic restart attempts after three consecutive restart failures
- **FR-014**: Frontend MUST implement a 5-second timeout for embedded server connections and retry failed connections before considering them permanently failed
- **FR-015**: System MUST attempt to recover IPC communication when it fails during startup, and gracefully restart the entire initialization process if IPC recovery fails

### Key Entities

- **Embedded API Server**: The backend service that handles agent interactions and chat functionality, requires a specific port to operate
- **IPC Message**: Communication mechanism between the Tauri backend and Next.js frontend, carries port numbers and startup status
- **Debug Console**: Frontend component that displays real-time logs and system state for debugging purposes
- **Chat Interface**: Frontend component that enables user interaction with agents, dependent on server connection status
- **Port Range**: The allowable port numbers (1906-1915) for the embedded server, with preference for lower numbers
- **Health Check Endpoint**: A dedicated server endpoint that confirms the embedded API server is ready to handle chat requests
- **Crash Recovery System**: Monitors embedded server health and automatically restarts it when failures are detected, with exponential backoff retry logic
- **Connection Timeout**: A 5-second limit for establishing connections to the embedded server before retrying
- **IPC Recovery System**: Monitors IPC communication health and attempts recovery before restarting the initialization process

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Application startup completes and chat functionality becomes available within 10 seconds under normal conditions
- **SC-002**: System successfully finds and uses an available port in 95% of startup attempts when preferred ports are blocked
- **SC-003**: All startup events and logs are displayed in the debug console within 500ms of occurrence
- **SC-004**: Users can successfully send their first chat message within 30 seconds of launching the application
- **SC-005**: Application provides clear error feedback within 15 seconds when startup fails due to port unavailability
- **SC-006**: IPC communication maintains real-time log streaming with less than 100ms delay between backend events and frontend display

## Assumptions

- The Shannon desktop application uses Tauri framework for the desktop wrapper and Next.js for the frontend
- The embedded API server is a Rust-based service that can be started programmatically from the Tauri backend
- Port range 1906-1915 provides sufficient options for most deployment scenarios
- IPC communication is reliable between Tauri backend and Next.js frontend
- Users have necessary permissions to bind to ports in the specified range
- The debug console view already exists or will be implemented as part of this feature
- Chat functionality refers to sending prompts to AI agents through the embedded server