/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { listWorkspaceFiles, getWorkspaceFileContent, WorkspaceFileInfo } from "@/lib/shannon/api";
import { RefreshCw, ChevronRight, ChevronDown, FileText, Folder, FolderOpen, X, Loader2, Download } from "lucide-react";
import { isTauri, saveFileDialog } from "@/lib/tauri";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";

const MAX_FILE_CONTENT_LENGTH = 50000;

interface WorkspacePanelProps {
    sessionId: string;
    workspaceUpdateSeq: number;
}

interface TreeNode {
    name: string;
    path: string;
    isDir: boolean;
    sizeBytes: number;
    children: TreeNode[];
}

function buildFileTree(files: WorkspaceFileInfo[]): TreeNode[] {
    const root: TreeNode[] = [];
    const dirMap = new Map<string, TreeNode>();

    // Sort: directories first, then alphabetically
    const sorted = [...files].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
        return a.name.localeCompare(b.name);
    });

    for (const f of sorted) {
        const node: TreeNode = {
            name: f.name,
            path: f.path,
            isDir: f.is_dir,
            sizeBytes: f.size_bytes,
            children: [],
        };
        if (f.is_dir) {
            dirMap.set(f.path, node);
        }
        root.push(node);
    }

    return root;
}

function formatFileSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const IMAGE_EXTENSIONS = new Set(["png", "jpg", "jpeg", "gif", "webp", "svg"]);

function isImage(path: string): boolean {
    const ext = path.split(".").pop()?.toLowerCase();
    return !!ext && IMAGE_EXTENSIONS.has(ext);
}

function getFileIcon(name: string): string {
    const ext = name.split(".").pop()?.toLowerCase();
    if (ext && IMAGE_EXTENSIONS.has(ext)) return "🖼️";
    switch (ext) {
        case "md": return "📝";
        case "json": return "📋";
        case "yaml": case "yml": return "⚙️";
        case "py": return "🐍";
        case "go": return "🔵";
        case "rs": return "🦀";
        case "js": case "ts": return "📜";
        case "csv": return "📊";
        case "txt": case "log": return "📄";
        case "html": return "🌐";
        default: return "📄";
    }
}

function isMarkdown(path: string): boolean {
    return path.endsWith(".md") || path.endsWith(".markdown");
}

function isJSON(path: string): boolean {
    return path.endsWith(".json");
}

// Directory tree item with collapsible children
function DirNode({
    node,
    sessionId,
    onSelectFile,
    selectedPath,
    loadedDirs,
    onExpandDir,
}: {
    node: TreeNode;
    sessionId: string;
    onSelectFile: (path: string) => void;
    selectedPath: string | null;
    loadedDirs: Map<string, WorkspaceFileInfo[]>;
    onExpandDir: (dirPath: string) => void;
}) {
    const [isOpen, setIsOpen] = useState(false);

    const childFiles = loadedDirs.get(node.path);
    const childNodes = useMemo(() => {
        if (!childFiles) return [];
        return buildFileTree(childFiles);
    }, [childFiles]);

    const handleToggle = useCallback((open: boolean) => {
        setIsOpen(open);
        if (open && !loadedDirs.has(node.path)) {
            onExpandDir(node.path);
        }
    }, [node.path, loadedDirs, onExpandDir]);

    return (
        <Collapsible open={isOpen} onOpenChange={handleToggle}>
            <CollapsibleTrigger className="flex items-center gap-1.5 w-full px-2 py-1 text-sm hover:bg-muted/50 rounded-sm cursor-pointer text-left">
                {isOpen ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
                {isOpen ? <FolderOpen className="h-3.5 w-3.5 shrink-0 text-amber-500" /> : <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500" />}
                <span className="truncate">{node.name}/</span>
            </CollapsibleTrigger>
            <CollapsibleContent>
                <div className="ml-4 border-l border-border/50 pl-1">
                    {!childFiles ? (
                        <div className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground">
                            <Loader2 className="h-3 w-3 animate-spin" /> Loading...
                        </div>
                    ) : childNodes.length === 0 ? (
                        <div className="px-2 py-1 text-xs text-muted-foreground italic">Empty</div>
                    ) : (
                        childNodes.map((child) =>
                            child.isDir ? (
                                <DirNode
                                    key={child.path}
                                    node={child}
                                    sessionId={sessionId}
                                    onSelectFile={onSelectFile}
                                    selectedPath={selectedPath}
                                    loadedDirs={loadedDirs}
                                    onExpandDir={onExpandDir}
                                />
                            ) : (
                                <FileNode
                                    key={child.path}
                                    node={child}
                                    onSelectFile={onSelectFile}
                                    isSelected={selectedPath === child.path}
                                />
                            )
                        )
                    )}
                </div>
            </CollapsibleContent>
        </Collapsible>
    );
}

function FileNode({
    node,
    onSelectFile,
    isSelected,
}: {
    node: TreeNode;
    onSelectFile: (path: string) => void;
    isSelected: boolean;
}) {
    return (
        <button
            onClick={() => onSelectFile(node.path)}
            className={`flex items-center gap-1.5 w-full px-2 py-1 text-sm hover:bg-muted/50 rounded-sm cursor-pointer text-left ${isSelected ? "bg-muted" : ""}`}
        >
            <span className="shrink-0 text-xs">{getFileIcon(node.name)}</span>
            <span className="truncate flex-1">{node.name}</span>
            <span className="text-xs text-muted-foreground shrink-0">{formatFileSize(node.sizeBytes)}</span>
        </button>
    );
}

export function WorkspacePanel({ sessionId, workspaceUpdateSeq }: WorkspacePanelProps) {
    const [rootFiles, setRootFiles] = useState<WorkspaceFileInfo[]>([]);
    const [loadedDirs, setLoadedDirs] = useState<Map<string, WorkspaceFileInfo[]>>(new Map());
    const [isLoading, setIsLoading] = useState(false);
    const [selectedFile, setSelectedFile] = useState<string | null>(null);
    const [fileContent, setFileContent] = useState<string | null>(null);
    const [fileContentType, setFileContentType] = useState<string | null>(null);
    const [isLoadingContent, setIsLoadingContent] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Cache loaded file contents
    const contentCache = useRef<Map<string, { content: string; contentType: string }>>(new Map());

    // Debounce timer ref
    const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Load root files
    const loadRootFiles = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const resp = await listWorkspaceFiles(sessionId);
            if (resp.success) {
                setRootFiles(resp.files);
            } else {
                setError(resp.error || "Failed to list files");
            }
        } catch (e: any) {
            // Don't show error for common cases (session not found, etc.)
            if (e?.status !== 404) {
                setError(e?.message || "Failed to list files");
            }
            setRootFiles([]);
        } finally {
            setIsLoading(false);
        }
    }, [sessionId]);

    // Debounced refresh on workspaceUpdateSeq change
    useEffect(() => {
        if (debounceTimer.current) {
            clearTimeout(debounceTimer.current);
        }
        debounceTimer.current = setTimeout(() => {
            loadRootFiles();
        }, workspaceUpdateSeq === 0 ? 0 : 500);

        return () => {
            if (debounceTimer.current) {
                clearTimeout(debounceTimer.current);
            }
        };
    }, [workspaceUpdateSeq, loadRootFiles]);

    // Expand directory
    const handleExpandDir = useCallback(async (dirPath: string) => {
        try {
            const resp = await listWorkspaceFiles(sessionId, dirPath);
            if (resp.success) {
                setLoadedDirs((prev) => {
                    const next = new Map(prev);
                    next.set(dirPath, resp.files);
                    return next;
                });
            }
        } catch {
            // Silently fail for directory expansion
        }
    }, [sessionId]);

    // Select file and load content
    const handleSelectFile = useCallback(async (filePath: string) => {
        setSelectedFile(filePath);

        // Check cache first
        const cached = contentCache.current.get(filePath);
        if (cached) {
            setFileContent(cached.content);
            setFileContentType(cached.contentType);
            return;
        }

        setIsLoadingContent(true);
        setFileContent(null);
        setFileContentType(null);
        try {
            const resp = await getWorkspaceFileContent(sessionId, filePath);
            if (resp.success && resp.content !== undefined) {
                let content = resp.content;
                const contentType = resp.content_type || "text/plain";

                // Truncate large text files (skip binary/image content — truncating base64 corrupts it)
                const isBinary = isImage(filePath) || contentType.startsWith("image/") || contentType === "application/octet-stream";
                if (!isBinary && content.length > MAX_FILE_CONTENT_LENGTH) {
                    content = content.substring(0, MAX_FILE_CONTENT_LENGTH) + "\n\n--- Truncated (file too large to display fully) ---";
                }

                setFileContent(content);
                setFileContentType(contentType);
                contentCache.current.set(filePath, { content, contentType });
            } else {
                setFileContent(resp.error || "Failed to load file content");
                setFileContentType("text/plain");
            }
        } catch (e: any) {
            setFileContent(e?.message || "Failed to load file");
            setFileContentType("text/plain");
        } finally {
            setIsLoadingContent(false);
        }
    }, [sessionId]);

    // Download the currently selected file
    const handleDownload = useCallback(async () => {
        if (!selectedFile || !fileContent) return;
        const fileName = selectedFile.split("/").pop() || selectedFile;
        const isBinary = isImage(selectedFile) || fileContentType?.startsWith("image/") || fileContentType === "application/octet-stream";

        if (isTauri()) {
            const ext = fileName.split(".").pop() || "*";
            const content = isBinary
                ? Uint8Array.from(atob(fileContent), c => c.charCodeAt(0))
                : fileContent;
            await saveFileDialog(content, fileName, [{ name: "File", extensions: [ext] }]);
        } else {
            // Web: create blob and trigger download
            const blob = isBinary
                ? new Blob([Uint8Array.from(atob(fileContent), c => c.charCodeAt(0))])
                : new Blob([fileContent], { type: fileContentType || "text/plain" });
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = fileName;
            a.click();
            URL.revokeObjectURL(url);
        }
    }, [selectedFile, fileContent, fileContentType]);

    const rootTree = useMemo(() => buildFileTree(rootFiles), [rootFiles]);

    const hasFiles = rootTree.length > 0;

    return (
        <div className="flex flex-col h-full pr-0.5">
            {/* Header */}
            <div className="p-4 flex items-center justify-between gap-2 shrink-0">
                <div className="font-medium text-sm text-muted-foreground uppercase tracking-wider">
                    Workspace Files
                </div>
                <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6"
                    onClick={loadRootFiles}
                    disabled={isLoading}
                >
                    <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
                </Button>
            </div>

            {/* File tree */}
            <div className={`${selectedFile ? "max-h-[40%]" : "flex-1"} min-h-0`}>
                <ScrollArea className="h-full">
                    <div className="px-2 pb-2">
                        {error && (
                            <div className="px-2 py-1 text-xs text-destructive">{error}</div>
                        )}
                        {isLoading && rootTree.length === 0 ? (
                            <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
                                <Loader2 className="h-4 w-4 animate-spin mr-2" /> Loading files...
                            </div>
                        ) : !hasFiles ? (
                            <div className="text-center py-8 text-sm text-muted-foreground">
                                <FileText className="h-8 w-8 mx-auto mb-2 opacity-50" />
                                <p>No workspace files yet</p>
                                <p className="text-xs mt-1">Files will appear as agents create them</p>
                            </div>
                        ) : (
                            rootTree.map((node) =>
                                node.isDir ? (
                                    <DirNode
                                        key={node.path}
                                        node={node}
                                        sessionId={sessionId}
                                        onSelectFile={handleSelectFile}
                                        selectedPath={selectedFile}
                                        loadedDirs={loadedDirs}
                                        onExpandDir={handleExpandDir}
                                    />
                                ) : (
                                    <FileNode
                                        key={node.path}
                                        node={node}
                                        onSelectFile={handleSelectFile}
                                        isSelected={selectedFile === node.path}
                                    />
                                )
                            )
                        )}
                    </div>
                </ScrollArea>
            </div>

            {/* File content preview */}
            {selectedFile && (
                <>
                    <div className="border-t px-3 py-2 flex items-center justify-between shrink-0 bg-muted/30">
                        <span className="text-xs font-mono text-muted-foreground truncate">{selectedFile}</span>
                        <div className="flex items-center gap-0.5 shrink-0">
                            <Button
                                variant="ghost"
                                size="icon"
                                className="h-5 w-5"
                                onClick={handleDownload}
                                disabled={!fileContent || isLoadingContent}
                                title="Download file"
                            >
                                <Download className="h-3 w-3" />
                            </Button>
                            <Button
                                variant="ghost"
                                size="icon"
                                className="h-5 w-5"
                                onClick={() => {
                                    setSelectedFile(null);
                                    setFileContent(null);
                                    setFileContentType(null);
                                }}
                            >
                                <X className="h-3 w-3" />
                            </Button>
                        </div>
                    </div>
                    <div className="flex-1 min-h-0">
                        <ScrollArea className="h-full">
                            <div className="p-3">
                                {isLoadingContent ? (
                                    <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
                                        <Loader2 className="h-4 w-4 animate-spin mr-2" /> Loading file...
                                    </div>
                                ) : fileContent !== null ? (
                                    <FileContentRenderer
                                        content={fileContent}
                                        filePath={selectedFile}
                                        contentType={fileContentType}
                                    />
                                ) : null}
                            </div>
                        </ScrollArea>
                    </div>
                </>
            )}
        </div>
    );
}

function FileContentRenderer({
    content,
    filePath,
    contentType,
}: {
    content: string;
    filePath: string;
    contentType: string | null;
}) {
    if (isImage(filePath) || contentType?.startsWith("image/")) {
        const mimeType = contentType || "image/png";
        return (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img
                src={`data:${mimeType};base64,${content}`}
                alt={filePath}
                className="max-w-full rounded"
            />
        );
    }

    if (isMarkdown(filePath)) {
        return (
            <div className="prose prose-sm dark:prose-invert max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
                    {content}
                </ReactMarkdown>
            </div>
        );
    }

    if (isJSON(filePath)) {
        try {
            const formatted = JSON.stringify(JSON.parse(content), null, 2);
            return (
                <pre className="text-xs font-mono whitespace-pre-wrap break-all bg-muted/30 p-2 rounded-md overflow-x-auto">
                    {formatted}
                </pre>
            );
        } catch {
            // Not valid JSON, fall through to plain text
        }
    }

    return (
        <pre className="text-xs font-mono whitespace-pre-wrap break-all">
            {content}
        </pre>
    );
}
