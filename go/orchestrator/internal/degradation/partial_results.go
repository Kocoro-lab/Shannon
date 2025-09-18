package degradation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PartialResultsManager handles partial result aggregation and fallback behaviors
type PartialResultsManager struct {
	logger   *zap.Logger
	strategy DegradationStrategy
}

// NewPartialResultsManager creates a new partial results manager
func NewPartialResultsManager(strategy DegradationStrategy, logger *zap.Logger) *PartialResultsManager {
	return &PartialResultsManager{
		logger:   logger,
		strategy: strategy,
	}
}

// PartialResult represents a partial result from a failed operation
type PartialResult struct {
	Source     string                 `json:"source"`             // Component that generated result
	Success    bool                   `json:"success"`            // Whether this component succeeded
	Result     interface{}            `json:"result"`             // Actual result data
	Error      string                 `json:"error,omitempty"`    // Error message if failed
	Timestamp  time.Time              `json:"timestamp"`          // When result was generated
	Metadata   map[string]interface{} `json:"metadata,omitempty"` // Additional context
	Confidence float64                `json:"confidence"`         // Confidence score (0-1)
	Degraded   bool                   `json:"degraded"`           // Whether this was a degraded result
}

// AggregatedResult combines multiple partial results into a coherent response
type AggregatedResult struct {
	Success         bool             `json:"success"`                    // Overall success status
	Result          interface{}      `json:"result"`                     // Combined result
	PartialResults  []PartialResult  `json:"partial_results"`            // Individual component results
	TotalComponents int              `json:"total_components"`           // Total number of components attempted
	SuccessCount    int              `json:"success_count"`              // Number of successful components
	FailureCount    int              `json:"failure_count"`              // Number of failed components
	DegradationInfo *DegradationInfo `json:"degradation_info,omitempty"` // Degradation context
	Timestamp       time.Time        `json:"timestamp"`                  // When aggregation was completed
	Warning         string           `json:"warning,omitempty"`          // Warning message for partial results
}

// DegradationInfo provides context about why results were degraded
type DegradationInfo struct {
	Level              DegradationLevel `json:"level"`
	FailedDependencies []string         `json:"failed_dependencies"`
	Reason             string           `json:"reason"`
	RecommendedAction  string           `json:"recommended_action"`
}

// AggregateResults combines partial results into a meaningful response
func (prm *PartialResultsManager) AggregateResults(
	ctx context.Context,
	results []PartialResult,
	workflowType string,
) (*AggregatedResult, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results to aggregate")
	}

	successCount := 0
	var successfulResults []interface{}
	var errors []string
	var warnings []string

	for _, result := range results {
		if result.Success {
			successCount++
			if result.Result != nil {
				successfulResults = append(successfulResults, result.Result)
			}
		} else {
			if result.Error != "" {
				errors = append(errors, fmt.Sprintf("%s: %s", result.Source, result.Error))
			}
		}

		if result.Degraded {
			warnings = append(warnings, fmt.Sprintf("degraded result from %s", result.Source))
		}
	}

	// Determine overall success based on partial success threshold
	overallSuccess := prm.shouldConsiderSuccessful(successCount, len(results), workflowType)

	// Create aggregated result
	aggregated := &AggregatedResult{
		Success:         overallSuccess,
		PartialResults:  results,
		TotalComponents: len(results),
		SuccessCount:    successCount,
		FailureCount:    len(results) - successCount,
		Timestamp:       time.Now(),
	}

	// Combine successful results
	if len(successfulResults) > 0 {
		aggregated.Result = prm.combineResults(successfulResults, workflowType)
	}

	// Add degradation info if applicable
	shouldDegrade, degradationLevel, err := prm.strategy.ShouldDegrade(ctx)
	if err == nil && shouldDegrade {
		aggregated.DegradationInfo = &DegradationInfo{
			Level:             degradationLevel,
			Reason:            fmt.Sprintf("partial results due to %s degradation", degradationLevel.String()),
			RecommendedAction: prm.getRecommendedAction(degradationLevel),
		}

		// Collect failed dependencies
		var failedDeps []string
		for _, result := range results {
			if !result.Success {
				failedDeps = append(failedDeps, result.Source)
			}
		}
		aggregated.DegradationInfo.FailedDependencies = failedDeps
	}

	// Create warning message
	if len(warnings) > 0 || len(errors) > 0 {
		var warningParts []string
		if len(warnings) > 0 {
			warningParts = append(warningParts, strings.Join(warnings, "; "))
		}
		if len(errors) > 0 && !overallSuccess {
			warningParts = append(warningParts, fmt.Sprintf("errors: %s", strings.Join(errors, "; ")))
		}
		aggregated.Warning = strings.Join(warningParts, ". ")
	}

	// Record metrics
	RecordPartialResults(workflowType, fmt.Sprintf("success_ratio_%d_%d", successCount, len(results)))

	prm.logger.Info("Aggregated partial results",
		zap.String("workflow_type", workflowType),
		zap.Int("total_components", len(results)),
		zap.Int("success_count", successCount),
		zap.Int("failure_count", len(results)-successCount),
		zap.Bool("overall_success", overallSuccess),
		zap.String("warning", aggregated.Warning),
	)

	return aggregated, nil
}

// shouldConsiderSuccessful determines if partial results should be considered successful
func (prm *PartialResultsManager) shouldConsiderSuccessful(successCount, totalCount int, workflowType string) bool {
	if totalCount == 0 {
		return false
	}

	successRatio := float64(successCount) / float64(totalCount)

	// Define success thresholds per workflow type
	switch workflowType {
	case "simple":
		// Simple workflows need at least 1 success
		return successCount >= 1
	case "standard":
		// Standard workflows need at least 50% success
		return successRatio >= 0.5
	case "complex":
		// Complex workflows need at least 60% success
		return successRatio >= 0.6
	case "agent_dag":
		// DAG workflows need at least 40% success (more lenient due to parallel execution)
		return successRatio >= 0.4
	default:
		// Default: need at least 50% success
		return successRatio >= 0.5
	}
}

// combineResults combines successful results into a single result
func (prm *PartialResultsManager) combineResults(results []interface{}, workflowType string) interface{} {
	if len(results) == 0 {
		return nil
	}

	if len(results) == 1 {
		return results[0]
	}

	// For multiple results, create a combined response
	switch workflowType {
	case "agent_dag":
		// For agent DAG, results are typically structured
		return map[string]interface{}{
			"type":         "combined_agent_results",
			"results":      results,
			"result_count": len(results),
			"combined_at":  time.Now(),
		}
	default:
		// For other workflows, concatenate string results or return structured data
		return map[string]interface{}{
			"type":         "combined_results",
			"results":      results,
			"result_count": len(results),
			"combined_at":  time.Now(),
		}
	}
}

// getRecommendedAction returns recommended action based on degradation level
func (prm *PartialResultsManager) getRecommendedAction(level DegradationLevel) string {
	switch level {
	case LevelMinor:
		return "Monitor system health and retry if needed"
	case LevelModerate:
		return "Check failed dependencies and consider using cached results"
	case LevelSevere:
		return "Investigate system issues immediately and use fallback procedures"
	default:
		return "Monitor system status"
	}
}

// CreatePartialResult creates a partial result from a component execution
func (prm *PartialResultsManager) CreatePartialResult(
	source string,
	success bool,
	result interface{},
	err error,
	confidence float64,
	degraded bool,
) PartialResult {
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	}

	return PartialResult{
		Source:     source,
		Success:    success,
		Result:     result,
		Error:      errorMsg,
		Timestamp:  time.Now(),
		Confidence: confidence,
		Degraded:   degraded,
		Metadata:   make(map[string]interface{}),
	}
}

// ShouldReturnPartialResults determines if partial results should be returned instead of failing
func (prm *PartialResultsManager) ShouldReturnPartialResults(
	ctx context.Context,
	workflowType string,
	successCount, totalCount int,
) (bool, error) {
	if totalCount == 0 {
		return false, nil
	}

	// Check if system is degraded
	shouldDegrade, degradationLevel, err := prm.strategy.ShouldDegrade(ctx)
	if err != nil {
		return false, err
	}

	if !shouldDegrade {
		// No degradation, use normal failure logic
		return false, nil
	}

	// In degraded state, be more lenient about partial results
	successRatio := float64(successCount) / float64(totalCount)

	switch degradationLevel {
	case LevelMinor:
		// Minor degradation: return partial if we have any success
		return successCount > 0, nil
	case LevelModerate:
		// Moderate degradation: return partial if we have >= 25% success
		return successRatio >= 0.25, nil
	case LevelSevere:
		// Severe degradation: return partial if we have any success at all
		return successCount > 0, nil
	default:
		return false, nil
	}
}
