# Research: Embedded App Startup Readiness

## Decisions

### Embedded storage is local-only in embedded mode

- **Decision**: Use a single local data store instance for all embedded components in embedded mode, with no external database services required.
- **Rationale**: Embedded mode must be self-contained and offline-capable, so all state must be local and shared across the embedded API and workflow engine.
- **Alternatives considered**: Remote database services or separate stores per component (rejected due to connectivity dependence and higher failure risk during startup).

### Deterministic port fallback within a fixed range

- **Decision**: Attempt port 1906 first, then iterate 1907-1915 until an available port is found.
- **Rationale**: Predictable port discovery is required for the desktop UI to connect without race conditions.
- **Alternatives considered**: Random port selection or OS-assigned ports (rejected due to higher discovery complexity).

### Readiness gating via health checks

- **Decision**: Treat a positive readiness signal from the embedded service as the gate for enabling the chat UI.
- **Rationale**: Ensures users cannot prompt until the embedded service is fully initialized and healthy.
- **Alternatives considered**: Time-based delays or UI-only readiness indicators (rejected due to unreliability).

### Log buffering until UI is ready

- **Decision**: Buffer startup logs from embedded components and flush them to the debug console when the UI is ready.
- **Rationale**: Logs emitted before renderer readiness must still be visible for debugging.
- **Alternatives considered**: Dropping early logs or relying on external log files only (rejected due to loss of visibility in the debug console).
