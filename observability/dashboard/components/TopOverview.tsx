"use client";

import React, { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { appStore } from '@/lib/store';

function useMetrics() {
  return useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().metrics,
    () => appStore.getState().metrics,
  );
}

function fmtInt(n?: number) {
  const v = Number.isFinite(n as number) ? (n as number) : 0;
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(v);
}
function fmtUSD(n?: number) {
  const v = Number.isFinite(n as number) ? (n as number) : 0;
  return `$${new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(v)}`;
}

export default function TopOverview({ compact = false, hideCompletion = false }: { compact?: boolean; hideCompletion?: boolean }) {
  const m = useMetrics();

  // Completion over time graph state
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [points, setPoints] = useState<Array<{ t: number; v: number }>>([]);
  const startRef = useRef<number>(Date.now());
  const wasCompleteRef = useRef<boolean>(false);

  // Collect points over time; start at 0%
  useEffect(() => {
    // ensure first point at t=0 is 0
    setPoints([{ t: 0, v: 0 }]);
    const id = window.setInterval(() => {
      const state = appStore.getState();
      const arr = Object.values(state.items || {});
      const hasItems = arr.length > 0;
      const allDone = hasItems && arr.every((it) => it.status === 'done');

      const vRaw = state.metrics?.completion_rate || 0;
      const v = Math.max(0, Math.min(1, vRaw));

      // Detect transition: complete -> not complete (new run). Reset series.
      if (wasCompleteRef.current && !allDone) {
        wasCompleteRef.current = false;
        startRef.current = Date.now();
        setPoints([{ t: 0, v: 0 }]);
        return;
      }

      // While complete, freeze series (do not append new points)
      if (allDone) {
        wasCompleteRef.current = true;
        return;
      }

      // If not running, do not advance time/series
      if (!state.running) {
        return;
      }

      // Not complete: append point using current baseline
      const t = (Date.now() - startRef.current) / 1000;
      setPoints((prev) => {
        const next = prev.concat({ t, v });
        // keep last ~600 points to cap memory
        return next.length > 600 ? next.slice(next.length - 600) : next;
      });
    }, 400);
    return () => clearInterval(id);
  }, []);

  // Reset the series when a fresh snapshot is applied (e.g., switching plans)
  useEffect(() => {
    let prevLast = appStore.getState().lastTickId;
    const unsub = appStore.subscribe((s) => {
      const currentLast = s.lastTickId;
      if (prevLast > 0 && currentLast === 0) {
        wasCompleteRef.current = false;
        startRef.current = Date.now();
        setPoints([{ t: 0, v: 0 }]);
      }
      prevLast = currentLast;
    });
    return () => {
      if (typeof unsub === 'function') unsub();
    };
  }, []);

  const gridCols = compact ? '1fr' : '1fr 1fr 1fr';
  const gapPx = compact ? 6 : 8;
  const headerCls = compact
    ? 'text-sm text-[#d79326ff] pl-2 pr-2 bg-[#130f04ff]'
    : 'text-lg text-[#d79326ff] pl-2 pr-2 bg-[#130f04ff]';
  const labelCls = compact ? 'text-xs' : 'text-md';
  const valueCls = compact ? 'text-base' : 'text-xl';

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: gridCols,
        gap: `${gapPx}px`,
        height: '100%',
        minHeight: 0,
      }}
    >
      {/* Main Metrics card */}
      <div style={{ background: '#000' }}>
        <div className={headerCls}>MAIN METRICS</div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: gapPx, color: '#cfcfcf', padding: '8px' }}>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className={labelCls} style={{ color: '#a0a0a0'}}>ACTIVE AGENTS</div>
            <div className={valueCls} style={{ marginTop: 4 }}>{fmtInt(m.active_agents)}</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className={labelCls} style={{ color: '#a0a0a0'}}>TOTAL TOKENS</div>
            <div className={valueCls} style={{ marginTop: 4 }}>{fmtInt(m.total_tokens)}</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className={labelCls} style={{ color: '#a0a0a0'}}>TOTAL SPEND</div>
            <div className={valueCls} style={{ marginTop: 4 }}>{fmtUSD(m.total_spend_usd)}</div>
          </div>
        </div>
      </div>

      {/* Live Throughput card */}
      <div style={{ background: '#000' }}>
        <div className={headerCls}>LIVE THROUGHPUT</div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: gapPx, color: '#cfcfcf', padding: '8px' }}>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className={labelCls} style={{ color: '#a0a0a0'}}>TOKENS / SEC</div>
            <div className={valueCls} style={{ marginTop: 4 }}>{fmtInt(m.live_tps)}</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className={labelCls} style={{ color: '#a0a0a0'}}>SPEND / SEC</div>
            <div className={valueCls} style={{ marginTop: 4 }}>{fmtUSD(m.live_spend_per_s)}</div>
          </div>
        </div>
      </div>

      {/* Project Completion Rate card (optionally hidden on mobile) */}
      {hideCompletion ? null : (
        <div style={{ background: '#000', display: 'flex', flexDirection: 'column', minHeight: 0 }}>
          <div className={headerCls}>COMPLETION RATE</div>
          <div style={{ padding: '8px', flex: 1, minHeight: 0, height: compact ? 120 : undefined }}>
            <Chart points={points} containerRef={containerRef} />
          </div>
        </div>
      )}
    </div>
  );
}

function Chart({ points, containerRef }: { points: Array<{ t: number; v: number }>; containerRef: React.RefObject<HTMLDivElement | null> }) {
  const [size, setSize] = useState({ w: 300, h: 120 });
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const r = new ResizeObserver(() => setSize({ w: el.clientWidth, h: el.clientHeight }));
    r.observe(el);
    setSize({ w: el.clientWidth, h: el.clientHeight });
    return () => r.disconnect();
  }, [containerRef]);
  const pad = 12; // general padding
  const w = Math.max(10, size.w);
  const h = Math.max(10, size.h);
  const span = Math.max(5, points.length ? points[points.length - 1].t : 5);
  // Dynamic Y max scales with current progress so small movements are visible
  const yMax = React.useMemo(() => {
    let m = 0;
    for (const p of points) if (Number.isFinite(p.v) && p.v > m) m = p.v;
    return m;
  }, [points]);
  const yMaxClamp = Math.max(0.0001, yMax);
  const yTopPct = Math.max(1, Math.round(yMaxClamp * 100));
  const d = React.useMemo(() => {
    if (!points.length) return '';
    const coords = points.map(({ t, v }) => {
      const x = pad + (t / span) * Math.max(1, w - pad * 2);
      const vy = Math.max(0, Math.min(1, v / yMaxClamp));
      const y = pad + (1 - vy) * Math.max(1, h - pad * 2);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    return `M ${coords.join(' L ')}`;
  }, [points, w, h, span, yMaxClamp]);
  return (
    <div ref={containerRef} style={{ position: 'relative', width: '100%', height: '100%' }}>
      <svg width="100%" height="100%" style={{ display: 'block' }}>
        {/* Axes */}
        <line x1={pad} y1={h - pad} x2={w - pad} y2={h - pad} stroke="#555" strokeWidth={1} />
        <line x1={pad} y1={pad} x2={pad} y2={h - pad} stroke="#555" strokeWidth={1} />
        {/* X ticks and moving time label */}
        {(() => {
          const ticks: React.ReactElement[] = [];
          // Choose a friendly tick interval
          const tick = span <= 10 ? 1 : span <= 30 ? 5 : span <= 120 ? 10 : 30;
          for (let t = 0; t <= span + 0.001; t += tick) {
            const x = pad + (t / span) * Math.max(1, w - pad * 2);
            ticks.push(
              <g key={`xt-${t.toFixed(2)}`}>
                {/* Keep ticks and labels inside the chart area to avoid clipping */}
                <line x1={x+10} y1={h} x2={x+10} y2={h - pad } stroke="#555" strokeWidth={1} />
                <text x={x+10} y={h - pad + 10} fill="#777" fontSize={9} textAnchor="middle">{Math.round(t)}s</text>
              </g>
            );
          }
          // Moving time label at right end
          const xr = w - pad;
          return (
            <g key="time-label">
              {ticks}
              <text x={xr} y={h - pad - 16} fill="#aaa" fontSize={10} textAnchor="end">{Math.round(span)}s</text>
            </g>
          );
        })()}
        {/* Axis labels */}
        <text x={w / 2} y={h} textAnchor="middle" fill="#888" fontSize={10}>Duration</text>
        {/* <text x={pad - 4} y={h / 2} fill="#888" fontSize={10} transform={`rotate(-90 ${pad - 4},${h / 2})`} textAnchor="middle">Progress</text> */}
        {/* 0% and dynamic-top markers */}
        <text x={pad + 2} y={h - pad - 2} fill="#666" fontSize={9}>0%</text>
        <text x={pad + 2} y={pad + 9} fill="#666" fontSize={9}>{yTopPct}%</text>
        {/* Path */}
        <path d={d} fill="none" stroke="var(--row-queued-fg)" strokeWidth={2} />
      </svg>
    </div>
  );
}
