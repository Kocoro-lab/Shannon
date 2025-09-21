import { describe, it, expect } from 'vitest';
import { makeInitialState, hzToMs, zeroMetrics } from './engine';

describe('engine-handshake helpers', () => {
  it('makes an initial state with zeros and defaults', () => {
    const s = makeInitialState('seed');
    expect(s.seed).toBe('seed');
    expect(s.items).toEqual({});
    expect(s.agents).toEqual({});
    expect(s.metrics).toEqual(zeroMetrics());
  });

  it('converts Hz to ms reasonably', () => {
    expect(hzToMs(50)).toBeGreaterThan(0);
    expect(hzToMs(25)).toBeCloseTo(40, 0);
  });
});
