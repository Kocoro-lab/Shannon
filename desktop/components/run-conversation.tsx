"use client";

/* eslint-disable @typescript-eslint/no-explicit-any */

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Card } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Button } from "@/components/ui/button";
import { cn, openExternalUrl, isSafeUrl } from "@/lib/utils";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark.css";
import { ExternalLink, Copy, Check, Sparkles, Microscope, AlertCircle, XCircle, Brain, Users, Zap, CheckCircle, Loader2, Search, Play, Pause, CircleSlash, Clock, Link, MessageSquare, FolderSync, ShieldAlert, RefreshCw, Image as ImageIcon, Globe, Camera, TrendingUp } from "lucide-react";
import { ToolOutputRenderer, detectToolOutputType } from "@/components/tool-output-renderers";
import NextImage from "next/image";
import React, { type ReactNode, useState, useMemo, memo, useEffect } from "react";

// Stub: model display names (inline fallback)
function getModelDisplayName(model: string): string { return model; }
function getProviderDisplayName(provider: string): string { return provider; }
function useIsIOS(): boolean { return false; }

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
    // Browser automation specific
    isScreenshot?: boolean;
    isBrowserError?: boolean;
    metadata?: {
        usage?: {
            total_tokens: number;
            input_tokens: number;
            output_tokens: number;
        };
        model?: string;
        provider?: string;
        citations?: Citation[];
        // Browser automation metadata
        screenshot?: string; // Base64 PNG
        screenshotPath?: string; // Workspace file path for SCREENSHOT_SAVED
        sessionId?: string; // Session ID for workspace file fetch
        pageUrl?: string;
        pageTitle?: string;
        tool?: string; // Tool that failed (for errors)
        retryAfterSeconds?: number; // For rate limit errors
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
        // Browser automation events
        case "TOOL_STARTED":
            return <Globe className="h-3.5 w-3.5 text-blue-500 animate-pulse" />;
        case "TOOL_COMPLETED":
            return <CheckCircle className="h-3.5 w-3.5 text-green-500" />;
        case "ROLE_ASSIGNED":
            return <Globe className="h-3.5 w-3.5 text-blue-500" />;
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

// Helper to convert simple URL citations to Citation objects
function convertUrlsToCitations(urls: string[]): Citation[] {
    return urls.map(url => {
        // Extract domain as source
        let domain = "";
        try {
            domain = new URL(url).hostname.replace("www.", "");
        } catch {
            domain = url;
        }
        return {
            url,
            title: domain,
            source: domain,
        };
    });
}

// Helper to extract domain info from URL
function getDomainInfo(url: string, source?: string): { domain: string; sourceName: string } {
    let domain = "";
    let sourceName = source || "";
    try {
        const parsed = new URL(url);
        domain = parsed.hostname;
        if (!sourceName) {
            sourceName = domain.replace(/^www\./, '').split('.')[0];
            sourceName = sourceName.charAt(0).toUpperCase() + sourceName.slice(1);
        }
    } catch {
        domain = source || "";
        sourceName = domain;
    }
    return { domain, sourceName };
}

// Helper to generate a readable title from URL when no title available
function getReadableTitle(url: string, title?: string): string {
    if (title) return title;

    try {
        const parsed = new URL(url);
        const params = parsed.searchParams;

        // Check for common search query params
        const searchQuery = params.get('q') || params.get('query') || params.get('search') || params.get('s');
        if (searchQuery) {
            return `Search: "${decodeURIComponent(searchQuery)}"`;
        }

        // Clean up pathname
        let path = parsed.pathname;
        if (path === '/' || path === '') {
            return parsed.hostname.replace(/^www\./, '');
        }

        path = path.replace(/^\/+|\/+$/g, '').replace(/\.\w+$/, '');
        const segments = path.split('/').filter(s => s.length > 0);
        const lastSegment = segments[segments.length - 1] || '';

        let readable = decodeURIComponent(lastSegment)
            .replace(/[-_]/g, ' ')
            .replace(/\s+/g, ' ')
            .trim();

        if (readable) {
            readable = readable.split(' ')
                .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
                .join(' ');
            return readable.length > 60 ? readable.slice(0, 60) + '...' : readable;
        }

        return parsed.hostname.replace(/^www\./, '');
    } catch {
        return url.slice(0, 60) + (url.length > 60 ? '...' : '');
    }
}

interface RunConversationProps {
    messages: readonly Message[];
    agentType?: "normal" | "deep_research" | "browser_use";
    sessionTitle?: string;
}


// Component to render text with citation badges grouped by domain
function TextWithCitations({ text, citations }: { text: string; citations?: Citation[] }) {
    if (!citations || citations.length === 0 || typeof text !== 'string') {
        return <>{text}</>;
    }

    // Find all citation references like [1], [2], [1][2][3], etc.
    // Group consecutive citations by domain
    const citationPattern = /(\[(\d+)\])+/g;
    const parts: (string | ReactNode)[] = [];
    let lastIndex = 0;
    let match;

    while ((match = citationPattern.exec(text)) !== null) {
        const fullMatch = match[0]; // e.g., "[1][2][3]" or "[1]"

        // Extract all citation indices from this match
        const indices: number[] = [];
        const singlePattern = /\[(\d+)\]/g;
        let singleMatch;
        while ((singleMatch = singlePattern.exec(fullMatch)) !== null) {
            indices.push(parseInt(singleMatch[1], 10));
        }

        // Get citations and group by domain
        const citationsForMatch: Citation[] = [];
        for (const idx of indices) {
            const citation = citations[idx - 1]; // Citations are 1-indexed
            if (citation) {
                citationsForMatch.push(citation);
            }
        }

        // Add text before citation
        if (match.index > lastIndex) {
            parts.push(text.substring(lastIndex, match.index));
        }

        if (citationsForMatch.length > 0) {
            // Group by domain
            const byDomain = new Map<string, { citations: Citation[]; sourceName: string; domain: string }>();

            for (const citation of citationsForMatch) {
                const { domain, sourceName } = getDomainInfo(citation.url, citation.source);
                const existing = byDomain.get(domain);
                if (existing) {
                    existing.citations.push(citation);
                } else {
                    byDomain.set(domain, { citations: [citation], sourceName, domain });
                }
            }

            // Render badges for each domain group - use domain as stable key
            byDomain.forEach(({ citations: domainCitations, sourceName, domain }) => {
                parts.push(
                    <CitationBadge
                        key={`badge-${match!.index}-${domain}`}
                        citations={domainCitations}
                        sourceName={sourceName}
                        domain={domain}
                    />
                );
            });
        } else {
            // No valid citations found, render as plain text
            parts.push(fullMatch);
        }

        lastIndex = match.index + fullMatch.length;
    }

    // Add remaining text
    if (lastIndex < text.length) {
        parts.push(text.substring(lastIndex));
    }

    return <>{parts}</>;
}

// Memoized markdown components for better performance
const baseMarkdownComponents = getMarkdownComponents();

// Helper to strip Sources/References section from content
// Since users can access citations via the popup button, we hide the inline section
function stripSourcesSection(text: string): string {
    // Match common patterns for Sources/References sections at the end of content
    // Supports: ## Sources, ## References, ## Citations, **Sources**, ---\n## Sources, etc.
    // Also supports Japanese: 参照, 参考文献, 引用元
    const patterns = [
        // Markdown heading patterns (## Sources, ### References, etc.)
        /\n---\s*\n##?\s*(?:Sources|References|Citations|参照|参考文献|引用元)[\s\S]*$/i,
        /\n##?\s*(?:Sources|References|Citations|参照|参考文献|引用元)\s*\n[\s\S]*$/i,
        // Bold heading patterns (**Sources**, **References**)
        /\n\*\*(?:Sources|References|Citations|参照|参考文献|引用元)\*\*:?\s*\n[\s\S]*$/i,
    ];

    for (const pattern of patterns) {
        const match = text.match(pattern);
        if (match) {
            return text.slice(0, match.index).trimEnd();
        }
    }

    return text;
}

// Component to render markdown with inline citation components
export const MarkdownWithCitations = memo(function MarkdownWithCitations({ content, citations }: { content: string; citations?: Citation[] }) {
    // Ensure content is a string - handle object content gracefully
    const displayContent = useMemo(() => {
        let text: string;
        if (typeof content !== 'string') {
            console.warn("[MarkdownWithCitations] content is not a string:", content);
            if (content && typeof content === 'object') {
                text = (content as any).text || (content as any).message ||
                    (content as any).response || (content as any).content ||
                    JSON.stringify(content, null, 2);
            } else {
                text = String(content || '');
            }
        } else {
            text = content;
        }

        // Strip the Sources/References section since users can access via popup button
        return stripSourcesSection(text);
    }, [content]);

    // Memoize the components object to prevent recreation on every render
    const components = useMemo(() => {
        if (!citations || citations.length === 0) {
            return baseMarkdownComponents;
        }

        // Helper to process children with citations - handles nested React elements recursively
        const processChildren = (children: React.ReactNode): React.ReactNode => {
            return React.Children.map(children, (child, index) => {
                if (typeof child === 'string') {
                    // Use a stable key based on content hash instead of index
                    const stableKey = `text-${index}-${child.slice(0, 20).replace(/\s/g, '')}`;
                    return <TextWithCitations key={stableKey} text={child} citations={citations} />;
                }
                // Handle React elements with children (e.g., <strong>, <em>, <a>)
                if (React.isValidElement(child)) {
                    const childProps = child.props as { children?: React.ReactNode };
                    if (childProps.children) {
                        return React.cloneElement(child as React.ReactElement<{ children?: React.ReactNode }>, {
                            key: child.key || `elem-${index}`,
                            children: processChildren(childProps.children)
                        });
                    }
                }
                return child;
            });
        };

        return {
            ...baseMarkdownComponents,
            p: ({ children, ...props }: any) => (
                <p className="leading-relaxed break-words" {...props}>{processChildren(children)}</p>
            ),
            li: ({ children, ...props }: any) => (
                <li {...props}>{processChildren(children)}</li>
            ),
            td: ({ children, ...props }: any) => (
                <td {...props}>{processChildren(children)}</td>
            ),
        };
    }, [citations]);

    return (
        <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeHighlight]}
            components={components}
        >
            {displayContent}
        </ReactMarkdown>
    );
});

// Extract markdown components for reuse
function getMarkdownComponents() {
    return {
        code: ({ className, children, ...props }: any) => {
            // react-markdown v10 no longer passes `inline` prop.
            // Block code has language-* class (from rehypeHighlight) and is wrapped in <pre>.
            const isBlock = /language-|hljs/.test(className || '');
            return isBlock ? (
                <code className={cn("block p-3 rounded-lg bg-muted/50 overflow-x-auto whitespace-pre font-mono text-xs", className)} {...props}>
                    {children}
                </code>
            ) : (
                <code className={cn("px-1.5 py-0.5 rounded bg-muted/50 font-mono text-xs break-all", className)} {...props}>
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
        // Headings - adjusted for better readability
        h1: ({ children, ...props }: any) => (
            <h1 className="!mt-3 !mb-2 !font-bold !text-xl !text-current" {...props}>{children}</h1>
        ),
        h2: ({ children, ...props }: any) => (
            <h2 className="!mt-3 !mb-1.5 !font-semibold !text-lg !text-current" {...props}>{children}</h2>
        ),
        h3: ({ children, ...props }: any) => (
            <h3 className="!mt-2 !mb-1 !font-semibold !text-base !text-current" {...props}>{children}</h3>
        ),
        h4: ({ children, ...props }: any) => (
            <h4 className="!mt-2 !mb-1 !font-semibold !text-base !text-current" {...props}>{children}</h4>
        ),
        h5: ({ children, ...props }: any) => (
            <h5 className="!mt-1.5 !mb-0.5 !font-medium !text-sm !text-current" {...props}>{children}</h5>
        ),
        h6: ({ children, ...props }: any) => (
            <h6 className="!mt-1.5 !mb-0.5 !font-medium !text-sm !text-current" {...props}>{children}</h6>
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
        // Horizontal rule - reduced margins
        hr: ({ ...props }: any) => (
            <hr className="my-3 border-border" {...props} />
        ),
        a: ({ children, href, ...props }: any) => (
            <a
                className="!text-current underline hover:text-primary cursor-pointer break-all overflow-wrap-anywhere"
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
        // Bold and italic
        strong: ({ children, ...props }: any) => (
            <strong className="!font-semibold !text-current" {...props}>{children}</strong>
        ),
        em: ({ children, ...props }: any) => (
            <em className="!italic !text-current" {...props}>{children}</em>
        ),
        // Tables
        table: ({ children, ...props }: any) => (
            <div className="overflow-x-auto my-2">
                <table className="min-w-full divide-y divide-border" {...props}>{children}</table>
            </div>
        ),
        thead: ({ children, ...props }: any) => (
            <thead className="bg-muted/50" {...props}>{children}</thead>
        ),
        th: ({ children, ...props }: any) => (
            <th className="px-3 py-2 text-left text-xs font-semibold !text-current" {...props}>{children}</th>
        ),
        td: ({ children, ...props }: any) => (
            <td className="px-3 py-2 text-sm !text-current" {...props}>{children}</td>
        ),
    };
}

// Browser screenshot preview component
function ScreenshotPreview({
    screenshot,
    screenshotPath,
    sessionId,
    pageUrl,
    pageTitle
}: {
    screenshot?: string;
    screenshotPath?: string;
    sessionId?: string;
    pageUrl?: string;
    pageTitle?: string;
}) {
    const [isExpanded, setIsExpanded] = useState(false);
    const [fetchedSrc, setFetchedSrc] = useState<string | null>(null);
    const [isLoading, setIsLoading] = useState(false);
    const [fetchError, setFetchError] = useState(false);

    // Fetch screenshot from workspace when path-based props are provided
    useEffect(() => {
        if (!screenshotPath || !sessionId || screenshot) return;
        // Sanitize path to prevent traversal
        const safePath = screenshotPath.replace(/\.\.\//g, '').replace(/^\//, '');
        if (!safePath) return;
        let cancelled = false;
        setIsLoading(true);
        setFetchError(false);
        // Workspace file fetch not available in OSS — show path as fallback
        setFetchError(true);
        setIsLoading(false);
        return () => { cancelled = true; };
    }, [screenshotPath, sessionId, screenshot]);

    // Handle both raw base64 and data URL formats
    const imgSrc = screenshot
        ? (screenshot.startsWith('data:') ? screenshot : `data:image/png;base64,${screenshot}`)
        : fetchedSrc;

    if (isLoading) {
        return (
            <div className="flex items-center gap-2 text-sm text-muted-foreground py-4">
                <Loader2 className="h-4 w-4 animate-spin" />
                <span>Loading screenshot...</span>
            </div>
        );
    }

    if (fetchError) {
        return (
            <div className="text-sm text-muted-foreground py-2">
                Failed to load screenshot
            </div>
        );
    }

    if (!imgSrc) return null;

    return (
        <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Camera className="h-4 w-4" />
                <span>Screenshot captured</span>
                {pageTitle && <span className="text-xs">• {pageTitle}</span>}
            </div>
            <div className="relative">
                <button
                    type="button"
                    onClick={() => setIsExpanded(!isExpanded)}
                    className={cn(
                        "block w-full rounded-lg overflow-hidden border hover:border-primary transition-colors cursor-pointer",
                        isExpanded ? "max-h-none" : "max-h-64"
                    )}
                >
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img
                        src={imgSrc}
                        alt={pageTitle || "Browser screenshot"}
                        className={cn(
                            "w-full object-contain",
                            !isExpanded && "max-h-64"
                        )}
                    />
                    {!isExpanded && (
                        <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/60 to-transparent p-2 text-white text-xs text-center">
                            Click to expand
                        </div>
                    )}
                </button>
            </div>
            {pageUrl && (
                <a
                    href={pageUrl}
                    className="text-xs text-blue-600 dark:text-blue-400 hover:underline truncate block"
                    onClick={(e) => {
                        e.preventDefault();
                        openExternalUrl(pageUrl);
                    }}
                >
                    {pageUrl}
                </a>
            )}
        </div>
    );
}

// Browser error message with retry button
function BrowserErrorMessage({ 
    error, 
    tool, 
    retryAfterSeconds,
    onRetry 
}: { 
    error: string; 
    tool?: string;
    retryAfterSeconds?: number;
    onRetry?: () => void;
}) {
    const [countdown, setCountdown] = useState(retryAfterSeconds || 0);
    
    // Countdown timer for rate limits
    useEffect(() => {
        if (countdown <= 0) return;
        const timer = setInterval(() => {
            setCountdown(c => Math.max(0, c - 1));
        }, 1000);
        return () => clearInterval(timer);
    }, [countdown]);
    
    const isRateLimit = error.toLowerCase().includes("rate limit");
    const isTimeout = error.toLowerCase().includes("timeout");
    const isNotFound = error.toLowerCase().includes("not found");
    
    return (
        <div className="flex items-start gap-3 p-3 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800">
            <AlertCircle className="h-5 w-5 text-red-500 shrink-0 mt-0.5" />
            <div className="flex-1 min-w-0 space-y-2">
                <div>
                    <p className="text-sm font-medium text-red-700 dark:text-red-300">
                        {isRateLimit ? "Rate Limit Exceeded" : 
                         isTimeout ? "Operation Timed Out" : 
                         isNotFound ? "Element Not Found" : 
                         "Browser Action Failed"}
                    </p>
                    <p className="text-xs text-red-600 dark:text-red-400 mt-0.5">
                        {error}
                    </p>
                    {tool && (
                        <p className="text-xs text-red-500/70 dark:text-red-400/70 mt-1">
                            Tool: {tool}
                        </p>
                    )}
                </div>
                {onRetry && (
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={onRetry}
                        disabled={countdown > 0}
                        className="h-7 text-xs border-red-300 dark:border-red-700 hover:bg-red-100 dark:hover:bg-red-900/40"
                    >
                        {countdown > 0 ? `Retry in ${countdown}s` : "Retry"}
                    </Button>
                )}
            </div>
        </div>
    );
}

// Citation badge component - shows domain name with count, hover shows all citations
const CitationBadge = memo(function CitationBadge({ citations, sourceName, domain }: { citations: Citation[]; sourceName: string; domain: string }) {
    const count = citations.length;
    const firstCitation = citations[0];

    return (
        <HoverCard openDelay={200} closeDelay={100}>
            <HoverCardTrigger asChild>
                <button
                    type="button"
                    className="inline-flex items-center gap-1 px-2 py-0.5 mx-0.5 rounded-md bg-zinc-200/90 dark:bg-zinc-700/90 hover:bg-zinc-300 dark:hover:bg-zinc-600 text-xs text-zinc-600 dark:text-zinc-300 hover:text-zinc-800 dark:hover:text-zinc-100 transition-colors cursor-pointer align-middle"
                    onClick={(e) => {
                        e.preventDefault();
                        openExternalUrl(firstCitation.url);
                    }}
                >
                    <span className="font-medium">{sourceName}</span>
                    {count > 1 && <span className="text-muted-foreground">+{count - 1}</span>}
                </button>
            </HoverCardTrigger>
            <HoverCardContent
                side="top"
                align="start"
                className="w-[380px] p-0 shadow-lg border border-border/50"
                sideOffset={8}
            >
                {/* Header */}
                <div className="px-4 py-2 border-b">
                    <h3 className="text-xs font-medium text-muted-foreground">Citations ({count})</h3>
                </div>

                {/* Scrollable citations list */}
                <div className="max-h-[320px] overflow-y-auto">
                    <div className="divide-y">
                        {citations.map((citation, idx) => {
                            const displayTitle = getReadableTitle(citation.url, citation.title);
                            let description = "";
                            if (citation.title) {
                                try {
                                    const url = new URL(citation.url);
                                    const path = decodeURIComponent(url.pathname).replace(/\//g, ' ').replace(/-/g, ' ').trim();
                                    if (path && path.length > 5) {
                                        description = path.slice(0, 80) + (path.length > 80 ? '...' : '');
                                    }
                                } catch { /* ignore */ }
                            }

                            return (
                                <a
                                    key={idx}
                                    href={citation.url}
                                    className="block p-3 hover:bg-muted/50 transition-colors cursor-pointer"
                                    onClick={(e) => {
                                        e.preventDefault();
                                        openExternalUrl(citation.url);
                                    }}
                                >
                                    <div className="space-y-1.5">
                                        <div className="flex items-center gap-2">
                                            <div className="flex-shrink-0 h-5 w-5 rounded-full bg-muted flex items-center justify-center overflow-hidden">
                                                <NextImage
                                                    src={`https://www.google.com/s2/favicons?domain=${domain}&sz=32`}
                                                    alt={sourceName}
                                                    width={14}
                                                    height={14}
                                                    className="h-3.5 w-3.5"
                                                    unoptimized
                                                    onError={(e) => {
                                                        const parent = (e.target as HTMLImageElement).parentElement;
                                                        if (parent) {
                                                            (e.target as HTMLImageElement).style.display = 'none';
                                                            parent.innerHTML = `<span class="text-[10px] font-medium text-muted-foreground">${sourceName.charAt(0).toUpperCase()}</span>`;
                                                        }
                                                    }}
                                                />
                                            </div>
                                            <span className="text-xs text-muted-foreground">{sourceName}</span>
                                        </div>

                                        <h4 className="font-medium text-sm leading-snug line-clamp-2">
                                            {displayTitle}
                                        </h4>

                                        {description && (
                                            <p className="text-xs text-muted-foreground leading-relaxed line-clamp-2">
                                                {description}
                                            </p>
                                        )}
                                    </div>
                                </a>
                            );
                        })}
                    </div>
                </div>
            </HoverCardContent>
        </HoverCard>
    );
}, (prev, next) => {
    // Custom comparison to prevent re-renders when citations array reference changes but content is same
    if (prev.domain !== next.domain) return false;
    if (prev.sourceName !== next.sourceName) return false;
    if (prev.citations.length !== next.citations.length) return false;
    
    // Deep compare all citation URLs to ensure accuracy even if grouping looks same
    for (let i = 0; i < prev.citations.length; i++) {
        if (prev.citations[i].url !== next.citations[i].url) return false;
        // Optionally check title if it changes during streaming, though less critical
        if (prev.citations[i].title !== next.citations[i].title) return false;
    }
    
    return true;
});

// MessageItem component to isolate updates and memoize citations
const MessageItem = memo(function MessageItem({
    message,
    agentType,
    sessionTitle,
    copiedMessageId,
    onCopy
}: {
    message: any,
    agentType: string,
    sessionTitle?: string,
    copiedMessageId: string | null,
    onCopy: (id: string, content: string) => void
}) {
    // Use a ref to hold the citations array to ensure reference stability across renders
    const citationsRef = React.useRef<Citation[] | undefined>(undefined);
    
    // Memoize citations array with deep comparison to prevent unnecessary re-renders
    // of CitationBadge and MarkdownWithCitations when new array references come in but content is same
    const rawCitations = message.metadata?.citations ||
             (message.metadata?.artifacts?.citations
                 ? convertUrlsToCitations(message.metadata.artifacts.citations)
                 : undefined);

    if (!citationsRef.current && rawCitations) {
        citationsRef.current = rawCitations;
    } else if (rawCitations && citationsRef.current) {
        // Simple deep check: length and first/last URL
        // If length changed or first/last item changed, update ref
        const prev = citationsRef.current;
        const next = rawCitations;
        
        let changed = prev.length !== next.length;
        if (!changed && prev.length > 0) {
            // Check first and last for efficiency
            if (prev[0].url !== next[0].url) changed = true;
            if (prev[prev.length - 1].url !== next[next.length - 1].url) changed = true;
            
            // If still arguably same, check random middle one just in case?
            // Usually streaming appends or replaces all.
        }
        
        if (changed) {
            citationsRef.current = rawCitations;
        }
    } else if (!rawCitations && citationsRef.current) {
        citationsRef.current = undefined;
    }
    
    const citations = citationsRef.current;

    return (
        <div
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
                                    agentType === "deep_research" ? "bg-violet-100 dark:bg-violet-900/30" :
                                        agentType === "browser_use" ? "bg-blue-100 dark:bg-blue-900/30" : "bg-amber-100 dark:bg-amber-900/30"
                )}>
                    {message.role === "user" ? "U" :
                        message.role === "tool" ? "T" :
                            message.role === "system" ? (message.isError ? <AlertCircle className="h-4 w-4 text-red-500" /> : message.isCancelled ? <XCircle className="h-4 w-4 text-yellow-600" /> : "S") :
                                    agentType === "deep_research" ? <Microscope className="h-4 w-4 text-violet-500" /> :
                                        agentType === "browser_use" ? <Globe className="h-4 w-4 text-blue-500" /> : <Sparkles className="h-4 w-4 text-amber-500" />}
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
                    {message.metadata?.isSwarmInput && (
                        <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-600 dark:text-amber-400">→ Lead</span>
                    )}
                    <span className="text-xs text-muted-foreground">
                        {message.timestamp}
                    </span>
                </div>
                <div className="space-y-2 min-w-0 w-full">
                    <Card className={cn(
                        "px-2 sm:px-3 py-1 text-base prose prose-base max-w-none",
                        "prose-p:my-1 prose-p:leading-relaxed prose-p:text-current",
                        "prose-ul:my-1 prose-ol:my-1 prose-li:my-0 prose-li:text-current",
                        "prose-headings:mt-2 prose-headings:mb-1 prose-headings:leading-tight prose-headings:text-current",
                        "prose-strong:text-current prose-strong:font-semibold",
                        "prose-em:text-current",
                        "prose-a:text-current prose-code:text-current",
                        "break-words overflow-wrap-anywhere overflow-hidden",
                        message.role === "user" ? "bg-muted text-foreground" :
                            message.role === "tool" ? "bg-muted/50 font-mono text-xs prose-pre:bg-transparent" :
                                message.role === "system" ? (message.isError ? "bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border-red-200 dark:border-red-800" : message.isCancelled ? "bg-yellow-50 dark:bg-yellow-900/20 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-800" : "bg-gray-50 dark:bg-gray-900/20") :
                                    "bg-transparent border-none shadow-none dark:prose-invert"
                    )}>
                        {message.isGenerating ? (
                            <span className="flex items-center gap-1 text-muted-foreground">
                                <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: "0ms" }} />
                                <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: "150ms" }} />
                                <span className="inline-block w-1.5 h-1.5 bg-current rounded-full animate-bounce" style={{ animationDelay: "300ms" }} />
                            </span>
                        ) : message.isStreaming ? (
                            <pre className="whitespace-pre-wrap font-sans leading-relaxed break-words">
                                {typeof message.content === "string" ? message.content : String(message.content ?? "")}
                                <span className="inline-block w-2 h-4 ml-1 bg-current animate-pulse" />
                            </pre>
                        ) : (
                            // Check if content is a tool output that should be rendered with rich UI
                            (() => {
                                const contentStr = typeof message.content === "string" ? message.content : String(message.content ?? "");
                                const toolOutput = detectToolOutputType(contentStr);
                                if (toolOutput) {
                                    return <ToolOutputRenderer content={contentStr} />;
                                }
                                return (
                                    <MarkdownWithCitations
                                        content={message.content}
                                        citations={citations}
                                    />
                                );
                            })()
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
                                            onClick={() => onCopy(message.id, message.content)}
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

                            {/* Sources button - shows citations in a popup */}
                            {message.role === "assistant" && (
                                (citations?.length ?? 0) > 0
                            ) && (
                                    <SourcesButton
                                        citations={citations || []}
                                    />
                                )}
                        </div>
                    )}
                    {message.metadata?.usage && (
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            <span>{message.metadata.usage.total_tokens} tokens</span>
                            <span>•</span>
                            <span>
                                {message.metadata.model && getModelDisplayName(message.metadata.model)}
                                {message.metadata.provider && ` (${getProviderDisplayName(message.metadata.provider)})`}
                            </span>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
});

// Sources button component - shows favicons and opens citations popup
function SourcesButton({ citations }: { citations: Citation[] }) {
    if (!citations || citations.length === 0) return null;

    // Get unique domains for favicon display (max 5)
    const uniqueDomains = Array.from(new Set(
        citations.map(c => getDomainInfo(c.url, c.source).domain)
    )).slice(0, 5);

    return (
        <Popover>
            <PopoverTrigger asChild>
                <button className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md hover:bg-muted/50 transition-colors text-muted-foreground hover:text-foreground">
                    {/* Favicon stack */}
                    <div className="flex items-center -space-x-1">
                        {uniqueDomains.map((domain, i) => (
                            <div
                                key={domain}
                                className="h-5 w-5 rounded-full bg-muted border-2 border-background flex items-center justify-center overflow-hidden"
                                style={{ zIndex: uniqueDomains.length - i }}
                            >
                                <NextImage
                                    src={`https://www.google.com/s2/favicons?domain=${domain}&sz=32`}
                                    alt={domain}
                                    width={12}
                                    height={12}
                                    className="h-3 w-3"
                                    unoptimized
                                    onError={(e) => {
                                        (e.target as HTMLImageElement).style.display = 'none';
                                    }}
                                />
                            </div>
                        ))}
                    </div>
                    <span className="text-xs font-medium">{citations.length} sources</span>
                </button>
            </PopoverTrigger>
            <PopoverContent
                side="top"
                align="start"
                className="w-[380px] p-0"
                sideOffset={8}
            >
                {/* Header */}
                <div className="px-4 py-3 border-b">
                    <h3 className="text-sm font-medium text-muted-foreground">Citations</h3>
                </div>

                {/* Scrollable citations list */}
                <div className="max-h-[320px] overflow-y-auto">
                    <div className="divide-y">
                        {citations.map((citation, index) => {
                            const { domain, sourceName } = getDomainInfo(citation.url, citation.source);
                            const displayTitle = getReadableTitle(citation.url, citation.title);

                            // Generate description from URL path
                            let description = "";
                            if (citation.title) {
                                try {
                                    const url = new URL(citation.url);
                                    const path = decodeURIComponent(url.pathname).replace(/\//g, ' ').replace(/-/g, ' ').trim();
                                    if (path && path.length > 5) {
                                        description = path.slice(0, 80) + (path.length > 80 ? '...' : '');
                                    }
                                } catch {
                                    // ignore
                                }
                            }

                            return (
                                <a
                                    key={`source-${index}`}
                                    href={citation.url}
                                    className="block px-4 py-3 hover:bg-muted/50 transition-colors cursor-pointer"
                                    onClick={(e) => {
                                        e.preventDefault();
                                        openExternalUrl(citation.url);
                                    }}
                                >
                                    <div className="space-y-1.5">
                                        {/* Source with favicon */}
                                        <div className="flex items-center gap-2">
                                            <div className="flex-shrink-0 h-5 w-5 rounded-full bg-muted flex items-center justify-center overflow-hidden">
                                                <NextImage
                                                    src={`https://www.google.com/s2/favicons?domain=${domain}&sz=32`}
                                                    alt={sourceName}
                                                    width={14}
                                                    height={14}
                                                    className="h-3.5 w-3.5"
                                                    unoptimized
                                                    onError={(e) => {
                                                        const parent = (e.target as HTMLImageElement).parentElement;
                                                        if (parent) {
                                                            (e.target as HTMLImageElement).style.display = 'none';
                                                            parent.innerHTML = `<span class="text-[10px] font-medium text-muted-foreground">${sourceName.charAt(0).toUpperCase()}</span>`;
                                                        }
                                                    }}
                                                />
                                            </div>
                                            <span className="text-xs text-muted-foreground">{sourceName}</span>
                                        </div>

                                        {/* Title */}
                                        <h4 className="font-medium text-sm leading-snug line-clamp-2">
                                            {displayTitle}
                                        </h4>

                                        {/* Description */}
                                        {description && (
                                            <p className="text-xs text-muted-foreground leading-relaxed line-clamp-2">
                                                {description}
                                            </p>
                                        )}
                                    </div>
                                </a>
                            );
                        })}
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}

// Moved outside component to prevent recreation on every render
// These agents produce final output that should be displayed (not intermediate)
const directOutputAgents = ["simple-agent", "final_output", "swarm-lead"];

function isIntermediateMessage(msg: Message): boolean {
    if (msg.role !== "assistant") return false;
    const sender = msg.sender || "";

    // Check if it's a tool call message (JSON with selected_tools)
    if (msg.content && msg.content.trim().startsWith('{') && msg.content.includes('"selected_tools"')) {
        return true;
    }

    // Empty sender is treated as final output (API-fetched results, simple responses)
    if (!sender) return false;

    // If it's a direct output agent, it's not intermediate
    if (directOutputAgents.includes(sender)) return false;

    // Everything else is intermediate (synthesis, reasoner-*, actor-*, etc.)
    return true;
}

export const RunConversation = memo(function RunConversation({ messages, agentType = "normal", sessionTitle }: RunConversationProps) {
    const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null);
    const isIOS = useIsIOS();

    // Copy message content to clipboard
    const handleCopyMessage = async (messageId: string, content: string) => {
        try {
            await navigator.clipboard.writeText(content);
            setCopiedMessageId(messageId);
            setTimeout(() => setCopiedMessageId(null), 2000);
        } catch (err) {
            console.error("Failed to copy message:", err);
        }
    };

    // Memoize filtered messages to prevent unnecessary re-filtering
    const displayedMessages = useMemo(
        () => messages.filter(msg => !isIntermediateMessage(msg)),
        [messages]
    );

    return (
        <div className={cn("space-y-2 p-3 sm:p-4", isIOS && "ios-content")}>
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

                // Render screenshot messages with preview
                if (message.isScreenshot && (message.metadata?.screenshot || message.metadata?.screenshotPath)) {
                    return (
                        <div
                            key={message.id}
                            className="flex gap-2 sm:gap-3 animate-in fade-in-0 slide-in-from-bottom-2 duration-300"
                        >
                            <Avatar className="h-7 w-7 sm:h-8 sm:w-8 shrink-0">
                                <AvatarFallback className="bg-blue-100 dark:bg-blue-900/30">
                                    <Camera className="h-4 w-4 text-blue-500" />
                                </AvatarFallback>
                            </Avatar>
                            <div className="flex-1 max-w-[85%] sm:max-w-[80%]">
                                <div className="flex items-center gap-2 mb-1">
                                    <span className="text-xs font-medium text-muted-foreground">Screenshot</span>
                                    <span className="text-xs text-muted-foreground">{message.timestamp}</span>
                                </div>
                                <Card className="p-3">
                                    <ScreenshotPreview
                                        screenshot={message.metadata.screenshot}
                                        screenshotPath={message.metadata.screenshotPath}
                                        sessionId={message.metadata.sessionId}
                                        pageUrl={message.metadata.pageUrl}
                                        pageTitle={message.metadata.pageTitle}
                                    />
                                </Card>
                            </div>
                        </div>
                    );
                }

                // Render browser error messages with retry option
                if (message.isBrowserError && message.isError) {
                    return (
                        <div
                            key={message.id}
                            className="flex gap-2 sm:gap-3 animate-in fade-in-0 slide-in-from-bottom-2 duration-300"
                        >
                            <Avatar className="h-7 w-7 sm:h-8 sm:w-8 shrink-0">
                                <AvatarFallback className="bg-red-100 dark:bg-red-900/30">
                                    <AlertCircle className="h-4 w-4 text-red-500" />
                                </AvatarFallback>
                            </Avatar>
                            <div className="flex-1 max-w-[85%] sm:max-w-[80%]">
                                <div className="flex items-center gap-2 mb-1">
                                    <span className="text-xs font-medium text-muted-foreground">Browser Error</span>
                                    <span className="text-xs text-muted-foreground">{message.timestamp}</span>
                                </div>
                                <BrowserErrorMessage
                                    error={message.content}
                                    tool={message.metadata?.tool}
                                    retryAfterSeconds={message.metadata?.retryAfterSeconds}
                                />
                            </div>
                        </div>
                    );
                }

                // Render user and final assistant messages normally
                return (
                    <MessageItem
                        key={message.id}
                        message={message}
                        agentType={agentType}
                        sessionTitle={sessionTitle}
                        copiedMessageId={copiedMessageId}
                        onCopy={handleCopyMessage}
                    />
                );
            })}
        </div>
    );
});

RunConversation.displayName = "RunConversation";
