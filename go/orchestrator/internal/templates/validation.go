package templates

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationIssue captures a single validation failure with a stable code for metrics.
type ValidationIssue struct {
	Code    string
	Message string
}

// ValidationError aggregates template validation failures.
type ValidationError struct {
	Issues []ValidationIssue
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "template validation failed"
	}
	if len(e.Issues) == 1 {
		return e.Issues[0].Message
	}
	msgs := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		msgs[i] = issue.Message
	}
	return fmt.Sprintf("%d validation errors: %s", len(e.Issues), strings.Join(msgs, "; "))
}

// HasIssues reports whether any validation problems were captured.
func (e *ValidationError) HasIssues() bool {
	return e != nil && len(e.Issues) > 0
}

// Messages returns just the human-readable text for each issue.
func (e *ValidationError) Messages() []string {
	if e == nil {
		return nil
	}
	msgs := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		msgs[i] = issue.Message
	}
	return msgs
}

var (
	allowedNodeTypes = map[NodeType]struct{}{
		NodeTypeSimple:     {},
		NodeTypeDAG:        {},
		NodeTypeSupervisor: {},
		NodeTypeCognitive:  {},
	}
	allowedStrategies = map[StrategyType]struct{}{
		StrategyReact:          {},
		StrategyChainOfThought: {},
		StrategyTreeOfThoughts: {},
		StrategyDebate:         {},
		StrategyReflection:     {},
	}
)

// ValidateTemplate performs structural checks and returns a ValidationError when problems exist.
func ValidateTemplate(tpl *Template) error {
	if tpl == nil {
		return &ValidationError{Issues: []ValidationIssue{{Code: "template_nil", Message: "template is nil"}}}
	}

	var issues []ValidationIssue

	if strings.TrimSpace(tpl.Name) == "" {
		issues = append(issues, ValidationIssue{Code: "template_name_missing", Message: "template name is required"})
	}
	hasExtends := len(tpl.Extends) > 0
	if len(tpl.Nodes) == 0 && !hasExtends {
		issues = append(issues, ValidationIssue{Code: "template_nodes_empty", Message: "at least one node is required"})
	}
	if tpl.Defaults.BudgetAgentMax < 0 {
		issues = append(issues, ValidationIssue{Code: "defaults_budget_negative", Message: "defaults.budget_agent_max cannot be negative"})
	}

	nodes := make(map[string]*TemplateNode, len(tpl.Nodes))
	for i := range tpl.Nodes {
		node := &tpl.Nodes[i]
		if strings.TrimSpace(node.ID) == "" {
			issues = append(issues, ValidationIssue{Code: "node_id_missing", Message: fmt.Sprintf("node at index %d is missing an id", i)})
			continue
		}
		if _, exists := nodes[node.ID]; exists {
			issues = append(issues, ValidationIssue{Code: "node_id_duplicate", Message: fmt.Sprintf("duplicate node id '%s'", node.ID)})
			continue
		}
		nodes[node.ID] = node
	}

	for _, node := range nodes {
		if _, ok := allowedNodeTypes[node.Type]; !ok {
			issues = append(issues, ValidationIssue{Code: "node_type_unknown", Message: fmt.Sprintf("unknown node type '%s' at node '%s'", node.Type, node.ID)})
		}
		if node.Strategy != "" {
			if _, ok := allowedStrategies[node.Strategy]; !ok {
				issues = append(issues, ValidationIssue{Code: "strategy_unknown", Message: fmt.Sprintf("unknown strategy '%s' at node '%s'", node.Strategy, node.ID)})
			}
		}
		if node.BudgetMax != nil && *node.BudgetMax < 0 {
			issues = append(issues, ValidationIssue{Code: "budget_negative", Message: fmt.Sprintf("budget_max cannot be negative at node '%s'", node.ID)})
		}
		if node.BudgetMax != nil && tpl.Defaults.BudgetAgentMax > 0 && *node.BudgetMax > tpl.Defaults.BudgetAgentMax {
			issues = append(issues, ValidationIssue{Code: "budget_exceeds_default", Message: fmt.Sprintf("budget_max %d exceeds defaults.budget_agent_max %d at node '%s'", *node.BudgetMax, tpl.Defaults.BudgetAgentMax, node.ID)})
		}
		depSet := make(map[string]struct{}, len(node.DependsOn))
		for _, dep := range node.DependsOn {
			if dep == node.ID {
				issues = append(issues, ValidationIssue{Code: "dependency_self", Message: fmt.Sprintf("node '%s' cannot depend on itself", node.ID)})
				continue
			}
			if !hasExtends {
				if _, ok := nodes[dep]; !ok {
					issues = append(issues, ValidationIssue{Code: "dependency_unknown", Message: fmt.Sprintf("node '%s' depends on unknown node '%s'", node.ID, dep)})
					continue
				}
			}
			if _, dup := depSet[dep]; dup {
				continue
			}
			depSet[dep] = struct{}{}
		}
		if node.OnFail != nil {
			if node.OnFail.DegradeTo != "" {
				if _, ok := allowedStrategies[node.OnFail.DegradeTo]; !ok {
					issues = append(issues, ValidationIssue{Code: "on_fail_degrade_unknown", Message: fmt.Sprintf("on_fail.degrade_to '%s' at node '%s' is not a known strategy", node.OnFail.DegradeTo, node.ID)})
				}
			}
			if node.OnFail.Retry < 0 {
				issues = append(issues, ValidationIssue{Code: "on_fail_retry_negative", Message: fmt.Sprintf("on_fail.retry cannot be negative at node '%s'", node.ID)})
			}
			if node.OnFail.EscalateTo != "" {
				if _, ok := allowedNodeTypes[node.OnFail.EscalateTo]; !ok {
					issues = append(issues, ValidationIssue{Code: "on_fail_escalate_unknown", Message: fmt.Sprintf("on_fail.escalate_to '%s' at node '%s' is not a known node type", node.OnFail.EscalateTo, node.ID)})
				}
			}
		}
	}

	adjacency := make(map[string][]string, len(nodes))
	for id := range nodes {
		adjacency[id] = nil
	}

	if !hasExtends {
		for _, node := range nodes {
			for _, dep := range node.DependsOn {
				adjacency[dep] = append(adjacency[dep], node.ID)
			}
		}

		for i, edge := range tpl.Edges {
			if strings.TrimSpace(edge.From) == "" || strings.TrimSpace(edge.To) == "" {
				issues = append(issues, ValidationIssue{Code: "edge_missing_vertex", Message: fmt.Sprintf("edge at index %d must define both 'from' and 'to'", i)})
				continue
			}
			if edge.From == edge.To {
				issues = append(issues, ValidationIssue{Code: "edge_self", Message: fmt.Sprintf("edge at index %d forms a self-cycle on node '%s'", i, edge.From)})
			}
			if _, ok := nodes[edge.From]; !ok {
				issues = append(issues, ValidationIssue{Code: "edge_from_unknown", Message: fmt.Sprintf("edge at index %d references unknown node '%s' in 'from'", i, edge.From)})
				continue
			}
			if _, ok := nodes[edge.To]; !ok {
				issues = append(issues, ValidationIssue{Code: "edge_to_unknown", Message: fmt.Sprintf("edge at index %d references unknown node '%s' in 'to'", i, edge.To)})
				continue
			}
			adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		}

		if cycle := findCycle(adjacency); cycle != "" {
			issues = append(issues, ValidationIssue{Code: "graph_cycle", Message: fmt.Sprintf("cycle detected: %s", cycle)})
		}
	}

	if len(issues) > 0 {
		sort.Slice(issues, func(i, j int) bool {
			if issues[i].Code == issues[j].Code {
				return issues[i].Message < issues[j].Message
			}
			return issues[i].Code < issues[j].Code
		})
		return &ValidationError{Issues: issues}
	}
	return nil
}

func findCycle(adjacency map[string][]string) string {
	const (
		stateUnvisited = 0
		stateVisiting  = 1
		stateVisited   = 2
	)

	state := make(map[string]int, len(adjacency))
	stack := make([]string, 0, len(adjacency))
	var cycle string

	var dfs func(string) bool
	dfs = func(node string) bool {
		state[node] = stateVisiting
		stack = append(stack, node)

		for _, next := range adjacency[node] {
			switch state[next] {
			case stateVisiting:
				cycle = formatCycle(stack, next)
				return true
			case stateUnvisited:
				if dfs(next) {
					return true
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[node] = stateVisited
		return false
	}

	for node := range adjacency {
		if state[node] == stateUnvisited {
			if dfs(node) {
				return cycle
			}
		}
	}
	return ""
}

func formatCycle(stack []string, start string) string {
	idx := -1
	for i, n := range stack {
		if n == start {
			idx = i
			break
		}
	}
	if idx == -1 {
		return strings.Join(append(stack, start), " → ")
	}
	cycle := append([]string(nil), stack[idx:]...)
	cycle = append(cycle, start)
	return strings.Join(cycle, " → ")
}
