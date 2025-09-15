package activities

// ComplexityAnalysisInput is the input for complexity analysis
type ComplexityAnalysisInput struct {
    Query   string
    Context map[string]interface{}
}

// ComplexityAnalysisResult is the result of complexity analysis
type ComplexityAnalysisResult struct {
    ComplexityScore float64
    Mode            string
    Subtasks        []Subtask
}

// Subtask represents a decomposed subtask
type Subtask struct {
    ID              string
    Description     string
    Dependencies    []string
    EstimatedTokens int
    // Plan IO (optional, plan_io_v1): topics produced/consumed by this subtask
    Produces        []string
    Consumes        []string
    // LLM-native tool selection
    SuggestedTools  []string               `json:"suggested_tools"`
    ToolParameters  map[string]interface{} `json:"tool_parameters"`
    // Persona assignment for specialized agent behavior
    SuggestedPersona string                `json:"suggested_persona"`
}

// AgentExecutionInput is the input for agent execution
type AgentExecutionInput struct {
    Query          string
    AgentID        string
    Context        map[string]interface{}
    Mode           string
    SessionID      string   // Session identifier
    History        []string // Conversation history
    // LLM-native tool selection
    SuggestedTools []string               `json:"suggested_tools"`
    ToolParameters map[string]interface{} `json:"tool_parameters"`
    // Persona for specialized agent behavior
    PersonaID      string                 `json:"persona_id"`
}

// AgentExecutionResult is the result of agent execution
type AgentExecutionResult struct {
    AgentID    string
    Response   string
    TokensUsed int
    ModelUsed  string
    DurationMs int64
    Success    bool
    Error      string
    // Tools used and their outputs (when applicable)
    ToolsUsed      []string        `json:"tools_used,omitempty"`
    ToolExecutions []ToolExecution `json:"tool_executions,omitempty"`
}

// ToolExecution summarizes a single tool invocation result returned by Agent-Core
type ToolExecution struct {
    Tool    string      `json:"tool"`
    Success bool        `json:"success"`
    Output  interface{} `json:"output,omitempty"`
    Error   string      `json:"error,omitempty"`
}

// SynthesisInput is the input for result synthesis
type SynthesisInput struct {
    Query        string
    AgentResults []AgentExecutionResult
    Context      map[string]interface{} // Optional context for synthesis
}

// SynthesisResult is the result of synthesis
type SynthesisResult struct {
    FinalResult string
    TokensUsed  int
}

// EvaluateResultInput carries data for reflection/quality checks
type EvaluateResultInput struct {
    Query    string
    Response string
    Criteria []string
}

// EvaluateResultOutput returns a simple quality score and feedback
type EvaluateResultOutput struct {
    Score    float64
    Feedback string
}

// SessionUpdateInput is the input for updating session
type SessionUpdateInput struct {
    SessionID  string
    Result     string
    TokensUsed int
    AgentsUsed int
    CostUSD    float64
}

// SessionUpdateResult is the result of session update
type SessionUpdateResult struct {
    Success bool
    Error   string
}
