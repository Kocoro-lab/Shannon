'use client';

import { useTaskStatus } from '../../shannon/useTaskStatus';
import type { TaskStatus } from '../../shannon/types';

interface Props {
  taskId: string | null;
  apiKey?: string;
}

export function StatusDisplay({ taskId, apiKey }: Props) {
  const { data, error, loading } = useTaskStatus(taskId, apiKey);

  if (!taskId) {
    return <div className="text-xs text-[#7b7b7b]">Submit a task to view status.</div>;
  }

  if (loading && !data) {
    return <div className="text-xs text-[#8aa9cf]">Fetching status…</div>;
  }

  if (error) {
    return <div className="text-xs text-[#f87171]">{error.message}</div>;
  }

  if (!data) {
    return null;
  }

  return (
    <div className="space-y-3 text-xs text-[#d5d5d5]">
      <StatusBadge status={data.status} />
      {data.error && <div className="text-[#f87171]">{data.error}</div>}
      {data.response && (
        <pre className="bg-[#050505] border border-[#1b1407] p-2 max-h-48 overflow-auto text-[11px]">
          {JSON.stringify(data.response, null, 2)}
        </pre>
      )}
      {data.created_at && (
        <div className="text-[#7a8cb6]">Created: {new Date(data.created_at).toLocaleString()}</div>
      )}
      {data.updated_at && (
        <div className="text-[#7a8cb6]">Updated: {new Date(data.updated_at).toLocaleString()}</div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: TaskStatus }) {
  const tone = (() => {
    switch (status) {
      case 'COMPLETED':
        return 'bg-[#05291d] text-[#2ec27e]';
      case 'FAILED':
        return 'bg-[#2d0b0b] text-[#f87171]';
      case 'RUNNING':
        return 'bg-[#212107] text-[#c79224]';
      default:
        return 'bg-[#0e1d46] text-[#8aa9cf]';
    }
  })();
  return (
    <span className={`inline-flex px-3 py-1 uppercase tracking-[0.3em] text-[10px] ${tone}`}>
      {status}
    </span>
  );
}
