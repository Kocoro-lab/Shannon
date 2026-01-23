"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useSession } from "next-auth/react";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { AIChatPanel } from "@/components/ai-chat-panel";
import { EditableField, EditableRow } from "@/components/editable-field";
import { useAppSelector, useAppDispatch } from "@/lib/store";
import { updateStrategy, removeStrategy } from "@/lib/store/slices/marketing";
import { deleteStrategy } from "@/lib/supabase/strategies";
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
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Strategy } from "@/lib/shannon/types";
import {
  ArrowLeft,
  Lightbulb,
  CheckCircle,
  Circle,
  Clock,
  Play,
  Calendar,
  FileText,
  Trash2,
} from "lucide-react";
import { cn } from "@/lib/utils";

export default function StrategyDetailPage() {
  const params = useParams();
  const router = useRouter();
  const { data: session } = useSession();
  const strategyId = params.id as string;
  const { strategies, goals } = useAppSelector((state) => state.marketing);
  const dispatch = useAppDispatch();
  const strategy = strategies.find((s) => s.id === strategyId);
  const goal = strategy ? goals.find((g) => g.id === strategy.goalId) : null;

  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);

  const handleUpdateStrategy = (updates: Partial<Strategy>) => {
    if (strategy) {
      dispatch(
        updateStrategy({
          ...strategy,
          ...updates,
          updatedAt: new Date().toISOString(),
        })
      );
    }
  };

  const handleDeleteStrategy = async () => {
    if (!strategy || !session?.user?.id) return;
    setIsDeleting(true);
    try {
      await deleteStrategy(strategy.id, session.user.id);
      dispatch(removeStrategy(strategy.id));
      router.push("/strategies");
    } catch (error) {
      throw new Error("施策の削除に失敗しました");
    } finally {
      setIsDeleting(false);
      setShowDeleteDialog(false);
    }
  };

  if (!strategy) {
    return (
      <div className="container-kocoro py-8">
        <Card className="text-center py-12">
          <CardContent>
            <Lightbulb className="w-12 h-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="font-serif text-lg font-medium mb-2">
              施策が見つかりません
            </h3>
            <p className="text-muted-foreground mb-6">
              指定された施策は存在しないか、削除されました
            </p>
            <Link href="/strategies">
              <Button variant="outline">
                <ArrowLeft className="w-4 h-4 mr-2" />
                施策一覧に戻る
              </Button>
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  const completedTasks = strategy.tasks.filter(
    (t) => t.status === "completed"
  ).length;
  const progress =
    strategy.tasks.length > 0
      ? (completedTasks / strategy.tasks.length) * 100
      : 0;

  return (
    <div className="container-kocoro py-8">
      {/* Header */}
      <div className="mb-8">
        <Link
          href="/strategies"
          className="inline-flex items-center text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ArrowLeft className="w-4 h-4 mr-1" />
          施策一覧
        </Link>
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <EditableField
                value={strategy.name}
                onSave={(value) => handleUpdateStrategy({ name: String(value) })}
                type="text"
                displayClassName="font-serif text-3xl font-bold"
              />
              <StatusBadge status={strategy.status} />
            </div>
            <EditableField
              value={strategy.description}
              onSave={(value) => handleUpdateStrategy({ description: String(value) })}
              type="textarea"
              displayClassName="text-muted-foreground"
            />
            {goal && (
              <Link
                href={`/goals/${goal.id}`}
                className="text-sm text-primary hover:underline mt-2 inline-block"
              >
                目標: {goal.name}
              </Link>
            )}
          </div>
          <div className="flex gap-2">
            {strategy.status === "proposed" && (
              <Button>承認して開始</Button>
            )}
            {strategy.status === "approved" && (
              <Button>
                <Play className="w-4 h-4 mr-2" />
                実行開始
              </Button>
            )}
            <Button
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => setShowDeleteDialog(true)}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              削除
            </Button>
          </div>
        </div>
      </div>

      {/* Progress Overview */}
      <Card className="mb-6">
        <CardContent className="py-4">
          <div className="flex items-center gap-6">
            <div className="flex-1">
              <div className="flex justify-between text-sm mb-2">
                <span className="text-muted-foreground">進捗</span>
                <span className="font-medium">{Math.round(progress)}%</span>
              </div>
              <Progress value={progress} />
            </div>
            <div className="flex gap-4 text-sm">
              <div className="text-center">
                <div className="font-semibold text-green-600">
                  {completedTasks}
                </div>
                <div className="text-muted-foreground">完了</div>
              </div>
              <div className="text-center">
                <div className="font-semibold text-primary">
                  {strategy.tasks.filter((t) => t.status === "in_progress").length}
                </div>
                <div className="text-muted-foreground">進行中</div>
              </div>
              <div className="text-center">
                <div className="font-semibold text-muted-foreground">
                  {strategy.tasks.filter((t) => t.status === "pending").length}
                </div>
                <div className="text-muted-foreground">未着手</div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Main Content */}
      <div className="grid gap-6 lg:grid-cols-3">
        <div className="lg:col-span-2 space-y-6">
          {/* Task List */}
          <Card>
            <CardHeader>
              <CardTitle>タスク一覧</CardTitle>
              <CardDescription>
                施策実行に必要なタスク
              </CardDescription>
            </CardHeader>
            <CardContent>
              {strategy.tasks.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  <Clock className="w-12 h-12 mx-auto mb-4 opacity-50" />
                  <p>タスクがまだ設定されていません</p>
                  <p className="text-sm mt-2">
                    AI実行プラン機能でタスクを生成しましょう
                  </p>
                </div>
              ) : (
                <div className="space-y-3">
                  {strategy.tasks.map((task) => (
                    <div
                      key={task.id}
                      className="flex items-start gap-3 p-3 rounded-lg border border-border hover:bg-neutral-1 transition-colors"
                    >
                      <TaskStatusIcon status={task.status} />
                      <div className="flex-1 min-w-0">
                        <div className="font-medium">{task.name}</div>
                        <div className="text-sm text-muted-foreground">
                          {task.description}
                        </div>
                        {task.dueDate && (
                          <div className="flex items-center gap-1 text-xs text-muted-foreground mt-1">
                            <Calendar className="w-3 h-3" />
                            {new Date(task.dueDate).toLocaleDateString("ja-JP")}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Research Background */}
          {strategy.researchBackground && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <FileText className="w-5 h-5 text-primary" />
                  施策背景の調査内容
                </CardTitle>
                <CardDescription>
                  この施策を提案した際のリサーチ結果
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="prose prose-sm max-w-none">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {strategy.researchBackground}
                  </ReactMarkdown>
                </div>
              </CardContent>
            </Card>
          )}
        </div>

        <div className="space-y-6">
          {/* Strategy Details */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">施策詳細</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <EditableRow
                label="インパクト"
                value={strategy.impact}
                onSave={(value) => handleUpdateStrategy({ impact: value as Strategy["impact"] })}
                type="select"
                options={[
                  { value: "high", label: "高" },
                  { value: "medium", label: "中" },
                  { value: "low", label: "低" },
                ]}
              />
              <EditableRow
                label="工数"
                value={strategy.effort}
                onSave={(value) => handleUpdateStrategy({ effort: value as Strategy["effort"] })}
                type="select"
                options={[
                  { value: "high", label: "高" },
                  { value: "medium", label: "中" },
                  { value: "low", label: "低" },
                ]}
              />
              <EditableRow
                label="ステータス"
                value={strategy.status}
                onSave={(value) => handleUpdateStrategy({ status: value as Strategy["status"] })}
                type="select"
                options={[
                  { value: "proposed", label: "提案中" },
                  { value: "approved", label: "承認済" },
                  { value: "in_progress", label: "進行中" },
                  { value: "completed", label: "完了" },
                  { value: "rejected", label: "却下" },
                ]}
              />
              <EditableRow
                label="優先度"
                value={strategy.priority}
                onSave={(value) => handleUpdateStrategy({ priority: Number(value) })}
                type="number"
              />
              <DetailRow
                label="作成日"
                value={new Date(strategy.createdAt).toLocaleDateString("ja-JP")}
              />
            </CardContent>
          </Card>

          {/* AI Execution Plan */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">AI実行プラン</CardTitle>
              <CardDescription>
                AIによる詳細な実行プランの生成
              </CardDescription>
            </CardHeader>
            <CardContent>
              <AIChatPanel
                initialPrompt={`施策「${strategy.name}」の詳細な実行プランを作成してください。具体的なタスク、スケジュール、必要なリソース、リスクと対策を含めてください。`}
                workflowType="execution_planning"
                context={{
                  strategyId: strategy.id,
                  strategyName: strategy.name,
                  goalId: goal?.id,
                }}
                userId={session?.user?.id}
                goalId={goal?.id}
              />
            </CardContent>
          </Card>
        </div>
      </div>

      {/* 施策削除確認ダイアログ */}
      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>施策を削除しますか？</AlertDialogTitle>
            <AlertDialogDescription>
              この施策と関連するすべてのタスクが削除されます。この操作は取り消せません。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>キャンセル</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteStrategy}
              disabled={isDeleting}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {isDeleting ? "削除中..." : "削除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { label: string; class: string }> = {
    proposed: { label: "提案中", class: "bg-yellow-100 text-yellow-700" },
    approved: { label: "承認済", class: "bg-blue-100 text-blue-700" },
    in_progress: { label: "進行中", class: "bg-primary/10 text-primary" },
    completed: { label: "完了", class: "bg-green-100 text-green-700" },
    rejected: { label: "却下", class: "bg-gray-100 text-gray-700" },
  };

  const { label, class: className } = config[status] || config.proposed;

  return (
    <span
      className={cn(
        "inline-flex px-2 py-1 rounded-full text-xs font-medium",
        className
      )}
    >
      {label}
    </span>
  );
}

function TaskStatusIcon({ status }: { status: string }) {
  switch (status) {
    case "completed":
      return <CheckCircle className="w-5 h-5 text-green-600 flex-shrink-0" />;
    case "in_progress":
      return (
        <div className="w-5 h-5 flex-shrink-0 rounded-full border-2 border-primary border-t-transparent animate-spin" />
      );
    default:
      return <Circle className="w-5 h-5 text-muted-foreground flex-shrink-0" />;
  }
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between p-2 rounded-lg bg-neutral-1">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-sm font-medium capitalize">{value}</span>
    </div>
  );
}
