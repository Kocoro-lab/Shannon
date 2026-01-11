//! Usage tracking and model breakdown for task execution.
//!
//! This module provides comprehensive tracking of model usage, token consumption,
//! and cost breakdown across multi-model workflows. Supports aggregation of
//! usage statistics from multiple LLM calls within a single task.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Per-model usage breakdown for a task (T128).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ModelUsageBreakdown {
    /// Model identifier (e.g., "gpt-4o", "claude-3-5-sonnet-latest").
    pub model: String,

    /// Provider name (e.g., "openai", "anthropic").
    pub provider: String,

    /// Number of calls made to this model.
    pub call_count: u32,

    /// Total input/prompt tokens for this model.
    pub prompt_tokens: u32,

    /// Total output/completion tokens for this model.
    pub completion_tokens: u32,

    /// Total tokens (prompt + completion) for this model.
    pub total_tokens: u32,

    /// Estimated cost in USD for this model.
    pub cost_usd: f64,
}

/// Aggregate usage tracker for a workflow/task (T129).
#[derive(Debug, Clone, Default)]
pub struct UsageTracker {
    /// Per-model usage breakdown.
    model_usage: HashMap<String, ModelUsageBreakdown>,

    /// Primary model used (first or most-used model).
    primary_model: Option<String>,

    /// Primary provider used.
    primary_provider: Option<String>,
}

impl UsageTracker {
    /// Create a new usage tracker.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Record a model call with usage statistics.
    ///
    /// This updates the per-model breakdown and aggregates totals.
    pub fn record_call(
        &mut self,
        model: impl Into<String>,
        provider: impl Into<String>,
        prompt_tokens: u32,
        completion_tokens: u32,
        cost_usd: f64,
    ) {
        let model = model.into();
        let provider = provider.into();

        // Set primary model and provider if not set
        if self.primary_model.is_none() {
            self.primary_model = Some(model.clone());
            self.primary_provider = Some(provider.clone());
        }

        // Update or insert model usage
        self.model_usage
            .entry(model.clone())
            .and_modify(|usage| {
                usage.call_count += 1;
                usage.prompt_tokens += prompt_tokens;
                usage.completion_tokens += completion_tokens;
                usage.total_tokens += prompt_tokens + completion_tokens;
                usage.cost_usd += cost_usd;
            })
            .or_insert(ModelUsageBreakdown {
                model,
                provider,
                call_count: 1,
                prompt_tokens,
                completion_tokens,
                total_tokens: prompt_tokens + completion_tokens,
                cost_usd,
            });
    }

    /// Get the primary model used (T125).
    #[must_use]
    pub fn primary_model(&self) -> Option<&str> {
        self.primary_model.as_deref()
    }

    /// Get the primary provider used (T126).
    #[must_use]
    pub fn primary_provider(&self) -> Option<&str> {
        self.primary_provider.as_deref()
    }

    /// Get the aggregated token usage across all models.
    #[must_use]
    pub fn total_usage(&self) -> super::task::TokenUsage {
        let mut total = super::task::TokenUsage::default();

        for usage in self.model_usage.values() {
            total.prompt_tokens += usage.prompt_tokens;
            total.completion_tokens += usage.completion_tokens;
            total.total_tokens += usage.total_tokens;
            total.cost_usd += usage.cost_usd;
        }

        total
    }

    /// Get the per-model breakdown as a vector (T128).
    #[must_use]
    pub fn model_breakdown(&self) -> Vec<ModelUsageBreakdown> {
        let mut breakdown: Vec<_> = self.model_usage.values().cloned().collect();

        // Sort by call count descending
        breakdown.sort_by(|a, b| b.call_count.cmp(&a.call_count));

        breakdown
    }

    /// Check if any usage has been recorded.
    #[must_use]
    pub fn has_usage(&self) -> bool {
        !self.model_usage.is_empty()
    }

    /// Get the number of unique models used.
    #[must_use]
    pub fn model_count(&self) -> usize {
        self.model_usage.len()
    }
}

/// Extended task result with usage metadata (T125-T129).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TaskResultWithMetadata {
    /// Task ID.
    pub task_id: String,

    /// Final state.
    pub state: super::task::TaskState,

    /// Result content (for completed tasks).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,

    /// Structured output data.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,

    /// Error message (for failed tasks).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,

    /// Primary model used (T125).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_used: Option<String>,

    /// Primary provider used (T126).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub provider: Option<String>,

    /// Aggregated token usage (T127).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token_usage: Option<super::task::TokenUsage>,

    /// Per-model usage breakdown (T128).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_breakdown: Option<Vec<ModelUsageBreakdown>>,

    /// Execution duration in milliseconds.
    pub duration_ms: u64,

    /// Sources and citations (for research tasks).
    #[serde(default)]
    pub sources: Vec<super::task::Source>,
}

impl TaskResultWithMetadata {
    /// Create from a basic task result and usage tracker.
    #[must_use]
    pub fn from_result_and_tracker(
        result: super::task::TaskResult,
        tracker: &UsageTracker,
    ) -> Self {
        Self {
            task_id: result.task_id,
            state: result.state,
            content: result.content,
            data: result.data,
            error: result.error,
            model_used: tracker.primary_model().map(String::from),
            provider: tracker.primary_provider().map(String::from),
            token_usage: if tracker.has_usage() {
                Some(tracker.total_usage())
            } else {
                result.token_usage
            },
            model_breakdown: if tracker.has_usage() {
                Some(tracker.model_breakdown())
            } else {
                None
            },
            duration_ms: result.duration_ms,
            sources: result.sources,
        }
    }

    /// Create from a basic task result without tracking.
    #[must_use]
    pub fn from_result(result: super::task::TaskResult) -> Self {
        Self {
            task_id: result.task_id,
            state: result.state,
            content: result.content,
            data: result.data,
            error: result.error,
            model_used: None,
            provider: None,
            token_usage: result.token_usage,
            model_breakdown: None,
            duration_ms: result.duration_ms,
            sources: result.sources,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_usage_tracker_single_model() {
        let mut tracker = UsageTracker::new();

        tracker.record_call("gpt-4o", "openai", 100, 50, 0.005);
        tracker.record_call("gpt-4o", "openai", 200, 100, 0.010);

        assert_eq!(tracker.primary_model(), Some("gpt-4o"));
        assert_eq!(tracker.primary_provider(), Some("openai"));
        assert_eq!(tracker.model_count(), 1);

        let total = tracker.total_usage();
        assert_eq!(total.prompt_tokens, 300);
        assert_eq!(total.completion_tokens, 150);
        assert_eq!(total.total_tokens, 450);
        assert!((total.cost_usd - 0.015).abs() < 0.001);
    }

    #[test]
    fn test_usage_tracker_multi_model() {
        let mut tracker = UsageTracker::new();

        tracker.record_call("gpt-4o", "openai", 100, 50, 0.005);
        tracker.record_call("claude-3-5-sonnet", "anthropic", 200, 100, 0.012);
        tracker.record_call("gpt-4o", "openai", 50, 25, 0.002);

        assert_eq!(tracker.primary_model(), Some("gpt-4o"));
        assert_eq!(tracker.model_count(), 2);

        let breakdown = tracker.model_breakdown();
        assert_eq!(breakdown.len(), 2);

        // First should be gpt-4o (2 calls)
        assert_eq!(breakdown[0].model, "gpt-4o");
        assert_eq!(breakdown[0].call_count, 2);
        assert_eq!(breakdown[0].prompt_tokens, 150);

        // Second should be claude (1 call)
        assert_eq!(breakdown[1].model, "claude-3-5-sonnet");
        assert_eq!(breakdown[1].call_count, 1);
    }

    #[test]
    fn test_task_result_with_metadata() {
        let mut tracker = UsageTracker::new();
        tracker.record_call("gpt-4o", "openai", 100, 50, 0.005);

        let basic_result = super::super::task::TaskResult {
            task_id: "task-123".to_string(),
            state: super::super::task::TaskState::Completed,
            content: Some("Test result".to_string()),
            data: None,
            error: None,
            token_usage: None,
            duration_ms: 1000,
            sources: vec![],
        };

        let extended = TaskResultWithMetadata::from_result_and_tracker(basic_result, &tracker);

        assert_eq!(extended.model_used, Some("gpt-4o".to_string()));
        assert_eq!(extended.provider, Some("openai".to_string()));
        assert!(extended.token_usage.is_some());
        assert!(extended.model_breakdown.is_some());
    }
}
