"use client";

import React from "react";
import { useSelector } from "react-redux";
import { RootState } from "@/lib/store";
import { Clock, CheckCircle2, XCircle, Loader2, AlertCircle } from "lucide-react";

interface SubtaskStatus {
    id: string;
    description: string;
    status: "pending" | "running" | "completed" | "failed";
    agent_id?: string;
}

interface TaskProgressPanelProps {
    className?: string;
}

/**
 * Task Progress Panel - Manus-style progress display
 * Shows subtask completion, current activity, and timing information
 */
export function TaskProgressPanel({ className = "" }: TaskProgressPanelProps) {
    const { events, connectionState } = useSelector((state: RootState) => state.run);

    // Extract progress information from events
    const progressInfo = React.useMemo(() => {
        let totalSubtasks = 0;
        let completedSubtasks = 0;
        let currentActivity = "";
        let startTime: Date | null = null;
        let isComplete = false;
        let isFailed = false;
        const subtasks: SubtaskStatus[] = [];
        const agentStatuses: Map<string, "pending" | "running" | "completed" | "failed"> = new Map();

        for (const event of events) {
            switch (event.type) {
                case "WORKFLOW_STARTED":
                    if (event.timestamp) {
                        startTime = new Date(event.timestamp);
                    }
                    break;
                case "PROGRESS":
                    if (event.message) {
                        currentActivity = event.message;
                    }
                    break;
                case "AGENT_STARTED":
                    if (event.agent_id) {
                        agentStatuses.set(event.agent_id, "running");
                        currentActivity = event.message || `Agent ${event.agent_id} working...`;
                    }
                    break;
                case "AGENT_COMPLETED":
                    if (event.agent_id) {
                        agentStatuses.set(event.agent_id, "completed");
                    }
                    break;
                case "AGENT_THINKING":
                    currentActivity = event.message || "Thinking...";
                    break;
                case "TOOL_INVOKED":
                    currentActivity = `Using tool: ${event.message || "..."}`;
                    break;
                case "WORKFLOW_COMPLETED":
                case "done":
                    isComplete = true;
                    break;
                case "WORKFLOW_FAILED":
                case "error":
                    isFailed = true;
                    break;
            }
        }

        // Build subtask list from agent statuses
        agentStatuses.forEach((status, agentId) => {
            subtasks.push({
                id: agentId,
                description: `Agent: ${agentId}`,
                status,
                agent_id: agentId,
            });
        });

        // Calculate elapsed time
        let elapsedMs = 0;
        if (startTime) {
            elapsedMs = Date.now() - startTime.getTime();
        }

        // Estimate remaining time (rough estimate based on progress)
        let estimatedRemainingMs: number | undefined;
        if (totalSubtasks > 0 && completedSubtasks > 0 && !isComplete) {
            const avgTimePerSubtask = elapsedMs / completedSubtasks;
            const remainingSubtasks = totalSubtasks - completedSubtasks;
            estimatedRemainingMs = avgTimePerSubtask * remainingSubtasks;
        }

        return {
            totalSubtasks,
            completedSubtasks,
            currentActivity,
            elapsedMs,
            estimatedRemainingMs,
            subtasks,
            isComplete,
            isFailed,
            progressPercent: totalSubtasks > 0 
                ? Math.round((completedSubtasks / totalSubtasks) * 100)
                : isComplete ? 100 : 0,
        };
    }, [events]);

    const formatTime = (ms: number) => {
        const seconds = Math.floor(ms / 1000);
        const minutes = Math.floor(seconds / 60);
        const remainingSeconds = seconds % 60;
        if (minutes > 0) {
            return `${minutes}m ${remainingSeconds}s`;
        }
        return `${seconds}s`;
    };

    // Don't show if no progress events
    if (events.length === 0) {
        return null;
    }

    return (
        <div className={`bg-background border rounded-lg p-4 ${className}`}>
            {/* Header */}
            <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-medium text-foreground">Task Progress</h3>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    {connectionState === "connected" && (
                        <span className="flex items-center gap-1">
                            <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
                            Live
                        </span>
                    )}
                    {connectionState === "polling" && (
                        <span className="flex items-center gap-1">
                            <span className="w-2 h-2 bg-yellow-500 rounded-full" />
                            Polling
                        </span>
                    )}
                    {connectionState === "reconnecting" && (
                        <span className="flex items-center gap-1">
                            <Loader2 className="w-3 h-3 animate-spin" />
                            Reconnecting
                        </span>
                    )}
                    {connectionState === "error" && (
                        <span className="flex items-center gap-1 text-red-500">
                            <AlertCircle className="w-3 h-3" />
                            Error
                        </span>
                    )}
                </div>
            </div>

            {/* Progress bar */}
            <div className="mb-3">
                <div className="flex justify-between text-xs text-muted-foreground mb-1">
                    <span>
                        {progressInfo.completedSubtasks}/{progressInfo.totalSubtasks || "?"} completed
                    </span>
                    <span>{progressInfo.progressPercent}%</span>
                </div>
                <div className="h-2 bg-muted rounded-full overflow-hidden">
                    <div 
                        className={`h-full transition-all duration-300 ${
                            progressInfo.isFailed 
                                ? "bg-red-500" 
                                : progressInfo.isComplete 
                                    ? "bg-green-500" 
                                    : "bg-primary"
                        }`}
                        style={{ width: `${progressInfo.progressPercent}%` }}
                    />
                </div>
            </div>

            {/* Current activity */}
            {progressInfo.currentActivity && !progressInfo.isComplete && (
                <div className="flex items-center gap-2 text-sm text-muted-foreground mb-3">
                    <Loader2 className="w-4 h-4 animate-spin" />
                    <span className="truncate">{progressInfo.currentActivity}</span>
                </div>
            )}

            {/* Timing info */}
            <div className="flex items-center gap-4 text-xs text-muted-foreground mb-3">
                <div className="flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    <span>Elapsed: {formatTime(progressInfo.elapsedMs)}</span>
                </div>
                {progressInfo.estimatedRemainingMs && !progressInfo.isComplete && (
                    <div className="flex items-center gap-1">
                        <span>Est. remaining: {formatTime(progressInfo.estimatedRemainingMs)}</span>
                    </div>
                )}
            </div>

            {/* Subtask list */}
            {progressInfo.subtasks.length > 0 && (
                <div className="border-t pt-3 mt-3">
                    <div className="text-xs font-medium text-muted-foreground mb-2">Agents</div>
                    <div className="space-y-1 max-h-40 overflow-y-auto">
                        {progressInfo.subtasks.map((subtask) => (
                            <div 
                                key={subtask.id}
                                className="flex items-center gap-2 text-xs"
                            >
                                {subtask.status === "completed" && (
                                    <CheckCircle2 className="w-3 h-3 text-green-500" />
                                )}
                                {subtask.status === "running" && (
                                    <Loader2 className="w-3 h-3 animate-spin text-primary" />
                                )}
                                {subtask.status === "failed" && (
                                    <XCircle className="w-3 h-3 text-red-500" />
                                )}
                                {subtask.status === "pending" && (
                                    <div className="w-3 h-3 rounded-full border border-muted-foreground" />
                                )}
                                <span className={`truncate ${
                                    subtask.status === "completed" 
                                        ? "text-muted-foreground" 
                                        : "text-foreground"
                                }`}>
                                    {subtask.description}
                                </span>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Completion status */}
            {progressInfo.isComplete && (
                <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mt-2">
                    <CheckCircle2 className="w-4 h-4" />
                    <span>Task completed successfully</span>
                </div>
            )}
            {progressInfo.isFailed && (
                <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 mt-2">
                    <XCircle className="w-4 h-4" />
                    <span>Task failed</span>
                </div>
            )}
        </div>
    );
}

export default TaskProgressPanel;
