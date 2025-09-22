import { describe, it, expect } from 'vitest';
import { calmPlan } from './calm';

describe('calm plan shape', () => {
  it('has 12 unique items and valid deps', () => {
    const ids = new Set(calmPlan.items.map(i => i.id));
    expect(ids.size).toBe(calmPlan.items.length);
    const idList = new Set(calmPlan.items.map(i => i.id));
    for (const item of calmPlan.items) {
      for (const dep of item.depends_on) {
        expect(idList.has(dep)).toBe(true);
      }
      expect(item.tps_min).toBeLessThan(item.tps_max);
      expect(item.estimate_ms).toBeGreaterThan(0);
    }
  });

  it('is acyclic', () => {
    const map = new Map<string, string[]>();
    calmPlan.items.forEach(i => map.set(i.id, i.depends_on));
    const seen = new Set<string>();
    const stack = new Set<string>();
    const visit = (id: string): boolean => {
      if (stack.has(id)) return false; // cycle
      if (seen.has(id)) return true;
      stack.add(id);
      for (const d of map.get(id) || []) if (!visit(d)) return false;
      stack.delete(id);
      seen.add(id);
      return true;
    };
    for (const id of map.keys()) {
      expect(visit(id)).toBe(true);
    }
  });
});

