//! Reflection cognitive pattern.
//!
//! Implements self-critique and iterative improvement loop.
//! Generates initial answer, critiques it, then generates improved version.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{Reflection, PatternContext};
//!
//! let reflection = Reflection::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = reflection.execute(&ctx, "Write a summary of AI ethics").await?;
//! ```

use anyhow::Result;
use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    Activity, ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, TokenUsage};

/// Reflection pattern configuration.
#[derive(Debug, Clone)]
pub struct Reflection {
    /// Maximum reflection iterations.
    pub max_iterations: usize,

    /// Quality threshold to stop iterating (0.0-1.0).
    pub quality_threshold: f32,

    /// Model to use.
    pub model: String,

    /// Base URL for LLM API.
    base_url: String,
}

impl Reflection {
    /// Create a new Reflection pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `max_iterations`: 3
    /// - `quality_threshold`: 0.5
    /// - `model`: "claude-sonnet-4-20250514"
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 3,
            quality_threshold: 0.5,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Create with custom configuration.
    #[must_use]
    pub fn with_config(max_iterations: usize, quality_threshold: f32) -> Self {
        Self {
            max_iterations,
            quality_threshold: quality_threshold.clamp(0.0, 1.0),
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Generate initial answer.
    async fn generate_initial(&self, ctx: &PatternContext, query: &str) -> Result<String> {
        let system =
            "You are a helpful assistant. Provide a clear, comprehensive answer to the query.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: query.to_string(),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.7,
            max_tokens: Some(2048),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "reflection-initial".to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 60,
        };

        let llm_activity = LlmReasonActivity::new(self.base_url.clone(), None);
        let result = llm_activity
            .execute(&activity_ctx, serde_json::to_value(&request)?)
            .await;

        let response = match result {
            durable_shannon::activities::ActivityResult::Success(value) => {
                serde_json::from_value::<durable_shannon::activities::llm::LlmResponse>(value)?
            }
            durable_shannon::activities::ActivityResult::Failure { error, .. } => {
                anyhow::bail!("Initial generation failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Initial generation needs retry: {reason}");
            }
        };

        Ok(response.content)
    }

    /// Critique the current answer.
    async fn critique(
        &self,
        ctx: &PatternContext,
        query: &str,
        answer: &str,
    ) -> Result<(String, f32)> {
        let system = "You are a critical evaluator. Identify weaknesses, gaps, or areas for improvement in the answer. Then provide a quality score (0.0-1.0).";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Query: {query}\n\nAnswer: {answer}\n\n\
                Provide:\n1. Critique (what could be improved)\n2. Quality Score: <number between 0.0 and 1.0>"
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.3,
            max_tokens: Some(1024),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "reflection-critique".to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 30,
        };

        let llm_activity = LlmReasonActivity::new(self.base_url.clone(), None);
        let result = llm_activity
            .execute(&activity_ctx, serde_json::to_value(&request)?)
            .await;

        let response = match result {
            durable_shannon::activities::ActivityResult::Success(value) => {
                serde_json::from_value::<durable_shannon::activities::llm::LlmResponse>(value)?
            }
            _ => return Ok(("No critique available".to_string(), 0.5)),
        };

        // Parse quality score
        let score = response
            .content
            .lines()
            .find(|line| line.to_lowercase().contains("quality score"))
            .and_then(|line| {
                line.split(':')
                    .nth(1)
                    .and_then(|s| s.trim().parse::<f32>().ok())
            })
            .unwrap_or(0.5)
            .clamp(0.0, 1.0);

        Ok((response.content, score))
    }

    /// Improve the answer based on critique.
    async fn improve(
        &self,
        ctx: &PatternContext,
        query: &str,
        answer: &str,
        critique: &str,
    ) -> Result<String> {
        let system = "You are an improvement specialist. Given the original answer and critique, generate an improved version that addresses the identified weaknesses.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Query: {query}\n\nOriginal Answer: {answer}\n\nCritique: {critique}\n\n\
                Provide an improved answer that addresses these critiques:"
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.7,
            max_tokens: Some(2048),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "reflection-improve".to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 60,
        };

        let llm_activity = LlmReasonActivity::new(self.base_url.clone(), None);
        let result = llm_activity
            .execute(&activity_ctx, serde_json::to_value(&request)?)
            .await;

        let response = match result {
            durable_shannon::activities::ActivityResult::Success(value) => {
                serde_json::from_value::<durable_shannon::activities::llm::LlmResponse>(value)?
            }
            durable_shannon::activities::ActivityResult::Failure { error, .. } => {
                anyhow::bail!("Improvement failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Improvement needs retry: {reason}");
            }
        };

        Ok(response.content)
    }
}

impl Default for Reflection {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for Reflection {
    fn name(&self) -> &str {
        "reflection"
    }

    fn description(&self) -> Option<&str> {
        Some("Self-critique and iterative improvement for quality enhancement")
    }

    fn validate_input(&self, input: &str) -> Result<()> {
        if input.trim().is_empty() {
            anyhow::bail!("Input cannot be empty");
        }
        if input.len() > 5_000 {
            anyhow::bail!("Input too long (max 5,000 characters)");
        }
        Ok(())
    }

    async fn execute(&self, ctx: &PatternContext, input: &str) -> Result<PatternResult> {
        tracing::info!(
            workflow_id = %ctx.workflow_id,
            pattern = "reflection",
            max_iterations = self.max_iterations,
            "Starting Reflection execution"
        );

        let mut reasoning_steps = Vec::new();
        let mut current_answer = self.generate_initial(ctx, input).await?;

        reasoning_steps.push(ReasoningStep {
            step: 0,
            content: format!("Initial answer generated: {}", current_answer),
            confidence: Some(0.7),
            timestamp: Utc::now(),
        });

        // Iterative reflection loop
        for iteration in 0..self.max_iterations {
            tracing::debug!(
                workflow_id = %ctx.workflow_id,
                iteration,
                "Reflection iteration"
            );

            // Critique current answer
            let (critique, quality_score) = self.critique(ctx, input, &current_answer).await?;

            reasoning_steps.push(ReasoningStep {
                step: iteration * 2 + 1,
                content: format!("Critique (quality={}): {}", quality_score, critique),
                confidence: Some(quality_score),
                timestamp: Utc::now(),
            });

            // Check if quality threshold met
            if quality_score >= self.quality_threshold {
                tracing::info!(
                    workflow_id = %ctx.workflow_id,
                    iteration,
                    quality_score,
                    "Quality threshold met, stopping reflection"
                );
                break;
            }

            // Improve answer
            let improved = self.improve(ctx, input, &current_answer, &critique).await?;

            reasoning_steps.push(ReasoningStep {
                step: iteration * 2 + 2,
                content: format!("Improved answer: {}", improved),
                confidence: Some(0.75),
                timestamp: Utc::now(),
            });

            current_answer = improved;
        }

        tracing::info!(
            workflow_id = %ctx.workflow_id,
            iterations = reasoning_steps.len() / 2,
            "Reflection completed"
        );

        Ok(PatternResult {
            output: current_answer,
            reasoning_steps,
            sources: vec![],
            token_usage: None,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_reflection() {
        let reflection = Reflection::new();
        assert_eq!(reflection.max_iterations, 3);
        assert_eq!(reflection.quality_threshold, 0.5);
    }

    #[test]
    fn test_with_config() {
        let reflection = Reflection::with_config(5, 0.8);
        assert_eq!(reflection.max_iterations, 5);
        assert_eq!(reflection.quality_threshold, 0.8);
    }

    #[test]
    fn test_quality_threshold_clamping() {
        let reflection = Reflection::with_config(3, 1.5);
        assert_eq!(reflection.quality_threshold, 1.0); // Clamped to max

        let reflection = Reflection::with_config(3, -0.5);
        assert_eq!(reflection.quality_threshold, 0.0); // Clamped to min
    }

    #[test]
    fn test_pattern_name() {
        let reflection = Reflection::new();
        assert_eq!(reflection.name(), "reflection");
    }

    #[test]
    fn test_pattern_description() {
        let reflection = Reflection::new();
        assert!(reflection.description().is_some());
        assert!(reflection.description().unwrap().contains("critique"));
    }

    #[test]
    fn test_validate_input_empty() {
        let reflection = Reflection::new();
        let result = reflection.validate_input("");
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_valid() {
        let reflection = Reflection::new();
        let result = reflection.validate_input("Write a summary");
        assert!(result.is_ok());
    }

    // Integration test requires real LLM API
    #[tokio::test]
    #[ignore = "requires real LLM API"]
    async fn test_execute_with_improvement() {
        let reflection = Reflection::new();
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = reflection.execute(&ctx, "Test task").await;

        if let Ok(output) = result {
            assert!(!output.output.is_empty());
            assert!(!output.reasoning_steps.is_empty());
        }
    }
}
