package templates

// NodeType enumerates supported template node types.
type NodeType string

const (
	NodeTypeSimple     NodeType = "simple"
	NodeTypeDAG        NodeType = "dag"
	NodeTypeSupervisor NodeType = "supervisor"
	NodeTypeCognitive  NodeType = "cognitive"
)

// StrategyType enumerates supported execution strategies within templates.
type StrategyType string

const (
	StrategyReact          StrategyType = "react"
	StrategyChainOfThought StrategyType = "chain_of_thought"
	StrategyTreeOfThoughts StrategyType = "tree_of_thoughts"
	StrategyDebate         StrategyType = "debate"
	StrategyReflection     StrategyType = "reflection"
)

// Template captures the raw user-defined workflow structure.
type Template struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Version     string           `yaml:"version"`
	Extends     []string         `yaml:"extends"`
	Defaults    TemplateDefaults `yaml:"defaults"`
	Nodes       []TemplateNode   `yaml:"nodes"`
	Edges       []TemplateEdge   `yaml:"edges"`
	Metadata    map[string]any   `yaml:"metadata"`
}

// TemplateDefaults define shared knobs applied to nodes when individual values are absent.
type TemplateDefaults struct {
	ModelTier       string `yaml:"model_tier"`
	BudgetAgentMax  int    `yaml:"budget_agent_max"`
	RequireApproval *bool  `yaml:"require_approval"`
}

// TemplateNodeFailure specifies backoff behaviour when a node fails.
type TemplateNodeFailure struct {
	DegradeTo  StrategyType `yaml:"degrade_to"`
	Retry      int          `yaml:"retry"`
	EscalateTo NodeType     `yaml:"escalate_to"`
}

// TemplateNode defines a workflow node within a template.
type TemplateNode struct {
	ID             string               `yaml:"id"`
	Type           NodeType             `yaml:"type"`
	Strategy       StrategyType         `yaml:"strategy"`
	DependsOn      []string             `yaml:"depends_on"`
	BudgetMax      *int                 `yaml:"budget_max"`
	ToolsAllowlist []string             `yaml:"tools_allowlist"`
	OnFail         *TemplateNodeFailure `yaml:"on_fail"`
	Metadata       map[string]any       `yaml:"metadata"`
}

// TemplateEdge expresses an explicit edge in the workflow graph.
type TemplateEdge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// NodeByID returns a pointer to the node with the supplied ID, if present.
func (t *Template) NodeByID(id string) *TemplateNode {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			return &t.Nodes[i]
		}
	}
	return nil
}
