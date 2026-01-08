"use client";

import { useMemo, useState } from "react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { cn, openExternalUrl } from "@/lib/utils";
import type { Citation } from "@/lib/shannon/citations";
import { Check, Copy, ExternalLink, Search } from "lucide-react";

type MessageWithCitations = {
  metadata?: {
    citations?: Citation[];
  };
};

type SourceItem = Citation & {
  index: number; // 1-based display index
  titleDisplay: string;
  domain: string | null;
  dateDisplay: string | null;
  groupKey: string;
};

function safeHostname(url: string): string | null {
  try {
    const u = new URL(url);
    return u.hostname || null;
  } catch {
    return null;
  }
}

function safeDateDisplay(iso?: string): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleDateString();
}

function normalizeUrl(url: string): string {
  // Most citations should already be https://...; if not, keep as-is for copy,
  // but try to open with a best-effort scheme.
  if (/^https?:\/\//i.test(url)) return url;
  return `https://${url}`;
}

export function SourcesPanel({ messages }: { messages: readonly MessageWithCitations[] }) {
  const [query, setQuery] = useState("");
  const [copiedUrl, setCopiedUrl] = useState<string | null>(null);

  const { items, relatedMap } = useMemo(() => {
    const seen = new Set<string>();
    const citations: Citation[] = [];

    for (const m of messages) {
      const list = m?.metadata?.citations;
      if (!list || list.length === 0) continue;
      for (const c of list) {
        if (!c?.url) continue;
        if (seen.has(c.url)) continue;
        seen.add(c.url);
        citations.push(c);
      }
    }

    const derived: SourceItem[] = citations.map((c, i) => {
      const domain = safeHostname(c.url);
      const titleDisplay = c.title?.trim() || c.source?.trim() || c.url;
      const dateDisplay = safeDateDisplay(c.published_date) ?? safeDateDisplay(c.retrieved_at);
      const groupKey = (c.source?.trim() || domain || "unknown").toLowerCase();
      return {
        ...c,
        index: i + 1,
        titleDisplay,
        domain,
        dateDisplay,
        groupKey,
      };
    });

    const map = new Map<string, SourceItem[]>();
    for (const it of derived) {
      const arr = map.get(it.groupKey);
      if (arr) arr.push(it);
      else map.set(it.groupKey, [it]);
    }

    return { items: derived, relatedMap: map };
  }, [messages]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => {
      const hay = [
        it.titleDisplay,
        it.url,
        it.domain ?? "",
        it.source ?? "",
        it.source_type ?? "",
        it.dateDisplay ?? "",
        it.groupKey,
      ]
        .join(" ")
        .toLowerCase();
      return hay.includes(q);
    });
  }, [items, query]);

  const handleCopy = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url);
      setCopiedUrl(url);
      window.setTimeout(() => setCopiedUrl(null), 1500);
    } catch {
      // ignore
    }
  };

  const handleCopyView = async () => {
    try {
      await navigator.clipboard.writeText(window.location.href);
    } catch {
      // ignore
    }
  };

  return (
    <Card className="p-4 sm:p-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-lg font-bold tracking-tight">Sources</h2>
          <p className="text-sm text-muted-foreground max-w-[72ch]">
            Scan citations quickly. Filter by title, domain, date, or source group. Copy/open links without losing your place.
          </p>
        </div>

        <div className="flex flex-wrap gap-2 items-center justify-start sm:justify-end">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter sources…"
              className="h-9 w-[min(360px,70vw)] rounded-full pl-9"
            />
          </div>

          <Button type="button" variant="outline" size="sm" className="rounded-full" onClick={handleCopyView}>
            Copy view
            <Copy className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <div className="mt-4">
        {filtered.length === 0 ? (
          <div className="bg-muted/30 rounded-2xl p-6 text-center text-sm text-muted-foreground">
            No sources match your filter.
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {filtered.map((it) => {
              const group = relatedMap.get(it.groupKey) ?? [];
              const hasRelated = group.length > 1;
              const openUrl = normalizeUrl(it.url);
              const subtitle = it.source?.trim() || it.domain || "Unknown source";

              return (
                <article
                  key={it.url}
                  className="bg-muted/30 rounded-2xl p-3 sm:p-4 shadow-sm"
                >
                  <div className="flex gap-3">
                    <div className="shrink-0">
                      <div className="h-9 w-9 rounded-xl bg-accent/20 text-accent-foreground grid place-items-center font-semibold text-sm">
                        {it.index}
                      </div>
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex gap-2 items-start justify-between">
                        <div className="min-w-0">
                          <div className="text-sm font-semibold leading-snug line-clamp-2">
                            <a
                              href={openUrl}
                              className="hover:underline"
                              onClick={(e) => {
                                e.preventDefault();
                                openExternalUrl(openUrl);
                              }}
                            >
                              {it.titleDisplay}
                            </a>
                          </div>
                        </div>

                        <div className="flex gap-1 shrink-0">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="rounded-xl bg-card/60 hover:bg-card"
                            onClick={() => openExternalUrl(openUrl)}
                            title="Open"
                          >
                            <ExternalLink className="h-4 w-4" />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="rounded-xl bg-card/60 hover:bg-card"
                            onClick={() => handleCopy(it.url)}
                            title="Copy link"
                          >
                            {copiedUrl === it.url ? (
                              <Check className="h-4 w-4" />
                            ) : (
                              <Copy className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </div>

                      <div className="mt-2 flex flex-wrap gap-1.5 items-center">
                        <Badge variant="outline" className="bg-card/60">
                          {subtitle}
                        </Badge>
                        {it.dateDisplay && (
                          <Badge variant="outline" className="bg-card/60">
                            {it.dateDisplay}
                          </Badge>
                        )}
                        {it.source_type && (
                          <Badge variant="outline" className="bg-card/60">
                            {it.source_type}
                          </Badge>
                        )}
                      </div>

                      {hasRelated && (
                        <details className="mt-2">
                          <summary className="cursor-pointer select-none text-xs font-medium text-primary inline-flex items-center gap-2">
                            <span>Related</span>
                            <span className="text-muted-foreground font-normal">
                              ({group.length - 1})
                            </span>
                          </summary>
                          <div className="mt-2 space-y-1.5">
                            {group
                              .filter((x) => x.url !== it.url)
                              .slice(0, 6)
                              .map((rel) => {
                                const relUrl = normalizeUrl(rel.url);
                                const relTitle = rel.title?.trim() || rel.url;
                                return (
                                  <div key={rel.url} className="min-w-0">
                                    <a
                                      href={relUrl}
                                      className={cn(
                                        "text-xs text-primary hover:underline break-all",
                                      )}
                                      onClick={(e) => {
                                        e.preventDefault();
                                        openExternalUrl(relUrl);
                                      }}
                                    >
                                      {relTitle}
                                    </a>
                                  </div>
                                );
                              })}
                            {group.length - 1 > 6 && (
                              <div className="text-xs text-muted-foreground">
                                +{group.length - 1 - 6} more
                              </div>
                            )}
                          </div>
                        </details>
                      )}
                    </div>
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </div>
    </Card>
  );
}

