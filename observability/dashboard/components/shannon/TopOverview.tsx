'use client';

import { useDashboardMetrics } from '../../shannon/dashboardContext';

function formatNumber(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(value);
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(0)}%`;
}

export function TopOverview() {
  const { metrics, totals } = useDashboardMetrics();

  const elapsedMs = metrics.startedAt
    ? (metrics.lastEventAt ?? Date.now()) - metrics.startedAt
    : 0;
  const elapsedSeconds = elapsedMs > 0 ? elapsedMs / 1000 : 0;
  const eventsPerSec = elapsedSeconds > 0 ? metrics.totalEvents / elapsedSeconds : 0;

  return (
    <div className="grid grid-cols-1 xl:grid-cols-3 gap-3 text-[#d5d5d5]">
      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Main Metrics</div>
        <div className="grid grid-cols-3 gap-3 p-3 text-sm">
          <Metric label="Active Agents" value={formatNumber(totals.activeCount)} />
          <Metric label="Completed" value={formatNumber(totals.completedCount)} />
          <Metric label="Errors" value={formatNumber(metrics.errorEvents)} tone={metrics.errorEvents > 0 ? 'danger' : 'muted'} />
        </div>
      </section>
      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Live Throughput</div>
        <div className="grid grid-cols-2 gap-3 p-3 text-sm">
          <Metric label="Events" value={formatNumber(metrics.totalEvents)} />
          <Metric label="Events / Sec" value={eventsPerSec.toFixed(2)} />
        </div>
      </section>
      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Completion Rate</div>
        <div className="p-3 flex flex-col gap-2 text-sm">
          <Metric label="Progress" value={formatPercent(totals.completionRate)} />
          <div className="h-2 bg-[#0b0b0b] border border-[#1f2910]">
            <div className="h-full bg-green-600 transition-all duration-500" style={{ width: `${totals.completionRate * 100}%` }} />
          </div>
        </div>
      </section>
    </div>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'danger' | 'muted' }) {
  const color = tone === 'danger' ? '#f87171' : tone === 'muted' ? '#a0a0a0' : '#d5d5d5';
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs" style={{ color: '#a0a0a0' }}>{label}</span>
      <span className="text-lg font-semibold" style={{ color }}>{value}</span>
    </div>
  );
}
