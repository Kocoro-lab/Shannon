import { CheckCircle2, Circle, AlertCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { CollapsibleDetails } from "./collapsible-details";

interface TimelineEvent {
    id: string;
    type: "agent" | "llm" | "tool" | "system";
    status: "completed" | "running" | "failed" | "pending";
    title: string;
    timestamp: string;
    details?: string;
    detailsType?: "json" | "text";
}

interface RunTimelineProps {
    events: readonly TimelineEvent[];
}

export function RunTimeline({ events }: RunTimelineProps) {
    return (
        <div className="space-y-6 p-4">
            {events.map((event, index) => (
                <div key={`${event.id}-${index}`} className="relative pl-8 pr-2">
                    {/* Vertical line */}
                    {index !== events.length - 1 && (
                        <div className="absolute left-[11px] top-8 h-full w-px bg-border" />
                    )}

                    {/* Icon */}
                    <div className={cn(
                        "absolute left-0 top-1 flex h-6 w-6 items-center justify-center rounded-full border bg-background",
                        event.status === "running" && "border-blue-500 text-blue-500",
                        event.status === "failed" && "border-red-500 text-red-500",
                        event.status === "completed" && "border-green-500 text-green-500"
                    )}>
                        {event.status === "completed" && <CheckCircle2 className="h-4 w-4" />}
                        {event.status === "running" && <Circle className="h-4 w-4 animate-pulse fill-current" />}
                        {event.status === "failed" && <AlertCircle className="h-4 w-4" />}
                        {event.status === "pending" && <Circle className="h-4 w-4 text-muted-foreground" />}
                    </div>

                    {/* Content */}
                    <div className="space-y-1">
                        <div className="flex items-center gap-2">
                            <span className="text-sm font-medium leading-none">
                                {event.title}
                            </span>
                            <span className="text-xs text-muted-foreground">
                                {event.timestamp}
                            </span>
                        </div>
                        {event.details && event.detailsType && (
                            <CollapsibleDetails
                                content={event.details}
                                type={event.detailsType}
                            />
                        )}
                    </div>
                </div>
            ))}
        </div>
    );
}
