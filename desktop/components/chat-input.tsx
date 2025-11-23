"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Send, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { submitTask } from "@/lib/shannon/api";

export type AgentSelection = "normal" | "deep_research";
export type ResearchStrategy = "quick" | "standard" | "deep" | "academic";

interface ChatInputProps {
    sessionId?: string;
    disabled?: boolean;
    isTaskComplete?: boolean;
    selectedAgent?: AgentSelection;
    onTaskCreated?: (taskId: string, query: string, workflowId?: string) => void;
}

export function ChatInput({ sessionId, disabled, isTaskComplete, selectedAgent = "normal", onTaskCreated }: ChatInputProps) {
    const [query, setQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [isComposing, setIsComposing] = useState(false);
    const [researchStrategy, setResearchStrategy] = useState<ResearchStrategy>("quick");
    const router = useRouter();

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

    const isInputDisabled = disabled && !isTaskComplete;

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
                    onCompositionStart={() => setIsComposing(true)}
                    onCompositionEnd={() => setIsComposing(false)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey && !isComposing) {
                            e.preventDefault();
                            handleSubmit(e);
                        }
                    }}
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
            {sessionId && (
                <p className="text-xs text-muted-foreground">
                    Session: <span className="font-mono">{sessionId}</span>
                    {isTaskComplete && <span className="ml-2 text-green-600">✓ Ready for follow-up</span>}
                </p>
            )}
        </form>
    );
}
