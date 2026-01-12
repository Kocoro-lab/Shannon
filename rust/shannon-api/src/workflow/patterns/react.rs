//! ReAct (Reason-Act-Observe) cognitive pattern.
//!
//! Implements iterative loop of reasoning, action execution, and observation.
//! Enables multi-step tool usage with feedback loops for autonomous task completion.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{ReAct, PatternContext};
//!
//! let react = ReAct::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = react.execute(&ctx, "What's the weather in Paris?").await?;
//! ```

use anyhow::Result;
use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    Activity, ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, TokenUsage};

/// ReAct pattern configuration.
#[derive(Debug, Clone)]
pub struct ReAct {
    /// Maximum reasoning-action iterations.
    pub max_iterations: usize,

    /// Model to use.
    pub model: String,

    /// Base URL for LLM API.
    base_url: String,
}

impl ReAct {
    /// Create a new ReAct pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `max_iterations`: 5
    /// - `model`: "claude-sonnet-4-20250514"
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 5,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Create with custom max iterations.
    #[must_use]
    pub fn with_max_iterations(mut self, max_iterations: usize) -> Self {
        self.max_iterations = max_iterations;
        self
    }

    /// Check if response contains final answer marker.
    fn has_final_answer(&self, content: &str) -> bool {
        content.to_uppercase().contains("FINAL ANSWER:")
            || content.to_lowercase().contains("task complete")
            || content.to_lowercase().contains("answer:")
    }

    /// Extract tool name and parameters from LLM response.
    fn parse_tool_call(&self, content: &str) -> Option<(String, String)> {
        // Look for patterns like "Action: tool_name(params)" or "Tool: tool_name"
        for line in content.lines() {
            let line = line.trim();
            if line.to_lowercase().starts_with("action:")
                || line.to_lowercase().starts_with("tool:")
            {
                if let Some((_, rest)) = line.split_once(':') {
                    let rest = rest.trim();
                    // Extract tool name (everything before parenthesis or end of string)
                    let tool_name = rest
                        .split(|c| c == '(' || c == ' ')
                        .next()
                        .unwrap_or("")
                        .trim()
                        .to_string();

                    if !tool_name.is_empty() {
                        return Some((tool_name, rest.to_string()));
                    }
                }
            }
        }
        None
    }

    /// Execute a tool (mock implementation).
    async fn execute_tool(&self, tool_name: &str, params: &str) -> Result<String> {
        // Mock tool execution (real implementation would delegate to tool activity)
        tracing::debug!(tool = tool_name, "Executing tool (mock)");

        match tool_name {
            "web_search" => Ok(format!("Search results for: {params}")),
            "calculator" => Ok("42".to_string()),
            _ => Ok(format!("Tool {tool_name} executed successfully")),
        }
    }
}

impl Default for ReAct {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for ReAct {
    fn name(&self) -> &str {
        "react"
    }

    fn description(&self) -> Option<&str> {
        Some("Reason-Act-Observe loop for multi-step tool usage with feedback")
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
            pattern = "react",
            max_iterations = self.max_iterations,
            "Starting ReAct execution"
        );

        let mut reasoning_steps = Vec::new();
        let mut conversation_history = Vec::new();
        let mut final_answer = None;

        let system_prompt = "You are an autonomous agent using the ReAct framework. For each step:
1. Reason: Think about what needs to be done
2. Act: Specify a tool to use (format: 'Action: tool_name(params)')
3. Observe: Analyze the tool result

Continue until you have a final answer, then respond with 'FINAL ANSWER: <answer>'.

Available tools: web_search, calculator";

        for iteration in 0..self.max_iterations {
            tracing::debug!(
                workflow_id = %ctx.workflow_id,
                iteration,
                "ReAct iteration"
            );

            // Reason step
            let user_message = if iteration == 0 {
                format!("Task: {input}\n\nLet's solve this step by step using the ReAct framework.")
            } else {
                "Continue with the next step or provide FINAL ANSWER if complete.".to_string()
            };

            conversation_history.push(Message {
                role: "user".to_string(),
                content: user_message,
            });

            // Call LLM
            let request = LlmRequest {
                model: self.model.clone(),
                system: system_prompt.to_string(),
                messages: conversation_history.clone(),
                temperature: 0.7,
                max_tokens: Some(1024),
            };

            let activity_ctx = ActivityContext {
                workflow_id: ctx.workflow_id.clone(),
                activity_id: format!("react-reason-{iteration}"),
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
                    anyhow::bail!("ReAct reasoning failed: {error}");
                }
                durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                    anyhow::bail!("ReAct reasoning needs retry: {reason}");
                }
            };

            // Add to history
            conversation_history.push(Message {
                role: "assistant".to_string(),
                content: response.content.clone(),
            });

            // Record reasoning step
            reasoning_steps.push(ReasoningStep {
                step: iteration * 3,
                content: format!("Reason: {}", response.content),
                confidence: Some(0.8),
                timestamp: Utc::now(),
            });

            // Check for final answer
            if self.has_final_answer(&response.content) {
                final_answer = response
                    .content
                    .split("FINAL ANSWER:")
                    .nth(1)
                    .or_else(|| response.content.split("Answer:").nth(1))
                    .map(|s| s.trim().to_string());

                tracing::info!(
                    workflow_id = %ctx.workflow_id,
                    iteration,
                    "ReAct completed with final answer"
                );
                break;
            }

            // Parse tool call
            if let Some((tool_name, params)) = self.parse_tool_call(&response.content) {
                // Act step
                reasoning_steps.push(ReasoningStep {
                    step: iteration * 3 + 1,
                    content: format!("Action: {}({})", tool_name, params),
                    confidence: Some(0.9),
                    timestamp: Utc::now(),
                });

                // Execute tool
                let tool_result = self.execute_tool(&tool_name, &params).await?;

                // Observe step
                reasoning_steps.push(ReasoningStep {
                    step: iteration * 3 + 2,
                    content: format!("Observation: {}", tool_result),
                    confidence: Some(0.9),
                    timestamp: Utc::now(),
                });

                // Add observation to conversation
                conversation_history.push(Message {
                    role: "user".to_string(),
                    content: format!("Observation: {tool_result}"),
                });
            }
        }

        let output = final_answer
            .unwrap_or_else(|| "Max iterations reached without final answer".to_string());

        Ok(PatternResult {
            output,
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
    fn test_new_react() {
        let react = ReAct::new();
        assert_eq!(react.max_iterations, 5);
        assert_eq!(react.model, "claude-sonnet-4-20250514");
    }

    #[test]
    fn test_with_max_iterations() {
        let react = ReAct::new().with_max_iterations(10);
        assert_eq!(react.max_iterations, 10);
    }

    #[test]
    fn test_pattern_name() {
        let react = ReAct::new();
        assert_eq!(react.name(), "react");
    }

    #[test]
    fn test_pattern_description() {
        let react = ReAct::new();
        assert!(react.description().is_some());
        assert!(react.description().unwrap().contains("Reason-Act-Observe"));
    }

    #[test]
    fn test_has_final_answer() {
        let react = ReAct::new();
        assert!(react.has_final_answer("Step 1\nFINAL ANSWER: The answer is 42"));
        assert!(react.has_final_answer("Task complete: result is ready"));
        assert!(react.has_final_answer("Answer: The capital is Paris"));
        assert!(!react.has_final_answer("Still working on it"));
    }

    #[test]
    fn test_parse_tool_call() {
        let react = ReAct::new();

        let result = react.parse_tool_call("Action: web_search(Paris weather)");
        assert!(result.is_some());
        let (tool, _params) = result.unwrap();
        assert_eq!(tool, "web_search");

        let result = react.parse_tool_call("Tool: calculator");
        assert!(result.is_some());
        let (tool, _) = result.unwrap();
        assert_eq!(tool, "calculator");

        let result = react.parse_tool_call("No tool here");
        assert!(result.is_none());
    }

    #[test]
    fn test_validate_input_empty() {
        let react = ReAct::new();
        let result = react.validate_input("");
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_too_long() {
        let react = ReAct::new();
        let long_input = "a".repeat(5_001);
        let result = react.validate_input(&long_input);
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_valid() {
        let react = ReAct::new();
        let result = react.validate_input("Find the weather in Paris");
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_execute_tool_mock() {
        let react = ReAct::new();

        let result = react.execute_tool("web_search", "test query").await;
        assert!(result.is_ok());
        assert!(result.unwrap().contains("Search results"));
    }

    // Integration test requires real LLM API
    #[tokio::test]
    #[ignore = "requires real LLM API"]
    async fn test_execute_with_tools() {
        let react = ReAct::new();
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = react.execute(&ctx, "Test task with tools").await;

        if let Ok(output) = result {
            assert!(!output.output.is_empty());
            assert!(!output.reasoning_steps.is_empty());
        }
    }
}
