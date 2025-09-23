import { describe, it, expect } from 'vitest';
import { createSimBridge, type PortLike, type MessageEventLike, type SimMsg } from './simBridge';

class FakePort implements PortLike {
  private handlers = new Set<(e: MessageEventLike) => void>();
  public sent: unknown[] = [];
  postMessage(msg: unknown): void {
    this.sent.push(msg);
  }
  addEventListener(event: 'message', handler: (e: MessageEventLike) => void): void {
    if (event === 'message') this.handlers.add(handler);
  }
  removeEventListener(event: 'message', handler: (e: MessageEventLike) => void): void {
    if (event === 'message') this.handlers.delete(handler);
  }
  emit(msg: SimMsg) {
    for (const h of this.handlers) h({ data: msg });
  }
}

describe('simBridge', () => {
  it('drops out-of-order tick messages', () => {
    const port = new FakePort();
    const bridge = createSimBridge(port, { batchMs: 0 });
    const seen: number[] = [];
    bridge.subscribe((msg) => {
      if (msg.type === 'tick') seen.push(msg.tick_id);
    });

    port.emit({ type: 'tick', tick_id: 1 });
    port.emit({ type: 'tick', tick_id: 3 });
    port.emit({ type: 'tick', tick_id: 2 }); // should be dropped
    port.emit({ type: 'tick', tick_id: 4 });

    expect(seen).toEqual([1, 3, 4]);
  });

  it('resets ordering on snapshot', () => {
    const port = new FakePort();
    const bridge = createSimBridge(port, { batchMs: 0 });
    const seen: SimMsg[] = [];
    bridge.subscribe((msg) => seen.push(msg));

    port.emit({ type: 'tick', tick_id: 2 });
    port.emit({ type: 'snapshot', state: { items: {}, agents: {}, metrics: { active_agents: 0, total_tokens: 0, total_spend_usd: 0, live_tps: 0, live_spend_per_s: 0, completion_rate: 0 }, seed: 's', running: true } });
    port.emit({ type: 'tick', tick_id: 1 }); // accepted after snapshot

    const ticks = seen.filter((m) => m.type === 'tick') as Extract<SimMsg, { type: 'tick' }>[];
    expect(ticks.map((t) => t.tick_id)).toEqual([2, 1]);
  });
});

