package degradation

import (
	"context"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
	"go.uber.org/zap"
)

// DegradationStrategy defines how the system should degrade when dependencies fail
type DegradationStrategy interface {
	// ShouldDegrade returns true if the system should enter degraded mode
	ShouldDegrade(ctx context.Context) (bool, DegradationLevel, error)
	
	// GetFallbackBehavior returns the fallback behavior for a specific operation
	GetFallbackBehavior(operation string) FallbackBehavior
	
	// RecordDegradation records a degradation event for metrics
	RecordDegradation(level DegradationLevel, reason string)
}

// DegradationLevel represents the severity of degradation
type DegradationLevel int

const (
	LevelNone DegradationLevel = iota
	LevelMinor                  // Single dependency issue
	LevelModerate              // Multiple dependency issues
	LevelSevere                // Critical dependency failure
)

func (d DegradationLevel) String() string {
	switch d {
	case LevelNone:
		return "none"
	case LevelMinor:
		return "minor"
	case LevelModerate:
		return "moderate"
	case LevelSevere:
		return "severe"
	default:
		return "unknown"
	}
}

// FallbackBehavior defines how to handle operations when degraded
type FallbackBehavior int

const (
	BehaviorProceed FallbackBehavior = iota // Continue with warnings
	BehaviorDegrade                         // Downgrade mode
	BehaviorCache                           // Use cached results
	BehaviorSkip                            // Skip non-essential operations
	BehaviorFail                            // Fail fast
)

func (f FallbackBehavior) String() string {
	switch f {
	case BehaviorProceed:
		return "proceed"
	case BehaviorDegrade:
		return "degrade"
	case BehaviorCache:
		return "cache"
	case BehaviorSkip:
		return "skip"
	case BehaviorFail:
		return "fail"
	default:
		return "unknown"
	}
}

// DependencyHealth represents the health status of a dependency
type DependencyHealth struct {
	Name           string
	IsHealthy      bool
	CircuitBreaker circuitbreaker.State
	LastError      error
	LastCheckTime  time.Time
}

// SystemHealth aggregates dependency health information
type SystemHealth struct {
	Redis      DependencyHealth
	Database   DependencyHealth
	AgentCore  DependencyHealth
	LLMService DependencyHealth
	Overall    DegradationLevel
	Timestamp  time.Time
}

// DefaultStrategy implements a conservative degradation strategy
type DefaultStrategy struct {
	logger               *zap.Logger
	redisWrapper         interface{ IsCircuitBreakerOpen() bool }
	databaseWrapper      interface{ IsCircuitBreakerOpen() bool }
	agentCoreHealthy     bool
	llmServiceHealthy    bool
	degradationThreshold int // Number of failed dependencies to trigger degradation
}

// NewDefaultStrategy creates a new default degradation strategy
func NewDefaultStrategy(
	logger *zap.Logger,
	redisWrapper interface{ IsCircuitBreakerOpen() bool },
	databaseWrapper interface{ IsCircuitBreakerOpen() bool },
) *DefaultStrategy {
	return &DefaultStrategy{
		logger:               logger,
		redisWrapper:         redisWrapper,
		databaseWrapper:      databaseWrapper,
		agentCoreHealthy:     true,
		llmServiceHealthy:    true,
		degradationThreshold: 2, // Degrade when 2+ dependencies fail
	}
}

// ShouldDegrade determines if system should degrade based on circuit breaker states
func (ds *DefaultStrategy) ShouldDegrade(ctx context.Context) (bool, DegradationLevel, error) {
	health := ds.checkSystemHealth()
	
	failedCount := 0
	if !health.Redis.IsHealthy {
		failedCount++
	}
	if !health.Database.IsHealthy {
		failedCount++
	}
	if !health.AgentCore.IsHealthy {
		failedCount++
	}
	if !health.LLMService.IsHealthy {
		failedCount++
	}

	var level DegradationLevel
	shouldDegrade := false

	switch failedCount {
	case 0:
		level = LevelNone
	case 1:
		level = LevelMinor
		shouldDegrade = true
	case 2:
		level = LevelModerate
		shouldDegrade = true
	default:
		level = LevelSevere
		shouldDegrade = true
	}

	if shouldDegrade {
		ds.logger.Warn("System degradation triggered",
			zap.String("level", level.String()),
			zap.Int("failed_dependencies", failedCount),
			zap.Bool("redis_healthy", health.Redis.IsHealthy),
			zap.Bool("database_healthy", health.Database.IsHealthy),
			zap.Bool("agent_core_healthy", health.AgentCore.IsHealthy),
			zap.Bool("llm_service_healthy", health.LLMService.IsHealthy),
		)
	}

	return shouldDegrade, level, nil
}

// GetFallbackBehavior returns appropriate fallback behavior for operations
func (ds *DefaultStrategy) GetFallbackBehavior(operation string) FallbackBehavior {
	// Check current system health to determine fallback behavior
	health := ds.checkSystemHealth()
	
	switch operation {
	case "session_update":
		if !health.Redis.IsHealthy {
			return BehaviorProceed // Continue without session updates
		}
		return BehaviorProceed

	case "task_persistence":
		if !health.Database.IsHealthy {
			return BehaviorProceed // Continue but log warnings
		}
		return BehaviorProceed

	case "agent_execution":
		if !health.AgentCore.IsHealthy {
			return BehaviorDegrade // Downgrade to simpler mode
		}
		return BehaviorProceed

	case "llm_call":
		if !health.LLMService.IsHealthy {
			return BehaviorCache // Use cached responses if available
		}
		return BehaviorProceed

	case "complex_workflow":
		if health.Overall >= LevelModerate {
			return BehaviorDegrade // Downgrade to standard/simple mode
		}
		return BehaviorProceed

	case "non_essential_logging":
		if health.Overall >= LevelSevere {
			return BehaviorSkip // Skip non-essential operations
		}
		return BehaviorProceed

	default:
		// Conservative default - proceed with warnings
		return BehaviorProceed
	}
}

// RecordDegradation records degradation events for monitoring
func (ds *DefaultStrategy) RecordDegradation(level DegradationLevel, reason string) {
	ds.logger.Info("Degradation event recorded",
		zap.String("level", level.String()),
		zap.String("reason", reason),
		zap.Time("timestamp", time.Now()),
	)
	
	// Update metrics
	degradationEventsTotal.WithLabelValues(level.String(), reason).Inc()
	currentDegradationLevel.Set(float64(level))
}

// checkSystemHealth checks the health of all dependencies
func (ds *DefaultStrategy) checkSystemHealth() SystemHealth {
	now := time.Now()
	
	health := SystemHealth{
		Timestamp: now,
		Redis: DependencyHealth{
			Name:          "redis",
			IsHealthy:     !ds.redisWrapper.IsCircuitBreakerOpen(),
			LastCheckTime: now,
		},
		Database: DependencyHealth{
			Name:          "database",
			IsHealthy:     !ds.databaseWrapper.IsCircuitBreakerOpen(),
			LastCheckTime: now,
		},
		AgentCore: DependencyHealth{
			Name:          "agent-core",
			IsHealthy:     ds.agentCoreHealthy,
			LastCheckTime: now,
		},
		LLMService: DependencyHealth{
			Name:          "llm-service",
			IsHealthy:     ds.llmServiceHealthy,
			LastCheckTime: now,
		},
	}

	// Map circuit breaker states
	if ds.redisWrapper.IsCircuitBreakerOpen() {
		health.Redis.CircuitBreaker = circuitbreaker.StateOpen
	} else {
		health.Redis.CircuitBreaker = circuitbreaker.StateClosed
	}

	if ds.databaseWrapper.IsCircuitBreakerOpen() {
		health.Database.CircuitBreaker = circuitbreaker.StateOpen
	} else {
		health.Database.CircuitBreaker = circuitbreaker.StateClosed
	}

	// Determine overall health
	failedCount := 0
	if !health.Redis.IsHealthy {
		failedCount++
	}
	if !health.Database.IsHealthy {
		failedCount++
	}
	if !health.AgentCore.IsHealthy {
		failedCount++
	}
	if !health.LLMService.IsHealthy {
		failedCount++
	}

	switch failedCount {
	case 0:
		health.Overall = LevelNone
	case 1:
		health.Overall = LevelMinor
	case 2:
		health.Overall = LevelModerate
	default:
		health.Overall = LevelSevere
	}

	return health
}

// UpdateAgentCoreHealth updates the agent core health status
func (ds *DefaultStrategy) UpdateAgentCoreHealth(healthy bool) {
	ds.agentCoreHealthy = healthy
}

// UpdateLLMServiceHealth updates the LLM service health status  
func (ds *DefaultStrategy) UpdateLLMServiceHealth(healthy bool) {
	ds.llmServiceHealthy = healthy
}