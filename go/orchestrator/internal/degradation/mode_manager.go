package degradation

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// ExecutionMode represents the execution mode for workflows
type ExecutionMode string

const (
	ModeSimple   ExecutionMode = "simple"
	ModeStandard ExecutionMode = "standard"
	ModeComplex  ExecutionMode = "complex"
)

// ModeDowngradeReason explains why mode was downgraded
type ModeDowngradeReason string

const (
	ReasonCircuitBreakerOpen ModeDowngradeReason = "circuit_breaker_open"
	ReasonHighDegradation    ModeDowngradeReason = "high_degradation_level"
	ReasonDependencyFailure  ModeDowngradeReason = "dependency_failure"
	ReasonResourceConstraint ModeDowngradeReason = "resource_constraint"
	ReasonTimeoutRisk        ModeDowngradeReason = "timeout_risk"
)

// ModeManager handles mode selection and degradation decisions
type ModeManager struct {
	strategy DegradationStrategy
	logger   *zap.Logger
}

// NewModeManager creates a new mode manager with degradation strategy
func NewModeManager(strategy DegradationStrategy, logger *zap.Logger) *ModeManager {
	return &ModeManager{
		strategy: strategy,
		logger:   logger,
	}
}

// DetermineFinalMode determines the final execution mode considering degradation
// This must be deterministic for Temporal workflow replay
func (mm *ModeManager) DetermineFinalMode(
	ctx context.Context,
	originalMode ExecutionMode,
	query string,
	sessionID string,
) (ExecutionMode, *ModeDowngradeReason, error) {
	// Check if degradation is necessary
	shouldDegrade, degradationLevel, err := mm.strategy.ShouldDegrade(ctx)
	if err != nil {
		return originalMode, nil, fmt.Errorf("failed to check degradation: %w", err)
	}

	if !shouldDegrade {
		// No degradation needed, return original mode
		return originalMode, nil, nil
	}

	// Determine if mode downgrade is needed based on degradation level and original mode
	finalMode, reason := mm.calculateDowngradedMode(originalMode, degradationLevel)

	if finalMode != originalMode {
		mm.logger.Info("Mode downgraded due to system degradation",
			zap.String("original_mode", string(originalMode)),
			zap.String("final_mode", string(finalMode)),
			zap.String("degradation_level", degradationLevel.String()),
			zap.String("reason", string(reason)),
			zap.String("session_id", sessionID),
		)

		// Record metrics
		RecordModeDowngrade(string(originalMode), string(finalMode), string(reason))
		mm.strategy.RecordDegradation(degradationLevel, fmt.Sprintf("mode_downgrade_%s_to_%s", originalMode, finalMode))
	}

	return finalMode, &reason, nil
}

// calculateDowngradedMode determines the appropriate downgraded mode
func (mm *ModeManager) calculateDowngradedMode(
	originalMode ExecutionMode,
	degradationLevel DegradationLevel,
) (ExecutionMode, ModeDowngradeReason) {
	switch degradationLevel {
	case LevelNone:
		return originalMode, ""

	case LevelMinor:
		// Minor degradation: Only downgrade Complex to Standard
		if originalMode == ModeComplex {
			return ModeStandard, ReasonHighDegradation
		}
		return originalMode, ""

	case LevelModerate:
		// Moderate degradation: Downgrade Complex->Standard, Standard->Simple
		switch originalMode {
		case ModeComplex:
			return ModeStandard, ReasonHighDegradation
		case ModeStandard:
			return ModeSimple, ReasonHighDegradation
		default:
			return originalMode, ""
		}

	case LevelSevere:
		// Severe degradation: Everything goes to Simple
		if originalMode != ModeSimple {
			return ModeSimple, ReasonHighDegradation
		}
		return originalMode, ""

	default:
		return originalMode, ""
	}
}

// ShouldUsePartialResults determines if partial results should be returned
func (mm *ModeManager) ShouldUsePartialResults(ctx context.Context, workflowType string) (bool, error) {
	shouldDegrade, degradationLevel, err := mm.strategy.ShouldDegrade(ctx)
	if err != nil {
		return false, err
	}

	if !shouldDegrade {
		return false, nil
	}

	// Use partial results for moderate or severe degradation
	usePartial := degradationLevel >= LevelModerate

	if usePartial {
		mm.logger.Info("Partial results recommended",
			zap.String("workflow_type", workflowType),
			zap.String("degradation_level", degradationLevel.String()),
		)
	}

	return usePartial, nil
}

// GetFallbackBehaviorForOperation returns the appropriate fallback behavior
func (mm *ModeManager) GetFallbackBehaviorForOperation(operation string) FallbackBehavior {
	behavior := mm.strategy.GetFallbackBehavior(operation)
	
	// Record that fallback behavior was requested
	RecordFallbackBehavior(operation, behavior)
	
	return behavior
}

// CanExecuteOperation checks if an operation should proceed in current degradation state
func (mm *ModeManager) CanExecuteOperation(ctx context.Context, operation string) (bool, FallbackBehavior, error) {
	behavior := mm.GetFallbackBehaviorForOperation(operation)
	
	switch behavior {
	case BehaviorProceed:
		return true, behavior, nil
	case BehaviorDegrade:
		return true, behavior, nil // Proceed but with degraded mode
	case BehaviorCache:
		return true, behavior, nil // Proceed but use cached results
	case BehaviorSkip:
		return false, behavior, nil // Skip this operation
	case BehaviorFail:
		return false, behavior, fmt.Errorf("operation %s failed due to degradation", operation)
	default:
		return true, behavior, nil // Default to proceed
	}
}

// ModeDowngradeDecision encapsulates a mode downgrade decision for workflow context
type ModeDowngradeDecision struct {
	OriginalMode    ExecutionMode
	FinalMode       ExecutionMode
	WasDowngraded   bool
	Reason          ModeDowngradeReason
	DegradationLevel DegradationLevel
	Timestamp       int64 // Unix timestamp for determinism
}

// CreateModeDecision creates a deterministic mode decision for workflow use
func (mm *ModeManager) CreateModeDecision(
	ctx context.Context,
	originalMode ExecutionMode,
	query string,
	sessionID string,
	timestamp int64, // Passed from workflow for determinism
) (*ModeDowngradeDecision, error) {
	finalMode, reasonPtr, err := mm.DetermineFinalMode(ctx, originalMode, query, sessionID)
	if err != nil {
		return nil, err
	}

	var reason ModeDowngradeReason
	if reasonPtr != nil {
		reason = *reasonPtr
	}

	wasDowngraded := finalMode != originalMode

	// Get current degradation level for context
	_, degradationLevel, err := mm.strategy.ShouldDegrade(ctx)
	if err != nil {
		degradationLevel = LevelNone // Default to no degradation on error
	}

	return &ModeDowngradeDecision{
		OriginalMode:     originalMode,
		FinalMode:        finalMode,
		WasDowngraded:    wasDowngraded,
		Reason:           reason,
		DegradationLevel: degradationLevel,
		Timestamp:        timestamp,
	}, nil
}