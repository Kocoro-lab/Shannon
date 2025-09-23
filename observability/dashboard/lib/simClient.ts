"use client";

import { createSimBridge, type SimBridge } from '@/lib/simBridge';
import { attachBridgeToStore } from '@/lib/bridgeToStore';
import { appStore } from '@/lib/store';
import { debugLog } from '@/lib/debug';

let _bridge: SimBridge | null = null;
let _worker: Worker | null = null;
let _link: { destroy: () => void } | null = null;

export function isConnected() {
  return !!_bridge;
}

export function ensureConnected() {
  if (_bridge) return _bridge;
  if (typeof window === 'undefined' || typeof Worker === 'undefined') return null;
  try {
    _worker = new Worker(new URL('../workers/engine.ts', import.meta.url), { type: 'module' });
    debugLog('simClient', 'worker-created');
    _worker.addEventListener('error', (e: ErrorEvent) => debugLog('simClient', 'worker-error', e?.message || e));
    _worker.addEventListener('message', (e: MessageEvent) => debugLog('simClient', 'worker-message', e?.data));
    _bridge = createSimBridge(_worker);
    _link = attachBridgeToStore(_bridge, appStore);
    // initial handshake
    _bridge.postIntent({ type: 'request_snapshot' });
    return _bridge;
  } catch (e) {
    debugLog('simClient', 'ensureConnected-failed', e);
    return null;
  }
}

export function setExternalBridge(bridge: SimBridge, worker: Worker, link: { destroy: () => void }) {
  if (_bridge) return; // already connected
  _bridge = bridge; _worker = worker; _link = link;
}

export function postIntent(intent: Parameters<SimBridge['postIntent']>[0]) {
  if (!_bridge) ensureConnected();
  _bridge?.postIntent(intent);
}

export function destroyConnection() {
  _link?.destroy();
  try { _worker?.terminate(); } catch {}
  _link = null; _bridge = null; _worker = null;
}

