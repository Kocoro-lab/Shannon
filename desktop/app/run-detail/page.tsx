"use client";

import { useSearchParams, useRouter } from "next/navigation";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RunTimeline } from "@/components/run-timeline";
import { RunConversation } from "@/components/run-conversation";
import { ChatInput } from "@/components/chat-input";
import { ArrowLeft, Download, Share, Loader2 } from "lucide-react";
import Link from "next/link";
import { Suspense, useEffect, useState } from "react";
import { useRunStream } from "@/lib/shannon/stream";
import { useSelector, useDispatch } from "react-redux";
import { RootState } from "@/lib/store";
import { getTask } from "@/lib/shannon/api";
import { resetRun } from "@/lib/features/runSlice";

function RunDetailContent() {
    const searchParams = useSearchParams();
    const id = searchParams.get("id");
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [taskData, setTaskData] = useState<any>(null);
    const dispatch = useDispatch();

    // Connect to SSE stream
    useRunStream(id);

    // Get data from Redux
    const runEvents = useSelector((state: RootState) => state.run.events);
    const runMessages = useSelector((state: RootState) => state.run.messages);
    const runStatus = useSelector((state: RootState) => state.run.status);

    // Reset and fetch initial task data
    useEffect(() => {
        if (!id) return;

        // Reset Redux state for new task
        dispatch(resetRun());

        const fetchTaskData = async () => {
            setIsLoading(true);
            setError(null);
            try {
                const data = await getTask(id);
                setTaskData(data);
                console.log("Fetched task data:", data);
            } catch (err) {
                setError(err instanceof Error ? err.message : "Failed to load task");
                console.error("Failed to fetch task:", err);
            } finally {
                setIsLoading(false);
            }
        };

        fetchTaskData();
    }, [id, dispatch]);

    // Transform raw events into timeline format - filter out streaming deltas
    const timelineEvents = runEvents
        .filter((event: any) => {
            // Filter out streaming delta events - only show complete important events
            return event.type !== "thread.message.delta";
        })
        .map((event: any, index) => ({
            id: event.stream_id || `event-${index}`,
            type: event.type?.includes("AGENT") ? "agent" : 
                  event.type?.includes("LLM") ? "llm" :
                  event.type?.includes("TOOL") ? "tool" : "system",
            status: event.type === "error" ? "failed" :
                    event.type?.includes("STARTED") || event.type?.includes("THINKING") ? "running" :
                    event.type === "thread.message.completed" ? "completed" :
                    "completed",
            title: event.message || event.type || "Event",
            timestamp: event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : "",
            details: event.payload ? JSON.stringify(event.payload, null, 2) : undefined
        }));

    // Always add user's initial query as first message if we have taskData
    const allMessages = taskData?.query 
        ? [
            {
                id: "user-query",
                role: "user" as const,
                content: taskData.query,
                timestamp: taskData.created_at ? new Date(taskData.created_at).toLocaleTimeString() : "",
            },
            ...runMessages
          ]
        : runMessages;

    // Add final result from taskData if task is complete and no assistant message exists
    const hasAssistantMessage = allMessages.some((m: any) => m.role === "assistant");
    const messages = !hasAssistantMessage && taskData?.result && taskData?.status === "TASK_STATUS_COMPLETED"
        ? [
            ...allMessages,
            {
                id: "final-result",
                role: "assistant" as const,
                content: taskData.result,
                timestamp: taskData.completed_at ? new Date(taskData.completed_at).toLocaleTimeString() : "",
                metadata: {
                    usage: taskData.usage,
                    model: taskData.model_used,
                    provider: taskData.provider,
                }
            }
          ]
        : allMessages;

    // Debug logging
    useEffect(() => {
        console.log("[RunDetail] Messages updated:", {
            taskDataQuery: taskData?.query,
            runMessagesCount: runMessages.length,
            totalMessages: messages.length,
            messages
        });
    }, [taskData, runMessages, messages]);

    if (!id) {
        return (
            <div className="flex items-center justify-center h-screen">
                <div className="text-center">
                    <h2 className="text-2xl font-bold mb-2">Run ID not found</h2>
                    <p className="text-muted-foreground mb-4">No task ID provided in URL</p>
                    <Button asChild>
                        <Link href="/">Go Home</Link>
                    </Button>
                </div>
            </div>
        );
    }

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-screen">
                <div className="text-center space-y-4">
                    <Loader2 className="h-8 w-8 animate-spin mx-auto text-primary" />
                    <div>
                        <h2 className="text-xl font-semibold">Loading task...</h2>
                        <p className="text-sm text-muted-foreground">Connecting to Shannon</p>
                    </div>
                </div>
            </div>
        );
    }

    if (error) {
        return (
            <div className="flex items-center justify-center h-screen">
                <div className="text-center space-y-4">
                    <div className="text-red-500 text-5xl mb-4">⚠️</div>
                    <h2 className="text-2xl font-bold">Failed to load task</h2>
                    <p className="text-muted-foreground">{error}</p>
                    <div className="flex gap-2 justify-center">
                        <Button variant="outline" onClick={() => window.location.reload()}>
                            Retry
                        </Button>
                        <Button asChild>
                            <Link href="/">Go Home</Link>
                        </Button>
                    </div>
                </div>
            </div>
        );
    }

    return (
        <div className="flex h-full flex-col">
            {/* Header */}
            <header className="flex h-14 items-center justify-between border-b px-6 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                <div className="flex items-center gap-4">
                    <Button variant="ghost" size="icon" asChild>
                        <Link href="/runs">
                            <ArrowLeft className="h-4 w-4" />
                        </Link>
                    </Button>
                    <div className="flex items-center gap-2">
                        <h1 className="text-lg font-semibold">Run {id}</h1>
                        <Badge
                            variant="outline"
                            className={
                                runStatus === "completed"
                                    ? "bg-green-50 text-green-700 border-green-200"
                                    : runStatus === "failed"
                                        ? "bg-red-50 text-red-700 border-red-200"
                                        : "bg-blue-50 text-blue-700 border-blue-200"
                            }
                        >
                            {runStatus}
                        </Badge>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm">
                        <Share className="mr-2 h-4 w-4" />
                        Share
                    </Button>
                    <Button variant="outline" size="sm">
                        <Download className="mr-2 h-4 w-4" />
                        Export
                    </Button>
                </div>
            </header>

            {/* Main Content - Split View */}
            <div className="flex flex-1 overflow-hidden">
                {/* Left Column: Timeline */}
                <div className="w-1/3 border-r bg-muted/10">
                    <div className="p-4 font-medium text-sm text-muted-foreground uppercase tracking-wider">
                        Execution Timeline
                    </div>
                    <ScrollArea className="h-[calc(100vh-8rem)]">
                        {timelineEvents.length > 0 ? (
                            <RunTimeline events={timelineEvents as any} />
                        ) : (
                            <div className="p-4 text-sm text-muted-foreground text-center">
                                Waiting for events...
                            </div>
                        )}
                    </ScrollArea>
                </div>

                {/* Right Column: Tabs */}
                <div className="flex-1 bg-background">
                    <Tabs defaultValue="conversation" className="h-full flex flex-col">
                        <div className="border-b px-4">
                            <TabsList className="h-12 w-full justify-start bg-transparent p-0">
                                <TabsTrigger value="conversation" className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none h-full px-4">
                                    Conversation
                                </TabsTrigger>
                                <TabsTrigger value="raw" className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none h-full px-4">
                                    Raw Events
                                </TabsTrigger>
                                <TabsTrigger value="summary" className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none h-full px-4">
                                    Summary
                                </TabsTrigger>
                            </TabsList>
                        </div>

                        <TabsContent value="conversation" className="flex-1 p-0 m-0 data-[state=active]:flex flex-col overflow-hidden">
                            <ScrollArea className="flex-1">
                                {messages.length > 0 ? (
                                    <RunConversation messages={messages as any} />
                                ) : (
                                    <div className="flex items-center justify-center h-full">
                                        <div className="text-center text-muted-foreground">
                                            <p className="text-lg mb-2">Waiting for conversation...</p>
                                            <p className="text-sm">Messages will appear here as the agent responds</p>
                                        </div>
                                    </div>
                                )}
                            </ScrollArea>
                            
                            {/* Chat Input Box */}
                            <div className="border-t bg-background p-4">
                                <ChatInput 
                                    sessionId={taskData?.session_id} 
                                    disabled={!taskData || (runStatus === "running" && taskData?.status !== "TASK_STATUS_COMPLETED")}
                                    isTaskComplete={taskData?.status === "TASK_STATUS_COMPLETED"}
                                />
                            </div>
                        </TabsContent>

                        <TabsContent value="raw" className="flex-1 p-4 m-0 overflow-auto">
                            <pre className="text-xs font-mono bg-muted p-4 rounded-lg overflow-auto max-h-full whitespace-pre-wrap break-words">
                                {JSON.stringify({ taskData, runEvents, messages, runStatus }, null, 2)}
                            </pre>
                        </TabsContent>

                        <TabsContent value="summary" className="flex-1 p-8 m-0">
                            <div className="max-w-2xl mx-auto space-y-4">
                                <h2 className="text-2xl font-bold">Task Summary</h2>
                                {taskData?.result && (
                                    <div className="bg-muted p-4 rounded-lg">
                                        <div className="text-sm font-medium mb-2">Result:</div>
                                        <p className="text-sm">{taskData.result}</p>
                                    </div>
                                )}
                                <div className="grid grid-cols-2 gap-4 mt-8">
                                    {taskData?.usage?.total_tokens && (
                                        <Card className="p-4">
                                            <div className="text-sm font-medium text-muted-foreground">Tokens Used</div>
                                            <div className="text-2xl font-bold">{taskData.usage.total_tokens}</div>
                                        </Card>
                                    )}
                                    {taskData?.model_used && (
                                        <Card className="p-4">
                                            <div className="text-sm font-medium text-muted-foreground">Model</div>
                                            <div className="text-lg font-semibold">{taskData.model_used}</div>
                                            {taskData?.provider && (
                                                <div className="text-xs text-muted-foreground">{taskData.provider}</div>
                                            )}
                                        </Card>
                                    )}
                                    {taskData?.usage?.estimated_cost && (
                                        <Card className="p-4">
                                            <div className="text-sm font-medium text-muted-foreground">Estimated Cost</div>
                                            <div className="text-2xl font-bold">${taskData.usage.estimated_cost.toFixed(4)}</div>
                                        </Card>
                                    )}
                                    <Card className="p-4">
                                        <div className="text-sm font-medium text-muted-foreground">Status</div>
                                        <div className="text-2xl font-bold capitalize">{runStatus || taskData?.status || "running"}</div>
                                    </Card>
                                </div>
                            </div>
                        </TabsContent>
                    </Tabs>
                </div>
            </div>
        </div>
    );
}

export default function RunDetailPage() {
    return (
        <Suspense fallback={<div>Loading...</div>}>
            <RunDetailContent />
        </Suspense>
    );
}
