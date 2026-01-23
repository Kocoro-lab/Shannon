"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { submitTask } from "@/lib/shannon/client";
import { useMarketingStream } from "@/lib/shannon/stream";
import { useAppSelector, useAppDispatch } from "@/lib/store";
import {
  setCurrentWorkflowId,
  setCurrentTaskId,
  clearAiResponse,
  addStrategy,
  setProposedStrategies,
  clearProposedStrategies,
} from "@/lib/store/slices/marketing";
import { parseStrategiesFromResponse } from "@/lib/shannon/strategy-parser";
import { StrategyProposalList } from "@/components/strategy-proposal-card";
import { FileUpload } from "@/components/file-upload";
import type { ProposedStrategy, Strategy, UploadedFile } from "@/lib/shannon/types";
import { formatFileContext } from "@/lib/file-utils";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";
import {
  Send,
  Loader2,
  Sparkles,
  AlertCircle,
  RefreshCw,
  StopCircle,
  CheckCircle,
} from "lucide-react";

interface AIChatPanelProps {
  initialPrompt?: string;
  workflowType?: string;
  context?: Record<string, unknown>;
  className?: string;
  goalId?: string;
  userId?: string;
  onStrategyRegistered?: (strategy: Strategy) => void;
}

export function AIChatPanel({
  initialPrompt,
  workflowType,
  context,
  className,
  goalId,
  userId,
  onStrategyRegistered,
}: AIChatPanelProps) {
  const [input, setInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [parsedMarkdown, setParsedMarkdown] = useState<string>("");
  const [registeredIds, setRegisteredIds] = useState<Set<string>>(new Set());
  const [uploadedFiles, setUploadedFiles] = useState<UploadedFile[]>([]);
  const [supplementaryText, setSupplementaryText] = useState("");
  const [enableDeepResearch, setEnableDeepResearch] = useState(true);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const dispatch = useAppDispatch();

  const {
    aiResponse,
    isGenerating,
    connectionState,
    currentWorkflowId,
    events,
    proposedStrategies,
  } = useAppSelector((state) => state.marketing);

  // Stream connection
  useMarketingStream(currentWorkflowId, {
    onComplete: (result) => {
      console.log("Stream completed", result);
    },
    onError: (err) => {
      setError(err.message);
    },
  });

  // Parse strategies when AI response is complete
  useEffect(() => {
    if (!isGenerating && aiResponse) {
      const { strategies, markdownContent } = parseStrategiesFromResponse(aiResponse);
      setParsedMarkdown(markdownContent);
      dispatch(setProposedStrategies(strategies));
    }
  }, [isGenerating, aiResponse, dispatch]);

  // Register a proposed strategy as an actual strategy
  const handleRegisterStrategy = useCallback((proposed: ProposedStrategy) => {
    const strategy: Strategy = {
      id: `strategy-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
      goalId: goalId || "",
      name: proposed.name,
      description: proposed.description,
      impact: proposed.impact,
      effort: proposed.effort,
      status: "proposed",
      priority: 0,
      tasks: proposed.steps.map((step, index) => ({
        id: `task-${Date.now()}-${index}`,
        name: step,
        description: "",
        status: "pending" as const,
      })),
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      researchBackground: parsedMarkdown || aiResponse,  // リサーチ結果を保存
    };

    dispatch(addStrategy(strategy));
    setRegisteredIds((prev) => new Set(prev).add(proposed.id));
    onStrategyRegistered?.(strategy);
  }, [dispatch, goalId, onStrategyRegistered, parsedMarkdown, aiResponse]);

  // Request revision for a strategy
  const handleRequestRevision = useCallback((proposed: ProposedStrategy) => {
    setInput(`「${proposed.name}」の施策について修正をお願いします: `);
  }, []);

  // Auto-scroll to bottom
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [aiResponse, events]);

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    let query = input.trim() || initialPrompt;
    if (!query) return;

    // Append supplementary text if provided
    if (supplementaryText.trim()) {
      query = `${query}\n\n## ユーザーからの追加指示\n${supplementaryText.trim()}`;
    }

    console.log("[AIChatPanel] Submitting query:", query.substring(0, 100));

    setError(null);
    dispatch(clearAiResponse());
    dispatch(clearProposedStrategies());
    setParsedMarkdown("");
    setRegisteredIds(new Set());
    setInput("");

    // Build file context
    const fileContext = formatFileContext(uploadedFiles);

    try {
      const response = await submitTask({
        query,
        context: {
          ...context,
          workflow: workflowType,
          // Enable Deep Research with web_search and web_fetch tools
          force_research: enableDeepResearch,
          // Attach file context if files are uploaded
          ...(fileContext.files.length > 0 && {
            attachedFiles: fileContext,
          }),
        },
      });

      console.log("[AIChatPanel] API Response:", response);

      dispatch(setCurrentTaskId(response.task_id));
      if (response.workflow_id) {
        dispatch(setCurrentWorkflowId(response.workflow_id));
      } else {
        console.warn("[AIChatPanel] No workflow_id in response");
        setError("ストリーム接続に必要なIDがありません");
      }
    } catch (err) {
      console.error("[AIChatPanel] Submit error:", err);
      setError(err instanceof Error ? err.message : "タスクの送信に失敗しました");
    }
  };

  const handleStop = async () => {
    // Cancel the current task
    dispatch(setCurrentWorkflowId(null));
  };

  const handleRetry = () => {
    setError(null);
    if (initialPrompt) {
      handleSubmit();
    }
  };

  // Get current status from events
  const currentStatus = events.length > 0 ? events[events.length - 1] : null;
  const statusMessage = getStatusMessage(currentStatus?.type);

  return (
    <div className={cn("flex flex-col h-full", className)}>
      {/* Messages Area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4 min-h-[300px] max-h-[600px] scrollbar-thin">
        {!aiResponse && !isGenerating && !error && (
          <div className="text-center text-muted-foreground py-8">
            <Sparkles className="w-8 h-8 mx-auto mb-3 opacity-50" />
            <p className="text-sm">
              {initialPrompt
                ? "「分析を開始」をクリックしてAI分析を開始してください"
                : "質問を入力してAIに分析を依頼してください"}
            </p>
          </div>
        )}

        {/* Status indicator during generation */}
        {isGenerating && (
          <div className="flex items-center gap-2 p-3 rounded-lg bg-primary/5 border border-primary/20">
            <Loader2 className="w-4 h-4 animate-spin text-primary" />
            <span className="text-sm text-primary">{statusMessage}</span>
          </div>
        )}

        {/* AI Response */}
        {aiResponse && (
          <>
            {/* Proposed Strategies Cards */}
            {!isGenerating && proposedStrategies.length > 0 && (
              <StrategyProposalList
                strategies={proposedStrategies.filter(
                  (s) => !registeredIds.has(s.id)
                )}
                onRegister={handleRegisterStrategy}
                onRequestRevision={handleRequestRevision}
              />
            )}

            {/* Registered Strategies Confirmation */}
            {registeredIds.size > 0 && (
              <div className="flex items-center gap-2 p-3 rounded-lg bg-green-50 border border-green-200 text-green-700">
                <CheckCircle className="w-4 h-4" />
                <span className="text-sm">
                  {registeredIds.size}件の施策を登録しました
                </span>
              </div>
            )}

            {/* Markdown Content */}
            <Card className="p-4 bg-muted/50">
              <div className="prose prose-sm max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {isGenerating ? aiResponse : (parsedMarkdown || aiResponse)}
                </ReactMarkdown>
              </div>
            </Card>
          </>
        )}

        {/* Error State */}
        {error && (
          <div className="flex items-center gap-2 p-3 rounded-lg bg-destructive/10 border border-destructive/20 text-destructive">
            <AlertCircle className="w-4 h-4" />
            <span className="text-sm flex-1">{error}</span>
            <Button variant="ghost" size="sm" onClick={handleRetry}>
              <RefreshCw className="w-4 h-4 mr-1" />
              再試行
            </Button>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Input Area */}
      <div className="border-t border-border p-4">
        {initialPrompt && !aiResponse && !isGenerating ? (
          <div className="space-y-4">
            {/* Supplementary Text Input */}
            <div>
              <label className="text-sm text-muted-foreground mb-2 block">
                追加の指示やコンテキスト（オプション）
              </label>
              <Textarea
                value={supplementaryText}
                onChange={(e) => setSupplementaryText(e.target.value)}
                placeholder="AIに伝えたい追加情報があればここに入力..."
                className="min-h-[80px]"
              />
            </div>

            {/* File Upload */}
            {userId && (
              <FileUpload
                files={uploadedFiles}
                onFilesChange={setUploadedFiles}
                userId={userId}
                goalId={goalId}
                maxFiles={5}
              />
            )}

            {/* Deep Research Toggle */}
            <div className="flex items-center gap-2">
              <Checkbox
                id="deep-research"
                checked={enableDeepResearch}
                onCheckedChange={(checked) => setEnableDeepResearch(checked === true)}
              />
              <label
                htmlFor="deep-research"
                className="text-sm text-muted-foreground cursor-pointer"
              >
                Deep Research を有効にする（Web検索・競合調査）
              </label>
            </div>

            {/* Start Analysis Button */}
            <Button onClick={() => handleSubmit()} className="w-full">
              <Sparkles className="w-4 h-4 mr-2" />
              分析を開始
            </Button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="flex gap-2">
            <Input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="追加の質問を入力..."
              disabled={isGenerating}
              className="flex-1"
            />
            {isGenerating ? (
              <Button
                type="button"
                variant="destructive"
                onClick={handleStop}
              >
                <StopCircle className="w-4 h-4" />
              </Button>
            ) : (
              <Button type="submit" disabled={!input.trim()}>
                <Send className="w-4 h-4" />
              </Button>
            )}
          </form>
        )}
      </div>
    </div>
  );
}

function getStatusMessage(eventType?: string): string {
  switch (eventType) {
    case "WORKFLOW_STARTED":
      return "分析を開始しています...";
    case "AGENT_THINKING":
      return "考え中...";
    case "AGENT_STARTED":
      return "エージェントが処理を開始しました";
    case "DATA_PROCESSING":
      return "データを処理しています...";
    case "TOOL_INVOKED":
      return "ツールを実行中...";
    case "TOOL_OBSERVATION":
      return "結果を確認中...";
    case "SYNTHESIS":
      return "結果を統合しています...";
    case "LLM_PARTIAL":
    case "thread.message.delta":
      return "回答を生成中...";
    default:
      return "処理中...";
  }
}
