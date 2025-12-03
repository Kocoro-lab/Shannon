"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface CollapsibleMessageProps {
    sender: string;
    content: string;
    timestamp?: string;
}

export function CollapsibleMessage({ sender, content, timestamp }: CollapsibleMessageProps) {
    const [isExpanded, setIsExpanded] = useState(false);
    
    // Extract first 2 lines or ~120 characters for preview (plain text)
    const getPreview = () => {
        // Remove markdown syntax for preview
        const plainText = content
            .replace(/#{1,6}\s+/g, '') // Remove headers
            .replace(/\[(\d+)\]/g, '') // Remove citation markers for cleaner preview
            .replace(/\*\*/g, '') // Remove bold
            .replace(/\*/g, ''); // Remove italic
        
        const lines = plainText.split('\n').filter(line => line.trim());
        const preview = lines.slice(0, 2).join(' ');
        return preview.length > 120 ? preview.substring(0, 120) + '...' : preview + (lines.length > 2 ? '...' : '');
    };

    const agentLabel = sender || 'agent';
    const preview = getPreview();

    return (
        <Card className="p-3 my-2 bg-muted/30 border-muted">
            <div className="flex items-start gap-2">
                <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0 shrink-0"
                    onClick={() => setIsExpanded(!isExpanded)}
                >
                    {isExpanded ? (
                        <ChevronDown className="h-4 w-4" />
                    ) : (
                        <ChevronRight className="h-4 w-4" />
                    )}
                </Button>
                
                <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                        <Badge variant="outline" className="text-xs font-mono">
                            {agentLabel}
                        </Badge>
                        {timestamp && (
                            <span className="text-xs text-muted-foreground">
                                {timestamp}
                            </span>
                        )}
                    </div>
                    
                    {isExpanded ? (
                        <div className="text-sm whitespace-pre-wrap break-words">
                            {content}
                        </div>
                    ) : (
                        <div className="text-sm text-muted-foreground line-clamp-2">
                            {preview}
                        </div>
                    )}
                </div>
            </div>
        </Card>
    );
}

