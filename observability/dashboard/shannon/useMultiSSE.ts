'use client';

import { useEffect, useState, useRef, useCallback } from 'react';
import type { TaskEvent } from './types';
import { parseSSEEvent } from './events';

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const KNOWN_EVENT_TYPES = [
  'WORKFLOW_STARTED',
  'WORKFLOW_COMPLETED',
  'AGENT_STARTED',
  'AGENT_COMPLETED',
  'AGENT_THINKING',
  'MESSAGE_SENT',
  'MESSAGE_RECEIVED',
  'WORKSPACE_UPDATED',
  'TEAM_RECRUITED',
  'TEAM_RETIRED',
  'ROLE_ASSIGNED',
  'TEAM_STATUS',
  'DELEGATION',
  'DEPENDENCY_SATISFIED',
  'TOOL_INVOKED',
  'TOOL_OBSERVATION',
  'LLM_PROMPT',
  'LLM_PARTIAL',
  'LLM_OUTPUT',
  'PROGRESS',
  'DATA_PROCESSING',
  'WAITING',
  'ERROR_RECOVERY',
  'ERROR_OCCURRED',
  'STREAM_END',
  'APPROVAL_REQUESTED',
  'APPROVAL_DECISION',
];

type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'error';

interface WorkflowConnection {
  workflowId: string;
  eventSource: EventSource | null;
  status: ConnectionStatus;
  lastEventId: string | null;
  lastSeq: number;
  completed: boolean;
}

interface UseMultiSSEOptions {
  maxEventsPerWorkflow?: number;
  onEvent?: (event: TaskEvent) => void;
}

/**
 * Hook to manage multiple SSE streams for platform-wide monitoring.
 * Creates one EventSource per active workflow and aggregates all events.
 */
export function useMultiSSE(
  workflowIds: string[],
  apiKey?: string,
  options?: UseMultiSSEOptions
) {
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [status, setStatus] = useState<ConnectionStatus>('idle');
  const [error, setError] = useState<Error | null>(null);

  const connectionsRef = useRef<Map<string, WorkflowConnection>>(new Map());
  const seenEventsRef = useRef<Set<string>>(new Set());
  const maxEventsPerWorkflow = options?.maxEventsPerWorkflow ?? 200;
  const onEvent = options?.onEvent;

  const closeConnection = useCallback((workflowId: string) => {
    const conn = connectionsRef.current.get(workflowId);
    if (conn?.eventSource) {
      KNOWN_EVENT_TYPES.forEach((type) => {
        conn.eventSource?.removeEventListener(type, (() => {}) as EventListener);
      });
      conn.eventSource.close();
    }
    connectionsRef.current.delete(workflowId);
  }, []);

  const openConnection = useCallback(
    (workflowId: string) => {
      // Close existing connection if any (direct call, not via callback)
      const existingConn = connectionsRef.current.get(workflowId);
      if (existingConn?.eventSource) {
        KNOWN_EVENT_TYPES.forEach((type) => {
          existingConn.eventSource?.removeEventListener(type, (() => {}) as EventListener);
        });
        existingConn.eventSource.close();
      }

      const params = new URLSearchParams({ workflow_id: workflowId });
      if (apiKey && process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH !== 'true') {
        params.append('api_key', apiKey);
      }

      const url = `${API_BASE_URL}/api/v1/stream/sse?${params.toString()}`;
      const eventSource = new EventSource(url);

      const connection: WorkflowConnection = {
        workflowId,
        eventSource,
        status: 'connecting',
        lastEventId: null,
        lastSeq: 0,
        completed: false,
      };

      connectionsRef.current.set(workflowId, connection);

      const handleEvent = (e: MessageEvent) => {
        try {
          const event = parseSSEEvent(e.data);

          // Tag event with workflow_id to prevent collisions
          const taggedEvent: TaskEvent = {
            ...event,
            workflow_id: workflowId,
          };

          if (event.type === 'WORKFLOW_COMPLETED') {
            connection.completed = true;
          }

          // Create unique key for deduplication
          const eventKey = `${workflowId}::${event.seq}::${event.type}::${event.agent_id || ''}`;
          if (seenEventsRef.current.has(eventKey)) {
            return;
          }
          seenEventsRef.current.add(eventKey);

          // Track last event ID for potential resume
          if (event.stream_id) connection.lastEventId = event.stream_id;
          if (event.seq && event.seq > connection.lastSeq) {
            connection.lastSeq = event.seq;
          }

          // Add to aggregated events
          setEvents((prev) => {
            const next = [...prev, taggedEvent];
            // Sort by timestamp for timeline view
            next.sort((a, b) => {
              const timeA = a.timestamp instanceof Date ? a.timestamp.getTime() : 0;
              const timeB = b.timestamp instanceof Date ? b.timestamp.getTime() : 0;
              return timeA - timeB;
            });
            // Keep reasonable limit based on active connections
            const activeCount = connectionsRef.current.size || 1;
            const maxTotal = maxEventsPerWorkflow * activeCount;
            if (next.length > maxTotal) {
              next.splice(0, next.length - maxTotal);
            }
            return next;
          });

          onEvent?.(taggedEvent);

          // Close gracefully after workflow completion
          if (connection.completed) {
            setTimeout(() => closeConnection(workflowId), 1000);
          }
        } catch (err) {
          console.error(`[MultiSSE] Failed to parse event for ${workflowId}:`, err);
        }
      };

      eventSource.onopen = () => {
        connection.status = 'connected';
        setStatus('connected');
        setError(null);
      };

      eventSource.onerror = (e) => {
        if (connection.completed) {
          closeConnection(workflowId);
          return;
        }

        connection.status = 'error';
        setError(new Error(`SSE error for workflow ${workflowId}`));

        // Auto-reconnect after delay
        setTimeout(() => {
          if (connectionsRef.current.has(workflowId)) {
            openConnection(workflowId);
          }
        }, 3000);
      };

      KNOWN_EVENT_TYPES.forEach((type) => {
        eventSource.addEventListener(type, handleEvent as EventListener);
      });
      eventSource.onmessage = handleEvent;
    },
    [apiKey, maxEventsPerWorkflow, onEvent]
  );

  useEffect(() => {
    const closeById = (workflowId: string) => {
      const conn = connectionsRef.current.get(workflowId);
      if (conn?.eventSource) {
        KNOWN_EVENT_TYPES.forEach((type) => {
          conn.eventSource?.removeEventListener(type, (() => {}) as EventListener);
        });
        conn.eventSource.close();
      }
      connectionsRef.current.delete(workflowId);
    };

    console.log(`[useMultiSSE] Effect triggered with ${workflowIds.length} workflow IDs:`, workflowIds);

    if (workflowIds.length === 0) {
      console.log('[useMultiSSE] No workflows - closing all connections');
      // Close all connections
      connectionsRef.current.forEach((_, id) => closeById(id));
      setStatus('idle');
      return;
    }

    // Get current workflow IDs
    const currentIds = new Set(workflowIds);
    const existingIds = new Set(connectionsRef.current.keys());

    // Close connections for workflows no longer active
    existingIds.forEach((id) => {
      if (!currentIds.has(id)) {
        console.log(`[useMultiSSE] Closing connection for removed workflow: ${id}`);
        closeById(id);
      }
    });

    // Open connections for new workflows
    currentIds.forEach((id) => {
      if (!existingIds.has(id)) {
        console.log(`[useMultiSSE] Opening connection for new workflow: ${id}`);
        openConnection(id);
      }
    });

    console.log(`[useMultiSSE] Active connections: ${connectionsRef.current.size}`);
    setStatus(connectionsRef.current.size > 0 ? 'connected' : 'idle');
  }, [workflowIds, openConnection]);

  useEffect(() => {
    // Cleanup on unmount
    return () => {
      connectionsRef.current.forEach((conn) => {
        if (conn?.eventSource) {
          KNOWN_EVENT_TYPES.forEach((type) => {
            conn.eventSource?.removeEventListener(type, (() => {}) as EventListener);
          });
          conn.eventSource.close();
        }
      });
      connectionsRef.current.clear();
    };
  }, []);

  return {
    events,
    status,
    error,
    activeConnections: connectionsRef.current.size,
  };
}
