import { describe, it, expect } from 'vitest';
import { BRIDGE_BATCH_MS, STORE_FLUSH_INTERVAL_MS, ENGINE_TICK_HZ, DEFAULT_SEED } from './config';

describe('config defaults', () => {
  it('has sane positive numeric defaults', () => {
    expect(BRIDGE_BATCH_MS).toBeGreaterThan(0);
    expect(STORE_FLUSH_INTERVAL_MS).toBeGreaterThan(0);
    expect(ENGINE_TICK_HZ).toBeGreaterThan(0);
  });

  it('has a default seed string', () => {
    expect(typeof DEFAULT_SEED).toBe('string');
    expect(DEFAULT_SEED.length).toBeGreaterThan(0);
  });
});

