"use client";

/* eslint-disable @typescript-eslint/no-explicit-any */

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { cn, openExternalUrl } from "@/lib/utils";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark.css";
import { ExternalLink, Copy, Check, Sparkles, Microscope, AlertCircle, XCircle, Brain, Users, Zap, CheckCircle, Loader2, Search, Play, Pause, CircleSlash, Clock, Link, MessageSquare, FolderSync, Info, ShieldAlert, RefreshCw } from "lucide-react";
import React, { type ReactNode, useState } from "react";

export interface Citation {
    url: string;
    title?: string;
    source?: string;
    source_type?: string;
    retrieved_at?: string;
    published_date?: string;
    credibility_score?: number;
}

interface Message {
    id: string;
    role: "user" | "assistant" | "system" | "tool" | "status";
    content: string;
    sender?: string;
    timestamp: string;
    isStreaming?: boolean;
    isGenerating?: boolean;
    isError?: boolean;
    isCancelled?: boolean;
    taskId?: string;
    eventType?: string; // For status messages
    metadata?: {
        usage?: {
            total_tokens: number;
            input_tokens: number;
            output_tokens: number;
        };
        model?: string;
        provider?: string;
        citations?: Citation[];
    };
    toolData?: any;
}

// Status message icon mapping based on event type
function StatusIcon({ eventType }: { eventType?: string }) {
    switch (eventType) {
        case "AGENT_THINKING":
            return <Brain className="h-3.5 w-3.5 text-blue-500 animate-pulse" />;
        case "AGENT_STARTED":
            return <Play className="h-3.5 w-3.5 text-green-500" />;
        case "AGENT_COMPLETED":
            return <CheckCircle className="h-3.5 w-3.5 text-green-500" />;
        case "DELEGATION":
            return <Users className="h-3.5 w-3.5 text-purple-500" />;
        case "PROGRESS":
        case "STATUS_UPDATE":
            return <Zap className="h-3.5 w-3.5 text-amber-500" />;
        case "DATA_PROCESSING":
            return <Loader2 className="h-3.5 w-3.5 text-green-500 animate-spin" />;
        case "TOOL_INVOKED":
            return <Search className="h-3.5 w-3.5 text-blue-500 animate-pulse" />;
        case "TOOL_OBSERVATION":
            return <Sparkles className="h-3.5 w-3.5 text-emerald-500" />;
        case "APPROVAL_REQUESTED":
            return <AlertCircle className="h-3.5 w-3.5 text-orange-500" />;
        case "APPROVAL_DECISION":
            return <CheckCircle className="h-3.5 w-3.5 text-green-500" />;
        case "WAITING":
            return <Clock className="h-3.5 w-3.5 text-amber-500 animate-pulse" />;
        case "DEPENDENCY_SATISFIED":
            return <Link className="h-3.5 w-3.5 text-green-500" />;
        case "ERROR_OCCURRED":
            return <ShieldAlert className="h-3.5 w-3.5 text-red-500" />;
        case "ERROR_RECOVERY":
            return <RefreshCw className="h-3.5 w-3.5 text-amber-500 animate-spin" />;
        case "MESSAGE_SENT":
        case "MESSAGE_RECEIVED":
            return <MessageSquare className="h-3.5 w-3.5 text-blue-500" />;
        case "WORKSPACE_UPDATED":
            return <FolderSync className="h-3.5 w-3.5 text-purple-500" />;
        case "workflow.pausing":
            return <Pause className="h-3.5 w-3.5 text-amber-500 animate-pulse" />;
        case "workflow.paused":
            return <Pause className="h-3.5 w-3.5 text-amber-500" />;
        case "workflow.resumed":
            return <Play className="h-3.5 w-3.5 text-green-500" />;
        case "workflow.cancelling":
            return <CircleSlash className="h-3.5 w-3.5 text-red-500 animate-pulse" />;
        case "workflow.cancelled":
            return <XCircle className="h-3.5 w-3.5 text-red-500" />;
        case "WORKFLOW_STARTED":
        default:
            return <Loader2 className="h-3.5 w-3.5 text-muted-foreground animate-spin" />;
    }
}

interface RunConversationProps {
    messages: readonly Message[];
    agentType?: "normal" | "deep_research";
}

// Component to render a single citation with tooltip
function CitationLink({ index, citation }: { index: number; citation: Citation }) {
    const displayTitle = citation.title || citation.source || citation.url;
    const publishedDate = citation.published_date ? new Date(citation.published_date).toLocaleDateString() : null;
    
    return (
        <TooltipProvider delayDuration={200}>
            <Tooltip>
                <TooltipTrigger asChild>
                    <a
                        href={citation.url}
                        className="inline-flex items-baseline text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 font-medium no-underline hover:underline mx-0.5 cursor-pointer"
                        onClick={(e) => {
                            e.preventDefault();
                            openExternalUrl(citation.url);
                        }}
                    >
                        [{index}]
                    </a>
                </TooltipTrigger>
                <TooltipContent 
                    side="top" 
                    className="max-w-sm p-3 space-y-2"
                    sideOffset={5}
                >
                    <div className="space-y-1">
                        <div className="font-semibold text-sm line-clamp-2">
                            {displayTitle}
                        </div>
                        {citation.source && (
                            <div className="text-xs text-muted-foreground flex items-center gap-1">
                                <ExternalLink className="h-3 w-3" />
                                <span className="truncate">{citation.source}</span>
                            </div>
                        )}
                        {publishedDate && (
                            <div className="text-xs text-muted-foreground">
                                Published: {publishedDate}
                            </div>
                        )}
                        {citation.credibility_score !== undefined && citation.credibility_score > 0 && (
                            <div className="text-xs text-muted-foreground">
                                Credibility: {(citation.credibility_score * 100).toFixed(0)}%
                            </div>
                        )}
                    </div>
                </TooltipContent>
            </Tooltip>
        </TooltipProvider>
    );
}

// Component to render text with citation links
function TextWithCitations({ text, citations }: { text: string; citations?: Citation[] }) {
    if (!citations || citations.length === 0 || typeof text !== 'string') {
        return <>{text}</>;
    }

    console.log("[Citations] Processing text:", text.substring(0, 100), "Citations count:", citations.length);

    // Parse citations like [1], [2], etc.
    const citationRegex = /\[(\d+)\]/g;
    const parts: (string | ReactNode)[] = [];
    let lastIndex = 0;
    let match;
    let matchCount = 0;

    while ((match = citationRegex.exec(text)) !== null) {
        matchCount++;
        const fullMatch = match[0];
        const citationIndex = parseInt(match[1], 10);
        const citation = citations[citationIndex - 1]; // Citations are 1-indexed

        console.log("[Citations] Found match:", fullMatch, "Index:", citationIndex, "Has citation:", !!citation);

        // Add text before citation
        if (match.index > lastIndex) {
            parts.push(text.substring(lastIndex, match.index));
        }

        // Add citation link with tooltip
        if (citation) {
            parts.push(
                <CitationLink
                    key={`citation-${match.index}-${citationIndex}`}
                    index={citationIndex}
                    citation={citation}
                />
            );
        } else {
            // Citation not found in metadata, render as plain text
            parts.push(fullMatch);
        }

        lastIndex = match.index + fullMatch.length;
    }

    console.log("[Citations] Total matches found:", matchCount);

    // Add remaining text
    if (lastIndex < text.length) {
        parts.push(text.substring(lastIndex));
    }

    return <>{parts}</>;
}

// Component to render markdown with inline citation components
export function MarkdownWithCitations({ content, citations }: { content: string; citations?: Citation[] }) {
    // Ensure content is a string - handle object content gracefully
    let displayContent = content;
    if (typeof content !== 'string') {
        console.warn("[MarkdownWithCitations] content is not a string:", content);
        if (content && typeof content === 'object') {
            displayContent = (content as any).text || (content as any).message || 
                           (content as any).response || (content as any).content ||
                           JSON.stringify(content, null, 2);
        } else {
            displayContent = String(content || '');
        }
    }
    
    if (!citations || citations.length === 0) {
        return (
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                rehypePlugins={[rehypeHighlight]}
                components={getMarkdownComponents()}
            >
                {displayContent}
            </ReactMarkdown>
        );
    }
    
    console.log("[MarkdownWithCitations] Rendering with citations:", citations.length);
    
    // Custom component to handle text nodes with citations
    const components = {
        ...getMarkdownComponents(),
        // Override paragraph to process citations inline
        p: ({ children, ...props }: any) => {
            const processedChildren = React.Children.map(children, (child, index) => {
                if (typeof child === 'string') {
                    return <TextWithCitations key={index} text={child} citations={citations} />;
                }
                return child;
            });
            return <p className="leading-relaxed break-words" {...props}>{processedChildren}</p>;
        },
        // Also handle other text containers
        li: ({ children, ...props }: any) => {
            const processedChildren = React.Children.map(children, (child, index) => {
                if (typeof child === 'string') {
                    return <TextWithCitations key={index} text={child} citations={citations} />;
                }
                return child;
            });
            return <li {...props}>{processedChildren}</li>;
        },
        td: ({ children, ...props }: any) => {
            const processedChildren = React.Children.map(children, (child, index) => {
                if (typeof child === 'string') {
                    return <TextWithCitations key={index} text={child} citations={citations} />;
                }
                return child;
            });
            return <td {...props}>{processedChildren}</td>;
        },
    };
    
    return (
        <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeHighlight]}
            components={components}
        >
            {displayContent}
        </ReactMarkdown>
    );
}

// Extract markdown components for reuse
function getMarkdownComponents() {
    return {
        code: ({ inline, className, children, ...props }: any) => {
            return inline ? (
                <code className={cn("px-1.5 py-0.5 rounded bg-muted/50 font-mono text-xs break-all", className)} {...props}>
                    {children}
                </code>
            ) : (
                <code className={cn("block p-3 rounded-lg bg-muted/50 overflow-x-auto whitespace-pre font-mono text-xs", className)} {...props}>
                    {children}
                </code>
            );
        },
        pre: ({ children, ...props }: any) => (
            <pre className="my-2 overflow-x-auto rounded-lg bg-black/90 dark:bg-black/50 p-0 whitespace-pre" {...props}>
                {children}
            </pre>
        ),
        p: ({ children, ...props }: any) => (
            <p className="leading-relaxed break-words" {...props}>{children}</p>
        ),
        // Headings
        h1: ({ children, ...props }: any) => (
            <h1 className="mt-2 mb-1 font-semibold text-2xl" {...props}>{children}</h1>
        ),
        h2: ({ children, ...props }: any) => (
            <h2 className="mt-2 mb-1 font-semibold text-xl" {...props}>{children}</h2>
        ),
        h3: ({ children, ...props }: any) => (
            <h3 className="mt-1.5 mb-1 font-semibold text-lg" {...props}>{children}</h3>
        ),
        h4: ({ children, ...props }: any) => (
            <h4 className="mt-1.5 mb-0.5 font-semibold text-base" {...props}>{children}</h4>
        ),
        h5: ({ children, ...props }: any) => (
            <h5 className="mt-1 mb-0.5 font-semibold text-sm" {...props}>{children}</h5>
        ),
        h6: ({ children, ...props }: any) => (
            <h6 className="mt-1 mb-0.5 font-semibold text-xs" {...props}>{children}</h6>
        ),
        // Lists
        ul: ({ children, ...props }: any) => (
            <ul className="ml-4 list-outside list-disc space-y-0.5" {...props}>{children}</ul>
        ),
        ol: ({ children, ...props }: any) => (
            <ol className="ml-4 list-outside list-decimal space-y-0.5" {...props}>{children}</ol>
        ),
        // Blockquote
        blockquote: ({ children, ...props }: any) => (
            <blockquote className="border-l-4 pl-4 italic text-muted-foreground my-2" {...props}>{children}</blockquote>
        ),
        a: ({ children, href, ...props }: any) => (
            <a
                className="underline hover:text-primary cursor-pointer break-all overflow-wrap-anywhere"
                href={href}
                onClick={(e) => {
                    if (href && (href.startsWith('http://') || href.startsWith('https://'))) {
                        e.preventDefault();
                        openExternalUrl(href);
                    }
                }}
                {...props}
            >
                {children}
            </a>
        ),
    };
}

export function RunConversation({ messages, agentType = "normal" }: RunConversationProps) {
    const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null);

    // Debug: Check if any messages have citations
    const messagesWithCitations = messages.filter(m => m.metadata?.citations && m.metadata.citations.length > 0);
    if (messagesWithCitations.length > 0) {
        console.log("[Citations] Messages with citations:", messagesWithCitations.length);
        messagesWithCitations.forEach(m => {
            console.log("[Citations] Message:", m.id, "Citations:", m.metadata?.citations?.length);
        });
    }

    // Copy message content to clipboard
    const handleCopyMessage = async (messageId: string, content: string) => {
        try {
            await navigator.clipboard.writeText(content);
            setCopiedMessageId(messageId);
            // Reset the copied state after 2 seconds
            setTimeout(() => {
                setCopiedMessageId(null);
            }, 2000);
        } catch (err) {
            console.error("Failed to copy message:", err);
        }
    };

    // Determine if a message is intermediate (from a specific agent in multi-agent flow)
    // Per backend guidance: synthesis outputs are intermediate during streaming
    // Final answers come via API fetch after WORKFLOW_COMPLETED, or directly from simple-agent
    const isIntermediateMessage = (msg: Message) => {
        if (msg.role !== "assistant") return false;
        const sender = msg.sender || "";
        
        // Check if it's a tool call message (JSON with selected_tools)
        if (msg.content && msg.content.trim().startsWith('{') && msg.content.includes('"selected_tools"')) {
            return true;
        }
        
        // Empty sender is treated as final output (API-fetched results, simple responses)
        if (!sender) return false;
        
        // WHITELIST: Only simple-agent shows directly during streaming
        // Other final answers (synthesis) come from API fetch after completion
        const directOutputAgents = [
            "simple-agent",        // Simple task agent (non-research)
        ];
        
        // If it's a direct output agent, it's not intermediate
        if (directOutputAgents.includes(sender)) return false;
        
        // Everything else is intermediate (synthesis, reasoner-*, actor-*, etc.)
        return true;
    };

    // Always filter out intermediate agent messages (they go to timeline panel)
    const displayedMessages = messages.filter(msg => !isIntermediateMessage(msg));

    return (
        <div className="space-y-2 p-3 sm:p-4 overflow-hidden">
            {displayedMessages.map((message) => {
                // Render status messages inline (human-readable progress updates)
                if (message.role === "status") {
                    return (
                        <div 
                            key={message.id}
                            className="flex items-center justify-center gap-2 py-2 animate-in fade-in-0 slide-in-from-bottom-2 duration-300"
                        >
                            <div className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-muted/50 text-muted-foreground text-xs">
                                <StatusIcon eventType={message.eventType} />
                                <span>{message.content}</span>
                            </div>
                        </div>
                    );
                }

                // Render user and final assistant messages normally
                return (
                <div
                    key={message.id}
                    className={cn(
                        "flex gap-2 sm:gap-3",
                        message.role === "user" ? "flex-row-reverse" : "flex-row"
                    )}
                >
                    <Avatar className="h-7 w-7 sm:h-8 sm:w-8 shrink-0">
                        <AvatarFallback className={cn(
                            message.role === "user" ? "bg-primary text-primary-foreground" :
                                message.role === "tool" ? "bg-orange-100 text-orange-700" : 
                                message.role === "system" ? (message.isError ? "bg-red-100 dark:bg-red-900/30" : message.isCancelled ? "bg-yellow-100 dark:bg-yellow-900/30" : "bg-gray-100 dark:bg-gray-900/30") :
                                agentType === "deep_research" ? "bg-violet-100 dark:bg-violet-900/30" : "bg-amber-100 dark:bg-amber-900/30"
                        )}>
                            {message.role === "user" ? "U" : 
                             message.role === "tool" ? "T" : 
                             message.role === "system" ? (message.isError ? <AlertCircle className="h-4 w-4 text-red-500" /> : message.isCancelled ? <XCircle className="h-4 w-4 text-yellow-600" /> : "S") :
                             agentType === "deep_research" ? <Microscope className="h-4 w-4 text-violet-500" /> : <Sparkles className="h-4 w-4 text-amber-500" />}
                        </AvatarFallback>
                    </Avatar>
                    <div className={cn(
                        "flex max-w-[85%] sm:max-w-[80%] flex-col gap-1 min-w-0",
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
                        <div className="space-y-2 min-w-0 w-full">
                            <Card className={cn(
                                "px-2 sm:px-3 py-1 text-sm prose prose-sm max-w-none prose-p:my-0.5 prose-p:leading-relaxed prose-ul:my-0.5 prose-ol:my-0.5 prose-li:my-0 prose-headings:mt-2 prose-headings:mb-0.5 prose-headings:leading-tight break-words overflow-wrap-anywhere overflow-hidden",
                                message.role === "user" ? "bg-primary text-primary-foreground [&_a]:text-primary-foreground [&_a]:underline [&_a]:decoration-primary-foreground/50 [&_a:hover]:text-primary-foreground/80 [&_a:hover]:decoration-primary-foreground" :
                                    message.role === "tool" ? "bg-muted/50 font-mono text-xs prose-pre:bg-transparent" : 
                                    message.role === "system" ? (message.isError ? "bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border-red-200 dark:border-red-800" : message.isCancelled ? "bg-yellow-50 dark:bg-yellow-900/20 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-800" : "bg-gray-50 dark:bg-gray-900/20") :
                                    "bg-muted dark:prose-invert"
                            )}>
                                {message.isGenerating ? (
                                    <span className="flex items-center gap-1 text-muted-foreground">
                                        <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                                        <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
                                        <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
                                    </span>
                                ) : (
                                    <>
                                        <MarkdownWithCitations 
                                            content={message.content} 
                                            citations={message.metadata?.citations}
                                        />
                                        {message.isStreaming && (
                                            <span className="inline-block w-2 h-4 ml-1 bg-current animate-pulse" />
                                        )}
                                    </>
                                )}
                            </Card>
                            {/* Action buttons below the card - modern design pattern */}
                            {!message.isGenerating && (
                                <div className="flex items-center gap-1 ml-1">
                                    <TooltipProvider>
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <Button
                                                    variant="ghost"
                                                    size="sm"
                                                    className="h-5 w-5 p-0 hover:bg-muted"
                                                    onClick={() => handleCopyMessage(message.id, message.content)}
                                                >
                                                    {copiedMessageId === message.id ? (
                                                        <Check className="h-3 w-3" />
                                                    ) : (
                                                        <Copy className="h-3 w-3" />
                                                    )}
                                                </Button>
                                            </TooltipTrigger>
                                            <TooltipContent>
                                                <p>{copiedMessageId === message.id ? "Copied!" : "Copy"}</p>
                                            </TooltipContent>
                                        </Tooltip>
                                    </TooltipProvider>
                                </div>
                            )}
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
                );
            })}
        </div>
    );
}
