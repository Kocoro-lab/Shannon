'use client';

import { useEffect, useRef, useState } from 'react';
import type { TaskEvent } from './types';
import { parseSSEEvent } from './events';

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const KNOWN_EVENT_TYPES = [
  // Core workflow events
  'WORKFLOW_STARTED',
  'WORKFLOW_COMPLETED',

  // Agent lifecycle
  'AGENT_STARTED',
  'AGENT_COMPLETED',
  'AGENT_THINKING',

  // Communication (P2P v1)
  'MESSAGE_SENT',
  'MESSAGE_RECEIVED',
  'WORKSPACE_UPDATED',

  // Team coordination
  'TEAM_RECRUITED',
  'TEAM_RETIRED',
  'ROLE_ASSIGNED',
  'TEAM_STATUS',
  'DELEGATION',
  'DEPENDENCY_SATISFIED',

  // Tool usage
  'TOOL_INVOKED',
  'TOOL_OBSERVATION',

  // LLM streaming (critical for token-by-token display)
  'LLM_PROMPT',
  'LLM_PARTIAL',
  'LLM_OUTPUT',

  // Progress & status
  'PROGRESS',
  'DATA_PROCESSING',
  'WAITING',
  'ERROR_RECOVERY',
  'ERROR_OCCURRED',

  // Stream lifecycle
  'STREAM_END',

  // Human approval
  'APPROVAL_REQUESTED',
  'APPROVAL_DECISION',
];

type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'error' | 'closed';

interface Options {
  maxEvents?: number;
  onEvent?: (event: TaskEvent) => void;
}

export function useSSE(workflowId: string, apiKey?: string, options?: Options) {
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [status, setStatus] = useState<ConnectionStatus>('idle');
  const [error, setError] = useState<Error | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const attemptsRef = useRef(0);
  const lastStreamIdRef = useRef<string | null>(null);
  const lastSeqRef = useRef<number>(0);
  const seenRef = useRef<Set<string>>(new Set());
  const lastWorkflowIdRef = useRef<string | null>(null);
  const maxEvents = options?.maxEvents ?? 200;
  const onEvent = options?.onEvent;
  const completedRef = useRef(false);

  useEffect(() => {
    if (!workflowId) {
      setStatus((prev) => (prev === 'idle' ? prev : 'idle'));
      setError(null);
      setEvents((prev) => (prev.length === 0 ? prev : []));
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      lastStreamIdRef.current = null;
      lastSeqRef.current = 0;
      seenRef.current.clear();
      completedRef.current = false;
      return;
    }
    // If workflow changed (new id), reset client-side state
    if (workflowId !== lastWorkflowIdRef.current) {
      lastWorkflowIdRef.current = workflowId;
      setEvents([]);
      seenRef.current.clear();
      lastStreamIdRef.current = null;
      lastSeqRef.current = 0;
      attemptsRef.current = 0;
      completedRef.current = false;
    }

    function open() {
      // ensure we don't carry completion state across opens
      completedRef.current = false;
      if (eventSourceRef.current) eventSourceRef.current.close();
      setStatus('connecting');
      setError(null);
      // Do not clear prior events to keep history visible across reconnects

      const params = new URLSearchParams({ workflow_id: workflowId });
      const lastId = lastStreamIdRef.current || (lastSeqRef.current > 0 ? String(lastSeqRef.current) : null);
      if (lastId) params.append('last_event_id', String(lastId));
      if (apiKey && process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH !== 'true') {
        params.append('api_key', apiKey);
      }
      const url = `${API_BASE_URL}/api/v1/stream/sse?${params.toString()}`;
      const source = new EventSource(url);
      eventSourceRef.current = source;

      const handleEvent = (e: MessageEvent) => {
        try {
          const event = parseSSEEvent(e.data);
          if (event.type === 'WORKFLOW_COMPLETED') {
            completedRef.current = true;
          }
          const key = event.stream_id || `${event.seq}-${event.type}-${event.agent_id || ''}`;
          if (!key) return;
          if (seenRef.current.has(key)) {
            return; // dedupe replayed events
          }
          seenRef.current.add(key);
          // Track last id for resume
          if (event.stream_id) lastStreamIdRef.current = event.stream_id;
          if (event.seq && event.seq > (lastSeqRef.current || 0)) lastSeqRef.current = event.seq;
          setEvents((prev) => {
            const next = [...prev, event];
            if (next.length > maxEvents) next.splice(0, next.length - maxEvents);
            return next;
          });
          onEvent?.(event);
          // If workflow completed, close gracefully so we don't show an error after end
          if (completedRef.current && eventSourceRef.current) {
            try { eventSourceRef.current.close(); } catch {}
            eventSourceRef.current = null;
            setStatus('closed');
          }
        } catch (err) {
          console.error('Failed to parse SSE event', err);
        }
      };

      source.onopen = () => {
        attemptsRef.current = 0; // reset backoff
        setStatus('connected');
      };

      source.onerror = (e) => {
        // After workflow completion, browsers often emit a terminal 'error' for closed streams.
        // Treat it as a normal close without noisy logs.
        if (completedRef.current) {
          setStatus('closed');
          return;
        }
        // Be quiet to avoid Next.js error overlay; use warn in dev only.
        if (process.env.NODE_ENV === 'development') {
          // eslint-disable-next-line no-console
          console.warn('[SSE] transient disconnect; attempting to reconnect', e);
        }
        setStatus('reconnecting');
        setError(null); // suppress red banner while reconnecting
        // Let EventSource auto-retry. Only reopen ourselves if CLOSED.
        const es = eventSourceRef.current as EventSource | null;
        // 2 === CLOSED per EventSource spec
        if (!es || (es as any).readyState === 2) {
          const attempt = attemptsRef.current + 1;
          attemptsRef.current = attempt;
          const delay = Math.min(15000, 1000 * Math.pow(2, attempt - 1));
          if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
          reconnectTimerRef.current = setTimeout(() => {
            if (eventSourceRef.current) {
              try { eventSourceRef.current.close(); } catch {}
              eventSourceRef.current = null;
            }
            open();
          }, delay);
        }
      };

      KNOWN_EVENT_TYPES.forEach((type) => {
        source.addEventListener(type, handleEvent as EventListener);
      });
      source.onmessage = handleEvent;
    }

    open();

    return () => {
      if (eventSourceRef.current) {
        KNOWN_EVENT_TYPES.forEach((type) => {
          eventSourceRef.current?.removeEventListener(type, (() => {}) as EventListener);
        });
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      setStatus('closed');
    };
  }, [workflowId, apiKey, maxEvents, onEvent]);

  return { events, status, error };
}
