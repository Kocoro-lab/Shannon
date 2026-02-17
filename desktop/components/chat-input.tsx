"use client";

import { useState, useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Send, Loader2, Sparkles, Pause, Play, Square, CheckCircle2, Workflow, Wand2, X } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { useRouter } from "next/navigation";
import { submitTask, submitReviewFeedback, approveReviewPlan } from "@/lib/shannon/api";
import { cn } from "@/lib/utils";

export type AgentSelection = "normal" | "deep_research";
export type ResearchStrategy = "quick" | "standard" | "deep" | "academic";
export type AutoApproveMode = "on" | "off";

interface ChatInputProps {
    sessionId?: string;
    disabled?: boolean;
    isTaskComplete?: boolean;
    selectedAgent?: AgentSelection;
    initialResearchStrategy?: ResearchStrategy;
    initialAutoApprove?: AutoApproveMode;
    onTaskCreated?: (taskId: string, query: string, workflowId?: string) => void;
    /** Use centered textarea layout for empty sessions */
    variant?: "default" | "centered";
    /** Task control props */
    isTaskRunning?: boolean;
    isPaused?: boolean;
    isPauseLoading?: boolean;
    isResumeLoading?: boolean;
    isCancelling?: boolean;
    onPause?: () => void;
    onResume?: () => void;
    onCancel?: () => void;
    /** Review Plan (HITL) props */
    reviewStatus?: "none" | "reviewing" | "approved";
    reviewWorkflowId?: string | null;
    reviewVersion?: number;
    reviewIntent?: "feedback" | "ready" | "execute" | null;
    onAutoApproveChange?: (mode: AutoApproveMode) => void;
    onReviewSending?: (userMessage: string) => void;
    onReviewFeedback?: (version: number, intent: "feedback" | "ready" | "execute", planMessage: string, round: number, userMessage: string) => void;
    onReviewError?: () => void;
    onApprove?: () => void;
    /** Swarm mode toggle */
    swarmMode?: boolean;
    onSwarmModeChange?: (enabled: boolean) => void;
    /** Skill selection */
    selectedSkill?: string | null;
    onSkillDismiss?: () => void;
}

export function ChatInput({
    sessionId,
    disabled,
    isTaskComplete,
    selectedAgent = "normal",
    initialResearchStrategy = "quick",
    initialAutoApprove = "on",
    onTaskCreated,
    variant = "default",
    isTaskRunning = false,
    isPaused = false,
    isPauseLoading = false,
    isResumeLoading = false,
    isCancelling = false,
    onPause,
    onResume,
    onCancel,
    reviewStatus = "none",
    reviewWorkflowId,
    reviewVersion = 0,
    reviewIntent,
    onAutoApproveChange,
    onReviewSending,
    onReviewFeedback,
    onReviewError,
    onApprove,
    swarmMode = false,
    onSwarmModeChange,
    selectedSkill,
    onSkillDismiss,
}: ChatInputProps) {
    const [query, setQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [researchStrategy, setResearchStrategy] = useState<ResearchStrategy>(initialResearchStrategy);
    const [autoApprove, setAutoApproveLocal] = useState<AutoApproveMode>(initialAutoApprove);
    const router = useRouter();

    const isReviewing = reviewStatus === "reviewing";
    
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
            // Review mode: send feedback instead of new task
            if (isReviewing && reviewWorkflowId) {
                const feedbackText = query.trim();
                setQuery("");
                // Immediately show user message + loading animation
                onReviewSending?.(feedbackText);
                try {
                    const result = await submitReviewFeedback(reviewWorkflowId, feedbackText, reviewVersion);
                    if (onReviewFeedback && result.plan) {
                        onReviewFeedback(result.plan.version, result.plan.intent, result.plan.message, result.plan.round, feedbackText);
                    }
                } catch (feedbackErr) {
                    // Clean up temporary messages on error
                    onReviewError?.();
                    const msg = feedbackErr instanceof Error ? feedbackErr.message : "Failed to submit feedback";
                    setError(msg);
                }
                return;
            }

            const context: Record<string, unknown> = {};
            let research_strategy: "deep" | "academic" | "quick" | "standard" | undefined;

            // Swarm mode: multi-agent persistent loop
            if (swarmMode) {
                context.force_swarm = true;
            }

            if (selectedAgent === "deep_research") {
                context.force_research = true;
                research_strategy = researchStrategy;
                if (autoApprove === "off") {
                    context.require_review = true;
                }
            }

            const response = await submitTask({
                query: query.trim(),
                session_id: sessionId,
                context: Object.keys(context).length ? context : undefined,
                research_strategy,
                skill: selectedSkill || undefined,
            });

            setQuery("");
            if (selectedSkill) {
                onSkillDismiss?.();
            }

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

    const handleApprove = async () => {
        if (!reviewWorkflowId) return;
        setIsSubmitting(true);
        setError(null);
        try {
            await approveReviewPlan(reviewWorkflowId, reviewVersion);
            onApprove?.();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to approve plan");
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
                            <div className="flex items-center justify-center gap-4 flex-wrap">
                                <div className="flex items-center gap-2">
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
                                <div className="flex items-center gap-2">
                                    <span className="text-sm text-muted-foreground">Auto-Approve:</span>
                                    <Select
                                        value={autoApprove}
                                        onValueChange={(val) => {
                                            setAutoApproveLocal(val as AutoApproveMode);
                                            onAutoApproveChange?.(val as AutoApproveMode);
                                        }}
                                    >
                                        <SelectTrigger className="h-9 w-32">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="on">On</SelectItem>
                                            <SelectItem value="off">Off</SelectItem>
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>
                        )}
                        
                        {selectedSkill && (
                            <div className="flex items-center gap-2 mb-2">
                                <div className="inline-flex items-center gap-1.5 rounded-full bg-primary/10 border border-primary/20 px-3 py-1 text-sm">
                                    <Wand2 className="h-3.5 w-3.5 text-primary" />
                                    <span className="text-primary font-medium">Skill: {selectedSkill}</span>
                                    <button
                                        type="button"
                                        onClick={onSkillDismiss}
                                        aria-label="Dismiss skill"
                                        className="ml-1 rounded-full p-0.5 hover:bg-primary/20 transition-colors"
                                    >
                                        <X className="h-3 w-3 text-primary" />
                                    </button>
                                </div>
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
                            <div className="absolute right-3 bottom-3 flex items-center gap-2">
                                {selectedAgent === "normal" && (
                                    <Tooltip>
                                        <TooltipTrigger asChild>
                                            <button
                                                type="button"
                                                onClick={() => onSwarmModeChange?.(!swarmMode)}
                                                className={cn(
                                                    "flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors",
                                                    swarmMode
                                                        ? "bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30"
                                                        : "text-muted-foreground hover:text-foreground hover:bg-muted"
                                                )}
                                            >
                                                <Workflow className="h-3.5 w-3.5" />
                                                Swarm Mode
                                            </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="top">
                                            Performs longer tasks with a team of agents
                                        </TooltipContent>
                                    </Tooltip>
                                )}
                                <Button type="submit" size="icon" disabled={!query.trim() || isInputDisabled || isSubmitting}>
                                    {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                                </Button>
                            </div>
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
            {selectedAgent === "deep_research" && !isReviewing && (
                <div className="flex items-center gap-4 flex-wrap">
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
                    <div className="flex items-center gap-2">
                        <span className="text-xs text-muted-foreground">Auto-Approve:</span>
                        <Select
                            value={autoApprove}
                            onValueChange={(val) => {
                                setAutoApproveLocal(val as AutoApproveMode);
                                onAutoApproveChange?.(val as AutoApproveMode);
                            }}
                        >
                            <SelectTrigger className="h-8 w-28 text-xs">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="on">On</SelectItem>
                                <SelectItem value="off">Off</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                </div>
            )}

            {/* Review mode: Approve & Run bar — shown when LLM has produced an actionable plan (intent=approve) */}
            {isReviewing && reviewIntent === "ready" && (
                <div className="flex items-center justify-between px-3 py-2 rounded-lg border bg-violet-50 dark:bg-violet-950 border-violet-300 dark:border-violet-700">
                    <span className="text-sm text-violet-700 dark:text-violet-300">
                        Ready? Approve to start research execution.
                    </span>
                    <Button
                        type="button"
                        size="sm"
                        onClick={handleApprove}
                        disabled={isSubmitting}
                        className="gap-1.5 bg-violet-600 hover:bg-violet-700 text-white"
                    >
                        {isSubmitting ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                            <CheckCircle2 className="h-3.5 w-3.5" />
                        )}
                        Approve & Run
                    </Button>
                </div>
            )}

            {selectedSkill && (
                <div className="flex items-center gap-2">
                    <div className="inline-flex items-center gap-1.5 rounded-full bg-primary/10 border border-primary/20 px-3 py-1 text-sm">
                        <Wand2 className="h-3.5 w-3.5 text-primary" />
                        <span className="text-primary font-medium">Skill: {selectedSkill}</span>
                        <button
                            type="button"
                            onClick={onSkillDismiss}
                            aria-label="Dismiss skill"
                            className="ml-1 rounded-full p-0.5 hover:bg-primary/20 transition-colors"
                        >
                            <X className="h-3 w-3 text-primary" />
                        </button>
                    </div>
                </div>
            )}

            <div className="flex gap-2 items-end">
                <div className="flex-1 flex flex-col gap-1.5">
                    <Textarea
                        placeholder={
                            isReviewing
                                ? "Type feedback or approve the plan..."
                                : isInputDisabled
                                    ? "Waiting for task to complete..."
                                    : "Ask a question..."
                        }
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        disabled={isInputDisabled || isSubmitting}
                        autoFocus
                        rows={2}
                        onCompositionStart={handleCompositionStart}
                        onCompositionEnd={handleCompositionEnd}
                        onKeyDown={handleKeyDown}
                        className="min-h-[44px]"
                    />
                    {selectedAgent === "normal" && (
                        <div className="flex items-center">
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <button
                                        type="button"
                                        onClick={() => onSwarmModeChange?.(!swarmMode)}
                                        className={cn(
                                            "flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium transition-colors",
                                            swarmMode
                                                ? "bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30"
                                                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                                        )}
                                    >
                                        <Workflow className="h-3 w-3" />
                                        Swarm Mode{swarmMode ? " ON" : ""}
                                    </button>
                                </TooltipTrigger>
                                <TooltipContent side="top">
                                    Performs longer tasks with a team of agents
                                </TooltipContent>
                            </Tooltip>
                        </div>
                    )}
                </div>
                {/* Show Pause/Stop buttons when task is running, otherwise show Send button */}
                {isTaskRunning ? (
                    <div className="flex gap-1.5">
                        {/* Pause/Resume toggle */}
                        {isPaused ? (
                            <Button
                                type="button"
                                size="icon"
                                variant="outline"
                                onClick={onResume}
                                disabled={isResumeLoading}
                                title="Resume workflow"
                            >
                                {isResumeLoading ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <Play className="h-4 w-4" />
                                )}
                            </Button>
                        ) : (
                            <Button
                                type="button"
                                size="icon"
                                variant="outline"
                                onClick={onPause}
                                disabled={isPauseLoading}
                                title="Pause at next checkpoint"
                            >
                                {isPauseLoading ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <Pause className="h-4 w-4" />
                                )}
                            </Button>
                        )}
                        {/* Stop button - always visible when running */}
                        <Button
                            type="button"
                            size="icon"
                            variant="destructive"
                            onClick={onCancel}
                            disabled={isCancelling || isPauseLoading || isResumeLoading}
                            title="Stop"
                        >
                            {isCancelling ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                                <Square className="h-4 w-4" />
                            )}
                        </Button>
                    </div>
                ) : (
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
                )}
            </div>
            {error && (
                <p className="text-xs text-red-500">{error}</p>
            )}
        </form>
    );
}
