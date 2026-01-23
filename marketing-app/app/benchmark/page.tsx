"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { BenchmarkChart, GapAnalysisChart } from "@/components/benchmark-chart";
import { AIChatPanel } from "@/components/ai-chat-panel";
import { useAppSelector } from "@/lib/store";
import { INDUSTRIES, getIndustryKPIs, getBenchmarkValue } from "@/lib/marketing/kpi-definitions";
import {
  TrendingUp,
  BarChart3,
  Search,
  ArrowLeft,
  Building2,
  Target,
  Users,
} from "lucide-react";
import { cn } from "@/lib/utils";

export default function BenchmarkPage() {
  return (
    <Suspense fallback={<BenchmarkPageSkeleton />}>
      <BenchmarkPageContent />
    </Suspense>
  );
}

function BenchmarkPageSkeleton() {
  return (
    <div className="container-kocoro py-8">
      <div className="animate-pulse">
        <div className="h-8 bg-neutral-2 rounded w-48 mb-4"></div>
        <div className="h-4 bg-neutral-2 rounded w-96 mb-8"></div>
        <div className="h-16 bg-neutral-2 rounded mb-6"></div>
        <div className="grid gap-6 lg:grid-cols-3">
          <div className="lg:col-span-2 h-96 bg-neutral-2 rounded"></div>
          <div className="h-96 bg-neutral-2 rounded"></div>
        </div>
      </div>
    </div>
  );
}

function BenchmarkPageContent() {
  const searchParams = useSearchParams();
  const { data: session } = useSession();
  const goalId = searchParams.get("goal");
  const { goals } = useAppSelector((state) => state.marketing);
  const goal = goalId ? goals.find((g) => g.id === goalId) : null;

  const selectedIndustry = goal?.industry || "saas_b2b";
  const industryKPIs = getIndustryKPIs(selectedIndustry);
  const industry = INDUSTRIES[selectedIndustry];

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
            <h1 className="font-serif text-3xl font-bold">ベンチマーク分析</h1>
            <p className="text-muted-foreground mt-2">
              {goal
                ? `「${goal.name}」の業界ベンチマークとの比較`
                : "業界標準との比較分析"}
            </p>
          </div>
          <Button variant="outline">
            <Search className="w-4 h-4 mr-2" />
            競合をリサーチ
          </Button>
        </div>
      </div>

      {/* Industry Info */}
      <Card className="mb-6">
        <CardContent className="py-4">
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-2">
              <Building2 className="w-5 h-5 text-primary" />
              <span className="font-medium">{industry.nameJa}</span>
            </div>
            <div className="text-sm text-muted-foreground">
              {industryKPIs.length} 個のベンチマークKPI
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Main Content */}
      <Tabs defaultValue="comparison" className="space-y-6">
        <TabsList>
          <TabsTrigger value="comparison" className="flex items-center gap-2">
            <BarChart3 className="w-4 h-4" />
            ベンチマーク比較
          </TabsTrigger>
          <TabsTrigger value="gap" className="flex items-center gap-2">
            <TrendingUp className="w-4 h-4" />
            ギャップ分析
          </TabsTrigger>
          <TabsTrigger value="competitors" className="flex items-center gap-2">
            <Users className="w-4 h-4" />
            競合分析
          </TabsTrigger>
        </TabsList>

        <TabsContent value="comparison">
          <div className="grid gap-6 lg:grid-cols-3">
            <div className="lg:col-span-2">
              {goal ? (
                <BenchmarkChart goal={goal} />
              ) : (
                <Card>
                  <CardContent className="py-12 text-center">
                    <Target className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
                    <p className="text-muted-foreground">
                      目標を選択してベンチマークと比較してください
                    </p>
                    <Link href="/goals" className="mt-4 inline-block">
                      <Button variant="outline">目標一覧へ</Button>
                    </Link>
                  </CardContent>
                </Card>
              )}
            </div>
            <div>
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">業界ベンチマーク</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {industryKPIs.slice(0, 6).map((kpi) => (
                    <div
                      key={kpi.id}
                      className="flex items-center justify-between p-2 rounded-lg bg-neutral-1"
                    >
                      <span className="text-sm">{kpi.nameJa}</span>
                      <span className="text-sm font-medium">
                        {getBenchmarkValue(kpi)}
                      </span>
                    </div>
                  ))}
                </CardContent>
              </Card>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="gap">
          <div className="grid gap-6 lg:grid-cols-3">
            <div className="lg:col-span-2">
              {goal ? (
                <GapAnalysisChart goal={goal} />
              ) : (
                <Card>
                  <CardContent className="py-12 text-center">
                    <TrendingUp className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
                    <p className="text-muted-foreground">
                      目標を選択してギャップを分析してください
                    </p>
                  </CardContent>
                </Card>
              )}
            </div>
            <div>
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">AI分析</CardTitle>
                  <CardDescription>
                    ギャップの原因と改善策を分析
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <AIChatPanel
                    initialPrompt={
                      goal
                        ? `${industry.nameJa}業界の目標「${goal.name}」について、ベンチマークとのギャップを分析し、改善のための具体的な提案をしてください。`
                        : undefined
                    }
                    workflowType="benchmark_research"
                    context={{ goalId: goal?.id, industry: selectedIndustry }}
                    userId={session?.user?.id}
                    goalId={goal?.id}
                  />
                </CardContent>
              </Card>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="competitors">
          <div className="grid gap-6 lg:grid-cols-3">
            <div className="lg:col-span-2">
              <Card>
                <CardHeader>
                  <CardTitle>競合分析</CardTitle>
                  <CardDescription>
                    業界内の競合他社との比較
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <AIChatPanel
                    initialPrompt={`${industry.nameJa}業界の主要な競合他社をリサーチし、自社との比較分析を行ってください。それぞれの強みと弱み、市場ポジショニングを分析してください。`}
                    workflowType="benchmark_research"
                    context={{
                      goalId: goal?.id,
                      industry: selectedIndustry,
                      analysisType: "competitor",
                    }}
                    userId={session?.user?.id}
                    goalId={goal?.id}
                  />
                </CardContent>
              </Card>
            </div>
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">分析のヒント</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3 text-sm text-muted-foreground">
                  <p>競合分析では以下の観点が重要です：</p>
                  <ul className="list-disc pl-4 space-y-1">
                    <li>市場シェアと成長率</li>
                    <li>製品・サービスの差別化ポイント</li>
                    <li>価格戦略</li>
                    <li>顧客セグメント</li>
                    <li>マーケティングチャネル</li>
                  </ul>
                </CardContent>
              </Card>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
