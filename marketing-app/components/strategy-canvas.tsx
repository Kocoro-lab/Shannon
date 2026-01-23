"use client";

import { useMemo } from "react";
import Link from "next/link";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { useAppDispatch } from "@/lib/store";
import { updateStrategy } from "@/lib/store/slices/marketing";
import type { Strategy } from "@/lib/shannon/types";
import { Lightbulb, Zap, Scale, Timer, ExternalLink } from "lucide-react";

interface StrategyCanvasProps {
  strategies: Strategy[];
  onStrategyClick?: (strategy: Strategy) => void;
  className?: string;
}

export function StrategyCanvas({
  strategies,
  onStrategyClick,
  className,
}: StrategyCanvasProps) {
  // Group strategies by impact/effort quadrant
  const quadrants = useMemo(() => {
    return {
      quickWins: strategies.filter(
        (s) => s.impact === "high" && s.effort === "low"
      ),
      majorProjects: strategies.filter(
        (s) => s.impact === "high" && s.effort === "high"
      ),
      fillIns: strategies.filter(
        (s) => s.impact === "low" && s.effort === "low"
      ),
      thankless: strategies.filter(
        (s) => s.impact === "low" && s.effort === "high"
      ),
      medium: strategies.filter(
        (s) => s.impact === "medium" || s.effort === "medium"
      ),
    };
  }, [strategies]);

  return (
    <div className={cn("grid grid-cols-2 gap-4", className)}>
      {/* Quick Wins (High Impact, Low Effort) */}
      <QuadrantCard
        title="Quick Wins"
        subtitle="高インパクト・低工数"
        icon={<Zap className="w-5 h-5" />}
        strategies={quadrants.quickWins}
        colorClass="bg-green-50 border-green-200"
        onStrategyClick={onStrategyClick}
      />

      {/* Major Projects (High Impact, High Effort) */}
      <QuadrantCard
        title="Major Projects"
        subtitle="高インパクト・高工数"
        icon={<Lightbulb className="w-5 h-5" />}
        strategies={quadrants.majorProjects}
        colorClass="bg-blue-50 border-blue-200"
        onStrategyClick={onStrategyClick}
      />

      {/* Fill-Ins (Low Impact, Low Effort) */}
      <QuadrantCard
        title="Fill-Ins"
        subtitle="低インパクト・低工数"
        icon={<Timer className="w-5 h-5" />}
        strategies={quadrants.fillIns}
        colorClass="bg-yellow-50 border-yellow-200"
        onStrategyClick={onStrategyClick}
      />

      {/* Thankless Tasks (Low Impact, High Effort) */}
      <QuadrantCard
        title="Thankless Tasks"
        subtitle="低インパクト・高工数"
        icon={<Scale className="w-5 h-5" />}
        strategies={quadrants.thankless}
        colorClass="bg-gray-50 border-gray-200"
        onStrategyClick={onStrategyClick}
      />
    </div>
  );
}

interface QuadrantCardProps {
  title: string;
  subtitle: string;
  icon: React.ReactNode;
  strategies: Strategy[];
  colorClass: string;
  onStrategyClick?: (strategy: Strategy) => void;
}

function QuadrantCard({
  title,
  subtitle,
  icon,
  strategies,
  colorClass,
  onStrategyClick,
}: QuadrantCardProps) {
  const dispatch = useAppDispatch();

  const handleStatusChange = (strategy: Strategy, newStatus: Strategy["status"]) => {
    dispatch(
      updateStrategy({
        ...strategy,
        status: newStatus,
        updatedAt: new Date().toISOString(),
      })
    );
  };

  return (
    <Card className={cn("border-2", colorClass)}>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          <div>
            <div>{title}</div>
            <div className="text-xs font-normal text-muted-foreground">
              {subtitle}
            </div>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {strategies.length === 0 ? (
          <p className="text-xs text-muted-foreground text-center py-4">
            施策がありません
          </p>
        ) : (
          <div className="space-y-2">
            {strategies.map((strategy) => (
              <div
                key={strategy.id}
                className="p-2 rounded-md bg-white/70 hover:bg-white transition-colors border border-transparent hover:border-border"
              >
                <div className="flex items-start justify-between gap-2">
                  <Link
                    href={`/strategies/${strategy.id}`}
                    className="flex-1 cursor-pointer"
                    onClick={(e) => {
                      if (onStrategyClick) {
                        e.preventDefault();
                        onStrategyClick(strategy);
                      }
                    }}
                  >
                    <div className="font-medium text-sm truncate hover:text-primary">
                      {strategy.name}
                    </div>
                    <div className="text-xs text-muted-foreground truncate">
                      {strategy.description}
                    </div>
                  </Link>
                  <Link
                    href={`/strategies/${strategy.id}`}
                    className="text-muted-foreground hover:text-primary flex-shrink-0"
                  >
                    <ExternalLink className="w-3 h-3" />
                  </Link>
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <Select
                    value={strategy.status}
                    onValueChange={(value) => handleStatusChange(strategy, value as Strategy["status"])}
                  >
                    <SelectTrigger className="h-6 text-xs w-[90px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="proposed">提案中</SelectItem>
                      <SelectItem value="approved">承認済</SelectItem>
                      <SelectItem value="in_progress">進行中</SelectItem>
                      <SelectItem value="completed">完了</SelectItem>
                      <SelectItem value="rejected">却下</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// Strategy List Component
interface StrategyListProps {
  strategies: Strategy[];
  onStrategyClick?: (strategy: Strategy) => void;
  className?: string;
}

export function StrategyList({
  strategies,
  onStrategyClick,
  className,
}: StrategyListProps) {
  const dispatch = useAppDispatch();
  const sortedStrategies = useMemo(() => {
    return [...strategies].sort((a, b) => a.priority - b.priority);
  }, [strategies]);

  const handleStatusChange = (strategy: Strategy, newStatus: Strategy["status"]) => {
    dispatch(
      updateStrategy({
        ...strategy,
        status: newStatus,
        updatedAt: new Date().toISOString(),
      })
    );
  };

  const handlePriorityChange = (strategy: Strategy, newPriority: number) => {
    dispatch(
      updateStrategy({
        ...strategy,
        priority: newPriority,
        updatedAt: new Date().toISOString(),
      })
    );
  };

  return (
    <div className={cn("space-y-3", className)}>
      {sortedStrategies.map((strategy, index) => (
        <Card
          key={strategy.id}
          className="hover:shadow-md transition-shadow"
        >
          <CardContent className="py-4">
            <div className="flex items-start gap-4">
              <div className="flex-shrink-0 w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center text-primary font-medium text-sm">
                {index + 1}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  <Link
                    href={`/strategies/${strategy.id}`}
                    className="font-medium truncate hover:text-primary cursor-pointer"
                    onClick={(e) => {
                      if (onStrategyClick) {
                        e.preventDefault();
                        onStrategyClick(strategy);
                      }
                    }}
                  >
                    {strategy.name}
                  </Link>
                  <Link
                    href={`/strategies/${strategy.id}`}
                    className="text-muted-foreground hover:text-primary"
                  >
                    <ExternalLink className="w-3 h-3" />
                  </Link>
                </div>
                <p className="text-sm text-muted-foreground line-clamp-2">
                  {strategy.description}
                </p>
                <div className="flex items-center gap-4 mt-2 text-xs text-muted-foreground">
                  <span>
                    インパクト:{" "}
                    <ImpactLabel impact={strategy.impact} />
                  </span>
                  <span>
                    工数:{" "}
                    <EffortLabel effort={strategy.effort} />
                  </span>
                  <span>タスク: {strategy.tasks.length}件</span>
                </div>
                <div className="flex items-center gap-3 mt-3">
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">ステータス:</span>
                    <Select
                      value={strategy.status}
                      onValueChange={(value) => handleStatusChange(strategy, value as Strategy["status"])}
                    >
                      <SelectTrigger className="h-7 text-xs w-[100px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="proposed">提案中</SelectItem>
                        <SelectItem value="approved">承認済</SelectItem>
                        <SelectItem value="in_progress">進行中</SelectItem>
                        <SelectItem value="completed">完了</SelectItem>
                        <SelectItem value="rejected">却下</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">優先度:</span>
                    <Select
                      value={String(strategy.priority)}
                      onValueChange={(value) => handlePriorityChange(strategy, Number(value))}
                    >
                      <SelectTrigger className="h-7 text-xs w-[70px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {[1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map((p) => (
                          <SelectItem key={p} value={String(p)}>
                            {p}位
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

function ImpactLabel({ impact }: { impact: Strategy["impact"] }) {
  const config = {
    high: { label: "高", class: "text-green-600" },
    medium: { label: "中", class: "text-yellow-600" },
    low: { label: "低", class: "text-gray-600" },
  };
  const { label, class: className } = config[impact];
  return <span className={cn("font-medium", className)}>{label}</span>;
}

function EffortLabel({ effort }: { effort: Strategy["effort"] }) {
  const config = {
    high: { label: "高", class: "text-red-600" },
    medium: { label: "中", class: "text-yellow-600" },
    low: { label: "低", class: "text-green-600" },
  };
  const { label, class: className } = config[effort];
  return <span className={cn("font-medium", className)}>{label}</span>;
}
