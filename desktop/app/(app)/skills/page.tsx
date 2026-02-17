"use client";

import { useEffect, useState, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { useDispatch } from "react-redux";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
    Search,
    Loader2,
    RefreshCw,
    Wand2,
    Shield,
    ShieldAlert,
    Wrench,
    ChevronDown,
    ChevronRight,
    XCircle,
    Play,
} from "lucide-react";
import {
    listSkills,
    getSkill,
    SkillSummary,
    SkillDetail,
} from "@/lib/shannon/api";
import { setSelectedSkill } from "@/lib/features/runSlice";

function normalizeDetail(d: SkillDetail) {
    return {
        name: d.name ?? "",
        version: d.version ?? "",
        author: d.author,
        category: d.category ?? "",
        description: d.description ?? "",
        requires_tools: d.requires_tools,
        requires_role: d.requires_role,
        budget_max: d.budget_max,
        dangerous: d.dangerous ?? false,
        enabled: d.enabled ?? false,
        metadata: d.metadata,
        content: d.content,
    };
}

export default function SkillsPage() {
    const [skills, setSkills] = useState<SkillSummary[]>([]);
    const [categories, setCategories] = useState<string[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState("");
    const [activeCategory, setActiveCategory] = useState<string | null>(null);
    const [expandedSkill, setExpandedSkill] = useState<string | null>(null);
    const [skillDetail, setSkillDetail] = useState<SkillDetail | null>(null);
    const [isLoadingDetail, setIsLoadingDetail] = useState(false);

    const dispatch = useDispatch();
    const router = useRouter();

    const fetchSkills = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await listSkills(activeCategory || undefined);
            setSkills(data.skills || []);
            if (!activeCategory) {
                setCategories(data.categories || []);
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load skills");
        } finally {
            setIsLoading(false);
        }
    }, [activeCategory]);

    useEffect(() => {
        fetchSkills();
    }, [fetchSkills]);

    const expandRequestRef = useRef(0);

    const handleExpand = async (skillName: string) => {
        if (expandedSkill === skillName) {
            setExpandedSkill(null);
            setSkillDetail(null);
            return;
        }

        const requestId = ++expandRequestRef.current;
        setExpandedSkill(skillName);
        setSkillDetail(null);
        setIsLoadingDetail(true);
        try {
            const data = await getSkill(skillName);
            if (expandRequestRef.current !== requestId) return; // stale response
            setSkillDetail(data.skill);
        } catch {
            if (expandRequestRef.current !== requestId) return;
            setSkillDetail(null);
        } finally {
            if (expandRequestRef.current === requestId) {
                setIsLoadingDetail(false);
            }
        }
    };

    const handleUseSkill = (skillName: string) => {
        dispatch(setSelectedSkill(skillName));
        router.push("/run-detail?session_id=new");
    };

    const filteredSkills = skills.filter((skill) => {
        const q = searchQuery.toLowerCase();
        return (
            (skill.name || "").toLowerCase().includes(q) ||
            (skill.description || "").toLowerCase().includes(q) ||
            (skill.category || "").toLowerCase().includes(q)
        );
    });

    return (
        <div className="h-full overflow-y-auto p-4 sm:p-8 space-y-6 sm:space-y-8">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl sm:text-3xl font-bold tracking-tight">Skills</h1>
                    <p className="text-muted-foreground text-sm sm:text-base">
                        Browse and invoke reusable agent skills.
                    </p>
                </div>
                <Button
                    variant="outline"
                    size="sm"
                    onClick={fetchSkills}
                    disabled={isLoading}
                >
                    <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
                    <span className="hidden sm:inline ml-2">Refresh</span>
                </Button>
            </div>

            {/* Search + category filters */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center gap-4">
                <div className="relative flex-1 max-w-sm">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input
                        type="search"
                        placeholder="Search skills..."
                        className="pl-8"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                    />
                </div>
                {categories.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                        <Button
                            variant={activeCategory === null ? "default" : "outline"}
                            size="sm"
                            onClick={() => setActiveCategory(null)}
                        >
                            All
                        </Button>
                        {categories.map((cat) => (
                            <Button
                                key={cat}
                                variant={activeCategory === cat ? "default" : "outline"}
                                size="sm"
                                onClick={() => setActiveCategory(cat)}
                            >
                                {cat}
                            </Button>
                        ))}
                    </div>
                )}
            </div>

            {error && (
                <div className="rounded-lg border border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-950/50 p-4">
                    <div className="flex items-start justify-between gap-4">
                        <p className="text-sm text-red-800 dark:text-red-200">{error}</p>
                        <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0 text-red-600 hover:text-red-800 hover:bg-red-100 dark:text-red-400 dark:hover:bg-red-900/50"
                            onClick={() => setError(null)}
                        >
                            <XCircle className="h-4 w-4" />
                        </Button>
                    </div>
                    <Button
                        variant="outline"
                        size="sm"
                        className="mt-2"
                        onClick={() => {
                            setError(null);
                            fetchSkills();
                        }}
                    >
                        Retry
                    </Button>
                </div>
            )}

            {isLoading ? (
                <div className="flex items-center justify-center py-12">
                    <Loader2 className="h-8 w-8 animate-spin text-primary" />
                </div>
            ) : filteredSkills.length === 0 ? (
                <div className="text-center py-12 text-muted-foreground">
                    {searchQuery || activeCategory
                        ? "No skills match your search."
                        : "No skills available. Add skill YAML files to the skills directory."}
                </div>
            ) : (
                <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    {filteredSkills.map((skill) => {
                        const isExpanded = expandedSkill === skill.name;
                        const toolCount = skill.requires_tools?.length || 0;

                        return (
                            <Card
                                key={skill.name}
                                className={`transition-shadow hover:shadow-md ${isExpanded ? "sm:col-span-2 lg:col-span-3" : ""}`}
                            >
                                <CardHeader
                                    className="cursor-pointer"
                                    onClick={() => handleExpand(skill.name)}
                                >
                                    <div className="flex items-start justify-between gap-2">
                                        <div className="flex items-center gap-2 min-w-0">
                                            <Wand2 className="h-5 w-5 text-primary shrink-0" />
                                            <CardTitle className="text-base truncate">
                                                {skill.name}
                                            </CardTitle>
                                        </div>
                                        <div className="flex items-center gap-1.5 shrink-0">
                                            {isExpanded ? (
                                                <ChevronDown className="h-4 w-4 text-muted-foreground" />
                                            ) : (
                                                <ChevronRight className="h-4 w-4 text-muted-foreground" />
                                            )}
                                        </div>
                                    </div>
                                    <CardDescription className="line-clamp-2">
                                        {skill.description}
                                    </CardDescription>
                                    <div className="flex flex-wrap items-center gap-2 mt-2">
                                        <Badge variant="secondary" className="text-xs">
                                            v{skill.version}
                                        </Badge>
                                        <Badge variant="outline" className="text-xs">
                                            {skill.category}
                                        </Badge>
                                        {toolCount > 0 && (
                                            <Badge variant="outline" className="text-xs">
                                                <Wrench className="h-3 w-3 mr-1" />
                                                {toolCount} tool{toolCount !== 1 ? "s" : ""}
                                            </Badge>
                                        )}
                                        {skill.dangerous && (
                                            <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30 text-xs">
                                                <ShieldAlert className="h-3 w-3 mr-1" />
                                                Dangerous
                                            </Badge>
                                        )}
                                        {!skill.enabled && (
                                            <Badge variant="secondary" className="text-xs opacity-60">
                                                Disabled
                                            </Badge>
                                        )}
                                        {skill.enabled && (
                                            <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-xs">
                                                <Shield className="h-3 w-3 mr-1" />
                                                Enabled
                                            </Badge>
                                        )}
                                    </div>
                                </CardHeader>

                                {isExpanded && (
                                    <CardContent className="border-t pt-4 space-y-4">
                                        {isLoadingDetail ? (
                                            <div className="flex items-center justify-center py-4">
                                                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                                            </div>
                                        ) : skillDetail ? (() => {
                                            const detail = normalizeDetail(skillDetail);
                                            return (
                                            <>
                                                <div className="grid gap-3 text-sm sm:grid-cols-2">
                                                    {detail.requires_role && (
                                                        <div>
                                                            <span className="text-muted-foreground">Role:</span>{" "}
                                                            <span className="font-medium">{detail.requires_role}</span>
                                                        </div>
                                                    )}
                                                    {detail.budget_max !== undefined && detail.budget_max > 0 && (
                                                        <div>
                                                            <span className="text-muted-foreground">Budget max:</span>{" "}
                                                            <span className="font-medium">{detail.budget_max} tokens</span>
                                                        </div>
                                                    )}
                                                    {detail.author && (
                                                        <div>
                                                            <span className="text-muted-foreground">Author:</span>{" "}
                                                            <span className="font-medium">{detail.author}</span>
                                                        </div>
                                                    )}
                                                    {detail.requires_tools && detail.requires_tools.length > 0 && (
                                                        <div className="sm:col-span-2">
                                                            <span className="text-muted-foreground">Required tools:</span>{" "}
                                                            <span className="font-mono text-xs">
                                                                {detail.requires_tools.join(", ")}
                                                            </span>
                                                        </div>
                                                    )}
                                                    {detail.metadata && Object.keys(detail.metadata).length > 0 && (
                                                        <div className="sm:col-span-2">
                                                            <span className="text-muted-foreground">Metadata:</span>{" "}
                                                            <code className="text-xs bg-muted px-1 py-0.5 rounded">
                                                                {JSON.stringify(detail.metadata)}
                                                            </code>
                                                        </div>
                                                    )}
                                                </div>

                                                {detail.content && (
                                                    <div className="rounded-md border bg-muted/50 p-4 max-h-[300px] overflow-auto">
                                                        <pre className="text-sm whitespace-pre-wrap font-mono">
                                                            {detail.content}
                                                        </pre>
                                                    </div>
                                                )}

                                                <div className="flex justify-end">
                                                    <Button
                                                        onClick={() => handleUseSkill(skill.name)}
                                                        disabled={!skill.enabled}
                                                    >
                                                        <Play className="h-4 w-4 mr-2" />
                                                        Use this skill
                                                    </Button>
                                                </div>
                                            </>
                                            );
                                        })() : (
                                            <p className="text-sm text-muted-foreground">
                                                Failed to load skill details.
                                            </p>
                                        )}
                                    </CardContent>
                                )}
                            </Card>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
