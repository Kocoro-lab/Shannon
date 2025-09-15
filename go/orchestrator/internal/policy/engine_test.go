package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestOPAEngine_Basic(t *testing.T) {
	// Create temporary directory for test policies
	tempDir, err := os.MkdirTemp("", "opa_test_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a simple test policy
	testPolicy := `package shannon.task

default decision := {
    "allow": false,
    "reason": "default deny"
}

decision := {
    "allow": true,
    "reason": "test environment allowed"
} {
    input.environment == "test"
    input.mode == "simple"
}
`

	policyPath := filepath.Join(tempDir, "test.rego")
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0644); err != nil {
		t.Fatalf("Failed to write test policy: %v", err)
	}

	// Create engine configuration
	config := &Config{
		Enabled:     true,
		Mode:        ModeEnforce,
		Path:        tempDir,
		FailClosed:  false,
		Environment: "test",
	}

	logger := zaptest.NewLogger(t)
	engine, err := NewOPAEngine(config, logger)
	if err != nil {
		t.Fatalf("Failed to create OPA engine: %v", err)
	}

	if !engine.IsEnabled() {
		t.Fatal("Engine should be enabled")
	}

	// Test cases
	tests := []struct {
		name     string
		input    *PolicyInput
		expected bool
	}{
		{
			name: "allowed_request",
			input: &PolicyInput{
				SessionID:   "test-session",
				AgentID:     "test-agent",
				Query:       "what is the weather?",
				Mode:        "simple",
				Environment: "test",
				Timestamp:   time.Now(),
			},
			expected: true,
		},
		{
			name: "denied_request_wrong_env",
			input: &PolicyInput{
				SessionID:   "test-session", 
				AgentID:     "test-agent",
				Query:       "what is the weather?",
				Mode:        "simple",
				Environment: "prod", // Wrong environment
				Timestamp:   time.Now(),
			},
			expected: false,
		},
		{
			name: "denied_request_wrong_mode",
			input: &PolicyInput{
				SessionID:   "test-session",
				AgentID:     "test-agent", 
				Query:       "what is the weather?",
				Mode:        "complex", // Wrong mode
				Environment: "test",
				Timestamp:   time.Now(),
			},
			expected: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := engine.Evaluate(ctx, tt.input)
			if err != nil {
				t.Fatalf("Evaluation failed: %v", err)
			}

			if decision.Allow != tt.expected {
				t.Errorf("Expected allow=%v, got allow=%v, reason=%s", 
					tt.expected, decision.Allow, decision.Reason)
			}

			// Check that reason is provided
			if decision.Reason == "" {
				t.Error("Decision should include a reason")
			}

			t.Logf("Decision: allow=%v, reason=%s", decision.Allow, decision.Reason)
		})
	}
}

func TestOPAEngine_DryRunMode(t *testing.T) {
	// Create temporary directory for test policies
	tempDir, err := os.MkdirTemp("", "opa_test_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a simple deny-all policy for testing
	testPolicy := `package shannon.task

default decision := {
    "allow": false,
    "reason": "deny all for testing"
}
`

	policyPath := filepath.Join(tempDir, "deny.rego")
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0644); err != nil {
		t.Fatalf("Failed to write test policy: %v", err)
	}

	// Create engine in dry-run mode
	config := &Config{
		Enabled:     true,
		Mode:        ModeDryRun,
		Path:        tempDir,
		FailClosed:  false,
		Environment: "test",
	}

	logger := zaptest.NewLogger(t)
	engine, err := NewOPAEngine(config, logger)
	if err != nil {
		t.Fatalf("Failed to create OPA engine: %v", err)
	}

	// In dry-run mode, engine should still evaluate but not enforce
	input := &PolicyInput{
		SessionID:   "test-session",
		AgentID:     "test-agent",
		Query:       "test query",
		Mode:        "simple",
		Environment: "test",
		Timestamp:   time.Now(),
	}

	ctx := context.Background()
	decision, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluation failed: %v", err)
	}

	// In dry-run mode, should always allow but with a reason indicating what would have happened
	if !decision.Allow {
		t.Error("Expected dry-run mode to allow request")
	}

	if !strings.Contains(decision.Reason, "DRY-RUN") {
		t.Errorf("Expected dry-run reason prefix, got: %s", decision.Reason)
	}

	t.Logf("Dry-run decision: allow=%v, reason=%s", decision.Allow, decision.Reason)
}

func TestLoadConfigFromShannon(t *testing.T) {
	// Test Shannon config parsing
	shannonPolicy := map[string]interface{}{
		"enabled":     true,
		"mode":        "enforce",
		"path":        "/test/path",
		"fail_closed": true,
		"environment": "prod",
	}

	config := LoadConfigFromShannon(shannonPolicy)

	if !config.Enabled {
		t.Error("Expected policy to be enabled")
	}
	if config.Mode != ModeEnforce {
		t.Errorf("Expected mode to be %s, got %s", ModeEnforce, config.Mode)
	}
	if config.Path != "/test/path" {
		t.Errorf("Expected path to be /test/path, got %s", config.Path)
	}
	if !config.FailClosed {
		t.Error("Expected fail_closed to be true")
	}
	if config.Environment != "prod" {
		t.Errorf("Expected environment to be prod, got %s", config.Environment)
	}
}

func TestLoadConfigFromShannon_InvalidMode(t *testing.T) {
	// Test invalid mode handling
	shannonPolicy := map[string]interface{}{
		"enabled": true,
		"mode":    "invalid_mode",
		"path":    "/test/path",
	}

	config := LoadConfigFromShannon(shannonPolicy)

	// Should default to off mode and disable engine
	if config.Mode != ModeOff {
		t.Errorf("Expected mode to default to %s, got %s", ModeOff, config.Mode)
	}
	if config.Enabled {
		t.Error("Expected engine to be disabled with invalid mode")
	}
}

func TestDenyPrecedence(t *testing.T) {
	// Create temporary directory for test policies
	tempDir, err := os.MkdirTemp("", "opa_deny_test_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create policy with both allow and deny rules
	testPolicy := `package shannon.task

default decision := {
    "allow": false,
    "reason": "default deny"
}

# Deny precedence - deny rules override allows
decision := {
    "allow": false,
    "reason": reason,
    "require_approval": false
} {
    some reason
    deny[reason]
} else := {
    "allow": allow,
    "reason": reason,
    "require_approval": false
} {
    allow := allow_rules[_].allow
    reason := allow_rules[_].reason
}

# Allow rules
allow_rules[{"allow": true, "reason": "simple mode allowed"}] {
    input.mode == "simple"
    count(deny) == 0  # Only if no deny rules match
}

# Deny rule for dangerous queries (should override allows)
deny["dangerous query pattern detected"] {
    contains(lower(input.query), "dangerous")
}
`

	policyPath := filepath.Join(tempDir, "deny_precedence.rego")
	if err := os.WriteFile(policyPath, []byte(testPolicy), 0644); err != nil {
		t.Fatalf("Failed to write test policy: %v", err)
	}

	config := &Config{
		Enabled:     true,
		Mode:        ModeEnforce,
		Path:        tempDir,
		FailClosed:  false,
		Environment: "test",
	}

	logger := zaptest.NewLogger(t)
	engine, err := NewOPAEngine(config, logger)
	if err != nil {
		t.Fatalf("Failed to create OPA engine: %v", err)
	}

	ctx := context.Background()
	
	// Test 1: Allow rule should work when no deny rules match
	input1 := &PolicyInput{
		SessionID:   "test",
		AgentID:     "test",
		Query:       "what is the weather?", // Safe query
		Mode:        "simple",
		Environment: "test",
		Timestamp:   time.Now(),
	}
	
	decision1, err := engine.Evaluate(ctx, input1)
	if err != nil {
		t.Fatalf("Evaluation failed: %v", err)
	}
	
	if !decision1.Allow {
		t.Errorf("Expected allow=true for safe simple query, got allow=%v, reason=%s", 
			decision1.Allow, decision1.Reason)
	}
	
	// Test 2: Deny rule should override allow rule
	input2 := &PolicyInput{
		SessionID:   "test",
		AgentID:     "test", 
		Query:       "dangerous query here", // Triggers deny rule
		Mode:        "simple",              // Would normally be allowed
		Environment: "test",
		Timestamp:   time.Now(),
	}
	
	decision2, err := engine.Evaluate(ctx, input2)
	if err != nil {
		t.Fatalf("Evaluation failed: %v", err)
	}
	
	if decision2.Allow {
		t.Errorf("Expected deny precedence - allow=false, but got allow=%v, reason=%s",
			decision2.Allow, decision2.Reason)
	}
	
	if decision2.Reason != "dangerous query pattern detected" {
		t.Errorf("Expected deny reason, got: %s", decision2.Reason)
	}
	
	t.Logf("Deny precedence test passed - deny overrides allow")
	t.Logf("Allow case: %v (%s)", decision1.Allow, decision1.Reason)
	t.Logf("Deny case: %v (%s)", decision2.Allow, decision2.Reason)
}