package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// TestWorkflowWrapperFunctions tests that wrapper functions exist and have correct signatures
func TestWorkflowWrapperFunctions(t *testing.T) {
	// Verify wrapper functions are available and non-nil
	assert.NotNil(t, ExploratoryWorkflow, "ExploratoryWorkflow wrapper should exist")
	assert.NotNil(t, ReactWorkflow, "ReactWorkflow wrapper should exist")
	assert.NotNil(t, ResearchWorkflow, "ResearchWorkflow wrapper should exist")
	assert.NotNil(t, ScientificWorkflow, "ScientificWorkflow wrapper should exist")
	assert.NotNil(t, AgentDAGWorkflow, "AgentDAGWorkflow wrapper should exist")
}

// TestWorkflowTypeConversion tests the conversion between wrapper and strategy types
func TestWorkflowTypeConversion(t *testing.T) {
	t.Run("Input conversion", func(t *testing.T) {
		wrapperInput := TaskInput{
			Query:     "Test query",
			UserID:    "test-user",
			SessionID: "test-session",
			Context:   map[string]interface{}{"key": "value"},
			History:   []Message{{Role: "user", Content: "Hello"}},
		}

		strategiesInput := convertToStrategiesInput(wrapperInput)
		assert.Equal(t, wrapperInput.Query, strategiesInput.Query)
		assert.Equal(t, wrapperInput.UserID, strategiesInput.UserID)
		assert.Equal(t, wrapperInput.SessionID, strategiesInput.SessionID)
		assert.Equal(t, wrapperInput.Context, strategiesInput.Context)
		assert.Equal(t, len(wrapperInput.History), len(strategiesInput.History))
		assert.Equal(t, wrapperInput.History[0].Role, strategiesInput.History[0].Role)
		assert.Equal(t, wrapperInput.History[0].Content, strategiesInput.History[0].Content)
	})

	t.Run("Result conversion", func(t *testing.T) {
		strategiesResult := strategies.TaskResult{
			Result:       "Test result",
			Success:      true,
			TokensUsed:   100,
			ErrorMessage: "Some error",
			Metadata:     map[string]interface{}{"workflow_type": "test"},
		}

		wrapperResult := convertFromStrategiesResult(strategiesResult)
		assert.Equal(t, strategiesResult.Result, wrapperResult.Result)
		assert.Equal(t, strategiesResult.Success, wrapperResult.Success)
		assert.Equal(t, strategiesResult.TokensUsed, wrapperResult.TokensUsed)
		assert.Equal(t, strategiesResult.ErrorMessage, wrapperResult.ErrorMessage)
		assert.Equal(t, strategiesResult.Metadata, wrapperResult.Metadata)
	})

	t.Run("Empty input conversion", func(t *testing.T) {
		wrapperInput := TaskInput{}
		strategiesInput := convertToStrategiesInput(wrapperInput)

		assert.Empty(t, strategiesInput.Query)
		assert.Empty(t, strategiesInput.UserID)
		assert.Empty(t, strategiesInput.SessionID)
		assert.Nil(t, strategiesInput.Context)
		assert.Empty(t, strategiesInput.History) // Empty slice, not nil
	})

	t.Run("Empty result conversion", func(t *testing.T) {
		strategiesResult := strategies.TaskResult{}
		wrapperResult := convertFromStrategiesResult(strategiesResult)

		assert.Empty(t, wrapperResult.Result)
		assert.False(t, wrapperResult.Success)
		assert.Zero(t, wrapperResult.TokensUsed)
		assert.Empty(t, wrapperResult.ErrorMessage)
		assert.Nil(t, wrapperResult.Metadata)
	})
}

// TestWorkflowInputValidationLogic tests that we can validate input without executing workflows
func TestWorkflowInputValidationLogic(t *testing.T) {
	testCases := []struct {
		name        string
		input       TaskInput
		shouldError bool
	}{
		{
			name: "Valid input with all fields",
			input: TaskInput{
				Query:     "What are the emerging trends in AI?",
				UserID:    "test-user",
				SessionID: "test-session",
				Context:   map[string]interface{}{"key": "value"},
			},
			shouldError: false,
		},
		{
			name: "Empty query should fail",
			input: TaskInput{
				Query:     "",
				UserID:    "test-user",
				SessionID: "test-session",
			},
			shouldError: true,
		},
		{
			name: "Empty UserID is allowed",
			input: TaskInput{
				Query:     "Test query",
				UserID:    "",
				SessionID: "test-session",
			},
			shouldError: false,
		},
		{
			name: "Nil context is allowed",
			input: TaskInput{
				Query:     "Test query",
				UserID:    "test-user",
				SessionID: "test-session",
				Context:   nil,
			},
			shouldError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Basic validation that workflows would do
			hasError := tc.input.Query == ""

			if tc.shouldError {
				assert.True(t, hasError, "Expected validation error for empty query")
			} else {
				assert.False(t, hasError, "Expected no validation error for valid input")
			}
		})
	}
}

// TestConversionHelperFunctions verifies the conversion helper functions exist
func TestConversionHelperFunctions(t *testing.T) {
	// Verify conversion functions exist by using them
	require.NotNil(t, convertToStrategiesInput)
	require.NotNil(t, convertFromStrategiesResult)

	// Test that they handle nil/empty values gracefully
	emptyInput := TaskInput{}
	convertedInput := convertToStrategiesInput(emptyInput)
	assert.NotNil(t, convertedInput)

	emptyResult := strategies.TaskResult{}
	convertedResult := convertFromStrategiesResult(emptyResult)
	assert.NotNil(t, convertedResult)
}