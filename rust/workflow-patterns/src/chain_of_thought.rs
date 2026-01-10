//! Chain of Thought (CoT) cognitive pattern.
//!
//! Implements step-by-step reasoning where the model explicitly shows
//! its thought process before arriving at a conclusion.

use chrono::Utc;

use crate::{CognitivePattern, PatternInput, PatternOutput, ReasoningStep, TokenUsage};

/// Chain of Thought pattern implementation.
#[derive(Debug, Default)]
pub struct ChainOfThought {
    /// Default maximum iterations.
    pub max_iterations: usize,
    /// Default model to use.
    pub default_model: String,
}

impl ChainOfThought {
    /// Create a new Chain of Thought pattern.
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 5,
            default_model: "claude-sonnet-4-20250514".to_string(),
        }
    }

    /// Set the maximum iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max: usize) -> Self {
        self.max_iterations = max;
        self
    }

    /// Set the default model.
    #[must_use]
    pub fn with_model(mut self, model: impl Into<String>) -> Self {
        self.default_model = model.into();
        self
    }

    /// Build the system prompt for CoT reasoning.
    #[allow(dead_code)]
    fn system_prompt(&self) -> String {
        r#"You are a reasoning assistant that thinks step by step.

For each step in your reasoning:
1. Clearly state what you're considering
2. Show your logical reasoning
3. Draw intermediate conclusions

When you've reached a final answer, clearly state it with "FINAL ANSWER:" prefix.

Be thorough but concise. Each step should build on the previous ones."#
            .to_string()
    }

    /// Build a prompt for a reasoning step.
    fn step_prompt(&self, query: &str, previous_thoughts: &[String]) -> String {
        let mut prompt = format!("Question: {query}\n\n");

        if !previous_thoughts.is_empty() {
            prompt.push_str("Previous reasoning steps:\n");
            for (i, thought) in previous_thoughts.iter().enumerate() {
                prompt.push_str(&format!("\nStep {}: {thought}", i + 1));
            }
            prompt.push_str("\n\nContinue reasoning to the next step or provide the final answer:");
        } else {
            prompt.push_str("Begin your step-by-step reasoning:");
        }

        prompt
    }
}

#[async_trait::async_trait]
impl CognitivePattern for ChainOfThought {
    fn name(&self) -> &'static str {
        "chain_of_thought"
    }

    async fn execute(&self, input: PatternInput) -> anyhow::Result<PatternOutput> {
        let start = std::time::Instant::now();
        let max_iterations = input.max_iterations.unwrap_or(self.max_iterations);
        let model = input.model.as_deref().unwrap_or(&self.default_model);

        let mut thoughts: Vec<String> = Vec::new();
        let mut reasoning_steps: Vec<ReasoningStep> = Vec::new();
        let mut total_tokens = TokenUsage::default();

        tracing::info!(
            "Starting Chain of Thought with query: {} (max {} iterations)",
            input.query,
            max_iterations
        );

        for step in 0..max_iterations {
            // Build the prompt for this step
            let _prompt = self.step_prompt(&input.query, &thoughts);

            tracing::debug!("CoT step {}: generating thought", step + 1);

            // TODO: Call LLM activity here
            // For now, simulate the response
            let thought = format!(
                "Step {} reasoning: Analyzing the query '{}'. \
                This is a placeholder - actual LLM integration pending.",
                step + 1,
                input.query
            );

            // Check if we've reached a final answer
            if thought.contains("FINAL ANSWER:") {
                thoughts.push(thought.clone());
                reasoning_steps.push(ReasoningStep {
                    step: step + 1,
                    step_type: "conclusion".to_string(),
                    content: thought,
                    timestamp: Utc::now(),
                });
                break;
            }

            thoughts.push(thought.clone());
            reasoning_steps.push(ReasoningStep {
                step: step + 1,
                step_type: "thought".to_string(),
                content: thought,
                timestamp: Utc::now(),
            });

            // Simulate token usage
            total_tokens.prompt_tokens += 100;
            total_tokens.completion_tokens += 50;
            total_tokens.total_tokens += 150;
        }

        // Synthesize final answer
        let final_answer = if let Some(last) = reasoning_steps.last() {
            if last.content.contains("FINAL ANSWER:") {
                last.content
                    .split("FINAL ANSWER:")
                    .nth(1)
                    .unwrap_or(&last.content)
                    .trim()
                    .to_string()
            } else {
                format!(
                    "Based on the reasoning steps, the answer to '{}' requires further analysis. \
                    Model: {}, Steps taken: {}",
                    input.query,
                    model,
                    reasoning_steps.len()
                )
            }
        } else {
            "No reasoning steps were generated.".to_string()
        };

        Ok(PatternOutput {
            answer: final_answer,
            confidence: 0.8,
            reasoning_steps,
            sources: Vec::new(),
            token_usage: total_tokens,
            duration_ms: start.elapsed().as_millis() as u64,
        })
    }

    fn default_config(&self) -> serde_json::Value {
        serde_json::json!({
            "max_iterations": self.max_iterations,
            "model": self.default_model,
            "temperature": 0.7
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_chain_of_thought_basic() {
        let cot = ChainOfThought::new();
        let input = PatternInput::new("What is 2 + 2?");
        
        let output = cot.execute(input).await.unwrap();
        
        assert!(!output.reasoning_steps.is_empty());
        assert!(!output.answer.is_empty());
    }
}
