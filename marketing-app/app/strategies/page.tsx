"use client";

import { useState, Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { StrategyCanvas, StrategyList } from "@/components/strategy-canvas";
import { AIChatPanel } from "@/components/ai-chat-panel";
import { useAppSelector } from "@/lib/store";
import { INDUSTRIES, KPI_DEFINITIONS } from "@/lib/marketing/kpi-definitions";
import type { Strategy, Goal, GoalKPI } from "@/lib/shannon/types";
import { getStrategyPromptSuffix } from "@/lib/shannon/strategy-parser";
import {
  ArrowLeft,
  Lightbulb,
  LayoutGrid,
  List,
  Sparkles,
  Plus,
} from "lucide-react";

export default function StrategiesPage() {
  return (
    <Suspense fallback={<StrategiesPageSkeleton />}>
      <StrategiesPageContent />
    </Suspense>
  );
}

function StrategiesPageSkeleton() {
  return (
    <div className="container-kocoro py-8">
      <div className="animate-pulse">
        <div className="h-8 bg-neutral-2 rounded w-48 mb-4"></div>
        <div className="h-4 bg-neutral-2 rounded w-96 mb-8"></div>
        <div className="grid gap-6 lg:grid-cols-3">
          <div className="lg:col-span-2 h-96 bg-neutral-2 rounded"></div>
          <div className="h-96 bg-neutral-2 rounded"></div>
        </div>
      </div>
    </div>
  );
}

// Helper function to format KPI data for the prompt
function formatKPIData(goal: Goal): string {
  const kpiDefs = KPI_DEFINITIONS[goal.industry] || [];

  const kpiLines = goal.kpis.map((kpi: GoalKPI) => {
    const def = kpiDefs.find((d) => d.id === kpi.kpiId);
    const kpiName = def?.nameJa || kpi.kpiId;
    const gap = kpi.targetValue - kpi.currentValue;
    const gapPercent = kpi.currentValue > 0
      ? ((gap / kpi.currentValue) * 100).toFixed(1)
      : "N/A";

    return `- ${kpiName}: 現在値 ${kpi.currentValue.toLocaleString()}${kpi.unit} → 目標値 ${kpi.targetValue.toLocaleString()}${kpi.unit} (ギャップ: ${gap > 0 ? '+' : ''}${gap.toLocaleString()}${kpi.unit}, ${gapPercent}%改善が必要)`;
  });

  return kpiLines.join('\n');
}

function StrategiesPageContent() {
  const searchParams = useSearchParams();
  const { data: session } = useSession();
  const goalId = searchParams.get("goal");
  const { goals, strategies } = useAppSelector((state) => state.marketing);
  const goal = goalId ? goals.find((g) => g.id === goalId) : null;
  const [viewMode, setViewMode] = useState<"canvas" | "list">("canvas");

  const filteredStrategies = goalId
    ? strategies.filter((s) => s.goalId === goalId)
    : strategies;

  const industry = goal ? INDUSTRIES[goal.industry] : null;
  const kpiDataString = goal ? formatKPIData(goal) : "";

  return (
    <div className="container-kocoro py-8">
      {/* Header */}
      <div className="mb-8">
        {goal && (
          <Link
            href={`/goals/${goal.id}`}
            className="inline-flex items-center text-sm text-muted-foreground hover:text-foreground mb-4"
          >
            <ArrowLeft className="w-4 h-4 mr-1" />
            目標に戻る
          </Link>
        )}
        <div className="flex items-start justify-between">
          <div>
            <h1 className="font-serif text-3xl font-bold">施策管理</h1>
            <p className="text-muted-foreground mt-2">
              {goal
                ? `「${goal.name}」の施策一覧と実行プラン`
                : "マーケティング施策の管理と優先順位付け"}
            </p>
          </div>
          <div className="flex gap-2">
            <div className="flex rounded-lg border border-border p-1">
              <Button
                variant={viewMode === "canvas" ? "secondary" : "ghost"}
                size="sm"
                onClick={() => setViewMode("canvas")}
              >
                <LayoutGrid className="w-4 h-4" />
              </Button>
              <Button
                variant={viewMode === "list" ? "secondary" : "ghost"}
                size="sm"
                onClick={() => setViewMode("list")}
              >
                <List className="w-4 h-4" />
              </Button>
            </div>
            <Button>
              <Plus className="w-4 h-4 mr-2" />
              施策を追加
            </Button>
          </div>
        </div>
      </div>

      {/* Main Content - AI施策提案を優先表示 */}
      <div className="space-y-8">
        {/* AI Strategy Generator - フルワイド、優先表示 */}
        <Card className="border-primary/20 bg-gradient-to-br from-primary/5 to-transparent">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-xl">
              <Sparkles className="w-6 h-6 text-primary" />
              AI施策提案
            </CardTitle>
            <CardDescription className="text-base">
              目標とベンチマークに基づき、競合分析とDeep Researchを行い最適な施策を提案します
            </CardDescription>
          </CardHeader>
          <CardContent>
            <AIChatPanel
              initialPrompt={
                goal
                  ? `${industry?.nameJa}業界の目標「${goal.name}」を達成するための具体的な施策を5つ提案してください。

## 現在の目標とKPI状況
期限: ${goal.deadline}

${kpiDataString}

## 調査内容
まず、以下の調査を行ってください：
1. この業界の主要な競合企業のマーケティング手法をDeep Researchで調査
2. 最新のマーケティングトレンドと成功事例を分析
3. 競合が採用している効果的な施策を特定

## 施策提案の要件
上記のKPIギャップを埋めることを目的として、各施策について以下を含めてください：
- 期待されるインパクト(high/medium/low) - 上記KPIへの貢献度を考慮
- 必要な工数(high/medium/low)
- 具体的な実行ステップ(3-5個)
- 競合との差別化ポイント
- 期待される効果（どのKPIがどの程度改善されるか）

${getStrategyPromptSuffix()}`
                  : undefined
              }
              workflowType="strategy_planning"
              context={{
                goalId: goal?.id,
                industry: goal?.industry,
                deadline: goal?.deadline,
                kpis: goal?.kpis,
              }}
              goalId={goal?.id}
              userId={session?.user?.id}
              className="min-h-[400px]"
            />
          </CardContent>
        </Card>

        {/* 施策一覧とサマリー */}
        <div className="grid gap-6 lg:grid-cols-4">
          {/* 施策一覧 - 3/4幅 */}
          <div className="lg:col-span-3">
            <div className="flex items-center justify-between mb-4">
              <h2 className="font-serif text-xl font-semibold">施策一覧</h2>
            </div>
            {filteredStrategies.length === 0 ? (
              <Card>
                <CardContent className="py-12 text-center">
                  <Lightbulb className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
                  <h3 className="font-serif text-lg font-medium mb-2">
                    施策がまだありません
                  </h3>
                  <p className="text-muted-foreground mb-6">
                    上のAI施策提案で「分析を開始」をクリックして施策を提案してもらいましょう
                  </p>
                </CardContent>
              </Card>
            ) : viewMode === "canvas" ? (
              <StrategyCanvas strategies={filteredStrategies} />
            ) : (
              <StrategyList strategies={filteredStrategies} />
            )}
          </div>

          {/* Quick Stats - 1/4幅 */}
          <div>
            <Card>
              <CardHeader>
                <CardTitle className="text-base">施策サマリー</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <StatRow
                  label="提案中"
                  value={
                    filteredStrategies.filter((s) => s.status === "proposed").length
                  }
                  color="text-yellow-600"
                />
                <StatRow
                  label="進行中"
                  value={
                    filteredStrategies.filter((s) => s.status === "in_progress")
                      .length
                  }
                  color="text-primary"
                />
                <StatRow
                  label="完了"
                  value={
                    filteredStrategies.filter((s) => s.status === "completed").length
                  }
                  color="text-green-600"
                />
                <StatRow
                  label="Quick Wins"
                  value={
                    filteredStrategies.filter(
                      (s) => s.impact === "high" && s.effort === "low"
                    ).length
                  }
                  color="text-blue-600"
                />
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}

function StatRow({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div className="flex items-center justify-between p-2 rounded-lg bg-neutral-1">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className={`font-semibold ${color}`}>{value}</span>
    </div>
  );
}
