package config

import (
    "fmt"
    "os"

    "github.com/spf13/viper"
)

type ObservabilityConfig struct {
    Metrics struct {
        Enabled bool   `mapstructure:"enabled"`
        Provider string `mapstructure:"provider"`
        Port    int    `mapstructure:"port"`
    } `mapstructure:"metrics"`
    Logging struct {
        Level string `mapstructure:"level"`
        Format string `mapstructure:"format"`
    } `mapstructure:"logging"`
}

type Features struct {
    Observability ObservabilityConfig `mapstructure:"observability"`
    Budget        BudgetConfig        `mapstructure:"budget"`
}

// Load loads features.yaml from CONFIG_PATH or /app/config/features.yaml
func Load() (*Features, error) {
    cfgPath := os.Getenv("CONFIG_PATH")
    if cfgPath == "" {
        cfgPath = "/app/config/features.yaml"
    }

    v := viper.New()
    v.SetConfigFile(cfgPath)
    if err := v.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("read config: %w", err)
    }
    var f Features
    if err := v.Unmarshal(&f); err != nil {
        return nil, fmt.Errorf("unmarshal config: %w", err)
    }
    return &f, nil
}

// MetricsPort returns port from config or an env override METRICS_PORT, falling back to defaultPort
func MetricsPort(defaultPort int) int {
    if p := os.Getenv("METRICS_PORT"); p != "" {
        var v int
        _, _ = fmt.Sscanf(p, "%d", &v)
        if v > 0 { return v }
    }
    if f, err := Load(); err == nil {
        if f.Observability.Metrics.Port > 0 {
            return f.Observability.Metrics.Port
        }
    }
    return defaultPort
}

// BudgetConfig captures budget-related knobs loaded from config or env
type BudgetConfig struct {
    Backpressure struct {
        Threshold  float64 `mapstructure:"threshold"`
        MaxDelayMs int     `mapstructure:"max_delay_ms"`
    } `mapstructure:"backpressure"`
    CircuitBreaker struct {
        FailureThreshold int `mapstructure:"failure_threshold"`
        ResetTimeoutMs   int `mapstructure:"reset_timeout_ms"`
        HalfOpenRequests int `mapstructure:"half_open_requests"`
    } `mapstructure:"circuit_breaker"`
    RateLimit struct {
        Requests  int `mapstructure:"requests"`
        IntervalMs int `mapstructure:"interval_ms"`
    } `mapstructure:"rate_limit"`
}

// BudgetFromEnvOrDefaults returns merged budget config using env overrides first, then config file, with sensible defaults.
func BudgetFromEnvOrDefaults(f *Features) BudgetConfig {
    // defaults
    bc := BudgetConfig{}
    bc.Backpressure.Threshold = 0.8
    bc.Backpressure.MaxDelayMs = 5000
    bc.CircuitBreaker.FailureThreshold = 5
    bc.CircuitBreaker.ResetTimeoutMs = 60000
    bc.CircuitBreaker.HalfOpenRequests = 1
    // rate-limit defaults disabled (0)

    // merge from config file if provided
    if f != nil {
        if f.Budget.Backpressure.Threshold > 0 {
            bc.Backpressure.Threshold = f.Budget.Backpressure.Threshold
        }
        if f.Budget.Backpressure.MaxDelayMs > 0 {
            bc.Backpressure.MaxDelayMs = f.Budget.Backpressure.MaxDelayMs
        }
        if f.Budget.CircuitBreaker.FailureThreshold > 0 {
            bc.CircuitBreaker.FailureThreshold = f.Budget.CircuitBreaker.FailureThreshold
        }
        if f.Budget.CircuitBreaker.ResetTimeoutMs > 0 {
            bc.CircuitBreaker.ResetTimeoutMs = f.Budget.CircuitBreaker.ResetTimeoutMs
        }
        if f.Budget.CircuitBreaker.HalfOpenRequests > 0 {
            bc.CircuitBreaker.HalfOpenRequests = f.Budget.CircuitBreaker.HalfOpenRequests
        }
        if f.Budget.RateLimit.Requests > 0 {
            bc.RateLimit.Requests = f.Budget.RateLimit.Requests
        }
        if f.Budget.RateLimit.IntervalMs > 0 {
            bc.RateLimit.IntervalMs = f.Budget.RateLimit.IntervalMs
        }
    }

    // env overrides
    if v := os.Getenv("BACKPRESSURE_THRESHOLD"); v != "" {
        var x float64
        _, _ = fmt.Sscanf(v, "%f", &x)
        if x > 0 {
            bc.Backpressure.Threshold = x
        }
    }
    if v := os.Getenv("MAX_BACKPRESSURE_DELAY_MS"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.Backpressure.MaxDelayMs = x
        }
    }
    if v := os.Getenv("CIRCUIT_FAILURE_THRESHOLD"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.CircuitBreaker.FailureThreshold = x
        }
    }
    if v := os.Getenv("CIRCUIT_RESET_TIMEOUT_MS"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.CircuitBreaker.ResetTimeoutMs = x
        }
    }
    if v := os.Getenv("CIRCUIT_HALF_OPEN_REQUESTS"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.CircuitBreaker.HalfOpenRequests = x
        }
    }
    if v := os.Getenv("RATE_LIMIT_REQUESTS"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.RateLimit.Requests = x
        }
    }
    if v := os.Getenv("RATE_LIMIT_INTERVAL_MS"); v != "" {
        var x int
        _, _ = fmt.Sscanf(v, "%d", &x)
        if x > 0 {
            bc.RateLimit.IntervalMs = x
        }
    }

    return bc
}

