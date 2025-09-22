"use client";

import { useEffect, useRef } from 'react';
import type { TaskEvent } from '../../shannon/types';
import { appStore } from '../../lib/store';

// Minimal adapter mapping Shannon SSE events to the ATC store model
// so the original canvas-based RadarCanvas can animate flights.

const DEFAULT_ESTIMATE_MS = 25000; // ~25s to center

type Props = {
  workflowId: string;
  events: TaskEvent[];
};

export function ShannonATCBridge({ workflowId, events }: Props) {
  const lastSeqRef = useRef<number>(-1);
  const tickRef = useRef<number>(0);
  const timeoutRefs = useRef<Map<string, number>>(new Map());
  const currentWfRef = useRef<string | null>(null);

  // Reset store on workflow change
  useEffect(() => {
    if (!workflowId) return;
    if (currentWfRef.current === workflowId) return;
    currentWfRef.current = workflowId;
    // Clear any pending completion timers
    for (const t of timeoutRefs.current.values()) window.clearTimeout(t);
    timeoutRefs.current.clear();
    // Fresh snapshot
    appStore.getState().applySnapshot({
      items: {},
      agents: {},
      metrics: {
        active_agents: 0,
        total_tokens: 0,
        total_spend_usd: 0,
        live_tps: 0,
        live_spend_per_s: 0,
        completion_rate: 0,
      },
      seed: 'shannon',
      running: true,
    });
    lastSeqRef.current = -1;
    tickRef.current = 0;
  }, [workflowId]);

  useEffect(() => {
    // Process only newly arrived events (by seq)
    for (const ev of events) {
      if (ev.seq <= lastSeqRef.current) continue;
      lastSeqRef.current = ev.seq;
      const id = ev.agent_id || `agent-${ev.seq}`;
      const ts = ev.timestamp instanceof Date ? ev.timestamp.getTime() : Date.now();
      if (typeof window !== 'undefined') {
        // lightweight debug to verify mapping in the browser console
        console.debug('[ATC-Bridge] ingest', { seq: ev.seq, type: ev.type, agent: id });
      }

      const startLike = new Set([
        'AGENT_STARTED',
        'AGENT_THINKING',
        'MESSAGE_SENT',
        'TOOL_INVOKED',
        'MESSAGE_RECEIVED',
        'ROLE_ASSIGNED',
      ]);
      const doneLike = new Set(['AGENT_COMPLETED', 'TOOL_COMPLETED']);

      // Start or update in-progress flight
      if (startLike.has(ev.type)) {
        const tick = ++tickRef.current;
        if (typeof window !== 'undefined') console.debug('[ATC-Bridge] start/in_progress', { id, tick });
        appStore.getState().applyTick({
          tick_id: tick,
          items: [
            {
              id,
              group: 'A',
              sector: 'PLANNING',
              depends_on: [],
              estimate_ms: DEFAULT_ESTIMATE_MS,
              started_at: ts,
              status: 'in_progress',
              tps_min: 1,
              tps_max: 1,
              tps: 1,
              tokens_done: 0,
              est_tokens: 0,
            },
          ],
          agents: [
            { id, work_item_id: id, x: 0, y: 0, v: 0.002, curve_phase: 0 },
          ],
        });
        continue;
      }

      // Completion: snap to center, then mark done shortly after so pulse renders
      if (doneLike.has(ev.type)) {
        const tick1 = ++tickRef.current;
        if (typeof window !== 'undefined') console.debug('[ATC-Bridge] complete->arrive center', { id, tick: tick1 });
        appStore.getState().applyTick({
          tick_id: tick1,
          items: [
            {
              id,
              estimate_ms: DEFAULT_ESTIMATE_MS,
              started_at: ts - DEFAULT_ESTIMATE_MS,
              eta_ms: 0,
              status: 'in_progress',
            },
          ],
          agents: [{ id, work_item_id: id }],
        });
        // After a short delay, flip to done and remove agent
        const handle = window.setTimeout(() => {
          const tick2 = ++tickRef.current;
          if (typeof window !== 'undefined') console.debug('[ATC-Bridge] mark done + remove', { id, tick: tick2 });
          appStore.getState().applyTick({
            tick_id: tick2,
            items: [{ id, status: 'done' }],
            agents_remove: [id],
          });
          timeoutRefs.current.delete(id);
        }, 600);
        // Track timeout to clear on reset
        const prev = timeoutRefs.current.get(id);
        if (prev) window.clearTimeout(prev);
        timeoutRefs.current.set(id, handle);
        continue;
      }

      // Errors => blocked; remove agent
      if (ev.type === 'ERROR_OCCURRED') {
        const tick = ++tickRef.current;
        if (typeof window !== 'undefined') console.debug('[ATC-Bridge] error->blocked', { id, tick });
        appStore.getState().applyTick({
          tick_id: tick,
          items: [{ id, status: 'blocked' }],
          agents_remove: [id],
        });
      }
    }
  }, [events]);

  return null;
}
