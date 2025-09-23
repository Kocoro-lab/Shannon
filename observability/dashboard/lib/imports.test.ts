import { describe, it, expect } from 'vitest';

describe('imports-don\'t-throw', () => {
  it('imports all stub modules without throwing', async () => {
    // Test that each module can be imported
    const { TYPES_MODULE_LOADED } = await import('./types');
    expect(TYPES_MODULE_LOADED).toBe(true);
    
    const { CONSTANTS_MODULE_LOADED } = await import('./constants');
    expect(CONSTANTS_MODULE_LOADED).toBe(true);
    
    const { RNG_MODULE_LOADED } = await import('./rng');
    expect(RNG_MODULE_LOADED).toBe(true);
    
    const { SIMBRIDGE_MODULE_LOADED } = await import('./simBridge');
    expect(SIMBRIDGE_MODULE_LOADED).toBe(true);
    
    // Worker module can't be directly imported in test environment,
    // but we can verify the file exists
    const workerModule = await import('../workers/engine');
    expect(workerModule.ENGINE_WORKER_MODULE_LOADED).toBe(true);
  });
});
