'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { listTasks, fetchTaskStatus, getTaskEvents, buildTimeline } from '../../shannon/api';
import type { ListTasksResponse, TaskSummary, TaskStatusResponse, TaskEvent } from '../../shannon/types';

const SKIP_AUTH = process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH === 'true';

// Helper function to truncate task ID (show first 12 and last 8 chars)
function truncateTaskId(taskId: string): string {
  if (!taskId || taskId.length <= 20) return taskId;
  return `${taskId.slice(0, 12)}...${taskId.slice(-8)}`;
}

export default function TasksPage() {
  const [apiKey, setApiKey] = useState('');
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [details, setDetails] = useState<TaskStatusResponse | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [timeline, setTimeline] = useState<TaskEvent[]>([]);
  const [tlLoading, setTlLoading] = useState(false);
  const [tlNotice, setTlNotice] = useState<string | null>(null);
  const [showPrompts, setShowPrompts] = useState(true);
  const [showPartials, setShowPartials] = useState(true);
  const [showToolObs, setShowToolObs] = useState(true);

  const limit = 20;
  const offset = useMemo(() => page * limit, [page]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const stored = window.sessionStorage.getItem('api_key');
    if (stored) setApiKey(stored);
  }, []);

  useEffect(() => {
    let mounted = true;
    setLoading(true);
    setError(null);
    listTasks({ limit, offset }, apiKey)
      .then((res: ListTasksResponse) => {
        if (!mounted) return;
        setTasks(res.tasks || []);
        setTotal(res.total_count || 0);
      })
      .catch((e) => setError(e.message || 'Failed to load tasks'))
      .finally(() => setLoading(false));
    return () => { mounted = false; };
  }, [apiKey, offset]);

  const loadTask = async (taskId: string) => {
    setSelected(taskId);
    try {
      const [d, ev] = await Promise.all([
        fetchTaskStatus(taskId, apiKey),
        getTaskEvents(taskId, { limit: 200, offset: 0 }, apiKey),
      ]);
      setDetails(d);
      const arr = (ev?.events || []) as any[];
      setEvents(
        arr.map((e) => ({
          workflow_id: e.workflow_id,
          type: e.type,
          agent_id: e.agent_id,
          message: e.message,
          timestamp: new Date(e.timestamp),
          seq: e.seq,
          stream_id: e.stream_id,
        }))
      );
      setTimeline([]);
      setTlNotice(null);
    } catch (e: any) {
      setError(e?.message || 'Failed to load task details');
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / limit));

  const isTimelineType = (t: string) =>
    t.startsWith('WF_') || t.startsWith('ACT_') || t.startsWith('CHILD_') ||
    t.startsWith('SIG_') || t.startsWith('TIMER_') || t.startsWith('ATTR_') ||
    t.startsWith('MARKER_');

  const Badge: React.FC<{ kind: 'timeline' | 'stream' }> = ({ kind }) => (
    <span
      className={`ml-2 text-[9px] uppercase tracking-wider px-1 py-[1px] border ${
        kind === 'timeline' ? 'text-[#8aa9cf] border-[#1b2a3d]' : 'text-[#c79224] border-[#3a2a10]'
      }`}
    >
      {kind}
    </span>
  );

  // Load/save toggle preferences
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const sp = window.sessionStorage.getItem('events_show_prompts');
    const spp = window.sessionStorage.getItem('events_show_partials');
    const sto = window.sessionStorage.getItem('events_show_toolobs');
    if (sp != null) setShowPrompts(sp === '1');
    if (spp != null) setShowPartials(spp === '1');
    if (sto != null) setShowToolObs(sto === '1');
  }, []);
  useEffect(() => { if (typeof window !== 'undefined') window.sessionStorage.setItem('events_show_prompts', showPrompts ? '1' : '0'); }, [showPrompts]);
  useEffect(() => { if (typeof window !== 'undefined') window.sessionStorage.setItem('events_show_partials', showPartials ? '1' : '0'); }, [showPartials]);
  useEffect(() => { if (typeof window !== 'undefined') window.sessionStorage.setItem('events_show_toolobs', showToolObs ? '1' : '0'); }, [showToolObs]);

  const isPromptOrOutput = (t: string) => t === 'LLM_PROMPT' || t === 'LLM_OUTPUT' || t === 'MESSAGE_SENT' || t === 'MESSAGE_RECEIVED';
  const isPartial = (t: string) => t === 'LLM_PARTIAL';
  const isToolObservation = (t: string) => t === 'TOOL_OBSERVATION';

  const filteredEvents = useMemo(() => {
    return events.filter(e => {
      if (!showPrompts && isPromptOrOutput(e.type)) return false;
      if (!showPartials && isPartial(e.type)) return false;
      if (!showToolObs && isToolObservation(e.type)) return false;
      return true;
    });
  }, [events, showPrompts, showPartials, showToolObs]);

  return (
    <div className="h-screen overflow-hidden bg-black text-white flex flex-col">
      <header className="p-4 flex items-center justify-between">
        <div>
          <h1 className="text-2xl tracking-tighter font-bold text-gray-500">TASKS</h1>
          <p className="text-[10px] leading-none text-gray-500">Shannon Task List & Audit</p>
        </div>
        <div className="flex items-center gap-4">
          {!SKIP_AUTH && (
            <span className="text-[10px] text-gray-500">API key from main page session</span>
          )}
          <Link href="/" className="text-[12px] text-[#c79325] underline">Back to Dashboard</Link>
        </div>
      </header>
      <div className="border-t border-gray-800" />

      <div className="flex-1 grid grid-cols-1 xl:grid-cols-[48%_52%] gap-4 p-4 min-h-0">
        <section className="min-h-0 border border-[#352b19] flex flex-col">
          <div className="ui-label ui-label--tab">Task List</div>
          <div className="flex-1 flex flex-col min-h-0">
            {error && (
              <div className="px-3 py-2 text-xs text-[#f87171] uppercase tracking-wide border-b border-[#1b1407]">{error}</div>
            )}
            <div className="flex-1 min-w-0 overflow-x-auto overflow-y-auto relative">
              <table
                className="text-xs min-w-max"
                style={{
                  width: 'max-content',
                  minWidth: '100%',
                  borderCollapse: 'separate',
                  borderSpacing: '2px',
                  backgroundColor: '#000',
                }}
              >
                <thead className="text-[#d79326] sticky top-0 bg-black z-10">
                  <tr>
                    <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '90px', minWidth: '90px' }}>Time</th>
                    <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '140px', minWidth: '140px' }}>Task ID</th>
                    <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '100px', minWidth: '100px' }}>Status</th>
                    <th className="text-left px-2 py-1 whitespace-nowrap" style={{ minWidth: '350px' }}>Query</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? (
                    <tr>
                      <td colSpan={4} className="px-3 py-4 text-center text-[#666]">
                        Loading...
                      </td>
                    </tr>
                  ) : tasks.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="px-3 py-4 text-center text-[#666]">
                        No tasks yet.
                      </td>
                    </tr>
                  ) : (
                    tasks.map((t) => {
                      const isCompleted = t.status === 'TASK_STATUS_COMPLETED';
                      const isFailed = t.status === 'TASK_STATUS_FAILED';
                      const isRunning = t.status === 'TASK_STATUS_RUNNING';
                      const rowClass = isCompleted ? 'tr-status-done' :
                                      isFailed ? 'bg-[#2d0b0b] text-[#f87171]' :
                                      isRunning ? 'tr-status-inprogress' :
                                      'tr-status-queued';
                      const statusColor = isCompleted ? 'text-[#2ec27e]' :
                                        isFailed ? 'text-[#f87171]' :
                                        isRunning ? 'text-[#c79224]' :
                                        'text-[#8aa9cf]';
                      return (
                        <tr
                          key={t.task_id}
                          onClick={() => loadTask(t.task_id)}
                          className={`cursor-pointer ${rowClass}`}
                        >
                          <td
                            className="px-2 py-1 font-mono text-[#a0a0a0] whitespace-nowrap"
                            style={{
                              width: '90px',
                              minWidth: '90px',
                              borderLeft: selected === t.task_id ? '4px solid #c79325' : '4px solid transparent'
                            }}
                          >
                            {t.created_at ? new Date(t.created_at).toLocaleTimeString('en-US', {
                              hour: '2-digit',
                              minute: '2-digit',
                              second: '2-digit',
                            }) : '—'}
                          </td>
                          <td
                            className="px-2 py-1 font-mono text-[#8aa9cf] whitespace-nowrap"
                            style={{
                              width: '140px',
                              minWidth: '140px'
                            }}
                            title={t.task_id}
                          >
                            <div className="text-[11px]">
                              {truncateTaskId(t.task_id)}
                            </div>
                          </td>
                          <td className={`px-2 py-1 font-mono ${statusColor} whitespace-nowrap`} style={{ width: '100px', minWidth: '100px' }}>
                            {t.status?.replace('TASK_STATUS_', '')}
                          </td>
                          <td className="px-2 py-1" style={{ minWidth: '350px' }} title={t.query}>
                            <div className="truncate">
                              {t.query || '—'}
                            </div>
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>
            <div className="flex items-center justify-between px-3 py-2 border-t border-[#1b1407] text-[10px] uppercase tracking-[0.3em] text-[#8a8a8a]">
              <span>Page {page+1} of {totalPages}</span>
              <span>{total} total tasks</span>
              <div className="flex gap-2">
                <button
                  className="px-2 py-1 text-[#c79325] disabled:text-[#666] disabled:cursor-not-allowed"
                  disabled={page===0}
                  onClick={()=>setPage(p=>Math.max(0,p-1))}
                >
                  ← PREV
                </button>
                <button
                  className="px-2 py-1 text-[#c79325] disabled:text-[#666] disabled:cursor-not-allowed"
                  disabled={page+1>=totalPages}
                  onClick={()=>setPage(p=>p+1)}
                >
                  NEXT →
                </button>
              </div>
            </div>
          </div>
        </section>

        <aside className="min-h-0 border border-[#352b19] grid grid-rows-[auto_1fr]">
          <div className="ui-label ui-label--tab">Task Details</div>
          <div className="p-3 overflow-auto">
            {!selected && <div className="text-[#9fa6b2] text-xs">Select a task to view details.</div>}
            {selected && details && (
              <div className="space-y-2 text-xs">
                <div><span className="text-[#9fa6b2]">Task ID:</span> <span className="font-mono text-[#c79325]">{details.task_id}</span></div>
                <div><span className="text-[#9fa6b2]">Status:</span> {details.status}</div>
                {details.query && <div><span className="text-[#9fa6b2]">Query:</span> {details.query}</div>}
                {details.session_id && <div><span className="text-[#9fa6b2]">Session:</span> <span className="font-mono">{details.session_id}</span></div>}
                {details.mode && <div><span className="text-[#9fa6b2]">Mode:</span> {details.mode}</div>}
                {details.error && <div className="text-[#f87171]">Error: {details.error}</div>}
                <div className="flex gap-2 mt-2">
                  <button
                    className="border border-[#352b19] px-2 py-1"
                    disabled={tlLoading}
                    onClick={async () => {
                      if (!selected) return;
                      setTlLoading(true);
                      setTlNotice(null);
                      try {
                        const res = await buildTimeline(selected, { mode: 'summary', persist: true }, apiKey);
                        if (res.status === 202) {
                          setTlNotice('Timeline build accepted; refreshing…');
                          // Brief delay then refresh stored events
                          setTimeout(async () => {
                            try {
                              const ev = await getTaskEvents(selected, { limit: 300, offset: 0 }, apiKey);
                              const arr = (ev?.events || []) as any[];
                              setEvents(arr.map((e) => ({
                                workflow_id: e.workflow_id,
                                type: e.type,
                                agent_id: e.agent_id,
                                message: e.message,
                                timestamp: new Date(e.timestamp),
                                seq: e.seq,
                                stream_id: e.stream_id,
                              })));
                              setTlNotice('Timeline persisted.');
                            } catch {
                              setTlNotice('Timeline accepted; refresh later.');
                            }
                          }, 1200);
                        } else if (res.status === 200 && res.body?.events) {
                          const arr = res.body.events as any[];
                          setTimeline(arr.map((e: any) => ({
                            workflow_id: e.workflow_id,
                            type: e.type,
                            agent_id: e.agent_id,
                            message: e.message,
                            timestamp: new Date(e.timestamp),
                            seq: e.seq || 0,
                            stream_id: e.stream_id,
                          })));
                          setTlNotice('Preview timeline (not persisted).');
                        } else {
                          setTlNotice('No timeline data.');
                        }
                      } catch (e: any) {
                        setTlNotice(e?.message || 'Failed to build timeline');
                      } finally {
                        setTlLoading(false);
                      }
                    }}
                  >
                    {tlLoading ? 'Building…' : 'Build Timeline'}
                  </button>
                  {tlNotice && <span className="text-[#9fa6b2]">{tlNotice}</span>}
                </div>
                <div className="border-t border-[#1b1407] pt-2" />
                <div className="text-[#9fa6b2] flex items-center justify-between">
                  <span>Events ({filteredEvents.length})</span>
                  <div className="flex items-center gap-3">
                    <label className="flex items-center gap-1"><input type="checkbox" checked={showPrompts} onChange={(e)=>setShowPrompts(e.target.checked)} /> <span className="uppercase">Prompts/Outputs</span></label>
                    <label className="flex items-center gap-1"><input type="checkbox" checked={showPartials} onChange={(e)=>setShowPartials(e.target.checked)} /> <span className="uppercase">Partials</span></label>
                    <label className="flex items-center gap-1"><input type="checkbox" checked={showToolObs} onChange={(e)=>setShowToolObs(e.target.checked)} /> <span className="uppercase">Tool Obs</span></label>
                  </div>
                </div>
                <div className="max-h-[50vh] overflow-auto font-mono text-[11px] leading-snug">
                  {filteredEvents.map((e, i) => (
                    <div key={i} className="border-b border-[#1b1407] py-1">
                      <div className="text-[#c79325]">
                        [{new Date(e.timestamp).toLocaleTimeString()}] {e.type}
                        <Badge kind={isTimelineType(e.type) ? 'timeline' : 'stream'} />
                      </div>
                      <div className="text-[#d5d5d5] break-words">{e.message}</div>
                      {e.agent_id && <div className="text-[#9fa6b2]">agent: {e.agent_id}</div>}
                    </div>
                  ))}
                </div>
                {timeline.length > 0 && (
                  <>
                    <div className="border-t border-[#1b1407] pt-2" />
                    <div className="text-[#9fa6b2]">Timeline Preview ({timeline.length})</div>
                    <div className="max-h-[40vh] overflow-auto font-mono text-[11px] leading-snug">
                      {timeline.map((e, i) => (
                        <div key={i} className="border-b border-[#1b1407] py-1">
                          <div className="text-[#c79325]">
                            [{new Date(e.timestamp).toLocaleTimeString()}] {e.type}
                            <Badge kind={'timeline'} />
                          </div>
                          <div className="text-[#d5d5d5] break-words">{e.message}</div>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}
          </div>
        </aside>
      </div>
    </div>
  );
}
