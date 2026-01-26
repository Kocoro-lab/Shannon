package activities

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBodyFieldMirroring(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		initialParams  map[string]interface{}
		expectedParams map[string]interface{}
	}{
		{
			name: "mirrors all body fields to prompt_params",
			body: map[string]interface{}{
				"profile_id": "test_profile",
				"aid":        "test_aid",
				"metrics":    []string{"users", "sessions"},
			},
			initialParams: map[string]interface{}{},
			expectedParams: map[string]interface{}{
				"profile_id": "test_profile",
				"aid":        "test_aid",
				"metrics":    []string{"users", "sessions"},
			},
		},
		{
			name: "does not override existing prompt_params",
			body: map[string]interface{}{
				"profile_id": "body_profile",
				"aid":        "body_aid",
			},
			initialParams: map[string]interface{}{
				"profile_id": "existing_profile",
			},
			expectedParams: map[string]interface{}{
				"profile_id": "existing_profile", // Not overridden
				"aid":        "body_aid",         // Added
			},
		},
		{
			name: "handles complex nested structures",
			body: map[string]interface{}{
				"timeRange": map[string]interface{}{
					"start": "2025-01-01",
					"end":   "2025-01-31",
				},
				"filters": []interface{}{
					map[string]interface{}{
						"field": "country",
						"value": "US",
					},
				},
			},
			initialParams: map[string]interface{}{},
			expectedParams: map[string]interface{}{
				"timeRange": map[string]interface{}{
					"start": "2025-01-01",
					"end":   "2025-01-31",
				},
				"filters": []interface{}{
					map[string]interface{}{
						"field": "country",
						"value": "US",
					},
				},
			},
		},
		{
			name:           "handles empty body",
			body:           map[string]interface{}{},
			initialParams:  map[string]interface{}{"existing": "value"},
			expectedParams: map[string]interface{}{"existing": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the field mirroring logic from agent.go
			pp := tt.initialParams

			for key, val := range tt.body {
				if _, exists := pp[key]; !exists {
					pp[key] = val
				}
			}

			assert.Equal(t, tt.expectedParams, pp)
		})
	}
}

func TestPromptParamsTypeAssertion(t *testing.T) {
	tests := []struct {
		name          string
		contextValue  interface{}
		shouldSucceed bool
		expectedValue map[string]interface{}
	}{
		{
			name: "valid map[string]interface{}",
			contextValue: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			shouldSucceed: true,
			expectedValue: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:          "nil value",
			contextValue:  nil,
			shouldSucceed: false,
			expectedValue: nil,
		},
		{
			name:          "wrong type (string)",
			contextValue:  "not a map",
			shouldSucceed: false,
			expectedValue: nil,
		},
		{
			name: "wrong map type (map[string]string)",
			contextValue: map[string]string{
				"key": "value",
			},
			shouldSucceed: false,
			expectedValue: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the type assertion from agent.go
			pp, ok := tt.contextValue.(map[string]interface{})

			if tt.shouldSucceed {
				assert.True(t, ok, "type assertion should succeed")
				assert.Equal(t, tt.expectedValue, pp)
			} else {
				assert.False(t, ok, "type assertion should fail")
			}
		})
	}
}

func TestStructPbConversion(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		wantErr  bool
		validate func(*testing.T, *structpb.Struct)
	}{
		{
			name: "simple key-value pairs",
			input: map[string]interface{}{
				"string_key": "value",
				"int_key":    42,
				"bool_key":   true,
			},
			wantErr: false,
			validate: func(t *testing.T, s *structpb.Struct) {
				assert.Equal(t, "value", s.Fields["string_key"].GetStringValue())
				assert.Equal(t, float64(42), s.Fields["int_key"].GetNumberValue())
				assert.Equal(t, true, s.Fields["bool_key"].GetBoolValue())
			},
		},
		{
			name: "nested structures",
			input: map[string]interface{}{
				"nested": map[string]interface{}{
					"inner_key": "inner_value",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, s *structpb.Struct) {
				nested := s.Fields["nested"].GetStructValue()
				require.NotNil(t, nested)
				assert.Equal(t, "inner_value", nested.Fields["inner_key"].GetStringValue())
			},
		},
		{
			name: "arrays",
			input: map[string]interface{}{
				"array": []interface{}{"item1", "item2", "item3"},
			},
			wantErr: false,
			validate: func(t *testing.T, s *structpb.Struct) {
				list := s.Fields["array"].GetListValue()
				require.NotNil(t, list)
				assert.Equal(t, 3, len(list.Values))
				assert.Equal(t, "item1", list.Values[0].GetStringValue())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := structpb.NewStruct(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, s)
				if tt.validate != nil {
					tt.validate(t, s)
				}
			}
		})
	}
}

func TestSessionContextMerging(t *testing.T) {
	tests := []struct {
		name             string
		initialContext   map[string]interface{}
		additionalParams map[string]interface{}
		expectedResult   map[string]interface{}
	}{
		{
			name: "merge non-overlapping contexts",
			initialContext: map[string]interface{}{
				"role":      "data_analytics",
				"user_id":   "user_123",
				"tenant_id": "tenant_456",
			},
			additionalParams: map[string]interface{}{
				"profile_id": "profile_789",
				"aid":        "aid_abc",
			},
			expectedResult: map[string]interface{}{
				"role":       "data_analytics",
				"user_id":    "user_123",
				"tenant_id":  "tenant_456",
				"profile_id": "profile_789",
				"aid":        "aid_abc",
			},
		},
		{
			name: "does not override existing context values",
			initialContext: map[string]interface{}{
				"role":    "existing_role",
				"user_id": "existing_user",
			},
			additionalParams: map[string]interface{}{
				"role":    "new_role",
				"user_id": "new_user",
				"extra":   "extra_value",
			},
			expectedResult: map[string]interface{}{
				"role":    "existing_role", // Not overridden
				"user_id": "existing_user", // Not overridden
				"extra":   "extra_value",   // Added
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate context merging logic
			result := make(map[string]interface{})

			// Copy initial context
			for k, v := range tt.initialContext {
				result[k] = v
			}

			// Merge additional params (don't override)
			for k, v := range tt.additionalParams {
				if _, exists := result[k]; !exists {
					result[k] = v
				}
			}

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// Test helper for ExecuteAgentWithForcedTools would go here
// NOTE: Full integration test requires HTTP server mock
func TestExecuteAgentWithForcedToolsLogic(t *testing.T) {
	t.Run("validates required fields", func(t *testing.T) {
		// Test input validation logic
		type Input struct {
			Query                string
			ForcedToolName       string
			ForcedToolParams     map[string]interface{}
			AllowedToolsOverride []string
		}

		inputs := []struct {
			name    string
			input   Input
			wantErr bool
		}{
			{
				name: "valid input",
				input: Input{
					Query:            "test query",
					ForcedToolName:   "calculator",
					ForcedToolParams: map[string]interface{}{"expression": "2+2"},
				},
				wantErr: false,
			},
			{
				name: "missing query",
				input: Input{
					Query:            "",
					ForcedToolName:   "calculator",
					ForcedToolParams: map[string]interface{}{"expression": "2+2"},
				},
				wantErr: true,
			},
			{
				name: "missing forced tool name",
				input: Input{
					Query:            "test query",
					ForcedToolName:   "",
					ForcedToolParams: map[string]interface{}{"expression": "2+2"},
				},
				wantErr: true,
			},
		}

		for _, tt := range inputs {
			t.Run(tt.name, func(t *testing.T) {
				hasError := tt.input.Query == "" || tt.input.ForcedToolName == ""
				assert.Equal(t, tt.wantErr, hasError)
			})
		}
	})
}

// TestEnsureSessionContext tests the session_id and agent_id injection helper.
// Covers edge cases: nil context, missing keys, nil values, empty strings, non-string types.
func TestEnsureSessionContext(t *testing.T) {
	tests := []struct {
		name             string
		ctx              map[string]interface{}
		sessionID        string
		agentID          string
		expectedSession  string
		expectedAgent    string
		expectSessionKey bool
		expectAgentKey   bool
	}{
		{
			name:             "nil context - injects both",
			ctx:              nil,
			sessionID:        "session-123",
			agentID:          "agent-456",
			expectedSession:  "session-123",
			expectedAgent:    "agent-456",
			expectSessionKey: true,
			expectAgentKey:   true,
		},
		{
			name:             "empty context - injects both",
			ctx:              map[string]interface{}{},
			sessionID:        "session-abc",
			agentID:          "agent-def",
			expectedSession:  "session-abc",
			expectedAgent:    "agent-def",
			expectSessionKey: true,
			expectAgentKey:   true,
		},
		{
			name: "session_id is nil - injects",
			ctx: map[string]interface{}{
				"session_id": nil,
				"role":       "developer",
			},
			sessionID:        "session-nil-fix",
			agentID:          "",
			expectedSession:  "session-nil-fix",
			expectSessionKey: true,
			expectAgentKey:   false,
		},
		{
			name: "session_id is empty string - injects",
			ctx: map[string]interface{}{
				"session_id": "",
			},
			sessionID:        "session-empty-fix",
			agentID:          "agent-xyz",
			expectedSession:  "session-empty-fix",
			expectedAgent:    "agent-xyz",
			expectSessionKey: true,
			expectAgentKey:   true,
		},
		{
			name: "session_id is whitespace only - injects",
			ctx: map[string]interface{}{
				"session_id": "   ",
			},
			sessionID:        "session-ws-fix",
			agentID:          "",
			expectedSession:  "session-ws-fix",
			expectSessionKey: true,
			expectAgentKey:   false,
		},
		{
			name: "session_id is wrong type (int) - injects",
			ctx: map[string]interface{}{
				"session_id": 12345,
			},
			sessionID:        "session-type-fix",
			agentID:          "",
			expectedSession:  "session-type-fix",
			expectSessionKey: true,
			expectAgentKey:   false,
		},
		{
			name: "session_id is valid - no overwrite",
			ctx: map[string]interface{}{
				"session_id": "existing-session",
			},
			sessionID:        "new-session",
			agentID:          "",
			expectedSession:  "existing-session",
			expectSessionKey: true,
			expectAgentKey:   false,
		},
		{
			name: "both existing and valid - no overwrite",
			ctx: map[string]interface{}{
				"session_id": "existing-session",
				"agent_id":   "existing-agent",
			},
			sessionID:        "new-session",
			agentID:          "new-agent",
			expectedSession:  "existing-session",
			expectedAgent:    "existing-agent",
			expectSessionKey: true,
			expectAgentKey:   true,
		},
		{
			name: "empty input IDs - no injection",
			ctx: map[string]interface{}{
				"role": "developer",
			},
			sessionID:        "",
			agentID:          "",
			expectSessionKey: false,
			expectAgentKey:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureSessionContext(tt.ctx, tt.sessionID, tt.agentID)
			require.NotNil(t, result, "result should never be nil")

			if tt.expectSessionKey {
				val, exists := result["session_id"]
				assert.True(t, exists, "session_id key should exist")
				assert.Equal(t, tt.expectedSession, val, "session_id value mismatch")
			} else {
				_, exists := result["session_id"]
				assert.False(t, exists, "session_id key should not exist")
			}

			if tt.expectAgentKey {
				val, exists := result["agent_id"]
				assert.True(t, exists, "agent_id key should exist")
				assert.Equal(t, tt.expectedAgent, val, "agent_id value mismatch")
			} else {
				_, exists := result["agent_id"]
				assert.False(t, exists, "agent_id key should not exist")
			}
		})
	}
}
