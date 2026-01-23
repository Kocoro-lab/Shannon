"use client";

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  Cell,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Goal, GoalKPI } from "@/lib/shannon/types";
import { getKPIById, formatKPIValue } from "@/lib/marketing/kpi-definitions";

interface BenchmarkChartProps {
  goal: Goal;
  className?: string;
}

export function BenchmarkChart({ goal, className }: BenchmarkChartProps) {
  const data = goal.kpis
    .map((kpi) => {
      const kpiDef = getKPIById(goal.industry, kpi.kpiId);
      if (!kpiDef) return null;

      const benchmark = kpiDef.benchmark || kpiDef.benchmarkRange?.low || 0;
      const current = kpi.currentValue;
      const target = kpi.targetValue;

      return {
        name: kpiDef.nameJa,
        kpiId: kpi.kpiId,
        current: normalizeValue(current, kpiDef.unit),
        benchmark: normalizeValue(benchmark, kpiDef.unit),
        target: normalizeValue(target, kpiDef.unit),
        unit: kpiDef.unit,
        rawCurrent: current,
        rawBenchmark: benchmark,
        rawTarget: target,
      };
    })
    .filter(Boolean);

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>ベンチマーク比較</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={300}>
          <BarChart
            data={data}
            layout="vertical"
            margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
          >
            <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
            <XAxis type="number" stroke="#71717a" fontSize={12} />
            <YAxis
              type="category"
              dataKey="name"
              stroke="#71717a"
              fontSize={12}
              width={100}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "#ffffff",
                border: "1px solid #e4e4e7",
                borderRadius: "8px",
              }}
              formatter={(value: number, name: string, props: any) => {
                const item = props.payload;
                const raw =
                  name === "current"
                    ? item.rawCurrent
                    : name === "benchmark"
                    ? item.rawBenchmark
                    : item.rawTarget;
                return [
                  `${raw.toLocaleString()} ${item.unit}`,
                  name === "current"
                    ? "現在値"
                    : name === "benchmark"
                    ? "業界平均"
                    : "目標値",
                ];
              }}
            />
            <Legend
              formatter={(value) =>
                value === "current"
                  ? "現在値"
                  : value === "benchmark"
                  ? "業界平均"
                  : "目標値"
              }
            />
            <Bar dataKey="current" fill="#fe2f5c" name="current" radius={[0, 4, 4, 0]} />
            <Bar dataKey="benchmark" fill="#94a3b8" name="benchmark" radius={[0, 4, 4, 0]} />
            <Bar dataKey="target" fill="#22c55e" name="target" radius={[0, 4, 4, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

// Normalize values to percentage for comparison
function normalizeValue(value: number, unit: string): number {
  // For percentage values, return as-is
  if (unit === "%" || unit === "倍") {
    return value;
  }
  // For absolute values, we'll show them as-is but the chart will auto-scale
  return value;
}

interface CompetitorComparisonChartProps {
  data: Array<{
    name: string;
    value: number;
    benchmark: number;
  }>;
  className?: string;
}

export function CompetitorComparisonChart({
  data,
  className,
}: CompetitorComparisonChartProps) {
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>競合比較</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={300}>
          <BarChart
            data={data}
            margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
          >
            <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
            <XAxis dataKey="name" stroke="#71717a" fontSize={12} />
            <YAxis stroke="#71717a" fontSize={12} />
            <Tooltip
              contentStyle={{
                backgroundColor: "#ffffff",
                border: "1px solid #e4e4e7",
                borderRadius: "8px",
              }}
            />
            <Legend />
            <Bar dataKey="value" name="自社" fill="#fe2f5c" radius={[4, 4, 0, 0]}>
              {data.map((entry, index) => (
                <Cell
                  key={`cell-${index}`}
                  fill={entry.value >= entry.benchmark ? "#22c55e" : "#fe2f5c"}
                />
              ))}
            </Bar>
            <Bar dataKey="benchmark" name="競合平均" fill="#94a3b8" radius={[4, 4, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

interface GapAnalysisChartProps {
  goal: Goal;
  className?: string;
}

export function GapAnalysisChart({ goal, className }: GapAnalysisChartProps) {
  const data = goal.kpis
    .map((kpi) => {
      const kpiDef = getKPIById(goal.industry, kpi.kpiId);
      if (!kpiDef) return null;

      const benchmark = kpiDef.benchmark || kpiDef.benchmarkRange?.low || 0;
      const gapFromBenchmark = benchmark > 0 ? ((kpi.currentValue - benchmark) / benchmark) * 100 : 0;
      const gapFromTarget = kpi.targetValue > 0 ? ((kpi.currentValue - kpi.targetValue) / kpi.targetValue) * 100 : 0;

      return {
        name: kpiDef.nameJa,
        gapFromBenchmark: Math.round(gapFromBenchmark * 10) / 10,
        gapFromTarget: Math.round(gapFromTarget * 10) / 10,
      };
    })
    .filter(Boolean);

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>ギャップ分析</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={300}>
          <BarChart
            data={data}
            layout="vertical"
            margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
          >
            <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
            <XAxis
              type="number"
              stroke="#71717a"
              fontSize={12}
              tickFormatter={(value) => `${value}%`}
            />
            <YAxis
              type="category"
              dataKey="name"
              stroke="#71717a"
              fontSize={12}
              width={100}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "#ffffff",
                border: "1px solid #e4e4e7",
                borderRadius: "8px",
              }}
              formatter={(value: number) => [`${value}%`]}
            />
            <Legend />
            <Bar
              dataKey="gapFromBenchmark"
              name="業界平均との差"
              radius={[0, 4, 4, 0]}
            >
              {data.map((entry, index) => (
                <Cell
                  key={`cell-${index}`}
                  fill={
                    entry && entry.gapFromBenchmark >= 0 ? "#22c55e" : "#ef4444"
                  }
                />
              ))}
            </Bar>
            <Bar
              dataKey="gapFromTarget"
              name="目標との差"
              radius={[0, 4, 4, 0]}
            >
              {data.map((entry, index) => (
                <Cell
                  key={`cell-target-${index}`}
                  fill={
                    entry && entry.gapFromTarget >= 0 ? "#22c55e" : "#f59e0b"
                  }
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
