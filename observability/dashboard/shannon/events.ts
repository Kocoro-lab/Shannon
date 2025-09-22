import type { TaskEvent, EventType } from './types';

type EventPayload = Record<string, unknown>;

export function parseSSEEvent(payload: string): TaskEvent {
  const data = JSON.parse(payload) as EventPayload;
  const type = data.type as EventType;
  return {
    workflow_id: typeof data.workflow_id === 'string' ? data.workflow_id : '',
    type,
    agent_id: typeof data.agent_id === 'string' ? data.agent_id : 'system',
    message: typeof data.message === 'string' ? data.message : '',
    timestamp: new Date((data.timestamp as string | number | undefined) ?? Date.now()),
    seq: typeof data.seq === 'number' ? data.seq : Number((data.seq as string | number | undefined) ?? 0),
    stream_id: typeof (data as any).stream_id === 'string' ? (data as any).stream_id : undefined,
    formatted: formatEvent(type, data),
  };
}

function formatEvent(type: EventType, data: EventPayload): string {
  const message = typeof data.message === 'string' ? data.message : '';
  const agent = typeof data.agent_id === 'string' ? data.agent_id : 'unknown';
  switch (type) {
    case 'WORKFLOW_STARTED':
      return `Workflow started: ${message}`.trim();
    case 'WORKFLOW_COMPLETED':
      return `Workflow completed: ${message}`.trim();
    case 'AGENT_STARTED':
      return `Agent ${agent} started: ${message}`.trim();
    case 'AGENT_COMPLETED':
      return `Agent ${agent} completed: ${message}`.trim();
    case 'ERROR_OCCURRED':
      return `Error: ${message}`.trim();
    default:
      return `${type}: ${message}`.trim();
  }
}
