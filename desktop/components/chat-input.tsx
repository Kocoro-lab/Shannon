"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Send, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { submitTask } from "@/lib/shannon/api";

interface ChatInputProps {
    sessionId?: string;
    disabled?: boolean;
    isTaskComplete?: boolean;
}

export function ChatInput({ sessionId, disabled, isTaskComplete }: ChatInputProps) {
    const [query, setQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const router = useRouter();

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        
        if (!query.trim() || !sessionId) {
            return;
        }

        setIsSubmitting(true);
        setError(null);

        try {
            const response = await submitTask({
                query: query.trim(),
                session_id: sessionId,
                model_tier: "medium",
            });

            setQuery("");
            // Navigate to the new task
            router.push(`/run-detail?id=${response.task_id}`);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to submit");
        } finally {
            setIsSubmitting(false);
        }
    };

    if (!sessionId) {
        return (
            <div className="text-center text-sm text-muted-foreground py-2">
                No session ID available for follow-up questions
            </div>
        );
    }

    const isInputDisabled = disabled && !isTaskComplete;

    return (
        <form onSubmit={handleSubmit} className="space-y-2">
            <div className="flex gap-2">
                <Input
                    placeholder={isInputDisabled ? "Waiting for task to complete..." : "Ask a follow-up question..."}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    disabled={isInputDisabled || isSubmitting}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey) {
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

