"use client";

import { useState, useEffect, useCallback } from 'react';
import { Bug } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { DebugConsole } from '@/components/debug-console';
import { useServerLogs } from '@/lib/use-server-logs';
import { cn } from '@/lib/utils';

/**
 * Debug console wrapper with keyboard shortcuts and floating toggle button.
 * 
 * Keyboard shortcuts:
 * - Ctrl/Cmd + D: Toggle debug console
 * - Ctrl/Cmd + L: Clear logs (when console is open)
 * - ESC: Close debug console
 */
export function DebugConsoleWrapper() {
  const [open, setOpen] = useState(false);
  const { clearLogs, getLogCountByLevel } = useServerLogs();

  // Handle keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const isMac = typeof window !== 'undefined' && navigator.platform.toUpperCase().indexOf('MAC') >= 0;
      const modifierKey = isMac ? event.metaKey : event.ctrlKey;

      // Ctrl/Cmd + D: Toggle debug console
      if (modifierKey && event.key === 'd') {
        event.preventDefault();
        setOpen(prev => !prev);
        return;
      }

      // Ctrl/Cmd + L: Clear logs (when console is open)
      if (modifierKey && event.key === 'l' && open) {
        event.preventDefault();
        clearLogs();
        return;
      }

      // ESC: Close debug console
      if (event.key === 'Escape' && open) {
        event.preventDefault();
        setOpen(false);
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, clearLogs]);

  // Get error count for badge
  const logCounts = getLogCountByLevel();
  const errorCount = logCounts.error + logCounts.critical;
  const hasErrors = errorCount > 0;

  const handleToggle = useCallback(() => {
    setOpen(prev => !prev);
  }, []);

  return (
    <>
      {/* Floating Toggle Button */}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              onClick={handleToggle}
              size="icon"
              variant={hasErrors ? "destructive" : "outline"}
              className={cn(
                "fixed bottom-6 right-6 h-12 w-12 rounded-full shadow-lg z-40 transition-all",
                "hover:scale-110",
                hasErrors && "animate-pulse"
              )}
            >
              <div className="relative">
                <Bug className="h-5 w-5" />
                {errorCount > 0 && (
                  <Badge
                    variant="destructive"
                    className="absolute -top-2 -right-2 h-5 w-5 flex items-center justify-center p-0 text-xs"
                  >
                    {errorCount > 9 ? '9+' : errorCount}
                  </Badge>
                )}
              </div>
            </Button>
          </TooltipTrigger>
          <TooltipContent side="left">
            <div className="text-xs space-y-1">
              <p className="font-medium">Debug Console</p>
              <p className="text-muted-foreground">
                {typeof navigator !== 'undefined' && navigator.platform.toUpperCase().indexOf('MAC') >= 0
                  ? 'Cmd + D'
                  : 'Ctrl + D'}
              </p>
            </div>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      {/* Debug Console Sheet */}
      <DebugConsole open={open} onOpenChange={setOpen} />
    </>
  );
}
