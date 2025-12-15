"use client";

import { useState, useEffect, useMemo } from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Clock, Calendar, Code } from "lucide-react";

// Frequency types
type Frequency = "hourly" | "daily" | "weekly" | "monthly";

interface ScheduleConfig {
    frequency: Frequency;
    // Hourly
    minuteOfHour: number;
    // Daily
    hour: number;
    minute: number;
    // Weekly
    daysOfWeek: number[]; // 0=Sun, 1=Mon, ..., 6=Sat
    // Monthly
    dayOfMonth: number;
}

interface ScheduleBuilderProps {
    value: string; // cron expression
    onChange: (cron: string) => void;
    timezone?: string;
}

const DAYS_OF_WEEK = [
    { value: 0, label: "Sun", short: "S" },
    { value: 1, label: "Mon", short: "M" },
    { value: 2, label: "Tue", short: "T" },
    { value: 3, label: "Wed", short: "W" },
    { value: 4, label: "Thu", short: "T" },
    { value: 5, label: "Fri", short: "F" },
    { value: 6, label: "Sat", short: "S" },
];

const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = [0, 15, 30, 45];
const DAYS_OF_MONTH = Array.from({ length: 28 }, (_, i) => i + 1); // 1-28 to avoid month-end issues

// Parse cron to config (best effort)
function parseCronToConfig(cron: string): ScheduleConfig | null {
    const parts = cron.trim().split(/\s+/);
    if (parts.length !== 5) return null;

    const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;

    // Every N minutes at start of interval: */N or 0 with hour *
    if (minute.startsWith("*/") && hour === "*" && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        return null; // Not supported in simple mode, use advanced
    }

    // Hourly: N * * * *
    if (hour === "*" && dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        const min = parseInt(minute) || 0;
        return {
            frequency: "hourly",
            minuteOfHour: min,
            hour: 9,
            minute: 0,
            daysOfWeek: [1, 2, 3, 4, 5],
            dayOfMonth: 1,
        };
    }

    // Weekly: M H * * D or M H * * D,D,D or M H * * D-D
    if (dayOfMonth === "*" && month === "*" && dayOfWeek !== "*") {
        const h = parseInt(hour) || 9;
        const m = parseInt(minute) || 0;
        
        // Parse day of week - handle ranges like "1-5" and comma-separated like "1,2,3"
        let days: number[] = [];
        if (dayOfWeek.includes("-")) {
            // Range format: "1-5" means Mon-Fri
            const [start, end] = dayOfWeek.split("-").map((d) => parseInt(d));
            if (!isNaN(start) && !isNaN(end)) {
                for (let d = start; d <= end; d++) {
                    days.push(d);
                }
            }
        } else {
            // Comma-separated: "1,2,3,4,5"
            days = dayOfWeek.split(",").map((d) => parseInt(d)).filter((d) => !isNaN(d));
        }
        
        if (days.length > 0) {
            return {
                frequency: "weekly",
                minuteOfHour: 0,
                hour: h,
                minute: m,
                daysOfWeek: days,
                dayOfMonth: 1,
            };
        }
    }

    // Monthly: M H D * *
    if (dayOfMonth !== "*" && month === "*" && dayOfWeek === "*") {
        const h = parseInt(hour) || 9;
        const m = parseInt(minute) || 0;
        const d = parseInt(dayOfMonth) || 1;
        return {
            frequency: "monthly",
            minuteOfHour: 0,
            hour: h,
            minute: m,
            daysOfWeek: [1, 2, 3, 4, 5],
            dayOfMonth: d,
        };
    }

    // Daily: M H * * *
    if (dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
        const h = parseInt(hour) || 9;
        const m = parseInt(minute) || 0;
        return {
            frequency: "daily",
            minuteOfHour: 0,
            hour: h,
            minute: m,
            daysOfWeek: [1, 2, 3, 4, 5],
            dayOfMonth: 1,
        };
    }

    return null; // Unrecognized pattern
}

// Build cron from config
function buildCronFromConfig(config: ScheduleConfig): string {
    switch (config.frequency) {
        case "hourly":
            return `${config.minuteOfHour} * * * *`;
        case "daily":
            return `${config.minute} ${config.hour} * * *`;
        case "weekly":
            const days = config.daysOfWeek.length > 0 ? config.daysOfWeek.sort().join(",") : "1";
            return `${config.minute} ${config.hour} * * ${days}`;
        case "monthly":
            return `${config.minute} ${config.hour} ${config.dayOfMonth} * *`;
        default:
            return "0 9 * * *";
    }
}

// Parse day of week field (handles ranges like "1-5" and comma-separated like "1,2,3")
function parseDayOfWeek(dayOfWeek: string): number[] {
    if (dayOfWeek === "*") return [0, 1, 2, 3, 4, 5, 6];
    
    const days: number[] = [];
    const parts = dayOfWeek.split(",");
    
    for (const part of parts) {
        if (part.includes("-")) {
            const [start, end] = part.split("-").map((d) => parseInt(d));
            if (!isNaN(start) && !isNaN(end)) {
                for (let d = start; d <= end; d++) {
                    days.push(d);
                }
            }
        } else {
            const d = parseInt(part);
            if (!isNaN(d)) days.push(d);
        }
    }
    
    return days;
}

// Calculate next N run times (simplified - uses browser local time for preview)
// Note: Actual execution times are determined by backend using the specified timezone
function getNextRuns(cron: string, timezone: string, count: number = 3): Date[] {
    const runs: Date[] = [];
    const parts = cron.trim().split(/\s+/);
    if (parts.length !== 5) return runs;

    const [minuteStr, hourStr, dayOfMonthStr, , dayOfWeekStr] = parts;
    
    const targetMinute = minuteStr === "*" ? null : parseInt(minuteStr);
    const targetHour = hourStr === "*" ? null : parseInt(hourStr);
    const targetDayOfMonth = dayOfMonthStr === "*" ? null : parseInt(dayOfMonthStr);
    const allowedDaysOfWeek = parseDayOfWeek(dayOfWeekStr);

    const now = new Date();
    // Start from the next minute to avoid duplicates
    let current = new Date(now);
    current.setSeconds(0, 0);
    current.setMinutes(current.getMinutes() + 1);

    // For hourly jobs, we only need to check ~24*7 iterations max
    // For daily/weekly/monthly, we need more iterations
    const maxIterations = targetHour === null ? 200 : 1500;

    for (let i = 0; i < maxIterations && runs.length < count; i++) {
        const h = current.getHours();
        const m = current.getMinutes();
        const dom = current.getDate();
        const dow = current.getDay();

        // Check all conditions
        const minuteMatch = targetMinute === null || m === targetMinute;
        const hourMatch = targetHour === null || h === targetHour;
        const dayOfMonthMatch = targetDayOfMonth === null || dom === targetDayOfMonth;
        const dayOfWeekMatch = allowedDaysOfWeek.includes(dow);

        if (minuteMatch && hourMatch && dayOfMonthMatch && dayOfWeekMatch) {
            runs.push(new Date(current));
        }

        // Increment by appropriate interval based on what we're looking for
        if (targetMinute === null) {
            // Every minute matching other criteria - but we shouldn't hit this with our presets
            current.setMinutes(current.getMinutes() + 1);
        } else if (targetHour === null) {
            // Hourly job - jump to next hour at the target minute
            current.setHours(current.getHours() + 1);
            current.setMinutes(targetMinute);
        } else {
            // Daily/weekly/monthly - jump to next day at the target time
            current.setDate(current.getDate() + 1);
            current.setHours(targetHour);
            current.setMinutes(targetMinute);
        }
    }

    return runs;
}

// Format time for display
function formatTime(hour: number, minute: number): string {
    const h = hour % 12 || 12;
    const ampm = hour < 12 ? "AM" : "PM";
    return `${h}:${minute.toString().padStart(2, "0")} ${ampm}`;
}

export function ScheduleBuilder({ value, onChange, timezone = "UTC" }: ScheduleBuilderProps) {
    const [isAdvanced, setIsAdvanced] = useState(false);
    const [config, setConfig] = useState<ScheduleConfig>({
        frequency: "daily",
        minuteOfHour: 0,
        hour: 9,
        minute: 0,
        daysOfWeek: [1, 2, 3, 4, 5], // Weekdays
        dayOfMonth: 1,
    });

    // Parse initial value
    useEffect(() => {
        const parsed = parseCronToConfig(value);
        if (parsed) {
            setConfig(parsed);
            setIsAdvanced(false);
        } else if (value && value !== "0 9 * * *") {
            // Unrecognized pattern, show advanced mode
            setIsAdvanced(true);
        }
    }, []); // Only on mount

    // Update cron when config changes (not in advanced mode)
    useEffect(() => {
        if (!isAdvanced) {
            const newCron = buildCronFromConfig(config);
            if (newCron !== value) {
                onChange(newCron);
            }
        }
    }, [config, isAdvanced, onChange, value]);

    const updateConfig = (updates: Partial<ScheduleConfig>) => {
        setConfig((prev) => ({ ...prev, ...updates }));
    };

    const toggleDayOfWeek = (day: number) => {
        setConfig((prev) => {
            const days = prev.daysOfWeek.includes(day)
                ? prev.daysOfWeek.filter((d) => d !== day)
                : [...prev.daysOfWeek, day];
            return { ...prev, daysOfWeek: days.length > 0 ? days : [day] }; // Ensure at least one day
        });
    };

    const nextRuns = useMemo(() => getNextRuns(value, timezone, 3), [value, timezone]);

    // Summary text
    const getSummary = (): string => {
        switch (config.frequency) {
            case "hourly":
                return `Runs every hour at :${config.minuteOfHour.toString().padStart(2, "0")}`;
            case "daily":
                return `Runs daily at ${formatTime(config.hour, config.minute)}`;
            case "weekly": {
                const dayNames = config.daysOfWeek
                    .sort()
                    .map((d) => DAYS_OF_WEEK.find((day) => day.value === d)?.label)
                    .join(", ");
                return `Runs ${dayNames} at ${formatTime(config.hour, config.minute)}`;
            }
            case "monthly":
                return `Runs monthly on day ${config.dayOfMonth} at ${formatTime(config.hour, config.minute)}`;
            default:
                return "";
        }
    };

    if (isAdvanced) {
        return (
            <div className="space-y-3">
                <div className="flex items-center justify-between">
                    <Label>Cron Expression</Label>
                    <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                            const parsed = parseCronToConfig(value);
                            if (parsed) {
                                setConfig(parsed);
                                setIsAdvanced(false);
                            }
                        }}
                        className="text-xs h-7"
                    >
                        <Calendar className="h-3 w-3 mr-1" />
                        Simple Mode
                    </Button>
                </div>
                <Input
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                    placeholder="0 9 * * *"
                    className="font-mono"
                />
                <p className="text-xs text-muted-foreground">
                    Format: minute hour day month weekday (e.g., &quot;0 9 * * 1-5&quot; = weekdays at 9am)
                </p>
                {nextRuns.length > 0 && (
                    <div className="text-xs text-muted-foreground">
                        <span className="font-medium">Next runs (preview):</span>{" "}
                        {nextRuns.map((d) => d.toLocaleString()).join(" → ")}
                    </div>
                )}
            </div>
        );
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between">
                <Label>Schedule</Label>
                <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setIsAdvanced(true)}
                    className="text-xs h-7"
                >
                    <Code className="h-3 w-3 mr-1" />
                    Advanced
                </Button>
            </div>

            {/* Frequency selector */}
            <div className="grid grid-cols-4 gap-2">
                {(["hourly", "daily", "weekly", "monthly"] as Frequency[]).map((freq) => (
                    <Button
                        key={freq}
                        type="button"
                        variant={config.frequency === freq ? "default" : "outline"}
                        size="sm"
                        onClick={() => updateConfig({ frequency: freq })}
                        className="capitalize"
                    >
                        {freq}
                    </Button>
                ))}
            </div>

            {/* Frequency-specific controls */}
            <div className="space-y-3 p-3 rounded-lg bg-muted/50 border">
                {config.frequency === "hourly" && (
                    <div className="flex items-center gap-3">
                        <span className="text-sm text-muted-foreground">At minute</span>
                        <Select
                            value={config.minuteOfHour.toString()}
                            onValueChange={(v) => updateConfig({ minuteOfHour: parseInt(v) })}
                        >
                            <SelectTrigger className="w-20">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {[0, 15, 30, 45].map((m) => (
                                    <SelectItem key={m} value={m.toString()}>
                                        :{m.toString().padStart(2, "0")}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <span className="text-sm text-muted-foreground">of every hour</span>
                    </div>
                )}

                {(config.frequency === "daily" || config.frequency === "weekly" || config.frequency === "monthly") && (
                    <div className="flex items-center gap-3">
                        <Clock className="h-4 w-4 text-muted-foreground" />
                        <span className="text-sm text-muted-foreground">At</span>
                        <Select
                            value={config.hour.toString()}
                            onValueChange={(v) => updateConfig({ hour: parseInt(v) })}
                        >
                            <SelectTrigger className="w-24">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {HOURS.map((h) => (
                                    <SelectItem key={h} value={h.toString()}>
                                        {formatTime(h, 0).split(":")[0]} {h < 12 ? "AM" : "PM"}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <span className="text-sm text-muted-foreground">:</span>
                        <Select
                            value={config.minute.toString()}
                            onValueChange={(v) => updateConfig({ minute: parseInt(v) })}
                        >
                            <SelectTrigger className="w-20">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {MINUTES.map((m) => (
                                    <SelectItem key={m} value={m.toString()}>
                                        {m.toString().padStart(2, "0")}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                )}

                {config.frequency === "weekly" && (
                    <div className="space-y-2">
                        <span className="text-sm text-muted-foreground">On days</span>
                        <div className="flex gap-1">
                            {DAYS_OF_WEEK.map((day) => (
                                <button
                                    key={day.value}
                                    type="button"
                                    onClick={() => toggleDayOfWeek(day.value)}
                                    className={`w-9 h-9 rounded-full text-sm font-medium transition-colors ${
                                        config.daysOfWeek.includes(day.value)
                                            ? "bg-primary text-primary-foreground"
                                            : "bg-muted hover:bg-muted/80 text-muted-foreground"
                                    }`}
                                    title={day.label}
                                >
                                    {day.short}
                                </button>
                            ))}
                        </div>
                        <div className="flex gap-2 pt-1">
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-6 text-xs"
                                onClick={() => updateConfig({ daysOfWeek: [1, 2, 3, 4, 5] })}
                            >
                                Weekdays
                            </Button>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-6 text-xs"
                                onClick={() => updateConfig({ daysOfWeek: [0, 6] })}
                            >
                                Weekends
                            </Button>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-6 text-xs"
                                onClick={() => updateConfig({ daysOfWeek: [0, 1, 2, 3, 4, 5, 6] })}
                            >
                                Every day
                            </Button>
                        </div>
                    </div>
                )}

                {config.frequency === "monthly" && (
                    <div className="flex items-center gap-3">
                        <Calendar className="h-4 w-4 text-muted-foreground" />
                        <span className="text-sm text-muted-foreground">On day</span>
                        <Select
                            value={config.dayOfMonth.toString()}
                            onValueChange={(v) => updateConfig({ dayOfMonth: parseInt(v) })}
                        >
                            <SelectTrigger className="w-20">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {DAYS_OF_MONTH.map((d) => (
                                    <SelectItem key={d} value={d.toString()}>
                                        {d}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <span className="text-sm text-muted-foreground">of each month</span>
                    </div>
                )}
            </div>

            {/* Summary */}
            <div className="text-sm">
                <span className="font-medium">{getSummary()}</span>
                <span className="text-muted-foreground ml-2">({timezone})</span>
            </div>

            {/* Next runs preview */}
            {nextRuns.length > 0 && (
                <div className="text-xs text-muted-foreground border-t pt-3">
                    <span className="font-medium">Next runs (preview): </span>
                    {nextRuns.map((d, i) => (
                        <span key={i}>
                            {i > 0 && " → "}
                            {d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" })}{" "}
                            {d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}
                        </span>
                    ))}
                </div>
            )}
        </div>
    );
}

export { type Frequency, type ScheduleConfig };

