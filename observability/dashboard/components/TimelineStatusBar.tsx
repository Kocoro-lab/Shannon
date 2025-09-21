"use client";

import React, { useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { appStore } from '@/lib/store';
import type { UIState } from '@/lib/store';
import type { WorkItem } from '@/lib/types';

function useAppSelector<T>(selector: (s: UIState) => T): T {
  return useSyncExternalStore(
    appStore.subscribe,
    () => selector(appStore.getState()),
    () => selector(appStore.getState())
  );
}

function fmtHMS(ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return '0:00';
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const mm = m.toString();
  const ss = s.toString().padStart(2, '0');
  if (h > 0) return `${h}:${mm.padStart(2, '0')}:${ss}`;
  return `${mm}:${ss}`;
}

export default function TimelineStatusBar() {
  const items = useAppSelector((s) => s.items);

  // Compute totals and progress across items, plus earliest start and completion
  const { totalEstimateMs, progressMs, hasAnyStarted, earliestStart, projectComplete } = useMemo(() => {
    let total = 0;
    let progressed = 0;
    let anyStarted = false;
    let earliest: number | undefined = undefined;
    let allDone = true;
    const now = Date.now();
    const arr = Object.values(items) as WorkItem[];
    if (arr.length === 0) allDone = false; // empty project is not complete
    for (const it of arr) {
      const est = Math.max(0, it.estimate_ms || 0);
      total += est;
      if (typeof it.started_at === 'number' && isFinite(it.started_at)) {
        if (earliest === undefined || it.started_at < earliest) earliest = it.started_at;
      }
      if (it.status !== 'queued' || (typeof it.started_at === 'number' && isFinite(it.started_at))) anyStarted = true;
      if (it.status !== 'done') allDone = false;
      if (est <= 0) continue;
      if (it.status === 'done') {
        progressed += est;
      } else if (it.status === 'in_progress') {
        if (typeof it.eta_ms === 'number' && isFinite(it.eta_ms)) {
          const elapsed = Math.max(0, Math.min(est, est - it.eta_ms));
          progressed += elapsed;
        } else if (typeof it.started_at === 'number' && isFinite(it.started_at)) {
          const elapsed = Math.max(0, Math.min(est, now - it.started_at));
          progressed += elapsed;
        }
      }
    }
    return { totalEstimateMs: total, progressMs: progressed, hasAnyStarted: anyStarted, earliestStart: earliest, projectComplete: allDone };
  }, [items]);

  const [now, setNow] = useState<number>(() => Date.now());
  const [stoppedAt, setStoppedAt] = useState<number | undefined>(undefined);

  // Determine if any items are currently in progress (agents active)
  // const anyInProgress = useMemo(() => {
  //   for (const it of Object.values(items) as WorkItem[]) {
  //     if (it.status === 'in_progress') return true;
  //   }
  //   return false;
  // }, [items]);

  const hasStarted = hasAnyStarted;
  const isComplete = !!projectComplete;

  // True if any item is actively processing
  const anyInProgress = useMemo(() => {
    for (const it of Object.values(items) as WorkItem[]) {
      if (it.status === 'in_progress') return true;
    }
    return false;
  }, [items]);

  // Tick while started and not complete; freeze when complete
  useEffect(() => {
    let id: number | undefined;
    if (hasStarted && !isComplete) {
      setStoppedAt(undefined);
      // Faster tick for smoother progress updates
      id = window.setInterval(() => setNow(Date.now()), 100);
    } else if (isComplete) {
      // capture the moment we detected completion (freeze timer)
      setStoppedAt((prev) => prev ?? Date.now());
    }
    return () => {
      if (id) clearInterval(id);
    };
  }, [hasStarted, isComplete]);

  const denom = totalEstimateMs > 0 ? totalEstimateMs : 1;
  // Progress bar uses aggregate item progress (responds to engine speed)
  const progressed = hasStarted ? Math.max(0, Math.min(denom, progressMs)) : 0;
  const baseProgress = Math.max(0, Math.min(1, progressed / denom));
  const progress = isComplete ? 1 : baseProgress;
  // Timer uses wall-clock since first start, freezes on completion
  const effectiveNow = !isComplete ? now : (stoppedAt ?? now);
  const timerMs = hasStarted && typeof earliestStart === 'number' ? Math.max(0, effectiveNow - earliestStart) : 0;
  const statusLabel = !hasStarted ? 'INITIALIZING' : isComplete ? 'COMPLETE' : 'RUNNING';

  return (
    <div className="w-full bg-black">
      <div className="flex items-center gap-4 px-3 py-2">
        <div className="flex items-center gap-3 min-w-[180px]">
          <LiveBadge flashing={anyInProgress} />
          <span
            className={`text-xs font-semibold ${statusLabel === 'INITIALIZING' ? 'text-yellow-300' : 'text-[var(--row-done-fg)]'}`}
          >
            {statusLabel}
          </span>
          {hasStarted && (
            <span className="font-mono text-xs text-[var(--row-done-fg)]">{fmtHMS(timerMs)}</span>
          )}
        </div>
        <div className="flex-1">
          <ProgressBar pct={progress} />
        </div>
      </div>
    </div>
  );
}

function ProgressBar({ pct }: { pct: number }) {
  const pc = Math.max(0, Math.min(1, pct));
  const percent = pc * 100;
  return (
    <div className="relative h-3 w-full bg-[#0b0b0b] border border-[#1f2910]">
      {/* filled portion */}
      <div
        className="h-full bg-green-600"
        style={{ width: `${percent}%`, transition: 'width 0.3s linear' }}
      />
      {/* notch at current progress */}
      <div
        className="absolute top-0 h-full border-r-2 border-white/70"
        style={{ left: `calc(${percent}% - 1px)`, transition: 'left 0.3s linear' }}
      />
    </div>
  );
}

function LiveBadge({ flashing = false }: { flashing?: boolean }) {
  const dotClass = flashing ? 'bg-red-500 animate-pulse' : 'bg-green-500';
  const label = flashing ? 'LIVE' : 'READY';
  const labelClass = flashing ? 'text-gray-300' : 'text-green-400';
  return (
    <div className="flex items-center gap-2 border border-gray-600/60 rounded px-2 py-0.5 select-none">
      <span className={`inline-block h-2 w-2 rounded-full ${dotClass}`}></span>
      <span className={`text-[10px] leading-none font-bold tracking-widest ${labelClass}`}>{label}</span>
    </div>
  );
}
