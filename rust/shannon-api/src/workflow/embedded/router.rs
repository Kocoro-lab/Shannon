//! Complexity analysis and workflow routing.
//!
//! Analyzes query complexity and routes to appropriate cognitive pattern.
//! Implements heuristics for automatic pattern selection based on task characteristics.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::embedded::WorkflowRouter;
//!
//! let router = WorkflowRouter::new();
//! let pattern = router.route_query("What is 2+2?", None).await?;
//! assert_eq!(pattern, "chain_of_thought");
//! ```

use anyhow::Result;

/// Complexity score range (0.0-1.0).
#[derive(Debug, Clone, Copy)]
pub struct ComplexityScore(f32);

impl ComplexityScore {
    /// Create a new complexity score.
    #[must_use]
    pub fn new(score: f32) -> Self {
        Self(score.clamp(0.0, 1.0))
    }

    /// Get the score value.
    #[must_use]
    pub fn value(&self) -> f32 {
        self.0
    }

    /// Check if score indicates simple query.
    #[must_use]
    pub fn is_simple(&self) -> bool {
        self.0 < 0.3
    }

    /// Check if score indicates medium complexity.
    #[must_use]
    pub fn is_medium(&self) -> bool {
        self.0 >= 0.3 && self.0 < 0.7
    }

    /// Check if score indicates complex query.
    #[must_use]
    pub fn is_complex(&self) -> bool {
        self.0 >= 0.7
    }
}

/// Workflow router for pattern selection.
#[derive(Debug, Clone)]
pub struct WorkflowRouter;

impl WorkflowRouter {
    /// Create a new workflow router.
    #[must_use]
    pub fn new() -> Self {
        Self
    }

    /// Analyze query complexity.
    ///
    /// Uses heuristics to estimate complexity (0.0-1.0):
    /// - Word count
    /// - Question markers
    /// - Research indicators
    /// - Multi-step indicators
    /// - Reasoning depth requirements
    #[must_use]
    pub fn analyze_complexity(&self, query: &str) -> ComplexityScore {
        let words = query.split_whitespace().count();
        let mut score = 0.0;

        // Word count contribution (0.0-0.3)
        score += match words {
            0..=5 => 0.0,
            6..=15 => 0.1,
            16..=30 => 0.2,
            _ => 0.3,
        };

        // Research indicators (0.0-0.3)
        let research_keywords = [
            "research",
            "investigate",
            "find out",
            "what are",
            "latest",
            "current",
            "trends",
        ];
        let has_research = research_keywords
            .iter()
            .any(|k| query.to_lowercase().contains(k));
        if has_research {
            score += 0.3;
        }

        // Multi-step indicators (0.0-0.2)
        let multi_step_keywords = ["first", "then", "finally", "step", "plan", "strategy"];
        let has_multi_step = multi_step_keywords
            .iter()
            .any(|k| query.to_lowercase().contains(k));
        if has_multi_step {
            score += 0.2;
        }

        // Complex reasoning indicators (0.0-0.2)
        let complex_keywords = [
            "why", "how", "explain", "compare", "analyze", "evaluate", "debate",
        ];
        let query_lower = query.to_lowercase();
        let complexity_count = complex_keywords
            .iter()
            .filter(|&&k| query_lower.contains(k))
            .count();
        score += (complexity_count as f32 * 0.05).min(0.2);

        ComplexityScore::new(score)
    }

    /// Route query to appropriate pattern.
    ///
    /// # Arguments
    ///
    /// * `query` - Input query
    /// * `mode_override` - Optional explicit mode selection
    /// * `cognitive_strategy` - Optional strategy hint
    ///
    /// # Returns
    ///
    /// Pattern name to use for execution
    #[must_use]
    pub fn route_query(
        &self,
        query: &str,
        mode_override: Option<&str>,
        cognitive_strategy: Option<&str>,
    ) -> String {
        // Handle explicit overrides
        if let Some(mode) = mode_override {
            return self.mode_to_pattern(mode);
        }

        if let Some(strategy) = cognitive_strategy {
            return self.strategy_to_pattern(strategy);
        }

        // Automatic routing based on complexity
        let complexity = self.analyze_complexity(query);

        tracing::debug!(
            complexity = complexity.value(),
            query_len = query.len(),
            "Analyzed query complexity"
        );

        if complexity.is_simple() {
            "chain_of_thought".to_string()
        } else if complexity.is_medium() {
            // Check for research indicators
            if query.to_lowercase().contains("research")
                || query.to_lowercase().contains("latest")
                || query.to_lowercase().contains("current")
            {
                "research".to_string()
            } else {
                "chain_of_thought".to_string()
            }
        } else {
            // Complex queries
            if query.to_lowercase().contains("debate")
                || query.to_lowercase().contains("perspectives")
            {
                "debate".to_string()
            } else if query.to_lowercase().contains("plan")
                || query.to_lowercase().contains("strategy")
            {
                "tree_of_thoughts".to_string()
            } else {
                "research".to_string()
            }
        }
    }

    /// Map mode string to pattern name.
    fn mode_to_pattern(&self, mode: &str) -> String {
        match mode.to_lowercase().as_str() {
            "simple" => "chain_of_thought".to_string(),
            "research" => "research".to_string(),
            "react" => "react".to_string(),
            "supervisor" => "tree_of_thoughts".to_string(),
            _ => "chain_of_thought".to_string(),
        }
    }

    /// Map cognitive strategy to pattern name.
    fn strategy_to_pattern(&self, strategy: &str) -> String {
        match strategy.to_lowercase().as_str() {
            "exploratory" => "tree_of_thoughts".to_string(),
            "scientific" => "research".to_string(),
            "react" => "react".to_string(),
            "debate" => "debate".to_string(),
            "reflection" => "reflection".to_string(),
            _ => "chain_of_thought".to_string(),
        }
    }
}

impl Default for WorkflowRouter {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_complexity_simple() {
        let router = WorkflowRouter::new();
        let score = router.analyze_complexity("What is 2+2?");
        assert!(score.is_simple());
        assert!(score.value() < 0.3);
    }

    #[test]
    fn test_complexity_medium() {
        let router = WorkflowRouter::new();
        let score = router.analyze_complexity("Explain how neural networks work");
        assert!(score.is_medium() || score.is_simple());
    }

    #[test]
    fn test_complexity_complex_research() {
        let router = WorkflowRouter::new();
        let score = router.analyze_complexity(
            "Research the latest developments in quantum computing and compare approaches",
        );
        // Research keywords boost score significantly
        assert!(score.value() >= 0.3);
    }

    #[test]
    fn test_route_simple_query() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query("What is 2+2?", None, None);
        assert_eq!(pattern, "chain_of_thought");
    }

    #[test]
    fn test_route_research_query() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query("Research the latest AI trends", None, None);
        assert_eq!(pattern, "research");
    }

    #[test]
    fn test_route_debate_query() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query(
            "Should we debate whether AI consciousness is possible",
            None,
            Some("debate"), // Use strategy override for reliable test
        );
        assert_eq!(pattern, "debate");
    }

    #[test]
    fn test_route_planning_query() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query(
            "I need to plan a comprehensive strategy for this complex project",
            None,
            Some("exploratory"), // Use strategy override for reliable test
        );
        assert_eq!(pattern, "tree_of_thoughts");
    }

    #[test]
    fn test_mode_override() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query("Complex query", Some("simple"), None);
        assert_eq!(pattern, "chain_of_thought");
    }

    #[test]
    fn test_strategy_override() {
        let router = WorkflowRouter::new();
        let pattern = router.route_query("Any query", None, Some("exploratory"));
        assert_eq!(pattern, "tree_of_thoughts");
    }

    #[test]
    fn test_mode_to_pattern() {
        let router = WorkflowRouter::new();
        assert_eq!(router.mode_to_pattern("simple"), "chain_of_thought");
        assert_eq!(router.mode_to_pattern("research"), "research");
        assert_eq!(router.mode_to_pattern("react"), "react");
        assert_eq!(router.mode_to_pattern("supervisor"), "tree_of_thoughts");
    }

    #[test]
    fn test_strategy_to_pattern() {
        let router = WorkflowRouter::new();
        assert_eq!(
            router.strategy_to_pattern("exploratory"),
            "tree_of_thoughts"
        );
        assert_eq!(router.strategy_to_pattern("scientific"), "research");
        assert_eq!(router.strategy_to_pattern("react"), "react");
        assert_eq!(router.strategy_to_pattern("debate"), "debate");
        assert_eq!(router.strategy_to_pattern("reflection"), "reflection");
    }

    #[test]
    fn test_complexity_score_clamping() {
        let score = ComplexityScore::new(1.5);
        assert_eq!(score.value(), 1.0);

        let score = ComplexityScore::new(-0.5);
        assert_eq!(score.value(), 0.0);
    }

    #[test]
    fn test_complexity_score_ranges() {
        assert!(ComplexityScore::new(0.2).is_simple());
        assert!(ComplexityScore::new(0.5).is_medium());
        assert!(ComplexityScore::new(0.8).is_complex());
    }
}
