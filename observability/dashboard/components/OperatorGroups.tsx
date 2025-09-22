"use client";

import React, { useMemo, useSyncExternalStore } from 'react';
import { getPlanByName, ALL_PLANS } from '@/plans';
import type { PlanDefinition, WorkGroupDef } from '@/plans/types';
import { appStore } from '@/lib/store';
import { DEFAULT_PLAN_NAME } from '@/plans';

function pickPlan(name: string): PlanDefinition {
  return getPlanByName(name) ?? ALL_PLANS[0];
}

function deriveGroupsFromItems(items: ReturnType<typeof appStore.getState>['items']): WorkGroupDef[] {
  const seen = new Map<string, number>();
  for (const it of Object.values(items)) {
    seen.set(it.group, (seen.get(it.group) || 0) + 1);
  }
  const out: WorkGroupDef[] = [];
  for (const [id, count] of seen) {
    out.push({ id, title: `Group ${id}`, description: `${count} work items.` });
  }
  out.sort((a, b) => a.id.localeCompare(b.id));
  return out;
}

export default function OperatorGroups() {
  // Subscribe to live items so completion updates over time
  const items = useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().items,
    () => appStore.getState().items,
  );
  // Subscribe to the applied plan name so Operator Action Items update on Execute
  const planName = useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
  );

  const groups = useMemo(() => {
    const plan = pickPlan(planName);
    if (plan.groups && plan.groups.length) return plan.groups;
    return deriveGroupsFromItems(items);
  }, [planName, items]);

  function percentForGroup(groupId: string): number {
    const list = Object.values(items).filter((it) => it.group === groupId);
    if (!list.length) return 0;
    let sumEst = 0;
    let sumElapsed = 0;
    const now = Date.now();
    for (const it of list) {
      const est = Math.max(0, it.estimate_ms || 0);
      if (est <= 0) continue;
      sumEst += est;
      let elapsed = 0;
      if (it.status === 'done') {
        elapsed = est;
      } else if (it.status === 'in_progress') {
        if (typeof it.eta_ms === 'number' && isFinite(it.eta_ms)) {
          elapsed = Math.max(0, Math.min(est, est - it.eta_ms));
        } else if (typeof it.started_at === 'number' && isFinite(it.started_at)) {
          elapsed = Math.max(0, Math.min(est, now - it.started_at));
        }
      }
      sumElapsed += elapsed;
    }
    if (sumEst <= 0) return 0;
    return Math.max(0, Math.min(1, sumElapsed / sumEst));
  }

  return (
    <div className="flex flex-col">
      {groups.map((g) => {
        const pct = percentForGroup(g.id);
        const pctText = `${(pct * 100).toFixed(1)}%`;
        return (
        <div key={g.id}>
          <div
            className="grid gap-0.5 bg-[#0f1d34]"
            style={{ gridTemplateColumns: '64px 1fr', gridTemplateRows: 'auto auto' }}
          >
            {/* Top-left: Group ID */}
            <div className="text-xs p-1 bg-[#021b44ff] text-[#97aed4ff]">{g.id}</div>
            {/* Title to the right of ID */}
            <div className="text-xs p-1 bg-[#021b44ff] text-[#97aed4ff]">{g.title}</div>
            {/* Bottom-left: completion placeholder */}
            <div className="text-xs p-1 bg-[#06142eff]">{pctText}</div>
            {/* Description below the title */}
            <div className="text-xs p-1 bg-[#06142eff]">{g.description}</div>
          </div>
        </div>
        );
      })}
    </div>
  );
}
