"use client";

import { useEffect, useRef, useCallback } from "react";
import { sendNotification, setBadgeCount, isTauri } from "@/lib/tauri";
import { getTask } from "@/lib/shannon/api";

const STORAGE_KEY = "shannon_running_tasks";
const POLL_INTERVAL = 3000;
const STALE_TASK_THRESHOLD = 30 * 60 * 1000;

interface RunningTask {
    workflowId: string;
    title: string;
    startedAt: number;
}

function getRunningTasks(): RunningTask[] {
    if (typeof window === "undefined") return [];
    try {
        const stored = localStorage.getItem(STORAGE_KEY);
        return stored ? JSON.parse(stored) : [];
    } catch {
        return [];
    }
}

function setRunningTasks(tasks: RunningTask[]) {
    if (typeof window === "undefined") return;
    localStorage.setItem(STORAGE_KEY, JSON.stringify(tasks));
}

function cleanupStaleTasks(): void {
    const tasks = getRunningTasks();
    const now = Date.now();
    const freshTasks = tasks.filter(t => (now - t.startedAt) < STALE_TASK_THRESHOLD);
    if (freshTasks.length !== tasks.length) {
        setRunningTasks(freshTasks);
    }
}

export function addRunningTask(workflowId: string, title: string) {
    const tasks = getRunningTasks();
    if (!tasks.find(t => t.workflowId === workflowId)) {
        tasks.push({ workflowId, title, startedAt: Date.now() });
        setRunningTasks(tasks);
    }
    if (isTauri()) {
        setBadgeCount(tasks.length);
    }
}

export function removeRunningTask(workflowId: string) {
    const tasks = getRunningTasks().filter(t => t.workflowId !== workflowId);
    setRunningTasks(tasks);
    if (isTauri()) {
        setBadgeCount(tasks.length);
    }
}

/**
 * Background task tracker - polls for task completion even when user navigates away.
 * Shows notifications and updates badge count when tasks complete.
 */
export function BackgroundTaskTracker() {
    const pollIntervalRef = useRef<NodeJS.Timeout | null>(null);

    const checkTasks = useCallback(async () => {
        const tasks = getRunningTasks();
        if (tasks.length === 0) return;

        for (const task of tasks) {
            try {
                const result = await getTask(task.workflowId);
                const status = result.status?.toUpperCase();

                if (status === "COMPLETED" || status === "FAILED" ||
                    status === "TASK_STATUS_COMPLETED" || status === "TASK_STATUS_FAILED") {
                    removeRunningTask(task.workflowId);

                    if (isTauri()) {
                        const isCompleted = status === "COMPLETED" || status === "TASK_STATUS_COMPLETED";
                        const notifBody = isCompleted
                            ? `Task completed: ${task.title || "Task completed"}`
                            : `Task failed: ${task.title || "Task failed"}`;
                        await sendNotification("Shannon Task Complete", notifBody);
                    }
                }
            } catch (error) {
                if ((error as Error).message?.includes("404") || (error as Error).message?.includes("not found")) {
                    removeRunningTask(task.workflowId);
                }
            }
        }

        const remaining = getRunningTasks();
        if (isTauri()) {
            setBadgeCount(remaining.length);
        }
    }, []);

    useEffect(() => {
        if (!isTauri()) return;

        cleanupStaleTasks();
        checkTasks();
        pollIntervalRef.current = setInterval(checkTasks, POLL_INTERVAL);

        const tasks = getRunningTasks();
        setBadgeCount(tasks.length);

        return () => {
            if (pollIntervalRef.current) {
                clearInterval(pollIntervalRef.current);
            }
        };
    }, [checkTasks]);

    return null;
}
