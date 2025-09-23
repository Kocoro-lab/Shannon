import { describe, it, expect } from 'vitest';
import { COST_PER_TOKEN_USD, MAX_CONCURRENT, V_MIN, V_MAX } from './constants';

describe('constants-sanity', () => {
  it('has valid numeric relationships', () => {
    expect(Number.isFinite(COST_PER_TOKEN_USD)).toBe(true);
    expect(COST_PER_TOKEN_USD).toBeGreaterThan(0);
    expect(MAX_CONCURRENT).toBeGreaterThan(0);
    expect(V_MIN).toBeGreaterThan(0);
    expect(V_MAX).toBeGreaterThan(V_MIN);
  });
});

