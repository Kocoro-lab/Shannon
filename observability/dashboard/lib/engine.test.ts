import { describe, it, expect } from 'vitest';
import { buildItemsFromPlan, detectCycles, promoteQueuedToAssigned, computeMetrics } from './engine';
import type { PlanDefinition } from '@/plans/types';
import type { Agent, WorkItem } from './types';

describe('deps-resolver', () => {
  it('promotes items whose deps are satisfied', () => {
    const plan: PlanDefinition = {
      name: 'Test',
      items: [
        { id: 'A', group: 'X', sector: 'Planning', depends_on: [], estimate_ms: 1000, tps_min: 1, tps_max: 2 },
        { id: 'B', group: 'X', sector: 'Build', depends_on: ['A'], estimate_ms: 1000, tps_min: 1, tps_max: 2 },
      ],
    };
    const items = buildItemsFromPlan(plan);
    // Initially A has no deps -> should become assigned
    const first = promoteQueuedToAssigned(items);
    expect(first).toContain('A');
    expect(items['A'].status).toBe('assigned');
    expect(items['B'].status).toBe('queued');

    // Mark A done -> B becomes assigned
    items['A'].status = 'done';
    const second = promoteQueuedToAssigned(items);
    expect(second).toContain('B');
    expect(items['B'].status).toBe('assigned');
  });

  it('metrics-math computes totals and rates', () => {
    const plan: PlanDefinition = {
      name: 'Metrics',
      items: [
        { id: 'A', group: 'G', sector: 'Planning', depends_on: [], estimate_ms: 10000, tps_min: 2, tps_max: 4 },
        { id: 'B', group: 'G', sector: 'Build', depends_on: ['A'], estimate_ms: 5000, tps_min: 5, tps_max: 6 },
      ],
    };
    const items = buildItemsFromPlan(plan);
    // Make A done with tokens and B in_progress with current tps
    items['A'].status = 'done';
    items['A'].tokens_done = 100;
    items['B'].status = 'in_progress';
    items['B'].tps = 5;
    items['B'].tokens_done = 20;
    const agents: Record<string, Agent> = { AG1: { id: 'AG1', work_item_id: 'B', x: 0, y: 0, v: 0, curve_phase: 0 } };
    const m = computeMetrics(items as Record<string, WorkItem>, agents);
    expect(m.active_agents).toBe(1);
    expect(m.total_tokens).toBeCloseTo(120, 5);
    expect(m.live_tps).toBeCloseTo(5, 5);
    // Time-weighted completion across whole plan:
    // A contributes 100% of its 10s, B contributes (20/28) of its 5s
    // => (10s + 5s * 20/28) / (10s + 5s)
    const estTokensB = Math.round(((5 + 6) / 2) * (5000 / 1000));
    const expected = (10000 + 5000 * (20 / estTokensB)) / (10000 + 5000);
    expect(m.completion_rate).toBeCloseTo(expected, 5);
    expect(m.total_spend_usd).toBeGreaterThan(0);
    expect(m.live_spend_per_s).toBeGreaterThan(0);
  });

  it('detects cycles and leaves them queued', () => {
    const plan: PlanDefinition = {
      name: 'Cycle',
      items: [
        { id: 'A', group: 'X', sector: 'Planning', depends_on: ['B'], estimate_ms: 1000, tps_min: 1, tps_max: 2 },
        { id: 'B', group: 'X', sector: 'Build', depends_on: ['A'], estimate_ms: 1000, tps_min: 1, tps_max: 2 },
      ],
    };
    const items = buildItemsFromPlan(plan);
    const cycles = detectCycles(items);
    expect(cycles.length).toBeGreaterThan(0);
    const changed = promoteQueuedToAssigned(items);
    expect(changed.length).toBe(0);
    expect(items['A'].status).toBe('queued');
    expect(items['B'].status).toBe('queued');
  });
});
