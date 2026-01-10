"use client";

import React, { createContext, useContext, useState, useEffect, useCallback, useRef, ReactNode } from 'react';
import type {
  ServerLogEvent,
  StateChangeEvent,
  LifecyclePhase
} from './ipc-events';
import { parseEventTimestamp } from './ipc-events';

export type ServerStatus = 'initializing' | 'starting' | 'ready' | 'failed' | 'stopping' | 'stopped' | 'unknown';

export interface ServerState {
  status: ServerStatus;
  url: string | null;
  port: number | null;
  error: string | null;
  lastChecked: Date | null;
}

interface ServerContextValue extends ServerState {
  isReady: boolean;
  isTauri: boolean;
  updateStatus: (status: ServerStatus, url?: string, port?: number, error?: string) => void;
  logs: ServerLogEvent[];
  stateHistory: StateChangeEvent[];
}

const ServerContext = createContext<ServerContextValue | undefined>(undefined);

/**
 * Maximum number of logs to keep in memory.
 */
const MAX_LOGS = 1000;

/**
 * Maximum number of state changes to keep in history.
 */
const MAX_STATE_HISTORY = 100;

/**
 * Map lifecycle phases from IPC events to ServerStatus.
 */
function mapLifecycleToStatus(phase: LifecyclePhase): ServerStatus {
  const mapping: Record<LifecyclePhase, ServerStatus> = {
    // Map ServerState values to ServerStatus
    'idle': 'unknown',
    'starting': 'starting',
    'port-discovery': 'initializing',
    'binding': 'initializing',
    'initializing': 'initializing',
    'ready': 'ready',
    'running': 'ready',
    'restarting': 'starting',
    'stopping': 'stopping',
    'stopped': 'stopped',
    'crashed': 'failed',
    'failed': 'failed',
  };
  return mapping[phase] || 'unknown';
}

export function ServerProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<ServerState>({
    status: 'initializing',
    url: null,
    port: null,
    error: null,
    lastChecked: null,
  });

  const [logs, setLogs] = useState<ServerLogEvent[]>([]);
  const [stateHistory, setStateHistory] = useState<StateChangeEvent[]>([]);

  const isTauri = typeof window !== 'undefined' && '__TAURI__' in window;

  // Use ref to track status without causing re-renders
  const statusRef = useRef<ServerStatus>('initializing');

  const updateStatus = useCallback((status: ServerStatus, url?: string, port?: number, error?: string) => {
    statusRef.current = status;
    setState(prev => ({
      ...prev,
      status,
      url: url || prev.url,
      port: port || prev.port,
      error: error || null,
      lastChecked: new Date(),
    }));
  }, []);

  useEffect(() => {
    // If not in Tauri, assume external server is available
    if (!isTauri) {
      const externalUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
      updateStatus('ready', externalUrl);
      return;
    }

    // In Tauri mode, listen for server events
    console.log('[ServerContext] Initializing Tauri server listener...');
    updateStatus('starting');

    let unlisten: (() => void) | undefined;
    let healthCheckInterval: NodeJS.Timeout | undefined;
    let startupTimeout: NodeJS.Timeout | undefined;

    const initializeTauriListeners = async () => {
      try {
        const { listen } = await import('@tauri-apps/api/event');
        const { invoke } = await import('@tauri-apps/api/core');

        const unlisteners: Array<() => void> = [];

        // Helper to handle successful connection
        const handleServerReady = (url: string, port?: number) => {
          console.log(`[ServerContext] ✅ Server ready at ${url}`);
          window.__SHANNON_API_URL = url;
          updateStatus('ready', url, port);

          if (startupTimeout) {
            clearTimeout(startupTimeout);
            startupTimeout = undefined;
          }
          if (startupInterval) {
            clearInterval(startupInterval);
            startupInterval = undefined;
          }
        };

        // STEP 1: Proactive health check on likely ports (1906-1915)
        // This eliminates race conditions where IPC events are sent before listeners are ready
        console.log('[ServerContext] 🔍 Checking for existing server on ports 1906-1915...');

        for (let port = 1906; port <= 1915; port++) {
          try {
            const url = `http://localhost:${port}`;
            const response = await fetch(`${url}/health`, {
              method: 'GET',
              cache: 'no-store',
              signal: AbortSignal.timeout(1000), // 1 second timeout per port
            });

            if (response.ok) {
              console.log(`[ServerContext] ✅ Found running server on port ${port}`);
              handleServerReady(url, port);
              return; // Server found, no need to set up IPC listeners
            }
          } catch (e) {
            // Port not available or server not running, continue to next port
            console.debug(`[ServerContext] Port ${port} not available:`, e);
          }
        }

        console.log('[ServerContext] 🔄 No existing server found, setting up IPC listeners...');

        // Listen for server-ready event
        const unlistenReady = await listen<{ url: string; port: number }>('server-ready', (event) => {
          const { url, port } = event.payload;
          console.log(`[ServerContext] Received server-ready event: ${url}`);
          handleServerReady(url, port);
        });
        unlisteners.push(unlistenReady);

        // Listen for server-log events
        const unlistenLog = await listen<ServerLogEvent>('server-log', (event) => {
          const logEvent = event.payload;
          setLogs(prev => {
            const updated = [...prev, logEvent];
            // Keep only the most recent MAX_LOGS entries
            if (updated.length > MAX_LOGS) {
              return updated.slice(updated.length - MAX_LOGS);
            }
            return updated;
          });
        });
        unlisteners.push(unlistenLog);

        // Listen for server-state-change events
        const unlistenState = await listen<StateChangeEvent>('server-state-change', (event) => {
          const stateEvent = event.payload;

          // Add to state history
          setStateHistory(prev => {
            const updated = [...prev, stateEvent];
            // Keep only the most recent MAX_STATE_HISTORY entries
            if (updated.length > MAX_STATE_HISTORY) {
              return updated.slice(updated.length - MAX_STATE_HISTORY);
            }
            return updated;
          });

          // Update server status based on lifecycle phase
          const newStatus = mapLifecycleToStatus(stateEvent.to);
          console.log(`[ServerContext] State change: ${stateEvent.from || 'none'} -> ${stateEvent.to} (status: ${newStatus})`);

          if (stateEvent.to === 'ready' && stateEvent.port) {
            const url = `http://localhost:${stateEvent.port}`;
            updateStatus(newStatus, url, stateEvent.port, stateEvent.error || undefined);
          } else if (stateEvent.to === 'failed') {
            updateStatus(newStatus, undefined, undefined, stateEvent.error || 'Server failed');
          } else {
            updateStatus(newStatus, undefined, undefined, stateEvent.error || undefined);
          }
        });
        unlisteners.push(unlistenState);

        // Store cleanup function
        unlisten = () => {
          unlisteners.forEach(fn => {
            fn();
          });
        };

        // Set a timeout for server startup (30 seconds)
        startupTimeout = setTimeout(() => {
          if (statusRef.current !== 'ready') {
            console.error('[ServerContext] ❌ Server startup timeout');
            updateStatus('failed', undefined, undefined, 'Server failed to start within 30 seconds');
            if (startupInterval) {
              clearInterval(startupInterval);
            }
          }
        }, 30000);

        // Polling loop to check for server readiness (robustness for missed events/startup race)
        let startupInterval: NodeJS.Timeout | undefined = setInterval(async () => {
          if (statusRef.current === 'ready') {
            if (startupInterval) clearInterval(startupInterval);
            return;
          }

          try {
            const url = await invoke<string | null>('get_embedded_api_url');
            const isRunning = await invoke<boolean>('is_embedded_api_running');

            if (url && isRunning) {
              console.log('[ServerContext] Poll detected server running at', url);
              handleServerReady(url);
            } else {
                // Determine port from URL if possible, or just log
                console.debug('[ServerContext] Polling... server not ready yet (url:', url, 'running:', isRunning, ')');
            }
          } catch (e) {
            console.debug('[ServerContext] Poll failed:', e);
          }
        }, 1000);

        // Initial check
        try {
          const url = await invoke<string | null>('get_embedded_api_url');
          const isRunning = await invoke<boolean>('is_embedded_api_running');

          if (url && isRunning) {
            console.log('[ServerContext] ✅ Server already running at (initial check)', url);
            handleServerReady(url);
          }
        } catch (e) {
          console.log('[ServerContext] Server not yet running, waiting for server-ready event...');
        }

        // Health check every 10 seconds once ready
        healthCheckInterval = setInterval(async () => {
          try {
            const isRunning = await invoke<boolean>('is_embedded_api_running');
            const url = await invoke<string | null>('get_embedded_api_url');

            if (isRunning && url) {
              try {
                const response = await fetch(`${url}/health`, {
                  method: 'GET',
                  cache: 'no-store',
                });

                if (!response.ok) {
                  console.warn('[ServerContext] ⚠️ Health check failed');
                  updateStatus('failed', undefined, undefined, 'Server health check failed');
                }
              } catch (e) {
                console.warn('[ServerContext] ⚠️ Health check error:', e);
                updateStatus('failed', undefined, undefined, 'Cannot reach server');
              }
            }
          } catch (e) {
            // Invoke failed, likely server not ready yet
            console.debug('[ServerContext] Health check skipped - server not ready');
          }
        }, 10000);

        console.log('[ServerContext] ✓ Tauri listeners initialized');
      } catch (error) {
        console.error('[ServerContext] ❌ Failed to initialize Tauri listeners:', error);
        updateStatus('failed', undefined, undefined, `Failed to initialize: ${error}`);
      }
    };

    initializeTauriListeners();

    // Cleanup
    return () => {
      if (unlisten) {
        unlisten();
      }
      if (healthCheckInterval) {
        clearInterval(healthCheckInterval);
      }
      if (startupTimeout) {
        clearTimeout(startupTimeout);
      }
    };
  }, [isTauri, updateStatus]); // Only re-run if isTauri or updateStatus changes

  const contextValue: ServerContextValue = {
    ...state,
    isReady: state.status === 'ready',
    isTauri,
    updateStatus,
    logs,
    stateHistory,
  };

  return (
    <ServerContext.Provider value={contextValue}>
      {children}
    </ServerContext.Provider>
  );
}

export function useServer(): ServerContextValue {
  const context = useContext(ServerContext);
  if (context === undefined) {
    throw new Error('useServer must be used within a ServerProvider');
  }
  return context;
}

export function useServerUrl(): string {
  const { url, isTauri } = useServer();

  if (!isTauri) {
    // Web mode: use environment variable or default
    return process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
  }

  // In Tauri mode, always use the URL from the server-ready event
  // If not set yet, return empty string to prevent premature API calls
  return url || "";
}
