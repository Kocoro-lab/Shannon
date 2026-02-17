"use client";

/* eslint-disable @typescript-eslint/no-explicit-any */

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ScheduleBuilder } from "@/components/schedule-builder";
import {
    Search,
    Loader2,
    RefreshCw,
    Plus,
    Play,
    Pause,
    Trash2,
    Pencil,
    ChevronDown,
    ChevronRight,
    Clock,
    CheckCircle2,
    XCircle,
    AlertCircle,
    Zap,
    DollarSign,
    Timer,
    Sparkles,
    Microscope,
    Settings2,
} from "lucide-react";
import { useEffect, useState, useCallback } from "react";
import {
    listSchedules,
    getScheduleRuns,
    pauseSchedule,
    resumeSchedule,
    deleteSchedule,
    createSchedule,
    updateSchedule,
    ScheduleInfo,
    ScheduleRun,
    ScheduleStatus,
    CreateScheduleRequest,
    UpdateScheduleRequest,
} from "@/lib/shannon/api";

// Common timezones for dropdown
const COMMON_TIMEZONES = [
    "UTC",
    "America/New_York",
    "America/Chicago",
    "America/Denver",
    "America/Los_Angeles",
    "America/Toronto",
    "America/Sao_Paulo",
    "Europe/London",
    "Europe/Paris",
    "Europe/Berlin",
    "Europe/Moscow",
    "Asia/Dubai",
    "Asia/Shanghai",
    "Asia/Tokyo",
    "Asia/Seoul",
    "Asia/Singapore",
    "Asia/Kolkata",
    "Australia/Sydney",
    "Pacific/Auckland",
];

// Workflow type definitions
type WorkflowType =
    | "auto"
    | "research_quick"
    | "research_standard"
    | "research_deep"
    | "research_academic"
    | "custom";

interface WorkflowTypeOption {
    value: WorkflowType;
    label: string;
    description: string;
    icon: React.ElementType;
    taskContext: Record<string, any> | null;
}

const WORKFLOW_TYPES: WorkflowTypeOption[] = [
    {
        value: "auto",
        label: "Auto (Simple)",
        description: "Automatically routes based on query complexity",
        icon: Sparkles,
        taskContext: null,
    },
    {
        value: "research_quick",
        label: "Research - Quick",
        description: "Fast research with minimal sources",
        icon: Microscope,
        taskContext: { research_strategy: "quick", force_research: "true" },
    },
    {
        value: "research_standard",
        label: "Research - Standard",
        description: "Balanced depth and speed",
        icon: Microscope,
        taskContext: { research_strategy: "standard", force_research: "true" },
    },
    {
        value: "research_deep",
        label: "Research - Deep",
        description: "Comprehensive analysis with many sources",
        icon: Microscope,
        taskContext: { research_strategy: "deep", force_research: "true" },
    },
    {
        value: "research_academic",
        label: "Research - Academic",
        description: "Scholarly sources with citations",
        icon: Microscope,
        taskContext: { research_strategy: "academic", force_research: "true" },
    },
    {
        value: "custom",
        label: "Custom (Advanced)",
        description: "Define custom task context JSON",
        icon: Settings2,
        taskContext: null,
    },
];

// Derive workflow type from task_context
function getWorkflowTypeFromContext(taskContext?: Record<string, any>): WorkflowType {
    if (!taskContext || Object.keys(taskContext).length === 0) {
        return "auto";
    }

    // Check research strategies
    if (taskContext.force_research || taskContext.research_strategy) {
        const strategy = taskContext.research_strategy;
        if (strategy === "quick") return "research_quick";
        if (strategy === "standard") return "research_standard";
        if (strategy === "deep") return "research_deep";
        if (strategy === "academic") return "research_academic";
        return "research_standard"; // Default research
    }

    // Has custom context
    return "custom";
}

// Get workflow type display info
function getWorkflowTypeInfo(type: WorkflowType): WorkflowTypeOption {
    return WORKFLOW_TYPES.find((t) => t.value === type) || WORKFLOW_TYPES[0];
}

// Workflow type badge component
function WorkflowTypeBadge({ taskContext }: { taskContext?: Record<string, any> }) {
    const type = getWorkflowTypeFromContext(taskContext);
    const info = getWorkflowTypeInfo(type);
    const Icon = info.icon;

    if (type === "auto") {
        return (
            <Badge variant="secondary" className="text-xs">
                <Icon className="h-3 w-3 mr-1" />
                Auto
            </Badge>
        );
    }

    if (type.startsWith("research_")) {
        return (
            <Badge className="bg-violet-500/15 text-violet-600 dark:text-violet-400 border-violet-500/30 text-xs">
                <Icon className="h-3 w-3 mr-1" />
                {info.label.replace("Research - ", "")}
            </Badge>
        );
    }

    return (
        <Badge variant="outline" className="text-xs">
            <Icon className="h-3 w-3 mr-1" />
            Custom
        </Badge>
    );
}

// Helper to format cron expression in human-readable form
function formatCron(cron: string): string {
    const parts = cron.split(" ");
    if (parts.length !== 5) return cron;

    const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;
    const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

    // Every N minutes: */N * * * *
    if (minute.startsWith("*/") && hour === "*" && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        const interval = minute.slice(2);
        return `Every ${interval} min`;
    }

    // Every hour at :MM
    if (minute !== "*" && hour === "*" && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        return `Every hour at :${minute.padStart(2, "0")}`;
    }

    // Every N hours: 0 */N * * *
    if (minute === "0" && hour.startsWith("*/") && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        const interval = hour.slice(2);
        return `Every ${interval} hours`;
    }

    // Daily at HH:MM
    if (minute !== "*" && hour !== "*" && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        return `Daily at ${hour}:${minute.padStart(2, "0")}`;
    }

    // Weekly on specific day
    if (dayOfWeek !== "*" && dayOfMonth === "*" && month === "*") {
        const dayNum = parseInt(dayOfWeek);
        const dayName = days[dayNum] || dayOfWeek;
        if (hour !== "*" && minute !== "*") {
            return `${dayName} at ${hour}:${minute.padStart(2, "0")}`;
        }
        return `Weekly on ${dayName}`;
    }

    // Weekdays (Mon-Fri)
    if (dayOfWeek === "1-5" && dayOfMonth === "*" && month === "*") {
        if (hour !== "*" && minute !== "*") {
            return `Weekdays at ${hour}:${minute.padStart(2, "0")}`;
        }
        return "Weekdays";
    }

    // Monthly on specific day
    if (dayOfMonth !== "*" && month === "*" && dayOfWeek === "*") {
        if (hour !== "*" && minute !== "*") {
            return `Monthly on ${dayOfMonth} at ${hour}:${minute.padStart(2, "0")}`;
        }
        return `Monthly on day ${dayOfMonth}`;
    }

    return cron;
}

// Helper to format relative time
function formatRelativeTime(dateStr: string | undefined): string {
    if (!dateStr) return "—";
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffMins = Math.round(diffMs / 60000);
    const diffHours = Math.round(diffMs / 3600000);
    const diffDays = Math.round(diffMs / 86400000);

    if (Math.abs(diffMins) < 60) {
        if (diffMins === 0) return "now";
        return diffMins > 0 ? `in ${diffMins}m` : `${Math.abs(diffMins)}m ago`;
    }
    if (Math.abs(diffHours) < 24) {
        return diffHours > 0 ? `in ${diffHours}h` : `${Math.abs(diffHours)}h ago`;
    }
    return diffDays > 0 ? `in ${diffDays}d` : `${Math.abs(diffDays)}d ago`;
}

// Helper to format duration
function formatDuration(ms: number | string | undefined): string {
    const msNum = typeof ms === "string" ? parseFloat(ms) : ms;
    if (!msNum) return "—";
    if (msNum < 1000) return `${msNum}ms`;
    if (msNum < 60000) return `${(msNum / 1000).toFixed(1)}s`;
    return `${(msNum / 60000).toFixed(1)}m`;
}

// Status badge component for schedules
function ScheduleStatusBadge({ status }: { status: ScheduleStatus }) {
    if (status === "ACTIVE") {
        return (
            <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30">
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Active
            </Badge>
        );
    }
    if (status === "PAUSED") {
        return (
            <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30">
                <Pause className="h-3 w-3 mr-1" />
                Paused
            </Badge>
        );
    }
    return (
        <Badge variant="secondary">
            {status}
        </Badge>
    );
}

// Status badge component for runs
function RunStatusBadge({ status }: { status: string }) {
    switch (status) {
        case "COMPLETED":
            return (
                <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30">
                    <CheckCircle2 className="h-3 w-3 mr-1" />
                    Completed
                </Badge>
            );
        case "FAILED":
            return (
                <Badge className="bg-red-500/15 text-red-600 dark:text-red-400 border-red-500/30">
                    <XCircle className="h-3 w-3 mr-1" />
                    Failed
                </Badge>
            );
        case "RUNNING":
            return (
                <Badge className="bg-blue-500/15 text-blue-600 dark:text-blue-400 border-blue-500/30">
                    <Loader2 className="h-3 w-3 mr-1 animate-spin" />
                    Running
                </Badge>
            );
        default:
            return (
                <Badge variant="secondary">
                    <AlertCircle className="h-3 w-3 mr-1" />
                    Unknown
                </Badge>
            );
    }
}

// Schedule row with expandable run history
function ScheduleRow({
    schedule,
    onPause,
    onResume,
    onEdit,
    onDelete,
    pendingAction,
}: {
    schedule: ScheduleInfo;
    onPause: (id: string) => void;
    onResume: (id: string) => void;
    onEdit: (schedule: ScheduleInfo) => void;
    onDelete: (id: string) => void;
    pendingAction: string | null;
}) {
    const [isExpanded, setIsExpanded] = useState(false);
    const [runs, setRuns] = useState<ScheduleRun[]>([]);
    const [isLoadingRuns, setIsLoadingRuns] = useState(false);
    const [runsError, setRunsError] = useState<string | null>(null);
    const [totalRuns, setTotalRuns] = useState(0);
    const [selectedRun, setSelectedRun] = useState<ScheduleRun | null>(null);
    const [hasFetchedRuns, setHasFetchedRuns] = useState(false);

    const isThisPending = pendingAction === schedule.schedule_id;

    const successRate = schedule.total_runs > 0
        ? Math.round((schedule.successful_runs / schedule.total_runs) * 100)
        : null;

    const fetchRuns = useCallback(async () => {
        setIsLoadingRuns(true);
        setRunsError(null);
        try {
            const data = await getScheduleRuns(schedule.schedule_id, 1, 10);
            setRuns(data.runs || []);
            setTotalRuns(data.total_count);
            setHasFetchedRuns(true);
        } catch (err) {
            setRunsError(err instanceof Error ? err.message : "Failed to load runs");
        } finally {
            setIsLoadingRuns(false);
        }
    }, [schedule.schedule_id]);

    useEffect(() => {
        if (isExpanded && !hasFetchedRuns && !isLoadingRuns) {
            fetchRuns();
        }
    }, [isExpanded, hasFetchedRuns, isLoadingRuns, fetchRuns]);

    return (
        <>
            <tr className="border-b transition-colors hover:bg-muted/50">
                <td className="p-4 align-middle">
                    <button
                        onClick={() => setIsExpanded(!isExpanded)}
                        className="flex items-center gap-2 text-left w-full"
                    >
                        {isExpanded ? (
                            <ChevronDown className="h-4 w-4 text-muted-foreground shrink-0" />
                        ) : (
                            <ChevronRight className="h-4 w-4 text-muted-foreground shrink-0" />
                        )}
                        <div className="flex flex-col min-w-0">
                            <span className="font-medium truncate">{schedule.name}</span>
                            {schedule.description && (
                                <span className="text-xs text-muted-foreground truncate max-w-[250px]">
                                    {schedule.description}
                                </span>
                            )}
                        </div>
                    </button>
                </td>
                <td className="p-4 align-middle hidden md:table-cell">
                    <WorkflowTypeBadge taskContext={schedule.task_context} />
                </td>
                <td className="p-4 align-middle hidden lg:table-cell">
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <div className="flex items-center gap-2 cursor-default">
                                    <Clock className="h-4 w-4 text-muted-foreground" />
                                    <span className="text-sm">{formatCron(schedule.cron_expression)}</span>
                                </div>
                            </TooltipTrigger>
                            <TooltipContent>
                                <p className="font-mono text-xs">{schedule.cron_expression}</p>
                                <p className="text-xs text-muted-foreground mt-1">{schedule.timezone}</p>
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </td>
                <td className="p-4 align-middle">
                    <ScheduleStatusBadge status={schedule.status} />
                </td>
                <td className="p-4 align-middle hidden sm:table-cell">
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <span className="text-sm cursor-default">
                                    {formatRelativeTime(schedule.next_run_at)}
                                </span>
                            </TooltipTrigger>
                            <TooltipContent>
                                {schedule.next_run_at
                                    ? new Date(schedule.next_run_at).toLocaleString()
                                    : "No next run scheduled"}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </td>
                <td className="p-4 align-middle hidden xl:table-cell">
                    {successRate !== null ? (
                        <TooltipProvider>
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <div className="flex items-center gap-2 cursor-default">
                                        <div className="w-16 h-2 bg-muted rounded-full overflow-hidden">
                                            <div
                                                className={`h-full ${successRate >= 80 ? "bg-emerald-500" : successRate >= 50 ? "bg-amber-500" : "bg-red-500"}`}
                                                style={{ width: `${successRate}%` }}
                                            />
                                        </div>
                                        <span className="text-sm">{successRate}%</span>
                                    </div>
                                </TooltipTrigger>
                                <TooltipContent>
                                    <p>{schedule.successful_runs} / {schedule.total_runs} runs successful</p>
                                    {schedule.failed_runs > 0 && (
                                        <p className="text-red-400">{schedule.failed_runs} failed</p>
                                    )}
                                </TooltipContent>
                            </Tooltip>
                        </TooltipProvider>
                    ) : (
                        <span className="text-sm text-muted-foreground">—</span>
                    )}
                </td>
                <td className="p-4 align-middle text-right">
                    <div className="flex items-center justify-end gap-1">
                        <TooltipProvider>
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => onEdit(schedule)}
                                        disabled={isThisPending}
                                    >
                                        <Pencil className="h-4 w-4" />
                                    </Button>
                                </TooltipTrigger>
                                <TooltipContent>Edit schedule</TooltipContent>
                            </Tooltip>
                        </TooltipProvider>
                        {schedule.status === "ACTIVE" ? (
                            <TooltipProvider>
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => onPause(schedule.schedule_id)}
                                            disabled={isThisPending}
                                        >
                                            {isThisPending ? (
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                            ) : (
                                                <Pause className="h-4 w-4" />
                                            )}
                                        </Button>
                                    </TooltipTrigger>
                                    <TooltipContent>Pause schedule</TooltipContent>
                                </Tooltip>
                            </TooltipProvider>
                        ) : schedule.status === "PAUSED" ? (
                            <TooltipProvider>
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => onResume(schedule.schedule_id)}
                                            disabled={isThisPending}
                                        >
                                            {isThisPending ? (
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                            ) : (
                                                <Play className="h-4 w-4" />
                                            )}
                                        </Button>
                                    </TooltipTrigger>
                                    <TooltipContent>Resume schedule</TooltipContent>
                                </Tooltip>
                            </TooltipProvider>
                        ) : null}
                        <TooltipProvider>
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => onDelete(schedule.schedule_id)}
                                        disabled={isThisPending}
                                        className="text-red-500 hover:text-red-600 hover:bg-red-500/10"
                                    >
                                        <Trash2 className="h-4 w-4" />
                                    </Button>
                                </TooltipTrigger>
                                <TooltipContent>Delete schedule</TooltipContent>
                            </Tooltip>
                        </TooltipProvider>
                    </div>
                </td>
            </tr>

            {/* Expanded run history */}
            {isExpanded && (
                <tr className="bg-muted/30">
                    <td colSpan={7} className="p-0">
                        <div className="p-4 space-y-3">
                            <div className="flex items-center justify-between">
                                <h4 className="text-sm font-medium">Recent Runs</h4>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={fetchRuns}
                                    disabled={isLoadingRuns}
                                >
                                    <RefreshCw className={`h-3 w-3 ${isLoadingRuns ? "animate-spin" : ""}`} />
                                </Button>
                            </div>

                            {runsError && (
                                <div className="text-sm text-red-500">{runsError}</div>
                            )}

                            {isLoadingRuns ? (
                                <div className="flex items-center justify-center py-4">
                                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                                </div>
                            ) : runs.length === 0 ? (
                                <div className="text-sm text-muted-foreground py-4 text-center">
                                    No runs yet. The schedule will execute at the next scheduled time.
                                </div>
                            ) : (
                                <div className="rounded-md border bg-background">
                                    <table className="w-full text-sm">
                                        <thead>
                                            <tr className="border-b">
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground">Triggered</th>
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground">Status</th>
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground hidden sm:table-cell">Duration</th>
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground hidden md:table-cell">Tokens</th>
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground hidden md:table-cell">Cost</th>
                                                <th className="h-9 px-3 text-left font-medium text-muted-foreground hidden lg:table-cell">Model</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {runs.map((run) => (
                                                <tr
                                                    key={run.workflow_id}
                                                    className="border-b last:border-0 hover:bg-muted/50 cursor-pointer"
                                                    onClick={() => setSelectedRun(run)}
                                                >
                                                    <td className="p-3">
                                                        <TooltipProvider>
                                                            <Tooltip>
                                                                <TooltipTrigger asChild>
                                                                    <span className="cursor-default">
                                                                        {formatRelativeTime(run.triggered_at)}
                                                                    </span>
                                                                </TooltipTrigger>
                                                                <TooltipContent>
                                                                    {new Date(run.triggered_at).toLocaleString()}
                                                                </TooltipContent>
                                                            </Tooltip>
                                                        </TooltipProvider>
                                                    </td>
                                                    <td className="p-3">
                                                        <RunStatusBadge status={run.status} />
                                                    </td>
                                                    <td className="p-3 hidden sm:table-cell">
                                                        <div className="flex items-center gap-1.5 text-muted-foreground">
                                                            <Timer className="h-3.5 w-3.5" />
                                                            {formatDuration(run.duration_ms)}
                                                        </div>
                                                    </td>
                                                    <td className="p-3 hidden md:table-cell">
                                                        <div className="flex items-center gap-1.5 text-muted-foreground">
                                                            <Zap className="h-3.5 w-3.5" />
                                                            {run.total_tokens.toLocaleString()}
                                                        </div>
                                                    </td>
                                                    <td className="p-3 hidden md:table-cell">
                                                        <div className="flex items-center gap-1.5 text-muted-foreground">
                                                            <DollarSign className="h-3.5 w-3.5" />
                                                            {run.total_cost_usd.toFixed(4)}
                                                        </div>
                                                    </td>
                                                    <td className="p-3 hidden lg:table-cell">
                                                        <span className="text-muted-foreground text-xs">
                                                            {run.model_used || "—"}
                                                        </span>
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                    {totalRuns > runs.length && (
                                        <div className="px-3 py-2 text-xs text-muted-foreground border-t">
                                            Showing {runs.length} of {totalRuns} runs
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>

                        {/* Run detail dialog */}
                        <Dialog open={!!selectedRun} onOpenChange={() => setSelectedRun(null)}>
                            <DialogContent className="max-w-2xl">
                                <DialogHeader>
                                    <DialogTitle>Run Details</DialogTitle>
                                    <DialogDescription>
                                        {selectedRun && new Date(selectedRun.triggered_at).toLocaleString()}
                                    </DialogDescription>
                                </DialogHeader>
                                {selectedRun && (
                                    <div className="space-y-4">
                                        <div className="flex items-center gap-4">
                                            <RunStatusBadge status={selectedRun.status} />
                                            {selectedRun.model_used && (
                                                <span className="text-sm text-muted-foreground">
                                                    {selectedRun.provider}/{selectedRun.model_used}
                                                </span>
                                            )}
                                        </div>

                                        <div className="grid grid-cols-3 gap-4 text-sm">
                                            <div>
                                                <div className="text-muted-foreground">Duration</div>
                                                <div className="font-medium">{formatDuration(selectedRun.duration_ms)}</div>
                                            </div>
                                            <div>
                                                <div className="text-muted-foreground">Tokens</div>
                                                <div className="font-medium">{selectedRun.total_tokens.toLocaleString()}</div>
                                            </div>
                                            <div>
                                                <div className="text-muted-foreground">Cost</div>
                                                <div className="font-medium">${selectedRun.total_cost_usd.toFixed(4)}</div>
                                            </div>
                                        </div>

                                        {selectedRun.error_message && (
                                            <div className="p-3 rounded-md bg-red-500/10 border border-red-500/30">
                                                <div className="text-sm font-medium text-red-600 dark:text-red-400 mb-1">Error</div>
                                                <div className="text-sm text-red-600/80 dark:text-red-400/80 whitespace-pre-wrap">
                                                    {selectedRun.error_message}
                                                </div>
                                            </div>
                                        )}

                                        {selectedRun.result && (
                                            <div className="space-y-2">
                                                <div className="text-sm font-medium">Result</div>
                                                <div className="p-3 rounded-md bg-muted text-sm max-h-[300px] overflow-auto whitespace-pre-wrap">
                                                    {selectedRun.result}
                                                </div>
                                            </div>
                                        )}
                                    </div>
                                )}
                            </DialogContent>
                        </Dialog>
                    </td>
                </tr>
            )}
        </>
    );
}

// Schedule form data type
interface ScheduleFormData {
    name: string;
    description: string;
    cron_expression: string;
    timezone: string;
    task_query: string;
    workflow_type: WorkflowType;
    custom_context: string;
    max_budget_per_run_usd: string;
    timeout_seconds: string;
}

const defaultFormData: ScheduleFormData = {
    name: "",
    description: "",
    cron_expression: "0 9 * * *",
    timezone: "UTC",
    task_query: "",
    workflow_type: "auto",
    custom_context: "{}",
    max_budget_per_run_usd: "",
    timeout_seconds: "",
};

// Build task_context from workflow type
// Backend expects map[string]string, so all values must be strings
function buildTaskContext(workflowType: WorkflowType, customContext: string): Record<string, string> | undefined {
    if (workflowType === "custom") {
        try {
            const parsed = JSON.parse(customContext);
            if (Object.keys(parsed).length === 0) return undefined;
            // Convert all values to strings for backend compatibility
            const stringified: Record<string, string> = {};
            for (const [key, value] of Object.entries(parsed)) {
                stringified[key] = typeof value === "string" ? value : JSON.stringify(value);
            }
            return stringified;
        } catch {
            return undefined;
        }
    }

    const typeInfo = WORKFLOW_TYPES.find((t) => t.value === workflowType);
    return typeInfo?.taskContext || undefined;
}

// Create/Edit schedule dialog
function ScheduleFormDialog({
    open,
    onOpenChange,
    onSaved,
    editingSchedule,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onSaved: () => void;
    editingSchedule: ScheduleInfo | null;
}) {
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [formData, setFormData] = useState<ScheduleFormData>(defaultFormData);

    const isEditing = !!editingSchedule;

    // Reset form when opening/closing or when editing schedule changes
    useEffect(() => {
        if (open && editingSchedule) {
            const workflowType = getWorkflowTypeFromContext(editingSchedule.task_context);
            setFormData({
                name: editingSchedule.name,
                description: editingSchedule.description || "",
                cron_expression: editingSchedule.cron_expression,
                timezone: editingSchedule.timezone,
                task_query: editingSchedule.task_query,
                workflow_type: workflowType,
                custom_context: workflowType === "custom" && editingSchedule.task_context
                    ? JSON.stringify(editingSchedule.task_context, null, 2)
                    : "{}",
                max_budget_per_run_usd: editingSchedule.max_budget_per_run_usd?.toString() || "",
                timeout_seconds: editingSchedule.timeout_seconds?.toString() || "",
            });
        } else if (open && !editingSchedule) {
            setFormData(defaultFormData);
        }
        setError(null);
    }, [open, editingSchedule]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setIsSubmitting(true);
        setError(null);

        // Validate custom JSON if needed
        if (formData.workflow_type === "custom") {
            try {
                JSON.parse(formData.custom_context);
            } catch {
                setError("Invalid JSON in custom context");
                setIsSubmitting(false);
                return;
            }
        }

        const taskContext = buildTaskContext(formData.workflow_type, formData.custom_context);

        try {
            if (isEditing) {
                const updateData: UpdateScheduleRequest = {
                    name: formData.name,
                    description: formData.description || undefined,
                    cron_expression: formData.cron_expression,
                    timezone: formData.timezone,
                    task_query: formData.task_query,
                    task_context: taskContext,
                    clear_task_context: !taskContext,
                };
                if (formData.max_budget_per_run_usd) {
                    updateData.max_budget_per_run_usd = parseFloat(formData.max_budget_per_run_usd);
                }
                if (formData.timeout_seconds) {
                    updateData.timeout_seconds = parseInt(formData.timeout_seconds);
                }
                await updateSchedule(editingSchedule!.schedule_id, updateData);
            } else {
                const createData: CreateScheduleRequest = {
                    name: formData.name,
                    description: formData.description || undefined,
                    cron_expression: formData.cron_expression,
                    timezone: formData.timezone,
                    task_query: formData.task_query,
                    task_context: taskContext,
                };
                if (formData.max_budget_per_run_usd) {
                    createData.max_budget_per_run_usd = parseFloat(formData.max_budget_per_run_usd);
                }
                if (formData.timeout_seconds) {
                    createData.timeout_seconds = parseInt(formData.timeout_seconds);
                }
                await createSchedule(createData);
            }
            onSaved();
            onOpenChange(false);
        } catch (err) {
            setError(err instanceof Error ? err.message : `Failed to ${isEditing ? "update" : "create"} schedule`);
        } finally {
            setIsSubmitting(false);
        }
    };

    const selectedTypeInfo = WORKFLOW_TYPES.find((t) => t.value === formData.workflow_type);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>{isEditing ? "Edit Schedule" : "Create Schedule"}</DialogTitle>
                    <DialogDescription>
                        {isEditing
                            ? "Update the schedule configuration."
                            : "Create a new scheduled task that runs automatically."}
                    </DialogDescription>
                </DialogHeader>

                <form
                    onSubmit={handleSubmit}
                    className="space-y-4"
                    onKeyDown={(e) => {
                        // Allow keyboard shortcuts (Cmd/Ctrl + A, C, V, X, Z) to work in inputs
                        if ((e.metaKey || e.ctrlKey) && ['a', 'c', 'v', 'x', 'z'].includes(e.key.toLowerCase())) {
                            e.stopPropagation();
                        }
                    }}
                >
                    {error && (
                        <div className="p-3 rounded-md bg-red-500/10 border border-red-500/30 text-sm text-red-600 dark:text-red-400">
                            {error}
                        </div>
                    )}

                    <div className="space-y-2">
                        <Label htmlFor="name">Name</Label>
                        <Input
                            id="name"
                            placeholder="Daily report"
                            value={formData.name}
                            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                            required
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="description">Description (optional)</Label>
                        <Input
                            id="description"
                            placeholder="Generate daily summary report"
                            value={formData.description}
                            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="workflow_type">Workflow Type</Label>
                        <Select
                            value={formData.workflow_type}
                            onValueChange={(value) => setFormData({ ...formData, workflow_type: value as WorkflowType })}
                        >
                            <SelectTrigger id="workflow_type">
                                <SelectValue placeholder="Select workflow type" />
                            </SelectTrigger>
                            <SelectContent>
                                {WORKFLOW_TYPES.map((type) => {
                                    const Icon = type.icon;
                                    return (
                                        <SelectItem key={type.value} value={type.value}>
                                            <div className="flex items-center gap-2">
                                                <Icon className="h-4 w-4 text-muted-foreground" />
                                                <span>{type.label}</span>
                                            </div>
                                        </SelectItem>
                                    );
                                })}
                            </SelectContent>
                        </Select>
                        {selectedTypeInfo && (
                            <p className="text-xs text-muted-foreground">
                                {selectedTypeInfo.description}
                            </p>
                        )}
                    </div>

                    {formData.workflow_type === "custom" && (
                        <div className="space-y-2">
                            <Label htmlFor="custom_context">Custom Task Context (JSON)</Label>
                            <Textarea
                                id="custom_context"
                                placeholder='{"research_strategy": "deep", "force_research": true}'
                                value={formData.custom_context}
                                onChange={(e) => setFormData({ ...formData, custom_context: e.target.value })}
                                className="font-mono text-sm"
                                rows={4}
                            />
                            <p className="text-xs text-muted-foreground">
                                Keys: research_strategy, force_research, role, template_id, synthesis_template
                            </p>
                        </div>
                    )}

                    <ScheduleBuilder
                        value={formData.cron_expression}
                        onChange={(cron) => setFormData({ ...formData, cron_expression: cron })}
                        timezone={formData.timezone}
                    />

                    <div className="space-y-2">
                        <Label htmlFor="timezone">Timezone</Label>
                        <Select
                            value={formData.timezone}
                            onValueChange={(value) => setFormData({ ...formData, timezone: value })}
                        >
                            <SelectTrigger id="timezone">
                                <SelectValue placeholder="Select timezone" />
                            </SelectTrigger>
                            <SelectContent>
                                {COMMON_TIMEZONES.map((tz) => (
                                    <SelectItem key={tz} value={tz}>
                                        {tz}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="query">Task Query</Label>
                        <textarea
                            id="query"
                            className="placeholder:text-muted-foreground border-input focus-visible:border-ring focus-visible:ring-ring/50 w-full rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:ring-[3px] disabled:opacity-50"
                            placeholder="What task should be executed?"
                            value={formData.task_query}
                            onChange={(e) => setFormData({ ...formData, task_query: e.target.value })}
                            required
                            rows={3}
                        />
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label htmlFor="budget">Max Budget per Run (USD)</Label>
                            <Input
                                id="budget"
                                type="number"
                                step="0.01"
                                min="0"
                                max="10"
                                placeholder="e.g. 1.00"
                                value={formData.max_budget_per_run_usd}
                                onChange={(e) => setFormData({ ...formData, max_budget_per_run_usd: e.target.value })}
                            />
                            <p className="text-xs text-muted-foreground">Max $10.00 per run</p>
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="timeout">Timeout (seconds)</Label>
                            <Input
                                id="timeout"
                                type="number"
                                min="60"
                                placeholder="e.g. 300"
                                value={formData.timeout_seconds}
                                onChange={(e) => setFormData({ ...formData, timeout_seconds: e.target.value })}
                            />
                            <p className="text-xs text-muted-foreground">Min 60 seconds</p>
                        </div>
                    </div>

                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                            Cancel
                        </Button>
                        <Button type="submit" disabled={isSubmitting}>
                            {isSubmitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                            {isEditing ? "Save Changes" : "Create Schedule"}
                        </Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    );
}

export default function SchedulesPage() {
    const [schedules, setSchedules] = useState<ScheduleInfo[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState("");
    const [totalCount, setTotalCount] = useState<number | null>(null);
    const [pendingAction, setPendingAction] = useState<string | null>(null);
    const [isFormOpen, setIsFormOpen] = useState(false);
    const [editingSchedule, setEditingSchedule] = useState<ScheduleInfo | null>(null);
    const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);

    const fetchSchedules = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await listSchedules(50, 1);
            setSchedules(data.schedules || []);
            setTotalCount(data.total_count);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load schedules");
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchSchedules();
    }, [fetchSchedules]);

    const handlePause = async (scheduleId: string) => {
        setPendingAction(scheduleId);
        try {
            await pauseSchedule(scheduleId);
            await fetchSchedules();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to pause schedule");
        } finally {
            setPendingAction(null);
        }
    };

    const handleResume = async (scheduleId: string) => {
        setPendingAction(scheduleId);
        try {
            await resumeSchedule(scheduleId);
            await fetchSchedules();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to resume schedule");
        } finally {
            setPendingAction(null);
        }
    };

    const handleEdit = (schedule: ScheduleInfo) => {
        setEditingSchedule(schedule);
        setIsFormOpen(true);
    };

    const handleDeleteConfirm = async () => {
        if (!deleteConfirmId) return;
        setPendingAction(deleteConfirmId);
        try {
            await deleteSchedule(deleteConfirmId);
            await fetchSchedules();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to delete schedule");
        } finally {
            setPendingAction(null);
            setDeleteConfirmId(null);
        }
    };

    const handleFormClose = (open: boolean) => {
        setIsFormOpen(open);
        if (!open) {
            setEditingSchedule(null);
        }
    };

    // Filter schedules based on search
    const filteredSchedules = schedules.filter((schedule) => {
        const query = searchQuery.toLowerCase();
        return (
            schedule.name.toLowerCase().includes(query) ||
            (schedule.description || "").toLowerCase().includes(query) ||
            schedule.task_query.toLowerCase().includes(query)
        );
    });

    const scheduleToDelete = deleteConfirmId
        ? schedules.find((s) => s.schedule_id === deleteConfirmId)
        : null;

    return (
        <div className="h-full overflow-y-auto p-4 sm:p-8 space-y-6 sm:space-y-8">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">Schedules</h1>
                    <p className="text-muted-foreground text-sm sm:text-base">
                        Manage your scheduled tasks and view execution history.
                    </p>
                </div>
                <div className="flex gap-2">
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={fetchSchedules}
                        disabled={isLoading}
                    >
                        <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
                        <span className="hidden sm:inline ml-2">Refresh</span>
                    </Button>
                    <Button size="sm" onClick={() => setIsFormOpen(true)}>
                        <Plus className="h-4 w-4" />
                        <span className="hidden sm:inline ml-2">Create</span>
                    </Button>
                </div>
            </div>

            <div className="flex items-center gap-4">
                <div className="relative flex-1 max-w-sm">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        type="search"
                        placeholder="Search schedules..."
                        className="pl-8"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
            </div>

            {error && (
                <div className="rounded-lg border border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-950/50 p-4">
                    <div className="flex items-start justify-between gap-4">
                        <p className="text-sm text-red-800 dark:text-red-200">{error}</p>
                        <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0 text-red-600 hover:text-red-800 hover:bg-red-100 dark:text-red-400 dark:hover:bg-red-900/50"
                            onClick={() => setError(null)}
                        >
                            <XCircle className="h-4 w-4" />
                        </Button>
                    </div>
                    <Button
                        variant="outline"
                        size="sm"
                        className="mt-2"
                        onClick={() => {
                            setError(null);
                            fetchSchedules();
                        }}
                    >
                        Retry
                    </Button>
                </div>
            )}

            {isLoading ? (
                <div className="flex items-center justify-center py-12">
                    <Loader2 className="h-8 w-8 animate-spin text-primary" />
                </div>
            ) : (
                <div className="rounded-md border">
                    <div className="w-full overflow-auto">
                        <table className="w-full caption-bottom text-sm">
                            <thead className="[&_tr]:border-b">
                                <tr className="border-b transition-colors hover:bg-muted/50">
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground">
                                        Name
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden md:table-cell">
                                        Type
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden lg:table-cell">
                                        Schedule
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground">
                                        Status
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden sm:table-cell">
                                        Next Run
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden xl:table-cell">
                                        Success
                                    </th>
                                    <th className="h-12 px-4 text-right align-middle font-medium text-muted-foreground">
                                        Actions
                                    </th>
                                </tr>
                            </thead>
                            <tbody className="[&_tr:last-child]:border-0">
                                {filteredSchedules.length === 0 ? (
                                    <tr>
                                        <td colSpan={7} className="p-8 text-center text-muted-foreground">
                                            {searchQuery ? (
                                                "No schedules match your search"
                                            ) : (
                                                <div className="space-y-3">
                                                    <p>No schedules yet.</p>
                                                    <Button size="sm" onClick={() => setIsFormOpen(true)}>
                                                        <Plus className="h-4 w-4 mr-2" />
                                                        Create your first schedule
                                                    </Button>
                                                </div>
                                            )}
                                        </td>
                                    </tr>
                                ) : (
                                    filteredSchedules.map((schedule) => (
                                        <ScheduleRow
                                            key={schedule.schedule_id}
                                            schedule={schedule}
                                            onPause={handlePause}
                                            onResume={handleResume}
                                            onEdit={handleEdit}
                                            onDelete={setDeleteConfirmId}
                                            pendingAction={pendingAction}
                                        />
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                    {totalCount !== null && schedules.length > 0 && (
                        <div className="px-4 py-2 border-t text-xs text-muted-foreground">
                            Showing {filteredSchedules.length} of {totalCount} schedules
                        </div>
                    )}
                </div>
            )}

            <ScheduleFormDialog
                open={isFormOpen}
                onOpenChange={handleFormClose}
                onSaved={fetchSchedules}
                editingSchedule={editingSchedule}
            />

            {/* Delete confirmation dialog */}
            <AlertDialog open={!!deleteConfirmId} onOpenChange={(open) => !open && setDeleteConfirmId(null)}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Delete Schedule</AlertDialogTitle>
                        <AlertDialogDescription>
                            Are you sure you want to delete &quot;{scheduleToDelete?.name}&quot;?
                            This action cannot be undone. The schedule will be removed and no future
                            runs will be executed.
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={handleDeleteConfirm}
                            className="bg-red-600 hover:bg-red-700 focus:ring-red-600"
                        >
                            {pendingAction === deleteConfirmId ? (
                                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                            ) : null}
                            Delete
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    );
}
