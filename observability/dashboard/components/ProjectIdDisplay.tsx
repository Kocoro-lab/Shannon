"use client";

import React, { useSyncExternalStore } from 'react';
import { DEFAULT_PLAN_NAME } from '@/plans';
import { appStore } from '@/lib/store';

export default function ProjectIdDisplay() {
  const plan = useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
    () => appStore.getState().plan_name || DEFAULT_PLAN_NAME,
  );
  return <span>{plan}</span>;
}
