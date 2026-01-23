"use client";

import { useState } from "react";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { AddKPIDialog } from "@/components/add-kpi-dialog";
import { useAppSelector, useAppDispatch } from "@/lib/store";
import { updateGoal } from "@/lib/store/slices/marketing";
import { INDUSTRIES } from "@/lib/marketing/kpi-definitions";
import { calculateGoalProgress, getDaysRemaining, getStatusColor, getProgressColor } from "@/lib/marketing/utils";
import type { Goal, GoalKPI } from "@/lib/shannon/types";
import { Plus, Target, Calendar, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

export default function GoalsPage() {
  const { goals } = useAppSelector((state) => state.marketing);
  const dispatch = useAppDispatch();

  const [addKPIDialogOpen, setAddKPIDialogOpen] = useState(false);
  const [selectedGoalForKPI, setSelectedGoalForKPI] = useState<Goal | null>(null);

  const handleOpenAddKPIDialog = (e: React.MouseEvent, goal: Goal) => {
    e.preventDefault();
    e.stopPropagation();
    setSelectedGoalForKPI(goal);
    setAddKPIDialogOpen(true);
  };

  const handleAddKPI = (kpi: GoalKPI) => {
    if (!selectedGoalForKPI) return;

    // 既に同じKPIが存在するかチェック
    const exists = selectedGoalForKPI.kpis.some((k) => k.kpiId === kpi.kpiId);
    if (exists) return;

    const updatedGoal: Goal = {
      ...selectedGoalForKPI,
      kpis: [...selectedGoalForKPI.kpis, kpi],
      updatedAt: new Date().toISOString(),
    };

    dispatch(updateGoal(updatedGoal));
    setSelectedGoalForKPI(null);
  };

  return (
    <div className="container-kocoro py-8">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="font-serif text-3xl font-bold">目標管理</h1>
          <p className="text-muted-foreground mt-2">
            マーケティング目標の設定と進捗管理
          </p>
        </div>
        <Link href="/goals/new">
          <Button>
            <Plus className="w-4 h-4 mr-2" />
            新しい目標を設定
          </Button>
        </Link>
      </div>

      {goals.length === 0 ? (
        <Card className="text-center py-12">
          <CardContent>
            <Target className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="font-serif text-lg font-medium mb-2">
              目標がまだ設定されていません
            </h3>
            <p className="text-muted-foreground mb-6">
              マーケティング目標を設定して、進捗を追跡しましょう
            </p>
            <Link href="/goals/new">
              <Button>
                <Plus className="w-4 h-4 mr-2" />
                最初の目標を設定
              </Button>
            </Link>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {goals.map((goal) => {
            const progress = calculateGoalProgress(goal);
            const daysRemaining = getDaysRemaining(goal.deadline);
            const industry = INDUSTRIES[goal.industry];

            return (
              <Link key={goal.id} href={`/goals/${goal.id}`}>
                <Card className="h-full transition-all hover:shadow-lg cursor-pointer">
                  <CardHeader>
                    <div className="flex items-center justify-between">
                      <span
                        className={cn(
                          "text-xs px-2 py-1 rounded-full font-medium",
                          getStatusColor(goal.status)
                        )}
                      >
                        {goal.status === "active"
                          ? "進行中"
                          : goal.status === "completed"
                          ? "完了"
                          : "アーカイブ"}
                      </span>
                      <ChevronRight className="w-4 h-4 text-muted-foreground" />
                    </div>
                    <CardTitle className="mt-2">{goal.name}</CardTitle>
                    <CardDescription>{industry.nameJa}</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-4">
                      <div>
                        <div className="flex justify-between text-sm mb-2">
                          <span className="text-muted-foreground">進捗</span>
                          <span className="font-medium">{progress}%</span>
                        </div>
                        <Progress
                          value={progress}
                          indicatorClassName={cn(
                            "bg-gradient-to-r",
                            getProgressColor(progress)
                          )}
                        />
                      </div>

                      <div className="flex items-center justify-between text-sm">
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Target className="w-4 h-4" />
                          <span>{goal.kpis.length} KPI</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Calendar className="w-4 h-4 text-muted-foreground" />
                          <span
                            className={cn(
                              daysRemaining < 7
                                ? "text-red-600 font-medium"
                                : daysRemaining < 30
                                ? "text-orange-600"
                                : "text-muted-foreground"
                            )}
                          >
                            {daysRemaining > 0
                              ? `残り${daysRemaining}日`
                              : daysRemaining === 0
                              ? "今日まで"
                              : "期限超過"}
                          </span>
                        </div>
                      </div>

                      {/* KPI追加ボタン */}
                      <Button
                        variant="outline"
                        size="sm"
                        className="w-full mt-4"
                        onClick={(e) => handleOpenAddKPIDialog(e, goal)}
                      >
                        <Plus className="w-4 h-4 mr-2" />
                        KPIを追加
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              </Link>
            );
          })}
        </div>
      )}

      {/* KPI追加ダイアログ */}
      {selectedGoalForKPI && (
        <AddKPIDialog
          goal={selectedGoalForKPI}
          open={addKPIDialogOpen}
          onOpenChange={setAddKPIDialogOpen}
          onAddKPI={handleAddKPI}
        />
      )}
    </div>
  );
}
