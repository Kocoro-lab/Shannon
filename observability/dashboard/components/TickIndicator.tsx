"use client";

import { useEffect, useRef, useState } from 'react';
import { createSimBridge, type SimBridge } from '@/lib/simBridge';
import { attachBridgeToStore } from '@/lib/bridgeToStore';
import { appStore } from '@/lib/store';
import { debugLog } from '@/lib/debug';
import { isConnected, setExternalBridge } from '@/lib/simClient';

export default function TickIndicator() {
  const [tick, setTick] = useState(0);
  const [running, setRunning] = useState(false);
  const [wired, setWired] = useState(false);
  const bridgeRef = useRef<SimBridge | null>(null);

  useEffect(() => {
    // If a connection already exists, just subscribe
    if (isConnected()) {
      const unsub = appStore.subscribe((state) => {
        setTick(state.lastTickId ?? 0);
        setRunning(!!state.running);
        debugLog('ui', 'state-update', { tick: state.lastTickId, running: state.running });
      });
      setWired(true);
      return () => unsub?.();
    }

    // Spin up worker and wire bridge on mount (only in browser)
    if (typeof window === 'undefined' || typeof Worker === 'undefined') {
      return; // SSR or non-browser test environment
    }
    let worker: Worker | null = null;
    try {
      worker = new Worker(new URL('../workers/engine.ts', import.meta.url), { type: 'module' });
      debugLog('ui', 'worker-created');
      worker.addEventListener('error', (e: ErrorEvent) => {
        debugLog('ui', 'worker-error', { message: e?.message, filename: e?.filename, lineno: e?.lineno, colno: e?.colno });
      });
      worker.addEventListener('message', (e: MessageEvent) => {
        debugLog('ui', 'worker-native-message', e?.data);
      });
    } catch (e) {
      // Worker not available (tests or older env); bail out
      debugLog('ui', 'worker-create-failed', e);
      return;
    }
    const bridge = createSimBridge(worker);
    debugLog('ui', 'bridge-created');
    bridgeRef!.current = bridge;
    const link = attachBridgeToStore(bridge, appStore);
    setExternalBridge(bridge, worker, link);

    // subscribe to lastTickId
    const unsub = appStore.subscribe((state) => {
      setTick(state.lastTickId ?? 0);
      setRunning(!!state.running);
      debugLog('ui', 'state-update', { tick: state.lastTickId, running: state.running });
    });
    setWired(true);
    // Proactively request a snapshot (handshake)
    bridge.postIntent({ type: 'request_snapshot' });

    return () => {
      unsub?.();
      link.destroy();
      bridge.destroy?.();
      worker?.terminate();
    };
  }, []);

  return (
    <div className="p-4">
      <div className="inline-flex items-center gap-3 bg-black border border-gray-700 px-3 py-2">
        <span className="text-sm text-gray-300">Engine</span>
        <span className="text-xs bg-gray-700 px-2 py-1 text-gray-200">{wired ? 'connected' : 'connecting...'}</span>
        <span className="text-sm font-mono">Tick: {tick}</span>
        <button
          onClick={() => {
            const next = !running;
            // Optimistically reflect new state; worker snapshot will reconcile
            setRunning(next);
            bridgeRef.current?.postIntent({ type: 'set_running', running: next });
            // Nudge a snapshot to ensure fast UI sync if batching delays it
            bridgeRef.current?.postIntent({ type: 'request_snapshot' });
          }}
          className={`text-xs px-2 py-1 border ${running ? 'bg-green-700/30 border-green-600 text-green-200' : 'bg-red-700/30 border-red-600 text-red-200'}`}
        >
          {running ? 'Pause' : 'Run'}
        </button>
      </div>
    </div>
  );
}
