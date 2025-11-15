'use client';

import { useState } from 'react';
import { submitTask, type SubmitTaskOptions } from '../../shannon/api';

interface Props {
  apiKey?: string;
}

export function TaskSubmitterEnhanced({ apiKey }: Props) {
  const [query, setQuery] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Options state
  const [researchStrategy, setResearchStrategy] = useState<string>('');
  const [modelTier, setModelTier] = useState<string>('');
  const [maxIterations, setMaxIterations] = useState<string>('');
  const [maxConcurrentAgents, setMaxConcurrentAgents] = useState<string>('');
  const [enableVerification, setEnableVerification] = useState(false);

  const [lastSubmittedId, setLastSubmittedId] = useState<string | null>(null);

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!query.trim()) {
      setError('Enter a task prompt');
      return;
    }
    setSubmitting(true);
    setError(null);

    try {
      const options: SubmitTaskOptions = {};

      if (researchStrategy) options.research_strategy = researchStrategy as any;
      if (modelTier) options.model_tier = modelTier as any;
      if (maxIterations) options.max_iterations = parseInt(maxIterations, 10);
      if (maxConcurrentAgents) options.max_concurrent_agents = parseInt(maxConcurrentAgents, 10);
      if (enableVerification) options.enable_verification = true;

      const response = await submitTask(query, options, apiKey);
      setLastSubmittedId(response.workflow_id || response.task_id);
      setQuery(''); // Clear query after successful submission
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Task submission failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-0 overflow-hidden flex flex-col p-0">
      {/* Header bar */}
      <div className="flex items-center bg-[#130f04ff]">
        <h2 className="bg-[#c79325] pl-2 pr-2 font-bold text-black">Try a task</h2>
      </div>

      <form onSubmit={handleSubmit}>
        <div className="border border-[#352b19ff]">
          {/* Last Submitted Task ID */}
          {lastSubmittedId && (
            <div className="border-b border-[#352b19ff] p-3 bg-[#0a0a0a]">
              <div className="text-xs text-[#2ec27e] mb-1">✓ Task Submitted</div>
              <div className="text-sm text-[#a4a4a4ff]">
                <a
                  href={`http://localhost:8088/namespaces/default/workflows/${lastSubmittedId}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono text-[11px] text-[#8aa9cf] hover:text-[#c79325] underline cursor-pointer"
                >
                  {lastSubmittedId}
                </a>
              </div>
            </div>
          )}

          {/* Main options */}
          <div className="p-3 space-y-3">
            {/* Query input */}
            <div>
              <div className="text-xs text-[#c89225ff] mb-1">Task Query</div>
              <textarea
                className="w-full bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2 font-mono resize-none"
                placeholder="Who is the president of Japan as of 2025?"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                disabled={submitting}
                rows={2}
              />
            </div>

            {/* Research Strategy */}
            <div>
              <div className="text-xs text-[#c89225ff] mb-1">Research Strategy</div>
              <select
                className="w-full bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2"
                value={researchStrategy}
                onChange={(e) => setResearchStrategy(e.target.value)}
                disabled={submitting}
              >
                <option value="">Default</option>
                <option value="quick">Quick (fast, fewer sources)</option>
                <option value="standard">Standard (balanced)</option>
                <option value="deep">Deep (thorough, more iterations)</option>
                <option value="academic">Academic (max rigor, citations)</option>
              </select>
            </div>

            {/* Model Tier */}
            <div>
              <div className="text-xs text-[#c89225ff] mb-1">Model Tier</div>
              <select
                className="w-full bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2"
                value={modelTier}
                onChange={(e) => setModelTier(e.target.value)}
                disabled={submitting}
              >
                <option value="">Default</option>
                <option value="small">Small (fast, cost-efficient)</option>
                <option value="medium">Medium (balanced)</option>
                <option value="large">Large (most capable)</option>
              </select>
            </div>

            {/* Advanced settings toggle */}
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="text-xs text-[#8aa9cf] hover:text-[#c79325] uppercase tracking-wider"
            >
              {showAdvanced ? '▼ Hide Advanced' : '▶ Show Advanced'}
            </button>

            {/* Advanced settings */}
            {showAdvanced && (
              <div className="space-y-3 border-t border-[#352b19ff] pt-3 mt-2">
                <div>
                  <div className="text-xs text-[#c89225ff] mb-1">Max Iterations (1-50)</div>
                  <input
                    type="number"
                    className="w-full bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2 font-mono"
                    placeholder="Auto"
                    value={maxIterations}
                    onChange={(e) => setMaxIterations(e.target.value)}
                    disabled={submitting}
                    min="1"
                    max="50"
                  />
                </div>

                <div>
                  <div className="text-xs text-[#c89225ff] mb-1">Max Concurrent Agents (1-20)</div>
                  <input
                    type="number"
                    className="w-full bg-black border border-[#352b19ff] text-sm text-[#a4a4a4ff] p-2 font-mono"
                    placeholder="Auto"
                    value={maxConcurrentAgents}
                    onChange={(e) => setMaxConcurrentAgents(e.target.value)}
                    disabled={submitting}
                    min="1"
                    max="20"
                  />
                </div>

                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id="enable-verification"
                    checked={enableVerification}
                    onChange={(e) => setEnableVerification(e.target.checked)}
                    disabled={submitting}
                    className="w-4 h-4"
                  />
                  <label htmlFor="enable-verification" className="text-xs text-[#a4a4a4ff] cursor-pointer">
                    Enable verification (slower, higher quality)
                  </label>
                </div>
              </div>
            )}

            {/* Execute button */}
            <button
              type="submit"
              disabled={submitting}
              className="w-full py-2 bg-[#c79325] text-black text-xs font-bold uppercase hover:bg-[#d9a63d] disabled:opacity-60"
            >
              {submitting ? 'EXECUTING...' : 'EXECUTE TASK'}
            </button>

            {error && (
              <div className="text-xs text-[#f87171] uppercase tracking-wide">{error}</div>
            )}
          </div>
        </div>
      </form>
    </div>
  );
}
