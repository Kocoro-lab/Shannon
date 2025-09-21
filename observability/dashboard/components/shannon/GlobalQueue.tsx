'use client';

import { useMemo } from 'react';
import { useFlights } from '../../shannon/dashboardContext';

const SHANNON_FEATURES = ['LLM', 'TOOLS', 'MEMORY', 'WORKFLOW'];

export function GlobalQueue() {
  const flights = useFlights();

  const stats = useMemo(() => {
    const counts = {
      active: flights.filter(f => f.status === 'active').length,
      queued: flights.filter(f => f.status === 'queued').length,
      completed: flights.filter(f => f.status === 'completed').length,
      error: flights.filter(f => f.status === 'error').length,
    };
    return counts;
  }, [flights]);

  const featureStatus = useMemo(() => {
    const status = new Map<string, { active: number; queued: number }>();

    // Initialize all features
    for (const feature of SHANNON_FEATURES) {
      status.set(feature, { active: 0, queued: 0 });
    }

    // Map flights to features based on their event types or agent IDs
    for (const flight of flights) {
      let feature = 'WORKFLOW'; // default

      if (flight.lastEventType?.includes('MESSAGE') || flight.lastEventType?.includes('THINKING')) {
        feature = 'LLM';
      } else if (flight.lastEventType?.includes('TOOL')) {
        feature = 'TOOLS';
      } else if (flight.lastEventType?.includes('WORKSPACE') || flight.lastMessage?.toLowerCase().includes('memory')) {
        feature = 'MEMORY';
      }

      const stat = status.get(feature)!;
      if (flight.status === 'active') {
        stat.active++;
      } else if (flight.status === 'queued') {
        stat.queued++;
      }
    }

    return Array.from(status.entries());
  }, [flights]);

  return (
    <div className="h-full min-h-0 border border-[#352b19] bg-black flex flex-col">
      <div className="ui-label ui-label--tab">Global Queue</div>
      <div className="flex-1 p-2 space-y-2">
        {/* Compact summary */}
        <div className="text-[10px] uppercase tracking-wider text-[#d79326]">
          <div className="flex justify-between">
            <span>Active</span>
            <span className="font-mono text-[#c79224]">{stats.active}</span>
          </div>
          <div className="flex justify-between">
            <span>Queue</span>
            <span className="font-mono text-[#8aa9cf]">{stats.queued}</span>
          </div>
          <div className="flex justify-between">
            <span>Completed</span>
            <span className="font-mono text-[#2ec27e]">{stats.completed}</span>
          </div>
          {stats.error > 0 && (
            <div className="flex justify-between">
              <span>Errors</span>
              <span className="font-mono text-[#f87171]">{stats.error}</span>
            </div>
          )}
        </div>

        {/* Feature breakdown */}
        <div className="border-t border-[#1b1407] pt-2 space-y-1">
          <div className="text-[10px] uppercase tracking-wider text-[#626262]">Subsystems</div>
          {featureStatus.map(([feature, status]) => (
            <div key={feature} className="text-[10px] flex items-center justify-between">
              <span className="text-[#d79326] uppercase">{feature}</span>
              <div className="flex gap-2 font-mono">
                {status.active > 0 && <span className="text-[#c79224]">{status.active}</span>}
                {status.queued > 0 && <span className="text-[#8aa9cf]">{status.queued}</span>}
                {status.active === 0 && status.queued === 0 && <span className="text-[#555]">—</span>}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
