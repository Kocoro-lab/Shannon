"use client";

import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { useAppSelector } from "@/lib/store";
import { INDUSTRIES } from "@/lib/marketing/kpi-definitions";
import {
  calculateGoalProgress,
  getDaysRemaining,
  getProgressColor,
} from "@/lib/marketing/utils";
import {
  Target,
  TrendingUp,
  Lightbulb,
  BarChart3,
  ArrowRight,
  Plus,
  Calendar,
  Sparkles,
} from "lucide-react";
import { cn } from "@/lib/utils";

export default function DashboardPage() {
  const { goals, strategies } = useAppSelector((state) => state.marketing);

  // Calculate summary stats
  const activeGoals = goals.filter((g) => g.status === "active");
  const avgProgress =
    activeGoals.length > 0
      ? Math.round(
          activeGoals.reduce((sum, g) => sum + calculateGoalProgress(g), 0) /
            activeGoals.length
        )
      : 0;
  const urgentGoals = activeGoals.filter((g) => getDaysRemaining(g.deadline) < 14);
  const inProgressStrategies = strategies.filter(
    (s) => s.status === "in_progress"
  ).length;

  return (
    <div className="container-kocoro py-8">
      {/* Hero Section */}
      <div className="mb-8">
        <h1 className="font-serif text-4xl font-bold mb-2">
          マーケティング戦略ダッシュボード
        </h1>
        <p className="text-muted-foreground text-lg">
          目標達成に向けた進捗を確認し、次のアクションを計画しましょう
        </p>
      </div>

      {/* Quick Stats */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 mb-8">
        <StatCard
          title="アクティブな目標"
          value={activeGoals.length}
          unit="件"
          icon={<Target className="w-5 h-5" />}
          trend={null}
          href="/goals"
        />
        <StatCard
          title="平均進捗率"
          value={avgProgress}
          unit="%"
          icon={<TrendingUp className="w-5 h-5" />}
          trend={avgProgress >= 50 ? "positive" : avgProgress >= 25 ? "neutral" : "negative"}
          href="/goals"
        />
        <StatCard
          title="進行中の施策"
          value={inProgressStrategies}
          unit="件"
          icon={<Lightbulb className="w-5 h-5" />}
          trend={null}
          href="/strategies"
        />
        <StatCard
          title="期限間近"
          value={urgentGoals.length}
          unit="件"
          icon={<Calendar className="w-5 h-5" />}
          trend={urgentGoals.length > 0 ? "warning" : "positive"}
          href="/goals"
        />
      </div>

      {/* Main Content */}
      <div className="grid gap-6 lg:grid-cols-3">
        {/* Goals Overview */}
        <div className="lg:col-span-2 space-y-6">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>目標の進捗</CardTitle>
                <CardDescription>アクティブな目標の進捗状況</CardDescription>
              </div>
              <Link href="/goals/new">
                <Button size="sm">
                  <Plus className="w-4 h-4 mr-2" />
                  新規作成
                </Button>
              </Link>
            </CardHeader>
            <CardContent>
              {activeGoals.length === 0 ? (
                <EmptyState
                  icon={<Target className="w-12 h-12" />}
                  title="目標がありません"
                  description="最初の目標を設定して、マーケティング戦略を始めましょう"
                  actionLabel="目標を設定"
                  actionHref="/goals/new"
                />
              ) : (
                <div className="space-y-4">
                  {activeGoals.slice(0, 3).map((goal) => {
                    const progress = calculateGoalProgress(goal);
                    const daysRemaining = getDaysRemaining(goal.deadline);
                    const industry = INDUSTRIES[goal.industry];

                    return (
                      <Link
                        key={goal.id}
                        href={`/goals/${goal.id}`}
                        className="block"
                      >
                        <div className="p-4 rounded-lg border border-border hover:border-primary/50 hover:shadow-sm transition-all">
                          <div className="flex items-center justify-between mb-2">
                            <div>
                              <h4 className="font-medium">{goal.name}</h4>
                              <span className="text-xs text-muted-foreground">
                                {industry.nameJa}
                              </span>
                            </div>
                            <div
                              className={cn(
                                "text-sm",
                                daysRemaining < 7
                                  ? "text-red-600 font-medium"
                                  : daysRemaining < 14
                                  ? "text-orange-600"
                                  : "text-muted-foreground"
                              )}
                            >
                              {daysRemaining > 0
                                ? `残り${daysRemaining}日`
                                : "期限超過"}
                            </div>
                          </div>
                          <div className="flex items-center gap-3">
                            <Progress
                              value={progress}
                              className="flex-1"
                              indicatorClassName={cn(
                                "bg-gradient-to-r",
                                getProgressColor(progress)
                              )}
                            />
                            <span className="text-sm font-medium w-12 text-right">
                              {progress}%
                            </span>
                          </div>
                        </div>
                      </Link>
                    );
                  })}
                  {activeGoals.length > 3 && (
                    <Link
                      href="/goals"
                      className="flex items-center justify-center gap-1 text-sm text-primary hover:underline py-2"
                    >
                      すべての目標を見る
                      <ArrowRight className="w-4 h-4" />
                    </Link>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Recent Strategies */}
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>最近の施策</CardTitle>
                <CardDescription>進行中および提案中の施策</CardDescription>
              </div>
              <Link href="/strategies">
                <Button variant="outline" size="sm">
                  すべて見る
                </Button>
              </Link>
            </CardHeader>
            <CardContent>
              {strategies.length === 0 ? (
                <EmptyState
                  icon={<Lightbulb className="w-12 h-12" />}
                  title="施策がありません"
                  description="AIに施策を提案してもらいましょう"
                  actionLabel="施策を提案"
                  actionHref="/strategies"
                />
              ) : (
                <div className="space-y-3">
                  {strategies.slice(0, 4).map((strategy) => (
                    <Link
                      key={strategy.id}
                      href={`/strategies/${strategy.id}`}
                      className="block"
                    >
                      <div className="flex items-center gap-3 p-3 rounded-lg hover:bg-neutral-1 transition-colors">
                        <div
                          className={cn(
                            "w-2 h-2 rounded-full",
                            strategy.status === "in_progress"
                              ? "bg-primary animate-pulse"
                              : strategy.status === "completed"
                              ? "bg-green-500"
                              : "bg-yellow-500"
                          )}
                        />
                        <div className="flex-1 min-w-0">
                          <div className="font-medium truncate">
                            {strategy.name}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            インパクト: {strategy.impact} / 工数:{" "}
                            {strategy.effort}
                          </div>
                        </div>
                        <ArrowRight className="w-4 h-4 text-muted-foreground" />
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Side Panel */}
        <div className="space-y-6">
          {/* Quick Actions */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">クイックアクション</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <Link href="/goals/new" className="block">
                <Button variant="outline" className="w-full justify-start">
                  <Target className="w-4 h-4 mr-2" />
                  新しい目標を設定
                </Button>
              </Link>
              <Link href="/benchmark" className="block">
                <Button variant="outline" className="w-full justify-start">
                  <BarChart3 className="w-4 h-4 mr-2" />
                  ベンチマーク分析
                </Button>
              </Link>
              <Link href="/strategies" className="block">
                <Button variant="outline" className="w-full justify-start">
                  <Sparkles className="w-4 h-4 mr-2" />
                  AI施策提案
                </Button>
              </Link>
            </CardContent>
          </Card>

          {/* Tips */}
          <Card className="bg-gradient-to-br from-primary/5 to-primary-coral/5 border-primary/20">
            <CardHeader>
              <CardTitle className="text-base flex items-center gap-2">
                <Sparkles className="w-4 h-4 text-primary" />
                今日のヒント
              </CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                目標達成率を上げるためのコツ：KPIは3-5個に絞り、毎週の進捗確認を習慣化しましょう。
              </p>
            </CardContent>
          </Card>

          {/* Industry Benchmarks Quick View */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">業種別ベンチマーク</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {Object.entries(INDUSTRIES)
                  .slice(0, 4)
                  .map(([key, industry]) => (
                    <Link
                      key={key}
                      href={`/benchmark?industry=${key}`}
                      className="flex items-center justify-between p-2 rounded-lg hover:bg-neutral-1 transition-colors"
                    >
                      <span className="text-sm">{industry.nameJa}</span>
                      <ArrowRight className="w-4 h-4 text-muted-foreground" />
                    </Link>
                  ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

interface StatCardProps {
  title: string;
  value: number;
  unit: string;
  icon: React.ReactNode;
  trend: "positive" | "negative" | "neutral" | "warning" | null;
  href: string;
}

function StatCard({ title, value, unit, icon, trend, href }: StatCardProps) {
  const trendColors = {
    positive: "text-green-600",
    negative: "text-red-600",
    neutral: "text-yellow-600",
    warning: "text-orange-600",
  };

  return (
    <Link href={href}>
      <Card className="hover:shadow-md transition-shadow cursor-pointer">
        <CardContent className="pt-6">
          <div className="flex items-center justify-between mb-2">
            <span className="text-muted-foreground">{icon}</span>
            {trend && (
              <span
                className={cn(
                  "text-xs px-2 py-0.5 rounded-full",
                  trend === "positive"
                    ? "bg-green-100 text-green-700"
                    : trend === "warning"
                    ? "bg-orange-100 text-orange-700"
                    : trend === "negative"
                    ? "bg-red-100 text-red-700"
                    : "bg-yellow-100 text-yellow-700"
                )}
              >
                {trend === "positive"
                  ? "良好"
                  : trend === "warning"
                  ? "注意"
                  : trend === "negative"
                  ? "要改善"
                  : "普通"}
              </span>
            )}
          </div>
          <div className="flex items-baseline gap-1">
            <span className="text-3xl font-bold">{value}</span>
            <span className="text-muted-foreground">{unit}</span>
          </div>
          <p className="text-sm text-muted-foreground mt-1">{title}</p>
        </CardContent>
      </Card>
    </Link>
  );
}

interface EmptyStateProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  actionLabel: string;
  actionHref: string;
}

function EmptyState({
  icon,
  title,
  description,
  actionLabel,
  actionHref,
}: EmptyStateProps) {
  return (
    <div className="text-center py-8">
      <div className="text-muted-foreground mb-4 flex justify-center opacity-50">
        {icon}
      </div>
      <h3 className="font-medium mb-2">{title}</h3>
      <p className="text-sm text-muted-foreground mb-4">{description}</p>
      <Link href={actionHref}>
        <Button size="sm">
          <Plus className="w-4 h-4 mr-2" />
          {actionLabel}
        </Button>
      </Link>
    </div>
  );
}
