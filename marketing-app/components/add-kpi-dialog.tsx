"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  getIndustryKPIs,
  KPI_CATEGORIES,
  getBenchmarkValue,
  generateCustomKPIId,
} from "@/lib/marketing/kpi-definitions";
import type { Goal, GoalKPI, KPIDefinition } from "@/lib/shannon/types";
import { Check, Plus } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

interface AddKPIDialogProps {
  goal: Goal;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddKPI: (kpi: GoalKPI) => void;
}

export function AddKPIDialog({ goal, open, onOpenChange, onAddKPI }: AddKPIDialogProps) {
  const [activeTab, setActiveTab] = useState<"predefined" | "custom">("predefined");

  // 事前定義KPI選択用
  const [selectedKPI, setSelectedKPI] = useState<KPIDefinition | null>(null);
  const [currentValue, setCurrentValue] = useState<number>(0);
  const [targetValue, setTargetValue] = useState<number>(0);

  // カスタムKPI用
  const [customKPIName, setCustomKPIName] = useState("");
  const [customKPIUnit, setCustomKPIUnit] = useState("");
  const [customCurrentValue, setCustomCurrentValue] = useState<number>(0);
  const [customTargetValue, setCustomTargetValue] = useState<number>(0);
  const [customLowerIsBetter, setCustomLowerIsBetter] = useState(false);

  // 既に目標に含まれるKPI IDのセット
  const existingKPIIds = new Set(goal.kpis.map((k) => k.kpiId));

  // 該当業種のKPIリスト
  const industryKPIs = getIndustryKPIs(goal.industry);
  const kpisByCategory = Object.entries(KPI_CATEGORIES).map(([category, info]) => ({
    category,
    ...info,
    kpis: industryKPIs.filter((k) => k.category === category),
  }));

  const handleSelectKPI = (kpi: KPIDefinition) => {
    if (existingKPIIds.has(kpi.id)) return;
    setSelectedKPI(kpi);
    setCurrentValue(0);
    setTargetValue(kpi.benchmark || 0);
  };

  const handleAddPredefinedKPI = () => {
    if (!selectedKPI || targetValue <= 0) return;

    const newKPI: GoalKPI = {
      kpiId: selectedKPI.id,
      currentValue,
      targetValue,
      unit: selectedKPI.unit,
    };

    onAddKPI(newKPI);
    resetAndClose();
  };

  const handleAddCustomKPI = () => {
    if (!customKPIName.trim() || !customKPIUnit.trim() || customTargetValue <= 0) return;

    const newKPI: GoalKPI = {
      kpiId: generateCustomKPIId(),
      currentValue: customCurrentValue,
      targetValue: customTargetValue,
      unit: customKPIUnit,
      isCustom: true,
      customName: customKPIName,
      lowerIsBetter: customLowerIsBetter,
    };

    onAddKPI(newKPI);
    resetAndClose();
  };

  const resetAndClose = () => {
    setSelectedKPI(null);
    setCurrentValue(0);
    setTargetValue(0);
    setCustomKPIName("");
    setCustomKPIUnit("");
    setCustomCurrentValue(0);
    setCustomTargetValue(0);
    setCustomLowerIsBetter(false);
    setActiveTab("predefined");
    onOpenChange(false);
  };

  const canAddPredefined = selectedKPI && targetValue > 0;
  const canAddCustom = customKPIName.trim() && customKPIUnit.trim() && customTargetValue > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>KPIを追加</DialogTitle>
          <DialogDescription>
            目標「{goal.name}」に新しいKPIを追加します
          </DialogDescription>
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as "predefined" | "custom")}>
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="predefined">事前定義KPI</TabsTrigger>
            <TabsTrigger value="custom">カスタムKPI</TabsTrigger>
          </TabsList>

          <TabsContent value="predefined" className="space-y-4 mt-4">
            {/* KPI選択リスト */}
            <div className="space-y-4 max-h-[300px] overflow-y-auto">
              {kpisByCategory
                .filter((cat) => cat.kpis.length > 0)
                .map((category) => (
                  <div key={category.category}>
                    <h4 className="font-medium text-sm text-muted-foreground mb-2">
                      {category.nameJa}
                    </h4>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                      {category.kpis.map((kpi) => {
                        const isExisting = existingKPIIds.has(kpi.id);
                        const isSelected = selectedKPI?.id === kpi.id;

                        return (
                          <button
                            key={kpi.id}
                            onClick={() => handleSelectKPI(kpi)}
                            disabled={isExisting}
                            className={cn(
                              "p-3 rounded-lg border text-left transition-all",
                              isExisting
                                ? "border-border bg-muted opacity-50 cursor-not-allowed"
                                : isSelected
                                ? "border-primary bg-primary/5"
                                : "border-border hover:border-primary/50"
                            )}
                          >
                            <div className="flex items-center justify-between">
                              <div className="flex-1 min-w-0">
                                <div className="font-medium text-sm truncate">
                                  {kpi.nameJa}
                                  {isExisting && (
                                    <span className="text-xs text-muted-foreground ml-2">
                                      (追加済み)
                                    </span>
                                  )}
                                </div>
                                <div className="text-xs text-muted-foreground truncate">
                                  {kpi.descriptionJa}
                                </div>
                              </div>
                              {isSelected && (
                                <div className="w-5 h-5 rounded-full bg-primary flex items-center justify-center ml-2 flex-shrink-0">
                                  <Check className="w-3 h-3 text-white" />
                                </div>
                              )}
                            </div>
                            {!isExisting && (
                              <div className="text-xs text-muted-foreground mt-1">
                                ベンチマーク: {getBenchmarkValue(kpi)}
                              </div>
                            )}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                ))}
            </div>

            {/* 値入力フォーム */}
            {selectedKPI && (
              <div className="p-4 rounded-lg border border-primary bg-primary/5 space-y-4">
                <div className="font-medium text-sm">
                  {selectedKPI.nameJa}の目標値を設定
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="currentValue">現在値</Label>
                    <div className="flex items-center gap-2">
                      <Input
                        id="currentValue"
                        type="number"
                        value={currentValue}
                        onChange={(e) => setCurrentValue(parseFloat(e.target.value) || 0)}
                      />
                      <span className="text-sm text-muted-foreground whitespace-nowrap">
                        {selectedKPI.unit}
                      </span>
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="targetValue">目標値</Label>
                    <div className="flex items-center gap-2">
                      <Input
                        id="targetValue"
                        type="number"
                        value={targetValue}
                        onChange={(e) => setTargetValue(parseFloat(e.target.value) || 0)}
                      />
                      <span className="text-sm text-muted-foreground whitespace-nowrap">
                        {selectedKPI.unit}
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            )}

            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                キャンセル
              </Button>
              <Button onClick={handleAddPredefinedKPI} disabled={!canAddPredefined}>
                <Plus className="w-4 h-4 mr-2" />
                追加
              </Button>
            </DialogFooter>
          </TabsContent>

          <TabsContent value="custom" className="space-y-4 mt-4">
            <div className="space-y-4">
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
                  <Label htmlFor="customCurrentValue">現在値</Label>
                  <Input
                    id="customCurrentValue"
                    type="number"
                    value={customCurrentValue}
                    onChange={(e) => setCustomCurrentValue(parseFloat(e.target.value) || 0)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="customTargetValue">目標値</Label>
                  <Input
                    id="customTargetValue"
                    type="number"
                    value={customTargetValue}
                    onChange={(e) => setCustomTargetValue(parseFloat(e.target.value) || 0)}
                  />
                </div>
              </div>
              <div className="flex items-center space-x-2">
                <Checkbox
                  id="lowerIsBetter"
                  checked={customLowerIsBetter}
                  onCheckedChange={(checked) => setCustomLowerIsBetter(checked === true)}
                />
                <Label htmlFor="lowerIsBetter" className="text-sm font-normal cursor-pointer">
                  低いほど良いKPI（例: コスト、解約率）
                </Label>
              </div>
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                キャンセル
              </Button>
              <Button onClick={handleAddCustomKPI} disabled={!canAddCustom}>
                <Plus className="w-4 h-4 mr-2" />
                追加
              </Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
