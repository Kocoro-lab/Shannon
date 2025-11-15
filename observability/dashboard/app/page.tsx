'use client';

import { useEffect, useState } from 'react';
import { TaskSubmitterEnhanced } from '../components/shannon/TaskSubmitterEnhanced';
import { EventStream } from '../components/shannon/EventStream';
import { TopOverview } from '../components/shannon/TopOverview';
import { InsightsPanel } from '../components/shannon/InsightsPanel';
import { GlobalQueue } from '../components/shannon/GlobalQueue';
import { ATCRadarPanel } from '../components/shannon/ATCRadarPanel';
import { LLMStreamViewer } from '../components/shannon/LLMStreamViewer';
import { MasterControlPanel } from '../components/shannon/MasterControlPanel';
import { DashboardProvider, useDashboardContext } from '../shannon/dashboardContext';
import { usePlatformTasks } from '../shannon/usePlatformTasks';
import { useMultiSSE } from '../shannon/useMultiSSE';

const SKIP_AUTH = process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH === 'true';

let shellRenderCount = 0;

function DashboardShell() {
  shellRenderCount++;
  console.log(`[DashboardShell] RENDER #${shellRenderCount}`);

  const [apiKey, setApiKey] = useState('');
  const [tempApiKey, setTempApiKey] = useState('');
  const [showApiKeyModal, setShowApiKeyModal] = useState(false);
  const { ingestEvent, reset } = useDashboardContext();

  // Poll for active workflows across the platform
  const { activeWorkflowIds } = usePlatformTasks(apiKey, {
    pollInterval: 15000, // 15 seconds to avoid rate limiting
    includeStatuses: ['RUNNING', 'QUEUED'],
  });

  // Subscribe to SSE streams for all active workflows
  const { events, status, error } = useMultiSSE(activeWorkflowIds, apiKey, {
    maxEventsPerWorkflow: 200,
    onEvent: ingestEvent,
  });

  // Reset dashboard state once on mount
  useEffect(() => {
    reset();
  }, [reset]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const stored = window.sessionStorage.getItem('api_key');
    if (stored) {
      setApiKey(stored);
    } else if (!SKIP_AUTH) {
      setShowApiKeyModal(true);
    }
  }, []);

  const handleSaveApiKey = (event: React.FormEvent) => {
    event.preventDefault();
    if (!tempApiKey.trim()) return;
    setApiKey(tempApiKey.trim());
    if (typeof window !== 'undefined') {
      window.sessionStorage.setItem('api_key', tempApiKey.trim());
    }
    setTempApiKey('');
    setShowApiKeyModal(false);
  };

  const _clearApiKey = () => {
    setApiKey('');
    if (typeof window !== 'undefined') {
      window.sessionStorage.removeItem('api_key');
    }
    reset();
    if (!SKIP_AUTH) {
      setShowApiKeyModal(true);
    }
  };

  return (
    <div className="h-screen overflow-hidden bg-black text-white flex flex-col">
      <header className="p-4">
        <div className="flex flex-col">
          <h1 className="text-2xl tracking-tighter font-bold text-gray-500">AGENT TRAFFIC CONTROL</h1>
          <div className="mt-1 flex items-baseline gap-2">
            <p className="text-[10px] leading-none text-gray-500">Shannon Observability Dashboard</p>
            {SKIP_AUTH && (
              <span className="text-[10px] leading-none text-[#f87171]">(Skip Auth Mode)</span>
            )}
            <a href="/tasks" className="ml-4 text-[10px] leading-none text-[#c79325] underline">Tasks & Audit</a>
          </div>
        </div>
      </header>
      <div className="border-t border-gray-800" />


      <div className="flex-1 overflow-hidden p-4">
        <div className="grid grid-cols-1 xl:grid-cols-[28%_72%] gap-4 h-full">
          {/* Left Column: Task Submission & Event Log */}
          <section className="grid grid-rows-[auto_1fr] gap-4 min-h-0 min-w-0 overflow-hidden">
            {/* Task Submitter */}
            <div className="border border-[#352b19] bg-black">
              <TaskSubmitterEnhanced apiKey={apiKey} />
            </div>

            {/* Event Stream Log (Platform Timeline) */}
            <div className="border border-[#352b19] bg-black flex flex-col min-h-0 min-w-0">
              <div className="ui-label ui-label--tab">Platform Event Timeline</div>
              <div className="flex-1 min-h-0 min-w-0">
                <EventStream
                  events={events}
                  status={status}
                  error={error}
                />
              </div>
            </div>
          </section>

          {/* Right Column: Visualizations */}
          <aside className="grid grid-rows-[auto_auto_1fr_auto] gap-4 min-h-0 overflow-hidden">
            <TopOverview />
            <InsightsPanel />

            {/* ATC Radar & LLM Output split */}
            <div className="grid grid-cols-[18%_40%_42%] gap-4 min-h-0">
              <GlobalQueue />
              <ATCRadarPanel events={events} />
              <div className="border border-[#352b19] bg-black flex flex-col min-h-0">
                <LLMStreamViewer events={events} />
              </div>
            </div>

            <MasterControlPanel />
          </aside>
        </div>
      </div>

      {showApiKeyModal && (
        <div className="fixed inset-0 bg-black/80 backdrop-blur-sm flex items-center justify-center z-50 p-6">
          <form onSubmit={handleSaveApiKey} className="bg-black border border-[#352b19] px-6 py-6 w-full max-w-md space-y-4">
            <h2 className="text-lg uppercase tracking-[0.3em] text-[#c79325]">API Key Required</h2>
            <p className="text-xs text-[#9fa6b2] uppercase tracking-[0.2em]">
              Enter your API key to access Shannon. Stored locally until you clear it.
            </p>
            <input
              type="password"
              className="w-full bg-[#050505] border border-[#1b1407] text-sm text-[#d5d5d5] px-3 py-2 font-mono"
              placeholder="sk_..."
              value={tempApiKey}
              onChange={(e) => setTempApiKey(e.target.value)}
              autoFocus
            />
            <div className="flex gap-3">
              <button
                type="submit"
                className="flex-1 bg-[#c79325] text-black uppercase tracking-[0.3em] py-2"
              >
                Save Key
              </button>
              <button
                type="button"
                className="flex-1 border border-[#352b19] uppercase tracking-[0.3em] py-2 text-[#c79325]"
                onClick={() => {
                  setTempApiKey('');
                  setShowApiKeyModal(false);
                }}
              >
                Cancel
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

export default function Home() {
  return (
    <DashboardProvider>
      <DashboardShell />
    </DashboardProvider>
  );
}
