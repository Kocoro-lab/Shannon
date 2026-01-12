//! Tree of Thoughts (ToT) cognitive pattern.
//!
//! Implements branching exploration with evaluation and backtracking.
//! Explores multiple solution paths in parallel, evaluates each branch,
//! and selects the best path through the thought tree.
//!
//! # Example
//!
//! ```rust,ignore
//! use shannon_api::workflow::patterns::{TreeOfThoughts, PatternContext};
//!
//! let tot = TreeOfThoughts::new();
//! let ctx = PatternContext::new("wf-1".into(), "user-1".into(), None);
//! let result = tot.execute(&ctx, "Plan a 3-day trip to Paris").await?;
//! ```

use anyhow::Result;
use async_trait::async_trait;
use chrono::Utc;
use durable_shannon::activities::{
    llm::{LlmReasonActivity, LlmRequest, Message},
    Activity, ActivityContext,
};

use super::{CognitivePattern, PatternContext, PatternResult, ReasoningStep, TokenUsage};

/// Thought node in the tree.
#[derive(Debug, Clone)]
struct ThoughtNode {
    /// Node content/thought.
    content: String,
    /// Evaluation score (0.0-1.0).
    score: f32,
    /// Child nodes.
    children: Vec<ThoughtNode>,
    /// Depth in tree.
    depth: usize,
}

/// Tree of Thoughts pattern configuration.
#[derive(Debug, Clone)]
pub struct TreeOfThoughts {
    /// Maximum tree depth.
    pub max_depth: usize,

    /// Branches to explore per node.
    pub branches_per_node: usize,

    /// Keep top K branches at each level.
    pub keep_top_k: usize,

    /// Score threshold for pruning (0.0-1.0).
    pub prune_threshold: f32,

    /// Model to use.
    pub model: String,

    /// Base URL for LLM API.
    base_url: String,
}

impl TreeOfThoughts {
    /// Create a new Tree of Thoughts pattern with defaults.
    ///
    /// # Defaults
    ///
    /// - `max_depth`: 3
    /// - `branches_per_node`: 3
    /// - `keep_top_k`: 2
    /// - `prune_threshold`: 0.3
    /// - `model`: "claude-sonnet-4-20250514"
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_depth: 3,
            branches_per_node: 3,
            keep_top_k: 2,
            prune_threshold: 0.3,
            model: "claude-sonnet-4-20250514".to_string(),
            base_url: "http://127.0.0.1:8765".to_string(),
        }
    }

    /// Generate branches from a thought.
    async fn generate_branches(
        &self,
        ctx: &PatternContext,
        query: &str,
        parent_thought: &str,
        depth: usize,
    ) -> Result<Vec<String>> {
        let system = "You are a strategic thinker. Generate multiple distinct approaches or perspectives for exploring the problem.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Problem: {query}\n\nCurrent thought: {parent_thought}\n\n\
                Generate {} distinct next thoughts or approaches. Number each thought (1, 2, 3...).",
                self.branches_per_node
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.8, // Higher temp for diversity
            max_tokens: Some(1024),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: format!("tot-generate-{depth}"),
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
                anyhow::bail!("Branch generation failed: {error}");
            }
            durable_shannon::activities::ActivityResult::Retry { reason, .. } => {
                anyhow::bail!("Branch generation needs retry: {reason}");
            }
        };

        // Parse numbered thoughts
        let branches: Vec<String> = response
            .content
            .lines()
            .filter_map(|line| {
                let trimmed = line.trim();
                if trimmed.starts_with(|c: char| c.is_numeric()) {
                    trimmed.split_once('.').map(|(_, t)| t.trim().to_string())
                } else {
                    None
                }
            })
            .collect();

        Ok(branches)
    }

    /// Evaluate a thought branch.
    async fn evaluate_thought(
        &self,
        ctx: &PatternContext,
        query: &str,
        thought: &str,
    ) -> Result<f32> {
        let system = "You are an evaluator. Score how promising this thought is for solving the problem. Return only a number between 0.0 and 1.0.";

        let messages = vec![Message {
            role: "user".to_string(),
            content: format!(
                "Problem: {query}\n\nThought: {thought}\n\n\
                Score this thought (0.0 = not promising, 1.0 = very promising):"
            ),
        }];

        let request = LlmRequest {
            model: self.model.clone(),
            system: system.to_string(),
            messages,
            temperature: 0.2, // Low temp for consistent scoring
            max_tokens: Some(10),
        };

        let activity_ctx = ActivityContext {
            workflow_id: ctx.workflow_id.clone(),
            activity_id: "tot-evaluate".to_string(),
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
            _ => return Ok(0.5), // Default score on failure
        };

        // Parse score
        let score = response
            .content
            .trim()
            .parse::<f32>()
            .unwrap_or(0.5)
            .clamp(0.0, 1.0);

        Ok(score)
    }

    /// Build thought tree with recursive exploration.
    async fn build_tree(&self, ctx: &PatternContext, query: &str) -> Result<ThoughtNode> {
        let root = ThoughtNode {
            content: query.to_string(),
            score: 1.0,
            children: vec![],
            depth: 0,
        };

        self.expand_node(ctx, query, root).await
    }

    /// Recursively expand a thought node (using Box::pin for recursion).
    fn expand_node<'a>(
        &'a self,
        ctx: &'a PatternContext,
        query: &'a str,
        mut node: ThoughtNode,
    ) -> std::pin::Pin<Box<dyn std::future::Future<Output = Result<ThoughtNode>> + Send + 'a>> {
        Box::pin(async move {
            // Stop at max depth
            if node.depth >= self.max_depth {
                return Ok(node);
            }

            // Generate branches
            let branches = self
                .generate_branches(ctx, query, &node.content, node.depth)
                .await
                .unwrap_or_default();

            // Create and evaluate child nodes
            let mut children = Vec::new();
            for branch_content in branches {
                let score = self.evaluate_thought(ctx, query, &branch_content).await?;

                // Prune low-scoring branches
                if score >= self.prune_threshold {
                    let child = ThoughtNode {
                        content: branch_content,
                        score,
                        children: vec![],
                        depth: node.depth + 1,
                    };
                    children.push(child);
                }
            }

            // Sort children by score (descending)
            children.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap());

            // Keep only top K
            children.truncate(self.keep_top_k);

            // Recursively expand top children
            let mut expanded_children = Vec::new();
            for child in children {
                let expanded = self.expand_node(ctx, query, child).await?;
                expanded_children.push(expanded);
            }

            node.children = expanded_children;

            Ok(node)
        })
    }

    /// Find the best path through the tree.
    fn find_best_path(&self, root: &ThoughtNode) -> Vec<String> {
        let mut path = vec![root.content.clone()];
        let mut current = root;

        while !current.children.is_empty() {
            // Follow highest-scoring child
            current = &current.children[0];
            path.push(current.content.clone());
        }

        path
    }
}

impl Default for TreeOfThoughts {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CognitivePattern for TreeOfThoughts {
    fn name(&self) -> &str {
        "tree_of_thoughts"
    }

    fn description(&self) -> Option<&str> {
        Some("Branching exploration with evaluation and backtracking for complex problems")
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
            pattern = "tree_of_thoughts",
            max_depth = self.max_depth,
            "Starting Tree of Thoughts execution"
        );

        let mut reasoning_steps = Vec::new();

        // Build thought tree
        reasoning_steps.push(ReasoningStep {
            step: 0,
            content: format!(
                "Building thought tree (depth={}, branches={})",
                self.max_depth, self.branches_per_node
            ),
            confidence: Some(0.9),
            timestamp: Utc::now(),
        });

        let tree = self.build_tree(ctx, input).await?;

        // Find best path
        let best_path = self.find_best_path(&tree);

        reasoning_steps.push(ReasoningStep {
            step: 1,
            content: format!(
                "Explored tree, found best path with {} steps",
                best_path.len()
            ),
            confidence: Some(0.85),
            timestamp: Utc::now(),
        });

        // Record each step in best path
        for (idx, thought) in best_path.iter().enumerate() {
            reasoning_steps.push(ReasoningStep {
                step: idx + 2,
                content: thought.clone(),
                confidence: Some(0.8),
                timestamp: Utc::now(),
            });
        }

        // Final answer is the last thought in best path
        let output = best_path
            .last()
            .cloned()
            .unwrap_or_else(|| input.to_string());

        tracing::info!(
            workflow_id = %ctx.workflow_id,
            path_length = best_path.len(),
            "Tree of Thoughts completed"
        );

        Ok(PatternResult {
            output,
            reasoning_steps,
            sources: vec![],
            token_usage: None, // Would track in real implementation
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_tree_of_thoughts() {
        let tot = TreeOfThoughts::new();
        assert_eq!(tot.max_depth, 3);
        assert_eq!(tot.branches_per_node, 3);
        assert_eq!(tot.keep_top_k, 2);
        assert_eq!(tot.prune_threshold, 0.3);
    }

    #[test]
    fn test_pattern_name() {
        let tot = TreeOfThoughts::new();
        assert_eq!(tot.name(), "tree_of_thoughts");
    }

    #[test]
    fn test_pattern_description() {
        let tot = TreeOfThoughts::new();
        assert!(tot.description().is_some());
        assert!(tot.description().unwrap().contains("Branching"));
    }

    #[test]
    fn test_validate_input_empty() {
        let tot = TreeOfThoughts::new();
        let result = tot.validate_input("");
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_too_long() {
        let tot = TreeOfThoughts::new();
        let long_input = "a".repeat(5_001);
        let result = tot.validate_input(&long_input);
        assert!(result.is_err());
    }

    #[test]
    fn test_validate_input_valid() {
        let tot = TreeOfThoughts::new();
        let result = tot.validate_input("Plan a trip");
        assert!(result.is_ok());
    }

    #[test]
    fn test_find_best_path() {
        let tot = TreeOfThoughts::new();

        let root = ThoughtNode {
            content: "root".to_string(),
            score: 1.0,
            children: vec![
                ThoughtNode {
                    content: "child1".to_string(),
                    score: 0.9,
                    children: vec![ThoughtNode {
                        content: "grandchild".to_string(),
                        score: 0.8,
                        children: vec![],
                        depth: 2,
                    }],
                    depth: 1,
                },
                ThoughtNode {
                    content: "child2".to_string(),
                    score: 0.7,
                    children: vec![],
                    depth: 1,
                },
            ],
            depth: 0,
        };

        let path = tot.find_best_path(&root);

        // Should follow highest-scoring path
        assert_eq!(path.len(), 3);
        assert_eq!(path[0], "root");
        assert_eq!(path[1], "child1");
        assert_eq!(path[2], "grandchild");
    }

    #[test]
    fn test_thought_node_structure() {
        let node = ThoughtNode {
            content: "test".to_string(),
            score: 0.8,
            children: vec![],
            depth: 0,
        };

        assert_eq!(node.content, "test");
        assert_eq!(node.score, 0.8);
        assert_eq!(node.depth, 0);
        assert!(node.children.is_empty());
    }

    // Integration tests would require real LLM API
    #[tokio::test]
    #[ignore = "requires real LLM API"]
    async fn test_execute_with_branching() {
        let tot = TreeOfThoughts::new();
        let ctx = PatternContext::new("wf-test".into(), "user-1".into(), None);

        let result = tot.execute(&ctx, "Plan a 3-day trip").await;

        if let Ok(output) = result {
            assert!(!output.output.is_empty());
            assert!(!output.reasoning_steps.is_empty());
        }
    }
}
