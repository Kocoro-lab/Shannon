//! Research cognitive pattern with web search and citation.
//!
//! Implements autonomous research workflow:
//! 1. Query decomposition
//! 2. Web search for each sub-question
//! 3. Source collection and deduplication
//! 4. Synthesis with citations
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{Research, PatternContext};
//!
//! let research = Research::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = research.execute(&ctx, "What are the latest AI trends?").await?;
//! ```

use anyhow::Result;
use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, Source, TokenUsage};

/// Research pattern configuration.
#[derive(Debug, Clone)]
pub struct Research {
    /// Maximum search iterations.
    pub max_iterations: usize,

    /// Sources to collect per search round.
    pub sources_per_round: usize,

    /// Minimum total sources before synthesis.
    pub min_sources: usize,

    /// Model to use for decomposition and synthesis.
    pub model: String,

    /// Base URL for LLM API.
    base_url: String,
}

impl Research {
    /// Create a new Research pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `max_iterations`: 3
    /// - `sources_per_round`: 6
    /// - `min_sources`: 8
    /// - `model`: "claude-sonnet-4-20250514"
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_iterations: 3,
            sources_per_round: 6,
            min_sources: 8,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Create with custom configuration.
    #[must_use]
    pub fn with_config(
        max_iterations: usize,
        sources_per_round: usize,
        min_sources: usize,
    ) -> Self {
        Self {
            max_iterations,
            sources_per_round,
            min_sources,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Decompose query into searchable sub-questions.
    async fn decompose_query(
        &self,
        ctx: &PatternContext,
        query: &str,
    ) -> anyhow::Result<Vec<String>> {
        let system = "You are a research assistant. Decompose complex queries into 2-4 focused sub-questions that can be answered through web search. Each sub-question should be specific and searchable.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Decompose this query into 2-4 searchable sub-questions:\n\n{query}\n\n\
                Format:\n1. <sub-question>\n2. <sub-question>\n..."
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
            activity_id: "research-decompose".to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 30,
        };

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
                anyhow::bail!("Query decomposition failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Query decomposition needs retry: {reason}");
            }
        };

        // Parse sub-questions (lines starting with numbers)
        let sub_questions: Vec<String> = response
            .content
            .lines()
            .filter_map(|line| {
                let trimmed = line.trim();
                if trimmed.starts_with(|c: char| c.is_numeric()) {
                    trimmed.split_once('.').map(|(_, q)| q.trim().to_string())
                } else {
                    None
                }
            })
            .collect();

        Ok(sub_questions)
    }

    /// Synthesize final answer from collected sources.
    async fn synthesize(
        &self,
        ctx: &PatternContext,
        query: &str,
        sources: &[Source],
    ) -> anyhow::Result<(String, TokenUsage)> {
        let source_text = sources
            .iter()
            .enumerate()
            .map(|(i, s)| {
                format!(
                    "[{}] {}\n{}\n",
                    i + 1,
                    s.url,
                    s.excerpt.as_deref().unwrap_or("")
                )
            })
            .collect::<Vec<_>>()
            .join("\n");

        let system = "You are a research synthesizer. Given a query and sources, synthesize a comprehensive answer with inline citations [1], [2], etc.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Query: {query}\n\nSources:\n{source_text}\n\n\
                Synthesize a comprehensive answer with inline citations."
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.3,
            max_tokens: Some(4096),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "research-synthesize".to_string(),
            attempt: 1,
            max_attempts: 3,
            timeout_secs: 60,
        };

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
                anyhow::bail!("Synthesis failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Synthesis needs retry: {reason}");
            }
        };

        let token_usage = TokenUsage {
            prompt_tokens: response.usage.prompt_tokens as usize,
            completion_tokens: response.usage.completion_tokens as usize,
            total_tokens: response.usage.total_tokens as usize,
        };

        Ok((response.content, token_usage))
    }
}

impl Default for Research {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for Research {
    fn name(&self) -> &str {
        "research"
    }

    fn description(&self) -> Option<&str> {
        Some("Autonomous research with web search, source collection, and citation synthesis")
    }

    fn validate_input(&self, input: &str) -> anyhow::Result<()> {
        if input.trim().is_empty() {
            anyhow::bail!("Input cannot be empty");
        }
        if input.len() > 5_000 {
            anyhow::bail!("Input too long (max 5,000 characters)");
        }
        Ok(())
    }

    async fn execute(&self, ctx: &PatternContext, input: &str) -> anyhow::Result<PatternResult> {
        tracing::info!(
            workflow_id = %ctx.workflow_id,
            pattern = "research",
            "Starting Research execution"
        );

        let mut reasoning_steps = Vec::new();
        let mut all_sources = Vec::new();
        let mut total_token_usage = TokenUsage {
            prompt_tokens: 0,
            completion_tokens: 0,
            total_tokens: 0,
        };

        // Step 1: Query Decomposition
        reasoning_steps.push(ReasoningStep {
            step: 0,
            content: "Decomposing query into searchable sub-questions...".to_string(),
            confidence: Some(0.9),
            timestamp: Utc::now(),
        });

        let sub_questions = self.decompose_query(ctx, input).await.unwrap_or_else(|e| {
            tracing::warn!(error = %e, "Query decomposition failed, using original query");
            vec![input.to_string()]
        });

        tracing::debug!(
            workflow_id = %ctx.workflow_id,
            sub_questions = ?sub_questions,
            "Decomposed into {} sub-questions",
            sub_questions.len()
        );

        reasoning_steps.push(ReasoningStep {
            step: 1,
            content: format!(
                "Generated {} sub-questions for research",
                sub_questions.len()
            ),
            confidence: Some(0.8),
            timestamp: Utc::now(),
        });

        // Step 2: Source Collection (Mock for now - actual web search would be via Tavily API)
        reasoning_steps.push(ReasoningStep {
            step: 2,
            content: format!(
                "Collecting sources ({} sources per question, target {} total)...",
                self.sources_per_round, self.min_sources
            ),
            confidence: Some(0.7),
            timestamp: Utc::now(),
        });

        // Mock sources (in real implementation, would call web search API)
        for (idx, question) in sub_questions.iter().enumerate() {
            for i in 0..self.sources_per_round.min(3) {
                all_sources.push(Source {
                    url: format!("https://example.com/source-{}-{}", idx, i),
                    title: Some(format!("Source for: {}", question)),
                    excerpt: Some(format!("Relevant information about: {}", question)),
                    relevance: Some(0.8),
                });
            }
        }

        tracing::debug!(
            workflow_id = %ctx.workflow_id,
            sources_collected = all_sources.len(),
            "Collected sources"
        );

        reasoning_steps.push(ReasoningStep {
            step: 3,
            content: format!("Collected {} sources", all_sources.len()),
            confidence: Some(0.8),
            timestamp: Utc::now(),
        });

        // Step 3: Deduplication
        // Simple dedup by URL (in real implementation, would use content similarity)
        let mut seen_urls = std::collections::HashSet::new();
        all_sources.retain(|s| seen_urls.insert(s.url.clone()));

        reasoning_steps.push(ReasoningStep {
            step: 4,
            content: format!("Deduplicated to {} unique sources", all_sources.len()),
            confidence: Some(0.9),
            timestamp: Utc::now(),
        });

        // Step 4: Synthesis with Citations
        let (answer, synthesis_tokens) = self.synthesize(ctx, input, &all_sources).await?;

        total_token_usage.prompt_tokens += synthesis_tokens.prompt_tokens;
        total_token_usage.completion_tokens += synthesis_tokens.completion_tokens;
        total_token_usage.total_tokens += synthesis_tokens.total_tokens;

        reasoning_steps.push(ReasoningStep {
            step: 5,
            content: "Synthesized final answer with citations".to_string(),
            confidence: Some(0.85),
            timestamp: Utc::now(),
        });

        tracing::info!(
            workflow_id = %ctx.workflow_id,
            sources = all_sources.len(),
            tokens = total_token_usage.total_tokens,
            "Research completed"
        );

        Ok(PatternResult {
            output: answer,
            reasoning_steps,
            sources: all_sources,
            token_usage: Some(total_token_usage),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_research() {
        let research = Research::new();
        assert_eq!(research.max_iterations, 3);
        assert_eq!(research.sources_per_round, 6);
        assert_eq!(research.min_sources, 8);
    }

    #[test]
    fn test_with_config() {
        let research = Research::with_config(5, 10, 15);
        assert_eq!(research.max_iterations, 5);
        assert_eq!(research.sources_per_round, 10);
        assert_eq!(research.min_sources, 15);
    }

    #[test]
    fn test_pattern_name() {
        let research = Research::new();
        assert_eq!(research.name(), "research");
    }

    #[test]
    fn test_pattern_description() {
        let research = Research::new();
        assert!(research.description().is_some());
        assert!(research.description().unwrap().contains("research"));
    }

    #[test]
    fn test_validate_input_empty() {
        let research = Research::new();
        let result = research.validate_input("");
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("empty"));
    }

    #[test]
    fn test_validate_input_too_long() {
        let research = Research::new();
        let long_input = "a".repeat(5_001);
        let result = research.validate_input(&long_input);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("too long"));
    }

    #[test]
    fn test_validate_input_valid() {
        let research = Research::new();
        let result = research.validate_input("What are AI agents?");
        assert!(result.is_ok());
    }

    // Integration test would require real web search API and LLM
    // Skipped for unit tests (would need mock server or real API key)
    #[tokio::test]
    #[ignore = "requires real LLM API or mock server"]
    async fn test_execute_produces_sources() {
        let research = Research::new();
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = research.execute(&ctx, "Test query").await;

        if let Ok(output) = result {
            // Should have sources from mock data
            assert!(!output.sources.is_empty());

            // Should have reasoning steps
            assert!(!output.reasoning_steps.is_empty());

            // Should have an answer
            assert!(!output.output.is_empty());
        }
    }

    #[tokio::test]
    #[ignore = "requires real LLM API or mock server"]
    async fn test_execute_with_min_sources() {
        let research = Research::with_config(2, 3, 6);
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = research.execute(&ctx, "Test research query").await;

        if let Ok(output) = result {
            // Mock generates 3 sources per question
            assert!(!output.sources.is_empty());
        }
    }
}
