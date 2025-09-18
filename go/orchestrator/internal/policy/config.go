package policy

import (
	"os"
	"strconv"
	"strings"
)

// Mode defines the policy engine operating mode
type Mode string

const (
	// ModeOff disables policy evaluation entirely
	ModeOff Mode = "off"
	// ModeDryRun evaluates policies but doesn't enforce them (log only)
	ModeDryRun Mode = "dry-run"
	// ModeEnforce evaluates and enforces policies
	ModeEnforce Mode = "enforce"
)

// CanaryConfig defines canary rollout configuration for staged enforcement
type CanaryConfig struct {
	// Enabled controls whether canary rollout is active
	Enabled bool

	// EnforcePercentage controls what percentage of requests get enforce mode
	// Remaining percentage will use dry-run mode for safety
	// Range: 0-100, default: 0 (all dry-run)
	EnforcePercentage int

	// EnforceUsers list of specific users who always get enforce mode
	EnforceUsers []string

	// EnforceAgents list of specific agents who always get enforce mode
	EnforceAgents []string

	// DryRunUsers list of specific users who always get dry-run mode (override percentage)
	DryRunUsers []string

	// SLOThresholds for monitoring and automatic rollback
	SLOThresholds SLOConfig
}

// SLOConfig defines Service Level Objective thresholds for safe rollout
type SLOConfig struct {
	// MaxErrorRate maximum acceptable error rate (0-100%)
	MaxErrorRate float64

	// MaxLatencyP95 maximum acceptable 95th percentile latency in milliseconds
	MaxLatencyP95 float64

	// MaxLatencyP50 maximum acceptable 50th percentile latency in milliseconds
	MaxLatencyP50 float64

	// MinCacheHitRate minimum acceptable cache hit rate (0-100%)
	MinCacheHitRate float64
}

// Config holds policy engine configuration
type Config struct {
	// Enabled controls whether the policy engine is active
	Enabled bool

	// Mode controls policy enforcement behavior
	Mode Mode

	// Path to the directory containing .rego policy files
	Path string

	// FailClosed determines behavior when policies can't be loaded
	// true: deny all requests if policies fail to load
	// false: allow all requests if policies fail to load (fail-open)
	FailClosed bool

	// Environment context for policy evaluation
	Environment string

	// Canary configuration for staged rollout
	Canary CanaryConfig

	// EmergencyKillSwitch forces all requests to dry-run mode regardless of other settings
	EmergencyKillSwitch bool
}

// LoadConfig loads policy configuration from environment variables
func LoadConfig() *Config {
	config := &Config{
		Enabled:             getEnvBool("SHANNON_POLICY_ENABLED", false),
		Mode:                Mode(getEnvString("SHANNON_POLICY_MODE", "off")),
		Path:                getEnvString("SHANNON_POLICY_PATH", "/app/config/opa/policies"),
		FailClosed:          getEnvBool("SHANNON_POLICY_FAIL_CLOSED", false),
		Environment:         getEnvString("ENVIRONMENT", "dev"),
		EmergencyKillSwitch: getEnvBool("SHANNON_POLICY_EMERGENCY_KILL_SWITCH", false),
		Canary: CanaryConfig{
			Enabled:           getEnvBool("SHANNON_POLICY_CANARY_ENABLED", false),
			EnforcePercentage: getEnvInt("SHANNON_POLICY_CANARY_ENFORCE_PERCENTAGE", 0),
			EnforceUsers:      getEnvStringSlice("SHANNON_POLICY_CANARY_ENFORCE_USERS"),
			EnforceAgents:     getEnvStringSlice("SHANNON_POLICY_CANARY_ENFORCE_AGENTS"),
			DryRunUsers:       getEnvStringSlice("SHANNON_POLICY_CANARY_DRYRUN_USERS"),
			SLOThresholds: SLOConfig{
				MaxErrorRate:    getEnvFloat64("SHANNON_POLICY_SLO_MAX_ERROR_RATE", 5.0),
				MaxLatencyP95:   getEnvFloat64("SHANNON_POLICY_SLO_MAX_LATENCY_P95", 5.0),
				MaxLatencyP50:   getEnvFloat64("SHANNON_POLICY_SLO_MAX_LATENCY_P50", 1.0),
				MinCacheHitRate: getEnvFloat64("SHANNON_POLICY_SLO_MIN_CACHE_HIT_RATE", 80.0),
			},
		},
	}

	// Validate and normalize mode
	switch config.Mode {
	case ModeOff, ModeDryRun, ModeEnforce:
		// Valid modes
	case "":
		config.Mode = ModeOff
	default:
		// Invalid mode, default to off
		config.Mode = ModeOff
	}

	// If mode is off, disable the engine
	if config.Mode == ModeOff {
		config.Enabled = false
	}

	return config
}

// LoadConfigFromShannon creates policy configuration from Shannon config
func LoadConfigFromShannon(shannonPolicy interface{}) *Config {
	// Start with environment defaults as fallback
	config := LoadConfig()

	// Parse Shannon policy config if provided
	if policyMap, ok := shannonPolicy.(map[string]interface{}); ok {
		if enabled, ok := policyMap["enabled"].(bool); ok {
			config.Enabled = enabled
		}
		if mode, ok := policyMap["mode"].(string); ok {
			config.Mode = Mode(mode)
		}
		if path, ok := policyMap["path"].(string); ok {
			config.Path = path
		}
		if failClosed, ok := policyMap["fail_closed"].(bool); ok {
			config.FailClosed = failClosed
		}
		if environment, ok := policyMap["environment"].(string); ok {
			config.Environment = environment
		}
		if emergencyKillSwitch, ok := policyMap["emergency_kill_switch"].(bool); ok {
			config.EmergencyKillSwitch = emergencyKillSwitch
		}

		// Parse canary configuration
		if canaryMap, ok := policyMap["canary"].(map[string]interface{}); ok {
			if enabled, ok := canaryMap["enabled"].(bool); ok {
				config.Canary.Enabled = enabled
			}
			if percentage, ok := canaryMap["enforce_percentage"].(int); ok {
				config.Canary.EnforcePercentage = percentage
			} else if percentageFloat, ok := canaryMap["enforce_percentage"].(float64); ok {
				config.Canary.EnforcePercentage = int(percentageFloat)
			}
			if users, ok := canaryMap["enforce_users"].([]interface{}); ok {
				config.Canary.EnforceUsers = interfaceSliceToStringSlice(users)
			}
			if agents, ok := canaryMap["enforce_agents"].([]interface{}); ok {
				config.Canary.EnforceAgents = interfaceSliceToStringSlice(agents)
			}
			if dryRunUsers, ok := canaryMap["dry_run_users"].([]interface{}); ok {
				config.Canary.DryRunUsers = interfaceSliceToStringSlice(dryRunUsers)
			}

			// Parse SLO thresholds
			if sloMap, ok := canaryMap["slo_thresholds"].(map[string]interface{}); ok {
				if maxErrorRate, ok := sloMap["max_error_rate"].(float64); ok {
					config.Canary.SLOThresholds.MaxErrorRate = maxErrorRate
				}
				if maxLatencyP95, ok := sloMap["max_latency_p95"].(float64); ok {
					config.Canary.SLOThresholds.MaxLatencyP95 = maxLatencyP95
				}
				if maxLatencyP50, ok := sloMap["max_latency_p50"].(float64); ok {
					config.Canary.SLOThresholds.MaxLatencyP50 = maxLatencyP50
				}
				if minCacheHitRate, ok := sloMap["min_cache_hit_rate"].(float64); ok {
					config.Canary.SLOThresholds.MinCacheHitRate = minCacheHitRate
				}
			}
		}
	}

	// Validate and normalize mode
	switch config.Mode {
	case ModeOff, ModeDryRun, ModeEnforce:
		// Valid modes
	case "":
		config.Mode = ModeOff
	default:
		// Invalid mode, default to off
		config.Mode = ModeOff
	}

	// If mode is off, disable the engine
	if config.Mode == ModeOff {
		config.Enabled = false
	}

	return config
}

// getEnvString returns environment variable value or default
func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool returns environment variable as boolean or default
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	// Parse common boolean representations
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on", "enable", "enabled":
		return true
	case "false", "0", "no", "off", "disable", "disabled":
		return false
	default:
		// Try parsing as boolean
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
		return defaultValue
	}
}

// getEnvInt returns environment variable as integer or default
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	if parsed, err := strconv.Atoi(value); err == nil {
		return parsed
	}
	return defaultValue
}

// getEnvFloat64 returns environment variable as float64 or default
func getEnvFloat64(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	return defaultValue
}

// getEnvStringSlice returns environment variable as string slice (comma-separated) or empty slice
func getEnvStringSlice(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return []string{}
	}

	// Split by comma and trim whitespace
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// interfaceSliceToStringSlice converts []interface{} to []string
func interfaceSliceToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if str, ok := item.(string); ok && str != "" {
			result = append(result, str)
		}
	}
	return result
}
