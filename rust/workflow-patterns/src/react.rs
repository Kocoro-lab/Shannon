//! ReAct (Reason-Act-Observe) cognitive pattern.
//!
//! Implements an interleaved reasoning and action loop where the model:
//! 1. Reasons about what to do next
//! 2. Takes an action (uses a tool)
//! 3. Observes the result
//! 4. Repeats until the task is complete

use chrono::Utc;

use crate::{CognitivePattern, PatternInput, PatternOutput, ReasoningStep, TokenUsage};

/// ReAct pattern implementation.
#[derive(Debug, Default)]
pub struct ReAct {
    /// Maximum iterations.
    pub max_iterations: usize,
    /// Default model.
    pub default_model: String,
    /// Available tools.
    pub available_tools: Vec<String>,
}

impl ReAct {
    /// Create a new ReAct pattern.
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 15,
            default_model: "claude-sonnet-4-20250514".to_string(),
            available_tools: vec![
                "web_search".to_string(),
                "calculator".to_string(),
                "code_executor".to_string(),
            ],
        }
    }

    /// Set the maximum iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max: usize) -> Self {
        self.max_iterations = max;
        self
    }

    /// Add available tools.
    #[must_use]
    pub fn with_tools(mut self, tools: Vec<String>) -> Self {
        self.available_tools = tools;
        self
    }

    /// Build the system prompt for ReAct.
    fn system_prompt(&self) -> String {
        let tools_list = self.available_tools.join(", ");
        format!(
            r#"You are a problem-solving assistant that uses the ReAct framework.

Available tools: {tools_list}

For each step:
1. THOUGHT: Reason about what you need to do next
2. ACTION: Choose a tool and specify its input
3. OBSERVATION: (Will be provided after action execution)

Format your response as:
THOUGHT: <your reasoning>
ACTION: <tool_name>(<input>)

When you have enough information to answer, respond with:
THOUGHT: <final reasoning>
ANSWER: <your final answer>

Be thorough and use tools when needed."#
        )
    }
}

#[async_trait::async_trait]
impl CognitivePattern for ReAct {
    fn name(&self) -> &'static str {
        "react"
    }

    async fn execute(&self, input: PatternInput) -> anyhow::Result<PatternOutput> {
        let start = std::time::Instant::now();
        let max_iterations = input.max_iterations.unwrap_or(self.max_iterations);

        let mut reasoning_steps: Vec<ReasoningStep> = Vec::new();
        let mut total_tokens = TokenUsage::default();

        tracing::info!(
            "Starting ReAct with query: {} (max {} iterations)",
            input.query,
            max_iterations
        );

        for step in 0..max_iterations {
            // Generate thought
            let thought = format!(
                "Analyzing the query '{}'. Step {}. \
                Considering available tools: {:?}",
                input.query,
                step + 1,
                self.available_tools
            );

            reasoning_steps.push(ReasoningStep {
                step: step * 3 + 1,
                step_type: "thought".to_string(),
                content: thought,
                timestamp: Utc::now(),
            });

            // Simulate action
            let action = format!(
                "ACTION: web_search(\"{}\")",
                input.query.split_whitespace().take(5).collect::<Vec<_>>().join(" ")
            );

            reasoning_steps.push(ReasoningStep {
                step: step * 3 + 2,
                step_type: "action".to_string(),
                content: action,
                timestamp: Utc::now(),
            });

            // Simulate observation
            let observation = format!(
                "OBSERVATION: Search results for query. \
                This is a placeholder - actual tool execution pending."
            );

            reasoning_steps.push(ReasoningStep {
                step: step * 3 + 3,
                step_type: "observation".to_string(),
                content: observation,
                timestamp: Utc::now(),
            });

            total_tokens.prompt_tokens += 150;
            total_tokens.completion_tokens += 100;
            total_tokens.total_tokens += 250;

            // Simulate reaching a conclusion after a few iterations
            if step >= 2 {
                break;
            }
        }

        // Generate final answer
        let final_answer = format!(
            "Based on the ReAct reasoning loop for '{}', \
            the analysis was completed in {} steps. \
            Full tool integration pending.",
            input.query,
            reasoning_steps.len()
        );

        Ok(PatternOutput {
            answer: final_answer,
            confidence: 0.75,
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
            "tools": self.available_tools
        })
    }
}
