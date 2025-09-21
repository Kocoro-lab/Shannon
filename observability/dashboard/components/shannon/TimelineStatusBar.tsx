'use client';

import { useDashboardMetrics } from '../../shannon/dashboardContext';

function formatDuration(ms: number): string {
  if (ms <= 0) return '0:00';
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const hours = Math.floor(totalSeconds / 3600);
  if (hours > 0) {
    return `${hours}:${minutes.toString().padStart(2, '0')}:${seconds.toString().padStart(2, '0')}`;
  }
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export function TimelineStatusBar() {
  const { metrics, totals } = useDashboardMetrics();

  const now = metrics.lastEventAt ?? metrics.startedAt ?? Date.now();
  const startedAt = metrics.startedAt ?? now;
  const elapsed = Math.max(0, now - startedAt);
  const progress = totals.completionRate;

  return (
    <div className="w-full bg-black border border-[#352b19]">
      <div className="flex items-center gap-4 px-3 py-2">
        <div className="flex items-center gap-3 min-w-[180px]">
          <LiveBadge active={totals.activeCount > 0} />
          <span className={`text-xs font-semibold uppercase tracking-wide ${totals.activeCount > 0 ? 'text-[var(--row-done-fg)]' : 'text-green-400'}`}>
            {totals.activeCount > 0 ? 'Running' : 'Ready'}
          </span>
          <span className="font-mono text-xs text-[#8aa9cf]">{formatDuration(elapsed)}</span>
        </div>
        <div className="flex-1">
          <div className="relative h-3 w-full bg-[#0b0b0b] border border-[#1f2910]">
            <div className="h-full bg-green-600 transition-all" style={{ width: `${progress * 100}%` }} />
            <div
              className="absolute top-0 h-full border-r-2 border-white/70"
              style={{ left: `calc(${progress * 100}% - 1px)` }}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

function LiveBadge({ active }: { active: boolean }) {
  const dotClass = active ? 'bg-red-500 animate-pulse' : 'bg-green-500';
  const label = active ? 'LIVE' : 'LIVE';
  const labelClass = active ? 'text-gray-300' : 'text-green-400';
  return (
    <div className="flex items-center gap-2 border border-gray-600/60 rounded px-2 py-0.5 select-none">
      <span className={`inline-flex h-2 w-2 rounded-full ${dotClass}`}></span>
      <span className={`text-[10px] leading-none font-bold tracking-widest ${labelClass}`}>{label}</span>
    </div>
  );
}
