// Deterministic RNG utilities (mulberry32 + string seeding)

// Hash a string to a 32-bit unsigned integer (xmur3-like)
export function seedFromString(str: string): number {
  let h = 1779033703 ^ str.length;
  for (let i = 0; i < str.length; i++) {
    h = Math.imul(h ^ str.charCodeAt(i), 3432918353);
    h = (h << 13) | (h >>> 19);
  }
  h = Math.imul(h ^ (h >>> 16), 2246822507);
  h = Math.imul(h ^ (h >>> 13), 3266489909);
  h ^= h >>> 16;
  // Ensure uint32
  return h >>> 0;
}

// mulberry32 PRNG
function mulberry32(seed: number) {
  let a = seed >>> 0;
  return function next() {
    a |= 0;
    a = (a + 0x6D2B79F5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296; // [0,1)
  };
}

export interface RNG {
  seed: number;
  next: () => number; // float in [0,1)
  float: (min: number, max: number) => number;
  int: (min: number, max: number) => number; // inclusive min/max
  bool: (p?: number) => boolean; // true with probability p (default 0.5)
}

export function createRNG(seed: number | string): RNG {
  const s = typeof seed === 'string' ? seedFromString(seed) : (seed >>> 0);
  const base = mulberry32(s);
  const next = () => base();
  const float = (min: number, max: number) => min + (max - min) * next();
  const int = (min: number, max: number) => Math.floor(float(min, max + 1));
  const bool = (p = 0.5) => next() < p;
  return { seed: s, next, float, int, bool };
}

// Keep stub flag to satisfy existing import smoke test
export const RNG_MODULE_LOADED = true;
