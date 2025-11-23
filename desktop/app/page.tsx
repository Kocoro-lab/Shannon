"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import Link from "next/link";
import { PlusCircle } from "lucide-react";

export default function Home() {
  return (
    <div className="p-8 space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Shannon Desktop</h1>
          <p className="text-muted-foreground">
            Submit tasks and monitor AI agent workflows.
          </p>
        </div>
        <Button asChild>
          <Link href="/runs">
            <PlusCircle className="mr-2 h-4 w-4" />
            Go to Runs
          </Link>
        </Button>
      </div>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <div className="space-y-1">
              <CardTitle className="text-lg">Quick Start</CardTitle>
              <CardDescription>
                Try these example queries:
              </CardDescription>
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-start gap-2">
              <Badge variant="outline" className="mt-0.5">1</Badge>
              <div>&quot;What is the capital of France?&quot;</div>
            </div>
            <div className="flex items-start gap-2">
              <Badge variant="outline" className="mt-0.5">2</Badge>
              <div>&quot;Calculate 25 * 47 + 123&quot;</div>
            </div>
            <div className="flex items-start gap-2">
              <Badge variant="outline" className="mt-0.5">3</Badge>
              <div>&quot;Search for latest news about AI&quot;</div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="space-y-1">
              <CardTitle className="text-lg">Features</CardTitle>
              <CardDescription>
                Shannon Desktop offers:
              </CardDescription>
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center gap-2">
              <Badge variant="secondary">✓</Badge>
              <span>Real-time streaming responses</span>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant="secondary">✓</Badge>
              <span>Tool execution visibility</span>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant="secondary">✓</Badge>
              <span>Token usage tracking</span>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant="secondary">✓</Badge>
              <span>Multi-agent orchestration</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
