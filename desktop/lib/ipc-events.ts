/**
 * IPC Event Type Definitions for Shannon Embedded API Server
 *
 * This file contains TypeScript definitions for all IPC events
 * exchanged between the Tauri backend and Next.js frontend.
 *
 * Based on contracts/ipc-events.json specification.
 */

// ============================================================================
// SERVER STATE TYPES
// ============================================================================

export type ServerState =
  | 'idle'
  | 'starting'
  | 'port-discovery'
  | 'binding'
  | 'initializing'
  | 'ready'
  | 'running'
  | 'crashed'
  | 'failed'
  | 'restarting'
  | 'stopping'
  | 'stopped';

export type LifecyclePhase = ServerState;

export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy';

export type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'trace' | 'critical';

export type Component =
  | 'embedded-api'
  | 'http-server'
  | 'database'
  | 'llm-client'
  | 'workflow-engine'
  | 'auth'
  | 'ipc'
  | 'health-check'
  | 'system';

// ============================================================================
// IPC EVENT PAYLOADS
// ============================================================================

/**
 * Payload for server-state-change IPC event
 * Emitted when embedded server state changes during startup, operation, or recovery
 */
export interface ServerStateChangePayload {
  /** Previous server state */
  from: ServerState;
  /** New server state */
  to: ServerState;
  /** ISO 8601 timestamp of state change */
  timestamp: string;
  /** Port number if available, null during discovery */
  port: number | null;
  /** Complete server URL when available */
  base_url: string | null;
  /** Error message if state change due to failure */
  error: string | null;
  /** Current restart attempt number if restarting */
  restart_attempt: number | null;
  /** Milliseconds until next restart attempt */
  next_retry_delay_ms: number | null;
}

/**
 * Payload for server-log IPC event
 * Real-time log events from embedded API server for debug console
 */
export interface ServerLogPayload {
  /** ISO 8601 timestamp of log event */
  timestamp: string;
  /** Log level */
  level: LogLevel;
  /** Source component that generated the log */
  component: Component;
  /** Human-readable log message */
  message: string;
  /** Optional structured context data */
  context?: Record<string, string>;
  /** Detailed error information if log level is error */
  error?: {
    type: string;
    message: string;
    stack?: string | null;
  } | null;
  /** Operation duration in milliseconds for performance tracking */
  duration_ms?: number | null;
}

/**
 * Payload for server-health IPC event
 * Health check results for embedded API server monitoring
 */
export interface ServerHealthPayload {
  /** Health check endpoint that was tested */
  endpoint: '/health' | '/ready' | '/startup';
  /** Overall health status */
  status: HealthStatus;
  /** When health check was performed */
  timestamp: string;
  /** Response time in milliseconds */
  response_time_ms: number;
  /** Detailed health check results by component */
  details?: {
    database?: {
      status: 'healthy' | 'unhealthy';
      response_time_ms: number;
    };
    llm_provider?: {
      status: 'configured' | 'missing';
      provider?: string | null;
    };
    workflow_engine?: {
      status: 'ready' | 'initializing' | 'failed';
    };
  };
}

/**
 * Payload for server-request IPC event
 * Access logs for HTTP requests handled by the embedded server
 */
export interface ServerRequestPayload {
  /** ISO 8601 timestamp of request */
  timestamp: string;
  /** HTTP method */
  method: string;
  /** Request path */
  path: string;
  /** Event type (request_start, request_c omplete, etc.) */
  event_type: string;
  /** HTTP status code (if completed) */
  status_code?: number | null;
  /** Error message (if failed) */
  error?: string | null;
  /** Request duration in milliseconds */
  duration_ms?: number | null;
}

/**
 * Payload for server-restart-attempt IPC event
 * Emitted during server restart attempts with exponential backoff
 */
export interface ServerRestartAttemptPayload {
  /** Current restart attempt number */
  attempt: number;
  /** Maximum allowed restart attempts */
  max_attempts: 3;
  /** Whether restart attempt was successful */
  success: boolean;
  /** When restart attempt was made */
  timestamp: string;
  /** Error message if restart failed */
  error: string | null;
  /** Milliseconds until next attempt (null if successful or no more attempts) */
  next_delay_ms: number | null;
  /** Port used for restart attempt */
  port: number | null;
}

// ============================================================================
// IPC COMMAND RESPONSES
// ============================================================================

/**
 * Response for get_server_status command
 */
export interface ServerStatusResponse {
  state: ServerState;
  port: number | null;
  base_url: string | null;
  uptime_seconds: number;
  restart_count: number;
  last_error: string | null;
}

/**
 * Response for restart_embedded_server command
 */
export interface RestartServerResponse {
  /** Whether restart request was accepted */
  accepted: boolean;
  /** Reason if restart was rejected */
  reason: string | null;
}

// ============================================================================
// IPC COMMAND PARAMETERS
// ============================================================================

/**
 * Parameters for get_recent_logs command
 */
export interface GetRecentLogsParams {
  /** Number of recent log entries to retrieve */
  count?: number;
  /** Optional log level filter */
  level_filter?: LogLevel | null;
}

/**
 * Parameters for restart_embedded_server command
 */
export interface RestartServerParams {
  /** Force restart even if server is healthy */
  force?: boolean;
}

// ============================================================================
// IPC EVENT NAMES (TYPE-SAFE CONSTANTS)
// ============================================================================

export const IPC_EVENTS = {
  SERVER_STATE_CHANGE: 'server-state-change',
  SERVER_LOG: 'server-log',
  SERVER_HEALTH: 'server-health',
  SERVER_REQUEST: 'server-request',
  SERVER_RESTART_ATTEMPT: 'server-restart-attempt',
} as const;

export const IPC_COMMANDS = {
  GET_SERVER_STATUS: 'get_server_status',
  GET_RECENT_LOGS: 'get_recent_logs',
  RESTART_EMBEDDED_SERVER: 'restart_embedded_server',
} as const;

// ============================================================================
// TYPE HELPERS
// ============================================================================

/**
 * Union type of all IPC event names
 */
export type IpcEventName = typeof IPC_EVENTS[keyof typeof IPC_EVENTS];

/**
 * Union type of all IPC command names
 */
export type IpcCommandName = typeof IPC_COMMANDS[keyof typeof IPC_COMMANDS];

/**
 * Map IPC event names to their payload types
 */
export interface IpcEventMap {
  [IPC_EVENTS.SERVER_STATE_CHANGE]: ServerStateChangePayload;
  [IPC_EVENTS.SERVER_LOG]: ServerLogPayload;
  [IPC_EVENTS.SERVER_HEALTH]: ServerHealthPayload;
  [IPC_EVENTS.SERVER_REQUEST]: ServerRequestPayload;
  [IPC_EVENTS.SERVER_RESTART_ATTEMPT]: ServerRestartAttemptPayload;
}

// ============================================================================
// TYPE ALIASES (For backward compatibility and ease of use)
// ============================================================================

export type ServerLogEvent = ServerLogPayload;
export type StateChangeEvent = ServerStateChangePayload;
export type HealthCheckEvent = ServerHealthPayload;
export type RequestEvent = ServerRequestPayload;

/**
 * Map IPC command names to their parameter and response types
 */
export interface IpcCommandMap {
  [IPC_COMMANDS.GET_SERVER_STATUS]: {
    params: Record<string, never>;
    response: ServerStatusResponse;
  };
  [IPC_COMMANDS.GET_RECENT_LOGS]: {
    params: GetRecentLogsParams;
    response: ServerLogPayload[];
  };
  [IPC_COMMANDS.RESTART_EMBEDDED_SERVER]: {
    params: RestartServerParams;
    response: RestartServerResponse;
  };
}

// ============================================================================
// VALIDATION CONSTANTS
// ============================================================================

/** Valid port range for embedded server */
export const PORT_RANGE = {
  MIN: 1906,
  MAX: 1915,
} as const;

/** Server startup timeout in milliseconds */
export const STARTUP_TIMEOUT_MS = 5000;

/** Maximum log buffer size for circular buffer */
export const MAX_LOG_BUFFER_SIZE = 1000;

/** Health check interval in milliseconds */
export const HEALTH_CHECK_INTERVAL_MS = 30000;

/** Maximum restart attempts */
export const MAX_RESTART_ATTEMPTS = 3;

/** Exponential backoff delays in milliseconds */
export const RESTART_DELAYS_MS = [1000, 2000, 4000] as const;

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

/**
 * Type guard to check if an object is a valid LogLevel.
 */
export function isLogLevel(value: unknown): value is LogLevel {
  return typeof value === 'string' &&
    ['error', 'warn', 'info', 'debug', 'trace', 'critical'].includes(value);
}

/**
 * Type guard to check if an object is a valid ServerState.
 */
export function isServerState(value: unknown): value is ServerState {
  return typeof value === 'string' &&
    ['idle', 'starting', 'port-discovery', 'binding', 'initializing', 'ready', 'running', 'crashed', 'failed', 'restarting', 'stopping', 'stopped'].includes(value);
}

/**
 * Parse timestamp string to Date object.
 * Handles ISO 8601 format from Rust chrono DateTime<Utc>.
 */
export function parseEventTimestamp(timestamp: string): Date {
  return new Date(timestamp);
}

/**
 * Format duration in milliseconds to human-readable string.
 */
export function formatDuration(durationMs: number): string {
  if (durationMs < 1000) {
    return `${durationMs}ms`;
  }
  if (durationMs < 60000) {
    return `${(durationMs / 1000).toFixed(2)}s`;
  }
  const minutes = Math.floor(durationMs / 60000);
  const seconds = ((durationMs % 60000) / 1000).toFixed(0);
  return `${minutes}m ${seconds}s`;
}

/**
 * Get display color for log level.
 */
export function getLogLevelColor(level: LogLevel): string {
  const colors: Record<LogLevel, string> = {
    trace: 'text-gray-400',
    debug: 'text-blue-400',
    info: 'text-green-400',
    warn: 'text-yellow-400',
    error: 'text-red-400',
    critical: 'text-purple-400',
  };
  return colors[level] || 'text-gray-400';
}

/**
 * Get display badge variant for log level.
 */
export function getLogLevelBadgeVariant(level: LogLevel): 'default' | 'secondary' | 'destructive' | 'outline' {
  const variants: Record<LogLevel, 'default' | 'secondary' | 'destructive' | 'outline'> = {
    trace: 'outline',
    debug: 'secondary',
    info: 'default',
    warn: 'outline',
    error: 'destructive',
    critical: 'destructive',
  };
  return variants[level] || 'default';
}
