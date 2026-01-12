//! Chain of Thought (CoT) cognitive pattern.
//!
//! Implements step-by-step reasoning where the LLM explicitly articulates
//! its thought process before arriving at a final answer.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{ChainOfThought, PatternContext};
//!
//! let cot = ChainOfThought::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = cot.execute(&ctx, "What is 2+2?").await?;
//! ```

use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, TokenUsage};

/// Chain of Thought pattern configuration.
#[derive(Debug, Clone)]
pub struct ChainOfThought {
    /// Maximum reasoning iterations before synthesizing answer.
    pub max_iterations: usize,

    /// Model to use for reasoning.
    pub model: String,

    /// Temperature for sampling.
    pub temperature: f32,

    /// Base URL for LLM API.
    base_url: String,
}

impl ChainOfThought {
    /// Create a new Chain of Thought pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `max_iterations`: 5
    /// - `model`: "claude-sonnet-4-20250514"
    /// - `temperature`: 0.7
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 5,
            model: "claude-sonnet-4-20250514".to_string(),
            temperature: 0.7,
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Create with custom max iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max_iterations: usize) -> Self {
        self.max_iterations = max_iterations;
        self
    }

    /// Create with custom model.
    #[must_use]
    pub fn with_model(mut self, model: impl Into<String>) -> Self {
        self.model = model.into();
        self
    }

    /// Create with custom temperature.
    #[must_use]
    pub fn with_temperature(mut self, temperature: f32) -> Self {
        self.temperature = temperature;
        self
    }

    /// Extract reasoning steps from LLM response.
    ///
    /// Parses responses that follow the format:
    /// "Step 1: <thought>\nStep 2: <thought>\n...\nFinal Answer: <answer>"
    fn parse_reasoning_steps(&self, content: &str) -> Vec<String> {
        content
            .lines()
            .filter(|line| {
                line.trim().starts_with("Step")
                    || line.trim().starts_with("Thought")
                    || line.trim().starts_with('-')
            })
            .map(|line| line.trim().to_string())
            .collect()
    }

    /// Check if response contains a final answer.
    fn has_final_answer(&self, content: &str) -> bool {
        content.to_lowercase().contains("final answer")
            || content.to_lowercase().contains("therefore")
            || content.to_lowercase().contains("in conclusion")
    }
}

impl Default for ChainOfThought {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for ChainOfThought {
    fn name(&self) -> &str {
        "chain_of_thought"
    }

    fn description(&self) -> Option<&str> {
        Some("Step-by-step reasoning with explicit thought articulation")
    }

    fn validate_input(&self, input: &str) -> anyhow::Result<()> {
        if input.trim().is_empty() {
            anyhow::bail!("Input cannot be empty");
        }
        if input.len() > 10_000 {
            anyhow::bail!("Input too long (max 10,000 characters)");
        }
        Ok(())
    }

    async fn execute(&self, ctx: &PatternContext, input: &str) -> anyhow::Result<PatternResult> {
        tracing::info!(
            workflow_id = %ctx.workflow_id,
            pattern = "chain_of_thought",
            max_iterations = self.max_iterations,
            "Starting Chain of Thought execution"
        );

        let mut reasoning_steps = Vec::new();
        let mut total_prompt_tokens = 0_u32;
        let mut total_completion_tokens = 0_u32;
        let mut total_total_tokens = 0_u32;

        let system_prompt = format!(
            "You are an expert reasoning assistant. Use Chain of Thought reasoning to answer questions.

For each question:
1. Break down the problem into clear steps
2. Show your thinking process explicitly
3. Number each reasoning step (Step 1, Step 2, etc.)
4. After all reasoning steps, provide a \"Final Answer:\" section

Be thorough but concise. Show your work clearly. If you reach a conclusion before {} iterations, include \"Final Answer:\" to indicate completion.",
            self.max_iterations
        );

        let mut conversation_history = Vec::new();
        let mut final_answer = None;

        // Iterative reasoning loop
        for iteration in 0..self.max_iterations {
            tracing::debug!(
                workflow_id = %ctx.workflow_id,
                iteration,
                "Chain of Thought iteration"
            );

            // Build prompt for this iteration
            let user_message = if iteration == 0 {
                format!("Question: {input}\n\nPlease reason through this step by step.")
            } else {
                "Continue reasoning. If you've reached a conclusion, state your \"Final Answer:\"."
                    .to_string()
            };

            conversation_history.push(Message {
                role: "user".to_string(),
                content: user_message,
            });

            // Call LLM
            let request = LlmRequest {
                model: self.model.clone(),
                system: system_prompt.clone(),
                messages: conversation_history.clone(),
                temperature: self.temperature,
                max_tokens: Some(2048),
            };

            let activity_ctx = ActivityContext {
                workflow_id: ctx.workflow_id.clone(),
                activity_id: format!("cot-{iteration}"),
                attempt: 1,
                max_attempts: 3,
                timeout_secs: 60,
            };

            // Create activity for this call
            let llm_activity = LlmReasonActivity::new(self.base_url.clone(), None);

            use durable_shannon::activities::Activity;
            let result = llm_activity
                .execute(&activity_ctx, serde_json::to_value(&request)?)
                .await;

            let response = match result {
                durable_shannon::activities::ActivityResult::Success(value) => {
                    serde_json::from_value::<durable_shannon::activities::llm::LlmResponse>(value)?
                }
                durable_shannon::activities::ActivityResult::Failure { error, .. } => {
                    anyhow::bail!("LLM call failed: {error}");
                }
                durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                    anyhow::bail!("LLM call needs retry: {reason}");
                }
            };

            // Track tokens
            total_prompt_tokens += response.usage.prompt_tokens;
            total_completion_tokens += response.usage.completion_tokens;
            total_total_tokens += response.usage.total_tokens;

            // Add assistant response to history
            conversation_history.push(Message {
                role: "assistant".to_string(),
                content: response.content.clone(),
            });

            // Parse reasoning steps from this iteration
            let steps = self.parse_reasoning_steps(&response.content);
            for (step_idx, step_content) in steps.iter().enumerate() {
                reasoning_steps.push(ReasoningStep {
                    step: reasoning_steps.len(),
                    content: step_content.clone(),
                    confidence: Some(0.8), // Default confidence
                    timestamp: Utc::now(),
                });

                tracing::debug!(
                    workflow_id = %ctx.workflow_id,
                    iteration,
                    step = step_idx,
                    "Reasoning step: {}",
                    step_content
                );
            }

            // Check for final answer
            if self.has_final_answer(&response.content) {
                // Extract final answer (text after "Final Answer:" or similar markers)
                final_answer = response
                    .content
                    .split("Final Answer:")
                    .nth(1)
                    .or_else(|| response.content.split("Therefore,").nth(1))
                    .or_else(|| response.content.split("In conclusion,").nth(1))
                    .map(|s| s.trim().to_string());

                tracing::info!(
                    workflow_id = %ctx.workflow_id,
                    iteration,
                    "Chain of Thought completed with final answer"
                );
                break;
            }

            // If last iteration and no final answer, use last response
            if iteration == self.max_iterations - 1 {
                final_answer = Some(response.content.clone());
                tracing::warn!(
                    workflow_id = %ctx.workflow_id,
                    "Chain of Thought reached max iterations without explicit final answer"
                );
            }
        }

        let output = final_answer.unwrap_or_else(|| {
            "Unable to determine final answer within iteration limit".to_string()
        });

        Ok(PatternResult {
            output,
            reasoning_steps,
            sources: vec![], // CoT doesn't use external sources
            token_usage: Some(TokenUsage {
                prompt_tokens: total_prompt_tokens as usize,
                completion_tokens: total_completion_tokens as usize,
                total_tokens: total_total_tokens as usize,
            }),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_chain_of_thought() {
        let cot = ChainOfThought::new();
        assert_eq!(cot.max_iterations, 5);
        assert_eq!(cot.model, "claude-sonnet-4-20250514");
        assert_eq!(cot.temperature, 0.7);
    }

    #[test]
    fn test_with_max_iterations() {
        let cot = ChainOfThought::new().with_max_iterations(10);
        assert_eq!(cot.max_iterations, 10);
    }

    #[test]
    fn test_with_model() {
        let cot = ChainOfThought::new().with_model("gpt-4");
        assert_eq!(cot.model, "gpt-4");
    }

    #[test]
    fn test_with_temperature() {
        let cot = ChainOfThought::new().with_temperature(0.5);
        assert_eq!(cot.temperature, 0.5);
    }

    #[test]
    fn test_parse_reasoning_steps() {
        let cot = ChainOfThought::new();
        let content = "Step 1: First, we need to understand the problem\n\
            Step 2: Next, we analyze the data\n\
            Some other text\n\
            Step 3: Finally, we draw conclusions\n\
            Final Answer: The answer is 42";

        let steps = cot.parse_reasoning_steps(content);
        assert_eq!(steps.len(), 3);
        assert!(steps[0].contains("Step 1"));
        assert!(steps[1].contains("Step 2"));
        assert!(steps[2].contains("Step 3"));
    }

    #[test]
    fn test_has_final_answer() {
        let cot = ChainOfThought::new();

        assert!(cot.has_final_answer("Step 1: Think\nFinal Answer: 42"));
        assert!(cot.has_final_answer("Therefore, the answer is 42"));
        assert!(cot.has_final_answer("In conclusion, we find that 42"));
        assert!(!cot.has_final_answer("Step 1: Think\nStep 2: Analyze"));
    }

    #[test]
    fn test_validate_input_empty() {
        let cot = ChainOfThought::new();
        let result = cot.validate_input("");
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("empty"));
    }

    #[test]
    fn test_validate_input_too_long() {
        let cot = ChainOfThought::new();
        let long_input = "a".repeat(10_001);
        let result = cot.validate_input(&long_input);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("too long"));
    }

    #[test]
    fn test_validate_input_valid() {
        let cot = ChainOfThought::new();
        let result = cot.validate_input("What is 2+2?");
        assert!(result.is_ok());
    }

    #[test]
    fn test_pattern_name() {
        let cot = ChainOfThought::new();
        assert_eq!(cot.name(), "chain_of_thought");
    }

    #[test]
    fn test_pattern_description() {
        let cot = ChainOfThought::new();
        assert!(cot.description().is_some());
        assert!(cot
            .description()
            .unwrap()
            .contains("Step-by-step reasoning"));
    }
}
