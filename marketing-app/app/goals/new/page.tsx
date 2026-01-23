"use client";

import { GoalWizard } from "@/components/goal-wizard";

export default function NewGoalPage() {
  return (
    <div className="container-kocoro py-8">
      <div className="mb-8">
        <h1 className="font-serif text-3xl font-bold">新しい目標を設定</h1>
        <p className="text-muted-foreground mt-2">
          ウィザードに従って、マーケティング目標を設定してください
        </p>
      </div>
      <GoalWizard />
    </div>
  );
}
