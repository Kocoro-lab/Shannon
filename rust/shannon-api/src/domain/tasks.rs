//! Task domain models and context parameters.
//!
//! Defines comprehensive task submission and context types for feature parity
//! with cloud stack.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Task context with comprehensive parameter support.
///
/// Provides fine-grained control over task execution, model selection,
/// research strategies, and context window management.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TaskContext {
    // Role and prompt configuration
    /// Role preset: analysis, research, writer, code
    #[serde(skip_serializing_if = "Option::is_none")]
    pub role: Option<String>,

    /// Custom system prompt override
    #[serde(skip_serializing_if = "Option::is_none")]
    pub system_prompt: Option<String>,

    /// Template parameters for prompt rendering
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prompt_params: Option<HashMap<String, serde_json::Value>>,

    // Model selection
    /// Model tier: small, medium, large
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_tier: Option<String>,

    /// Specific model override (e.g., "gpt-4o", "claude-3-5-sonnet")
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_override: Option<String>,

    /// Provider override: openai, anthropic, google, groq, xai
    #[serde(skip_serializing_if = "Option::is_none")]
    pub provider_override: Option<String>,

    // Template configuration
    /// Template name for template-based execution
    #[serde(skip_serializing_if = "Option::is_none")]
    pub template: Option<String>,

    /// Template version
    #[serde(skip_serializing_if = "Option::is_none")]
    pub template_version: Option<String>,

    /// Disable AI and use template-only mode
    #[serde(default)]
    pub disable_ai: bool,

    // Research configuration
    /// Enable Deep Research 2.0
    #[serde(default)]
    pub force_research: bool,

    /// Research strategy: quick, standard, deep, academic
    #[serde(skip_serializing_if = "Option::is_none")]
    pub research_strategy: Option<String>,

    /// Maximum concurrent research agents (1-20)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_concurrent_agents: Option<u32>,

    /// Enable citation verification
    #[serde(default)]
    pub enable_verification: bool,

    /// Enable iterative research loop
    #[serde(default)]
    pub iterative_research_enabled: bool,

    /// Maximum iterative research iterations (1-5)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub iterative_max_iterations: Option<u32>,

    /// Extract structured facts from research
    #[serde(default)]
    pub enable_fact_extraction: bool,

    /// Enable citation collection
    #[serde(default = "default_true")]
    pub enable_citations: bool,

    // ReAct configuration
    /// Maximum ReAct loop iterations
    #[serde(skip_serializing_if = "Option::is_none")]
    pub react_max_iterations: Option<u32>,

    // Context window management
    /// Maximum conversation history window size
    #[serde(skip_serializing_if = "Option::is_none")]
    pub history_window_size: Option<u32>,

    /// Number of early messages to keep (primers)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub primers_count: Option<u32>,

    /// Number of recent messages to keep
    #[serde(skip_serializing_if = "Option::is_none")]
    pub recents_count: Option<u32>,

    /// Trigger compression at this ratio of window (0.0-1.0)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub compression_trigger_ratio: Option<f32>,

    /// Compress to this ratio of window (0.0-1.0)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub compression_target_ratio: Option<f32>,
}

fn default_true() -> bool {
    true
}

impl TaskContext {
    /// Create a new empty context.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Set the role preset.
    #[must_use]
    pub fn with_role(mut self, role: impl Into<String>) -> Self {
        self.role = Some(role.into());
        self
    }

    /// Set the system prompt.
    #[must_use]
    pub fn with_system_prompt(mut self, prompt: impl Into<String>) -> Self {
        self.system_prompt = Some(prompt.into());
        self
    }

    /// Set the model tier.
    #[must_use]
    pub fn with_model_tier(mut self, tier: impl Into<String>) -> Self {
        self.model_tier = Some(tier.into());
        self
    }

    /// Set the model override.
    #[must_use]
    pub fn with_model_override(mut self, model: impl Into<String>) -> Self {
        self.model_override = Some(model.into());
        self
    }

    /// Set the provider override.
    #[must_use]
    pub fn with_provider_override(mut self, provider: impl Into<String>) -> Self {
        self.provider_override = Some(provider.into());
        self
    }

    /// Set the research strategy.
    #[must_use]
    pub fn with_research_strategy(mut self, strategy: impl Into<String>) -> Self {
        self.research_strategy = Some(strategy.into());
        self
    }

    /// Enable research mode.
    #[must_use]
    pub fn with_research(mut self) -> Self {
        self.force_research = true;
        self
    }

    /// Get the effective model tier, falling back to default.
    #[must_use]
    pub fn effective_tier(&self) -> &str {
        self.model_tier.as_deref().unwrap_or("medium")
    }

    /// Get the effective research strategy, falling back to default.
    #[must_use]
    pub fn effective_research_strategy(&self) -> &str {
        self.research_strategy.as_deref().unwrap_or("standard")
    }

    /// Check if role-based execution should bypass decomposition.
    #[must_use]
    pub fn should_skip_decomposition(&self) -> bool {
        self.role.is_some()
    }

    /// Validate context parameters.
    ///
    /// # Errors
    ///
    /// Returns error if parameters are invalid.
    pub fn validate(&self) -> Result<(), String> {
        // Validate model tier
        if let Some(ref tier) = self.model_tier {
            if !["small", "medium", "large"].contains(&tier.as_str()) {
                return Err(format!(
                    "Invalid model_tier: {}. Must be small, medium, or large",
                    tier
                ));
            }
        }

        // Validate research strategy
        if let Some(ref strategy) = self.research_strategy {
            if !["quick", "standard", "deep", "academic"].contains(&strategy.as_str()) {
                return Err(format!(
                    "Invalid research_strategy: {}. Must be quick, standard, deep, or academic",
                    strategy
                ));
            }
        }

        // Validate max_concurrent_agents
        if let Some(max_agents) = self.max_concurrent_agents {
            if !(1..=20).contains(&max_agents) {
                return Err(format!(
                    "max_concurrent_agents must be between 1 and 20, got {}",
                    max_agents
                ));
            }
        }

        // Validate iterative_max_iterations
        if let Some(max_iter) = self.iterative_max_iterations {
            if !(1..=5).contains(&max_iter) {
                return Err(format!(
                    "iterative_max_iterations must be between 1 and 5, got {}",
                    max_iter
                ));
            }
        }

        // Validate compression ratios
        if let Some(ratio) = self.compression_trigger_ratio {
            if !(0.0..=1.0).contains(&ratio) {
                return Err(format!(
                    "compression_trigger_ratio must be between 0.0 and 1.0, got {}",
                    ratio
                ));
            }
        }

        if let Some(ratio) = self.compression_target_ratio {
            if !(0.0..=1.0).contains(&ratio) {
                return Err(format!(
                    "compression_target_ratio must be between 0.0 and 1.0, got {}",
                    ratio
                ));
            }
        }

        Ok(())
    }
}

/// Model tier enumeration for tier-based routing.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ModelTier {
    /// Small/fast models (e.g., GPT-4o-mini, Claude Haiku)
    Small,
    /// Medium-capability models (e.g., GPT-4o, Claude Sonnet)
    Medium,
    /// Large/most capable models (e.g., GPT-4, Claude Opus)
    Large,
}

impl ModelTier {
    /// Get default model for this tier.
    ///
    /// Returns a hardcoded default model name for the tier.
    #[must_use]
    pub fn default_model(&self) -> &'static str {
        match self {
            Self::Small => "gpt-4o-mini",
            Self::Medium => "gpt-4o",
            Self::Large => "gpt-4-turbo",
        }
    }

    /// Get fallback model for this tier.
    ///
    /// Returns a fallback model if the primary model is unavailable.
    #[must_use]
    pub fn fallback_model(&self) -> &'static str {
        match self {
            Self::Small => "claude-3-5-haiku-latest",
            Self::Medium => "claude-3-5-sonnet-latest",
            Self::Large => "claude-3-opus-latest",
        }
    }
}

impl std::str::FromStr for ModelTier {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "small" => Ok(Self::Small),
            "medium" => Ok(Self::Medium),
            "large" => Ok(Self::Large),
            _ => Err(format!("Unknown model tier: {s}")),
        }
    }
}

impl std::fmt::Display for ModelTier {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Small => write!(f, "small"),
            Self::Medium => write!(f, "medium"),
            Self::Large => write!(f, "large"),
        }
    }
}

/// Research strategy presets.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ResearchStrategy {
    /// Quick research (1-2 agents, basic verification)
    Quick,
    /// Standard research (3-5 agents, standard verification)
    Standard,
    /// Deep research (5-10 agents, thorough verification)
    Deep,
    /// Academic research (10-20 agents, comprehensive verification)
    Academic,
}

impl ResearchStrategy {
    /// Get the maximum concurrent agents for this strategy.
    #[must_use]
    pub const fn max_concurrent_agents(&self) -> u32 {
        match self {
            Self::Quick => 2,
            Self::Standard => 5,
            Self::Deep => 10,
            Self::Academic => 20,
        }
    }

    /// Check if verification should be enabled for this strategy.
    #[must_use]
    pub const fn enable_verification(&self) -> bool {
        match self {
            Self::Quick => false,
            Self::Standard | Self::Deep | Self::Academic => true,
        }
    }

    /// Get the iterative research iterations for this strategy.
    #[must_use]
    pub const fn iterative_iterations(&self) -> u32 {
        match self {
            Self::Quick => 1,
            Self::Standard => 2,
            Self::Deep => 3,
            Self::Academic => 5,
        }
    }
}

impl std::str::FromStr for ResearchStrategy {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "quick" => Ok(Self::Quick),
            "standard" => Ok(Self::Standard),
            "deep" => Ok(Self::Deep),
            "academic" => Ok(Self::Academic),
            _ => Err(format!("Unknown research strategy: {s}")),
        }
    }
}

impl std::fmt::Display for ResearchStrategy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Quick => write!(f, "quick"),
            Self::Standard => write!(f, "standard"),
            Self::Deep => write!(f, "deep"),
            Self::Academic => write!(f, "academic"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_task_context_validation() {
        let mut ctx = TaskContext::new();
        assert!(ctx.validate().is_ok());

        ctx.model_tier = Some("invalid".to_string());
        assert!(ctx.validate().is_err());

        ctx.model_tier = Some("medium".to_string());
        assert!(ctx.validate().is_ok());

        ctx.max_concurrent_agents = Some(25);
        assert!(ctx.validate().is_err());

        ctx.max_concurrent_agents = Some(10);
        assert!(ctx.validate().is_ok());
    }

    #[test]
    fn test_model_tier_parsing() {
        use std::str::FromStr;

        assert_eq!(ModelTier::from_str("small").unwrap(), ModelTier::Small);
        assert_eq!(ModelTier::from_str("MEDIUM").unwrap(), ModelTier::Medium);
        assert!(ModelTier::from_str("huge").is_err());
    }

    #[test]
    fn test_research_strategy_presets() {
        assert_eq!(ResearchStrategy::Quick.max_concurrent_agents(), 2);
        assert_eq!(ResearchStrategy::Academic.max_concurrent_agents(), 20);
        assert!(ResearchStrategy::Quick.enable_verification() == false);
        assert!(ResearchStrategy::Deep.enable_verification());
    }
}
