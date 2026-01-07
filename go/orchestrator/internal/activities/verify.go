package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

// VerifyClaimsActivity verifies claims in synthesis against citations
func (a *Activities) VerifyClaimsActivity(ctx context.Context, input VerifyClaimsInput) (VerificationResult, error) {
	logger := a.logger.With(
		zap.String("activity", "VerifyClaims"),
		zap.Int("total_citations", len(input.Citations)),
	)
	logger.Info("Starting claim verification")

	// Prepare request payload (V2 format with three-category classification)
	payload := map[string]interface{}{
		"answer":    input.Answer,
		"citations": input.Citations,
		"use_v2":    true, // Request V2 format with BM25 retrieval and three-category output
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Call Python LLM service
	llmServiceURL := os.Getenv("LLM_SERVICE_URL")
	if llmServiceURL == "" {
		llmServiceURL = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/api/verify_claims", llmServiceURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return VerificationResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second} // 2 minutes timeout
	resp, err := client.Do(req)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("verification request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return VerificationResult{}, fmt.Errorf("verification failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result VerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return VerificationResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	logger.Info("Verification completed (V2)",
		zap.Float64("overall_confidence", result.OverallConfidence),
		zap.Int("total_claims", result.TotalClaims),
		zap.Int("supported", result.SupportedClaims),
		zap.Int("unsupported", result.UnsupportedClaims),
		zap.Int("insufficient_evidence", result.InsufficientEvidenceClaims),
		zap.Float64("evidence_coverage", result.EvidenceCoverage),
		zap.Float64("avg_retrieval_score", result.AvgRetrievalScore),
		zap.Int("conflicts", len(result.Conflicts)),
	)

	return result, nil
}
