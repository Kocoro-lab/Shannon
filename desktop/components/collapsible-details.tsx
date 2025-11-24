import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

interface CollapsibleDetailsProps {
    content: string;
    type: "json" | "text";
}

export function CollapsibleDetails({ content, type }: CollapsibleDetailsProps) {
    const [isExpanded, setIsExpanded] = useState(false);

    // Generate preview (first 60 chars)
    const preview = content.length > 60 ? content.substring(0, 60) + "..." : content;

    // Format JSON with syntax highlighting
    const formatJson = (jsonString: string) => {
        try {
            const parsed = JSON.parse(jsonString);
            const formatted = JSON.stringify(parsed, null, 2);

            // Simple syntax highlighting using spans
            return formatted
                .split('\n')
                .map((line, i) => {
                    // Highlight keys (text before colon)
                    let highlighted = line.replace(
                        /"([^"]+)":/g,
                        '<span class="text-blue-400">"$1"</span>:'
                    );
                    // Highlight string values
                    highlighted = highlighted.replace(
                        /: "([^"]*)"/g,
                        ': <span class="text-green-400">"$1"</span>'
                    );
                    // Highlight numbers
                    highlighted = highlighted.replace(
                        /: (\d+)/g,
                        ': <span class="text-orange-400">$1</span>'
                    );
                    // Highlight booleans and null
                    highlighted = highlighted.replace(
                        /: (true|false|null)/g,
                        ': <span class="text-purple-400">$1</span>'
                    );

                    return `<div key="${i}">${highlighted}</div>`;
                })
                .join('');
        } catch {
            // If parsing fails, return as plain text
            return content;
        }
    };

    return (
        <div className="mt-2">
            <button
                onClick={() => setIsExpanded(!isExpanded)}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
                {isExpanded ? (
                    <ChevronDown className="h-3 w-3" />
                ) : (
                    <ChevronRight className="h-3 w-3" />
                )}
                <span className="font-medium">
                    {isExpanded ? "Hide details" : "Show details"}
                </span>
            </button>

            {isExpanded ? (
                <div className={cn(
                    "mt-2 rounded-md border bg-muted/30 py-3 pl-3 pr-4 text-xs w-full",
                    type === "json" ? "font-mono" : ""
                )}>
                    {type === "json" ? (
                        <div
                            className="whitespace-pre-wrap break-words overflow-wrap-anywhere"
                            dangerouslySetInnerHTML={{ __html: formatJson(content) }}
                        />
                    ) : (
                        <div className="whitespace-pre-wrap break-words overflow-wrap-anywhere text-muted-foreground">
                            {content}
                        </div>
                    )}
                </div>
            ) : (
                <div className="mt-1 text-xs text-muted-foreground/60 italic truncate">
                    {preview}
                </div>
            )}
        </div>
    );
}
