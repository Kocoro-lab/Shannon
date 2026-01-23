"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import { getKPIById, formatKPIValue, getBenchmarkValue } from "@/lib/marketing/kpi-definitions";
import { calculateKPIGap, getProgressColor, calculateKPIProgress } from "@/lib/marketing/utils";
import type { Goal, GoalKPI, IndustryType } from "@/lib/shannon/types";
import { TrendingUp, TrendingDown, Minus, Target } from "lucide-react";

interface KPITrackerProps {
  goal: Goal;
  className?: string;
}

export function KPITracker({ goal, className }: KPITrackerProps) {
  return (
    <div className={cn("space-y-4", className)}>
      {goal.kpis.map((kpi) => (
        <KPICard key={kpi.kpiId} kpi={kpi} industry={goal.industry} />
      ))}
    </div>
  );
}

interface KPICardProps {
  kpi: GoalKPI;
  industry: IndustryType;
}

function KPICard({ kpi, industry }: KPICardProps) {
  const kpiDef = getKPIById(industry, kpi.kpiId);
  if (!kpiDef) return null;

  const progress = calculateKPIProgress(kpi, kpiDef);
  const { gap, gapPercent, isAhead } = calculateKPIGap(kpi, kpiDef);
  const benchmark = kpiDef.benchmark || kpiDef.benchmarkRange?.low;
  const benchmarkComparison = benchmark
    ? ((kpi.currentValue - benchmark) / benchmark) * 100
    : 0;

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            <Target className="w-4 h-4 text-primary" />
            {kpiDef.nameJa}
          </CardTitle>
          <StatusBadge progress={progress} />
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {/* Progress Bar */}
          <div>
            <div className="flex justify-between text-sm mb-2">
              <span className="text-muted-foreground">進捗</span>
              <span className="font-medium">{Math.round(progress)}%</span>
            </div>
            <Progress
              value={Math.min(100, progress)}
              className="h-2"
              indicatorClassName={cn("bg-gradient-to-r", getProgressColor(progress))}
            />
          </div>

          {/* Values */}
          <div className="grid grid-cols-3 gap-4 pt-2">
            <div>
              <div className="text-xs text-muted-foreground">現在値</div>
              <div className="font-semibold text-lg">
                {formatKPIValue(kpiDef, kpi.currentValue)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">目標値</div>
              <div className="font-semibold text-lg">
                {formatKPIValue(kpiDef, kpi.targetValue)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">残り</div>
              <div
                className={cn(
                  "font-semibold text-lg flex items-center gap-1",
                  isAhead ? "text-green-600" : "text-orange-600"
                )}
              >
                {isAhead ? (
                  <TrendingUp className="w-4 h-4" />
                ) : (
                  <TrendingDown className="w-4 h-4" />
                )}
                {formatKPIValue(kpiDef, Math.abs(gap))}
              </div>
            </div>
          </div>

          {/* Benchmark Comparison */}
          {benchmark && (
            <div className="pt-2 border-t border-border">
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">
                  ベンチマーク: {getBenchmarkValue(kpiDef)}
                </span>
                <BenchmarkBadge comparison={benchmarkComparison} />
              </div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function StatusBadge({ progress }: { progress: number }) {
  if (progress >= 100) {
    return (
      <span className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-green-100 text-green-700">
        達成
      </span>
    );
  }
  if (progress >= 75) {
    return (
      <span className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-primary/10 text-primary">
        順調
      </span>
    );
  }
  if (progress >= 50) {
    return (
      <span className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-yellow-100 text-yellow-700">
        注意
      </span>
    );
  }
  return (
    <span className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-red-100 text-red-700">
      遅延
    </span>
  );
}

function BenchmarkBadge({ comparison }: { comparison: number }) {
  if (Math.abs(comparison) < 5) {
    return (
      <span className="inline-flex items-center gap-1 text-muted-foreground">
        <Minus className="w-3 h-3" />
        業界平均
      </span>
    );
  }
  if (comparison > 0) {
    return (
      <span className="inline-flex items-center gap-1 text-green-600">
        <TrendingUp className="w-3 h-3" />+{comparison.toFixed(0)}% 上回る
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-orange-600">
      <TrendingDown className="w-3 h-3" />
      {comparison.toFixed(0)}% 下回る
    </span>
  );
}

// Summary component for dashboard
interface KPISummaryProps {
  goal: Goal;
  className?: string;
}

export function KPISummary({ goal, className }: KPISummaryProps) {
  const kpisWithProgress = goal.kpis.map((kpi) => {
    const kpiDef = getKPIById(goal.industry, kpi.kpiId);
    const progress = calculateKPIProgress(kpi, kpiDef || undefined);
    return { ...kpi, progress, kpiDef };
  });

  const overallProgress =
    kpisWithProgress.length > 0
      ? kpisWithProgress.reduce((sum, k) => sum + k.progress, 0) / kpisWithProgress.length
      : 0;

  const onTrack = kpisWithProgress.filter((k) => k.progress >= 75).length;
  const needsAttention = kpisWithProgress.filter((k) => k.progress < 50).length;

  return (
    <Card className={className}>
      <CardContent className="pt-6">
        <div className="flex items-center justify-between mb-4">
          <div>
            <div className="text-2xl font-bold">{Math.round(overallProgress)}%</div>
            <div className="text-sm text-muted-foreground">全体進捗</div>
          </div>
          <div className="flex gap-4 text-sm">
            <div className="text-center">
              <div className="font-semibold text-green-600">{onTrack}</div>
              <div className="text-muted-foreground">順調</div>
            </div>
            <div className="text-center">
              <div className="font-semibold text-red-600">{needsAttention}</div>
              <div className="text-muted-foreground">要注意</div>
            </div>
          </div>
        </div>
        <Progress
          value={Math.min(100, overallProgress)}
          className="h-3"
          indicatorClassName={cn("bg-gradient-to-r", getProgressColor(overallProgress))}
        />
      </CardContent>
    </Card>
  );
}
