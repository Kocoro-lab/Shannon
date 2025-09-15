package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CheckerState represents the runtime state of a health checker
type CheckerState struct {
	checker   Checker
	enabled   bool
	interval  time.Duration
	timeout   time.Duration
	critical  bool
	lastCheck time.Time
}

// Manager implements the HealthManager interface
type Manager struct {
	checkers      map[string]*CheckerState
	lastResults   map[string]CheckResult
	config        *HealthConfiguration
	started       bool
	checkInterval time.Duration
	stopCh        chan struct{}
	logger        *zap.Logger
	mu            sync.RWMutex
}

// HealthConfiguration contains health check configuration
type HealthConfiguration struct {
	Enabled       bool
	CheckInterval time.Duration
	GlobalTimeout time.Duration
	Checks        map[string]CheckConfig
}

// CheckConfig represents individual check configuration
type CheckConfig struct {
	Enabled  bool
	Critical bool
	Timeout  time.Duration
	Interval time.Duration
}

// NewManager creates a new health manager
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		checkers:      make(map[string]*CheckerState),
		lastResults:   make(map[string]CheckResult),
		config:        &HealthConfiguration{
			Enabled:       true,
			CheckInterval: 30 * time.Second,
			GlobalTimeout: 5 * time.Second,
			Checks:        make(map[string]CheckConfig),
		},
		checkInterval: 30 * time.Second,
		stopCh:        make(chan struct{}),
		logger:        logger,
	}
}

// NewManagerWithConfig creates a health manager with specific configuration
func NewManagerWithConfig(config *HealthConfiguration, logger *zap.Logger) *Manager {
	if config == nil {
		return NewManager(logger)
	}
	
	return &Manager{
		checkers:      make(map[string]*CheckerState),
		lastResults:   make(map[string]CheckResult),
		config:        config,
		checkInterval: config.CheckInterval,
		stopCh:        make(chan struct{}),
		logger:        logger,
	}
}

// RegisterChecker registers a health check
func (m *Manager) RegisterChecker(checker Checker) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := checker.Name()
	if name == "" {
		return fmt.Errorf("checker name cannot be empty")
	}

	if _, exists := m.checkers[name]; exists {
		return fmt.Errorf("checker %s already registered", name)
	}

	// Get configuration for this checker
	checkConfig, hasConfig := m.config.Checks[name]
	
	// Create checker state with config or defaults
	state := &CheckerState{
		checker:  checker,
		enabled:  true,
		interval: m.config.CheckInterval,
		timeout:  checker.Timeout(),
		critical: checker.IsCritical(),
	}

	// Apply configuration overrides if available
	if hasConfig {
		state.enabled = checkConfig.Enabled
		if checkConfig.Interval > 0 {
			state.interval = checkConfig.Interval
		}
		if checkConfig.Timeout > 0 {
			state.timeout = checkConfig.Timeout
		}
		state.critical = checkConfig.Critical
	}

	m.checkers[name] = state
	m.logger.Info("Health checker registered",
		zap.String("checker", name),
		zap.Bool("enabled", state.enabled),
		zap.Bool("critical", state.critical),
		zap.Duration("timeout", state.timeout),
		zap.Duration("interval", state.interval),
	)

	return nil
}

// UnregisterChecker removes a health check
func (m *Manager) UnregisterChecker(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.checkers[name]; !exists {
		return fmt.Errorf("checker %s not found", name)
	}

	delete(m.checkers, name)
	delete(m.lastResults, name)

	m.logger.Info("Health checker unregistered", zap.String("checker", name))
	return nil
}

// GetCheckers returns all registered checkers
func (m *Manager) GetCheckers() map[string]Checker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Checker)
	for name, state := range m.checkers {
		result[name] = state.checker
	}
	return result
}

// GetOverallHealth returns the overall health status
func (m *Manager) GetOverallHealth(ctx context.Context) OverallHealth {
	startTime := time.Now()
	detailed := m.GetDetailedHealth(ctx)
	
	overall := OverallHealth{
		Status:    detailed.Overall.Status,
		Message:   detailed.Overall.Message,
		Timestamp: detailed.Timestamp,
		Duration:  time.Since(startTime),
		Degraded:  detailed.Overall.Degraded,
		Ready:     detailed.Overall.Ready,
		Live:      detailed.Overall.Live,
	}

	return overall
}

// GetDetailedHealth returns detailed health information
func (m *Manager) GetDetailedHealth(ctx context.Context) DetailedHealth {
	m.mu.RLock()
	checkerStates := make(map[string]*CheckerState)
	for name, state := range m.checkers {
		checkerStates[name] = state
	}
	m.mu.RUnlock()

	timestamp := time.Now()
	components := make(map[string]CheckResult)
	enabledCount := 0

	// Count enabled checkers
	for _, state := range checkerStates {
		if state.enabled {
			enabledCount++
		}
	}

	summary := HealthSummary{
		Total: enabledCount, // Only count enabled checkers
	}

	// Run health checks only for enabled checkers
	for name, state := range checkerStates {
		if !state.enabled {
			// Skip disabled checkers
			continue
		}

		result := m.runSingleCheckWithState(ctx, state)
		components[name] = result

		// Update summary
		switch result.Status {
		case StatusHealthy:
			summary.Healthy++
		case StatusDegraded:
			summary.Degraded++
		case StatusUnhealthy:
			summary.Unhealthy++
		}

		if result.Critical {
			summary.Critical++
		} else {
			summary.NonCritical++
		}
	}

	// Store results for future queries
	m.mu.Lock()
	for name, result := range components {
		m.lastResults[name] = result
	}
	m.mu.Unlock()

	// Determine overall status
	overall := m.calculateOverallStatus(components, summary)

	return DetailedHealth{
		Overall:    overall,
		Components: components,
		Summary:    summary,
		Timestamp:  timestamp,
	}
}

// runSingleCheck executes a single health check with timeout (legacy method)
func (m *Manager) runSingleCheck(ctx context.Context, checker Checker) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, checker.Timeout())
	defer cancel()

	startTime := time.Now()
	result := checker.Check(checkCtx)
	
	// Ensure result has required fields
	result.Component = checker.Name()
	result.Critical = checker.IsCritical()
	result.Duration = time.Since(startTime)
	result.Timestamp = startTime

	return result
}

// runSingleCheckWithState executes a single health check with state-based configuration
func (m *Manager) runSingleCheckWithState(ctx context.Context, state *CheckerState) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, state.timeout)
	defer cancel()

	startTime := time.Now()
	result := state.checker.Check(checkCtx)
	
	// Ensure result has required fields from state
	result.Component = state.checker.Name()
	result.Critical = state.critical
	result.Duration = time.Since(startTime)
	result.Timestamp = startTime

	// Update last check time
	state.lastCheck = startTime

	return result
}

// calculateOverallStatus determines overall health from component results
func (m *Manager) calculateOverallStatus(components map[string]CheckResult, summary HealthSummary) OverallHealth {
	if summary.Total == 0 {
		return OverallHealth{
			Status:  StatusUnknown,
			Message: "No health checks registered",
			Ready:   false,
			Live:    false,
		}
	}

	// Critical check failures make system unhealthy
	criticalFailures := 0
	nonCriticalFailures := 0
	degradedComponents := 0

	for _, result := range components {
		if result.Status == StatusDegraded {
			degradedComponents++
		}
		
		if result.Status == StatusUnhealthy {
			if result.Critical {
				criticalFailures++
			} else {
				nonCriticalFailures++
			}
		}
	}

	// Determine overall status
	var status CheckStatus
	var message string
	var ready, live bool

	if criticalFailures > 0 {
		status = StatusUnhealthy
		message = fmt.Sprintf("%d critical component(s) failing", criticalFailures)
		ready = false
		live = true // Still alive but not ready
	} else if degradedComponents > 0 {
		status = StatusDegraded
		message = fmt.Sprintf("%d component(s) degraded", degradedComponents)
		ready = true  // Still ready but degraded
		live = true
	} else if nonCriticalFailures > 0 {
		status = StatusDegraded
		message = fmt.Sprintf("%d non-critical component(s) failing", nonCriticalFailures)
		ready = true
		live = true
	} else {
		status = StatusHealthy
		message = fmt.Sprintf("All %d components healthy", summary.Total)
		ready = true
		live = true
	}

	return OverallHealth{
		Status:   status,
		Message:  message,
		Degraded: status == StatusDegraded || degradedComponents > 0,
		Ready:    ready,
		Live:     live,
	}
}

// IsReady returns true if the service is ready to serve requests
func (m *Manager) IsReady(ctx context.Context) bool {
	overall := m.GetOverallHealth(ctx)
	return overall.Ready
}

// IsLive returns true if the service is alive (for liveness probes)
func (m *Manager) IsLive(ctx context.Context) bool {
	overall := m.GetOverallHealth(ctx)
	return overall.Live
}

// Start begins background health checking
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.started = true
	go m.backgroundChecker()

	m.logger.Info("Health manager started",
		zap.Duration("check_interval", m.checkInterval),
		zap.Int("registered_checkers", len(m.checkers)),
	)

	return nil
}

// Stop stops background health checking
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	close(m.stopCh)
	m.started = false

	m.logger.Info("Health manager stopped")
	return nil
}

// backgroundChecker runs periodic health checks
func (m *Manager) backgroundChecker() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.runBackgroundChecks()
		}
	}
}

// runBackgroundChecks executes all health checks in background with per-check intervals
func (m *Manager) runBackgroundChecks() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m.mu.RLock()
	checkerStates := make(map[string]*CheckerState)
	for name, state := range m.checkers {
		checkerStates[name] = state
	}
	m.mu.RUnlock()

	now := time.Now()
	var checkResults = make(map[string]CheckResult)

	// Run checks based on individual intervals
	for name, state := range checkerStates {
		if !state.enabled {
			continue // Skip disabled checkers
		}

		// Check if it's time to run this check
		if now.Sub(state.lastCheck) >= state.interval {
			result := m.runSingleCheckWithState(ctx, state)
			checkResults[name] = result
		}
	}

	// Update last results if we have any new results
	if len(checkResults) > 0 {
		m.mu.Lock()
		for name, result := range checkResults {
			m.lastResults[name] = result
		}
		m.mu.Unlock()

		m.logger.Debug("Background health checks completed",
			zap.Int("checks_run", len(checkResults)),
		)
	}
}

// SetCheckInterval updates the background check interval
func (m *Manager) SetCheckInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.checkInterval = interval
	m.logger.Info("Health check interval updated", zap.Duration("interval", interval))
}

// GetLastResults returns the most recent health check results without running new checks
func (m *Manager) GetLastResults() map[string]CheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]CheckResult)
	for name, result := range m.lastResults {
		results[name] = result
	}
	return results
}

// UpdateConfiguration updates the health manager configuration
func (m *Manager) UpdateConfiguration(config *HealthConfiguration) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config
	m.checkInterval = config.CheckInterval

	// Update existing checker states with new configuration
	for name, state := range m.checkers {
		if checkConfig, exists := config.Checks[name]; exists {
			state.enabled = checkConfig.Enabled
			if checkConfig.Interval > 0 {
				state.interval = checkConfig.Interval
			}
			if checkConfig.Timeout > 0 {
				state.timeout = checkConfig.Timeout
			}
			state.critical = checkConfig.Critical

			m.logger.Info("Updated health checker configuration",
				zap.String("checker", name),
				zap.Bool("enabled", state.enabled),
				zap.Bool("critical", state.critical),
				zap.Duration("timeout", state.timeout),
				zap.Duration("interval", state.interval),
			)
		}
	}

	// Log overall configuration changes
	m.logger.Info("Health manager configuration updated",
		zap.Bool("enabled", config.Enabled),
		zap.Duration("global_check_interval", config.CheckInterval),
		zap.Duration("global_timeout", config.GlobalTimeout),
		zap.Int("check_configs", len(config.Checks)),
	)

	return nil
}

// GetConfiguration returns the current health configuration
func (m *Manager) GetConfiguration() *HealthConfiguration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent external modification
	configCopy := &HealthConfiguration{
		Enabled:       m.config.Enabled,
		CheckInterval: m.config.CheckInterval,
		GlobalTimeout: m.config.GlobalTimeout,
		Checks:        make(map[string]CheckConfig),
	}

	for name, checkConfig := range m.config.Checks {
		configCopy.Checks[name] = CheckConfig{
			Enabled:  checkConfig.Enabled,
			Critical: checkConfig.Critical,
			Timeout:  checkConfig.Timeout,
			Interval: checkConfig.Interval,
		}
	}

	return configCopy
}

// EnableChecker enables a specific health checker
func (m *Manager) EnableChecker(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.checkers[name]
	if !exists {
		return fmt.Errorf("checker %s not found", name)
	}

	if state.enabled {
		return nil // Already enabled
	}

	state.enabled = true
	m.logger.Info("Health checker enabled", zap.String("checker", name))
	return nil
}

// DisableChecker disables a specific health checker
func (m *Manager) DisableChecker(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.checkers[name]
	if !exists {
		return fmt.Errorf("checker %s not found", name)
	}

	if !state.enabled {
		return nil // Already disabled
	}

	state.enabled = false
	m.logger.Info("Health checker disabled", zap.String("checker", name))
	return nil
}

// GetCheckerStates returns the current state of all checkers (for debugging)
func (m *Manager) GetCheckerStates() map[string]CheckerState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make(map[string]CheckerState)
	for name, state := range m.checkers {
		states[name] = CheckerState{
			checker:   state.checker,
			enabled:   state.enabled,
			interval:  state.interval,
			timeout:   state.timeout,
			critical:  state.critical,
			lastCheck: state.lastCheck,
		}
	}
	return states
}