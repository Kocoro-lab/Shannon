"use client";

import { useEffect, useState, useCallback, useMemo } from 'react';
import {
  type ServerLogEvent,
  type StateChangeEvent,
  type RequestEvent,
  type HealthCheckEvent,
  type LogLevel,
  type Component,
  parseEventTimestamp,
} from './ipc-events';

/**
 * Maximum number of logs to keep in memory.
 * Prevents unbounded memory growth during long sessions.
 */
const MAX_LOGS = 1000;

/**
 * Maximum number of state changes to keep in history.
 */
const MAX_STATE_HISTORY = 100;

/**
 * Combined log entry that can represent any type of event.
 */
export interface LogEntry {
  /** Unique identifier for the log entry. */
  id: string;
  /** Event timestamp. */
  timestamp: Date;
  /** Log level (for server-log events). */
  level?: LogLevel;
  /** Component that generated the event. */
  component?: Component;
  /** Log message. */
  message: string;
  /** Event type discriminator. */
  type: 'log' | 'state' | 'request' | 'health';
  /** Original event data. */
  data: ServerLogEvent | StateChangeEvent | RequestEvent | HealthCheckEvent;
}

/**
 * Server logs hook return type.
 */
export interface UseServerLogsReturn {
  /** All log entries. */
  logs: LogEntry[];
  /** State change history. */
  stateHistory: StateChangeEvent[];
  /** Latest health check event. */
  latestHealth: HealthCheckEvent | null;
  /** Filter logs by level. */
  getLogsByLevel: (level: LogLevel) => LogEntry[];
  /** Filter logs by component. */
  getLogsByComponent: (component: Component) => LogEntry[];
  /** Search logs by text. */
  searchLogs: (query: string) => LogEntry[];
  /** Clear all logs. */
  clearLogs: () => void;
  /** Clear state history. */
  clearStateHistory: () => void;
  /** Get logs count by level. */
  getLogCountByLevel: () => Record<LogLevel, number>;
  /** Check if any logs are present. */
  hasLogs: boolean;
}

/**
 * Custom hook for managing server logs from IPC events.
 *
 * Listens to Tauri IPC events for server logs, state changes, request tracking,
 * and health checks. Provides filtering, searching, and management capabilities.
 *
 * @returns Server logs state and management functions.
 *
 * @example
 * ```tsx
 * function DebugConsole() {
 *   const { logs, getLogsByLevel, clearLogs } = useServerLogs();
 *   const errors = getLogsByLevel('error');
 *
 *   return (
 *     <div>
 *       <button onClick={clearLogs}>Clear</button>
 *       {logs.map(log => <LogItem key={log.id} log={log} />)}
 *     </div>
 *   );
 * }
 * ```
 */
export function useServerLogs(): UseServerLogsReturn {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [stateHistory, setStateHistory] = useState<StateChangeEvent[]>([]);
  const [latestHealth, setLatestHealth] = useState<HealthCheckEvent | null>(null);

  // Check if running in Tauri environment
  const isTauri = typeof window !== 'undefined' && '__TAURI__' in window;

  // Helper to generate unique IDs
  const generateId = useCallback(() => {
    return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
  }, []);

  // Add a log entry with automatic truncation
  const addLog = useCallback((entry: LogEntry) => {
    setLogs(prev => {
      const updated = [...prev, entry];
      // Keep only the most recent MAX_LOGS entries
      if (updated.length > MAX_LOGS) {
        return updated.slice(updated.length - MAX_LOGS);
      }
      return updated;
    });
  }, []);

  // Add a state change to history with automatic truncation
  const addStateChange = useCallback((event: StateChangeEvent) => {
    setStateHistory(prev => {
      const updated = [...prev, event];
      // Keep only the most recent MAX_STATE_HISTORY entries
      if (updated.length > MAX_STATE_HISTORY) {
        return updated.slice(updated.length - MAX_STATE_HISTORY);
      }
      return updated;
    });
  }, []);

  useEffect(() => {
    if (!isTauri) {
      return;
    }

    const unlisteners: Array<() => void> = [];

    const setupListeners = async () => {
      try {
        const { listen } = await import('@tauri-apps/api/event');
        const { invoke } = await import('@tauri-apps/api/core');

        // Fetch recent logs first
        try {
          const recentLogs = await invoke<ServerLogEvent[]>('get_recent_logs');
          console.log('[useServerLogs] Fetched recent logs:', recentLogs.length);

          recentLogs.forEach(payload => {
            addLog({
              id: generateId(),
              timestamp: parseEventTimestamp(payload.timestamp),
              level: payload.level,
              component: payload.component,
              message: payload.message,
              type: 'log',
              data: payload,
            });
          });
        } catch (err) {
          console.warn('[useServerLogs] Failed to fetch recent logs:', err);
        }

        // Listen for server-log events
        const unlistenLog = await listen<ServerLogEvent>('server-log', (event) => {
          const payload = event.payload;
          addLog({
            id: generateId(),
            timestamp: parseEventTimestamp(payload.timestamp),
            level: payload.level,
            component: payload.component,
            message: payload.message,
            type: 'log',
            data: payload,
          });
        });
        unlisteners.push(unlistenLog);

        // Listen for server-state-change events
        const unlistenState = await listen<StateChangeEvent>('server-state-change', (event) => {
          const payload = event.payload;
          addStateChange(payload);

          // Also add to main log for visibility
          const message = payload.error ? `State changed to: ${payload.to} (${payload.error})` : `State changed to: ${payload.to}`;
          addLog({
            id: generateId(),
            timestamp: parseEventTimestamp(payload.timestamp),
            level: 'info',
            component: 'embedded-api',
            message,
            type: 'state',
            data: payload,
          });
        });
        unlisteners.push(unlistenState);

        // Listen for server-request events
        const unlistenRequest = await listen<RequestEvent>('server-request', (event) => {
          const payload = event.payload;
          const message = payload.error
            ? `${payload.method} ${payload.path} - Failed: ${payload.error}`
            : `${payload.method} ${payload.path} - ${payload.status_code || 'Started'}`;

          const level: LogLevel = payload.error ? 'error' : 'debug';

          addLog({
            id: generateId(),
            timestamp: parseEventTimestamp(payload.timestamp),
            level,
            component: 'http-server',
            message,
            type: 'request',
            data: payload,
          });
        });
        unlisteners.push(unlistenRequest);

        // Listen for server-health events
        const unlistenHealth = await listen<HealthCheckEvent>('server-health', (event) => {
          const payload = event.payload;
          setLatestHealth(payload);

          const message = payload.status === 'healthy'
            ? `Health check passed`
            : 'Health check failed';

          addLog({
            id: generateId(),
            timestamp: parseEventTimestamp(payload.timestamp),
            level: payload.status === 'healthy' ? 'debug' : 'warn',
            component: 'system',
            message,
            type: 'health',
            data: payload,
          });
        });
        unlisteners.push(unlistenHealth);

        console.log('[useServerLogs] IPC listeners initialized');
      } catch (error) {
        console.error('[useServerLogs] Failed to setup IPC listeners:', error);
      }
    };

    setupListeners();

    // Cleanup
    return () => {
      unlisteners.forEach(unlisten => {
        unlisten();
      });
    };
  }, [isTauri, generateId, addLog, addStateChange]);

  // Filter logs by level
  const getLogsByLevel = useCallback((level: LogLevel): LogEntry[] => {
    return logs.filter(log => log.level === level);
  }, [logs]);

  // Filter logs by component
  const getLogsByComponent = useCallback((component: Component): LogEntry[] => {
    return logs.filter(log => log.component === component);
  }, [logs]);

  // Search logs by text (case-insensitive)
  const searchLogs = useCallback((query: string): LogEntry[] => {
    if (!query.trim()) {
      return logs;
    }

    const lowerQuery = query.toLowerCase();
    return logs.filter(log => {
      return (
        log.message.toLowerCase().includes(lowerQuery) ||
        log.component?.toLowerCase().includes(lowerQuery) ||
        log.level?.toLowerCase().includes(lowerQuery)
      );
    });
  }, [logs]);

  // Clear all logs
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  // Clear state history
  const clearStateHistory = useCallback(() => {
    setStateHistory([]);
  }, []);

  // Get log count by level
  const getLogCountByLevel = useCallback((): Record<LogLevel, number> => {
    const counts: Record<LogLevel, number> = {
      trace: 0,
      debug: 0,
      info: 0,
      warn: 0,
      error: 0,
      critical: 0,
    };

    logs.forEach(log => {
      if (log.level) {
        counts[log.level]++;
      }
    });

    return counts;
  }, [logs]);

  // Check if any logs are present
  const hasLogs = useMemo(() => logs.length > 0, [logs]);

  return {
    logs,
    stateHistory,
    latestHealth,
    getLogsByLevel,
    getLogsByComponent,
    searchLogs,
    clearLogs,
    clearStateHistory,
    getLogCountByLevel,
    hasLogs,
  };
}

/**
 * Hook to get only error and critical logs.
 * Useful for error monitoring components.
 */
export function useServerErrors(): LogEntry[] {
  const { logs } = useServerLogs();

  return useMemo(() => {
    return logs.filter(log => log.level === 'error' || log.level === 'critical');
  }, [logs]);
}

/**
 * Hook to get the latest state from state history.
 */
export function useLatestServerState(): StateChangeEvent | null {
  const { stateHistory } = useServerLogs();

  return useMemo(() => {
    if (stateHistory.length === 0) {
      return null;
    }
    return stateHistory[stateHistory.length - 1];
  }, [stateHistory]);
}
