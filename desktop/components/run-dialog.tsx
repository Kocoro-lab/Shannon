"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Play, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { submitTask } from "@/lib/shannon/api";

interface RunDialogProps {
    scenarioName: string;
    triggerButton?: React.ReactNode;
}

export function RunDialog({ scenarioName, triggerButton }: RunDialogProps) {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const router = useRouter();

    const handleRun = async () => {
        if (!query.trim()) {
            setError("Please enter a query");
            return;
        }

        setIsSubmitting(true);
        setError(null);

        try {
            const response = await submitTask({
                query: query.trim(),
                research_strategy: "standard",
            });

            setOpen(false);
            setQuery("");
            router.push(`/run-detail?id=${response.task_id}`);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to submit task");
        } finally {
            setIsSubmitting(false);
        }
    };

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
                {triggerButton || (
                    <Button size="sm">
                        <Play className="mr-2 h-4 w-4" />
                        Run
                    </Button>
                )}
            </DialogTrigger>
            <DialogContent className="sm:max-w-[425px]">
                <DialogHeader>
                    <DialogTitle>Run {scenarioName}</DialogTitle>
                    <DialogDescription>
                        Enter your query to submit to Shannon.
                    </DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-4">
                    <div className="grid gap-2">
                        <Label htmlFor="query">Query</Label>
                        <Input
                            id="query"
                            placeholder="What do you want to know?"
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && !e.shiftKey) {
                                    e.preventDefault();
                                    handleRun();
                                }
                            }}
                            disabled={isSubmitting}
                        />
                        {error && (
                            <p className="text-sm text-red-500">{error}</p>
                        )}
                    </div>
                </div>
                <DialogFooter>
                    <Button onClick={handleRun} disabled={isSubmitting || !query.trim()}>
                        {isSubmitting ? (
                            <>
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                Submitting...
                            </>
                        ) : (
                            "Start Execution"
                        )}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
