'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, type ReactNode } from 'react';
import type { TaskEvent, TaskStatusResponse } from './types';
import { fetchTaskStatus } from './api';

type FlightStatus = 'queued' | 'active' | 'completed' | 'error';

export interface FlightSummary {
  id: string;
  agentId: string;
  title: string;
  status: FlightStatus;
  sector: string;
  lastMessage: string;
  lastEventType: string;
  lastUpdated: number;
  startedAt?: number;
  seq: number;
}

interface DashboardMetrics {
  totalEvents: number;
  errorEvents: number;
  startedAt?: number;
  lastEventAt?: number;
}

interface DashboardState {
  flights: Record<string, FlightSummary>;
  metrics: DashboardMetrics;
  lastEvent?: TaskEvent;
  toolCounts: Record<string, number>;
  agentTimers: Record<string, { lastPhase: 'thinking' | 'execution' | null; lastTimestamp?: number; thinkingMs: number; executionMs: number }>;
  completedWorkflows: Set<string>;
}

type Action =
  | { type: 'RESET' }
  | { type: 'INGEST_EVENT'; event: TaskEvent }
  | { type: 'MARK_WORKFLOW_COMPLETED'; workflowId: string };

const initialState: DashboardState = {
  flights: {},
  metrics: {
    totalEvents: 0,
    errorEvents: 0,
  },
  toolCounts: {},
  agentTimers: {},
  completedWorkflows: new Set(),
};

function toMillis(input: Date | string | number | undefined): number | undefined {
  if (!input) return undefined;
  if (input instanceof Date) return input.getTime();
  if (typeof input === 'number') return input;
  const parsed = Date.parse(input);
  return Number.isNaN(parsed) ? undefined : parsed;
}

const DEFAULT_SECTOR = 'PLANNING';

function eventToSector(type: string): string {
  switch (type) {
    case 'AGENT_STARTED':
    case 'AGENT_THINKING':
    case 'MESSAGE_SENT':
      return 'PLANNING';
    case 'TOOL_INVOKED':
    case 'MESSAGE_RECEIVED':
      return 'BUILD';
    case 'TOOL_COMPLETED':
    case 'WORKSPACE_UPDATED':
      return 'EVAL';
    case 'AGENT_COMPLETED':
    case 'WORKFLOW_COMPLETED':
      return 'DEPLOY';
    case 'ERROR_OCCURRED':
      return 'OPERATIONS';
    default:
      return DEFAULT_SECTOR;
  }
}

function deriveStatus(type: string, current: FlightStatus): FlightStatus {
  switch (type) {
    case 'AGENT_STARTED':
    case 'AGENT_THINKING':
    case 'MESSAGE_SENT':
    case 'TOOL_INVOKED':
    case 'MESSAGE_RECEIVED':
      return 'active';
    case 'AGENT_COMPLETED':
    case 'TOOL_COMPLETED':
    case 'WORKFLOW_COMPLETED':
      return 'completed';
    case 'ERROR_OCCURRED':
      return 'error';
    default:
      return current;
  }
}

function eventPhase(type: string): 'thinking' | 'execution' | null {
  switch (type) {
    // Execution phase events
    case 'TOOL_INVOKED':
    case 'WORKSPACE_UPDATED':
    case 'MESSAGE_RECEIVED':
    case 'DELEGATION':
      return 'execution';
    // Thinking phase events
    case 'TOOL_COMPLETED':
    case 'AGENT_STARTED':
    case 'AGENT_THINKING':
    case 'MESSAGE_SENT':
      return 'thinking';
    // Workflow events don't count toward timing
    case 'WORKFLOW_STARTED':
    case 'WORKFLOW_COMPLETED':
    case 'AGENT_COMPLETED':
      return null;
    default:
      return 'execution'; // Default to execution for unknown events
  }
}

function parseToolName(ev: TaskEvent): string | undefined {
  const s = (ev.formatted || ev.message || '').trim();
  if (!s) return undefined;
  // Common patterns
  const m1 = s.match(/tool\s*[:=]\s*([A-Za-z0-9_.-]+)/i);
  if (m1 && m1[1]) return m1[1];
  const m2 = s.match(/\"tool\"\s*:\s*\"([^\"]+)\"/i);
  if (m2 && m2[1]) return m2[1];
  const m3 = s.match(/invoked\s+([A-Za-z0-9_.-]+)/i);
  if (m3 && m3[1]) return m3[1];
  const m4 = s.match(/routing\s+to\s+(?:tool\s+)?([A-Za-z0-9_.-]+)/i);
  if (m4 && m4[1]) return m4[1];
  // Explicit web_search mention
  const m5 = s.match(/\b(web[_-]?search)\b/i);
  if (m5 && m5[1]) return m5[1].toLowerCase();
  return undefined;
}

function extractToolsFromFinal(resp: any): Record<string, number> {
  const out: Record<string, number> = {};
  if (!resp || typeof resp !== 'object') return out;
  // tools_used: ["web_search", ...] or [{name,count}]
  const candidates = (resp.tools_used || resp.tools || resp.tool_usages) as any;
  if (Array.isArray(candidates)) {
    for (const t of candidates) {
      if (typeof t === 'string') out[t] = (out[t] || 0) + 1;
      else if (t && typeof t.name === 'string') out[t.name] = (out[t.name] || 0) + (Number(t.count) || 1);
    }
  } else if (candidates && typeof candidates === 'object') {
    for (const [k, v] of Object.entries(candidates)) out[k] = (out[k] || 0) + (Number(v) || 1);
  }
  return out;
}

function reducer(state: DashboardState, action: Action): DashboardState {
  switch (action.type) {
    case 'RESET':
      return {
        flights: {},
        metrics: {
          totalEvents: 0,
          errorEvents: 0,
          startedAt: Date.now(),
        },
        lastEvent: undefined,
        toolCounts: {},
        agentTimers: {},
        completedWorkflows: new Set(),
      };
    case 'INGEST_EVENT': {
      const { event } = action;
      const eventTime = toMillis(event.timestamp) ?? Date.now();
      const metrics: DashboardMetrics = {
        ...state.metrics,
        totalEvents: state.metrics.totalEvents + 1,
        lastEventAt: eventTime,
      };
      if (!metrics.startedAt || event.type === 'WORKFLOW_STARTED') {
        metrics.startedAt = eventTime;
      }
      if (event.type === 'ERROR_OCCURRED') {
        metrics.errorEvents = state.metrics.errorEvents + 1;
      }

      // Mark workflow as completed
      const completedWorkflows = new Set(state.completedWorkflows);
      if (event.type === 'WORKFLOW_COMPLETED' && event.workflow_id) {
        completedWorkflows.add(event.workflow_id);
      }

      if (!event.agent_id) {
        return {
          ...state,
          metrics,
          lastEvent: event,
          completedWorkflows,
        };
      }

      const flights = { ...state.flights };
      // Use composite key: workflow_id::agent_id to prevent collisions
      const workflowId = event.workflow_id || 'unknown';
      const key = `${workflowId}::${event.agent_id}`;
      const existing = flights[key];
      const previousStatus = existing?.status ?? 'queued';
      const nextStatus = deriveStatus(event.type, previousStatus);
      const sector = existing?.sector || eventToSector(event.type);
      const startedAt = existing?.startedAt ?? (nextStatus === 'active' ? eventTime : undefined);

      flights[key] = {
        id: key,
        agentId: event.agent_id,
        title: event.agent_id,
        status: nextStatus,
        sector,
        lastMessage: event.formatted || event.message || event.type,
        lastEventType: event.type,
        lastUpdated: eventTime,
        startedAt,
        seq: event.seq,
      };

      // Update tool usage and timing
      const toolCounts = { ...state.toolCounts };
      {
        const name = parseToolName(event);
        if (name) toolCounts[name] = (toolCounts[name] || 0) + 1;
      }

      const agentTimers = { ...state.agentTimers };
      const phase = eventPhase(event.type);
      const t = agentTimers[key] || { lastPhase: null as any, lastTimestamp: undefined, thinkingMs: 0, executionMs: 0 };
      if (typeof t.lastTimestamp === 'number' && t.lastPhase) {
        const dt = Math.max(0, eventTime - t.lastTimestamp);
        if (t.lastPhase === 'thinking') t.thinkingMs += dt;
        else if (t.lastPhase === 'execution') t.executionMs += dt;
      }
      if (phase) t.lastPhase = phase;
      t.lastTimestamp = eventTime;
      agentTimers[key] = t;

      return {
        ...state,
        flights,
        metrics,
        lastEvent: event,
        toolCounts,
        agentTimers,
        completedWorkflows,
      };
    }
    case 'MARK_WORKFLOW_COMPLETED': {
      const completedWorkflows = new Set(state.completedWorkflows);
      completedWorkflows.add(action.workflowId);
      return {
        ...state,
        completedWorkflows,
      };
    }
    default:
      return state;
  }
}

interface DashboardContextValue {
  state: DashboardState;
  reset: () => void;
  ingestEvent: (event: TaskEvent) => void;
}

const DashboardContext = createContext<DashboardContextValue | undefined>(undefined);

export function DashboardProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);

  const reset = useCallback(() => {
    dispatch({ type: 'RESET' });
  }, []);

  const ingestEvent = useCallback((event: TaskEvent) => {
    dispatch({ type: 'INGEST_EVENT', event });
  }, []);

  const value = useMemo(() => ({ state, reset, ingestEvent }), [state, reset, ingestEvent]);

  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>;
}

export function useDashboardContext() {
  const ctx = useContext(DashboardContext);
  if (!ctx) {
    throw new Error('useDashboardContext must be used within DashboardProvider');
  }
  return ctx;
}

export function useFlights(): FlightSummary[] {
  const { state } = useDashboardContext();
  return useMemo(() => Object.values(state.flights).sort((a, b) => a.seq - b.seq), [state.flights]);
}

export function useDashboardMetrics() {
  const { state } = useDashboardContext();
  return useMemo(() => {
    const flights = Object.values(state.flights);
    const activeCount = flights.filter((f) => f.status === 'active').length;
    const completedCount = flights.filter((f) => f.status === 'completed').length;
    const errorCount = flights.filter((f) => f.status === 'error').length;
    const totalFlights = flights.length;

    // Calculate completion rate based on workflow completion
    let completionRate = 0;
    if (state.lastEvent?.type === 'WORKFLOW_COMPLETED') {
      completionRate = 1.0; // 100% when workflow is completed
    } else if (state.lastEvent?.type === 'ERROR_OCCURRED') {
      completionRate = 0; // 0% on error
    } else if (totalFlights > 0) {
      // Otherwise, estimate based on completed agents
      completionRate = completedCount / Math.max(totalFlights, 1);
    }

    // Aggregate timers
    let thinkingMs = 0;
    let executionMs = 0;
    for (const t of Object.values(state.agentTimers)) {
      thinkingMs += t.thinkingMs;
      executionMs += t.executionMs;
    }

    return {
      metrics: state.metrics,
      totals: {
        activeCount,
        completedCount,
        errorCount,
        totalFlights,
        completionRate,
      },
      tools: state.toolCounts,
      timing: { thinkingMs, executionMs },
      finalStatus: state.finalStatus,
      lastEvent: state.lastEvent,
    };
  }, [state.flights, state.metrics, state.lastEvent, state.toolCounts, state.agentTimers, state.finalStatus]);
}
