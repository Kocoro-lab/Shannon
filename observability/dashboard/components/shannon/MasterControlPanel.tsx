
'use client';

import { useSyncExternalStore } from 'react';
import { TimelineStatusBar } from './TimelineStatusBar';
import { appStore } from '@/lib/store';

export function MasterControlPanel() {
  // Bind SFX toggle directly to the global app store used by RadarCanvas
  const sfxEnabled = useSyncExternalStore(
    appStore.subscribe,
    () => appStore.getState().pingAudioEnabled,
    () => appStore.getState().pingAudioEnabled,
  );
  const toggleSFX = () => appStore.getState().togglePingAudio();

  return (
    <div className="min-h-0 overflow-hidden flex flex-col">
      {/* Header bar matching MONITORING TABLE style */}
      <div className="flex items-center bg-[#130f04ff]">
        <h2 className="bg-[#c79325] pl-2 pr-2 font-bold text-black">MASTER CONTROL PANEL</h2>
      </div>

      {/* Control panel content */}
      <div className="border border-[#352b19ff] bg-black px-3 py-2">
        <div className="flex items-center justify-between gap-4">
          {/* Left side: Timeline status */}
          <div className="flex-1">
            <TimelineStatusBar />
          </div>

          {/* Right side: SFX toggle only (background music removed) */}
          <div className="flex items-center gap-6">
            {/* SFX Toggle */}
            <div className="flex flex-col items-center gap-1">
              <div className="text-xs text-[#c89225ff] uppercase tracking-[0.2em]">SFX</div>
              <button
                type="button"
                onClick={toggleSFX}
                title={sfxEnabled ? 'Sound effects: ON' : 'Sound effects: OFF'}
                className={`h-8 w-8 grid place-items-center border ${
                  sfxEnabled
                    ? 'border-[#2ec27e] text-[#2ec27e]'
                    : 'border-[#352b19ff] text-[#666]'
                } bg-black hover:bg-[#0a0a0a]`}
                aria-pressed={sfxEnabled}
                aria-label={sfxEnabled ? 'Disable sound effects' : 'Enable sound effects'}
              >
                {sfxEnabled ? (
                  // Speaker with waves (on)
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M3 10h3l4-3v10l-4-3H3z" fill="currentColor" stroke="none" />
                    <path d="M15 9c1.5 1.5 1.5 4.5 0 6" />
                    <path d="M17.5 7c2.5 2.5 2.5 7.5 0 10" />
                  </svg>
                ) : (
                  // Speaker with X (muted)
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M3 10h3l4-3v10l-4-3H3z" fill="currentColor" stroke="none" />
                    <path d="M16 8l6 6" />
                    <path d="M22 8l-6 6" />
                  </svg>
                )}
              </button>
            </div>

            {/* Background music removed */}
          </div>
        </div>
      </div>
    </div>
  );
}
