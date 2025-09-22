'use client';

import { useDashboardMetrics } from '../../shannon/dashboardContext';

export function OperatorSummary() {
  const { totals, lastEvent } = useDashboardMetrics();

  return (
    <div className="h-full min-h-0 border border-[#352b19] bg-black flex flex-col">
      <div className="ui-label ui-label--tab">Operator Action Items</div>
      <div className="flex-1 min-h-0 overflow-auto no-scrollbar p-3 text-xs text-[#d5d5d5] space-y-3">
        <OperatorRow
          title="Active Flights"
          description="Agents currently executing tasks. Monitor for completion."
          value={totals.activeCount}
        />
        <OperatorRow
          title="Completed Flights"
          description="Flights that have landed successfully during this workflow."
          value={totals.completedCount}
        />
        <OperatorRow
          title="Alerts"
          description="Flights with errors require immediate attention."
          value={totals.errorCount}
          highlight={totals.errorCount > 0}
        />
        <div className="border-t border-[#1b1407] pt-3 text-[#9fa6b2]">
          <div className="text-[10px] uppercase tracking-wide text-[#c79325]">Latest Transmission</div>
          <div className="mt-1 text-xs leading-snug">
            {lastEvent ? (
              <>
                <span className="font-mono text-[#8aa9cf]">[{lastEvent.type}]</span> {lastEvent.formatted || lastEvent.message || '—'}
              </>
            ) : (
              'Waiting for traffic from the orchestrator.'
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function OperatorRow({
  title,
  description,
  value,
  highlight = false,
}: {
  title: string;
  description: string;
  value: number;
  highlight?: boolean;
}) {
  return (
    <div className="grid grid-cols-[64px_1fr] gap-2">
      <div className={`px-2 py-1 text-xs font-semibold ${highlight ? 'bg-[#2d0b0b] text-[#f87171]' : 'bg-[#021b44] text-[#97aed4]'}`}>
        {value}
      </div>
      <div className="px-2 py-1 bg-[#06142e] text-[#97aed4]">
        <div className="font-semibold text-xs uppercase tracking-wide">{title}</div>
        <div className="text-[11px] text-[#7a8cb6] mt-1">{description}</div>
      </div>
    </div>
  );
}
