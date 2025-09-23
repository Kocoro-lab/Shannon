import { describe, it, expect } from 'vitest';
import { createAppStore } from './store';
import type { AppState } from './types';

describe('reducers-apply-snapshot-and-diff', () => {
  it('applies snapshot then tick diff and advances lastTickId', () => {
    const store = createAppStore();

    const snapshot: AppState = {
      items: {
        A1: {
          id: 'A1',
          group: 'A',
          sector: 'Planning',
          depends_on: [],
          estimate_ms: 10000,
          tps_min: 5,
          tps_max: 10,
          tps: 6,
          tokens_done: 0,
          est_tokens: 60,
          status: 'queued',
        },
        B1: {
          id: 'B1',
          group: 'B',
          sector: 'Build',
          depends_on: [],
          estimate_ms: 15000,
          tps_min: 7,
          tps_max: 12,
          tps: 8,
          tokens_done: 0,
          est_tokens: 100,
          status: 'assigned',
        },
      },
      agents: {},
      metrics: {
        active_agents: 0,
        total_tokens: 0,
        total_spend_usd: 0,
        live_tps: 0,
        live_spend_per_s: 0,
        completion_rate: 0,
      },
      seed: 's',
      running: true,
    };

    store.getState().applySnapshot(snapshot);

    // Apply a diff: update one item and metrics
    store.getState().applyTick({
      tick_id: 1,
      items: [
        { id: 'A1', status: 'in_progress', tps: 9, started_at: 123, eta_ms: 9000 },
      ],
      metrics: { active_agents: 1, live_tps: 9 },
    });

    const state = store.getState();
    expect(state.lastTickId).toBe(1);
    expect(state.items['A1'].status).toBe('in_progress');
    expect(state.items['A1'].tps).toBe(9);
    expect(state.metrics.active_agents).toBe(1);

    // Out-of-order tick should be ignored
    store.getState().applyTick({ tick_id: 1, metrics: { live_tps: 5 } });
    expect(store.getState().metrics.live_tps).toBe(9);
  });
});

