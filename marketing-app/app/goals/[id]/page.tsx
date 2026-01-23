"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { KPITracker, KPISummary } from "@/components/kpi-tracker";
import { AIChatPanel } from "@/components/ai-chat-panel";
import { EditableField } from "@/components/editable-field";
import { AddKPIDialog } from "@/components/add-kpi-dialog";
import { useAppSelector, useAppDispatch } from "@/lib/store";
import { updateGoal, removeGoal } from "@/lib/store/slices/marketing";
import { deleteGoal } from "@/lib/supabase/goals";
import { INDUSTRIES, getKPIById, getKPIDisplayName } from "@/lib/marketing/kpi-definitions";
import { getDaysRemaining, getStatusColor, calculateKPIProgress } from "@/lib/marketing/utils";
import type { Goal, GoalKPI } from "@/lib/shannon/types";
import {
  ArrowLeft,
  Target,
  Calendar,
  TrendingUp,
  Lightbulb,
  Play,
  BarChart3,
  Plus,
  Trash2,
} from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { cn } from "@/lib/utils";

export default function GoalDetailPage() {
  const params = useParams();
  const router = useRouter();
  const { data: session } = useSession();
  const goalId = params.id as string;
  const { goals } = useAppSelector((state) => state.marketing);
  const dispatch = useAppDispatch();
  const goal = goals.find((g) => g.id === goalId);

  const [addKPIDialogOpen, setAddKPIDialogOpen] = useState(false);
  const [kpiToDelete, setKpiToDelete] = useState<string | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteGoalDialog, setShowDeleteGoalDialog] = useState(false);

  const handleUpdateGoalName = (newName: string | number) => {
    if (goal) {
      dispatch(
        updateGoal({
          ...goal,
          name: String(newName),
          updatedAt: new Date().toISOString(),
        })
      );
    }
  };

  const handleUpdateKPI = (kpiId: string, field: "currentValue" | "targetValue", value: number) => {
    if (goal) {
      const updatedKpis = goal.kpis.map((kpi) =>
        kpi.kpiId === kpiId ? { ...kpi, [field]: value } : kpi
      );
      dispatch(
        updateGoal({
          ...goal,
          kpis: updatedKpis,
          updatedAt: new Date().toISOString(),
        })
      );
    }
  };

  const handleAddKPI = (kpi: GoalKPI) => {
    if (!goal) return;

    // 既に同じKPIが存在するかチェック
    const exists = goal.kpis.some((k) => k.kpiId === kpi.kpiId);
    if (exists) return;

    dispatch(
      updateGoal({
        ...goal,
        kpis: [...goal.kpis, kpi],
        updatedAt: new Date().toISOString(),
      })
    );
  };

  const handleDeleteKPI = (kpiId: string) => {
    if (!goal || goal.kpis.length <= 1) return; // 最低1つは残す

    dispatch(
      updateGoal({
        ...goal,
        kpis: goal.kpis.filter((k) => k.kpiId !== kpiId),
        updatedAt: new Date().toISOString(),
      })
    );
    setKpiToDelete(null);
  };

  const handleDeleteGoal = async () => {
    if (!goal || !session?.user?.id) return;
    setIsDeleting(true);
    try {
      await deleteGoal(goal.id, session.user.id);
      dispatch(removeGoal(goal.id));
      router.push("/goals");
    } catch (error) {
      throw new Error("目標の削除に失敗しました");
    } finally {
      setIsDeleting(false);
      setShowDeleteGoalDialog(false);
    }
  };

  if (!goal) {
    return (
      <div className="container-kocoro py-8">
        <Card className="text-center py-12">
          <CardContent>
            <Target className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="font-serif text-lg font-medium mb-2">
              目標が見つかりません
            </h3>
            <p className="text-muted-foreground mb-6">
              指定された目標は存在しないか、削除されました
            </p>
            <Link href="/goals">
              <Button variant="outline">
                <ArrowLeft className="w-4 h-4 mr-2" />
                目標一覧に戻る
              </Button>
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  const industry = INDUSTRIES[goal.industry];
  const daysRemaining = getDaysRemaining(goal.deadline);

  return (
    <div className="container-kocoro py-8">
      {/* Header */}
      <div className="mb-8">
        <Link
          href="/goals"
          className="inline-flex items-center text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ArrowLeft className="w-4 h-4 mr-1" />
          目標一覧
        </Link>
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <EditableField
                value={goal.name}
                onSave={handleUpdateGoalName}
                type="text"
                displayClassName="font-serif text-3xl font-bold"
              />
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
            </div>
            <div className="flex items-center gap-4 text-sm text-muted-foreground">
              <span className="flex items-center gap-1">
                <BarChart3 className="w-4 h-4" />
                {industry.nameJa}
              </span>
              <span className="flex items-center gap-1">
                <Target className="w-4 h-4" />
                {goal.kpis.length} KPI
              </span>
              <span
                className={cn(
                  "flex items-center gap-1",
                  daysRemaining < 7
                    ? "text-red-600"
                    : daysRemaining < 30
                    ? "text-orange-600"
                    : ""
                )}
              >
                <Calendar className="w-4 h-4" />
                {daysRemaining > 0
                  ? `残り${daysRemaining}日`
                  : daysRemaining === 0
                  ? "今日まで"
                  : "期限超過"}
              </span>
            </div>
          </div>
          <div className="flex gap-2">
            <Link href={`/benchmark?goal=${goal.id}`}>
              <Button variant="outline">
                <TrendingUp className="w-4 h-4 mr-2" />
                ベンチマーク分析
              </Button>
            </Link>
            <Link href={`/strategies?goal=${goal.id}`}>
              <Button>
                <Lightbulb className="w-4 h-4 mr-2" />
                施策を提案
              </Button>
            </Link>
            <Button
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => setShowDeleteGoalDialog(true)}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              削除
            </Button>
          </div>
        </div>
      </div>

      {/* Summary */}
      <KPISummary goal={goal} className="mb-8" />

      {/* Main Content */}
      <Tabs defaultValue="kpis" className="space-y-6">
        <TabsList>
          <TabsTrigger value="kpis" className="flex items-center gap-2">
            <Target className="w-4 h-4" />
            KPI詳細
          </TabsTrigger>
          <TabsTrigger value="analysis" className="flex items-center gap-2">
            <TrendingUp className="w-4 h-4" />
            AI分析
          </TabsTrigger>
          <TabsTrigger value="actions" className="flex items-center gap-2">
            <Play className="w-4 h-4" />
            アクション
          </TabsTrigger>
        </TabsList>

        <TabsContent value="kpis">
          <div className="grid gap-6 lg:grid-cols-2">
            {goal.kpis.map((kpi) => {
              const kpiDef = getKPIById(goal.industry, kpi.kpiId);
              const kpiName = getKPIDisplayName(goal.industry, kpi);
              const progress = calculateKPIProgress(kpi, kpiDef || undefined);

              return (
                <Card key={kpi.kpiId}>
                  <CardHeader className="pb-2">
                    <div className="flex items-center justify-between">
                      <CardTitle className="text-base font-medium flex items-center gap-2">
                        <Target className="w-4 h-4 text-primary" />
                        {kpiName}
                        {kpi.isCustom && (
                          <span className="text-xs text-muted-foreground font-normal">
                            （カスタム）
                          </span>
                        )}
                        {(kpi.lowerIsBetter || kpiDef?.lowerIsBetter) && (
                          <span className="text-xs text-blue-600 font-normal">
                            ↓低いほど良い
                          </span>
                        )}
                      </CardTitle>
                      {goal.kpis.length > 1 && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-destructive"
                          onClick={() => setKpiToDelete(kpi.kpiId)}
                        >
                          <Trash2 className="w-4 h-4" />
                        </Button>
                      )}
                    </div>
                  </CardHeader>
                  <CardContent>
                    <div className="grid grid-cols-3 gap-4">
                      <div>
                        <div className="text-xs text-muted-foreground mb-1">現在値</div>
                        <EditableField
                          value={kpi.currentValue}
                          onSave={(value) => handleUpdateKPI(kpi.kpiId, "currentValue", Number(value))}
                          type="number"
                          displayClassName="font-semibold text-lg"
                        />
                        <span className="text-xs text-muted-foreground">{kpi.unit}</span>
                      </div>
                      <div>
                        <div className="text-xs text-muted-foreground mb-1">目標値</div>
                        <EditableField
                          value={kpi.targetValue}
                          onSave={(value) => handleUpdateKPI(kpi.kpiId, "targetValue", Number(value))}
                          type="number"
                          displayClassName="font-semibold text-lg"
                        />
                        <span className="text-xs text-muted-foreground">{kpi.unit}</span>
                      </div>
                      <div>
                        <div className="text-xs text-muted-foreground mb-1">進捗</div>
                        <div className="font-semibold text-lg">{Math.round(progress)}%</div>
                      </div>
                    </div>
                    <div className="mt-4 h-2 bg-neutral-2 rounded-full overflow-hidden">
                      <div
                        className={cn(
                          "h-full rounded-full transition-all",
                          progress >= 100
                            ? "bg-green-500"
                            : progress >= 75
                            ? "bg-primary"
                            : progress >= 50
                            ? "bg-yellow-500"
                            : "bg-red-500"
                        )}
                        style={{ width: `${Math.min(100, progress)}%` }}
                      />
                    </div>
                  </CardContent>
                </Card>
              );
            })}

            {/* KPI追加カード */}
            <Card
              className="border-dashed cursor-pointer hover:border-primary hover:bg-primary/5 transition-all"
              onClick={() => setAddKPIDialogOpen(true)}
            >
              <CardContent className="flex flex-col items-center justify-center h-full min-h-[200px] py-8">
                <div className="w-12 h-12 rounded-full bg-primary/10 flex items-center justify-center mb-4">
                  <Plus className="w-6 h-6 text-primary" />
                </div>
                <div className="text-center">
                  <div className="font-medium mb-1">KPIを追加</div>
                  <div className="text-sm text-muted-foreground">
                    新しいKPIを追跡します
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* KPI追加ダイアログ */}
          <AddKPIDialog
            goal={goal}
            open={addKPIDialogOpen}
            onOpenChange={setAddKPIDialogOpen}
            onAddKPI={handleAddKPI}
          />

          {/* KPI削除確認ダイアログ */}
          <AlertDialog open={!!kpiToDelete} onOpenChange={() => setKpiToDelete(null)}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>KPIを削除しますか？</AlertDialogTitle>
                <AlertDialogDescription>
                  このKPIを目標から削除します。この操作は取り消せません。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>キャンセル</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => kpiToDelete && handleDeleteKPI(kpiToDelete)}
                  className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                >
                  削除
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>

          {/* 目標削除確認ダイアログ */}
          <AlertDialog open={showDeleteGoalDialog} onOpenChange={setShowDeleteGoalDialog}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>目標を削除しますか？</AlertDialogTitle>
                <AlertDialogDescription>
                  この目標と関連するすべてのKPIが削除されます。この操作は取り消せません。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={isDeleting}>キャンセル</AlertDialogCancel>
                <AlertDialogAction
                  onClick={handleDeleteGoal}
                  disabled={isDeleting}
                  className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                >
                  {isDeleting ? "削除中..." : "削除"}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </TabsContent>

        <TabsContent value="analysis">
          <div className="grid gap-6 lg:grid-cols-3">
            <div className="lg:col-span-2">
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <TrendingUp className="w-5 h-5 text-primary" />
                    目標分析
                  </CardTitle>
                  <CardDescription>
                    AIによる現状分析と達成可能性の評価
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <AIChatPanel
                    initialPrompt={`目標「${goal.name}」の現状を分析してください。業種: ${industry.nameJa}、KPI数: ${goal.kpis.length}、期限: ${goal.deadline}`}
                    workflowType="goal_analysis"
                    context={{ goalId: goal.id, goal }}
                    userId={session?.user?.id}
                    goalId={goal.id}
                  />
                </CardContent>
              </Card>
            </div>
            <div>
              <Card>
                <CardHeader>
                  <CardTitle>分析サマリー</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="p-3 rounded-lg bg-neutral-1">
                    <div className="text-xs text-muted-foreground mb-1">
                      達成予測
                    </div>
                    <div className="font-medium">分析を実行してください</div>
                  </div>
                  <div className="p-3 rounded-lg bg-neutral-1">
                    <div className="text-xs text-muted-foreground mb-1">
                      主な課題
                    </div>
                    <div className="font-medium">-</div>
                  </div>
                  <div className="p-3 rounded-lg bg-neutral-1">
                    <div className="text-xs text-muted-foreground mb-1">
                      推奨アクション
                    </div>
                    <div className="font-medium">-</div>
                  </div>
                </CardContent>
              </Card>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="actions">
          <Card>
            <CardHeader>
              <CardTitle>アクションプラン</CardTitle>
              <CardDescription>
                目標達成に向けた具体的なアクション
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-center py-8 text-muted-foreground">
                <Lightbulb className="w-12 h-12 mx-auto mb-4 opacity-50" />
                <p>まだアクションが設定されていません</p>
                <p className="text-sm mt-2">
                  「施策を提案」からAIに施策を提案してもらいましょう
                </p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
