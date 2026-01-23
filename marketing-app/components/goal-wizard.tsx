"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
  INDUSTRIES,
  KPI_CATEGORIES,
  getIndustryKPIs,
  formatKPIValue,
  getBenchmarkValue,
  generateCustomKPIId,
} from "@/lib/marketing/kpi-definitions";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { createGoal } from "@/lib/marketing/utils";
import { useAppDispatch, useAppSelector } from "@/lib/store";
import {
  setSelectedIndustry,
  setWizardStep,
  nextWizardStep,
  prevWizardStep,
  addGoal,
  setCurrentGoal,
} from "@/lib/store/slices/marketing";
import type { IndustryType, GoalKPI, KPIDefinition } from "@/lib/shannon/types";
import { Check, ChevronLeft, ChevronRight, Target, TrendingUp, Calendar, Plus, X } from "lucide-react";
import { cn } from "@/lib/utils";

const STEPS = [
  { id: 0, name: "業種選択", description: "事業の業種を選択してください" },
  { id: 1, name: "KPI選択", description: "追跡するKPIを選択してください" },
  { id: 2, name: "目標設定", description: "目標値と期限を設定してください" },
  { id: 3, name: "確認", description: "設定内容を確認してください" },
];

interface SelectedKPI {
  kpi: KPIDefinition;
  currentValue: number;
  targetValue: number;
  isCustom?: boolean;
  customName?: string;
}

export function GoalWizard() {
  const router = useRouter();
  const dispatch = useAppDispatch();
  const { selectedIndustry, wizardStep } = useAppSelector((state) => state.marketing);

  const [selectedKPIs, setSelectedKPIs] = useState<SelectedKPI[]>([]);
  const [goalName, setGoalName] = useState("");
  const [deadline, setDeadline] = useState("");

  // カスタムKPI追加ダイアログ用の状態
  const [customKPIDialogOpen, setCustomKPIDialogOpen] = useState(false);
  const [customKPIName, setCustomKPIName] = useState("");
  const [customKPIUnit, setCustomKPIUnit] = useState("");
  const [customKPICurrentValue, setCustomKPICurrentValue] = useState(0);
  const [customKPITargetValue, setCustomKPITargetValue] = useState(0);

  const handleIndustrySelect = (industry: IndustryType) => {
    dispatch(setSelectedIndustry(industry));
    setSelectedKPIs([]);
  };

  const handleKPIToggle = (kpi: KPIDefinition) => {
    const exists = selectedKPIs.find((s) => s.kpi.id === kpi.id);
    if (exists) {
      setSelectedKPIs(selectedKPIs.filter((s) => s.kpi.id !== kpi.id));
    } else {
      setSelectedKPIs([
        ...selectedKPIs,
        {
          kpi,
          currentValue: 0,
          targetValue: kpi.benchmark || 0,
        },
      ]);
    }
  };

  const handleKPIValueChange = (
    kpiId: string,
    field: "currentValue" | "targetValue",
    value: number
  ) => {
    setSelectedKPIs(
      selectedKPIs.map((s) =>
        s.kpi.id === kpiId ? { ...s, [field]: value } : s
      )
    );
  };

  const handleAddCustomKPI = () => {
    if (!customKPIName.trim() || !customKPIUnit.trim()) return;

    const customKPIId = generateCustomKPIId();
    const customKPI: KPIDefinition = {
      id: customKPIId,
      name: customKPIName,
      nameJa: customKPIName,
      category: "revenue", // デフォルトカテゴリ
      unit: customKPIUnit,
      description: "カスタムKPI",
      descriptionJa: "カスタムKPI",
    };

    setSelectedKPIs([
      ...selectedKPIs,
      {
        kpi: customKPI,
        currentValue: customKPICurrentValue,
        targetValue: customKPITargetValue,
        isCustom: true,
        customName: customKPIName,
      },
    ]);

    // ダイアログをリセット
    setCustomKPIName("");
    setCustomKPIUnit("");
    setCustomKPICurrentValue(0);
    setCustomKPITargetValue(0);
    setCustomKPIDialogOpen(false);
  };

  const handleRemoveKPI = (kpiId: string) => {
    setSelectedKPIs(selectedKPIs.filter((s) => s.kpi.id !== kpiId));
  };

  const handleNext = () => {
    dispatch(nextWizardStep());
  };

  const handlePrev = () => {
    dispatch(prevWizardStep());
  };

  const handleSubmit = () => {
    if (!selectedIndustry) return;

    const kpis: GoalKPI[] = selectedKPIs.map((s) => ({
      kpiId: s.kpi.id,
      currentValue: s.currentValue,
      targetValue: s.targetValue,
      unit: s.kpi.unit,
      ...(s.isCustom && {
        isCustom: true,
        customName: s.customName || s.kpi.nameJa,
      }),
    }));

    const goal = createGoal({
      name: goalName || `${INDUSTRIES[selectedIndustry].nameJa}の目標`,
      industry: selectedIndustry,
      kpis,
      deadline,
    });

    dispatch(addGoal(goal));
    dispatch(setCurrentGoal(goal));
    router.push(`/goals/${goal.id}`);
  };

  const canProceed = () => {
    switch (wizardStep) {
      case 0:
        return selectedIndustry !== null;
      case 1:
        return selectedKPIs.length > 0;
      case 2:
        return deadline !== "" && selectedKPIs.every((s) => s.targetValue > 0);
      case 3:
        return true;
      default:
        return false;
    }
  };

  const industryKPIs = selectedIndustry ? getIndustryKPIs(selectedIndustry) : [];
  const kpisByCategory = Object.entries(KPI_CATEGORIES).map(([category, info]) => ({
    category,
    ...info,
    kpis: industryKPIs.filter((k) => k.category === category),
  }));

  return (
    <div className="max-w-4xl mx-auto">
      {/* Progress Steps */}
      <div className="mb-8">
        <div className="flex items-center justify-between">
          {STEPS.map((step, index) => (
            <div key={step.id} className="flex items-center">
              <div
                className={cn(
                  "flex items-center justify-center w-10 h-10 rounded-full border-2 transition-all",
                  wizardStep > step.id
                    ? "bg-primary border-primary text-white"
                    : wizardStep === step.id
                    ? "border-primary text-primary"
                    : "border-border text-muted-foreground"
                )}
              >
                {wizardStep > step.id ? (
                  <Check className="w-5 h-5" />
                ) : (
                  <span className="text-sm font-medium">{step.id + 1}</span>
                )}
              </div>
              {index < STEPS.length - 1 && (
                <div
                  className={cn(
                    "w-20 h-1 mx-2",
                    wizardStep > step.id ? "bg-primary" : "bg-border"
                  )}
                />
              )}
            </div>
          ))}
        </div>
        <div className="flex justify-between mt-2">
          {STEPS.map((step) => (
            <div
              key={step.id}
              className={cn(
                "text-xs text-center w-24",
                wizardStep === step.id
                  ? "text-primary font-medium"
                  : "text-muted-foreground"
              )}
            >
              {step.name}
            </div>
          ))}
        </div>
      </div>

      {/* Step Content */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            {wizardStep === 0 && <Target className="w-5 h-5 text-primary" />}
            {wizardStep === 1 && <TrendingUp className="w-5 h-5 text-primary" />}
            {wizardStep === 2 && <Calendar className="w-5 h-5 text-primary" />}
            {wizardStep === 3 && <Check className="w-5 h-5 text-primary" />}
            {STEPS[wizardStep].name}
          </CardTitle>
          <CardDescription>{STEPS[wizardStep].description}</CardDescription>
        </CardHeader>
        <CardContent>
          {/* Step 0: Industry Selection */}
          {wizardStep === 0 && (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {Object.entries(INDUSTRIES).map(([key, industry]) => (
                <button
                  key={key}
                  onClick={() => handleIndustrySelect(key as IndustryType)}
                  className={cn(
                    "p-4 rounded-lg border-2 text-left transition-all hover:shadow-md",
                    selectedIndustry === key
                      ? "border-primary bg-primary/5"
                      : "border-border hover:border-primary/50"
                  )}
                >
                  <div className="font-medium text-sm">{industry.nameJa}</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    {industry.description}
                  </div>
                </button>
              ))}
            </div>
          )}

          {/* Step 1: KPI Selection */}
          {wizardStep === 1 && selectedIndustry && (
            <div className="space-y-6">
              {kpisByCategory
                .filter((cat) => cat.kpis.length > 0)
                .map((category) => (
                  <div key={category.category}>
                    <h4 className="font-medium text-sm text-muted-foreground mb-3">
                      {category.nameJa}
                    </h4>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                      {category.kpis.map((kpi) => {
                        const isSelected = selectedKPIs.some(
                          (s) => s.kpi.id === kpi.id
                        );
                        return (
                          <button
                            key={kpi.id}
                            onClick={() => handleKPIToggle(kpi)}
                            className={cn(
                              "p-3 rounded-lg border text-left transition-all",
                              isSelected
                                ? "border-primary bg-primary/5"
                                : "border-border hover:border-primary/50"
                            )}
                          >
                            <div className="flex items-center justify-between">
                              <div>
                                <div className="font-medium text-sm">
                                  {kpi.nameJa}
                                </div>
                                <div className="text-xs text-muted-foreground">
                                  {kpi.descriptionJa}
                                </div>
                              </div>
                              <div
                                className={cn(
                                  "w-5 h-5 rounded-full border-2 flex items-center justify-center",
                                  isSelected
                                    ? "border-primary bg-primary"
                                    : "border-border"
                                )}
                              >
                                {isSelected && (
                                  <Check className="w-3 h-3 text-white" />
                                )}
                              </div>
                            </div>
                            <div className="text-xs text-muted-foreground mt-2">
                              ベンチマーク: {getBenchmarkValue(kpi)}
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                ))}

              {/* カスタムKPI追加セクション */}
              <div className="pt-4 border-t border-border">
                <h4 className="font-medium text-sm text-muted-foreground mb-3">
                  カスタムKPI
                </h4>

                {/* 追加済みカスタムKPIの表示 */}
                {selectedKPIs.filter((s) => s.isCustom).length > 0 && (
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-4">
                    {selectedKPIs
                      .filter((s) => s.isCustom)
                      .map((item) => (
                        <div
                          key={item.kpi.id}
                          className="p-3 rounded-lg border border-primary bg-primary/5 text-left"
                        >
                          <div className="flex items-center justify-between">
                            <div>
                              <div className="font-medium text-sm">
                                {item.customName}
                              </div>
                              <div className="text-xs text-muted-foreground">
                                カスタムKPI（単位: {item.kpi.unit}）
                              </div>
                            </div>
                            <button
                              onClick={() => handleRemoveKPI(item.kpi.id)}
                              className="p-1 hover:bg-muted rounded"
                              aria-label="削除"
                            >
                              <X className="w-4 h-4 text-muted-foreground" />
                            </button>
                          </div>
                        </div>
                      ))}
                  </div>
                )}

                {/* カスタムKPI追加ダイアログ */}
                <Dialog open={customKPIDialogOpen} onOpenChange={setCustomKPIDialogOpen}>
                  <DialogTrigger asChild>
                    <Button variant="outline" className="w-full">
                      <Plus className="w-4 h-4 mr-2" />
                      カスタムKPIを追加
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>カスタムKPIを追加</DialogTitle>
                      <DialogDescription>
                        追跡したい独自のKPIを設定してください
                      </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4 py-4">
                      <div className="space-y-2">
                        <Label htmlFor="customKPIName">KPI名</Label>
                        <Input
                          id="customKPIName"
                          placeholder="例: メール開封率"
                          value={customKPIName}
                          onChange={(e) => setCustomKPIName(e.target.value)}
                        />
                      </div>
                      <div className="space-y-2">
                        <Label htmlFor="customKPIUnit">単位</Label>
                        <Input
                          id="customKPIUnit"
                          placeholder="例: %, 円, 件"
                          value={customKPIUnit}
                          onChange={(e) => setCustomKPIUnit(e.target.value)}
                        />
                      </div>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                          <Label htmlFor="customKPICurrentValue">現在値</Label>
                          <Input
                            id="customKPICurrentValue"
                            type="number"
                            value={customKPICurrentValue}
                            onChange={(e) =>
                              setCustomKPICurrentValue(parseFloat(e.target.value) || 0)
                            }
                          />
                        </div>
                        <div className="space-y-2">
                          <Label htmlFor="customKPITargetValue">目標値</Label>
                          <Input
                            id="customKPITargetValue"
                            type="number"
                            value={customKPITargetValue}
                            onChange={(e) =>
                              setCustomKPITargetValue(parseFloat(e.target.value) || 0)
                            }
                          />
                        </div>
                      </div>
                    </div>
                    <DialogFooter>
                      <Button
                        variant="outline"
                        onClick={() => setCustomKPIDialogOpen(false)}
                      >
                        キャンセル
                      </Button>
                      <Button
                        onClick={handleAddCustomKPI}
                        disabled={!customKPIName.trim() || !customKPIUnit.trim()}
                      >
                        追加
                      </Button>
                    </DialogFooter>
                  </DialogContent>
                </Dialog>
              </div>
            </div>
          )}

          {/* Step 2: Target Setting */}
          {wizardStep === 2 && (
            <div className="space-y-6">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="goalName">目標名</Label>
                  <Input
                    id="goalName"
                    placeholder="例: Q1売上目標"
                    value={goalName}
                    onChange={(e) => setGoalName(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="deadline">目標達成期限</Label>
                  <Input
                    id="deadline"
                    type="date"
                    value={deadline}
                    onChange={(e) => setDeadline(e.target.value)}
                  />
                </div>
              </div>

              <div className="space-y-4">
                <Label>KPI目標値</Label>
                {selectedKPIs.map((item) => (
                  <div
                    key={item.kpi.id}
                    className="p-4 rounded-lg border border-border bg-neutral-1"
                  >
                    <div className="font-medium text-sm mb-3">
                      {item.kpi.nameJa}
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <Label className="text-xs">現在値</Label>
                        <div className="flex items-center gap-2">
                          <Input
                            type="number"
                            value={item.currentValue}
                            onChange={(e) =>
                              handleKPIValueChange(
                                item.kpi.id,
                                "currentValue",
                                parseFloat(e.target.value) || 0
                              )
                            }
                          />
                          <span className="text-sm text-muted-foreground whitespace-nowrap">
                            {item.kpi.unit}
                          </span>
                        </div>
                      </div>
                      <div className="space-y-2">
                        <Label className="text-xs">目標値</Label>
                        <div className="flex items-center gap-2">
                          <Input
                            type="number"
                            value={item.targetValue}
                            onChange={(e) =>
                              handleKPIValueChange(
                                item.kpi.id,
                                "targetValue",
                                parseFloat(e.target.value) || 0
                              )
                            }
                          />
                          <span className="text-sm text-muted-foreground whitespace-nowrap">
                            {item.kpi.unit}
                          </span>
                        </div>
                      </div>
                    </div>
                    {!item.isCustom && (
                      <div className="mt-2 text-xs text-muted-foreground">
                        ベンチマーク: {getBenchmarkValue(item.kpi)}
                      </div>
                    )}
                    {item.isCustom && (
                      <div className="mt-2 text-xs text-muted-foreground">
                        カスタムKPI
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Step 3: Confirmation */}
          {wizardStep === 3 && selectedIndustry && (
            <div className="space-y-6">
              <div className="p-4 rounded-lg border border-border bg-neutral-1">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <div className="text-xs text-muted-foreground">目標名</div>
                    <div className="font-medium">
                      {goalName || `${INDUSTRIES[selectedIndustry].nameJa}の目標`}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground">業種</div>
                    <div className="font-medium">
                      {INDUSTRIES[selectedIndustry].nameJa}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground">期限</div>
                    <div className="font-medium">
                      {deadline
                        ? new Date(deadline).toLocaleDateString("ja-JP")
                        : "-"}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground">KPI数</div>
                    <div className="font-medium">{selectedKPIs.length}件</div>
                  </div>
                </div>
              </div>

              <div>
                <Label className="mb-3 block">選択したKPI</Label>
                <div className="space-y-2">
                  {selectedKPIs.map((item) => {
                    const progress =
                      item.targetValue > 0
                        ? (item.currentValue / item.targetValue) * 100
                        : 0;
                    return (
                      <div
                        key={item.kpi.id}
                        className="p-3 rounded-lg border border-border"
                      >
                        <div className="flex items-center justify-between mb-2">
                          <span className="font-medium text-sm">
                            {item.kpi.nameJa}
                          </span>
                          <span className="text-sm text-muted-foreground">
                            {formatKPIValue(item.kpi, item.currentValue)} →{" "}
                            {formatKPIValue(item.kpi, item.targetValue)}
                          </span>
                        </div>
                        <Progress value={Math.min(100, progress)} />
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Navigation */}
      <div className="flex justify-between mt-6">
        <Button
          variant="outline"
          onClick={handlePrev}
          disabled={wizardStep === 0}
        >
          <ChevronLeft className="w-4 h-4 mr-2" />
          戻る
        </Button>
        {wizardStep < 3 ? (
          <Button onClick={handleNext} disabled={!canProceed()}>
            次へ
            <ChevronRight className="w-4 h-4 ml-2" />
          </Button>
        ) : (
          <Button onClick={handleSubmit}>
            目標を作成
            <Check className="w-4 h-4 ml-2" />
          </Button>
        )}
      </div>
    </div>
  );
}
