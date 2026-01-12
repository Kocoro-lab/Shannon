//! Coverage evaluation for Deep Research 2.0.
//!
//! Evaluates how comprehensively a set of sources covers the research query.

use crate::Source;

/// Evaluate coverage of sources for a given query.
///
/// In production, this would use an LLM to evaluate coverage.
/// For now, uses heuristics based on source count, confidence, and diversity.
///
/// # Arguments
/// * `query` - The research query
/// * `sources` - The sources collected so far
/// * `_model` - The LLM model to use for evaluation (unused in mock)
///
/// # Returns
/// Coverage score from 0.0 to 1.0
pub fn evaluate_coverage(query: &str, sources: &[Source], _model: &str) -> f64 {
    if sources.is_empty() {
        return 0.0;
    }

    // Heuristic evaluation (production would use LLM)

    // Factor 1: Number of sources (0-40% of score)
    let source_count_score = (sources.len() as f64 / 15.0).min(1.0) * 0.4;

    // Factor 2: Average confidence (0-30% of score)
    let avg_confidence = sources.iter().map(|s| s.confidence).sum::<f64>() / sources.len() as f64;
    let confidence_score = avg_confidence * 0.3;

    // Factor 3: URL diversity (0-20% of score)
    let unique_domains = sources
        .iter()
        .filter_map(|s| s.url.as_ref())
        .filter_map(|url| url.split("//").nth(1))
        .filter_map(|url| url.split('/').next())
        .collect::<std::collections::HashSet<_>>()
        .len();
    let diversity_score = (unique_domains as f64 / 5.0).min(1.0) * 0.2;

    // Factor 4: Content relevance (0-10% of score)
    // Check if snippets contain query terms
    let query_terms: Vec<&str> = query.split_whitespace().collect();
    let relevant_sources = sources
        .iter()
        .filter(|s| {
            s.snippet.as_ref().map_or(false, |snippet| {
                query_terms
                    .iter()
                    .any(|term| snippet.to_lowercase().contains(&term.to_lowercase()))
            })
        })
        .count();
    let relevance_score = (relevant_sources as f64 / sources.len() as f64) * 0.1;

    let total_score = source_count_score + confidence_score + diversity_score + relevance_score;

    tracing::debug!(
        "Coverage evaluation: sources={}, avg_conf={:.2}, domains={}, relevant={}, score={:.2}",
        sources.len(),
        avg_confidence,
        unique_domains,
        relevant_sources,
        total_score
    );

    total_score.clamp(0.0, 1.0)
}

/// Identify specific coverage gaps.
///
/// In production, this would use an LLM to analyze the query and sources
/// to identify specific aspects that are not well-covered.
///
/// # Arguments
/// * `query` - The research query
/// * `sources` - The sources collected so far
/// * `_model` - The LLM model to use (unused in mock)
///
/// # Returns
/// List of identified gaps as natural language descriptions
pub fn identify_gaps(query: &str, sources: &[Source], _model: &str) -> Vec<String> {
    let coverage_score = evaluate_coverage(query, sources, _model);

    let mut gaps = Vec::new();

    // Heuristic gap identification (production would use LLM)
    if coverage_score < 0.3 {
        gaps.push("Fundamental understanding of the topic".to_string());
        gaps.push("Core concepts and definitions".to_string());
        gaps.push("Historical context and background".to_string());
    } else if coverage_score < 0.5 {
        gaps.push("Practical applications and use cases".to_string());
        gaps.push("Current trends and developments".to_string());
    } else if coverage_score < 0.7 {
        gaps.push("Expert opinions and analysis".to_string());
        gaps.push("Comparative perspectives".to_string());
    } else if coverage_score < 0.9 {
        gaps.push("Recent updates and news".to_string());
    }

    // Check domain diversity
    let unique_domains = sources
        .iter()
        .filter_map(|s| s.url.as_ref())
        .filter_map(|url| url.split("//").nth(1))
        .filter_map(|url| url.split('/').next())
        .collect::<std::collections::HashSet<_>>()
        .len();

    if unique_domains < 3 {
        gaps.push("Diverse source perspectives".to_string());
    }

    tracing::debug!(
        "Identified {} gaps for query '{}' with coverage {:.2}",
        gaps.len(),
        query,
        coverage_score
    );

    gaps
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_source(title: &str, url: &str, confidence: f64) -> Source {
        Source {
            title: title.to_string(),
            url: Some(url.to_string()),
            snippet: Some(format!("Content about {}", title)),
            confidence,
        }
    }

    #[test]
    fn test_evaluate_coverage_empty() {
        let sources = vec![];
        let score = evaluate_coverage("test query", &sources, "test-model");
        assert_eq!(score, 0.0);
    }

    #[test]
    fn test_evaluate_coverage_few_sources() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.7),
            create_test_source("Source 2", "https://example.com/2", 0.8),
        ];
        let score = evaluate_coverage("test query", &sources, "test-model");
        assert!(score > 0.0);
        assert!(score < 0.5); // Should be low with only 2 sources
    }

    #[test]
    fn test_evaluate_coverage_many_sources() {
        let sources: Vec<Source> = (0..15)
            .map(|i| {
                create_test_source(
                    &format!("Source {}", i),
                    &format!("https://domain{}.com/page", i % 5),
                    0.8,
                )
            })
            .collect();
        let score = evaluate_coverage("test query", &sources, "test-model");
        assert!(score > 0.5); // Should be higher with many sources
    }

    #[test]
    fn test_evaluate_coverage_low_confidence() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.3),
            create_test_source("Source 2", "https://example.com/2", 0.4),
            create_test_source("Source 3", "https://example.com/3", 0.3),
        ];
        let score = evaluate_coverage("test query", &sources, "test-model");
        assert!(score < 0.5); // Low confidence should reduce score
    }

    #[test]
    fn test_evaluate_coverage_high_confidence() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.9),
            create_test_source("Source 2", "https://example.com/2", 0.95),
            create_test_source("Source 3", "https://example.com/3", 0.85),
        ];
        let score = evaluate_coverage("test query", &sources, "test-model");
        // High confidence should contribute to score
        assert!(score > 0.2);
    }

    #[test]
    fn test_identify_gaps_low_coverage() {
        let sources = vec![create_test_source("Source 1", "https://example.com/1", 0.5)];
        let gaps = identify_gaps("test query", &sources, "test-model");
        assert!(!gaps.is_empty());
        assert!(gaps.len() >= 3); // Should identify multiple gaps for low coverage
    }

    #[test]
    fn test_identify_gaps_medium_coverage() {
        let sources: Vec<Source> = (0..8)
            .map(|i| {
                create_test_source(
                    &format!("Source {}", i),
                    &format!("https://domain{}.com/page", i % 3),
                    0.7,
                )
            })
            .collect();
        let gaps = identify_gaps("test query", &sources, "test-model");
        assert!(!gaps.is_empty());
        // Medium coverage should still identify some gaps
        assert!(gaps.len() <= 3);
    }

    #[test]
    fn test_identify_gaps_high_coverage() {
        let sources: Vec<Source> = (0..15)
            .map(|i| {
                create_test_source(
                    &format!("Source {}", i),
                    &format!("https://domain{}.com/page", i),
                    0.9,
                )
            })
            .collect();
        let gaps = identify_gaps("test query", &sources, "test-model");
        // High coverage should identify fewer gaps
        assert!(gaps.len() <= 2);
    }

    #[test]
    fn test_domain_diversity_gap() {
        // All sources from same domain
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.8),
            create_test_source("Source 2", "https://example.com/2", 0.8),
            create_test_source("Source 3", "https://example.com/3", 0.8),
        ];
        let gaps = identify_gaps("test query", &sources, "test-model");
        assert!(
            gaps.iter()
                .any(|g| g.contains("Diverse") || g.contains("perspectives"))
        );
    }
}
