"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Search, Loader2, RefreshCw, MessageSquare, Layers, DollarSign, Sparkles, Microscope, CheckCircle2, XCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { listSessions, Session } from "@/lib/shannon/api";

export default function RunsPage() {
    const [sessions, setSessions] = useState<Session[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isLoadingMore, setIsLoadingMore] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState("");
    const [totalCount, setTotalCount] = useState<number | null>(null);

    const PAGE_SIZE = 50;

    const fetchSessions = async (append: boolean = false) => {
        // For initial load/refresh, show full-page loader
        if (!append) {
            setIsLoading(true);
        } else {
            setIsLoadingMore(true);
        }
        setError(null);
        try {
            const offset = append ? sessions.length : 0;
            const data = await listSessions(PAGE_SIZE, offset);
            setTotalCount(data.total_count);

            if (append) {
                setSessions(prev => {
                    const existingIds = new Set(prev.map(s => s.session_id));
                    const newSessions = (data.sessions || []).filter(s => !existingIds.has(s.session_id));
                    return [...prev, ...newSessions];
                });
            } else {
                setSessions(data.sessions);
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load sessions");
        } finally {
            if (!append) {
                setIsLoading(false);
            } else {
                setIsLoadingMore(false);
            }
        }
    };

    useEffect(() => {
        fetchSessions();
    }, []);

    // Filter sessions based on search
    const filteredSessions = sessions.filter(session => {
        const query = searchQuery.toLowerCase();
        const title = (session.title || "").toLowerCase();
        return title.includes(query) || session.session_id.toLowerCase().includes(query);
    });

    return (
        <div className="h-full overflow-y-auto p-4 sm:p-8 space-y-6 sm:space-y-8">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">History</h1>
                    <p className="text-muted-foreground text-sm sm:text-base">
                        View and manage your conversation sessions.
                    </p>
                </div>
                <div className="flex gap-2">
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => fetchSessions(false)}
                        disabled={isLoading || isLoadingMore}
                    >
                        <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
                        <span className="hidden sm:inline ml-2">Refresh</span>
                    </Button>
                </div>
            </div>

            <div className="flex items-center gap-4">
                <div className="relative flex-1 max-w-sm">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        type="search"
                        placeholder="Search sessions..."
                        className="pl-8"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
            </div>

            {error && (
                <div className="rounded-lg border border-red-200 bg-red-50 p-4">
                    <p className="text-sm text-red-800">{error}</p>
                    <Button variant="outline" size="sm" className="mt-2" onClick={() => fetchSessions()}>
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
                                        Session
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0 hidden md:table-cell">
                                        Agent
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0 hidden sm:table-cell">
                                        Tasks
                                    </th>
                                    <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0 hidden sm:table-cell">
                                        Cost
                                    </th>
                                    <th className="h-12 px-4 text-right align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0">
                                        Actions
                                    </th>
                                </tr>
                            </thead>
                            <tbody className="[&_tr:last-child]:border-0">
                                {filteredSessions.length === 0 ? (
                                    <tr>
                                        <td colSpan={5} className="p-8 text-center text-muted-foreground">
                                            {searchQuery
                                                ? "No sessions match your search"
                                                : "No sessions found. Start a new session!"}
                                        </td>
                                    </tr>
                                ) : (
                                    filteredSessions.map((session) => (
                                        <tr
                                            key={session.session_id}
                                            className="border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted"
                                        >
                                            <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0">
                                                <div className="flex items-start gap-2">
                                                    {(() => {
                                                        const isRunning = session.latest_task_status === "RUNNING" || session.latest_task_status === "QUEUED";
                                                        const isActive = session.is_active || isRunning;
                                                        // Friendly title: prefer title, else truncated query, else "New task..."
                                                        const truncatedQuery = session.latest_task_query 
                                                            ? (session.latest_task_query.length > 50 
                                                                ? session.latest_task_query.slice(0, 50) + "..." 
                                                                : session.latest_task_query)
                                                            : null;
                                                        const displayTitle = session.title || truncatedQuery || "New task...";
                                                        const hasRealTitle = !!session.title;
                                                        // Only show query below if we have a real title (avoid redundancy)
                                                        const showQueryBelow = hasRealTitle && session.latest_task_query;
                                                        return (
                                                            <>
                                                            <TooltipProvider>
                                                                <Tooltip>
                                                                    <TooltipTrigger asChild>
                                                                        <div className={`mt-1.5 w-2 h-2 rounded-full shrink-0 ${
                                                                            isRunning ? "bg-blue-500 animate-pulse" : 
                                                                            isActive ? "bg-emerald-500" : "bg-gray-300"
                                                                        }`} />
                                                                    </TooltipTrigger>
                                                                    <TooltipContent>
                                                                        <p>{isRunning 
                                                                            ? "Running..." 
                                                                            : isActive 
                                                                                ? `Active ${session.last_activity_at ? new Date(session.last_activity_at).toLocaleString() : "recently"}` 
                                                                                : "Inactive"}</p>
                                                                    </TooltipContent>
                                                                </Tooltip>
                                                            </TooltipProvider>
                                                            <div className="flex flex-col min-w-0">
                                                                <Link
                                                                    href={`/run-detail?session_id=${session.session_id}`}
                                                                    className={`font-medium truncate max-w-[280px] hover:text-primary hover:underline transition-colors ${!hasRealTitle ? 'text-muted-foreground' : ''}`}
                                                                    title={session.title || session.latest_task_query || session.session_id}
                                                                >
                                                                    {displayTitle}
                                                                </Link>
                                                                <span className="text-xs text-muted-foreground">
                                                                    {new Date(session.created_at).toLocaleString()}
                                                                </span>
                                                                {showQueryBelow && (
                                                                    <span className="text-xs text-muted-foreground truncate max-w-[280px] mt-0.5">
                                                                        {session.latest_task_query}
                                                                    </span>
                                                                )}
                                                            </div>
                                                            </>
                                                        );
                                                    })()}
                                                </div>
                                            </td>
                                            <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 hidden md:table-cell">
                                                <TooltipProvider>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <div className="flex items-center justify-center w-8 h-8 rounded-full cursor-default hover:bg-muted transition-colors">
                                                                {session.is_research_session ? (
                                                                    <Microscope className="h-5 w-5 text-violet-500" />
                                                                ) : (
                                                                    <Sparkles className="h-5 w-5 text-amber-500" />
                                                                )}
                                                            </div>
                                                        </TooltipTrigger>
                                                        <TooltipContent>
                                                            <p>{session.is_research_session ? "Deep Research Agent" : "Everyday Agent"}</p>
                                                        </TooltipContent>
                                                    </Tooltip>
                                                </TooltipProvider>
                                            </td>
                                            <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 hidden sm:table-cell">
                                                <TooltipProvider>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <div className="flex items-center gap-2 cursor-default">
                                                                <Layers className="h-4 w-4 text-muted-foreground" />
                                                                <span>{session.task_count}</span>
                                                            </div>
                                                        </TooltipTrigger>
                                                        <TooltipContent>
                                                            <div className="flex flex-col gap-1">
                                                                <div className="flex items-center gap-1.5">
                                                                    <CheckCircle2 className="h-3 w-3 text-emerald-500" />
                                                                    <span>{session.successful_tasks || 0} successful</span>
                                                                </div>
                                                                {(session.failed_tasks || 0) > 0 && (
                                                                    <div className="flex items-center gap-1.5">
                                                                        <XCircle className="h-3 w-3 text-red-500" />
                                                                        <span>{session.failed_tasks} failed</span>
                                                                    </div>
                                                                )}
                                                            </div>
                                                        </TooltipContent>
                                                    </Tooltip>
                                                </TooltipProvider>
                                            </td>
                                            <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 hidden sm:table-cell">
                                                <TooltipProvider>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <div className="flex items-center gap-2 cursor-default">
                                                                <DollarSign className="h-4 w-4 text-muted-foreground" />
                                                                <span>${(session.total_cost_usd || 0).toFixed(3)}</span>
                                                            </div>
                                                        </TooltipTrigger>
                                                        <TooltipContent>
                                                            <div className="flex flex-col gap-1 text-xs">
                                                                <span>{session.tokens_used.toLocaleString()} tokens</span>
                                                                {session.average_cost_per_task !== undefined && session.average_cost_per_task > 0 && (
                                                                    <span>${session.average_cost_per_task.toFixed(3)}/task avg</span>
                                                                )}
                                                            </div>
                                                        </TooltipContent>
                                                    </Tooltip>
                                                </TooltipProvider>
                                            </td>
                                            <td className="p-4 align-middle [&:has([role=checkbox])]:pr-0 text-right">
                                                <Button variant="ghost" size="sm" asChild>
                                                    <Link href={`/run-detail?session_id=${session.session_id}`}>
                                                        <MessageSquare className="h-4 w-4 mr-2" />
                                                        View
                                                    </Link>
                                                </Button>
                                            </td>
                                        </tr>
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                    {totalCount !== null && (
                        <div className="flex items-center justify-between px-4 py-2 border-t text-xs text-muted-foreground">
                            <span>
                                Showing {sessions.length} of {totalCount} sessions
                            </span>
                            {sessions.length < totalCount && (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    onClick={() => fetchSessions(true)}
                                    disabled={isLoadingMore}
                                >
                                    {isLoadingMore && (
                                        <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                                    )}
                                    Load more
                                </Button>
                            )}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
