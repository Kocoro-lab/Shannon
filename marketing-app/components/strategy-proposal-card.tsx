"use client";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ProposedStrategy } from "@/lib/shannon/types";
import { Check, MessageSquare, ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";

interface StrategyProposalCardProps {
  strategy: ProposedStrategy;
  onRegister: (strategy: ProposedStrategy) => void;
  onRequestRevision: (strategy: ProposedStrategy) => void;
  className?: string;
}

export function StrategyProposalCard({
  strategy,
  onRegister,
  onRequestRevision,
  className,
}: StrategyProposalCardProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <Card className={cn("border-primary/20 bg-primary/5", className)}>
      <CardContent className="py-3 px-4">
        <div className="flex items-start gap-3">
          <div className="flex-1 min-w-0">
            {/* Header */}
            <div className="flex items-center gap-2 mb-1">
              <h4 className="font-medium text-sm truncate">{strategy.name}</h4>
              <ImpactBadge impact={strategy.impact} />
              <EffortBadge effort={strategy.effort} />
            </div>

            {/* Description */}
            <p className="text-xs text-muted-foreground line-clamp-2 mb-2">
              {strategy.description}
            </p>

            {/* Steps (expandable) */}
            {strategy.steps.length > 0 && (
              <div>
                <button
                  onClick={() => setIsExpanded(!isExpanded)}
                  className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  {isExpanded ? (
                    <ChevronUp className="w-3 h-3" />
                  ) : (
                    <ChevronDown className="w-3 h-3" />
                  )}
                  <span>{strategy.steps.length}ステップ</span>
                </button>

                {isExpanded && (
                  <ul className="mt-2 space-y-1 text-xs text-muted-foreground pl-4">
                    {strategy.steps.map((step, index) => (
                      <li key={index} className="list-decimal">
                        {step}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>

          {/* Actions */}
          <div className="flex flex-col gap-1.5 flex-shrink-0">
            <Button
              size="sm"
              variant="default"
              onClick={() => onRegister(strategy)}
              className="h-7 text-xs px-2"
            >
              <Check className="w-3 h-3 mr-1" />
              登録
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => onRequestRevision(strategy)}
              className="h-7 text-xs px-2"
            >
              <MessageSquare className="w-3 h-3 mr-1" />
              修正
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function ImpactBadge({ impact }: { impact: ProposedStrategy["impact"] }) {
  const config = {
    high: { label: "高Impact", class: "bg-green-100 text-green-700" },
    medium: { label: "中Impact", class: "bg-yellow-100 text-yellow-700" },
    low: { label: "低Impact", class: "bg-gray-100 text-gray-700" },
  };
  const { label, class: className } = config[impact];
  return (
    <span
      className={cn(
        "inline-flex px-1.5 py-0.5 rounded text-[10px] font-medium",
        className
      )}
    >
      {label}
    </span>
  );
}

function EffortBadge({ effort }: { effort: ProposedStrategy["effort"] }) {
  const config = {
    high: { label: "高工数", class: "bg-red-100 text-red-700" },
    medium: { label: "中工数", class: "bg-yellow-100 text-yellow-700" },
    low: { label: "低工数", class: "bg-green-100 text-green-700" },
  };
  const { label, class: className } = config[effort];
  return (
    <span
      className={cn(
        "inline-flex px-1.5 py-0.5 rounded text-[10px] font-medium",
        className
      )}
    >
      {label}
    </span>
  );
}

// List container for multiple strategy cards
interface StrategyProposalListProps {
  strategies: ProposedStrategy[];
  onRegister: (strategy: ProposedStrategy) => void;
  onRequestRevision: (strategy: ProposedStrategy) => void;
  className?: string;
}

export function StrategyProposalList({
  strategies,
  onRegister,
  onRequestRevision,
  className,
}: StrategyProposalListProps) {
  if (strategies.length === 0) {
    return null;
  }

  return (
    <div className={cn("space-y-2", className)}>
      <div className="flex items-center gap-2 text-sm text-muted-foreground mb-2">
        <span className="font-medium">提案された施策</span>
        <span className="bg-primary/10 text-primary px-1.5 py-0.5 rounded text-xs font-medium">
          {strategies.length}件
        </span>
      </div>
      {strategies.map((strategy) => (
        <StrategyProposalCard
          key={strategy.id}
          strategy={strategy}
          onRegister={onRegister}
          onRequestRevision={onRequestRevision}
        />
      ))}
    </div>
  );
}
