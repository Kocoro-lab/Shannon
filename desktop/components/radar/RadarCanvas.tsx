"use client";

import React, { useEffect, useRef } from "react";
import {
  RING_COUNT,
  RADAR_CURVE_AMOUNT,
  RADAR_MAX_TURNS,
  RADAR_WOBBLE,
  RADAR_PULSE_DURATION_MS,
  RADAR_PULSE_MAX_RADIUS,
  RADAR_PULSE_WIDTH,
  RADAR_PULSE_SECONDARY,
  RADAR_REFRESH_HZ,
} from "@/lib/radar/constants";
import { radarStore } from "@/lib/radar/store";
import { createRNG } from "@/lib/radar/rng";

function clampDPR(dpr: number) {
  return Math.min(2, Math.max(1, dpr || 1));
}

export default function RadarCanvas() {
  const ref = useRef<HTMLCanvasElement | null>(null);
  const prevItemStatus = useRef(new Map<string, string>());

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    let wCss = 0,
      hCss = 0,
      cx = 0,
      cy = 0,
      r = 0;

    function resize() {
      if (!canvas || !ctx) return;
      const dpr = clampDPR(window.devicePixelRatio || 1);
      const rect = canvas.getBoundingClientRect();
      wCss = Math.max(100, rect.width);
      hCss = Math.max(100, rect.height);
      canvas.width = Math.floor(wCss * dpr);
      canvas.height = Math.floor(hCss * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      drawStatic(wCss, hCss);
      cx = wCss / 2;
      cy = hCss / 2;
      r = Math.min(wCss, hCss) * 0.42;
    }

    // Detect if dark mode is active
    function isDarkMode(): boolean {
      return document.documentElement.classList.contains("dark");
    }

    function drawStatic(w: number, h: number) {
      if (!canvas || !ctx) return;
      ctx.clearRect(0, 0, w, h);

      const dark = isDarkMode();
      
      // Colors based on theme
      const BG_COLOR = dark ? "#0a0f0a" : "#f8faf8";
      const GRID_COLOR = dark ? "rgba(100, 140, 120, 0.2)" : "rgba(60, 100, 80, 0.15)";
      const RING_COLOR = dark ? "rgba(100, 180, 140, 0.15)" : "rgba(60, 120, 90, 0.2)";
      const TICK_COLOR = dark ? "rgba(100, 180, 140, 0.25)" : "rgba(60, 120, 90, 0.3)";
      const CENTER_COLOR = dark ? "#22c55e" : "#16a34a";

      // Background
      ctx.fillStyle = BG_COLOR;
      ctx.fillRect(0, 0, w, h);

      const cx = w / 2;
      const cy = h / 2;
      const r = Math.min(w, h) * 0.42;

      // Grid lines (subtle)
      ctx.strokeStyle = GRID_COLOR;
      ctx.lineWidth = 1;
      ctx.setLineDash([2, 4]);

      // Horizontal lines
      for (let i = 1; i <= 2; i++) {
        const yy = (h * i) / 3;
        ctx.beginPath();
        ctx.moveTo(0, yy);
        ctx.lineTo(w, yy);
        ctx.stroke();
      }
      // Vertical lines
      for (let i = 1; i <= 2; i++) {
        const xx = (w * i) / 3;
        ctx.beginPath();
        ctx.moveTo(xx, 0);
        ctx.lineTo(xx, h);
        ctx.stroke();
      }
      ctx.setLineDash([]);

      // Radar circle fill
      ctx.beginPath();
      ctx.arc(cx, cy, r, 0, Math.PI * 2);
      ctx.fillStyle = dark ? "#000000" : "#ffffff";
      ctx.fill();

      // Border ticks
      const BORDER_TICK_COUNT = 72;
      const BORDER_TICK_LENGTH = Math.max(2, r * 0.025);
      ctx.strokeStyle = TICK_COLOR;
      ctx.lineWidth = 1;
      for (let i = 0; i < BORDER_TICK_COUNT; i++) {
        const a = (i / BORDER_TICK_COUNT) * Math.PI * 2;
        const x0 = cx + Math.cos(a) * (r + 2);
        const y0 = cy + Math.sin(a) * (r + 2);
        const x1 = cx + Math.cos(a) * (r + 2 + BORDER_TICK_LENGTH);
        const y1 = cy + Math.sin(a) * (r + 2 + BORDER_TICK_LENGTH);
        ctx.beginPath();
        ctx.moveTo(x0, y0);
        ctx.lineTo(x1, y1);
        ctx.stroke();
      }

      // Rings
      ctx.strokeStyle = RING_COLOR;
      ctx.lineWidth = 1;
      for (let i = 1; i <= RING_COUNT; i++) {
        const rr = (r * i) / RING_COUNT;
        ctx.beginPath();
        ctx.arc(cx, cy, rr, 0, Math.PI * 2);
        ctx.stroke();
      }

      // Cross
      ctx.strokeStyle = RING_COLOR;
      ctx.beginPath();
      ctx.moveTo(cx - r, cy);
      ctx.lineTo(cx + r, cy);
      ctx.moveTo(cx, cy - r);
      ctx.lineTo(cx, cy + r);
      ctx.stroke();

      // Center dot
      const box = Math.max(6, r * 0.04);
      ctx.fillStyle = CENTER_COLOR;
      ctx.globalAlpha = 0.6;
      ctx.beginPath();
      ctx.arc(cx, cy, box / 2, 0, Math.PI * 2);
      ctx.fill();
      ctx.globalAlpha = 1;
    }

    resize();
    prevItemStatus.current.clear();
    const init = radarStore.getState();
    for (const it of Object.values(init.items)) {
      prevItemStatus.current.set(it.id, it.status);
    }

    let obs: ResizeObserver | null = null;
    if (typeof ResizeObserver !== "undefined") {
      obs = new ResizeObserver(resize);
      obs.observe(canvas);
    } else {
      window.addEventListener("resize", resize);
    }

    let rafId: number | null = null;
    const intervalMs = Math.max(1, Math.floor(1000 / Math.max(1, RADAR_REFRESH_HZ)));
    let lastDrawAt = 0;

    function angleForAgent(agentId: string) {
      const rng = createRNG(agentId + ":angle");
      return rng.next() * Math.PI * 2;
    }

    function easeInOutSine(t: number) {
      return 0.5 - 0.5 * Math.cos(Math.PI * Math.max(0, Math.min(1, t)));
    }

    function curvedAngleOffset(agentId: string, t: number) {
      const k = Math.max(0, Math.min(1, RADAR_CURVE_AMOUNT));
      if (k <= 0) return 0;
      const rng = createRNG(agentId + ":curve");
      const spiralDir = rng.bool() ? 1 : -1;
      const f1 = rng.float(0.6, 1.6);
      const f2 = rng.float(1.2, 2.4);
      const p1 = rng.float(0, Math.PI * 2);
      const p2 = rng.float(0, Math.PI * 2);
      const maxSpin = (RADAR_MAX_TURNS * 2 * Math.PI) || Math.PI;
      const e = easeInOutSine(t);
      const wobblePortion = Math.max(0, Math.min(1, RADAR_WOBBLE));
      const spiralPortion = 1 - wobblePortion;
      const spiral = spiralDir * (k * spiralPortion) * maxSpin * e;
      const wobbleAmp = (k * wobblePortion) * maxSpin * 0.6;
      const wobble =
        wobbleAmp *
        (Math.sin(2 * Math.PI * f1 * t + p1) * 0.7 +
          Math.sin(2 * Math.PI * f2 * t + p2) * 0.3) *
        (0.85 + 0.15 * e);
      let s = spiral + wobble;
      if (s > maxSpin) s = maxSpin;
      if (s < -maxSpin) s = -maxSpin;
      return s;
    }

    function pathPoint(agentId: string, t: number) {
      const theta0 = angleForAgent(agentId);
      const theta = theta0 + curvedAngleOffset(agentId, t);
      const rad = r * (1 - t);
      const x = cx + Math.cos(theta) * rad;
      const y = cy + Math.sin(theta) * rad;
      return { x, y, theta, rad };
    }

    type TrailSeg = { x: number; y: number; dir: number; created: number };
    const trails = new Map<string, { segs: TrailSeg[]; lastEmit: number; lastX: number; lastY: number }>();
    const TRAIL_STROKE_LEN = 3;
    const TRAIL_STROKE_WIDTH = 2;
    const TRAIL_GAP_PX = 8;
    const MAX_SEGS = 60;
    const LIFESPAN_MS = 2000;

    type Pulse = { created: number };
    const pulses: Pulse[] = [];
    const pulsedItems = new Set<string>();

    function emitPulse(created: number) {
      pulses.push({ created });
    }

    function drawAgents() {
      const nowPerf = performance.now();
      if (nowPerf - lastDrawAt < intervalMs) {
        rafId = window.requestAnimationFrame(drawAgents);
        return;
      }
      lastDrawAt = nowPerf;
      if (!canvas || !ctx) return;

      drawStatic(wCss, hCss);
      const state = radarStore.getState();
      const now = Date.now();
      const c = ctx as CanvasRenderingContext2D;

      const dark = isDarkMode();
      const ARROW_CORNER_RADIUS = 1;
      const ARROW_SIZE = 1.25;
      const ARROW_NOTCH_RATIO = -0.25;
      const ARROW_COLOR = dark ? "#22c55e" : "#16a34a"; // green-500 / green-600
      const LABEL_BG = dark ? "rgba(0,0,0,0.8)" : "rgba(255,255,255,0.9)";
      const LABEL_TEXT_MUTED = dark ? "#a1a1aa" : "#71717a"; // zinc-400 / zinc-500
      const LABEL_OFFSET_Y = 20;
      const LABEL_PAD_X = 4;
      const LABEL_PAD_Y = 2;
      const LABEL_FONT = "9px ui-monospace, SFMono-Regular, Menlo, monospace";

      function fillRoundedPolygon(p: Array<{ x: number; y: number }>, radius: number) {
        if (p.length === 0) return;
        c.beginPath();
        c.moveTo(p[0].x, p[0].y);
        for (let i = 0; i < p.length; i++) {
          const p1 = p[(i + 1) % p.length];
          const p2 = p[(i + 2) % p.length];
          c.arcTo(p1.x, p1.y, p2.x, p2.y, radius);
        }
        c.closePath();
        c.fill();
      }

      function fmtElapsed(ms: number) {
        const total = Math.max(0, Math.round(ms / 1000));
        const m = Math.floor(total / 60);
        const s = total % 60;
        return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
      }

      function drawLabel(x: number, y: number, idLine: string, timeLine: string) {
        const ty = y + LABEL_OFFSET_Y;
        c.font = LABEL_FONT;
        const m1 = c.measureText(idLine);
        const m2 = c.measureText(timeLine);
        const tw = Math.ceil(Math.max(m1.width, m2.width));
        const th = 10;
        const lineGap = 2;
        const totalH = th * 2 + lineGap;
        const bw = tw + LABEL_PAD_X * 2;
        const bh = totalH + LABEL_PAD_Y * 2;
        const bx = x - bw;
        const by = ty - bh / 2;
        c.fillStyle = LABEL_BG;
        c.fillRect(bx, by, bw, bh);
        c.textAlign = "right";
        c.fillStyle = ARROW_COLOR;
        c.fillText(idLine, bx + bw - LABEL_PAD_X, by + LABEL_PAD_Y + th - 2);
        c.fillStyle = LABEL_TEXT_MUTED;
        c.fillText(timeLine, bx + bw - LABEL_PAD_X, by + LABEL_PAD_Y + th - 2 + th + lineGap);
        c.textAlign = "start";
      }

      // Detect newly completed items
      for (const it of Object.values(state.items)) {
        const prev = prevItemStatus.current.get(it.id);
        if (prev !== "done" && it.status === "done" && !pulsedItems.has(it.id)) {
          emitPulse(now);
          pulsedItems.add(it.id);
        }
        prevItemStatus.current.set(it.id, it.status);
      }

      // Draw trails
      for (const [agentId, trail] of trails) {
        trail.segs = trail.segs.filter((s) => now - s.created <= LIFESPAN_MS);
        c.save();
        c.lineCap = "round";
        c.lineWidth = TRAIL_STROKE_WIDTH;
        for (const s of trail.segs) {
          const age = now - s.created;
          const t = Math.max(0, Math.min(1, age / LIFESPAN_MS));
          const alpha = (1 - t) * 1.0;
          const dx = Math.cos(s.dir) * TRAIL_STROKE_LEN;
          const dy = Math.sin(s.dir) * TRAIL_STROKE_LEN;
          c.beginPath();
          c.moveTo(s.x - dx, s.y - dy);
          c.lineTo(s.x, s.y);
          c.strokeStyle = ARROW_COLOR;
          c.globalAlpha = alpha;
          c.stroke();
        }
        if (trail.segs.length === 0) trails.delete(agentId);
        c.restore();
      }

      // Draw agents
      for (const agent of Object.values(state.agents)) {
        const item = state.items[agent.work_item_id];
        if (!item || item.status !== "in_progress") continue;

        const labelId = (item.agent_id as string) || agent.id;
        const est = Math.max(1, item.estimate_ms || 0);
        let t = 0;
        if (typeof item.eta_ms === "number" && isFinite(item.eta_ms)) {
          t = 1 - Math.max(0, Math.min(1, item.eta_ms / est));
        } else if (typeof item.started_at === "number") {
          const elapsed = Date.now() - item.started_at;
          t = Math.max(0, Math.min(1, elapsed / est));
        }

        const { x, y } = pathPoint(agent.id, t);
        const eps = 0.002;
        const t0 = Math.max(0, Math.min(1, t - eps));
        const t1 = Math.max(0, Math.min(1, t + eps));
        const p0 = pathPoint(agent.id, t0);
        const p1 = pathPoint(agent.id, t1);
        let dir = Math.atan2(p1.y - p0.y, p1.x - p0.x);
        if (!isFinite(dir)) dir = Math.atan2(cy - y, cx - x);

        const len = ARROW_SIZE * Math.max(6, Math.min(10, r * 0.04));
        const width = ARROW_SIZE * 1.25 * Math.max(4, Math.min(7, r * 0.025));

        // Trail segments
        let trail = trails.get(agent.id);
        if (!trail) {
          trail = { segs: [], lastEmit: 0, lastX: x, lastY: y };
          trails.set(agent.id, trail);
        }
        if (trail.segs.length === 0) {
          trail.segs.push({ x, y, dir, created: now });
          trail.lastEmit = now;
          trail.lastX = x;
          trail.lastY = y;
        }
        const movedDx = x - trail.lastX;
        const movedDy = y - trail.lastY;
        const moved = Math.hypot(movedDx, movedDy);
        if (moved >= TRAIL_GAP_PX) {
          const ux = movedDx / moved;
          const uy = movedDy / moved;
          const count = Math.min(20, Math.floor(moved / TRAIL_GAP_PX));
          for (let i = 1; i <= count; i++) {
            const sx = trail.lastX + ux * TRAIL_GAP_PX * i;
            const sy = trail.lastY + uy * TRAIL_GAP_PX * i;
            trail.segs.push({ x: sx, y: sy, dir, created: now });
          }
          const adv = TRAIL_GAP_PX * count;
          trail.lastX = trail.lastX + ux * adv;
          trail.lastY = trail.lastY + uy * adv;
          trail.lastEmit = now;
          if (trail.segs.length > MAX_SEGS) trail.segs.splice(0, trail.segs.length - MAX_SEGS);
        }

        // Draw arrow
        c.save();
        c.translate(x, y);
        c.rotate(dir);
        c.fillStyle = ARROW_COLOR;
        c.globalAlpha = 0.9;
        const notch = len * ARROW_NOTCH_RATIO;
        const wing = width * 0.6;
        const pts = [
          { x: len, y: 0 },
          { x: -len * 0.45, y: wing },
          { x: -(len * 0.45 + notch), y: 0 },
          { x: -len * 0.45, y: -wing },
        ];
        fillRoundedPolygon(pts, ARROW_CORNER_RADIUS);
        c.restore();

        // Arrival pulse
        if (!pulsedItems.has(item.id)) {
          const nearT = t >= 0.985;
          const dx = x - cx;
          const dy = y - cy;
          const nearR = Math.hypot(dx, dy) <= Math.max(2, r * 0.01);
          if (nearT || nearR) {
            emitPulse(now);
            pulsedItems.add(item.id);
          }
        }

        // Label
        const elapsedMs = Math.round(t * est);
        drawLabel(x, y, labelId, fmtElapsed(elapsedMs));
      }

      // Draw pulses
      if (pulses.length) {
        const maxR = r * Math.max(0, Math.min(1, RADAR_PULSE_MAX_RADIUS));
        const secMul = Math.max(0, Math.min(2, RADAR_PULSE_SECONDARY));
        c.save();
        c.translate(cx, cy);
        const remain: Pulse[] = [];
        for (const p of pulses) {
          const age = now - p.created;
          const dur = Math.max(1, RADAR_PULSE_DURATION_MS);
          const u = Math.max(0, Math.min(1, age / dur));
          const radius = Math.max(0.5, u * maxR);
          const alpha = 1.0 - u;
          c.strokeStyle = ARROW_COLOR;
          c.globalAlpha = alpha * 0.9;
          c.lineWidth = RADAR_PULSE_WIDTH;
          c.beginPath();
          c.arc(0, 0, radius, 0, Math.PI * 2);
          c.stroke();
          if (secMul > 0) {
            c.globalAlpha = Math.max(0, alpha * 0.6);
            c.beginPath();
            c.arc(0, 0, Math.max(0.5, u * secMul * maxR), 0, Math.PI * 2);
            c.stroke();
          }
          if (age < dur) remain.push(p);
        }
        c.restore();
        pulses.length = 0;
        pulses.push(...remain);
      }

      rafId = window.requestAnimationFrame(drawAgents);
    }

    rafId = window.requestAnimationFrame(drawAgents);

    return () => {
      if (obs) obs.disconnect();
      else window.removeEventListener("resize", resize);
      if (rafId) cancelAnimationFrame(rafId);
    };
  }, []);

  return (
    <canvas
      ref={ref}
      className="w-full h-full block"
    />
  );
}

