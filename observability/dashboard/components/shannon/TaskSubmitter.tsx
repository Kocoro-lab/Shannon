'use client';

import { useState } from 'react';
import { submitTask } from '../../shannon/api';
import type { TaskSubmitResponse } from '../../shannon/types';

interface Props {
  apiKey?: string;
  workflowId?: string;
  onTaskSubmitted: (response: TaskSubmitResponse) => void;
}

export function TaskSubmitter({ apiKey, workflowId, onTaskSubmitted }: Props) {
  const [query, setQuery] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!query.trim()) {
      setError('Enter a task prompt');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const response = await submitTask(query, apiKey);
      onTaskSubmitted(response);
      // Don't clear the query after submission
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Task submission failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-0 overflow-hidden flex flex-col p-0">
      {/* Header bar matching MONITORING TABLE style */}
      <div className="flex items-center bg-[#130f04ff]">
        <h2 className="bg-[#c79325] pl-2 pr-2 font-bold text-black">Try a task</h2>
      </div>

      {/* Stacked layout - Task ID on top, Input below */}
      <form onSubmit={handleSubmit}>
        <div className="border border-[#352b19ff]" style={{ minHeight: 'auto' }}>
          {/* Top section: Task ID */}
          <div className="border-b border-[#352b19ff] p-3">
            <div className="text-xs text-[#d79326ff] mb-1">Task ID</div>
            <div className="text-sm text-[#a4a4a4ff]">
              {workflowId ? (
                <a
                  href={`http://localhost:8088/namespaces/default/workflows/${workflowId}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono text-[11px] text-[#8aa9cf] hover:text-[#c79325] underline cursor-pointer"
                >
                  {workflowId}
                </a>
              ) : (
                <span className="text-[#666]">—</span>
              )}
            </div>
          </div>

          {/* Bottom section: Task Input and Execute Button */}
          <div className="p-3">
            <div className="text-xs text-[#c89225ff] mb-1">Task Query</div>
            <div className="flex gap-3 items-start">
              <textarea
                className="flex-1 bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2 font-mono resize-none"
                placeholder="Who is the president of Japan as of 2025?"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                disabled={submitting}
                rows={2}
                style={{ minHeight: '40px' }}
              />
              <button
                type="submit"
                disabled={submitting}
                className="px-3 py-1 bg-[#c79325] text-black text-xs font-bold uppercase hover:bg-[#d9a63d] disabled:opacity-60 whitespace-nowrap"
              >
                {submitting ? 'EXECUTING' : 'EXECUTE'}
              </button>
            </div>
            {error && (
              <div className="text-xs text-[#f87171] uppercase tracking-wide mt-1">{error}</div>
            )}
          </div>
        </div>
      </form>
    </div>
  );
}
