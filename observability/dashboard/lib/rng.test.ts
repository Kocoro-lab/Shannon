import { describe, it, expect } from 'vitest';
import { createRNG, seedFromString } from './rng';

describe('rng-determinism', () => {
  it('same numeric seed yields same sequence', () => {
    const a = createRNG(123456);
    const b = createRNG(123456);
    const seqA = Array.from({ length: 5 }, () => a.next());
    const seqB = Array.from({ length: 5 }, () => b.next());
    expect(seqA).toEqual(seqB);
  });

  it('same string seed yields same sequence', () => {
    const a = createRNG('hello');
    const b = createRNG('hello');
    const seqA = Array.from({ length: 5 }, () => a.int(0, 1000));
    const seqB = Array.from({ length: 5 }, () => b.int(0, 1000));
    expect(seqA).toEqual(seqB);
  });

  it('different seed diverges quickly', () => {
    const a = createRNG('hello');
    const b = createRNG('hello2');
    const seqA = Array.from({ length: 5 }, () => a.next());
    const seqB = Array.from({ length: 5 }, () => b.next());
    expect(seqA).not.toEqual(seqB);
  });

  it('seedFromString is stable', () => {
    expect(seedFromString('abc')).toEqual(seedFromString('abc'));
  });
});

