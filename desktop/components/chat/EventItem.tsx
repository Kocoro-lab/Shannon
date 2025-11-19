'use client';

import React from 'react';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';
import type { ShannonEvent } from '@/lib/shannon/types';
import {
  Cpu,
  Zap,
  Wrench,
  Eye,
  Loader2,
  AlertCircle,
  CheckCircle2,
  Users,
  MessageSquare,
} from 'lucide-react';

interface EventItemProps {
  event: ShannonEvent;
}

export function EventItem({ event }: EventItemProps) {
  const { type, agent_id, message, metadata } = event;

  // Don't display LLM_PARTIAL events individually (handled by streaming text)
  if (type === 'LLM_PARTIAL') {
    return null;
  }

  // Don't display LLM_OUTPUT individually (shown as final message)
  if (type === 'LLM_OUTPUT') {
    return null;
  }

  const renderEventContent = () => {
    switch (type) {
      case 'WORKFLOW_STARTED':
        return (
          <EventCard
            icon={<Zap className="h-4 w-4" />}
            title="Workflow Started"
            description={message || 'Task processing initiated'}
            variant="info"
          />
        );

      case 'WORKFLOW_COMPLETED':
        return (
          <EventCard
            icon={<CheckCircle2 className="h-4 w-4" />}
            title="Workflow Completed"
            description={message || 'Task completed successfully'}
            variant="success"
          />
        );

      case 'AGENT_STARTED':
        return (
          <EventCard
            icon={<Cpu className="h-4 w-4" />}
            title={`Agent: ${agent_id || 'Unknown'}`}
            description={message || 'Agent execution started'}
            variant="default"
          />
        );

      case 'AGENT_THINKING':
        return (
          <EventCard
            icon={<Loader2 className="h-4 w-4 animate-spin" />}
            title={`${agent_id || 'Agent'} thinking...`}
            description={message}
            variant="muted"
            subtle
            expandable
          />
        );

      case 'AGENT_COMPLETED':
        return (
          <EventCard
            icon={<CheckCircle2 className="h-4 w-4" />}
            title={`${agent_id || 'Agent'} completed`}
            description={message}
            variant="success"
          />
        );

      case 'TOOL_INVOKED':
        const tool = metadata?.tool as string | undefined;
        const params = metadata?.params as Record<string, unknown> | undefined;
        return (
          <EventCard
            icon={<Wrench className="h-4 w-4" />}
            title={`Tool: ${tool || 'Unknown'}`}
            description={message}
            variant="tool"
            details={params ? JSON.stringify(params, null, 2) : undefined}
            expandable
          />
        );

      case 'TOOL_OBSERVATION':
        return (
          <EventCard
            icon={<Eye className="h-4 w-4" />}
            title="Tool Result"
            description={message}
            variant="tool"
            expandable
          />
        );

      case 'PROGRESS':
        const progress = metadata?.progress as number | undefined;
        return (
          <EventCard
            icon={<Loader2 className="h-4 w-4" />}
            title={progress ? `Progress: ${progress}%` : 'Progress'}
            description={message}
            variant="info"
            subtle
          />
        );

      case 'DATA_PROCESSING': {
        const text = (message || '').trim();
        const isFinal = text.toLowerCase() === 'final answer ready';

        if (!isFinal) {
          // Hide all non-final DATA_PROCESSING events to reduce noise
          return null;
        }

        return (
          <EventCard
            icon={<CheckCircle2 className="h-4 w-4" />}
            title="Final answer ready"
            description={undefined}
            variant="success"
          />
        );
      }

      case 'WAITING':
        return (
          <EventCard
            icon={<Loader2 className="h-4 w-4 animate-spin" />}
            title="Waiting"
            description={message}
            variant="muted"
            subtle
          />
        );

      case 'WORKSPACE_UPDATED':
        return (
          <EventCard
            icon={<MessageSquare className="h-4 w-4" />}
            title="Workspace Updated"
            description={message || 'Context memory updated'}
            variant="default"
          />
        );

      case 'ERROR_OCCURRED':
        const severity = metadata?.severity as string | undefined;
        return (
          <EventCard
            icon={<AlertCircle className="h-4 w-4" />}
            title={`Error${severity ? ` (${severity})` : ''}`}
            description={message || 'An error occurred'}
            variant="error"
            expandable
          />
        );

      case 'ERROR_RECOVERY':
        return (
          <EventCard
            icon={<Loader2 className="h-4 w-4 animate-spin" />}
            title="Recovering from Error"
            description={message || 'Attempting recovery...'}
            variant="warning"
          />
        );

      case 'TEAM_RECRUITED':
      case 'TEAM_RETIRED':
      case 'TEAM_STATUS':
        return (
          <EventCard
            icon={<Users className="h-4 w-4" />}
            title={type.replace('_', ' ')}
            description={message}
            variant="default"
          />
        );

      case 'APPROVAL_REQUESTED':
        return (
          <EventCard
            icon={<AlertCircle className="h-4 w-4" />}
            title="Approval Required"
            description={message || 'Human approval needed to proceed'}
            variant="warning"
          />
        );

      default:
        // Generic fallback for any other event types
        return (
          <EventCard
            title={type}
            description={message}
            variant="muted"
          />
        );
    }
  };

  return <div className="py-1">{renderEventContent()}</div>;
}

interface EventCardProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  variant?: 'default' | 'muted' | 'info' | 'success' | 'warning' | 'error' | 'tool';
  details?: string;
  expandable?: boolean;
  subtle?: boolean;
}

function EventCard({
  icon,
  title,
  description,
  variant = 'default',
  details,
  expandable = false,
  subtle = false,
}: EventCardProps) {
  const [isExpanded, setIsExpanded] = React.useState(false);

  const variantStyles = {
    default: 'border-border bg-background',
    muted: 'border-none bg-transparent text-muted-foreground',
    info: 'border-blue-500/50 bg-blue-500/10 text-blue-700 dark:text-blue-300',
    success: 'border-green-500/50 bg-green-500/10 text-green-700 dark:text-green-300',
    warning: 'border-yellow-500/50 bg-yellow-500/10 text-yellow-700 dark:text-yellow-300',
    error: 'border-red-500/50 bg-red-500/10 text-red-700 dark:text-red-300',
    tool: 'border-purple-500/50 bg-purple-500/10 text-purple-700 dark:text-purple-300',
  };

  return (
    <Card className={`${subtle ? 'p-2 text-xs' : 'p-3 text-sm'} ${variantStyles[variant]}`}>
      <div className="flex items-start gap-2">
        {icon && <div className="mt-0.5">{icon}</div>}
        <div className="flex-1 min-w-0">
          <div className="font-medium">{title}</div>
          {description && (
            <div className="mt-1 text-xs opacity-90 whitespace-pre-wrap">
              {expandable && description.length > 150 ? (
                <>
                  {isExpanded ? description : `${description.slice(0, 150)}...`}
                  <button
                    onClick={() => setIsExpanded(!isExpanded)}
                    className="ml-2 underline hover:no-underline"
                  >
                    {isExpanded ? 'Show less' : 'Show more'}
                  </button>
                </>
              ) : (
                description
              )}
            </div>
          )}
          {details && isExpanded && (
            <pre className="mt-2 text-xs bg-black/10 dark:bg-white/10 p-2 rounded overflow-x-auto">
              {details}
            </pre>
          )}
        </div>
      </div>
    </Card>
  );
}
