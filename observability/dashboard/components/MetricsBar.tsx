"use client";

import React, { useSyncExternalStore } from 'react';
import { appStore } from '@/lib/store';

function useAppMetrics() {
  return useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().metrics,
    () => appStore.getState().metrics,
  );
}

function fmtInt(n: number) {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(n || 0);
}

function fmtFloat(n: number, frac = 1) {
  const v = Number.isFinite(n) ? n : 0;
  return v.toFixed(frac);
}

function fmtUSD(n: number) {
  const v = Number.isFinite(n) ? n : 0;
  return `$${new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(v)}`;
}

function fmtPct(n: number) {
  const v = Number.isFinite(n) ? n : 0;
  return `${(v * 100).toFixed(0)}%`;
}

export default function MetricsBar() {
  const m = useAppMetrics();

  return (
    <div className="px-4 py-3">
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        <Metric label="Active Agents" value={fmtInt(m.active_agents)} />
        <Metric label="Total Tokens" value={fmtInt(m.total_tokens)} />
        <Metric label="Total Spend" value={fmtUSD(m.total_spend_usd)} />
        <Metric label="Live TPS" value={fmtFloat(m.live_tps, 1)} />
        <Metric label="Spend / sec" value={fmtUSD(m.live_spend_per_s)} />
        <Metric label="Completion" value={fmtPct(m.completion_rate)} />
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="border border-gray-800 bg-black px-3 py-2">
      <div className="text-xs text-gray-400">{label}</div>
      <div className="text-lg font-semibold text-gray-100">{value}</div>
    </div>
  );
}
