"use client";

import { useState, useMemo } from "react";
import { useSelector } from "react-redux";
import {
  ChevronDown,
  ChevronUp,
  Star,
  Circle,
  CheckCircle2,
  Loader2,
} from "lucide-react";
import type { RootState } from "@/lib/store";
import type { SwarmTask } from "@/lib/shannon/types";
import { generateAgentColor, LEAD_COLOR } from "@/lib/swarm/agent-colors";

export function SwarmTaskBoard() {
  const [collapsed, setCollapsed] = useState(false);

  const swarm = useSelector((state: RootState) => state.run.swarm);

  const sortedTasks = useMemo(() => {
    if (!swarm?.tasks?.length) return [];
    return [...swarm.tasks].sort((a, b) => a.id.localeCompare(b.id));
  }, [swarm?.tasks]);

  if (!swarm || sortedTasks.length === 0) return null;

  const completedCount = sortedTasks.filter(
    (t) => t.status === "completed"
  ).length;
  const totalCount = sortedTasks.length;

  return (
    <div className="shrink-0 border-y border-border/50 bg-white/[0.04]">
      {/* Section header — matches "EXECUTION TIMELINE" style */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="w-full p-4 pb-3 flex items-center justify-between hover:bg-muted/30 transition-colors"
      >
        <div className="font-medium text-sm text-muted-foreground uppercase tracking-wider">
          Task Board
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground tabular-nums">
            {completedCount}/{totalCount}
          </span>
          {collapsed ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          )}
        </div>
      </button>

      {!collapsed && (
        <div className="px-4 pb-4 space-y-3">
          {/* Lead status */}
          {swarm.leadStatus && (
            <div className="flex items-center gap-2">
              <div
                className="flex h-5 w-5 items-center justify-center rounded-full shrink-0"
                style={{ backgroundColor: LEAD_COLOR.bg, border: `1px solid ${LEAD_COLOR.border}` }}
              >
                <Star className="h-3 w-3" style={{ color: LEAD_COLOR.dot }} />
              </div>
              <span className="text-sm" style={{ color: LEAD_COLOR.text }}>
                {swarm.leadStatus}
              </span>
            </div>
          )}

          {/* Tasks */}
          {sortedTasks.map((task) => (
            <TaskRow
              key={task.id}
              task={task}
              agentRegistry={swarm.agentRegistry}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function TaskRow({
  task,
  agentRegistry,
}: {
  task: SwarmTask;
  agentRegistry: Record<
    string,
    { colorIndex: number; role: string; status: string }
  >;
}) {
  const agent = task.owner ? agentRegistry[task.owner] : null;
  const agentColor = agent ? generateAgentColor(agent.colorIndex) : null;

  return (
    <div className="flex items-start gap-3">
      {/* Status icon — same size/style as Timeline icons */}
      <div
        className="flex h-5 w-5 items-center justify-center rounded-full border bg-background shrink-0 mt-0.5"
        style={agentColor ? { borderColor: agentColor.border, color: agentColor.dot } : undefined}
      >
        {task.status === "completed" && (
          <CheckCircle2 className="h-3.5 w-3.5" style={agentColor ? { color: agentColor.dot } : { color: "hsl(142 70% 50%)" }} />
        )}
        {task.status === "in_progress" && (
          <Loader2 className="h-3.5 w-3.5 animate-spin" style={agentColor ? { color: agentColor.dot } : { color: "hsl(38 92% 60%)" }} />
        )}
        {task.status === "pending" && (
          <Circle className="h-3.5 w-3.5 text-muted-foreground/40" />
        )}
      </div>

      {/* Content */}
      <div className="min-w-0 flex-1 space-y-0.5">
        {/* Task ID + description */}
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-xs text-muted-foreground shrink-0">
            {task.id}
          </span>
          <span className="text-sm text-foreground leading-snug line-clamp-2">
            {task.description}
          </span>
        </div>

        {/* Agent assignment */}
        <div className="text-xs text-muted-foreground">
          {task.owner ? (
            <span className="flex items-center gap-1.5">
              <span
                className="inline-block h-2 w-2 rounded-full shrink-0"
                style={{ backgroundColor: agentColor?.dot ?? "hsl(142 70% 55%)" }}
              />
              <span style={{ color: agentColor?.text ?? "hsl(142 70% 65%)" }}>{task.owner}</span>
              {agent?.role && (
                <span className="opacity-50">({agent.role})</span>
              )}
            </span>
          ) : (
            <span className="opacity-40 italic">unassigned</span>
          )}
        </div>
      </div>
    </div>
  );
}
