package personas

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// Manager implements the PersonaManager interface
type Manager struct {
	selector   Selector
	config     *Config
	configPath string
	cache      Cache
	metrics    *Metrics
	logger     *zap.Logger

	// Configuration hot-reloading
	watcher   *fsnotify.Watcher
	watcherMu sync.RWMutex

	// Statistics tracking
	statsTracker *StatsTracker

	// Lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	closeMu sync.RWMutex
}

// StatsTracker tracks persona execution statistics
type StatsTracker struct {
	personaStats map[string]*PersonaStats
	mu           sync.RWMutex
	windowSize   time.Duration
}

// PersonaStats holds statistics for a single persona
type PersonaStats struct {
	TotalExecutions int
	SuccessCount    int
	RecentResults   []ExecutionResult
	LastUpdated     time.Time
	AvgTokenUsage   float64
	AvgDuration     time.Duration
	SuccessRate     float64
}

// NewManager creates a new persona manager
func NewManager(configPath string, logger *zap.Logger) (*Manager, error) {
	// Load initial configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create metrics
	metrics := NewMetrics()

	// Create cache
	cache := NewSafeCache(config.Selection.CacheTTL, logger, metrics)

	// Create selector - use enhanced selector for Phase 2 semantic matching
	matcherConfig := &MatcherConfig{
		EmbeddingEnabled: config.Selection.Method == "semantic" || config.Selection.Method == "enhanced",
		LocalFallback:    true,
		APITimeout:       2000, // 2 second timeout for external APIs
	}
	selector := NewEnhancedSelector(config, cache, metrics, logger, matcherConfig)

	// Create context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Create stats tracker
	statsTracker := &StatsTracker{
		personaStats: make(map[string]*PersonaStats),
		windowSize:   24 * time.Hour, // 24-hour rolling window
	}

	manager := &Manager{
		selector:     selector,
		config:       config,
		configPath:   configPath,
		cache:        cache,
		metrics:      metrics,
		logger:       logger,
		statsTracker: statsTracker,
		ctx:          ctx,
		cancel:       cancel,
		closed:       false,
	}

	// Initialize file watcher for hot-reloading
	if err := manager.initFileWatcher(); err != nil {
		logger.Warn("Failed to initialize file watcher", zap.Error(err))
		// Don't fail creation, just log the warning
	}

	// Start background tasks
	manager.startBackgroundTasks()

	logger.Info("Persona manager created successfully",
		zap.String("config_path", configPath),
		zap.Int("persona_count", len(config.Personas)))

	return manager, nil
}

// initFileWatcher initializes the configuration file watcher
func (m *Manager) initFileWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Watch the configuration file
	err = watcher.Add(m.configPath)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	// Also watch the directory (for atomic file replacements)
	configDir := filepath.Dir(m.configPath)
	err = watcher.Add(configDir)
	if err != nil {
		// Non-critical, continue without directory watching
		m.logger.Warn("Failed to watch config directory", zap.String("dir", configDir), zap.Error(err))
	}

	m.watcherMu.Lock()
	m.watcher = watcher
	m.watcherMu.Unlock()

	return nil
}

// startBackgroundTasks starts background goroutines
func (m *Manager) startBackgroundTasks() {
	// Start file watcher goroutine
	if m.watcher != nil {
		m.wg.Add(1)
		go m.watchConfigFile()
	}

	// Start stats updater goroutine
	m.wg.Add(1)
	go m.updatePersonaStats()

	// Start metrics reporter goroutine
	m.wg.Add(1)
	go m.reportMetrics()
}

// watchConfigFile monitors configuration file changes
func (m *Manager) watchConfigFile() {
	defer m.wg.Done()

	m.watcherMu.RLock()
	watcher := m.watcher
	m.watcherMu.RUnlock()

	if watcher == nil {
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if it's our config file and it's a write event
			if (event.Name == m.configPath || filepath.Base(event.Name) == filepath.Base(m.configPath)) &&
				(event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {

				m.logger.Info("Configuration file changed, reloading", zap.String("file", event.Name))

				// Add a small delay to ensure file write is complete
				time.Sleep(100 * time.Millisecond)

				if err := m.ReloadConfig(); err != nil {
					m.logger.Error("Failed to reload configuration", zap.Error(err))
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			m.logger.Error("File watcher error", zap.Error(err))

		case <-m.ctx.Done():
			return
		}
	}
}

// updatePersonaStats periodically updates persona statistics
func (m *Manager) updatePersonaStats() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute) // Update every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.computeAndReportStats()

		case <-m.ctx.Done():
			return
		}
	}
}

// reportMetrics periodically reports metrics
func (m *Manager) reportMetrics() {
	defer m.wg.Done()

	ticker := time.NewTicker(1 * time.Minute) // Report every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.reportCurrentMetrics()

		case <-m.ctx.Done():
			return
		}
	}
}

// computeAndReportStats computes and reports persona statistics
func (m *Manager) computeAndReportStats() {
	m.statsTracker.mu.RLock()
	defer m.statsTracker.mu.RUnlock()

	for personaID, stats := range m.statsTracker.personaStats {
		// Calculate success rate
		if stats.TotalExecutions > 0 {
			successRate := float64(stats.SuccessCount) / float64(stats.TotalExecutions)
			stats.SuccessRate = successRate

			// Report to metrics
			m.metrics.UpdateSuccessRate(personaID, "24h", successRate)
		}

		// Calculate token efficiency (quality / tokens used)
		if stats.AvgTokenUsage > 0 {
			// This is a placeholder - in practice, you'd need quality metrics
			efficiency := 1.0 / stats.AvgTokenUsage * 1000 // Normalize
			m.metrics.UpdateTokenEfficiency(personaID, "default", efficiency)
		}
	}
}

// reportCurrentMetrics reports current system metrics
func (m *Manager) reportCurrentMetrics() {
	// Report cache size
	if m.cache != nil {
		m.metrics.CacheSize.Set(float64(m.cache.Size()))
	}

	// Report configuration count
	if m.config != nil {
		// Could add a metric for persona count
		m.logger.Debug("Current metrics reported",
			zap.Int("persona_count", len(m.config.Personas)),
			zap.Int("cache_size", m.cache.Size()))
	}
}

// SelectPersona implements PersonaManager interface
func (m *Manager) SelectPersona(ctx context.Context, req *SelectionRequest) (*SelectionResult, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, ErrManagerClosed
	}
	m.closeMu.RUnlock()

	return m.selector.SelectPersona(ctx, req)
}

// GetPersona implements PersonaManager interface
func (m *Manager) GetPersona(personaID string) (*PersonaConfig, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, ErrManagerClosed
	}
	m.closeMu.RUnlock()

	return m.selector.GetPersona(personaID)
}

// ListPersonas implements PersonaManager interface
func (m *Manager) ListPersonas(filter *PersonaFilter) ([]*PersonaConfig, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, ErrManagerClosed
	}
	m.closeMu.RUnlock()

	return m.selector.ListPersonas(filter)
}

// ValidateConfig implements PersonaManager interface
func (m *Manager) ValidateConfig() error {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return ErrManagerClosed
	}
	m.closeMu.RUnlock()

	return m.selector.ValidateConfig()
}

// ReloadConfig reloads the configuration from file
func (m *Manager) ReloadConfig() error {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return ErrManagerClosed
	}
	m.closeMu.RUnlock()

	// Load new configuration
	newConfig, err := LoadConfig(m.configPath)
	if err != nil {
		m.logger.Error("Failed to load new configuration", zap.Error(err))
		return err
	}

	// Validate new configuration
	if err := newConfig.Validate(); err != nil {
		m.logger.Error("New configuration is invalid", zap.Error(err))
		return err
	}

	// Atomically update the configuration
	oldConfig := m.config
	m.config = newConfig

	// Update selector's configuration
	if enhancedSelector, ok := m.selector.(*EnhancedSelector); ok {
		enhancedSelector.config = newConfig
	} else if keywordSelector, ok := m.selector.(*KeywordSelector); ok {
		keywordSelector.config = newConfig
	}

	// Clear cache to ensure fresh selections with new config
	m.cache.Clear()

	m.logger.Info("Configuration reloaded successfully",
		zap.String("path", m.configPath),
		zap.Int("old_persona_count", len(oldConfig.Personas)),
		zap.Int("new_persona_count", len(newConfig.Personas)))

	return nil
}

// GetMetrics returns current metrics
func (m *Manager) GetMetrics() *Metrics {
	return m.metrics
}

// UpdatePersonaStats updates persona execution statistics
func (m *Manager) UpdatePersonaStats(personaID string, result *ExecutionResult) error {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return ErrManagerClosed
	}
	m.closeMu.RUnlock()

	m.statsTracker.mu.Lock()
	defer m.statsTracker.mu.Unlock()

	// Get or create stats for this persona
	stats, exists := m.statsTracker.personaStats[personaID]
	if !exists {
		stats = &PersonaStats{
			RecentResults: make([]ExecutionResult, 0),
			LastUpdated:   time.Now(),
		}
		m.statsTracker.personaStats[personaID] = stats
	}

	// Update statistics
	stats.TotalExecutions++
	if result.Success {
		stats.SuccessCount++
	}

	// Add to recent results (maintain sliding window)
	stats.RecentResults = append(stats.RecentResults, *result)
	if len(stats.RecentResults) > 100 { // Keep last 100 results
		stats.RecentResults = stats.RecentResults[1:]
	}

	// Update averages
	m.updateAverages(stats)
	stats.LastUpdated = time.Now()

	// Report to metrics
	m.metrics.RecordSelection(personaID, "execution", result.Duration, result.Success)

	return nil
}

// updateAverages updates the average statistics
func (m *Manager) updateAverages(stats *PersonaStats) {
	if len(stats.RecentResults) == 0 {
		return
	}

	totalTokens := 0
	totalDuration := time.Duration(0)
	cutoff := time.Now().Add(-m.statsTracker.windowSize)
	validResults := 0

	for _, result := range stats.RecentResults {
		if result.Timestamp.After(cutoff) {
			totalTokens += result.TokensUsed
			totalDuration += result.Duration
			validResults++
		}
	}

	if validResults > 0 {
		stats.AvgTokenUsage = float64(totalTokens) / float64(validResults)
		stats.AvgDuration = totalDuration / time.Duration(validResults)
	}
}

// GetPersonaStats returns statistics for a specific persona
func (m *Manager) GetPersonaStats(personaID string) (*PersonaStats, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, ErrManagerClosed
	}
	m.closeMu.RUnlock()

	m.statsTracker.mu.RLock()
	defer m.statsTracker.mu.RUnlock()

	stats, exists := m.statsTracker.personaStats[personaID]
	if !exists {
		return nil, fmt.Errorf("no statistics found for persona: %s", personaID)
	}

	// Return a copy to avoid race conditions
	statsCopy := *stats
	statsCopy.RecentResults = make([]ExecutionResult, len(stats.RecentResults))
	copy(statsCopy.RecentResults, stats.RecentResults)

	return &statsCopy, nil
}

// GetAllStats returns statistics for all personas
func (m *Manager) GetAllStats() map[string]*PersonaStats {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil
	}
	m.closeMu.RUnlock()

	m.statsTracker.mu.RLock()
	defer m.statsTracker.mu.RUnlock()

	result := make(map[string]*PersonaStats)
	for personaID, stats := range m.statsTracker.personaStats {
		// Create a copy
		statsCopy := *stats
		statsCopy.RecentResults = make([]ExecutionResult, len(stats.RecentResults))
		copy(statsCopy.RecentResults, stats.RecentResults)
		result[personaID] = &statsCopy
	}

	return result
}

// Close gracefully shuts down the manager
func (m *Manager) Close() error {
	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return nil
	}
	m.closed = true
	m.closeMu.Unlock()

	m.logger.Info("Shutting down persona manager...")

	// Cancel context to stop background tasks
	m.cancel()

	// Close file watcher
	m.watcherMu.Lock()
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
	m.watcherMu.Unlock()

	// Wait for background tasks to complete with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("All background tasks completed")
	case <-time.After(10 * time.Second):
		m.logger.Warn("Shutdown timeout reached, some tasks may not have completed")
	}

	// Close selector
	if err := m.selector.Close(); err != nil {
		m.logger.Error("Failed to close selector", zap.Error(err))
	}

	// Close cache
	if err := m.cache.Close(); err != nil {
		m.logger.Error("Failed to close cache", zap.Error(err))
	}

	m.logger.Info("Persona manager shutdown completed")
	return nil
}

// GetStatus returns the current status of the manager
func (m *Manager) GetStatus() map[string]interface{} {
	m.closeMu.RLock()
	defer m.closeMu.RUnlock()

	status := map[string]interface{}{
		"closed":        m.closed,
		"config_path":   m.configPath,
		"persona_count": len(m.config.Personas),
		"cache_size":    m.cache.Size(),
	}

	if m.watcher != nil {
		status["file_watcher"] = "active"
	} else {
		status["file_watcher"] = "inactive"
	}

	// Add persona names
	personaNames := make([]string, 0, len(m.config.Personas))
	for id := range m.config.Personas {
		personaNames = append(personaNames, id)
	}
	status["personas"] = personaNames

	return status
}
