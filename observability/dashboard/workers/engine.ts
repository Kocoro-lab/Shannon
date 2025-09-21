/* eslint-disable @typescript-eslint/no-explicit-any */
// Engine worker: minimal handshake loop (snapshot + ticks)
// Exports tiny pure helpers for tests; only starts the loop when actually
// running in a WorkerGlobalScope (not during Vitest/jsdom imports).

import { DEFAULT_SEED, RUNNING_DEFAULT } from '../lib/config';
import { ENGINE_TICK_HZ, TPS_ALPHA, TPS_TARGET_HOLD_MS_MIN, TPS_TARGET_HOLD_MS_MAX, TPS_JITTER_FRAC } from '@/lib/constants';
import { debugLog } from '../lib/debug';
import { buildItemsFromPlan, detectCycles, promoteQueuedToAssigned, countInProgress, computeMetrics } from '@/lib/engine';
import { getPlanByName, ALL_PLANS, DEFAULT_PLAN_NAME } from '@/plans';
import { createRNG } from '@/lib/rng';
import { MAX_CONCURRENT } from '@/lib/constants';
import type { AppState, ProjectMetrics, Agent, WorkItem } from '../lib/types';

export const ENGINE_WORKER_MODULE_LOADED = true;

export type PlanName = string;

export function zeroMetrics(): ProjectMetrics {
  return {
    active_agents: 0,
    total_tokens: 0,
    total_spend_usd: 0,
    live_tps: 0,
    live_spend_per_s: 0,
    completion_rate: 0,
  };
}

export function makeInitialState(seed: string = DEFAULT_SEED): AppState {
  return {
    items: {},
    agents: {},
    metrics: zeroMetrics(),
    seed,
    running: RUNNING_DEFAULT,
  };
}

export function hzToMs(hz: number): number {
  return hz > 0 ? Math.round(1000 / hz) : 1000 / 30;
}

interface Ctx {
  state: AppState;
  tickId: number;
  running: boolean;
  speed: number; // multiplier (1x default)
  plan: PlanName;
  timer: any;
  tickMs: number;
  rng: ReturnType<typeof createRNG>;
  nextStartAt: number; // epoch ms for next start attempt
  agentCounter: number;
  lastTickAt: number;
}

function postSnapshot(ctx: Ctx) {
  debugLog('worker', 'postSnapshot', { running: ctx.running, tickId: ctx.tickId, seed: ctx.state.seed, plan: ctx.plan });
  ;(self as any).postMessage({ type: 'snapshot', state: ctx.state });
}


function expDelayMs(mean: number, rng: ReturnType<typeof createRNG>) {
  const u = Math.max(1e-6, rng.next());
  return -Math.log(1 - u) * mean;
}

function tryStartOne(ctx: Ctx, now: number, diffs: { items: Partial<WorkItem>[]; agents: Partial<Agent>[] }) {
  const inProg = countInProgress(ctx.state.items);
  if (inProg >= MAX_CONCURRENT) return false;

  const assigned = Object.values(ctx.state.items).filter((i) => i.status === 'assigned');
  if (assigned.length === 0) return false;
  // pick random assigned
  const pick = assigned[Math.floor(ctx.rng.next() * assigned.length)];
  const agentId = `AG${++ctx.agentCounter}`;
  const nowMs = now;
  // minimal agent (positions unused yet)
  ctx.state.agents[agentId] = { id: agentId, work_item_id: pick.id, x: 0, y: 0, v: 0.002, curve_phase: 0 } as Agent;
  pick.status = 'in_progress';
  pick.agent_id = agentId;
  pick.started_at = nowMs;
  // include minimal diffs
  diffs.items.push({ id: pick.id, status: 'in_progress', agent_id: agentId, started_at: nowMs });
  diffs.agents.push({ id: agentId, work_item_id: pick.id } as Partial<Agent>);
  debugLog('worker', 'start_item', { id: pick.id, agent: agentId });
  return true;
}

function stepEngine(ctx: Ctx) {
  const now = Date.now();
  const diffs: { items: Partial<WorkItem>[]; agents: Partial<Agent>[]; agents_remove?: string[] } = { items: [], agents: [] };
  // Simulation delta (seconds)
  const dtSec = Math.max(0, (now - ctx.lastTickAt) / 1000);
  ctx.lastTickAt = now;

  // Promote any newly eligible items
  const newlyAssigned = promoteQueuedToAssigned(ctx.state.items);
  for (const id of newlyAssigned) diffs.items.push({ id, status: 'assigned' });

  // Start cadence: Poisson-like with mean 800ms, obey cap
  if (ctx.running && now >= ctx.nextStartAt) {
    if (tryStartOne(ctx, now, diffs)) {
      // schedule next start
      const mean = 800 / ctx.speed;
      ctx.nextStartAt = now + Math.max(50, expDelayMs(mean, ctx.rng));
    } else {
      // nothing to start; check again soon
      ctx.nextStartAt = now + 300;
    }
  }

  // Per-item TPS target state (held targets to create bursty behavior)
  type TpsState = { target: number; nextAt: number };
  // Keep this map across ticks by hoisting to module scope; fall back to a property on ctx
  // to avoid redeclaring across calls in certain bundlers.
  const tpsMap: Map<string, TpsState> = (stepEngine as any)._tpsMap || new Map<string, TpsState>();
  (stepEngine as any)._tpsMap = tpsMap;

  // Update in-progress items
  for (const it of Object.values(ctx.state.items)) {
    if (it.status !== 'in_progress') {
      // cleanup any lingering state when item leaves in_progress
      if ((it.status === 'done' || it.status === 'queued' || it.status === 'assigned') && tpsMap.has(it.id)) tpsMap.delete(it.id);
      continue;
    }

    // Sample/hold target for a burst window
    let st = tpsMap.get(it.id);
    if (!st || now >= st.nextAt) {
      const range = Math.max(0, it.tps_max - it.tps_min);
      const baseTarget = it.tps_min + range * ctx.rng.next();
      // Hold window scaled by speed so faster sims don't feel too stable
      const holdMs = (TPS_TARGET_HOLD_MS_MIN + (TPS_TARGET_HOLD_MS_MAX - TPS_TARGET_HOLD_MS_MIN) * ctx.rng.next()) / Math.max(0.0001, ctx.speed || 1);
      st = { target: baseTarget, nextAt: now + Math.max(100, Math.floor(holdMs)) };
      tpsMap.set(it.id, st);
    }

    // Small flutter around the held target
    const range = Math.max(0, it.tps_max - it.tps_min);
    const jitterAmp = TPS_JITTER_FRAC * range;
    const jitter = (ctx.rng.next() * 2 - 1) * jitterAmp;
    const targetEffective = Math.max(it.tps_min, Math.min(it.tps_max, st.target + jitter));

    // Exponential smoothing toward target to avoid instant jumps
    const prev = Number.isFinite(it.tps as number) ? (it.tps as number) : it.tps_min;
    let tps = (1 - TPS_ALPHA) * prev + TPS_ALPHA * targetEffective;
    if (!Number.isFinite(tps)) tps = it.tps_min;
    tps = Math.max(it.tps_min, Math.min(it.tps_max, tps));
    it.tps = tps;
    // accumulate tokens
    it.tokens_done += tps * dtSec;
    if (!Number.isFinite(it.tokens_done)) it.tokens_done = 0;
    // eta by elapsed time
    const started = it.started_at ?? now;
    // ETA based on real elapsed wall time
    it.eta_ms = Math.max(0, it.estimate_ms - (now - started));
    diffs.items.push({ id: it.id, tps: it.tps, tokens_done: it.tokens_done, eta_ms: it.eta_ms });
    // completion
    if (it.eta_ms <= 0) {
      it.status = 'done';
      // cleanup state on completion
      if (tpsMap.has(it.id)) tpsMap.delete(it.id);
      const agentId = it.agent_id;
      it.agent_id = undefined;
      diffs.items.push({ id: it.id, status: 'done', agent_id: undefined });
      if (agentId && (ctx.state.agents as any)[agentId]) {
        delete (ctx.state.agents as any)[agentId];
        (diffs.agents_remove ||= []).push(agentId);
      }
    }
  }
  return diffs;
}

function postTick(ctx: Ctx) {
  // apply engine step to build diffs
  const diffs = stepEngine(ctx);
  // metrics
  const metrics = computeMetrics(ctx.state.items as any, ctx.state.agents as any);
  ctx.state.metrics = metrics;
  ctx.tickId += 1;
  debugLog('worker', 'postTick', { tickId: ctx.tickId, items: diffs.items.length, agents: diffs.agents.length, agents_remove: diffs.agents_remove?.length ?? 0 });
  (self as any).postMessage({ type: 'tick', tick_id: ctx.tickId, items: diffs.items.length ? diffs.items : undefined, agents: diffs.agents.length ? diffs.agents : undefined, agents_remove: diffs.agents_remove && diffs.agents_remove.length ? diffs.agents_remove : undefined, metrics });
}


function startLoop(ctx: Ctx) {
  if (ctx.timer) return;
  debugLog('worker', 'startLoop', { tickMs: ctx.tickMs });
  ctx.timer = setInterval(() => {
    if (!ctx.running) return;
    postTick(ctx);
  }, ctx.tickMs);
}

function stopLoop(ctx: Ctx) {
  if (!ctx.timer) return;
  debugLog('worker', 'stopLoop');
  clearInterval(ctx.timer);
  ctx.timer = null;
}

function loadPlan(ctx: Ctx, name: PlanName) {
  // Choose plan by name from registry (fallback to first available)
  const planDef = getPlanByName(name) ?? ALL_PLANS[0];
  const items = buildItemsFromPlan(planDef);
  const cycles = detectCycles(items);
  if (cycles.length) debugLog('worker', 'plan-cycles-detected', { count: cycles.length });
  // Promote initial eligible (no deps) to assigned
  promoteQueuedToAssigned(items);
  ctx.state.items = items;
  ctx.plan = name;
}

function handleIntent(ctx: Ctx, intent: any) {
  switch (intent?.type) {
    case 'set_running': {
      ctx.running = !!intent.running;
      ctx.state.running = ctx.running; // keep snapshot state in sync
      debugLog('worker', 'intent:set_running', { running: ctx.running });
      if (ctx.running) startLoop(ctx); else stopLoop(ctx);
      // Echo state so UI reflects running flag
      postSnapshot(ctx);
      return;
    }
    case 'set_seed': {
      ctx.state.seed = String(intent.seed ?? DEFAULT_SEED);
      // Re-seed RNG and reset start scheduling for determinism
      ctx.rng = createRNG(ctx.state.seed);
      ctx.nextStartAt = Date.now();
      ctx.agentCounter = 0;
      debugLog('worker', 'intent:set_seed', { seed: ctx.state.seed });
      postSnapshot(ctx);
      return;
    }
    case 'set_plan': {
      const name = (intent.plan as PlanName) ?? DEFAULT_PLAN_NAME;
      debugLog('worker', 'intent:set_plan', { plan: name });
      // Load items from plan and pause so user can inspect
      loadPlan(ctx, name);
      ctx.running = false;
      ctx.state.running = false;
      stopLoop(ctx);
      postSnapshot(ctx);
      return;
    }
    case 'set_speed': {
      const s = Number(intent.speed);
      ctx.speed = Number.isFinite(s) && s > 0 ? s : 1;
      debugLog('worker', 'intent:set_speed', { speed: ctx.speed });
      return;
    }
    case 'request_snapshot': {
      debugLog('worker', 'intent:request_snapshot');
      postSnapshot(ctx);
      return;
    }
    default:
      return;
  }
}

function makeCtx(): Ctx {
  return {
    state: makeInitialState(DEFAULT_SEED),
    tickId: 0,
    running: RUNNING_DEFAULT,
    speed: 1,
    plan: DEFAULT_PLAN_NAME,
    timer: null,
    tickMs: hzToMs(ENGINE_TICK_HZ),
    rng: createRNG(DEFAULT_SEED),
    nextStartAt: Date.now(),
    agentCounter: 0,
    lastTickAt: Date.now(),
  };
}

// Detect dedicated worker context without relying on instanceof (not available across browsers)
const IS_DEDICATED_WORKER = typeof self !== 'undefined' && typeof (self as any).postMessage === 'function' && typeof (globalThis as any).window === 'undefined';
debugLog('worker', 'bootstrap', { isDedicatedWorker: IS_DEDICATED_WORKER, ENGINE_TICK_HZ });

if (IS_DEDICATED_WORKER) {
  const ctx = makeCtx();
  // Preload default plan so snapshot includes items for inspection
  loadPlan(ctx, DEFAULT_PLAN_NAME);
  // Post initial snapshot immediately so UI can latch onto state
  postSnapshot(ctx);
  // Start ticking if running by default
  if (ctx.running) startLoop(ctx);

  self.addEventListener('message', (event: MessageEvent) => {
    debugLog('worker', 'message', { data: (event as any).data });
    handleIntent(ctx, (event as any).data);
  });
}
