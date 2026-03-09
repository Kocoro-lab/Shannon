import { CheckCircle2, Circle, AlertCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { CollapsibleDetails } from "./collapsible-details";
import { generateAgentColor, LEAD_COLOR } from "@/lib/swarm/agent-colors";

interface TimelineEvent {
    id: string;
    type: "agent" | "llm" | "tool" | "system";
    status: "completed" | "running" | "failed" | "pending";
    title: string;
    timestamp: string;
    details?: string;
    detailsType?: "json" | "text";
    thumbnailUrl?: string;
    agentId?: string;
    agentRole?: string;
}

interface RunTimelineProps {
    events: readonly TimelineEvent[];
    swarmMode?: boolean;
    agentRegistry?: Record<string, { colorIndex: number; role: string; status: string }>;
}

/** Get agent-specific icon color for swarm mode. Returns undefined for non-swarm. */
function getAgentIconColor(
    event: TimelineEvent,
    swarmMode?: boolean,
    agentRegistry?: Record<string, { colorIndex: number; role: string; status: string }>,
): string | undefined {
    if (!swarmMode || !event.agentId) return undefined;
    const isLead = event.agentId === "swarm-lead" || event.agentId === "swarm-supervisor";
    if (isLead) return LEAD_COLOR.dot;
    const info = agentRegistry?.[event.agentId];
    if (info) return generateAgentColor(info.colorIndex).dot;
    return undefined;
}

export function RunTimeline({ events, swarmMode, agentRegistry }: RunTimelineProps) {
    return (
        <div className="space-y-6 p-4">
            {events.map((event, index) => {
                const agentColor = getAgentIconColor(event, swarmMode, agentRegistry);

                return (
                    <div key={`${event.id}-${index}`} className="relative pl-8 pr-2">
                        {/* Vertical line */}
                        {index !== events.length - 1 && (
                            <div className="absolute left-[11px] top-8 h-full w-px bg-border" />
                        )}

                        {/* Icon — use agent color in swarm mode, default status colors otherwise */}
                        <div
                            className={cn(
                                "absolute left-0 top-1 flex h-6 w-6 items-center justify-center rounded-full border bg-background",
                                !agentColor && event.status === "running" && "border-blue-500 text-blue-500",
                                !agentColor && event.status === "failed" && "border-red-500 text-red-500",
                                !agentColor && event.status === "completed" && "border-green-500 text-green-500",
                            )}
                            style={agentColor ? { borderColor: agentColor, color: agentColor } : undefined}
                        >
                            {event.status === "completed" && <CheckCircle2 className="h-4 w-4" />}
                            {event.status === "running" && <Circle className="h-4 w-4 animate-pulse fill-current" />}
                            {event.status === "failed" && <AlertCircle className="h-4 w-4" />}
                            {event.status === "pending" && <Circle className="h-4 w-4 text-muted-foreground" />}
                        </div>

                        {/* Content */}
                        <div className="space-y-1">
                            <div className="flex items-center gap-2">
                                <span className="text-sm font-medium leading-none whitespace-pre-wrap break-words">
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
                            {event.thumbnailUrl && (
                                <div className="mt-2">
                                    <img
                                        src={event.thumbnailUrl}
                                        alt="Screenshot"
                                        className="rounded border max-h-32 object-cover object-top"
                                        loading="lazy"
                                    />
                                </div>
                            )}
                        </div>
                    </div>
                );
            })}
        </div>
    );
}
