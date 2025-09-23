// simBridge: UI-side transport boundary for simulation messages
// Hides transport (Worker now, WebSocket later) behind a small API.

import type { Agent, AppState, ProjectMetrics, WorkItem } from './types';
import { BRIDGE_BATCH_MS } from './config';
import { debugLog } from './debug';

export type SimMsg =
  | { type: 'snapshot'; state: AppState }
  | { type: 'deps_cleared'; id: string }
  | { type: 'start_item'; id: string; agent: Agent }
  | {
      type: 'tick';
      tick_id: number;
      items?: Partial<WorkItem>[];
      agents?: Partial<Agent>[];
      metrics?: Partial<ProjectMetrics>;
      agents_remove?: string[];
    }
  | { type: 'complete_item'; id: string };

export type SimIntent =
  | { type: 'set_running'; running: boolean }
  | { type: 'set_plan'; plan: string }
  | { type: 'set_seed'; seed: string }
  | { type: 'set_speed'; speed: number }
  | { type: 'request_snapshot' };

// Minimal message event/port interfaces so we don't depend on DOM types in tests
export interface MessageEventLike<T = unknown> { data: T }
export interface PortLike {
  postMessage: (msg: unknown) => void;
  addEventListener: (event: 'message', handler: (e: MessageEventLike) => void) => void;
  removeEventListener: (event: 'message', handler: (e: MessageEventLike) => void) => void;
}

export interface SimBridge {
  subscribe: (handler: (msg: SimMsg) => void) => () => void;
  postIntent: (intent: SimIntent) => void;
  getLastTickId: () => number;
  destroy: () => void;
}

export interface BridgeOptions {
  batchMs?: number; // coalesce outbound notifications to subscribers
}

export function createSimBridge(port: PortLike, opts: BridgeOptions = {}): SimBridge {
  const subscribers = new Set<(msg: SimMsg) => void>();
  let lastTickId = 0;
  const batchMs = opts.batchMs ?? BRIDGE_BATCH_MS;
  const queue: SimMsg[] = [];
  let flushTimer: ReturnType<typeof setTimeout> | null = null;

  const emit = (msg: SimMsg) => {
    for (const fn of subscribers) fn(msg);
  };

  const scheduleFlush = () => {
    if (flushTimer != null) return;
    if (batchMs <= 0) {
      flushNow();
      return;
    }
    flushTimer = setTimeout(flushNow, batchMs);
  };

  const flushNow = () => {
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = null;
    debugLog('bridge', 'flush', { queued: queue.length });
    while (queue.length) {
      const msg = queue.shift()!;
      if (msg.type === 'tick') {
        if (msg.tick_id <= lastTickId) {
          debugLog('bridge', 'drop-late', { tick_id: msg.tick_id, lastTickId });
          continue; // drop out-of-order or duplicate
        }
        lastTickId = msg.tick_id;
      }
      if (msg.type === 'snapshot') {
        // reset ordering on fresh snapshot
        lastTickId = 0;
      }
      debugLog('bridge', 'emit', { type: msg.type, lastTickId });
      emit(msg);
    }
  };

  const onMessage = (e: MessageEventLike) => {
    const raw = e?.data;
    if (!raw || typeof raw !== 'object' || !('type' in raw)) return;
    const msg = raw as SimMsg;
    if (msg.type === 'tick') {
      // Early drop to keep queue lean
      if (msg.tick_id <= lastTickId) {
        debugLog('bridge', 'drop-early', { tick_id: msg.tick_id, lastTickId });
        return;
      }
    } else if (msg.type === 'snapshot') {
      // Snapshot should be delivered promptly; reset tick ordering
      lastTickId = 0;
    }
    debugLog('bridge', 'queue', { type: msg.type, tick_id: msg.type === 'tick' ? msg.tick_id : undefined, size: queue.length + 1 });
    queue.push(msg);
    scheduleFlush();
  };

  port.addEventListener('message', onMessage);
  debugLog('bridge', 'created', { batchMs });

  return {
    subscribe(handler) {
      subscribers.add(handler);
      return () => subscribers.delete(handler);
    },
    postIntent(intent) {
      port.postMessage(intent);
    },
    getLastTickId() {
      return lastTickId;
    },
    destroy() {
      port.removeEventListener('message', onMessage);
      subscribers.clear();
      queue.length = 0;
      if (flushTimer) clearTimeout(flushTimer);
      flushTimer = null;
    },
  };
}

// Keep stub flag for existing smoke test
export const SIMBRIDGE_MODULE_LOADED = true;
