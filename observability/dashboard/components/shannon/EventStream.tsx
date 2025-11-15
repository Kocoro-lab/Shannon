'use client';

import { useRef, useEffect } from 'react';
import type { TaskEvent } from '../../shannon/types';

interface Props {
  events: TaskEvent[];
  status: 'idle' | 'connecting' | 'connected' | 'error';
  error: Error | null;
}

function formatTime(ts: Date): string {
  return ts.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function eventTypeTone(type: string) {
  if (type.includes('ERROR')) return 'text-[#f87171]';
  if (type.includes('COMPLETED')) return 'text-[#2ec27e]';
  if (type.includes('WAITING')) return 'text-[#9ca3af]'; // grey tone for waiting
  if (type.includes('PROGRESS') || type.includes('STARTED') || type.includes('INVOKED') || type.includes('DATA_PROCESSING')) return 'text-[#c79224]'; // amber for work
  if (type.includes('TEAM_') || type.includes('STATUS')) return 'text-[#8aa9cf]'; // blue for coordination
  return 'text-[#8aa9cf]';
}

function rowClassForType(type: string) {
  if (type.includes('ERROR')) return 'bg-[#2d0b0b] text-[#f87171]';
  if (type.includes('COMPLETED')) return 'tr-status-done';
  if (type.includes('WAITING')) return 'bg-[#0b0b0b] text-[#9ca3af]';
  if (type.includes('PROGRESS') || type.includes('STARTED') || type.includes('INVOKED') || type.includes('DATA_PROCESSING')) return 'tr-status-inprogress';
  if (type.includes('TEAM_') || type.includes('STATUS')) return 'bg-[#030712] text-[#8aa9cf]';
  return 'tr-status-queued';
}

export function EventStream({ events, status, error }: Props) {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [events]);

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center justify-between px-3 py-2 border-b border-[#1b1407] text-[10px] uppercase tracking-[0.3em] text-[#8a8a8a]">
        <span>Platform Timeline</span>
        <span>{events.length} events</span>
      </div>
      {error && (
        <div className="px-3 py-2 text-xs text-[#f87171] uppercase tracking-wide border-b border-[#1b1407]">{error.message}</div>
      )}
      <div
        ref={scrollRef}
        className="flex-1 min-w-0 overflow-x-auto overflow-y-auto relative"
      >
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
              <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '140px', minWidth: '140px' }}>Type</th>
              <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '160px', minWidth: '160px' }}>Workflow</th>
              <th className="text-left px-2 py-1 whitespace-nowrap" style={{ width: '120px', minWidth: '120px' }}>Agent</th>
              <th className="text-left px-2 py-1 whitespace-nowrap" style={{ minWidth: '250px' }}>Message</th>
            </tr>
          </thead>
          <tbody>
            {events.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-3 py-4 text-center text-[#666]">
                  Monitoring platform... No events yet.
                </td>
              </tr>
            ) : (
              events.map((event, idx) => {
                const rowClass = rowClassForType(event.type);
                const key = event.stream_id || `${event.workflow_id}-${event.seq}-${event.agent_id}-${event.type}-${idx}`;
                const workflowShort = event.workflow_id ? event.workflow_id.split('-').pop() || event.workflow_id : '—';
                return (
                  <tr key={key} className={rowClass}>
                    <td className="px-2 py-1 font-mono text-[#a0a0a0] whitespace-nowrap" style={{ width: '90px', minWidth: '90px' }}>{formatTime(event.timestamp)}</td>
                    <td className={`px-2 py-1 font-mono ${eventTypeTone(event.type)} whitespace-nowrap`} style={{ width: '140px', minWidth: '140px', borderLeft: '4px solid currentColor' }}>{event.type}</td>
                    <td className="px-2 py-1 whitespace-nowrap font-mono text-[#8aa9cf]" style={{ width: '160px', minWidth: '160px' }} title={event.workflow_id}>
                      <div className="truncate">
                        {workflowShort}
                      </div>
                    </td>
                    <td className="px-2 py-1 whitespace-nowrap" style={{ width: '120px', minWidth: '120px' }} title={event.agent_id}>
                      <div className="truncate">
                        {event.agent_id || '—'}
                      </div>
                    </td>
                    <td className="px-2 py-1" style={{ minWidth: '250px' }} title={event.formatted || event.message}>
                      <div className="break-words">
                        {event.formatted || event.message || '—'}
                      </div>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
