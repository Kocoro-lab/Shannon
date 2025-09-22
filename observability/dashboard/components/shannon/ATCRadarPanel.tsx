"use client";

import RadarCanvas from '../RadarCanvas';
import { ShannonATCBridge } from './ShannonATCBridge';
import type { TaskEvent } from '../../shannon/types';

export function ATCRadarPanel({ workflowId, events }: { workflowId: string; events: TaskEvent[] }) {
  return (
    <div className="h-full min-h-0 border border-[#352b19] bg-black">
      <div className="ui-label ui-label--tab">Task Flights Radar</div>
      <div className="flex-1 min-h-0" style={{ height: '100%' }}>
        {/* Bridge Shannon events into the ATC store */}
        <ShannonATCBridge workflowId={workflowId} events={events} />
        {/* Canvas-based radar from the original ATC */}
        <div className="relative" style={{ height: '100%' }}>
          <RadarCanvas workflowId={workflowId} />
        </div>
      </div>
    </div>
  );
}
