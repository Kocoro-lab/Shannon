'use client';

import { useEffect, useMemo, useState } from 'react';
import { TaskSubmitter } from '../components/shannon/TaskSubmitter';
import { EventStream } from '../components/shannon/EventStream';
import { TopOverview } from '../components/shannon/TopOverview';
import { InsightsPanel } from '../components/shannon/InsightsPanel';
import { GlobalQueue } from '../components/shannon/GlobalQueue';
import { ATCRadarPanel } from '../components/shannon/ATCRadarPanel';
// import { TimelineStatusBar } from '../components/shannon/TimelineStatusBar';
import { MasterControlPanel } from '../components/shannon/MasterControlPanel';
import { DashboardProvider, useDashboardContext } from '../shannon/dashboardContext';
import { useSSE } from '../shannon/useSSE';
import type { TaskSubmitResponse } from '../shannon/types';

const SKIP_AUTH = process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH === 'true';

function DashboardShell() {
  const [apiKey, setApiKey] = useState('');
  const [tempApiKey, setTempApiKey] = useState('');
  const [showApiKeyModal, setShowApiKeyModal] = useState(false);
  const [currentTask, setCurrentTask] = useState<TaskSubmitResponse | null>(null);
  const { ingestEvent, reset } = useDashboardContext();

  const activeWorkflowId = useMemo(
    () => currentTask?.workflow_id || currentTask?.task_id || '',
    [currentTask]
  );

  const { events, status, error } = useSSE(activeWorkflowId, apiKey, {
    maxEvents: 200,
    onEvent: ingestEvent,
  });

  useEffect(() => {
    if (!activeWorkflowId) {
      reset('');
      return;
    }
    reset(activeWorkflowId);
  }, [activeWorkflowId, reset]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const stored = window.sessionStorage.getItem('api_key');
    if (stored) {
      setApiKey(stored);
    } else if (!SKIP_AUTH) {
      setShowApiKeyModal(true);
    }
  }, []);

  const handleTaskSubmitted = (response: TaskSubmitResponse) => {
    setCurrentTask(response);
  };

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
    setCurrentTask(null);
    reset('');
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
          <section className="grid grid-rows-[auto_1fr] gap-4 min-h-0 min-w-0 overflow-hidden">
            <div className="border border-[#352b19] bg-black p-4">
              <TaskSubmitter
                apiKey={apiKey}
                workflowId={activeWorkflowId || ''}
                onTaskSubmitted={handleTaskSubmitted}
              />
            </div>
            <div className="border border-[#352b19] bg-black flex flex-col min-h-0 min-w-0">
              <div className="ui-label ui-label--tab">Event Stream Log</div>
              <div className="flex-1 min-h-0 min-w-0">
                <EventStream
                  workflowId={activeWorkflowId || null}
                  events={events}
                  status={status}
                  error={error}
                />
              </div>
            </div>
          </section>

          <aside className="grid grid-rows-[auto_auto_1fr_auto] gap-4 min-h-0 overflow-hidden">
            <TopOverview />
            <InsightsPanel />
            <div className="grid grid-cols-[18%_82%] gap-4 min-h-0">
              <GlobalQueue />
              <ATCRadarPanel workflowId={activeWorkflowId} events={events} />
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
