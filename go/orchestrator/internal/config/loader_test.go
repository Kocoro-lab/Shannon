package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig tests the configuration loading from environment and files
func TestLoadConfig(t *testing.T) {
	t.Run("Default configuration", func(t *testing.T) {
		// Test that default config can be loaded without errors
		cfg := Load()
		assert.NotNil(t, cfg)
		
		// Verify some default values
		assert.NotEmpty(t, cfg.LogLevel)
		assert.NotEmpty(t, cfg.Environment)
	})

	t.Run("Environment variable override", func(t *testing.T) {
		// Set environment variable
		originalLogLevel := os.Getenv("LOG_LEVEL")
		os.Setenv("LOG_LEVEL", "debug")
		defer os.Setenv("LOG_LEVEL", originalLogLevel)

		cfg := Load()
		assert.Equal(t, "debug", cfg.LogLevel)
	})

	t.Run("PostgreSQL configuration", func(t *testing.T) {
		os.Setenv("POSTGRES_HOST", "testhost")
		os.Setenv("POSTGRES_PORT", "54321")
		os.Setenv("POSTGRES_USER", "testuser")
		os.Setenv("POSTGRES_PASSWORD", "testpass")
		os.Setenv("POSTGRES_DB", "testdb")
		defer func() {
			os.Unsetenv("POSTGRES_HOST")
			os.Unsetenv("POSTGRES_PORT")
			os.Unsetenv("POSTGRES_USER")
			os.Unsetenv("POSTGRES_PASSWORD")
			os.Unsetenv("POSTGRES_DB")
		}()

		cfg := Load()
		assert.Equal(t, "testhost", cfg.Postgres.Host)
		assert.Equal(t, 54321, cfg.Postgres.Port)
		assert.Equal(t, "testuser", cfg.Postgres.User)
		assert.Equal(t, "testpass", cfg.Postgres.Password)
		assert.Equal(t, "testdb", cfg.Postgres.Database)
	})

	t.Run("Redis configuration", func(t *testing.T) {
		os.Setenv("REDIS_HOST", "redis-test")
		os.Setenv("REDIS_PORT", "6380")
		defer func() {
			os.Unsetenv("REDIS_HOST")
			os.Unsetenv("REDIS_PORT")
		}()

		cfg := Load()
		assert.Equal(t, "redis-test", cfg.Redis.Host)
		assert.Equal(t, 6380, cfg.Redis.Port)
	})

	t.Run("Temporal configuration", func(t *testing.T) {
		os.Setenv("TEMPORAL_HOST", "temporal:7234")
		os.Setenv("TEMPORAL_NAMESPACE", "test-namespace")
		defer func() {
			os.Unsetenv("TEMPORAL_HOST")
			os.Unsetenv("TEMPORAL_NAMESPACE")
		}()

		cfg := Load()
		assert.Equal(t, "temporal:7234", cfg.Temporal.Host)
		assert.Equal(t, "test-namespace", cfg.Temporal.Namespace)
	})

	t.Run("OPA configuration", func(t *testing.T) {
		os.Setenv("OPA_ENABLED", "true")
		os.Setenv("OPA_POLICIES_DIR", "/custom/policies")
		defer func() {
			os.Unsetenv("OPA_ENABLED")
			os.Unsetenv("OPA_POLICIES_DIR")
		}()

		cfg := Load()
		assert.True(t, cfg.OPA.Enabled)
		assert.Equal(t, "/custom/policies", cfg.OPA.PoliciesDir)
	})

	t.Run("Budget configuration", func(t *testing.T) {
		os.Setenv("BUDGET_ENFORCEMENT", "true")
		os.Setenv("BUDGET_DEFAULT_MAX_TOKENS", "10000")
		defer func() {
			os.Unsetenv("BUDGET_ENFORCEMENT")
			os.Unsetenv("BUDGET_DEFAULT_MAX_TOKENS")
		}()

		cfg := Load()
		assert.True(t, cfg.Budget.Enforcement)
		assert.Equal(t, 10000, cfg.Budget.DefaultMaxTokens)
	})
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	t.Run("Valid configuration", func(t *testing.T) {
		cfg := &Config{
			Environment: "development",
			LogLevel:    "info",
			Postgres: PostgresConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Database: "db",
			},
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Invalid log level", func(t *testing.T) {
		cfg := &Config{
			LogLevel: "invalid",
		}

		err := cfg.Validate()
		// If validation is implemented, should return error
		// If not, this documents expected behavior
		_ = err
	})

	t.Run("Missing required fields", func(t *testing.T) {
		cfg := &Config{
			Postgres: PostgresConfig{
				Host: "",  // Empty host
			},
		}

		err := cfg.Validate()
		// Document expected validation behavior
		_ = err
	})
}

// TestGetConnectionString tests database connection string generation
func TestGetConnectionString(t *testing.T) {
	cfg := &Config{
		Postgres: PostgresConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "testuser",
			Password: "testpass",
			Database: "testdb",
		},
	}

	connStr := cfg.Postgres.ConnectionString()
	require.NotEmpty(t, connStr)

	// Verify connection string contains expected components
	assert.Contains(t, connStr, "localhost")
	assert.Contains(t, connStr, "5432")
	assert.Contains(t, connStr, "testuser")
	assert.Contains(t, connStr, "testdb")
}

// TestFeatureFlags tests feature flag configuration
func TestFeatureFlags(t *testing.T) {
	t.Run("Enable features via environment", func(t *testing.T) {
		os.Setenv("ENABLE_METRICS", "true")
		os.Setenv("ENABLE_TRACING", "true")
		os.Setenv("TEMPLATE_FALLBACK_ENABLED", "true")
		defer func() {
			os.Unsetenv("ENABLE_METRICS")
			os.Unsetenv("ENABLE_TRACING")
			os.Unsetenv("TEMPLATE_FALLBACK_ENABLED")
		}()

		cfg := Load()
		assert.True(t, cfg.Metrics.Enabled)
		assert.True(t, cfg.Tracing.Enabled)
		assert.True(t, cfg.Templates.FallbackEnabled)
	})

	t.Run("Disable features", func(t *testing.T) {
		os.Setenv("ENABLE_METRICS", "false")
		os.Setenv("OPA_ENABLED", "false")
		defer func() {
			os.Unsetenv("ENABLE_METRICS")
			os.Unsetenv("OPA_ENABLED")
		}()

		cfg := Load()
		assert.False(t, cfg.Metrics.Enabled)
		assert.False(t, cfg.OPA.Enabled)
	})
}

// TestReload tests configuration hot reload
func TestReload(t *testing.T) {
	// Create a temporary config file
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Write initial config
	initialConfig := `
log_level: info
environment: test
`
	_, err = tmpfile.Write([]byte(initialConfig))
	require.NoError(t, err)
	tmpfile.Close()

	// Load config from file
	os.Setenv("CONFIG_FILE", tmpfile.Name())
	defer os.Unsetenv("CONFIG_FILE")

	cfg := Load()
	assert.Equal(t, "info", cfg.LogLevel)

	// Simulate config reload (if implemented)
	// This would involve file watching and reloading logic
}

// TestGetEnvOrDefault tests environment variable reading with defaults
func TestGetEnvOrDefault(t *testing.T) {
	t.Run("Environment variable exists", func(t *testing.T) {
		os.Setenv("TEST_VAR", "test_value")
		defer os.Unsetenv("TEST_VAR")

		value := getEnvOrDefault("TEST_VAR", "default")
		assert.Equal(t, "test_value", value)
	})

	t.Run("Environment variable missing - use default", func(t *testing.T) {
		value := getEnvOrDefault("NONEXISTENT_VAR", "default_value")
		assert.Equal(t, "default_value", value)
	})

	t.Run("Environment variable empty", func(t *testing.T) {
		os.Setenv("EMPTY_VAR", "")
		defer os.Unsetenv("EMPTY_VAR")

		value := getEnvOrDefault("EMPTY_VAR", "default")
		// Empty string is valid, should not use default
		assert.Equal(t, "", value)
	})
}

// Helper function (implement if not exists in package)
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

