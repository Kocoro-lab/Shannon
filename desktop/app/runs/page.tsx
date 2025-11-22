"use client";

import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Search, Filter, Loader2, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { listTasks, TaskListResponse } from "@/lib/shannon/api";

export default function RunsPage() {
    const [tasks, setTasks] = useState<TaskListResponse["tasks"]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState("");
    const [statusFilter, setStatusFilter] = useState("all");

    const fetchTasks = async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await listTasks(50, 0);
            setTasks(data.tasks);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load tasks");
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => {
        fetchTasks();
    }, []);

    // Filter tasks based on search and status
    const filteredTasks = tasks.filter(task => {
        const matchesSearch = task.query.toLowerCase().includes(searchQuery.toLowerCase()) ||
                            task.task_id.toLowerCase().includes(searchQuery.toLowerCase());
        const matchesStatus = statusFilter === "all" || 
                            task.status.toLowerCase().includes(statusFilter.toLowerCase());
        return matchesSearch && matchesStatus;
    });

    const formatDuration = (startTime: string, endTime?: string) => {
        if (!endTime) return "Running...";
        const start = new Date(startTime).getTime();
        const end = new Date(endTime).getTime();
        const seconds = Math.floor((end - start) / 1000);
        if (seconds < 60) return `${seconds}s`;
        const minutes = Math.floor(seconds / 60);
        const remainingSeconds = seconds % 60;
        return `${minutes}m ${remainingSeconds}s`;
    };

    const formatStatus = (status: string) => {
        return status.replace("TASK_STATUS_", "").replace("_", " ");
    };
    return (
        <div className="p-8 space-y-8">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-3xl font-bold tracking-tight">Run History</h1>
                    <p className="text-muted-foreground">
                        View and analyze past workflow executions from Shannon.
                    </p>
                </div>
                <Button 
                    variant="outline" 
                    size="sm"
                    onClick={fetchTasks}
                    disabled={isLoading}
                >
                    <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
                    Refresh
                </Button>
            </div>

            <div className="flex items-center gap-4">
                <div className="relative flex-1 max-w-sm">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        type="search"
                        placeholder="Search by query or task ID..."
                        className="pl-8"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
                <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className="w-[180px]">
                        <SelectValue placeholder="Status" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All Statuses</SelectItem>
                        <SelectItem value="completed">Completed</SelectItem>
                        <SelectItem value="running">Running</SelectItem>
                        <SelectItem value="failed">Failed</SelectItem>
                    </SelectContent>
                </Select>
            </div>

            {error && (
                <div className="rounded-lg border border-red-200 bg-red-50 p-4">
                    <p className="text-sm text-red-800">{error}</p>
                    <Button variant="outline" size="sm" className="mt-2" onClick={fetchTasks}>
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
                            <tr className="border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted">
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Run ID
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Scenario
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Status
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Start Time
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Duration
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Metrics
                                </th>
                                <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                    Actions
                                </th>
                            </tr>
                        </thead>
                        <tbody className="[&_tr:last-child]:border-0">
                            {filteredTasks.length === 0 ? (
                                <tr>
                                    <td colSpan={7} className="p-8 text-center text-muted-foreground">
                                        {searchQuery || statusFilter !== "all" 
                                            ? "No tasks match your filters" 
                                            : "No tasks found. Submit a task to get started."}
                                    </td>
                                </tr>
                            ) : (
                                filteredTasks.map((task) => (
                                    <tr
                                        key={task.task_id}
                                        className="border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted"
                                    >
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 font-mono text-xs max-w-[200px] truncate" title={task.task_id}>
                                            {task.task_id}
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 font-medium max-w-[300px] truncate" title={task.query}>
                                            {task.query}
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0">
                                            <Badge
                                                variant={
                                                    task.status.includes("COMPLETED")
                                                        ? "default"
                                                        : task.status.includes("FAILED")
                                                            ? "destructive"
                                                            : "outline"
                                                }
                                                className={task.status.includes("COMPLETED") ? "bg-green-500 hover:bg-green-600" : ""}
                                            >
                                                {formatStatus(task.status)}
                                            </Badge>
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 text-sm">
                                            {new Date(task.created_at).toLocaleString()}
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 text-sm">
                                            {formatDuration(task.created_at, task.completed_at)}
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 text-muted-foreground text-sm">
                                            <div>{task.total_token_usage.total_tokens.toLocaleString()} tokens</div>
                                            <div className="text-xs">${task.total_token_usage.cost_usd.toFixed(4)}</div>
                                        </td>
                                        <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0">
                                            <Button variant="ghost" size="sm" asChild>
                                                <Link href={`/run-detail?id=${task.task_id}`}>View</Link>
                                            </Button>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            </div>
            )}
        </div>
    );
}
