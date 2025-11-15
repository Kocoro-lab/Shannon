"use client";

import React, { useEffect, useRef } from 'react';
import { RING_COUNT, RADAR_CURVE_AMOUNT, RADAR_MAX_TURNS, RADAR_WOBBLE, RADAR_PULSE_DURATION_MS, RADAR_PULSE_MAX_RADIUS, RADAR_PULSE_WIDTH, RADAR_PULSE_SECONDARY, RADAR_REFRESH_HZ, RADAR_PING_VOLUME, RADAR_PING_AUDIO_PATH } from '@/lib/constants';
import { appStore } from '@/lib/store';
import { createRNG } from '@/lib/rng';

function clampDPR(dpr: number) { return Math.min(2, Math.max(1, dpr || 1)); }

// Story script configuration for typing/deleting animation in the radar message box
type StoryLine = {
  text: string;
  pauseAfterMs?: number;
  // Optional per-line cursor overrides
  cursorChar?: string;
  blinkMs?: number;
};
type CursorConfig = { char: string; blinkMs: number };
type StoryConfig = {
  lines: StoryLine[];
  typingMsPerChar: number;    // speed while typing
  deletingMsPerChar: number;  // speed while deleting
  betweenLinesDelayMs: number; // delay after deleting before next line starts
  startDelayMs?: number;      // initial delay before the story starts
  loop?: boolean;             // loop back to the start after last line
  cursor: CursorConfig;       // default blinking end character
};

// Default story/tutorial script. Tweak text and timings here.
const STORY: StoryConfig = {
  lines: [
    { text: "Initializing agent traffic control..", pauseAfterMs: 3000, cursorChar: '.', blinkMs: 500 },
    { text: "Connecting to ATC radio..", pauseAfterMs: 3200, cursorChar: '.', blinkMs: 300 },
    { text: "Connecting ambient music..", pauseAfterMs: 3200, cursorChar: '.', blinkMs: 600 },
    { text: "Enjoy the vibes of agents working..", pauseAfterMs: 4200, cursorChar: '.', blinkMs: 450 },
    { text: "Play music below; switch between different ATC locations...", pauseAfterMs: 5200, cursorChar: '\\/', blinkMs: 500 },
  ],
  typingMsPerChar: 5,
  deletingMsPerChar: 14,
  betweenLinesDelayMs: 450,
  startDelayMs: 350,
  loop: false,
  cursor: { char: "|", blinkMs: 500 },
};

export default function RadarCanvas({ workflowId }: { workflowId?: string }) {
  const ref = useRef<HTMLCanvasElement | null>(null);
  // current text to draw in the lower-right message box (updated by story engine)
  const currentTextRef = useRef<string>("");
  // audio element for radar ping
  const pingAudioRef = useRef<HTMLAudioElement | null>(null);
  // Story engine state (kept in refs so we can update from rAF without rerenders)
  const storyCfgRef = useRef<StoryConfig>(STORY);
  const storyLineIdxRef = useRef<number>(0);
  const storyCharIdxRef = useRef<number>(0);
  const storyPhaseRef = useRef<"idle" | "typing" | "pause" | "deleting" | "between">("idle");
  const nextEventAtRef = useRef<number>(0); // timestamp for next char or phase transition
  const lastBlinkAtRef = useRef<number>(0);
  const cursorVisibleRef = useRef<boolean>(true);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Prepare ping audio element once on mount
    try {
      const a = new Audio(RADAR_PING_AUDIO_PATH);
      a.preload = 'auto';
      a.volume = RADAR_PING_VOLUME;
      // Some browsers require user gesture; we will only play after toggle
      pingAudioRef.current = a;
    } catch {}

    let wCss = 0, hCss = 0, cx = 0, cy = 0, r = 0;
    const isVisibleRef = { current: false } as { current: boolean };

    function resize() {
      if (!canvas || !ctx) return;
      const dpr = clampDPR(window.devicePixelRatio || 1);
      const rect = canvas.getBoundingClientRect();
      wCss = Math.max(200, rect.width);
      hCss = Math.max(200, rect.height);
      // Consider this canvas visible for audio purposes only if it actually has layout space
      isVisibleRef.current = rect.width > 0 && rect.height > 0;
      canvas.width = Math.floor(wCss * dpr);
      canvas.height = Math.floor(hCss * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      drawStatic(wCss, hCss);
      cx = wCss / 2; cy = hCss / 2; r = Math.min(wCss, hCss) * 0.45;
    }

    function drawStatic(w: number, h: number) {
      if (!canvas || !ctx) return;
      ctx.clearRect(0, 0, w, h);
      // Background outside the radar circle: off-black green
      const OUT_BG = '#050c0aff';
      ctx.fillStyle = OUT_BG;
      ctx.fillRect(0, 0, w, h);

      // center + radius
      const cx = w / 2;
      const cy = h / 2;
      const r = Math.min(w, h) * 0.45;

      // Subtle background grid (outside circle): draw across, then cover inside with circle fill
      // Tweak these constants to change dotted appearance
      const GRID_COLOR = 'rgba(60, 120, 90, 0.25)';
      const GRID_STROKE_WIDTH = 1.25; // thickness of the dot/stroke
      const GRID_DOT_LEN = 3;         // length of the drawn segment (dot)
      const GRID_GAP_LEN = 4;         // length of the gap between dots
      ctx.strokeStyle = GRID_COLOR;
      ctx.lineWidth = GRID_STROKE_WIDTH;
      ctx.lineCap = 'round';
      ctx.setLineDash([GRID_DOT_LEN, GRID_GAP_LEN]);
      // Horizontal lines: 4 evenly spaced (skip edges)
      for (let i = 1; i <= 4; i++) {
        const yy = (h * i) / 5;
        ctx.beginPath();
        ctx.moveTo(0, yy);
        ctx.lineTo(w, yy);
        ctx.stroke();
      }
      // Vertical lines: 2 evenly spaced (thirds)
      for (let i = 1; i <= 2; i++) {
        const xx = (w * i) / 3;
        ctx.beginPath();
        ctx.moveTo(xx, 0);
        ctx.lineTo(xx, h);
        ctx.stroke();
      }

      // Reset dashes so rings and crosshair are solid
      ctx.setLineDash([]);
      ctx.lineCap = 'butt';

      // Fill inside the radar circle with pure black to mask grid within
      ctx.beginPath();
      ctx.arc(cx, cy, r, 0, Math.PI * 2);
      ctx.fillStyle = '#000000';
      ctx.fill();

      // Border ticks around the radar (like minute indicators)
      // Controls for width, spacing (count), and height (length)
      const BORDER_TICK_STROKE_WIDTH = 1.5; // line width
      const BORDER_TICK_COUNT = 500;          // spacing: number of ticks around circle
      const BORDER_TICK_LENGTH = Math.max(2.5, r * 0.025); // line height/length
      const BORDER_TICK_OFFSET = 2;          // small outward offset from circle edge
      ctx.strokeStyle = 'rgba(60, 120, 90, 0.25)'; // same as GRID_COLOR
      ctx.lineWidth = BORDER_TICK_STROKE_WIDTH;
      ctx.lineCap = 'butt';
      for (let i = 0; i < BORDER_TICK_COUNT; i++) {
        const a = (i / BORDER_TICK_COUNT) * Math.PI * 2;
        const x0 = cx + Math.cos(a) * (r + BORDER_TICK_OFFSET);
        const y0 = cy + Math.sin(a) * (r + BORDER_TICK_OFFSET);
        const x1 = cx + Math.cos(a) * (r + BORDER_TICK_OFFSET + BORDER_TICK_LENGTH);
        const y1 = cy + Math.sin(a) * (r + BORDER_TICK_OFFSET + BORDER_TICK_LENGTH);
        ctx.beginPath();
        ctx.moveTo(x0, y0);
        ctx.lineTo(x1, y1);
        ctx.stroke();
      }

      // rings
      ctx.strokeStyle = 'rgba(255,255,255,0.08)';
      ctx.lineWidth = 1;
      for (let i = 1; i <= RING_COUNT; i++) {
        const rr = (r * i) / RING_COUNT;
        ctx.beginPath();
        ctx.arc(cx, cy, rr, 0, Math.PI * 2);
        ctx.stroke();
      }

      // tick marks (simple cross)
      ctx.strokeStyle = 'rgba(255,255,255,0.12)';
      ctx.beginPath();
      ctx.moveTo(cx - r, cy); ctx.lineTo(cx + r, cy);
      ctx.moveTo(cx, cy - r); ctx.lineTo(cx, cy + r);
      ctx.stroke();

      // center checkbox glyph
      const box = Math.max(10, r * 0.06);
      ctx.strokeStyle = 'rgba(255,255,255,0.4)';
      ctx.lineWidth = 1.5;
      ctx.strokeRect(cx - box / 2, cy - box / 2, box, box);

      // Lower-right message box (dynamic story text)
      const baseText = (currentTextRef.current || '');
      const effectiveLine = storyCfgRef.current.lines[(storyLineIdxRef.current || 0) % Math.max(1, storyCfgRef.current.lines.length)] || { text: '' };
      const effCursorChar = (effectiveLine.cursorChar ?? storyCfgRef.current.cursor.char) || '';
      const isPause = storyPhaseRef.current === 'pause';
      const textToDraw = baseText.trim().length > 0 ? baseText : '—';
      const measureText = isPause ? textToDraw + effCursorChar : textToDraw;
      const padX = 8, padY = 6;
      const margin = 10;
      const isMobile = (typeof window !== 'undefined' && window.innerWidth <= 640);
      const fontSize = isMobile ? 10 : 12;
      ctx.font = `${fontSize}px ui-monospace, SFMono-Regular, Menlo, monospace`;
      const metrics = ctx.measureText(measureText);
      const maxTextWidth = isMobile ? w - margin * 2 - padX * 2 : Math.max(60, w * 0.5);
      const tw = Math.min(Math.max(metrics.width, 60), maxTextWidth);
      const th = fontSize + 6;
      const boxW = tw + padX * 2;
      const boxH = th + padY * 2;
      const bx = w - boxW - margin;
      const by = h - boxH - margin;
      // box background
      ctx.fillStyle = 'rgba(0, 0, 0, 0.6)';
      ctx.fillRect(bx, by, boxW, boxH);
      // border
      ctx.strokeStyle = 'rgba(255,255,255,0.2)';
      ctx.lineWidth = 1;
      ctx.strokeRect(bx, by, boxW, boxH);
      // text + non-shifting cursor draw
      ctx.fillStyle = '#fff';
      const tx = bx + padX;
      const ty = by + padY + fontSize;
      // draw base text
      ctx.textAlign = 'start';
      ctx.fillText(textToDraw, tx, ty);
      // draw cursor char in-place (invisible when blink-off) so width doesn't shift
      if (isPause && effCursorChar) {
        const baseW = ctx.measureText(textToDraw).width;
        const prevAlpha = ctx.globalAlpha;
        ctx.globalAlpha = cursorVisibleRef.current ? 1 : 0;
        ctx.fillText(effCursorChar, tx + baseW, ty);
        ctx.globalAlpha = prevAlpha;
      }
    }

    // Track previous status per item to detect transitions to 'done'
    const prevItemStatus = new Map<string, string>();
    
    resize();
    // Initialize previous statuses to avoid pulsing existing done items on mount
    {
      const init = appStore.getState();
      for (const it of Object.values(init.items)) prevItemStatus.set(it.id, it.status);
    }
    let obs: ResizeObserver | null = null;
    if (typeof ResizeObserver !== 'undefined') {
      obs = new ResizeObserver(resize);
      obs.observe(canvas);
    } else {
      window.addEventListener('resize', resize);
    }
    // Simple animation loop to draw in-progress agents (throttled to RADAR_REFRESH_HZ)
    let rafId: number | null = null;
    const intervalMs = Math.max(1, Math.floor(1000 / Math.max(1, RADAR_REFRESH_HZ)));
    let lastDrawAt = 0;
    function angleForAgent(agentId: string) {
      // Base starting angle, deterministic per agent
      const rng = createRNG(agentId + ':angle');
      return rng.next() * Math.PI * 2;
    }

    // Smooth easing for spiral growth
    function easeInOutSine(t: number) {
      return 0.5 - 0.5 * Math.cos(Math.PI * Math.max(0, Math.min(1, t)));
    }

    // Compute a curved angle offset for an agent at progress t in [0,1]
    function curvedAngleOffset(agentId: string, t: number) {
      const k = Math.max(0, Math.min(1, RADAR_CURVE_AMOUNT));
      if (k <= 0) return 0;
      const rng = createRNG(agentId + ':curve');
      const spiralDir = rng.bool() ? 1 : -1; // clockwise vs counter-clockwise
      // Random wobble frequencies and phases (few direction changes only)
      const f1 = rng.float(0.6, 1.6); // cycles over [0..1]
      const f2 = rng.float(1.2, 2.4);
      const p1 = rng.float(0, Math.PI * 2);
      const p2 = rng.float(0, Math.PI * 2);
      const maxSpin = (RADAR_MAX_TURNS * 2 * Math.PI) || (0.5 * 2 * Math.PI);
      const e = easeInOutSine(t);
      // Allocate some of the curve budget to monotone spiral, some to wobble
      const wobblePortion = Math.max(0, Math.min(1, RADAR_WOBBLE));
      const spiralPortion = 1 - wobblePortion;
      const spiral = spiralDir * (k * spiralPortion) * maxSpin * e;
      const wobbleAmp = (k * wobblePortion) * maxSpin * 0.6; // keep wobble within 60% of budget
      // Wobble can change directions; envelope keeps it smooth
      const wobble = wobbleAmp * (
        Math.sin(2 * Math.PI * f1 * t + p1) * 0.7 +
        Math.sin(2 * Math.PI * f2 * t + p2) * 0.3
      ) * (0.85 + 0.15 * e);
      let s = spiral + wobble;
      // Never exceed the half-rotation cap
      if (s > maxSpin) s = maxSpin;
      if (s < -maxSpin) s = -maxSpin;
      return s;
    }

    // Position on curved path at progress t in [0,1]
    function pathPoint(agentId: string, t: number) {
      const theta0 = angleForAgent(agentId);
      const theta = theta0 + curvedAngleOffset(agentId, t);
      const rad = r * (1 - t);
      const x = cx + Math.cos(theta) * rad;
      const y = cy + Math.sin(theta) * rad;
      return { x, y, theta, rad };
    }
    // Per-agent trailing stroke segments emitted at the agent's position.
    type TrailSeg = { x: number; y: number; dir: number; created: number };
    const trails = new Map<string, { segs: TrailSeg[]; lastEmit: number; lastX: number; lastY: number }>();
    // Tuning knobs for the trailing stroke style
    const TRAIL_STROKE_LEN = 3;   // pixels: length of each stroke
    const TRAIL_STROKE_WIDTH = 2;  // pixels: width of each stroke
    const TRAIL_GAP_PX = 8;        // pixels: minimum distance moved before emitting next stroke
    
    const MAX_SEGS = 60;           // cap per-agent segments
    const LIFESPAN_MS = 2000;      // ms before a stroke fades out fully

    // Center pulses when agents complete
    type Pulse = { created: number };
    const pulses: Pulse[] = [];
    // Track items we've already emitted a pulse for (by item id)
    const pulsedItems = new Set<string>();
    // Track items that have already produced a sound effect to avoid double audio
    const soundedItems = new Set<string>();
    // Track items that already played a quiet start-of-flight ping
    const startedSounded = new Set<string>();
    // Centralized pulse emitter that also plays audio if enabled
    function emitPulse(created: number, itemId?: string, playSound: boolean = true) {
      pulses.push({ created });
      // If radar ping sound is enabled, play the short ping only once per itemId
      try {
        const enable = appStore.getState().pingAudioEnabled;
        if (playSound && enable && pingAudioRef.current && isVisibleRef.current) {
          if (!itemId) {
            // No item id context; play normally
            try { pingAudioRef.current.currentTime = 0; } catch {}
            void pingAudioRef.current.play().catch(() => {});
          } else if (!soundedItems.has(itemId)) {
            soundedItems.add(itemId);
            try { pingAudioRef.current.currentTime = 0; } catch {}
            void pingAudioRef.current.play().catch(() => {});
          }
        }
      } catch {}
    }

    // Play a softer ping without emitting a visual pulse (used on start-of-flight)
    function playSoftPing(itemId?: string, volumeScalar = 0.6) {
      try {
        const enable = appStore.getState().pingAudioEnabled;
        if (!enable || !pingAudioRef.current || !isVisibleRef.current) return;
        if (itemId && startedSounded.has(itemId)) return;
        if (itemId) startedSounded.add(itemId);
        const a = pingAudioRef.current;
        const prev = a.volume;
        a.volume = Math.max(0, Math.min(1, (typeof prev === 'number' ? prev : RADAR_PING_VOLUME) * volumeScalar));
        try { a.currentTime = 0; } catch {}
        void a.play().catch(() => {});
        // restore volume shortly after (ping is very short)
        window.setTimeout(() => {
          try { a.volume = prev; } catch {}
        }, 300);
      } catch {}
    }
    function drawAgents() {
      // Throttle by desired refresh rate
      const nowPerf = performance.now();
      if (nowPerf - lastDrawAt < intervalMs) {
        rafId = window.requestAnimationFrame(drawAgents);
        return;
      }
      lastDrawAt = nowPerf;
      // Guard against missing context (TS + runtime safety)
      if (!canvas || !ctx) return;

      // STORY ENGINE: update message text and cursor by time
      const now = Date.now();
      const cfg = storyCfgRef.current;
      const lines = cfg.lines;
      if (lines.length > 0) {
        if (storyPhaseRef.current === 'idle') {
          // Initialize on first run
          storyLineIdxRef.current = 0;
          storyCharIdxRef.current = 0;
          storyPhaseRef.current = 'typing';
          nextEventAtRef.current = now + (cfg.startDelayMs || 0);
          lastBlinkAtRef.current = now;
          cursorVisibleRef.current = true;
        }
        // Blink management (only visible when paused at end of a line)
        const effBlinkMs = Math.max(
          100,
          (lines[storyLineIdxRef.current]?.blinkMs ?? cfg.cursor.blinkMs)
        );
        if (now - lastBlinkAtRef.current >= effBlinkMs) {
          cursorVisibleRef.current = !cursorVisibleRef.current;
          lastBlinkAtRef.current = now;
        }
        const line = lines[storyLineIdxRef.current];
        const full = line.text;
        // Step the phase machine based on time
        if (now >= nextEventAtRef.current) {
          switch (storyPhaseRef.current) {
            case 'typing': {
              if (storyCharIdxRef.current < full.length) {
                storyCharIdxRef.current += 1;
                nextEventAtRef.current = now + Math.max(1, cfg.typingMsPerChar);
              }
              if (storyCharIdxRef.current >= full.length) {
                storyCharIdxRef.current = full.length;
                storyPhaseRef.current = 'pause';
                const pause = typeof line.pauseAfterMs === 'number' ? line.pauseAfterMs : 1000;
                nextEventAtRef.current = now + Math.max(0, pause);
              }
              break;
            }
            case 'pause': {
              // end of pause: begin deleting (if looping) or stay blinking at end
              if (cfg.loop) {
                storyPhaseRef.current = 'deleting';
                nextEventAtRef.current = now + Math.max(1, cfg.deletingMsPerChar);
              } else {
                // Hold final line with blink; schedule next check
                nextEventAtRef.current = now + 200; // keep engine alive for blink
              }
              break;
            }
            case 'deleting': {
              if (storyCharIdxRef.current > 0) {
                storyCharIdxRef.current -= 1;
                nextEventAtRef.current = now + Math.max(1, cfg.deletingMsPerChar);
              }
              if (storyCharIdxRef.current <= 0) {
                storyCharIdxRef.current = 0;
                storyPhaseRef.current = 'between';
                nextEventAtRef.current = now + Math.max(0, cfg.betweenLinesDelayMs);
              }
              break;
            }
            case 'between': {
              // advance to next line
              storyLineIdxRef.current = (storyLineIdxRef.current + 1) % lines.length;
              storyPhaseRef.current = 'typing';
              nextEventAtRef.current = now + Math.max(1, cfg.typingMsPerChar);
              break;
            }
            default:
              break;
          }
        }
        // Build the current display base string (cursor drawn separately to avoid shifting)
        const base = full.slice(0, Math.max(0, Math.min(full.length, storyCharIdxRef.current)));
        currentTextRef.current = base;
      }
      // Redraw static each frame (simple MVP approach)
      drawStatic(wCss, hCss);
      const state = appStore.getState();
      // 'now' already declared above for story engine
      const c = ctx as CanvasRenderingContext2D;

      // Arrow styling knobs
      const ARROW_CORNER_RADIUS = 1;    // rounded corners on arrow polygon
      const ARROW_SIZE = 1.25;           // overall arrow scale (length & width)
      const ARROW_NOTCH_RATIO = -0.25;   // how deep the rear notch is, relative to length
      // Pull arrow color from global CSS variable --green-bright (fallback to #57ff7a)
      function cssVar(name: string, fallback: string): string {
        try {
          const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
          return v || fallback;
        } catch {
          return fallback;
        }
      }
      const ARROW_COLOR_HEX = cssVar('--green-bright', '#57ff7a');
      // Label styling knobs
      const LABEL_OFFSET_Y = 25;        // pixels below arrow in screen space
      const LABEL_PAD_X = 4;
      const LABEL_PAD_Y = 2;
      const LABEL_FONT = '10px ui-monospace, SFMono-Regular, Menlo, monospace';

      function fillRoundedPolygon(p: Array<{x:number;y:number}>, r: number) {
        if (p.length === 0) return;
        c.beginPath();
        c.moveTo(p[0].x, p[0].y);
        for (let i = 0; i < p.length; i++) {
          const p1 = p[(i + 1) % p.length];
          const p2 = p[(i + 2) % p.length];
          c.arcTo(p1.x, p1.y, p2.x, p2.y, r);
        }
        c.closePath();
        c.fill();
      }

      function fmtElapsed(ms: number) {
        const total = Math.max(0, Math.round(ms / 1000));
        const m = Math.floor(total / 60);
        const s = total % 60;
        const mm = m.toString().padStart(2, '0');
        const ss = s.toString().padStart(2, '0');
        return `${mm}:${ss}`;
      }

      function drawLabel(x: number, y: number, idLine: string, timeLine: string, colorTop: string) {
        const rx = x; // anchor right edge at the arrow x
        const ty = y + LABEL_OFFSET_Y;
        c.font = LABEL_FONT;
        const m1 = c.measureText(idLine);
        const m2 = c.measureText(timeLine);
        const tw = Math.ceil(Math.max(m1.width, m2.width));
        const th = 12; // approx line height for 10px font
        const lineGap = 2;
        const totalH = th * 2 + lineGap;
        const bw = tw + LABEL_PAD_X * 2;
        const bh = totalH + LABEL_PAD_Y * 2;
        const bx = rx - bw; // align right edge to rx
        const by = ty - bh / 2;
        // background (no border)
        c.fillStyle = 'rgba(0,0,0,0.6)';
        c.fillRect(bx, by, bw, bh);
        // text lines (right-justified)
        c.textAlign = 'right';
        c.fillStyle = colorTop; // top line (agent id)
        c.fillText(idLine, bx + bw - LABEL_PAD_X, by + LABEL_PAD_Y + th - 3);
        c.fillStyle = 'rgba(220,220,220,0.95)'; // bottom line (duration)
        c.fillText(timeLine, bx + bw - LABEL_PAD_X, by + LABEL_PAD_Y + th - 3 + th + lineGap);
        c.textAlign = 'start';
      }

      // Detect newly started items (transition to 'in_progress') and play a soft ping once
      for (const it of Object.values(state.items)) {
        const prev = prevItemStatus.get(it.id);
        if (prev !== 'in_progress' && it.status === 'in_progress') {
          playSoftPing(it.id, 0.6);
        }
      }

      // Detect newly completed items (status transition to 'done') and pulse once
      for (const it of Object.values(state.items)) {
        const prev = prevItemStatus.get(it.id);
        if (prev !== 'done' && it.status === 'done' && !pulsedItems.has(it.id)) {
          // Visual pulse for 'done', but do not play sound here
          emitPulse(now, it.id, false);
          pulsedItems.add(it.id);
        }
        prevItemStatus.set(it.id, it.status);
      }

      // Draw and update trailing strokes before drawing arrows so arrows sit on top
      for (const [agentId, trail] of trails) {
        // purge expired
        trail.segs = trail.segs.filter(s => now - s.created <= LIFESPAN_MS);
        c.save();
        c.lineCap = 'round';
        c.lineWidth = TRAIL_STROKE_WIDTH;
        for (const s of trail.segs) {
          const age = now - s.created;
          const t = Math.max(0, Math.min(1, age / LIFESPAN_MS));
          const alpha = (1 - t) * 1.0; // start fully opaque, fade to 0
          const dx = Math.cos(s.dir) * TRAIL_STROKE_LEN;
          const dy = Math.sin(s.dir) * TRAIL_STROKE_LEN;
          c.beginPath();
          // draw segment behind the movement direction
          c.moveTo(s.x - dx, s.y - dy);
          c.lineTo(s.x, s.y);
          c.strokeStyle = ARROW_COLOR_HEX;
          c.globalAlpha = alpha;
          c.stroke();
        }
        if (trail.segs.length === 0) trails.delete(agentId);
        c.restore();
      }
      for (const agent of Object.values(state.agents)) {
        const item = state.items[agent.work_item_id];
        if (!item || item.status !== 'in_progress') continue;
        // Prefer the logical agent_id from the work item for display;
        // fall back to the internal agent.id (which may include workflow prefix).
        const labelId = (item.agent_id as string) || agent.id;
        const est = Math.max(1, item.estimate_ms || 0);
        let t = 0;
        if (typeof item.eta_ms === 'number' && isFinite(item.eta_ms)) {
          t = 1 - Math.max(0, Math.min(1, item.eta_ms / est));
        } else if (typeof item.started_at === 'number') {
          const elapsed = Date.now() - item.started_at;
          t = Math.max(0, Math.min(1, elapsed / est));
        }
        // Position along the curved path
        const { x, y } = pathPoint(agent.id, t);
        // Heading tangent to the path using a small finite difference
        const eps = 0.002;
        const t0 = Math.max(0, Math.min(1, t - eps));
        const t1 = Math.max(0, Math.min(1, t + eps));
        const p0 = pathPoint(agent.id, t0);
        const p1 = pathPoint(agent.id, t1);
        let dir = Math.atan2(p1.y - p0.y, p1.x - p0.x);
        if (!isFinite(dir)) {
          // Fallback towards center if derivative is degenerate
          dir = Math.atan2(cy - y, cx - x);
        }
        const len = ARROW_SIZE * Math.max(8, Math.min(14, r * 0.05));
        const width = ARROW_SIZE * 1.25 * Math.max(5, Math.min(9, r * 0.03));

        // Emit a trailing stroke segment at the current position with agent heading
        let trail = trails.get(agent.id);
        if (!trail) { trail = { segs: [], lastEmit: 0, lastX: x, lastY: y }; trails.set(agent.id, trail); }
        // Ensure a first segment exists to make trails immediately visible
        if (trail.segs.length === 0) {
          trail.segs.push({ x, y, dir, created: now });
          trail.lastEmit = now;
          trail.lastX = x; trail.lastY = y;
        }
        const movedDx = x - trail.lastX; const movedDy = y - trail.lastY;
        const moved = Math.hypot(movedDx, movedDy);
        if (moved >= TRAIL_GAP_PX) {
          // Emit as many evenly spaced segments as needed to cover the distance
          const ux = movedDx / moved;
          const uy = movedDy / moved;
          const count = Math.min(20, Math.floor(moved / TRAIL_GAP_PX));
          for (let i = 1; i <= count; i++) {
            const sx = trail.lastX + ux * TRAIL_GAP_PX * i;
            const sy = trail.lastY + uy * TRAIL_GAP_PX * i;
            trail.segs.push({ x: sx, y: sy, dir, created: now });
          }
          // Advance last anchor by the distance we filled
          const adv = TRAIL_GAP_PX * count;
          trail.lastX = trail.lastX + ux * adv;
          trail.lastY = trail.lastY + uy * adv;
          trail.lastEmit = now;
          if (trail.segs.length > MAX_SEGS) trail.segs.splice(0, trail.segs.length - MAX_SEGS);
        }
        c.save();
        c.translate(x, y);
        c.rotate(dir);
        c.fillStyle = ARROW_COLOR_HEX;
        c.globalAlpha = 0.9;
        // Arrow polygon with rear notch to suggest wings
        const notch = len * ARROW_NOTCH_RATIO;
        const wing = width * 0.6;
        const pts = [
          { x: len, y: 0 },                // nose
          { x: -len * 0.45, y: wing },     // lower wing
          { x: -(len * 0.45 + notch), y: 0 }, // rear notch inward
          { x: -len * 0.45, y: -wing },    // upper wing
        ];
        fillRoundedPolygon(pts, ARROW_CORNER_RADIUS);
        c.restore();

        // Emit a center pulse once when this item effectively arrives near the center
        if (!pulsedItems.has(item.id)) {
          const nearT = t >= 0.985; // slightly looser than 0.995 to avoid frame-miss
          const dx = x - cx, dy = y - cy;
          const nearR = Math.hypot(dx, dy) <= Math.max(2, r * 0.01); // pixel/radius guard
          if (nearT || nearR) {
            // Arrival pulse at center: emit with sound
            emitPulse(now, item.id, true);
            pulsedItems.add(item.id);
          }
        }

        // Label with agent id and elapsed time below arrow (screen space)
        const elapsedMs = Math.round(t * est);
        drawLabel(x, y, labelId, fmtElapsed(elapsedMs), ARROW_COLOR_HEX);
      }

      // Draw center pulses on top
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
          const alpha = 1.0 - u; // simple fade out
          c.strokeStyle = ARROW_COLOR_HEX;
          c.globalAlpha = alpha * 0.9;
          c.lineWidth = RADAR_PULSE_WIDTH;
          c.beginPath();
          c.arc(0, 0, radius, 0, Math.PI * 2);
          c.stroke();
          if (secMul > 0) {
            c.globalAlpha = Math.max(0, alpha * 0.6);
            c.beginPath();
            c.arc(0, 0, Math.max(0.5, (u * secMul) * maxR), 0, Math.PI * 2);
            c.stroke();
          }
          if (age < dur) remain.push(p);
        }
        c.restore();
        // retain non-finished pulses
        pulses.length = 0;
        pulses.push(...remain);
      }
      rafId = window.requestAnimationFrame(drawAgents);
    }
    rafId = window.requestAnimationFrame(drawAgents);

    return () => {
      if (obs) obs.disconnect();
      else window.removeEventListener('resize', resize);
      if (rafId) cancelAnimationFrame(rafId);
    };
  }, [workflowId]);

  return (
    <div className="h-full min-h-0 flex flex-col">
      <div className="flex-1 min-h-0 border border-gray-800 bg-black">
        <canvas ref={ref} style={{ width: '100%', height: '100%', display: 'block' }} />
      </div>
    </div>
  );
}
