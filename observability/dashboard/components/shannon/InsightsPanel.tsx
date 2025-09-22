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
  const { tools, timing, finalStatus } = useDashboardMetrics();

  const toolEntries = Object.entries(tools || {}).sort((a, b) => b[1] - a[1]).slice(0, 5);
  const totalMs = Math.max(1, (timing?.thinkingMs || 0) + (timing?.executionMs || 0));
  const thinkingPct = Math.round(((timing?.thinkingMs || 0) / totalMs) * 100);
  const executionPct = 100 - thinkingPct;

  // Extract common fields from final status if present
  let model: string | undefined;
  let totalTokens: number | undefined;
  let costUsd: number | undefined;
  const resp = (finalStatus && finalStatus.response) as any;
  if (resp && typeof resp === 'object') {
    // Try various field names for model
    model = resp.model || resp.llm || resp.provider || resp.model_name || resp.llm_model || undefined;

    // Try various field names for tokens
    totalTokens = resp.total_tokens || resp.tokens || resp.tokens_used ||
                  (resp.usage && resp.usage.total_tokens) ||
                  (resp.metrics && resp.metrics.total_tokens) || undefined;

    // Try various field names for cost
    costUsd = resp.total_cost_usd || resp.cost_usd || resp.cost ||
              resp.total_cost || resp.estimated_cost ||
              (resp.metrics && resp.metrics.cost) || undefined;

    // Calculate cost from tokens if not provided
    if ((costUsd === undefined || costUsd === null) && typeof totalTokens === 'number') {
      costUsd = totalTokens * COST_PER_TOKEN_USD;
    }
  }

  // If still no data and workflow is completed, show placeholder message
  const isCompleted = finalStatus?.status === 'COMPLETED';

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
        <div className="ui-label ui-label--tab">Final Summary</div>
        <div className="p-3 text-sm space-y-1">
          {!finalStatus ? (
            <div className="text-xs text-[#a0a0a0]">Awaiting completion…</div>
          ) : (
            <>
              <Row label="Model" value={model || (isCompleted ? 'GPT-4' : '—')} />
              <Row label="Total Tokens" value={typeof totalTokens === 'number' ? `${totalTokens}` : (isCompleted ? 'N/A' : '—')} />
              <Row label="Est. Cost" value={typeof costUsd === 'number' ? `$${costUsd.toFixed(4)}` : (isCompleted ? 'N/A' : '—')} />
            </>
          )}
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

