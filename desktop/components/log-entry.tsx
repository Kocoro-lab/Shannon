"use client";

import { memo, useState, useCallback } from 'react';
import { ChevronDown, ChevronRight, Clock, Box, Zap } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';
import {
  type LogLevel,
  type Component,
  type ServerLogEvent,
  type StateChangeEvent,
  type RequestEvent,
  type HealthCheckEvent,
  formatDuration,
} from '@/lib/ipc-events';
import { LogEntry } from '@/lib/use-server-logs';
import { cn } from '@/lib/utils';

interface LogEntryProps {
  log: LogEntry;
  className?: string;
}

/**
 * Get background color class for log level.
 */
function getLogLevelBgColor(level: LogLevel): string {
  const colors: Record<LogLevel, string> = {
    trace: 'bg-gray-500/10 text-gray-400 border-gray-500/20',
    debug: 'bg-blue-500/10 text-blue-400 border-blue-500/20',
    info: 'bg-green-500/10 text-green-400 border-green-500/20',
    warn: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20',
    error: 'bg-red-500/10 text-red-400 border-red-500/20',
    critical: 'bg-purple-500/10 text-purple-400 border-purple-500/20',
  };
  return colors[level] || 'bg-gray-500/10 text-gray-400 border-gray-500/20';
}

/**
 * Get border color for log level.
 */
function getLogLevelBorderColor(level: LogLevel): string {
  const colors: Record<LogLevel, string> = {
    trace: 'border-l-gray-500',
    debug: 'border-l-blue-500',
    info: 'border-l-green-500',
    warn: 'border-l-yellow-500',
    error: 'border-l-red-500',
    critical: 'border-l-purple-500',
  };
  return colors[level] || 'border-l-gray-500';
}

/**
 * Format timestamp to HH:MM:SS.mmm
 */
function formatTimestamp(date: Date): string {
  const hours = date.getHours().toString().padStart(2, '0');
  const minutes = date.getMinutes().toString().padStart(2, '0');
  const seconds = date.getSeconds().toString().padStart(2, '0');
  const ms = date.getMilliseconds().toString().padStart(3, '0');
  return `${hours}:${minutes}:${seconds}.${ms}`;
}

/**
 * Individual log entry component with expandable details.
 */
export const LogEntryComponent = memo(function LogEntryComponent({ log, className }: LogEntryProps) {
  const [expanded, setExpanded] = useState(false);

  const toggleExpanded = useCallback(() => {
    setExpanded(prev => !prev);
  }, []);

  const serverLog = log.type === 'log' ? (log.data as ServerLogEvent) : null;
  const stateChange = log.type === 'state' ? (log.data as StateChangeEvent) : null;
  const requestEvent = log.type === 'request' ? (log.data as RequestEvent) : null;
  const healthEvent = log.type === 'health' ? (log.data as HealthCheckEvent) : null;

  const hasDetails = serverLog?.context || serverLog?.error || stateChange || requestEvent || healthEvent;

  return (
    <Card
      className={cn(
        "p-3 border-l-4 cursor-pointer hover:bg-accent/5 transition-colors",
        log.level && getLogLevelBorderColor(log.level),
        className
      )}
      onClick={toggleExpanded}
    >
      {/* Header Row */}
      <div className="flex items-start gap-2">
        {/* Expand/Collapse Icon */}
        <div className="flex-shrink-0 mt-0.5">
          {hasDetails ? (
            expanded ? (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronRight className="h-4 w-4 text-muted-foreground" />
            )
          ) : (
            <div className="h-4 w-4" />
          )}
        </div>

        {/* Timestamp */}
        <div className="flex-shrink-0 flex items-center gap-1 text-xs text-muted-foreground font-mono min-w-[110px]">
          <Clock className="h-3 w-3" />
          {formatTimestamp(log.timestamp)}
        </div>

        {/* Log Level Badge */}
        {log.level && (
          <Badge
            variant="outline"
            className={cn(
              "flex-shrink-0 text-xs font-medium uppercase",
              getLogLevelBgColor(log.level)
            )}
          >
            {log.level}
          </Badge>
        )}

        {/* Component Badge */}
        {log.component && (
          <Badge variant="secondary" className="flex-shrink-0 text-xs">
            <Box className="h-3 w-3 mr-1" />
            {log.component}
          </Badge>
        )}

        {/* Duration Badge */}
        {serverLog?.duration_ms != null && (
          <Badge variant="outline" className="flex-shrink-0 text-xs">
            <Zap className="h-3 w-3 mr-1" />
            {formatDuration(serverLog.duration_ms ?? 0)}
          </Badge>
        )}
        {requestEvent?.duration_ms != null && (
          <Badge variant="outline" className="flex-shrink-0 text-xs">
            <Zap className="h-3 w-3 mr-1" />
            {formatDuration(requestEvent.duration_ms ?? 0)}
          </Badge>
        )}

        {/* Message */}
        <div className="flex-1 text-sm font-mono break-words">
          {log.message}
        </div>
      </div>

      {/* Expanded Details */}
      {expanded && hasDetails && (
        <div className="mt-3 pl-6 space-y-2 border-t pt-2">
          {/* State Change Details */}
          {stateChange && (
            <div className="text-xs space-y-1">
              <div className="text-muted-foreground">State Transition:</div>
              <div className="font-mono bg-muted/50 p-2 rounded">
                {stateChange.from && (
                  <div>From: <span className="text-yellow-400">{stateChange.from}</span></div>
                )}
                <div>To: <span className="text-green-400">{stateChange.to}</span></div>
                {stateChange.port && (
                  <div>Port: <span className="text-blue-400">{stateChange.port}</span></div>
                )}
              </div>
            </div>
          )}

          {/* Request Event Details */}
          {requestEvent && (
            <div className="text-xs space-y-1">
              <div className="text-muted-foreground">Request Details:</div>
              <div className="font-mono bg-muted/50 p-2 rounded space-y-0.5">
                <div>Method: <span className="text-blue-400">{requestEvent.method}</span></div>
                <div>Path: <span className="text-green-400">{requestEvent.path}</span></div>
                <div>Event: <span className="text-yellow-400">{requestEvent.event_type}</span></div>
                {requestEvent.status_code && (
                  <div>Status: <span className="text-purple-400">{requestEvent.status_code}</span></div>
                )}
              </div>
            </div>
          )}

          {/* Health Event Details */}
          {healthEvent && (
            <div className="text-xs space-y-1">
              <div className="text-muted-foreground">Health Check:</div>
              <div className="font-mono bg-muted/50 p-2 rounded space-y-0.5">
                <div>Status: <span className={healthEvent.status === 'healthy' ? "text-green-400" : "text-red-400"}>
                  {healthEvent.status === 'healthy' ? "Healthy" : "Unhealthy"}
                </span></div>
                {/* Uptime removed as it is not in payload */}
              </div>
            </div>
          )}

          {/* Error Details */}
          {serverLog?.error && (
            <div className="text-xs space-y-1">
              <div className="text-red-400 font-medium">Error Details:</div>
              <div className="font-mono bg-red-500/10 p-2 rounded border border-red-500/20 space-y-1">
                <div>Type: <span className="text-red-300">{serverLog.error.type}</span></div>
                <div>Message: <span className="text-red-200">{serverLog.error.message}</span></div>
                {serverLog.error.stack && (
                  <div className="mt-2">
                    <div className="text-red-300 mb-1">Stack Trace:</div>
                    <pre className="text-xs text-red-200 whitespace-pre-wrap bg-red-950/50 p-2 rounded max-h-40 overflow-y-auto">
                      {serverLog.error.stack}
                    </pre>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Context */}
          {serverLog?.context && Object.keys(serverLog.context).length > 0 && (
            <div className="text-xs space-y-1">
              <div className="text-muted-foreground">Context:</div>
              <pre className="font-mono bg-muted/50 p-2 rounded text-xs overflow-x-auto">
                {JSON.stringify(serverLog.context, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </Card>
  );
});

LogEntryComponent.displayName = 'LogEntryComponent';
