'use client';

import { useEffect, useState } from 'react';
import { fetchTaskStatus } from '../../shannon/api';
import type { TaskStatusResponse } from '../../shannon/types';

interface Props {
  taskId: string | null;
  apiKey?: string;
}

export function TaskMetadata({ taskId, apiKey }: Props) {
  const [status, setStatus] = useState<TaskStatusResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!taskId) {
      setStatus(null);
      setError(null);
      return;
    }

    let mounted = true;
    setLoading(true);
    setError(null);

    fetchTaskStatus(taskId, apiKey)
      .then(data => {
        if (mounted) {
          setStatus(data);
          setLoading(false);
        }
      })
      .catch(err => {
        if (mounted) {
          setError(err.message);
          setLoading(false);
        }
      });

    return () => {
      mounted = false;
    };
  }, [taskId, apiKey]);

  if (!taskId) {
    return (
      <div className="p-4 text-center text-[#666]">
        <p className="text-xs uppercase tracking-wider">No active task</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="p-4 text-center">
        <p className="text-xs text-[#c79325] uppercase tracking-wider">Loading metadata...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4">
        <p className="text-xs text-[#f87171] uppercase tracking-wider">Error: {error}</p>
      </div>
    );
  }

  if (!status) {
    return null;
  }

  return (
    <div className="p-4 space-y-4 text-xs">
      {/* Usage Section */}
      {(status.model_used || status.provider || status.usage) && (
        <div className="border-b border-[#352b19] pb-3">
          <h3 className="text-[#c79325] uppercase tracking-[0.2em] mb-2 font-bold">Usage</h3>
          <div className="space-y-1 text-[#a4a4a4]">
            {status.model_used && (
              <div className="flex justify-between">
                <span className="text-[#666]">Model:</span>
                <span className="font-mono">{status.model_used}</span>
              </div>
            )}
            {status.provider && (
              <div className="flex justify-between">
                <span className="text-[#666]">Provider:</span>
                <span className="font-mono uppercase">{status.provider}</span>
              </div>
            )}
            {status.usage?.total_tokens && (
              <div className="flex justify-between">
                <span className="text-[#666]">Total Tokens:</span>
                <span className="font-mono">{status.usage.total_tokens.toLocaleString()}</span>
              </div>
            )}
            {status.usage?.input_tokens && (
              <div className="flex justify-between">
                <span className="text-[#666]">Input:</span>
                <span className="font-mono">{status.usage.input_tokens.toLocaleString()}</span>
              </div>
            )}
            {status.usage?.output_tokens && (
              <div className="flex justify-between">
                <span className="text-[#666]">Output:</span>
                <span className="font-mono">{status.usage.output_tokens.toLocaleString()}</span>
              </div>
            )}
            {status.usage?.estimated_cost !== undefined && (
              <div className="flex justify-between text-[#c79325]">
                <span className="text-[#666]">Cost:</span>
                <span className="font-mono">${status.usage.estimated_cost.toFixed(6)}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Agent Breakdown */}
      {status.metadata?.agent_usages && status.metadata.agent_usages.length > 0 && (
        <div className="border-b border-[#352b19] pb-3">
          <h3 className="text-[#c79325] uppercase tracking-[0.2em] mb-2 font-bold">Agents</h3>
          <div className="space-y-2">
            {status.metadata.agent_usages.map((agent, idx) => (
              <div key={idx} className="text-[#a4a4a4]">
                <div className="font-mono text-[10px] text-[#8aa9cf]">{agent.agent_id}</div>
                <div className="flex justify-between text-[11px] mt-0.5">
                  {agent.tokens !== undefined && (
                    <span>{agent.tokens.toLocaleString()} tokens</span>
                  )}
                  {agent.cost_usd !== undefined && (
                    <span className="text-[#c79325]">${agent.cost_usd.toFixed(6)}</span>
                  )}
                </div>
                {agent.model && (
                  <div className="text-[10px] text-[#666] mt-0.5">{agent.model}</div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Citations */}
      {status.metadata?.citations && status.metadata.citations.length > 0 && (
        <div className="border-b border-[#352b19] pb-3">
          <h3 className="text-[#c79325] uppercase tracking-[0.2em] mb-2 font-bold">
            Citations ({status.metadata.citations.length})
          </h3>
          <div className="space-y-2 max-h-[200px] overflow-y-auto">
            {status.metadata.citations.map((cite, idx) => (
              <div key={idx} className="text-[#a4a4a4]">
                <a
                  href={cite.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-[#8aa9cf] hover:text-[#c79325] underline text-[11px] break-words"
                >
                  {cite.title || cite.url}
                </a>
                {cite.source && (
                  <div className="text-[10px] text-[#666] mt-0.5">{cite.source}</div>
                )}
                {cite.credibility_score !== undefined && (
                  <div className="text-[10px] text-[#666] mt-0.5">
                    Credibility: {(cite.credibility_score * 100).toFixed(0)}%
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Summary Stats */}
      {status.metadata?.num_agents && (
        <div className="text-[#a4a4a4]">
          <div className="flex justify-between">
            <span className="text-[#666]">Total Agents:</span>
            <span className="font-mono">{status.metadata.num_agents}</span>
          </div>
        </div>
      )}

      {/* Task Info */}
      {status.query && (
        <div className="border-t border-[#352b19] pt-3">
          <h3 className="text-[#c79325] uppercase tracking-[0.2em] mb-2 font-bold text-[10px]">Query</h3>
          <p className="text-[#a4a4a4] text-[11px] leading-relaxed">{status.query}</p>
        </div>
      )}
    </div>
  );
}
