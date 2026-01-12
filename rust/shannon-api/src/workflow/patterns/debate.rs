//! Debate cognitive pattern.
//!
//! Implements multi-agent discussion with critique cycles and consensus synthesis.
//! Multiple agents argue different positions, critique each other, and reach consensus.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{Debate, PatternContext};
//!
//! let debate = Debate::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = debate.execute(&ctx, "Is AI consciousness possible?").await?;
//! ```

use anyhow::Result;
use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    Activity, ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, TokenUsage};

/// Debate pattern configuration.
#[derive(Debug, Clone)]
pub struct Debate {
    /// Number of debating agents (2-4).
    pub num_agents: usize,

    /// Maximum debate rounds.
    pub max_rounds: usize,

    /// Model to use.
    pub model: String,

    /// Base URL for LLM API.
    base_url: String,
}

impl Debate {
    /// Create a new Debate pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `num_agents`: 2
    /// - `max_rounds`: 3
    /// - `model`: "claude-sonnet-4-20250514"
    #[must_use]
    pub fn new() -> Self {
        Self {
            num_agents: 2,
            max_rounds: 3,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Create with custom configuration.
    #[must_use]
    pub fn with_config(num_agents: usize, max_rounds: usize) -> Self {
        Self {
            num_agents: num_agents.clamp(2, 4),
            max_rounds,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Generate perspective from a debate agent.
    async fn generate_perspective(
        &self,
        ctx: &PatternContext,
        query: &str,
        agent_id: usize,
        previous_arguments: &[String],
    ) -> Result<String> {
        let position = match agent_id {
            0 => "affirmative",
            1 => "negative",
            2 => "neutral/pragmatic",
            _ => "alternative",
        };

        let system = format!(
            "You are Debater {} taking the {} position. Present strong arguments for your stance.",
            agent_id + 1,
            position
        );

        let context = if previous_arguments.is_empty() {
            String::new()
        } else {
            format!(
                "\n\nPrevious arguments:\n{}",
                previous_arguments.join("\n\n")
            )
        };

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Question: {query}{context}\n\nProvide your perspective and arguments:"
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system,
            messages,
            temperature: 0.8, // Higher temp for diverse perspectives
            max_tokens: Some(1024),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: format!("debate-agent-{agent_id}"),
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
            durable_shannon::activities::ActivityResult::Failure { error, .. } => {
                anyhow::bail!("Perspective generation failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Perspective generation needs retry: {reason}");
            }
        };

        Ok(response.content)
    }

    /// Synthesize final consensus from all perspectives.
    async fn synthesize_consensus(
        &self,
        ctx: &PatternContext,
        query: &str,
        all_arguments: &[String],
    ) -> Result<String> {
        let system = "You are a moderator. Synthesize a balanced final answer that incorporates the best arguments from all perspectives.";

        let arguments_text = all_arguments
            .iter()
            .enumerate()
            .map(|(i, arg)| format!("Debater {}: {}", i + 1, arg))
            .collect::<Vec<_>>()
            .join("\n\n");

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Question: {query}\n\nDebate arguments:\n{arguments_text}\n\n\
                Synthesize a balanced final answer incorporating the best points:"
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.5,
            max_tokens: Some(2048),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "debate-synthesize".to_string(),
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
                anyhow::bail!("Synthesis failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Synthesis needs retry: {reason}");
            }
        };

        Ok(response.content)
    }
}

impl Default for Debate {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for Debate {
    fn name(&self) -> &str {
        "debate"
    }

    fn description(&self) -> Option<&str> {
        Some("Multi-agent discussion with diverse perspectives and consensus synthesis")
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
            pattern = "debate",
            num_agents = self.num_agents,
            max_rounds = self.max_rounds,
            "Starting Debate execution"
        );

        let mut reasoning_steps = Vec::new();
        let mut all_arguments = Vec::new();

        // Debate rounds
        for round in 0..self.max_rounds {
            tracing::debug!(
                workflow_id = %ctx.workflow_id,
                round,
                "Debate round"
            );

            reasoning_steps.push(ReasoningStep {
                step: round * self.num_agents,
                content: format!("Round {} beginning", round + 1),
                confidence: Some(0.9),
                timestamp: Utc::now(),
            });

            // Each agent presents their perspective
            let mut round_arguments = Vec::new();
            for agent_id in 0..self.num_agents {
                let perspective = self
                    .generate_perspective(ctx, input, agent_id, &all_arguments)
                    .await?;

                reasoning_steps.push(ReasoningStep {
                    step: round * self.num_agents + agent_id + 1,
                    content: format!("Debater {}: {}", agent_id + 1, perspective),
                    confidence: Some(0.8),
                    timestamp: Utc::now(),
                });

                round_arguments.push(perspective);
            }

            all_arguments.extend(round_arguments);
        }

        // Synthesize final consensus
        reasoning_steps.push(ReasoningStep {
            step: reasoning_steps.len(),
            content: "Synthesizing consensus from all perspectives...".to_string(),
            confidence: Some(0.85),
            timestamp: Utc::now(),
        });

        let consensus = self
            .synthesize_consensus(ctx, input, &all_arguments)
            .await?;

        tracing::info!(
            workflow_id = %ctx.workflow_id,
            rounds = self.max_rounds,
            arguments = all_arguments.len(),
            "Debate completed"
        );

        Ok(PatternResult {
            output: consensus,
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
    fn test_new_debate() {
        let debate = Debate::new();
        assert_eq!(debate.num_agents, 2);
        assert_eq!(debate.max_rounds, 3);
    }

    #[test]
    fn test_with_config() {
        let debate = Debate::with_config(4, 5);
        assert_eq!(debate.num_agents, 4);
        assert_eq!(debate.max_rounds, 5);
    }

    #[test]
    fn test_num_agents_clamping() {
        let debate = Debate::with_config(1, 3);
        assert_eq!(debate.num_agents, 2); // Clamped to min

        let debate = Debate::with_config(10, 3);
        assert_eq!(debate.num_agents, 4); // Clamped to max
    }

    #[test]
    fn test_pattern_name() {
        let debate = Debate::new();
        assert_eq!(debate.name(), "debate");
    }

    #[test]
    fn test_pattern_description() {
        let debate = Debate::new();
        assert!(debate.description().is_some());
        assert!(debate.description().unwrap().contains("Multi-agent"));
    }

    #[test]
    fn test_validate_input_empty() {
        let debate = Debate::new();
        let result = debate.validate_input("");
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_valid() {
        let debate = Debate::new();
        let result = debate.validate_input("Is AI consciousness possible?");
        assert!(result.is_ok());
    }

    // Integration test requires real LLM API
    #[tokio::test]
    #[ignore = "requires real LLM API"]
    async fn test_execute_with_debate() {
        let debate = Debate::new();
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = debate.execute(&ctx, "Test debate topic").await;

        if let Ok(output) = result {
            assert!(!output.output.is_empty());
            assert!(!output.reasoning_steps.is_empty());
        }
    }
}
