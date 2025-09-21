"use client";

import React, { useMemo, useSyncExternalStore } from 'react';
import { appStore } from '@/lib/store';
import type { UIState } from '@/lib/store';
import type { WorkItem } from '@/lib/types';
import { SECTORS } from '@/lib/constants';

function useAppSelector<T>(selector: (s: UIState) => T): T {
  return useSyncExternalStore(
    appStore.subscribe,
    () => selector(appStore.getState()),
    () => selector(appStore.getState())
  );
}

type Bucket = {
  active: WorkItem[];
  upNext: WorkItem[];
  queue: WorkItem[];
};

export default function GlobalQueue() {
  const items = useAppSelector((s) => s.items);

  const bySector = useMemo(() => {
    const buckets = new Map<string, Bucket>();
    for (const sec of SECTORS) buckets.set(sec, { active: [], upNext: [], queue: [] });
    for (const it of Object.values(items)) {
      if (it.status === 'done') continue; // drop completed
      const sec = (it.sector || '').toUpperCase();
      if (!buckets.has(sec)) buckets.set(sec, { active: [], upNext: [], queue: [] });
      const b = buckets.get(sec)!;
      if (it.status === 'in_progress') b.active.push(it);
      else if (it.status === 'assigned') b.upNext.push(it);
      else b.queue.push(it); // queued/blocked/default
    }
    // stable sort by id for readability
    for (const b of buckets.values()) {
      b.active.sort((a, z) => a.id.localeCompare(z.id));
      b.upNext.sort((a, z) => a.id.localeCompare(z.id));
      b.queue.sort((a, z) => a.id.localeCompare(z.id));
    }
    return buckets;
  }, [items]);

  return (
    <div className="h-full min-h-0 bg-black">
      {/* 4 equal-height sector panes; scroll inside each */}
      <div className="grid grid-rows-4 h-full min-h-0">
        {SECTORS.map((sec, idx) => {
          const b = bySector.get(sec) || { active: [], upNext: [], queue: [] };
          const nextList = b.upNext.length > 0 ? b.upNext : (b.queue.length > 0 ? [b.queue[0]] : []);
          const remaining = b.upNext.length > 0 ? b.queue : b.queue.slice(1);
          return (
            <section key={sec} className={`min-h-0 overflow-hidden ${idx < SECTORS.length - 1 ? '' : ''}`}> 
              {/* Sector header styled like WorkTable headers */}
              <div className="px-2 pt-1 pb-1 text-[#d79326ff] border-b-1 text-xs font-semibold tracking-tight">
                {sec}
              </div>
              <div className="flex-1 min-h-0 overflow-auto no-scrollbar space-y-1">
                {/* Active */}
                {b.active.length > 0 && (
                  <div className="space-y-1">
                    {b.active.map((it) => (
                      <Row key={`act-${it.id}`} tone="active" label="ACTIVE" it={it} />
                    ))}
                  </div>
                )}

                {/* Up next: prefer assigned; else first queued item */}
                {nextList.length > 0 && (
                  <div className="space-y-1">
                    {nextList.map((it) => (
                      <Row key={`next-${it.id}`} tone="upnext" label="UP NEXT" it={it} />
                    ))}
                  </div>
                )}

                {/* Remaining queue (skip the first if it was promoted) */}
                {remaining.length > 0 ? (
                  <div className="space-y-1">
                    {remaining.map((it) => (
                      <Row key={`q-${it.id}`} tone="queue" label="" it={it} />
                    ))}
                  </div>
                ) : (
                  // Only show "queue empty" when there is truly no next item
                  nextList.length === 0 ? (
                    <div className="text-xs text-gray-500 italic pt-2">Queue empty</div>
                  ) : null
                )}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}

function Row({ it, tone, label }: { it: WorkItem; tone: 'active' | 'upnext' | 'queue'; label: string }) {
  const cls = tone === 'active'
    ? 'bg-[#0a1c0dff] text-[#2e904dff] border-r-2'
    : tone === 'upnext'
    ? 'bg-sky-900 text-sky-300 border-r-2'
    : 'bg-black text-zinc-300';
  return (
    <div className={`flex flex-col gap-0.5 ${cls}`} title={it.desc || ''}>
      {/* Status line (blank for queued) */}
      <div className="px-2 pt-1 text-[10px] tracking-wide h-5 leading-4">{label || ' '}</div>
      {/* ID line */}
      <div className="px-2 pb-1 font-mono text-xs text-gray-400">{it.id}</div>
    </div>
  );
}
