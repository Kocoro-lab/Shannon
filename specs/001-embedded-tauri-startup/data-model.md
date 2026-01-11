# Data Model: Embedded App Startup Readiness

## Embedded App Instance

- **Fields**: instance_id, launch_timestamp, startup_state, readiness_state, selected_port
- **Relationships**: owns one Embedded Service; uses one Local Data Store; produces many Log Entries
- **Validation**: readiness_state must not be "ready" until startup_state is "initialized"

## Local Data Store

- **Fields**: store_id, location, migration_version, initialized_at
- **Relationships**: shared by Embedded Service and workflow engine
- **Validation**: migration_version must be current before readiness_state can be "ready"

## Embedded Service

- **Fields**: service_id, base_url, bind_address, bind_port, startup_state, readiness_state
- **Relationships**: belongs to Embedded App Instance; exposes health checks
- **Validation**: bind_port must be within the fallback range; readiness_state depends on health checks

## Port Selection

- **Fields**: attempt_order, port, status, selected_at
- **Relationships**: linked to Embedded Service and Embedded App Instance
- **Validation**: exactly one Port Selection may be marked selected per instance

## Log Entry

- **Fields**: log_id, timestamp, level, message, component, buffered
- **Relationships**: belongs to Embedded App Instance
- **Validation**: buffered may be true only before UI readiness; logs must preserve chronological order
