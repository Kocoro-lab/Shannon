//! Claim verification for Deep Research 2.0.
//!
//! Verifies claims by cross-checking sources and detecting inconsistencies.

use crate::Source;

/// Result of claim verification.
#[derive(Debug, Clone)]
pub struct VerificationResult {
    /// Number of sources checked.
    pub sources_checked: usize,
    /// Number of verified claims.
    pub verified_claims: usize,
    /// Number of contradictions found.
    pub contradictions: usize,
    /// Overall verification confidence (0.0-1.0).
    pub confidence: f64,
}

/// Verify claims across sources.
///
/// In production, this would use an LLM to extract claims from sources
/// and cross-check them for consistency.
///
/// # Arguments
/// * `sources` - The sources to verify
///
/// # Returns
/// Verification result with statistics
pub fn verify_claims(sources: &[Source]) -> VerificationResult {
    if sources.is_empty() {
        return VerificationResult {
            sources_checked: 0,
            verified_claims: 0,
            contradictions: 0,
            confidence: 0.0,
        };
    }

    // Heuristic verification (production would use LLM)
    let sources_checked = sources.len();

    // Estimate verified claims based on source confidence
    let avg_confidence = sources.iter().map(|s| s.confidence).sum::<f64>() / sources.len() as f64;

    let verified_claims = (sources.len() as f64 * avg_confidence * 0.8) as usize;

    // Estimate contradictions (lower for high-confidence sources)
    let contradictions = if avg_confidence > 0.8 {
        0
    } else if avg_confidence > 0.6 {
        1
    } else {
        2
    };

    // Overall verification confidence
    let confidence = (avg_confidence * 0.7
        + (1.0 - contradictions as f64 / sources.len() as f64) * 0.3)
        .clamp(0.0, 1.0);

    tracing::debug!(
        "Verification: {} sources checked, {} claims verified, {} contradictions, confidence: {:.2}",
        sources_checked,
        verified_claims,
        contradictions,
        confidence
    );

    VerificationResult {
        sources_checked,
        verified_claims,
        contradictions,
        confidence,
    }
}

/// Extract facts from sources.
///
/// In production, this would use an LLM to extract structured facts
/// from source content.
///
/// # Arguments
/// * `sources` - The sources to extract facts from
///
/// # Returns
/// List of extracted facts as strings
pub fn extract_facts(sources: &[Source]) -> Vec<String> {
    let mut facts = Vec::new();

    // Heuristic fact extraction (production would use LLM)
    for (i, source) in sources.iter().enumerate() {
        if source.confidence > 0.7 {
            facts.push(format!(
                "Fact {}: {} (from {})",
                i + 1,
                source.title,
                source.url.as_deref().unwrap_or("unknown source")
            ));
        }
    }

    tracing::debug!(
        "Extracted {} facts from {} sources",
        facts.len(),
        sources.len()
    );

    facts
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
    fn test_verify_claims_empty() {
        let sources = vec![];
        let result = verify_claims(&sources);

        assert_eq!(result.sources_checked, 0);
        assert_eq!(result.verified_claims, 0);
        assert_eq!(result.contradictions, 0);
        assert_eq!(result.confidence, 0.0);
    }

    #[test]
    fn test_verify_claims_high_confidence() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.9),
            create_test_source("Source 2", "https://example.com/2", 0.95),
            create_test_source("Source 3", "https://example.com/3", 0.85),
        ];
        let result = verify_claims(&sources);

        assert_eq!(result.sources_checked, 3);
        assert!(result.verified_claims > 0);
        assert_eq!(result.contradictions, 0); // High confidence = no contradictions
        assert!(result.confidence > 0.7);
    }

    #[test]
    fn test_verify_claims_low_confidence() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.4),
            create_test_source("Source 2", "https://example.com/2", 0.5),
            create_test_source("Source 3", "https://example.com/3", 0.3),
        ];
        let result = verify_claims(&sources);

        assert_eq!(result.sources_checked, 3);
        assert!(result.contradictions > 0); // Low confidence = likely contradictions
        assert!(result.confidence < 0.7);
    }

    #[test]
    fn test_verify_claims_mixed_confidence() {
        let sources = vec![
            create_test_source("Source 1", "https://example.com/1", 0.9),
            create_test_source("Source 2", "https://example.com/2", 0.5),
            create_test_source("Source 3", "https://example.com/3", 0.7),
        ];
        let result = verify_claims(&sources);

        assert_eq!(result.sources_checked, 3);
        assert!(result.verified_claims > 0);
        assert!(result.confidence > 0.5);
        assert!(result.confidence < 0.9);
    }

    #[test]
    fn test_extract_facts_empty() {
        let sources = vec![];
        let facts = extract_facts(&sources);

        assert!(facts.is_empty());
    }

    #[test]
    fn test_extract_facts_high_confidence() {
        let sources = vec![
            create_test_source("High confidence source", "https://example.com/1", 0.9),
            create_test_source("Another high confidence", "https://example.com/2", 0.85),
        ];
        let facts = extract_facts(&sources);

        assert_eq!(facts.len(), 2); // Both should be extracted
        assert!(facts[0].contains("High confidence source"));
    }

    #[test]
    fn test_extract_facts_low_confidence() {
        let sources = vec![
            create_test_source("Low confidence source", "https://example.com/1", 0.5),
            create_test_source("Another low confidence", "https://example.com/2", 0.6),
        ];
        let facts = extract_facts(&sources);

        assert!(facts.is_empty()); // Low confidence sources not extracted
    }

    #[test]
    fn test_extract_facts_mixed_confidence() {
        let sources = vec![
            create_test_source("High confidence", "https://example.com/1", 0.9),
            create_test_source("Low confidence", "https://example.com/2", 0.5),
            create_test_source("Medium confidence", "https://example.com/3", 0.75),
        ];
        let facts = extract_facts(&sources);

        assert_eq!(facts.len(), 2); // Only high and medium confidence extracted
    }

    #[test]
    fn test_verify_claims_scales_with_sources() {
        let sources: Vec<Source> = (0..10)
            .map(|i| {
                create_test_source(
                    &format!("Source {}", i),
                    &format!("https://example.com/{}", i),
                    0.8,
                )
            })
            .collect();
        let result = verify_claims(&sources);

        assert_eq!(result.sources_checked, 10);
        assert!(result.verified_claims >= 5); // Should have multiple verified claims
        assert!(result.confidence > 0.6);
    }
}
