'use client';

import { useDashboardMetrics } from '../../shannon/dashboardContext';
import { COST_PER_TOKEN_USD } from '../../lib/constants';

function millis(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(s / 60);
  const r = s % 60;
  return m > 0 ? `${m}m ${r}s` : `${r}s`;
}

export function InsightsPanel() {
  const { tools, timing, totals, metrics } = useDashboardMetrics();

  const toolEntries = Object.entries(tools || {}).sort((a, b) => b[1] - a[1]).slice(0, 5);
  const totalMs = Math.max(1, (timing?.thinkingMs || 0) + (timing?.executionMs || 0));
  const thinkingPct = Math.round(((timing?.thinkingMs || 0) / totalMs) * 100);
  const executionPct = 100 - thinkingPct;

  return (
    <div className="grid grid-cols-1 xl:grid-cols-3 gap-3 text-[#d5d5d5]">
      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Tools Used</div>
        <div className="p-3 text-sm space-y-1">
          {toolEntries.length === 0 ? (
            <div className="text-xs text-[#a0a0a0]">No tools observed</div>
          ) : (
            toolEntries.map(([name, count]) => (
              <div key={name} className="flex items-center justify-between">
                <span className="text-[#a0a0a0]">{name}</span>
                <span className="font-mono">{count}</span>
              </div>
            ))
          )}
        </div>
      </section>

      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Time Breakdown</div>
        <div className="p-3 text-sm space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-[#a0a0a0]">Thinking</span>
            <span className="font-mono">{millis(timing?.thinkingMs || 0)} · {thinkingPct}%</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-[#a0a0a0]">Execution</span>
            <span className="font-mono">{millis(timing?.executionMs || 0)} · {executionPct}%</span>
          </div>
          <div className="h-2 bg-[#0b0b0b] border border-[#1f2910]">
            <div className="h-full bg-[#a3e635]" style={{ width: `${thinkingPct}%` }} />
          </div>
        </div>
      </section>

      <section className="bg-black border border-[#352b19]">
        <div className="ui-label ui-label--tab">Platform Summary</div>
        <div className="p-3 text-sm space-y-1">
          <Row label="Total Workflows" value={`${totals.totalFlights}`} />
          <Row label="Active Agents" value={`${totals.activeCount}`} />
          <Row label="Completed" value={`${totals.completedCount}`} />
          <Row label="Total Events" value={`${metrics.totalEvents}`} />
        </div>
      </section>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-[#a0a0a0]">{label}</span>
      <span className="font-mono">{value}</span>
    </div>
  );
}

