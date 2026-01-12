'use client';

/**
 * Source Citations Component
 *
 * Displays sources collected during research workflows with citations and links.
 * Shows source confidence scores and access timestamps.
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { ExternalLink } from 'lucide-react';

export interface Source {
  title: string;
  url?: string;
  snippet?: string;
  confidence: number;
  accessed_at: string;
}

interface SourceCitationsProps {
  sources: Source[];
}

export function SourceCitations({ sources }: SourceCitationsProps) {
  if (sources.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Sources</CardTitle>
          <CardDescription>No sources collected yet</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  // Sort by confidence descending
  const sortedSources = [...sources].sort((a, b) => b.confidence - a.confidence);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sources</CardTitle>
        <CardDescription>
          {sources.length} source{sources.length !== 1 ? 's' : ''} collected
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {sortedSources.map((source, index) => (
            <div
              key={index}
              className="border rounded-lg p-4 hover:bg-accent/50 transition-colors"
            >
              {/* Source Header */}
              <div className="flex items-start justify-between gap-2 mb-2">
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate">{source.title}</div>
                  {source.url && (
                    <a
                      href={source.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs text-primary hover:underline flex items-center gap-1 mt-1"
                    >
                      <span className="truncate">{source.url}</span>
                      <ExternalLink className="h-3 w-3 flex-shrink-0" />
                    </a>
                  )}
                </div>
                <Badge variant="outline">
                  {(source.confidence * 100).toFixed(0)}%
                </Badge>
              </div>

              {/* Source Snippet */}
              {source.snippet && (
                <div className="text-sm text-muted-foreground mt-2 line-clamp-3">
                  {source.snippet}
                </div>
              )}

              {/* Access Time */}
              <div className="text-xs text-muted-foreground mt-2">
                Accessed: {new Date(source.accessed_at).toLocaleString()}
              </div>
            </div>
          ))}
        </div>

        {/* Summary Stats */}
        <div className="mt-4 pt-4 border-t">
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <div className="text-muted-foreground">Total Sources</div>
              <div className="font-semibold">{sources.length}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Avg Confidence</div>
              <div className="font-semibold">
                {((sources.reduce((sum, s) => sum + s.confidence, 0) / sources.length) * 100).toFixed(0)}%
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
