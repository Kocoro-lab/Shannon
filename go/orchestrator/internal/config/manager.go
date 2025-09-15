package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ConfigFormat represents supported configuration file formats
type ConfigFormat string

const (
	FormatJSON ConfigFormat = "json"
	FormatYAML ConfigFormat = "yaml"
)

// ChangeEvent represents a configuration change event
type ChangeEvent struct {
	File      string                 `json:"file"`
	Action    string                 `json:"action"` // create, modify, delete
	Config    map[string]interface{} `json:"config"`
	Timestamp time.Time              `json:"timestamp"`
}

// ChangeHandler is called when configuration changes
type ChangeHandler func(event ChangeEvent) error

// ConfigManager manages file-based configuration with hot-reload
type ConfigManager struct {
	configDir     string
	configs       map[string]map[string]interface{}
	handlers      map[string][]ChangeHandler
	policyHandlers []func() error  // Policy reload handlers for .rego files
	watcher       *fsnotify.Watcher
	started       bool
	stopCh        chan struct{}
	logger        *zap.Logger
	mu            sync.RWMutex
	watcherMu     sync.Mutex
	
	// Configuration validation
	validators    map[string]func(map[string]interface{}) error
	
	// Polling fallback for when fsnotify isn't reliable
	pollInterval  time.Duration
	enablePolling bool
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configDir string, logger *zap.Logger) (*ConfigManager, error) {
	if configDir == "" {
		return nil, fmt.Errorf("config directory cannot be empty")
	}

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &ConfigManager{
		configDir:     configDir,
		configs:       make(map[string]map[string]interface{}),
		handlers:      make(map[string][]ChangeHandler),
		validators:    make(map[string]func(map[string]interface{}) error),
		watcher:       watcher,
		stopCh:        make(chan struct{}),
		logger:        logger,
		pollInterval:  10 * time.Second, // Fallback polling interval
		enablePolling: false,            // Disabled by default
	}, nil
}

// Start begins watching for configuration changes
func (cm *ConfigManager) Start(ctx context.Context) error {
    // Fast path: avoid holding cm.mu while doing I/O (watcher add, file loads)
    cm.mu.Lock()
    if cm.started {
        cm.mu.Unlock()
        return nil
    }
    cm.mu.Unlock()

    // Add config directory to watcher (no cm.mu needed)
    if err := cm.watcher.Add(cm.configDir); err != nil {
        return fmt.Errorf("failed to watch config directory: %w", err)
    }

    // Load initial configurations outside of cm.mu to avoid deadlocks
    if err := cm.loadAllConfigs(); err != nil {
        return fmt.Errorf("failed to load initial configs: %w", err)
    }

    // Mark as started
    cm.mu.Lock()
    cm.started = true
    // Snapshot values for logging while holding the lock
    loaded := len(cm.configs)
    polling := cm.enablePolling
    cm.mu.Unlock()

    // Start file watcher goroutine
    go cm.watchLoop()

    // Start polling fallback if enabled
    if polling {
        go cm.pollLoop()
    }

    cm.logger.Info("Configuration manager started",
        zap.String("config_dir", cm.configDir),
        zap.Int("loaded_configs", loaded),
        zap.Bool("polling_enabled", polling),
    )

    return nil
}

// Stop stops watching for configuration changes
func (cm *ConfigManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return nil
	}

	close(cm.stopCh)
	if err := cm.watcher.Close(); err != nil {
		cm.logger.Error("Error closing file watcher", zap.Error(err))
	}
	
	cm.started = false
	cm.logger.Info("Configuration manager stopped")
	
	return nil
}

// RegisterHandler registers a change handler for a specific config file
func (cm *ConfigManager) RegisterHandler(filename string, handler ChangeHandler) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.handlers[filename] == nil {
		cm.handlers[filename] = make([]ChangeHandler, 0)
	}
	cm.handlers[filename] = append(cm.handlers[filename], handler)

	cm.logger.Info("Configuration handler registered",
		zap.String("filename", filename),
		zap.Int("total_handlers", len(cm.handlers[filename])),
	)
}

// RegisterValidator registers a configuration validator for a specific file
func (cm *ConfigManager) RegisterValidator(filename string, validator func(map[string]interface{}) error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.validators[filename] = validator
	cm.logger.Info("Configuration validator registered", zap.String("filename", filename))
}

// RegisterPolicyHandler registers a handler for policy file changes
func (cm *ConfigManager) RegisterPolicyHandler(handler func() error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.policyHandlers = append(cm.policyHandlers, handler)
	cm.logger.Info("Policy reload handler registered")
}

// GetConfig returns the current configuration for a file
func (cm *ConfigManager) GetConfig(filename string) (map[string]interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config, exists := cm.configs[filename]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent concurrent modification
	result := make(map[string]interface{})
	for k, v := range config {
		result[k] = v
	}
	
	return result, true
}

// GetAllConfigs returns all loaded configurations
func (cm *ConfigManager) GetAllConfigs() map[string]map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]map[string]interface{})
	for filename, config := range cm.configs {
		configCopy := make(map[string]interface{})
		for k, v := range config {
			configCopy[k] = v
		}
		result[filename] = configCopy
	}
	
	return result
}

// ReloadConfig manually reloads a specific configuration file
func (cm *ConfigManager) ReloadConfig(filename string) error {
	filePath := filepath.Join(cm.configDir, filename)
	return cm.loadConfigFile(filePath, "manual_reload")
}

// ReloadAllConfigs manually reloads all configuration files
func (cm *ConfigManager) ReloadAllConfigs() error {
	return cm.loadAllConfigs()
}

// SetConfig programmatically sets a configuration (useful for testing)
func (cm *ConfigManager) SetConfig(filename string, config map[string]interface{}) error {
    // Get validator with minimal lock time
    cm.mu.RLock()
    validator, hasValidator := cm.validators[filename]
    cm.mu.RUnlock()
    
    // Validate outside of lock
    if hasValidator {
        if err := validator(config); err != nil {
            return fmt.Errorf("configuration validation failed for %s: %w", filename, err)
        }
    }

    // Create a deep copy of the config for handlers
    configCopy := make(map[string]interface{})
    for k, v := range config {
        configCopy[k] = v
    }

    // Update config and copy handlers under lock
    cm.mu.Lock()
    cm.configs[filename] = config
    handlers := make([]ChangeHandler, len(cm.handlers[filename]))
    copy(handlers, cm.handlers[filename])
    cm.mu.Unlock()

    // Notify handlers asynchronously without holding any locks
    if len(handlers) > 0 {
        event := ChangeEvent{
            File:      filename,
            Action:    "programmatic_set",
            Config:    configCopy, // Use the copy to prevent concurrent modification
            Timestamp: time.Now(),
        }

        // Execute handlers asynchronously to prevent blocking
        for _, handler := range handlers {
            h := handler
            go func() {
                if err := h(event); err != nil {
                    cm.logger.Error("Configuration handler error",
                        zap.String("filename", filename),
                        zap.String("action", "programmatic_set"),
                        zap.Error(err),
                    )
                }
            }()
        }
    }

    cm.logger.Info("Configuration set programmatically",
        zap.String("filename", filename),
        zap.Int("keys", len(config)),
    )

    return nil
}

// EnablePolling enables polling fallback for unreliable filesystems
func (cm *ConfigManager) EnablePolling(interval time.Duration) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.enablePolling = true
	cm.pollInterval = interval
	
	cm.logger.Info("Configuration polling enabled", zap.Duration("interval", interval))
}

// watchLoop handles file system events
func (cm *ConfigManager) watchLoop() {
	defer func() {
		if r := recover(); r != nil {
			cm.logger.Error("Watch loop panicked", zap.Any("panic", r))
		}
	}()

	for {
		select {
		case <-cm.stopCh:
			return
		case event, ok := <-cm.watcher.Events:
			if !ok {
				return
			}
			cm.handleWatchEvent(event)
		case err, ok := <-cm.watcher.Errors:
			if !ok {
				return
			}
			cm.logger.Error("File watcher error", zap.Error(err))
		}
	}
}

// pollLoop provides polling fallback for file changes
func (cm *ConfigManager) pollLoop() {
	ticker := time.NewTicker(cm.pollInterval)
	defer ticker.Stop()

	lastModTimes := make(map[string]time.Time)

	for {
		select {
		case <-cm.stopCh:
			return
		case <-ticker.C:
			cm.checkForChanges(lastModTimes)
		}
	}
}

// checkForChanges checks for file modifications via polling
func (cm *ConfigManager) checkForChanges(lastModTimes map[string]time.Time) {
	err := filepath.WalkDir(cm.configDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !cm.isConfigFile(path) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		filename := filepath.Base(path)
		lastMod := lastModTimes[filename]
		currentMod := info.ModTime()

		if currentMod.After(lastMod) {
			lastModTimes[filename] = currentMod
			cm.logger.Debug("Detected file change via polling",
				zap.String("file", filename),
				zap.Time("mod_time", currentMod),
			)
			return cm.loadConfigFile(path, "polling_detected")
		}

		return nil
	})

	if err != nil {
		cm.logger.Error("Error during polling check", zap.Error(err))
	}
}

// handleWatchEvent processes file system watch events
func (cm *ConfigManager) handleWatchEvent(event fsnotify.Event) {
	cm.watcherMu.Lock()
	defer cm.watcherMu.Unlock()

	filename := filepath.Base(event.Name)
	
	// Process config files OR policy files
	isConfig := cm.isConfigFile(event.Name)
	isPolicy := cm.isPolicyFile(event.Name)
	
	if !isConfig && !isPolicy {
		return
	}

	cm.logger.Debug("File system event",
		zap.String("file", filename),
		zap.String("op", event.Op.String()),
	)

	var action string
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		action = "create"
	case event.Op&fsnotify.Write == fsnotify.Write:
		action = "modify"
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		action = "delete"
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		action = "rename"
	case event.Op&fsnotify.Chmod == fsnotify.Chmod:
		// Usually ignore chmod events
		return
	default:
		action = event.Op.String()
	}

	if action == "delete" || action == "rename" {
		if isConfig {
			cm.handleFileRemoval(filename)
		}
		// For policy files, we still trigger a reload since policies may reference the deleted file
		if isPolicy {
			cm.handlePolicyReload(filename, action)
		}
	} else {
		// Small delay to handle rapid successive writes
		time.Sleep(50 * time.Millisecond)
		
		if isConfig {
			if err := cm.loadConfigFile(event.Name, action); err != nil {
				cm.logger.Error("Failed to load config file",
					zap.String("file", filename),
					zap.String("action", action),
					zap.Error(err),
				)
			}
		}
		
		if isPolicy {
			cm.handlePolicyReload(filename, action)
		}
	}
}

// loadAllConfigs loads all configuration files in the directory
func (cm *ConfigManager) loadAllConfigs() error {
	return filepath.WalkDir(cm.configDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !cm.isConfigFile(path) {
			return nil
		}

		return cm.loadConfigFile(path, "initial_load")
	})
}

// loadConfigFile loads a single configuration file
func (cm *ConfigManager) loadConfigFile(filePath, action string) error {
	// Perform all I/O and parsing operations before acquiring any locks
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	filename := filepath.Base(filePath)
	config := make(map[string]interface{})

	// Parse based on file extension (no lock needed for these pure functions)
	format := cm.detectFormat(filename)
	switch format {
	case FormatJSON:
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse JSON config %s: %w", filename, err)
		}
	case FormatYAML:
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse YAML config %s: %w", filename, err)
		}
	default:
		return fmt.Errorf("unsupported config format for %s", filename)
	}

	// Get validator with minimal lock time
	cm.mu.RLock()
	validator := cm.validators[filename]
	cm.mu.RUnlock()
	
	// Validate outside of lock
	if validator != nil {
		if err := validator(config); err != nil {
			return fmt.Errorf("configuration validation failed for %s: %w", filename, err)
		}
	}

	// Create a deep copy of the config for handlers to avoid sharing mutable data
	configCopy := make(map[string]interface{})
	for k, v := range config {
		configCopy[k] = v
	}

	// Update configuration and get handlers in one lock operation
	cm.mu.Lock()
	cm.configs[filename] = config
	// Make a copy of handlers slice to avoid holding lock during handler execution
	handlers := make([]ChangeHandler, len(cm.handlers[filename]))
	copy(handlers, cm.handlers[filename])
	cm.mu.Unlock()

	// Notify handlers without holding any locks (prevents deadlock if handler calls back)
	if len(handlers) > 0 {
		event := ChangeEvent{
			File:      filename,
			Action:    action,
			Config:    configCopy, // Use the copy to prevent concurrent modification
			Timestamp: time.Now(),
		}

		// Execute handlers asynchronously to prevent blocking
		for _, handler := range handlers {
			// Copy handler to avoid closure issues
			h := handler
			go func() {
				if err := h(event); err != nil {
					cm.logger.Error("Configuration handler error",
						zap.String("filename", filename),
						zap.String("action", action),
						zap.Error(err),
					)
				}
			}()
		}
	}

	cm.logger.Info("Configuration loaded",
		zap.String("filename", filename),
		zap.String("action", action),
		zap.String("format", string(format)),
		zap.Int("keys", len(config)),
	)

	return nil
}

// handleFileRemoval handles when a config file is removed
func (cm *ConfigManager) handleFileRemoval(filename string) {
	// Get config and handlers, then remove from map
	cm.mu.Lock()
	config := cm.configs[filename]
	delete(cm.configs, filename)
	handlers := make([]ChangeHandler, len(cm.handlers[filename]))
	copy(handlers, cm.handlers[filename])
	cm.mu.Unlock()

	// Create a copy of the last known config for handlers
	var configCopy map[string]interface{}
	if config != nil {
		configCopy = make(map[string]interface{})
		for k, v := range config {
			configCopy[k] = v
		}
	}

	// Notify handlers asynchronously without holding locks
	if len(handlers) > 0 {
		event := ChangeEvent{
			File:      filename,
			Action:    "delete",
			Config:    configCopy, // Last known config (copy)
			Timestamp: time.Now(),
		}

		// Execute handlers asynchronously to prevent blocking
		for _, handler := range handlers {
			h := handler
			go func() {
				if err := h(event); err != nil {
					cm.logger.Error("Configuration handler error on deletion",
						zap.String("filename", filename),
						zap.Error(err),
					)
				}
			}()
		}
	}

	cm.logger.Info("Configuration file removed", zap.String("filename", filename))
}

// handlePolicyReload triggers policy engine reloads when .rego files change
func (cm *ConfigManager) handlePolicyReload(filename, action string) {
	cm.mu.RLock()
	handlers := make([]func() error, len(cm.policyHandlers))
	copy(handlers, cm.policyHandlers)
	cm.mu.RUnlock()
	
	cm.logger.Info("Policy file changed, triggering reload",
		zap.String("file", filename),
		zap.String("action", action),
		zap.Int("handlers", len(handlers)),
	)
	
	for _, handler := range handlers {
		if err := handler(); err != nil {
			cm.logger.Error("Policy reload handler failed",
				zap.String("file", filename),
				zap.String("action", action),
				zap.Error(err),
			)
		}
	}
}

// isConfigFile checks if a file is a supported configuration file
func (cm *ConfigManager) isConfigFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

// isPolicyFile checks if a file is a policy file that should trigger policy reloads
func (cm *ConfigManager) isPolicyFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".rego"
}

// detectFormat detects the configuration file format
func (cm *ConfigManager) detectFormat(filename string) ConfigFormat {
	ext := filepath.Ext(filename)
	switch ext {
	case ".json":
		return FormatJSON
	case ".yaml", ".yml":
		return FormatYAML
	default:
		return FormatJSON // Default fallback
	}
}
