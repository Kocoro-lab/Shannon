import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

interface Message {
    id: string;
    role: "user" | "assistant" | "system" | "tool";
    content: string;
    sender?: string;
    timestamp: string;
    isStreaming?: boolean;
    metadata?: {
        usage?: {
            total_tokens: number;
            input_tokens: number;
            output_tokens: number;
        };
        model?: string;
        provider?: string;
    };
    toolData?: any;
}

interface RunConversationProps {
    messages: readonly Message[];
}

export function RunConversation({ messages }: RunConversationProps) {
    return (
        <div className="space-y-4 p-4">
            {messages.map((message) => (
                <div
                    key={message.id}
                    className={cn(
                        "flex gap-3",
                        message.role === "user" ? "flex-row-reverse" : "flex-row"
                    )}
                >
                    <Avatar className="h-8 w-8">
                        <AvatarFallback className={cn(
                            message.role === "user" ? "bg-primary text-primary-foreground" :
                                message.role === "tool" ? "bg-orange-100 text-orange-700" : "bg-muted"
                        )}>
                            {message.role === "user" ? "U" : message.role === "tool" ? "T" : "A"}
                        </AvatarFallback>
                    </Avatar>
                    <div className={cn(
                        "flex max-w-[80%] flex-col gap-1",
                        message.role === "user" ? "items-end" : "items-start"
                    )}>
                        <div className="flex items-center gap-2">
                            <span className="text-xs font-medium text-muted-foreground">
                                {message.sender || message.role}
                            </span>
                            <span className="text-xs text-muted-foreground">
                                {message.timestamp}
                            </span>
                        </div>
                        <div className="space-y-2">
                            <Card className={cn(
                                "p-3 text-sm",
                                message.role === "user" ? "bg-primary text-primary-foreground" :
                                    message.role === "tool" ? "bg-muted/50 font-mono text-xs" : "bg-muted"
                            )}>
                                {message.content}
                                {message.isStreaming && (
                                    <span className="inline-block w-2 h-4 ml-1 bg-current animate-pulse" />
                                )}
                            </Card>
                            {message.metadata?.usage && (
                                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                    <span>{message.metadata.usage.total_tokens} tokens</span>
                                    <span>•</span>
                                    <span>
                                        {message.metadata.model}
                                        {message.metadata.provider && ` (${message.metadata.provider})`}
                                    </span>
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            ))}
        </div>
    );
}
