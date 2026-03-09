"use client";

import { Globe, Loader2, CheckCircle, XCircle, MousePointer, FileText, Camera, FormInput, Code } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

// Human-readable tool names for browser automation (9 core Playwright tools)
const BROWSER_TOOL_DISPLAY: Record<string, { name: string; icon: React.ReactNode }> = {
    // Core navigation
    browser_navigate: { name: "Navigating", icon: <Globe className="h-3 w-3" /> },
    navigate: { name: "Navigating", icon: <Globe className="h-3 w-3" /> },
    // Interactions
    browser_click: { name: "Clicking", icon: <MousePointer className="h-3 w-3" /> },
    click: { name: "Clicking", icon: <MousePointer className="h-3 w-3" /> },
    browser_type: { name: "Typing", icon: <FormInput className="h-3 w-3" /> },
    type: { name: "Typing", icon: <FormInput className="h-3 w-3" /> },
    browser_scroll: { name: "Scrolling", icon: <MousePointer className="h-3 w-3" /> },
    scroll: { name: "Scrolling", icon: <MousePointer className="h-3 w-3" /> },
    // Data extraction
    browser_extract: { name: "Extracting", icon: <FileText className="h-3 w-3" /> },
    extract: { name: "Extracting", icon: <FileText className="h-3 w-3" /> },
    browser_screenshot: { name: "Capturing", icon: <Camera className="h-3 w-3" /> },
    screenshot: { name: "Capturing", icon: <Camera className="h-3 w-3" /> },
    // Waiting & evaluation
    browser_wait: { name: "Waiting", icon: <Loader2 className="h-3 w-3" /> },
    wait: { name: "Waiting", icon: <Loader2 className="h-3 w-3" /> },
    browser_evaluate: { name: "Running script", icon: <Code className="h-3 w-3" /> },
    evaluate: { name: "Running script", icon: <Code className="h-3 w-3" /> },
    // Session management
    browser_close: { name: "Closing session", icon: <XCircle className="h-3 w-3" /> },
    close: { name: "Closing session", icon: <XCircle className="h-3 w-3" /> },
};

function getToolDisplay(tool: string | null): { name: string; icon: React.ReactNode } {
    if (!tool) return { name: "Processing", icon: <Loader2 className="h-3 w-3" /> };
    const lower = tool.toLowerCase();
    return BROWSER_TOOL_DISPLAY[lower] || { name: tool, icon: <Globe className="h-3 w-3" /> };
}

interface BrowserToolExecution {
    tool: string;
    status: "running" | "completed" | "failed";
    message?: string;
    timestamp: string;
}

interface BrowserModeIndicatorProps {
    isActive: boolean;
    autoDetected?: boolean;
    currentTool?: string | null;
    iteration?: number;
    totalIterations?: number | null;
    toolHistory?: BrowserToolExecution[];
    className?: string;
}

export function BrowserModeIndicator({
    isActive,
    autoDetected,
    currentTool,
    iteration = 0,
    totalIterations,
    toolHistory = [],
    className,
}: BrowserModeIndicatorProps) {
    if (!isActive) return null;

    const toolDisplay = getToolDisplay(currentTool ?? null);
    const completedTools = toolHistory.filter(t => t.status === "completed").length;
    const failedTools = toolHistory.filter(t => t.status === "failed").length;

    return (
        <div className={cn("flex items-center gap-2 text-sm", className)}>
            {/* Main browser mode badge */}
            <TooltipProvider>
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge 
                            variant="secondary" 
                            className="gap-1.5 bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border-blue-200 dark:border-blue-800"
                        >
                            <Globe className="h-3.5 w-3.5" />
                            <span>Web Automation</span>
                            {autoDetected && (
                                <span className="text-blue-500/70 dark:text-blue-400/70 text-xs">(auto)</span>
                            )}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent side="bottom" className="max-w-xs">
                        <div className="space-y-1">
                            <p className="font-medium">Browser Automation Mode</p>
                            <p className="text-xs text-muted-foreground">
                                Multi-step web interactions via React workflow.
                                {autoDetected && " Role was auto-detected from your query."}
                            </p>
                            <div className="text-xs text-amber-600 dark:text-amber-400 pt-1">
                                ⚠️ No CAPTCHA, no login, 5min timeout
                            </div>
                        </div>
                    </TooltipContent>
                </Tooltip>
            </TooltipProvider>

            {/* Current tool indicator */}
            {currentTool && (
                <div className="flex items-center gap-1.5 text-muted-foreground animate-pulse">
                    <Loader2 className="h-3 w-3 animate-spin text-blue-500" />
                    <span className="text-xs">{toolDisplay.name}...</span>
                </div>
            )}

            {/* Iteration counter */}
            {iteration > 0 && (
                <span className="text-xs text-muted-foreground">
                    Step {iteration}
                    {totalIterations ? ` of ~${totalIterations}` : ''}
                </span>
            )}

            {/* Tool history summary (compact) */}
            {toolHistory.length > 0 && !currentTool && (
                <div className="flex items-center gap-1 text-xs text-muted-foreground">
                    {completedTools > 0 && (
                        <span className="flex items-center gap-0.5 text-green-600 dark:text-green-400">
                            <CheckCircle className="h-3 w-3" />
                            {completedTools}
                        </span>
                    )}
                    {failedTools > 0 && (
                        <span className="flex items-center gap-0.5 text-red-600 dark:text-red-400">
                            <XCircle className="h-3 w-3" />
                            {failedTools}
                        </span>
                    )}
                </div>
            )}
        </div>
    );
}

// Compact version for tight spaces
export function BrowserModeIndicatorCompact({
    isActive,
    currentTool,
}: {
    isActive: boolean;
    currentTool?: string | null;
}) {
    if (!isActive) return null;

    return (
        <TooltipProvider>
            <Tooltip>
                <TooltipTrigger asChild>
                    <div className="flex items-center gap-1">
                        <Globe className={cn(
                            "h-4 w-4 text-blue-500",
                            currentTool && "animate-pulse"
                        )} />
                        {currentTool && (
                            <Loader2 className="h-3 w-3 animate-spin text-blue-500" />
                        )}
                    </div>
                </TooltipTrigger>
                <TooltipContent>
                    <p>Browser Automation Active</p>
                    {currentTool && <p className="text-xs text-muted-foreground">{getToolDisplay(currentTool).name}...</p>}
                </TooltipContent>
            </Tooltip>
        </TooltipProvider>
    );
}

// Limitations banner for browser mode
export function BrowserLimitationsBanner({ className }: { className?: string }) {
    return (
        <div className={cn(
            "px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border-b border-amber-200 dark:border-amber-800",
            className
        )}>
            <p className="text-xs text-amber-700 dark:text-amber-300 flex items-center gap-1.5">
                <span>⚠️</span>
                <span>
                    Browser automation limitations: No CAPTCHA solving, no authenticated sessions, 
                    no file downloads. Sessions timeout after 5 minutes of inactivity.
                </span>
            </p>
        </div>
    );
}

