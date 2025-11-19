"use client";

import { useEffect, useRef } from 'react';
import type { TaskEvent } from '../../shannon/types';
import { appStore } from '../../lib/store';

// Minimal adapter mapping Shannon SSE events to the ATC store model
// so the original canvas-based RadarCanvas can animate flights.

const DEFAULT_ESTIMATE_MS = 45000; // ~45s to center (slower for better tracking)

type Props = {
  events: TaskEvent[];
};

export function ShannonATCBridge({ events }: Props) {
  const processedRef = useRef<Set<string>>(new Set());
  const tickRef = useRef<number>(0);
  const timeoutRefs = useRef<Map<string, number>>(new Map());
  const initializedRef = useRef<boolean>(false);

  // Initialize store once
  useEffect(() => {
    if (initializedRef.current) return;
    initializedRef.current = true;

    // Clear any pending completion timers
    for (const t of timeoutRefs.current.values()) window.clearTimeout(t);
    timeoutRefs.current.clear();

    // Fresh snapshot for platform-wide monitoring
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
      seed: 'shannon-platform',
      running: true,
    });
    tickRef.current = 0;
  }, []);

  useEffect(() => {
    // Process only newly arrived events (by seq)
    for (const ev of events) {
      const workflowId = ev.workflow_id || 'unknown';
      const agentId = ev.agent_id || `agent-${ev.seq}`;
      const id = `${workflowId}::${agentId}`;

      // Deduplicate by stream_id when available, otherwise by composite key
      const key = ev.stream_id || `${workflowId}::${ev.seq}::${ev.type}::${agentId}`;
      if (processedRef.current.has(key)) continue;
      processedRef.current.add(key);

      const ts = ev.timestamp instanceof Date ? ev.timestamp.getTime() : Date.now();
      if (typeof window !== 'undefined') {
        // lightweight debug to verify mapping in the browser console
        console.debug('[ATC-Bridge] ingest', { seq: ev.seq, type: ev.type, workflow: workflowId, agent: agentId });
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
              agent_id: agentId,
              tps_min: 1,
              tps_max: 1,
              tps: 1,
              tokens_done: 0,
              est_tokens: 0,
            },
          ],
          agents: [
            { id, work_item_id: id, x: 0, y: 0, v: 0.0015, curve_phase: 0 }, // Slower velocity (0.002 -> 0.0015)
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
        // After a delay, flip to done and remove agent (increased delay for visibility)
        const handle = window.setTimeout(() => {
          const tick2 = ++tickRef.current;
          if (typeof window !== 'undefined') console.debug('[ATC-Bridge] mark done + remove', { id, tick: tick2 });
          appStore.getState().applyTick({
            tick_id: tick2,
            items: [{ id, status: 'done' }],
            agents_remove: [id],
          });
          timeoutRefs.current.delete(id);
        }, 1200); // Increased from 600ms to 1200ms for better visibility
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
        continue;
      }

      // Fallback: if we have never seen this item before and there's any event,
      // spawn a basic in-progress flight so the radar shows traffic for active workflows.
      const existing = appStore.getState().items[id];
      if (!existing) {
        const tick = ++tickRef.current;
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
              agent_id: agentId,
              tps_min: 1,
              tps_max: 1,
              tps: 1,
              tokens_done: 0,
              est_tokens: 0,
            },
          ],
          agents: [
            { id, work_item_id: id, x: 0, y: 0, v: 0.0015, curve_phase: 0 },
          ],
        });
      }
    }
  }, [events]);

  return null;
}
