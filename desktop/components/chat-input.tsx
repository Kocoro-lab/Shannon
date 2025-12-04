"use client";

import { useState, useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Send, Loader2, Sparkles } from "lucide-react";
import { useRouter } from "next/navigation";
import { submitTask } from "@/lib/shannon/api";
import { cn } from "@/lib/utils";

export type AgentSelection = "normal" | "deep_research";
export type ResearchStrategy = "quick" | "standard" | "deep" | "academic";

interface ChatInputProps {
    sessionId?: string;
    disabled?: boolean;
    isTaskComplete?: boolean;
    selectedAgent?: AgentSelection;
    initialResearchStrategy?: ResearchStrategy;
    onTaskCreated?: (taskId: string, query: string, workflowId?: string) => void;
    /** Use centered textarea layout for empty sessions */
    variant?: "default" | "centered";
}

export function ChatInput({ sessionId, disabled, isTaskComplete, selectedAgent = "normal", initialResearchStrategy = "quick", onTaskCreated, variant = "default" }: ChatInputProps) {
    const [query, setQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [researchStrategy, setResearchStrategy] = useState<ResearchStrategy>(initialResearchStrategy);
    const router = useRouter();
    
    // Use ref for composition state to avoid race conditions with state updates
    // This is more reliable than state for IME handling
    const isComposingRef = useRef(false);

    // Update research strategy when prop changes (e.g., loading historical session)
    useEffect(() => {
        setResearchStrategy(initialResearchStrategy);
    }, [initialResearchStrategy]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();

        if (!query.trim()) {
            return;
        }

        setIsSubmitting(true);
        setError(null);

        try {
            const context: Record<string, unknown> = {};
            let research_strategy: "deep" | "academic" | "quick" | "standard" | undefined;

            if (selectedAgent === "deep_research") {
                context.force_research = true;
                research_strategy = researchStrategy;
            }

            const response = await submitTask({
                query: query.trim(),
                session_id: sessionId,
                context: Object.keys(context).length ? context : undefined,
                research_strategy,
            });

            setQuery("");

            if (onTaskCreated) {
                onTaskCreated(response.task_id, query.trim(), response.workflow_id);
            } else {
                // Fallback if no callback provided
                router.push(`/run-detail?id=${response.task_id}`);
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to submit");
        } finally {
            setIsSubmitting(false);
        }
    };

    const isInputDisabled = disabled;

    const handleKeyDown = (e: React.KeyboardEvent) => {
        const nativeEvent = e.nativeEvent as { isComposing?: boolean; keyCode?: number } | undefined;
        const isComposing =
            (e as unknown as { isComposing?: boolean }).isComposing ||
            isComposingRef.current ||
            nativeEvent?.isComposing ||
            nativeEvent?.keyCode === 229;

        // When using IME (Chinese, Japanese, etc.), do not send on Enter while composing/choosing characters
        if (isComposing) {
            return;
        }

        if (e.key === "Enter") {
            const target = e.currentTarget as HTMLElement | null;
            const isTextarea = target instanceof HTMLTextAreaElement;

            // For textarea, keep Shift+Enter as newline
            if (e.shiftKey && isTextarea) {
                return;
            }

            // For plain Enter (and Enter in single-line input), prevent default form submit
            e.preventDefault();

            if (!e.shiftKey) {
                handleSubmit(e);
            }
        }
    };

    const handleCompositionStart = () => {
        isComposingRef.current = true;
    };

    const handleCompositionEnd = () => {
        isComposingRef.current = false;
    };

    // Centered variant for empty sessions - modern ChatGPT-style layout
    if (variant === "centered") {
        return (
            <div className="flex flex-col items-center justify-center h-full p-8">
                <div className="w-full max-w-2xl space-y-6">
                    <div className="text-center space-y-2">
                        <div className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-primary/10 mb-4">
                            <Sparkles className="w-6 h-6 text-primary" />
                        </div>
                        <h2 className="text-2xl font-semibold tracking-tight">How can I help you today?</h2>
                        <p className="text-muted-foreground">
                            Ask me anything — I can research, analyze, and help you think through complex topics.
                        </p>
                    </div>

                    <form onSubmit={handleSubmit} className="space-y-4">
                        {selectedAgent === "deep_research" && (
                            <div className="flex items-center justify-center gap-2">
                                <span className="text-sm text-muted-foreground">Research Strategy:</span>
                                <Select
                                    value={researchStrategy}
                                    onValueChange={(val) => setResearchStrategy(val as ResearchStrategy)}
                                >
                                    <SelectTrigger className="h-9 w-36">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="quick">Quick</SelectItem>
                                        <SelectItem value="standard">Standard</SelectItem>
                                        <SelectItem value="deep">Deep</SelectItem>
                                        <SelectItem value="academic">Academic</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        )}
                        
                        <div className="relative">
                            <Textarea
                                placeholder="Ask a question..."
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                disabled={isInputDisabled || isSubmitting}
                                autoFocus
                                rows={4}
                                onCompositionStart={handleCompositionStart}
                                onCompositionEnd={handleCompositionEnd}
                                onKeyDown={handleKeyDown}
                                className="pr-14 min-h-[120px] text-base"
                            />
                            <Button
                                type="submit"
                                size="icon"
                                disabled={!query.trim() || isInputDisabled || isSubmitting}
                                className="absolute right-3 bottom-3"
                            >
                                {isSubmitting ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <Send className="h-4 w-4" />
                                )}
                            </Button>
                        </div>

                        {error && (
                            <p className="text-sm text-red-500 text-center">{error}</p>
                        )}
                    </form>

                    <div className="flex flex-wrap items-center justify-center gap-2 text-xs text-muted-foreground">
                        <span>Try:</span>
                        <button
                            type="button"
                            onClick={() => setQuery("What are the latest developments in AI?")}
                            className="px-2 py-1 rounded-md bg-muted hover:bg-muted/80 transition-colors"
                        >
                            Latest AI developments
                        </button>
                        <button
                            type="button"
                            onClick={() => setQuery("Explain quantum computing in simple terms")}
                            className="px-2 py-1 rounded-md bg-muted hover:bg-muted/80 transition-colors"
                        >
                            Explain quantum computing
                        </button>
                        <button
                            type="button"
                            onClick={() => setQuery("Compare React vs Vue for a new project")}
                            className="px-2 py-1 rounded-md bg-muted hover:bg-muted/80 transition-colors"
                        >
                            React vs Vue
                        </button>
                    </div>
                </div>
            </div>
        );
    }

    // Default compact variant for follow-up messages
    return (
        <form onSubmit={handleSubmit} className="space-y-2">
            {selectedAgent === "deep_research" && (
                <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">Research Strategy:</span>
                    <Select
                        value={researchStrategy}
                        onValueChange={(val) => setResearchStrategy(val as ResearchStrategy)}
                    >
                        <SelectTrigger className="h-8 w-32 text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="quick">Quick</SelectItem>
                            <SelectItem value="standard">Standard</SelectItem>
                            <SelectItem value="deep">Deep</SelectItem>
                            <SelectItem value="academic">Academic</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            )}
            <div className="flex gap-2">
                <Input
                    placeholder={isInputDisabled ? "Waiting for task to complete..." : "Ask a question..."}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    disabled={isInputDisabled || isSubmitting}
                    autoFocus
                    onCompositionStart={handleCompositionStart}
                    onCompositionEnd={handleCompositionEnd}
                    onKeyDown={handleKeyDown}
                />
                <Button
                    type="submit"
                    size="icon"
                    disabled={!query.trim() || isInputDisabled || isSubmitting}
                >
                    {isSubmitting ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                        <Send className="h-4 w-4" />
                    )}
                </Button>
            </div>
            {error && (
                <p className="text-xs text-red-500">{error}</p>
            )}
        </form>
    );
}
