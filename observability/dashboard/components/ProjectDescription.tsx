"use client";

import React, { useMemo, useSyncExternalStore } from 'react';
import { getPlanByName, ALL_PLANS, DEFAULT_PLAN_NAME } from '@/plans';
import { appStore } from '@/lib/store';

export default function ProjectDescription() {
  const plan = useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
  );

  const desc = useMemo(() => {
    const p = getPlanByName(plan) || ALL_PLANS[0];
    return p.description || 'Agent Traffic Control';
  }, [plan]);

  return <span>{desc}</span>;
}
