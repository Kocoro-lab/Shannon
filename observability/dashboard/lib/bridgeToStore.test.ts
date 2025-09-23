import { describe, it, expect } from 'vitest';
import { attachBridgeToStore } from './bridgeToStore';
import { createAppStore } from './store';
import type { SimMsg } from './simBridge';

class FakeBridge {
  private subs = new Set<(msg: SimMsg) => void>();
  subscribe(handler: (msg: SimMsg) => void) {
    this.subs.add(handler);
    return () => this.subs.delete(handler);
  }
  emit(msg: SimMsg) {
    for (const h of this.subs) h(msg);
  }
}

function wait(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

describe('throttle-coalesces-updates', () => {
  it('coalesces many ticks into a single store update per interval', async () => {
    const bridge = new FakeBridge();
    const store = createAppStore();
    let updates = 0;
    const unsubStore = store.subscribe(() => updates++);

    const { destroy } = attachBridgeToStore(bridge, store, { intervalMs: 20 });

    // Emit a burst of ticks
    for (let i = 1; i <= 10; i++) {
      bridge.emit({ type: 'tick', tick_id: i });
    }

    // Wait a bit longer than interval to allow flush
    await wait(30);

    expect(store.getState().lastTickId).toBe(10);
    // Should be 1 or a very small number, not 10
    expect(updates).toBeLessThanOrEqual(2);

    unsubStore();
    destroy();
  });

  it('applies snapshot immediately and resets aggregation', async () => {
    const bridge = new FakeBridge();
    const store = createAppStore();
    const { destroy } = attachBridgeToStore(bridge, store, { intervalMs: 50 });

    bridge.emit({ type: 'tick', tick_id: 1 });
    bridge.emit({
      type: 'snapshot',
      state: {
        items: {},
        agents: {},
        metrics: { active_agents: 0, total_tokens: 0, total_spend_usd: 0, live_tps: 0, live_spend_per_s: 0, completion_rate: 0 },
        seed: 's',
        running: true,
      },
    });

    // Next tick id lower than previous should still apply because snapshot reset
    bridge.emit({ type: 'tick', tick_id: 1 });
    await wait(60);
    expect(store.getState().lastTickId).toBe(1);

    destroy();
  });
});

