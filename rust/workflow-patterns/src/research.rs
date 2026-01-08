//! Research cognitive pattern.
//!
//! Implements deep research with citations where the model:
//! 1. Decomposes the query into sub-questions
//! 2. Searches for information
//! 3. Synthesizes findings with citations
//! 4. Produces a comprehensive research report

use chrono::Utc;

use crate::{CognitivePattern, PatternInput, PatternOutput, ReasoningStep, Source, TokenUsage};

/// Research pattern implementation.
#[derive(Debug, Default)]
pub struct Research {
    /// Maximum research depth.
    pub max_depth: usize,
    /// Sources per round.
    pub sources_per_round: usize,
    /// Minimum sources required.
    pub min_sources: usize,
    /// Default model.
    pub default_model: String,
}

impl Research {
    /// Create a new Research pattern.
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_depth: 5,
            sources_per_round: 6,
            min_sources: 8,
            default_model: "claude-sonnet-4-20250514".to_string(),
        }
    }

    /// Set the maximum depth.
    #[must_use]
    pub fn with_max_depth(mut self, depth: usize) -> Self {
        self.max_depth = depth;
        self
    }

    /// Set the minimum sources.
    #[must_use]
    pub fn with_min_sources(mut self, min: usize) -> Self {
        self.min_sources = min;
        self
    }
}

#[async_trait::async_trait]
impl CognitivePattern for Research {
    fn name(&self) -> &'static str {
        "research"
    }

    async fn execute(&self, input: PatternInput) -> anyhow::Result<PatternOutput> {
        let start = std::time::Instant::now();
        let max_depth = input.max_iterations.unwrap_or(self.max_depth);

        let mut reasoning_steps: Vec<ReasoningStep> = Vec::new();
        let mut sources: Vec<Source> = Vec::new();
        let mut total_tokens = TokenUsage::default();
        let mut step_count = 0;

        tracing::info!(
            "Starting Research with query: {} (max depth {})",
            input.query,
            max_depth
        );

        // Step 1: Decompose query
        step_count += 1;
        let sub_questions = vec![
            format!("What is {}?", input.query.split_whitespace().take(3).collect::<Vec<_>>().join(" ")),
            format!("Why is {} important?", input.query.split_whitespace().take(2).collect::<Vec<_>>().join(" ")),
            format!("What are the implications of {}?", input.query.split_whitespace().take(3).collect::<Vec<_>>().join(" ")),
        ];

        reasoning_steps.push(ReasoningStep {
            step: step_count,
            step_type: "decomposition".to_string(),
            content: format!(
                "Decomposed query into {} sub-questions: {:?}",
                sub_questions.len(),
                sub_questions
            ),
            timestamp: Utc::now(),
        });

        total_tokens.prompt_tokens += 100;
        total_tokens.completion_tokens += 150;
        total_tokens.total_tokens += 250;

        // Step 2: Research each sub-question
        for (i, question) in sub_questions.iter().enumerate() {
            step_count += 1;

            // Simulate search
            reasoning_steps.push(ReasoningStep {
                step: step_count,
                step_type: "search".to_string(),
                content: format!("Searching for: {question}"),
                timestamp: Utc::now(),
            });

            // Simulate finding sources
            for j in 0..self.sources_per_round.min(3) {
                sources.push(Source {
                    title: format!("Source {} for sub-question {}", j + 1, i + 1),
                    url: Some(format!("https://example.com/source-{}-{}", i, j)),
                    snippet: Some(format!(
                        "Relevant information about '{}'. This is placeholder content.",
                        question
                    )),
                    confidence: 0.7 + (j as f64 * 0.05),
                });
            }

            step_count += 1;
            reasoning_steps.push(ReasoningStep {
                step: step_count,
                step_type: "analysis".to_string(),
                content: format!(
                    "Analyzed {} sources for sub-question: {}",
                    self.sources_per_round.min(3),
                    question
                ),
                timestamp: Utc::now(),
            });

            total_tokens.prompt_tokens += 200;
            total_tokens.completion_tokens += 200;
            total_tokens.total_tokens += 400;
        }

        // Step 3: Synthesize findings
        step_count += 1;
        reasoning_steps.push(ReasoningStep {
            step: step_count,
            step_type: "synthesis".to_string(),
            content: format!(
                "Synthesizing {} sources into a comprehensive research report.",
                sources.len()
            ),
            timestamp: Utc::now(),
        });

        total_tokens.prompt_tokens += 300;
        total_tokens.completion_tokens += 500;
        total_tokens.total_tokens += 800;

        let final_answer = format!(
            r#"# Research Report: {}

## Summary
This research investigated "{}" through {} sub-questions, analyzing {} sources.

## Findings
Based on the research conducted:
1. The topic has multiple facets requiring comprehensive analysis.
2. Sources were gathered from various domains.
3. Key insights were synthesized from the collected information.

## Sources
{} sources were consulted during this research.

Note: Full LLM integration pending for actual research execution."#,
            input.query,
            input.query,
            sub_questions.len(),
            sources.len(),
            sources.len()
        );

        Ok(PatternOutput {
            answer: final_answer,
            confidence: 0.8,
            reasoning_steps,
            sources,
            token_usage: total_tokens,
            duration_ms: start.elapsed().as_millis() as u64,
        })
    }

    fn default_config(&self) -> serde_json::Value {
        serde_json::json!({
            "max_depth": self.max_depth,
            "sources_per_round": self.sources_per_round,
            "min_sources": self.min_sources,
            "model": self.default_model
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_research_basic() {
        let research = Research::new();
        let input = PatternInput::new("What are the benefits of Rust?");
        
        let output = research.execute(input).await.unwrap();
        
        assert!(!output.sources.is_empty());
        assert!(!output.reasoning_steps.is_empty());
        assert!(output.answer.contains("Research Report"));
    }
}
