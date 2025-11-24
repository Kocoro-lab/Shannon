"use client";

/* eslint-disable @typescript-eslint/no-explicit-any */

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark.css";
import { ExternalLink } from "lucide-react";
import React, { type ReactNode } from "react";
import { CollapsibleMessage } from "@/components/collapsible-message";

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
    role: "user" | "assistant" | "system" | "tool";
    content: string;
    sender?: string;
    timestamp: string;
    isStreaming?: boolean;
    isGenerating?: boolean;
    taskId?: string;
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

interface RunConversationProps {
    messages: readonly Message[];
    showAgentTrace?: boolean;
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
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-baseline text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 font-medium no-underline hover:underline mx-0.5 cursor-pointer"
                        onClick={(e) => {
                            e.preventDefault();
                            window.open(citation.url, '_blank', 'noopener,noreferrer');
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
    if (!citations || citations.length === 0) {
        return (
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                rehypePlugins={[rehypeHighlight]}
                components={getMarkdownComponents()}
            >
                {content}
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
            return <p className="leading-relaxed" {...props}>{processedChildren}</p>;
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
            {content}
        </ReactMarkdown>
    );
}

// Extract markdown components for reuse
function getMarkdownComponents() {
    return {
        code: ({ inline, className, children, ...props }: any) => {
            return inline ? (
                <code className={cn("px-1.5 py-0.5 rounded bg-muted/50 font-mono text-xs", className)} {...props}>
                    {children}
                </code>
            ) : (
                <code className={cn("block p-3 rounded-lg bg-muted/50 overflow-x-auto", className)} {...props}>
                    {children}
                </code>
            );
        },
        pre: ({ children, ...props }) => (
            <pre className="my-2 overflow-x-auto rounded-lg bg-black/90 dark:bg-black/50 p-0" {...props}>
                {children}
            </pre>
        ),
        p: ({ children, ...props }) => (
            <p className="leading-relaxed" {...props}>{children}</p>
        ),
        // Headings
        h1: ({ children, ...props }) => (
            <h1 className="mt-2 mb-1 font-semibold text-2xl" {...props}>{children}</h1>
        ),
        h2: ({ children, ...props }) => (
            <h2 className="mt-2 mb-1 font-semibold text-xl" {...props}>{children}</h2>
        ),
        h3: ({ children, ...props }) => (
            <h3 className="mt-1.5 mb-1 font-semibold text-lg" {...props}>{children}</h3>
        ),
        h4: ({ children, ...props }) => (
            <h4 className="mt-1.5 mb-0.5 font-semibold text-base" {...props}>{children}</h4>
        ),
        h5: ({ children, ...props }) => (
            <h5 className="mt-1 mb-0.5 font-semibold text-sm" {...props}>{children}</h5>
        ),
        h6: ({ children, ...props }) => (
            <h6 className="mt-1 mb-0.5 font-semibold text-xs" {...props}>{children}</h6>
        ),
        // Lists
        ul: ({ children, ...props }) => (
            <ul className="ml-4 list-outside list-disc space-y-0.5" {...props}>{children}</ul>
        ),
        ol: ({ children, ...props }) => (
            <ol className="ml-4 list-outside list-decimal space-y-0.5" {...props}>{children}</ol>
        ),
        // Blockquote
        blockquote: ({ children, ...props }) => (
            <blockquote className="border-l-4 pl-4 italic text-muted-foreground my-2" {...props}>{children}</blockquote>
        ),
        a: ({ children, ...props }) => (
            <a className="underline hover:text-primary cursor-pointer" {...props}>{children}</a>
        ),
    };
}

export function RunConversation({ messages, showAgentTrace = false }: RunConversationProps) {
    // Debug: Check if any messages have citations
    const messagesWithCitations = messages.filter(m => m.metadata?.citations && m.metadata.citations.length > 0);
    if (messagesWithCitations.length > 0) {
        console.log("[Citations] Messages with citations:", messagesWithCitations.length);
        messagesWithCitations.forEach(m => {
            console.log("[Citations] Message:", m.id, "Citations:", m.metadata?.citations?.length);
        });
    }

    // Determine if a message is intermediate (from a specific agent in multi-agent flow)
    const isIntermediateMessage = (msg: Message) => {
        if (msg.role !== "assistant") return false;
        const sender = msg.sender || "";
        
        // Check if it's a tool call message (JSON with selected_tools)
        if (msg.content && msg.content.trim().startsWith('{') && msg.content.includes('"selected_tools"')) {
            return true;
        }
        
        // Intermediate agents: reasoner-*, actor-*, react-synthesizer, etc.
        // Final synthesis: "synthesis" or empty/undefined (default assistant)
        return sender && sender !== "synthesis" && sender !== "assistant" && sender !== "";
    };

    // Filter messages based on showAgentTrace toggle
    const displayedMessages = showAgentTrace 
        ? messages 
        : messages.filter(msg => !isIntermediateMessage(msg));

    return (
        <div className="space-y-2 p-4">
            {displayedMessages.map((message) => {
                // Render intermediate messages with CollapsibleMessage
                if (isIntermediateMessage(message) && showAgentTrace) {
                    return (
                        <CollapsibleMessage
                            key={message.id}
                            sender={message.sender || "agent"}
                            content={message.content}
                            timestamp={message.timestamp}
                        />
                    );
                }

                // Render user and final assistant messages normally
                return (
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
                                "px-3 py-1 text-sm prose prose-sm max-w-none prose-p:my-0.5 prose-p:leading-relaxed prose-ul:my-0.5 prose-ol:my-0.5 prose-li:my-0 prose-headings:mt-2 prose-headings:mb-0.5 prose-headings:leading-tight",
                                message.role === "user" ? "bg-primary text-primary-foreground prose-invert" :
                                    message.role === "tool" ? "bg-muted/50 font-mono text-xs prose-pre:bg-transparent" : "bg-muted dark:prose-invert"
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
