package health

import (
	"context"
	"time"
)

// CheckStatus represents the result of a health check
type CheckStatus int

const (
	StatusHealthy CheckStatus = iota
	StatusDegraded
	StatusUnhealthy
	StatusUnknown
)

func (s CheckStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// CheckResult contains the result of a health check
type CheckResult struct {
	Status      CheckStatus            `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Timestamp   time.Time              `json:"timestamp"`
	Component   string                 `json:"component"`
	Critical    bool                   `json:"critical"`    // Whether failure affects service availability
}

// Checker defines the interface for health checks
type Checker interface {
	// Name returns the unique name of this health check
	Name() string
	
	// Check performs the health check and returns the result
	Check(ctx context.Context) CheckResult
	
	// IsCritical returns true if this check's failure should mark the service as unhealthy
	IsCritical() bool
	
	// Timeout returns the maximum duration this check should take
	Timeout() time.Duration
}

// Registrar allows components to register health checks
type Registrar interface {
	// RegisterChecker registers a health check
	RegisterChecker(checker Checker) error
	
	// UnregisterChecker removes a health check
	UnregisterChecker(name string) error
	
	// GetCheckers returns all registered checkers
	GetCheckers() map[string]Checker
}

// Reporter provides health status reporting
type Reporter interface {
	// GetOverallHealth returns the overall health status
	GetOverallHealth(ctx context.Context) OverallHealth
	
	// GetDetailedHealth returns detailed health information
	GetDetailedHealth(ctx context.Context) DetailedHealth
	
	// IsReady returns true if the service is ready to serve requests
	IsReady(ctx context.Context) bool
	
	// IsLive returns true if the service is alive (for liveness probes)
	IsLive(ctx context.Context) bool
}

// OverallHealth represents the overall service health
type OverallHealth struct {
	Status      CheckStatus   `json:"status"`
	Message     string        `json:"message,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
	Duration    time.Duration `json:"duration"`
	Degraded    bool          `json:"degraded"`
	Ready       bool          `json:"ready"`
	Live        bool          `json:"live"`
}

// DetailedHealth provides detailed health information
type DetailedHealth struct {
	Overall     OverallHealth              `json:"overall"`
	Components  map[string]CheckResult     `json:"components"`
	Summary     HealthSummary              `json:"summary"`
	Timestamp   time.Time                  `json:"timestamp"`
}

// HealthSummary provides summary statistics
type HealthSummary struct {
	Total      int `json:"total"`
	Healthy    int `json:"healthy"`
	Degraded   int `json:"degraded"`
	Unhealthy  int `json:"unhealthy"`
	Critical   int `json:"critical"`
	NonCritical int `json:"non_critical"`
}

// HealthManager combines registration and reporting functionality  
type HealthManager interface {
	Registrar
	Reporter
	
	// Start begins background health checking
	Start(ctx context.Context) error
	
	// Stop stops background health checking
	Stop() error
}