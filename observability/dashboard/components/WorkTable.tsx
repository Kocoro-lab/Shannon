"use client";

import React, { useMemo, useSyncExternalStore } from 'react';
import type { UIState } from '@/lib/store';
import { appStore } from '@/lib/store';
import type { WorkItem } from '@/lib/types';
import { formatTokensAbbrev } from '@/lib/format';

function useAppSelector<T>(selector: (s: UIState) => T): T {
  return useSyncExternalStore(
    appStore.subscribe,
    () => selector(appStore.getState()),
    () => selector(appStore.getState())
  );
}

const statusOrder: Record<string, number> = {
  in_progress: 0,
  queued: 1,
  assigned: 1,
  blocked: 1,
  done: 2,
};

function fmtETA(ms?: number) {
  if (typeof ms !== 'number' || !isFinite(ms) || ms < 0) return '—';
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const mm = m.toString().padStart(2, '0');
  const ss = s.toString().padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

type ColumnKey = 'id' | 'agent' | 'sector' | 'work' | 'tokens' | 'tps' | 'eta';

export default function WorkTable({
  compact = false,
  mini = false,
  maxHeight,
  columns,
}: {
  compact?: boolean;
  mini?: boolean;
  maxHeight?: number;
  columns?: ColumnKey[];
}) {
  const items = useAppSelector((s) => s.items);

  const rows = useMemo(() => {
    const list = Object.values(items);
    list.sort((a, b) => {
      const ao = statusOrder[a.status] ?? 99;
      const bo = statusOrder[b.status] ?? 99;
      if (ao !== bo) return ao - bo;
      return a.id.localeCompare(b.id);
    });
    return list;
  }, [items]);

  const widthMap: Record<ColumnKey, number> = compact
    ? { id: 22, agent: 26, sector: 38, work: 150, tokens: 34, tps: 34, eta: 42 }
    : { id: 30, agent: 35, sector: 65, work: 225, tokens: 40, tps: 45, eta: 50 };
  const labelMap: Record<ColumnKey, string> = {
    id: 'ID',
    agent: 'AGENT',
    sector: 'SECTOR',
    work: 'WORK ORDER',
    tokens: 'TOKENS',
    tps: 'TPS',
    eta: 'ETA',
  };
  const renderCell: Record<ColumnKey, (it: WorkItem, cls: string) => React.ReactNode> = {
    id: (it, cls) => (
      <td className={`${cls} font-mono`} style={{ backgroundColor: 'inherit', borderLeft: '4px solid currentColor' }}>{it.id}</td>
    ),
    agent: (it, cls) => (
      <td className={`${cls} font-mono`}>{it.agent_id ?? '—'}</td>
    ),
    sector: (it, cls) => (
      <td className={cls} style={{ backgroundColor: 'inherit' }}>{it.sector}</td>
    ),
    work: (it, cls) => (
      <td className={cls} style={{ backgroundColor: 'inherit', maxWidth: workMaxWidth, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={it.desc || ''}>
        {it.desc || '—'}
      </td>
    ),
    tokens: (it, cls) => (
      <td className={cls} style={{ backgroundColor: 'inherit' }}>{formatTokensAbbrev(it.tokens_done)}</td>
    ),
    tps: (it, cls) => {
      const text = it.status === 'in_progress'
        ? formatTokensAbbrev(it.tps, { tpsMode: true, extraDecimalUnder100: true })
        : '0';
      return (
        <td className={cls} style={{ backgroundColor: 'inherit' }}>{text}</td>
      );
    },
    eta: (it, cls) => (
      <td className={cls} style={{ backgroundColor: 'inherit' }}>{fmtETA(it.eta_ms)}</td>
    ),
  };

  const activeColumns: ColumnKey[] = columns && columns.length
    ? columns
    : ['id', 'agent', 'sector', 'work', 'tokens', 'tps', 'eta'];

  const workMaxWidth = compact ? 200 : 360;
  const tableTextCls = compact ? (mini ? 'text-[11px]' : 'text-xs') : 'text-sm';
  const thPad = compact ? (mini ? 'px-1 py-0.5' : 'px-1 py-1') : 'px-1 py-2';
  const tdPad = compact ? (mini ? 'px-1 py-0.5' : 'px-1 py-0.5') : 'px-2 py-1';

  return (
    <div className="h-full min-h-0 flex flex-col">
      <div className="flex-1 min-h-0 overflow-auto no-scrollbar border border-[#352b19ff] border-t-0" style={{ maxHeight: maxHeight }}>
        <table
              className={`min-w-full ${tableTextCls}`}
              style={{
                tableLayout: 'fixed',
                width: '100%',
                borderCollapse: 'separate',
                borderSpacing: compact ? (mini ? '1px' : '2px') : '4px',
                backgroundColor: '#000',
              }}>
          <colgroup>
            {activeColumns.map((key) => (
              <col key={key} style={{ width: widthMap[key] }} />
            ))}
          </colgroup>
          <thead className="text-[#d79326ff]">
            <tr>
              {activeColumns.map((key) => (
                <th key={key} className={`${thPad} text-left border-b-1`}>{labelMap[key]}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((it) => {
              const rowCls =
                it.status === 'done' ? 'tr-status-done' :
                it.status === 'in_progress' ? 'tr-status-inprogress' :
                'tr-status-queued';
              return (
                <tr key={it.id} className={`${rowCls}`}>
                  {activeColumns.map((key) => (
                    <React.Fragment key={`${it.id}:${key}`}>
                      {renderCell[key](it, tdPad)}
                    </React.Fragment>
                  ))}
                </tr>
              );
            })}
            {rows.length === 0 && (
              <tr className="pt-2">
                <td colSpan={activeColumns.length} className="px-3 py-3 text-gray-400">No items loaded yet.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
