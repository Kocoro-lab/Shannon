//! Debate cognitive pattern.
//!
//! Implements multi-perspective argumentation where multiple "agents"
//! debate a topic from different viewpoints to arrive at a balanced conclusion.

use chrono::Utc;

use crate::{CognitivePattern, PatternInput, PatternOutput, ReasoningStep, TokenUsage};

/// Debate pattern implementation.
#[derive(Debug, Default)]
pub struct Debate {
    /// Number of debate rounds.
    pub rounds: usize,
    /// Number of perspectives/debaters.
    pub perspectives: usize,
    /// Default model.
    pub default_model: String,
}

impl Debate {
    /// Create a new Debate pattern.
    #[must_use]
    pub fn new() -> Self {
        Self {
            rounds: 3,
            perspectives: 3,
            default_model: "claude-sonnet-4-20250514".to_string(),
        }
    }

    /// Set the number of rounds.
    #[must_use]
    pub fn with_rounds(mut self, rounds: usize) -> Self {
        self.rounds = rounds;
        self
    }

    /// Set the number of perspectives.
    #[must_use]
    pub fn with_perspectives(mut self, perspectives: usize) -> Self {
        self.perspectives = perspectives;
        self
    }

    /// Get perspective names.
    fn perspective_names(&self) -> Vec<&'static str> {
        vec![
            "Proponent",
            "Critic",
            "Synthesizer",
            "Devil's Advocate",
            "Pragmatist",
        ]
    }
}

#[async_trait::async_trait]
impl CognitivePattern for Debate {
    fn name(&self) -> &'static str {
        "debate"
    }

    async fn execute(&self, input: PatternInput) -> anyhow::Result<PatternOutput> {
        let start = std::time::Instant::now();
        let rounds = input.max_iterations.unwrap_or(self.rounds);
        let perspectives = self.perspective_names();

        let mut reasoning_steps: Vec<ReasoningStep> = Vec::new();
        let mut total_tokens = TokenUsage::default();
        let mut step_count = 0;

        tracing::info!(
            "Starting Debate with query: {} ({} rounds, {} perspectives)",
            input.query,
            rounds,
            self.perspectives
        );

        for round in 0..rounds {
            tracing::debug!("Debate round {}", round + 1);

            for (_i, perspective) in perspectives.iter().take(self.perspectives).enumerate() {
                step_count += 1;

                // Generate argument for this perspective
                let argument = format!(
                    "[Round {} - {}] Considering '{}' from this perspective: \
                    This is a placeholder argument. Full LLM integration pending.",
                    round + 1,
                    perspective,
                    input.query
                );

                reasoning_steps.push(ReasoningStep {
                    step: step_count,
                    step_type: format!("argument_{}", perspective.to_lowercase()),
                    content: argument,
                    timestamp: Utc::now(),
                });

                total_tokens.prompt_tokens += 120;
                total_tokens.completion_tokens += 100;
                total_tokens.total_tokens += 220;
            }

            // Cross-examination round
            if round < rounds - 1 {
                step_count += 1;
                reasoning_steps.push(ReasoningStep {
                    step: step_count,
                    step_type: "cross_examination".to_string(),
                    content: format!(
                        "Cross-examination of arguments from round {}. \
                        Identifying points of agreement and contention.",
                        round + 1
                    ),
                    timestamp: Utc::now(),
                });
            }
        }

        // Final synthesis
        step_count += 1;
        reasoning_steps.push(ReasoningStep {
            step: step_count,
            step_type: "synthesis".to_string(),
            content: format!(
                "Synthesizing {} arguments across {} rounds into a balanced conclusion.",
                self.perspectives * rounds,
                rounds
            ),
            timestamp: Utc::now(),
        });

        let final_answer = format!(
            "After {} rounds of debate with {} perspectives on '{}', \
            the synthesized conclusion considers multiple viewpoints. \
            Full LLM integration pending for actual argumentation.",
            rounds,
            self.perspectives,
            input.query
        );

        Ok(PatternOutput {
            answer: final_answer,
            confidence: 0.7,
            reasoning_steps,
            sources: Vec::new(),
            token_usage: total_tokens,
            duration_ms: start.elapsed().as_millis() as u64,
        })
    }

    fn default_config(&self) -> serde_json::Value {
        serde_json::json!({
            "rounds": self.rounds,
            "perspectives": self.perspectives,
            "model": self.default_model
        })
    }
}
