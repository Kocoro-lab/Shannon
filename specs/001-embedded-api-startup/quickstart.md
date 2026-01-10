# Quickstart Guide: Embedded API Server Startup with IPC Communication

**Feature**: 001-embedded-api-startup
**Date**: 2026-01-09

This guide helps developers implement the embedded API server startup system for Shannon desktop application. Follow the steps below to build a robust system that automatically discovers ports, starts the embedded server, and enables real-time chat functionality.

## Overview

The system automatically:
1. **Discovers available ports** (1906-1915) and starts the embedded API server
2. **Establishes IPC communication** between Tauri backend and Next.js frontend
3. **Streams real-time logs** for debugging and monitoring
4. **Enables chat functionality** once server connection is verified
5. **Handles failures gracefully** with automatic restart and exponential backoff

## Prerequisites

- Rust stable toolchain with tokio async runtime
- Node.js 18+ with Next.js 16+ and React 19.2.3
- Tauri framework configured for desktop development
- Existing Shannon API server (`rust/shannon-api/`)

## Development Workflow

### Phase 1: Enhanced Port Discovery (Rust Backend)

**Files to modify:** `desktop/src-tauri/src/embedded_api.rs` (new)

1. **Implement sequential port discovery** with robust error handling:

```rust
use tokio::net::TcpListener;
use std::net::SocketAddr;

async fn find_available_port(start: u16, end: u16) -> Result<(TcpListener, u16), PortBindError> {
    for port in start..=end {
        let addr = SocketAddr::from(([127, 0, 0, 1], port));
        match TcpListener::bind(addr).await {
            Ok(listener) => {
                tracing::info!("Successfully bound to port {}", port);
                return Ok((listener, port));
            }
            Err(e) => {
                tracing::debug!("Port {} unavailable: {}", port, e);
                // Continue to next port
            }
        }
    }
    Err(PortBindError::AllPortsUnavailable { start, end })
}
```

2. **Add comprehensive error classification** for intelligent retry decisions:

```rust
#[derive(Debug, thiserror::Error)]
pub enum PortBindError {
    #[error("All ports in range {start}-{end} are unavailable")]
    AllPortsUnavailable { start: u16, end: u16 },

    #[error("Permission denied binding to port {port}")]
    PermissionDenied { port: u16 },

    #[error("Network interface unavailable")]
    NetworkUnavailable,
}
```

### Phase 2: IPC Event System (Tauri Backend)

**Files to create:** `desktop/src-tauri/src/ipc_events.rs`, `desktop/src-tauri/src/ipc_logger.rs`

1. **Implement structured IPC events** following the contract in `contracts/ipc-events.json`:

```rust
#[derive(Debug, Clone, serde::Serialize)]
pub struct ServerStateChange {
    pub from: ServerState,
    pub to: ServerState,
    pub timestamp: String,
    pub port: Option<u16>,
    pub base_url: Option<String>,
    pub error: Option<String>,
    pub restart_attempt: Option<u32>,
    pub next_retry_delay_ms: Option<u64>,
}

impl ServerStateChange {
    pub fn emit_via_tauri(app: &tauri::AppHandle) -> Result<(), tauri::Error> {
        app.emit_all("server-state-change", self)
    }
}
```

2. **Set up real-time log streaming** with circular buffer pattern:

```rust
use std::sync::Arc;
use tokio::sync::RwLock;
use std::collections::VecDeque;

pub struct IpcLogger {
    buffer: Arc<RwLock<VecDeque<LogEntry>>>,
    app_handle: Arc<tauri::AppHandle>,
}

impl IpcLogger {
    const MAX_BUFFER_SIZE: usize = 1000;

    pub async fn log_event(&self, entry: LogEntry) {
        let mut buffer = self.buffer.write().await;
        if buffer.len() >= Self::MAX_BUFFER_SIZE {
            buffer.pop_front();
        }
        buffer.push_back(entry.clone());

        // Emit to frontend (non-blocking)
        if let Err(e) = self.app_handle.emit_all("server-log", &entry) {
            eprintln!("Failed to emit log event: {}", e);
        }
    }
}
```

### Phase 3: Health Check Endpoints (Rust API Server)

**Files to modify:** `rust/shannon-api/src/server.rs`

1. **Implement multi-endpoint health checks** following research findings:

```rust
// GET /health - Basic liveness probe
pub async fn health_handler() -> impl IntoResponse {
    Json(serde_json::json!({
        "status": "healthy",
        "timestamp": chrono::Utc::now().to_rfc3339(),
        "uptime_seconds": get_uptime_seconds()
    }))
}

// GET /ready - Comprehensive readiness check
pub async fn ready_handler(State(state): State<AppState>) -> impl IntoResponse {
    let health = HealthCheck::new()
        .check_database(&state.db)
        .await?
        .check_llm_provider(&state.config)
        .await?
        .check_workflow_engine(&state.workflow)
        .await?;

    match health.overall_status() {
        HealthStatus::Healthy => Ok(Json(health)),
        _ => Err(StatusCode::SERVICE_UNAVAILABLE)
    }
}
```

### Phase 4: Exponential Backoff Retry (Tauri Backend)

**Files to modify:** `desktop/src-tauri/src/embedded_api.rs`

1. **Implement retry manager** with configurable backoff:

```rust
pub struct RetryManager {
    max_attempts: u32,
    current_attempt: u32,
    backoff_sequence: Vec<Duration>,
    last_restart_time: Option<Instant>,
}

impl RetryManager {
    pub fn new() -> Self {
        Self {
            max_attempts: 3,
            current_attempt: 0,
            backoff_sequence: vec![
                Duration::from_secs(1),  // Attempt 1: 1s
                Duration::from_secs(2),  // Attempt 2: 2s
                Duration::from_secs(4),  // Attempt 3: 4s
            ],
            last_restart_time: None,
        }
    }

    pub async fn attempt_restart(&mut self) -> Result<(), RestartError> {
        if self.current_attempt >= self.max_attempts {
            return Err(RestartError::MaxAttemptsExceeded);
        }

        if self.current_attempt > 0 {
            let delay = self.backoff_sequence[self.current_attempt as usize - 1];
            tokio::time::sleep(delay).await;
        }

        self.current_attempt += 1;
        self.last_restart_time = Some(Instant::now());

        // Attempt server restart logic here
        match self.try_start_server().await {
            Ok(_) => {
                self.reset_attempts();
                Ok(())
            }
            Err(e) => Err(RestartError::StartupFailed(e))
        }
    }
}
```

### Phase 5: Frontend Integration (Next.js/React)

**Files to create:** `desktop/components/debug-console.tsx`, `desktop/lib/use-server-logs.ts`, `desktop/lib/server-context.tsx`

1. **Create real-time debug console** with React 19 patterns:

```typescript
import { useCallback, useEffect, useState } from 'react';
import { listen } from '@tauri-apps/api/event';

export function useServerLogs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isConnected, setIsConnected] = useState(false);

  useEffect(() => {
    // Listen for real-time log events
    const unlisten = listen<LogEntry>('server-log', (event) => {
      setLogs(prev => {
        const newLogs = [...prev, event.payload];
        // Maintain circular buffer
        return newLogs.slice(-1000);
      });
    });

    return () => {
      unlisten.then(fn => fn());
    };
  }, []);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  return { logs, isConnected, clearLogs };
}
```

2. **Implement server connection context**:

```typescript
export const ServerContext = createContext<{
  serverState: ServerState;
  baseUrl: string | null;
  isReady: boolean;
  restartServer: () => Promise<void>;
}>({
  serverState: 'idle',
  baseUrl: null,
  isReady: false,
  restartServer: async () => {},
});

export function ServerProvider({ children }: { children: ReactNode }) {
  const [serverState, setServerState] = useState<ServerState>('idle');
  const [baseUrl, setBaseUrl] = useState<string | null>(null);

  useEffect(() => {
    const unlisten = listen<ServerStateChange>('server-state-change', (event) => {
      const { to, base_url } = event.payload;
      setServerState(to);
      setBaseUrl(base_url || null);
    });

    return () => { unlisten.then(fn => fn()); };
  }, []);

  const isReady = serverState === 'ready' && baseUrl !== null;

  return (
    <ServerContext.Provider value={{ serverState, baseUrl, isReady, restartServer }}>
      {children}
    </ServerContext.Provider>
  );
}
```

## Testing Strategy

### Backend Tests (Rust)

**Framework**: `cargo test` with tokio-test for async code

```rust
#[tokio::test]
async fn test_port_discovery_sequential() {
    let (listener, port) = find_available_port(1906, 1915).await.unwrap();
    assert!(port >= 1906 && port <= 1915);
    drop(listener); // Release port
}

#[tokio::test]
async fn test_retry_manager_exponential_backoff() {
    let mut retry = RetryManager::new();
    let start = Instant::now();

    // Simulate failure scenarios
    let result = retry.attempt_restart().await;
    let elapsed = start.elapsed();

    // Verify backoff timing
    assert!(elapsed >= Duration::from_millis(900)); // ~1s with tolerance
}
```

### Frontend Tests (Next.js)

**Framework**: Vitest + React Testing Library + Tauri IPC mocking

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { mockIPC } from '@tauri-apps/api/mocks';

describe('Debug Console', () => {
  it('displays real-time server logs', async () => {
    // Mock Tauri IPC
    mockIPC((cmd, args) => {
      if (cmd === 'listen') {
        return Promise.resolve(() => {}); // Mock unlisten function
      }
    });

    render(<DebugConsole />);

    // Simulate log event
    const logEvent = {
      timestamp: '2026-01-09T10:30:15.123Z',
      level: 'info',
      component: 'embedded-api',
      message: 'Server started successfully'
    };

    // Trigger mock event
    window.__TAURI__.event.emit('server-log', logEvent);

    await waitFor(() => {
      expect(screen.getByText('Server started successfully')).toBeInTheDocument();
    });
  });
});
```

## Key Implementation Patterns

### 1. **State Management**
- Use structured enums for server states with clear transitions
- Implement comprehensive error classification for intelligent handling
- Maintain immutable state with proper Clone implementations

### 2. **Async Patterns**
- Use `tokio::time::sleep()` for non-blocking delays in retry logic
- Implement proper resource cleanup with RAII patterns
- Use `Arc<RwLock<T>>` for shared state between threads

### 3. **Error Handling**
- Follow [M-ERRORS-CANONICAL-STRUCTS] with `thiserror` for structured errors
- Implement `Display` and `Debug` for all error types
- Use `Result<T, E>` consistently throughout async code

### 4. **Logging Integration**
- Use `tracing-rs` with structured logging patterns
- Follow [M-LOG-STRUCTURED] with message templates
- Implement proper log level filtering and component categorization

## File Checklist

- [ ] `desktop/src-tauri/src/embedded_api.rs` - Port discovery and server management
- [ ] `desktop/src-tauri/src/ipc_events.rs` - IPC event definitions
- [ ] `desktop/src-tauri/src/ipc_logger.rs` - Real-time log streaming
- [ ] `desktop/components/debug-console.tsx` - Debug log display component
- [ ] `desktop/components/server-status-banner.tsx` - Server status indicator
- [ ] `desktop/lib/server-context.tsx` - Server connection context
- [ ] `desktop/lib/use-server-logs.ts` - Log streaming hook
- [ ] `rust/shannon-api/src/server.rs` - Health check endpoints
- [ ] `desktop/src-tauri/Cargo.toml` - Add tokio, networking dependencies
- [ ] `desktop/package.json` - Add Vitest and testing dependencies

## Next Steps

1. **Start with backend implementation** - Port discovery and IPC events
2. **Add health check endpoints** to shannon-api server
3. **Implement frontend components** with real-time log display
4. **Add comprehensive testing** for both backend and frontend
5. **Integration testing** with end-to-end startup scenarios

For detailed entity relationships and validation rules, see `data-model.md`. For complete IPC event specifications, see `contracts/ipc-events.json`.