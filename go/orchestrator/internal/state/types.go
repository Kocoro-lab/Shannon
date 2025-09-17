package state

import (
	"fmt"
	"time"
)

// AgentState represents the complete state of an agent execution
type AgentState struct {
	Query               string                 `json:"query" validate:"required,min=1"`
	Context             map[string]interface{} `json:"context"`
	PlanningState       PlanningState          `json:"planning_state"`
	ExecutionState      ExecutionState         `json:"execution_state"`
	BeliefState         BeliefState            `json:"belief_state"`
	ToolResults         []ToolResult           `json:"tool_results"`
	IntermediateResults []string               `json:"intermediate_results"`
	Errors              []ErrorRecord          `json:"errors"`
}

// PlanningState represents the planning phase state
type PlanningState struct {
	CurrentStep    int         `json:"current_step" validate:"min=0"`
	TotalSteps     int         `json:"total_steps" validate:"min=1"`
	Plan           []string    `json:"plan"`
	Completed      []bool      `json:"completed"`
	StepStartTimes []time.Time `json:"step_start_times"`
	StepEndTimes   []time.Time `json:"step_end_times"`
}

// ExecutionState represents the execution phase state
type ExecutionState struct {
	Status         string    `json:"status" validate:"oneof=pending running completed failed paused"`
	StartTime      time.Time `json:"start_time"`
	LastUpdateTime time.Time `json:"last_update_time"`
	ErrorCount     int       `json:"error_count" validate:"min=0,max=10"`
	RetryCount     int       `json:"retry_count" validate:"min=0,max=3"`
	PauseReason    string    `json:"pause_reason,omitempty"`
}

// BeliefState represents the agent's belief and confidence state
type BeliefState struct {
	Confidence     float64                `json:"confidence" validate:"min=0,max=1"`
	Assumptions    []string               `json:"assumptions"`
	KnowledgeGaps  []string               `json:"knowledge_gaps"`
	UpdatedBeliefs map[string]interface{} `json:"updated_beliefs"`
	Hypotheses     []Hypothesis           `json:"hypotheses"`
}

// Hypothesis represents a working hypothesis
type Hypothesis struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence" validate:"min=0,max=1"`
	Evidence    []string  `json:"evidence"`
	Status      string    `json:"status" validate:"oneof=active testing confirmed rejected"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ToolName      string    `json:"tool_name" validate:"required"`
	Input         string    `json:"input"`
	Output        string    `json:"output"`
	Success       bool      `json:"success"`
	ExecutionTime int64     `json:"execution_time_ms" validate:"min=0"`
	TokensUsed    int       `json:"tokens_used" validate:"min=0"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// ErrorRecord represents an error that occurred during execution
type ErrorRecord struct {
	Timestamp    time.Time              `json:"timestamp"`
	ErrorType    string                 `json:"error_type"`
	ErrorMessage string                 `json:"error_message"`
	Context      map[string]interface{} `json:"context"`
	Recoverable  bool                   `json:"recoverable"`
}

// Validate validates the AgentState
func (as *AgentState) Validate() error {
	if as.Query == "" {
		return fmt.Errorf("query cannot be empty")
	}

	if as.PlanningState.TotalSteps < as.PlanningState.CurrentStep {
		return fmt.Errorf("current step (%d) cannot exceed total steps (%d)",
			as.PlanningState.CurrentStep, as.PlanningState.TotalSteps)
	}

	if as.ExecutionState.Status == "completed" && as.PlanningState.CurrentStep < as.PlanningState.TotalSteps {
		return fmt.Errorf("execution marked complete but steps remaining (%d/%d)",
			as.PlanningState.CurrentStep, as.PlanningState.TotalSteps)
	}

	if as.BeliefState.Confidence < 0 || as.BeliefState.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got %f", as.BeliefState.Confidence)
	}

	if as.ExecutionState.ErrorCount > 10 {
		return fmt.Errorf("error count exceeds maximum allowed (10)")
	}

	return nil
}

// IsComplete checks if the execution is complete
func (as *AgentState) IsComplete() bool {
	return as.ExecutionState.Status == "completed" ||
		as.ExecutionState.Status == "failed"
}

// CanRetry checks if the execution can be retried
func (as *AgentState) CanRetry() bool {
	return as.ExecutionState.Status == "failed" &&
		as.ExecutionState.RetryCount < 3
}

// AddToolResult adds a tool execution result
func (as *AgentState) AddToolResult(result ToolResult) {
	as.ToolResults = append(as.ToolResults, result)
	as.ExecutionState.LastUpdateTime = time.Now()
}

// AddError adds an error record
func (as *AgentState) AddError(err ErrorRecord) {
	as.Errors = append(as.Errors, err)
	as.ExecutionState.ErrorCount++
	as.ExecutionState.LastUpdateTime = time.Now()
}

// UpdateStep updates the current planning step
func (as *AgentState) UpdateStep(step int, completed bool) error {
	if step >= as.PlanningState.TotalSteps {
		return fmt.Errorf("step %d exceeds total steps %d", step, as.PlanningState.TotalSteps)
	}

	as.PlanningState.CurrentStep = step
	if step < len(as.PlanningState.Completed) {
		as.PlanningState.Completed[step] = completed
	}

	as.ExecutionState.LastUpdateTime = time.Now()
	return nil
}

// GetTotalTokensUsed calculates total tokens used across all tools
func (as *AgentState) GetTotalTokensUsed() int {
	total := 0
	for _, result := range as.ToolResults {
		total += result.TokensUsed
	}
	return total
}

// GetExecutionDuration calculates the total execution duration
func (as *AgentState) GetExecutionDuration() time.Duration {
	if as.ExecutionState.StartTime.IsZero() {
		return 0
	}

	endTime := as.ExecutionState.LastUpdateTime
	if endTime.IsZero() {
		endTime = time.Now()
	}

	return endTime.Sub(as.ExecutionState.StartTime)
}

// Reset resets the state for a retry
func (as *AgentState) Reset() {
	as.ExecutionState.Status = "pending"
	as.ExecutionState.ErrorCount = 0
	as.ExecutionState.RetryCount++
	as.PlanningState.CurrentStep = 0
	as.PlanningState.Completed = make([]bool, as.PlanningState.TotalSteps)
	as.ToolResults = []ToolResult{}
	as.IntermediateResults = []string{}
	as.Errors = []ErrorRecord{}
	as.ExecutionState.StartTime = time.Time{}
	as.ExecutionState.LastUpdateTime = time.Now()
}
