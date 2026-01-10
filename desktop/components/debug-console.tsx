"use client";

import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { Trash2, Download, Search, Filter, ChevronRight, Activity } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import type { LogLevel, Component } from '@/lib/ipc-events';
import { useServerLogs } from '@/lib/use-server-logs';
import { LogEntryComponent } from '@/components/log-entry';
import { cn } from '@/lib/utils';

interface DebugConsoleProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Comprehensive debug console component for real-time server log monitoring.
 *
 * Features:
 * - Real-time log streaming from IPC events
 * - Filtering by log level and component
 * - Search with debouncing
 * - Auto-scroll toggle
 * - State timeline visualization
 * - Statistics footer
 * - Export and clear functions
 * - Performance optimizations
 */
export function DebugConsole({ open, onOpenChange }: DebugConsoleProps) {
  const {
    logs,
    stateHistory,
    latestHealth,
    searchLogs,
    clearLogs,
    getLogCountByLevel,
    hasLogs,
  } = useServerLogs();

  // Filter state
  const [levelFilter, setLevelFilter] = useState<LogLevel | 'all'>('all');
  const [componentFilter, setComponentFilter] = useState<Component | 'all'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);

  // Refs
  const scrollAreaRef = useRef<HTMLDivElement>(null);
  const scrollEndRef = useRef<HTMLDivElement>(null);

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchQuery);
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery]);

  // Get unique components from logs
  const availableComponents = useMemo(() => {
    const components = new Set<Component>();
    logs.forEach(log => {
      if (log.component) {
        components.add(log.component);
      }
    });
    return Array.from(components).sort();
  }, [logs]);

  // Filter logs based on current filters
  const filteredLogs = useMemo(() => {
    let result = logs;

    // Apply level filter
    if (levelFilter !== 'all') {
      result = result.filter(log => log.level === levelFilter);
    }

    // Apply component filter
    if (componentFilter !== 'all') {
      result = result.filter(log => log.component === componentFilter);
    }

    // Apply search filter
    if (debouncedSearch.trim()) {
      result = searchLogs(debouncedSearch);

      // Re-apply level and component filters after search
      if (levelFilter !== 'all') {
        result = result.filter(log => log.level === levelFilter);
      }
      if (componentFilter !== 'all') {
        result = result.filter(log => log.component === componentFilter);
      }
    }

    return result;
  }, [logs, levelFilter, componentFilter, debouncedSearch, searchLogs]);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll && scrollEndRef.current && filteredLogs.length > 0) {
      scrollEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [filteredLogs.length, autoScroll]);

  // Calculate statistics
  const stats = useMemo(() => {
    const levelCounts = getLogCountByLevel();
    return {
      total: logs.length,
      filtered: filteredLogs.length,
      errors: levelCounts.error + levelCounts.critical,
      warnings: levelCounts.warn,
    };
  }, [logs.length, filteredLogs.length, getLogCountByLevel]);

  // Export logs to JSON
  const handleExport = useCallback(() => {
    const data = {
      exported_at: new Date().toISOString(),
      total_logs: logs.length,
      logs: filteredLogs,
      state_history: stateHistory,
      latest_health: latestHealth,
    };

    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `shannon-logs-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }, [logs, filteredLogs, stateHistory, latestHealth]);

  // Reset all filters
  const handleResetFilters = useCallback(() => {
    setLevelFilter('all');
    setComponentFilter('all');
    setSearchQuery('');
    setDebouncedSearch('');
  }, []);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-4xl p-0 flex flex-col">
        <SheetHeader className="px-6 py-4 border-b">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Activity className="h-5 w-5 text-primary" />
              <SheetTitle>Debug Console</SheetTitle>
            </div>
          </div>
        </SheetHeader>

        {/* State Timeline */}
        {stateHistory.length > 0 && (
          <div className="px-6 py-3 border-b bg-muted/30">
            <div className="text-xs font-medium text-muted-foreground mb-2">State Timeline</div>
            <div className="flex items-center gap-2 flex-wrap">
              {stateHistory.slice(-10).map((state, index, array) => (
                <div key={`${state.timestamp}-${state.to}`} className="flex items-center gap-2">
                  <Badge
                    variant={state.to === 'ready' ? 'default' : state.to === 'failed' ? 'destructive' : 'secondary'}
                    className="text-xs"
                  >
                    {state.to}
                  </Badge>
                  {index < array.length - 1 && (
                    <ChevronRight className="h-3 w-3 text-muted-foreground" />
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Filters and Controls */}
        <div className="px-6 py-3 border-b space-y-3">
          {/* Search and Filter Row */}
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search logs..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-9"
              />
            </div>

            <Select value={levelFilter} onValueChange={(v) => setLevelFilter(v as LogLevel | 'all')}>
              <SelectTrigger className="w-32">
                <SelectValue placeholder="Level" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Levels</SelectItem>
                <SelectItem value="trace">Trace</SelectItem>
                <SelectItem value="debug">Debug</SelectItem>
                <SelectItem value="info">Info</SelectItem>
                <SelectItem value="warn">Warn</SelectItem>
                <SelectItem value="error">Error</SelectItem>
                <SelectItem value="critical">Critical</SelectItem>
              </SelectContent>
            </Select>

            <Select value={componentFilter} onValueChange={(v) => setComponentFilter(v as Component | 'all')}>
              <SelectTrigger className="w-40">
                <SelectValue placeholder="Component" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Components</SelectItem>
                {availableComponents.map(component => (
                  <SelectItem key={component} value={component}>
                    {component}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Action Buttons Row */}
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleResetFilters}
              disabled={levelFilter === 'all' && componentFilter === 'all' && !searchQuery}
            >
              <Filter className="h-3 w-3 mr-2" />
              Reset Filters
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={() => setAutoScroll(!autoScroll)}
              className={cn(autoScroll && "bg-primary/10")}
            >
              Auto-scroll {autoScroll ? 'ON' : 'OFF'}
            </Button>

            <div className="flex-1" />

            <Button
              variant="outline"
              size="sm"
              onClick={handleExport}
              disabled={!hasLogs}
            >
              <Download className="h-3 w-3 mr-2" />
              Export
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={clearLogs}
              disabled={!hasLogs}
            >
              <Trash2 className="h-3 w-3 mr-2" />
              Clear
            </Button>
          </div>
        </div>

        {/* Log Display Area */}
        <ScrollArea className="flex-1 px-6" ref={scrollAreaRef}>
          <div className="py-4 space-y-2">
            {filteredLogs.length === 0 ? (
              <Card className="p-8 text-center">
                <div className="text-muted-foreground">
                  {hasLogs ? (
                    <>
                      <Filter className="h-8 w-8 mx-auto mb-2 opacity-50" />
                      <p>No logs match the current filters</p>
                      <Button
                        variant="link"
                        size="sm"
                        onClick={handleResetFilters}
                        className="mt-2"
                      >
                        Reset filters
                      </Button>
                    </>
                  ) : (
                    <>
                      <Activity className="h-8 w-8 mx-auto mb-2 opacity-50" />
                      <p>No logs yet</p>
                      <p className="text-xs mt-1">Logs will appear here as the server operates</p>
                    </>
                  )}
                </div>
              </Card>
            ) : (
              <>
                {filteredLogs.map((log) => (
                  <LogEntryComponent key={log.id} log={log} />
                ))}
                <div ref={scrollEndRef} />
              </>
            )}
          </div>
        </ScrollArea>

        {/* Statistics Footer */}
        <div className="px-6 py-3 border-t bg-muted/30">
          <div className="flex items-center gap-4 text-xs">
            <div className="flex items-center gap-1.5">
              <span className="text-muted-foreground">Total:</span>
              <Badge variant="outline" className="font-mono">
                {stats.total}
              </Badge>
            </div>

            {(levelFilter !== 'all' || componentFilter !== 'all' || debouncedSearch) && (
              <>
                <Separator orientation="vertical" className="h-4" />
                <div className="flex items-center gap-1.5">
                  <span className="text-muted-foreground">Filtered:</span>
                  <Badge variant="outline" className="font-mono">
                    {stats.filtered}
                  </Badge>
                </div>
              </>
            )}

            <Separator orientation="vertical" className="h-4" />

            <div className="flex items-center gap-1.5">
              <span className="text-muted-foreground">Errors:</span>
              <Badge variant={stats.errors > 0 ? 'destructive' : 'outline'} className="font-mono">
                {stats.errors}
              </Badge>
            </div>

            <div className="flex items-center gap-1.5">
              <span className="text-muted-foreground">Warnings:</span>
              <Badge variant={stats.warnings > 0 ? 'outline' : 'outline'} className="font-mono">
                {stats.warnings}
              </Badge>
            </div>

            {latestHealth && (
              <>
                <Separator orientation="vertical" className="h-4" />
                <div className="flex items-center gap-1.5">
                  <span className="text-muted-foreground">Health:</span>
                  <Badge
                    variant={latestHealth.status === 'healthy' ? 'default' : 'destructive'}
                    className="font-mono"
                  >
                    {latestHealth.status === 'healthy' ? 'OK' : 'FAIL'}
                  </Badge>
                </div>
              </>
            )}

            <div className="flex-1" />

            <div className="text-muted-foreground">
              Max: {Math.floor((logs.length / 1000) * 100)}% of 1000 logs
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
