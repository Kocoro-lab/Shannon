'use client';

import { useEffect, useState, useRef } from 'react';
import type { TaskEvent } from '../../shannon/types';

interface Props {
  events: TaskEvent[];
}

interface LLMOutput {
  agent_id: string;
  content: string;
  is_partial: boolean;
  timestamp: Date;
}

export function LLMStreamViewer({ events }: Props) {
  const [outputs, setOutputs] = useState<Map<string, LLMOutput>>(new Map());
  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  useEffect(() => {
    const newOutputs = new Map<string, LLMOutput>();

    // Process events to build current LLM outputs
    events.forEach(event => {
      const agentKey = event.agent_id || 'unknown';

      if (event.type === 'LLM_PARTIAL') {
        // Incremental update - append to existing or create new
        const existing = newOutputs.get(agentKey);
        newOutputs.set(agentKey, {
          agent_id: agentKey,
          content: event.message || '',
          is_partial: true,
          timestamp: event.timestamp,
        });
      } else if (event.type === 'LLM_OUTPUT') {
        // Final output - replace any partial
        newOutputs.set(agentKey, {
          agent_id: agentKey,
          content: event.message || '',
          is_partial: false,
          timestamp: event.timestamp,
        });
      }
    });

    setOutputs(newOutputs);
  }, [events]);

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [outputs, autoScroll]);

  const handleScroll = () => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50;
    setAutoScroll(isAtBottom);
  };

  if (outputs.size === 0) {
    return (
      <div className="p-4 text-center text-[#666]">
        <p className="text-xs uppercase tracking-wider">Waiting for LLM output...</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 py-2 bg-[#130f04ff] border-b border-[#352b19]">
        <h3 className="text-xs text-[#c79325] uppercase tracking-[0.2em] font-bold">LLM Output</h3>
        <button
          onClick={() => setAutoScroll(!autoScroll)}
          className={`text-[10px] uppercase tracking-wider px-2 py-1 ${
            autoScroll ? 'text-[#c79325] bg-[#352b19]' : 'text-[#666]'
          }`}
        >
          {autoScroll ? 'Auto-scroll ON' : 'Auto-scroll OFF'}
        </button>
      </div>

      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto p-4 space-y-4"
      >
        {Array.from(outputs.values()).map((output, idx) => (
          <div key={idx} className="border border-[#352b19] bg-black">
            {/* Agent header */}
            <div className="flex items-center justify-between px-3 py-2 bg-[#130f04ff] border-b border-[#352b19]">
              <span className="text-[10px] text-[#8aa9cf] font-mono uppercase tracking-wider">
                {output.agent_id}
              </span>
              {output.is_partial && (
                <span className="text-[10px] text-[#c79325] animate-pulse">● Streaming...</span>
              )}
            </div>

            {/* Output content */}
            <div className="p-3">
              <div className="text-[#d5d5d5] text-sm leading-relaxed whitespace-pre-wrap font-mono">
                {output.content}
                {output.is_partial && (
                  <span className="inline-block w-2 h-4 bg-[#c79325] ml-1 animate-pulse" />
                )}
              </div>
            </div>

            {/* Timestamp */}
            <div className="px-3 py-1 text-[10px] text-[#666] border-t border-[#352b19]">
              {output.timestamp.toLocaleTimeString()}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
