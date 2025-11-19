'use client';

import React from 'react';
import { Card } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import type { Message } from '@/lib/store/sessionSlice';
import type { ShannonEvent } from '@/lib/shannon/types';
import { EventItem } from './EventItem';

interface MessageListProps {
  messages: Message[];
  events?: ShannonEvent[];
  currentWorkflowId?: string | null;
}

export function MessageList({ messages, events = [], currentWorkflowId }: MessageListProps) {
  const scrollRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    // Auto-scroll to bottom on new messages or events
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, events]);

  // Group messages and events chronologically
  const items: Array<{ type: 'message' | 'event'; data: Message | ShannonEvent; timestamp: string }> = [
    ...messages.map(msg => ({ type: 'message' as const, data: msg, timestamp: msg.timestamp })),
    ...events.map(evt => ({ type: 'event' as const, data: evt, timestamp: evt.timestamp })),
  ].sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());

  return (
    <div
      ref={scrollRef}
      className="h-full w-full overflow-y-auto p-4"
    >
      <div className="space-y-4">
        {items.map((item, index) => {
          if (item.type === 'message') {
            const message = item.data as Message;
            return (
              <div
                key={`msg-${message.id}`}
                className={`flex ${
                  message.role === 'user' ? 'justify-end' : 'justify-start'
                }`}
              >
                <Card
                  className={`max-w-[80%] p-4 ${
                    message.role === 'user'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted'
                  }`}
                >
                  <div className="space-y-2">
                    <div className="prose prose-sm dark:prose-invert max-w-none whitespace-pre-wrap">
                      {message.content}
                    </div>
                    {message.metadata && (
                      <div className="flex flex-wrap gap-2 mt-2 text-xs">
                        {message.metadata.mode && (
                          <Badge variant="outline">{message.metadata.mode}</Badge>
                        )}
                        {message.metadata.tokens && (
                          <Badge variant="outline">
                            {message.metadata.tokens.toLocaleString()} tokens
                          </Badge>
                        )}
                        {message.metadata.cost && (
                          <Badge variant="outline">
                            ${message.metadata.cost.toFixed(4)}
                          </Badge>
                        )}
                        {message.metadata.citations && message.metadata.citations > 0 && (
                          <Badge variant="outline">
                            {message.metadata.citations} citations
                          </Badge>
                        )}
                      </div>
                    )}
                  </div>
                </Card>
              </div>
            );
          } else {
            const event = item.data as ShannonEvent;
            // Only show events for the current workflow
            if (currentWorkflowId && event.workflow_id === currentWorkflowId) {
              return <EventItem key={`evt-${event.seq}-${index}`} event={event} />;
            }
            return null;
          }
        })}
      </div>
    </div>
  );
}
