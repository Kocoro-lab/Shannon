package templates

import "testing"

func TestCompileTemplateLinear(t *testing.T) {
	tpl := &Template{
		Name: "linear",
		Defaults: TemplateDefaults{
			BudgetAgentMax: 5000,
		},
		Nodes: []TemplateNode{
			{ID: "discover", Type: NodeTypeSimple, Strategy: StrategyReact},
			{ID: "reason", Type: NodeTypeCognitive, Strategy: StrategyChainOfThought, DependsOn: []string{"discover"}},
			{ID: "finalize", Type: NodeTypeCognitive, Strategy: StrategyReflection, DependsOn: []string{"reason"}},
		},
	}

	plan, err := CompileTemplate(tpl)
	if err != nil {
		t.Fatalf("CompileTemplate: %v", err)
	}
	if plan.TemplateName != "linear" {
		t.Fatalf("unexpected template name: %s", plan.TemplateName)
	}
	if len(plan.Order) != 3 {
		t.Fatalf("expected 3 nodes in order, got %d", len(plan.Order))
	}
	if plan.Order[0] != "discover" || plan.Order[2] != "finalize" {
		t.Fatalf("unexpected order: %v", plan.Order)
	}
	if plan.Nodes["discover"].BudgetMax != 5000 {
		t.Fatalf("expected default budget applied")
	}
	if deps := plan.Nodes["reason"].DependsOn; len(deps) != 1 || deps[0] != "discover" {
		t.Fatalf("expected reason to depend on discover, got %v", deps)
	}
}

func TestCompileTemplateBudgetsAndEdges(t *testing.T) {
	oneThousand := 1000
	tpl := &Template{
		Name:     "branch",
		Defaults: TemplateDefaults{BudgetAgentMax: 4000},
		Nodes: []TemplateNode{
			{ID: "start", Type: NodeTypeSimple, Strategy: StrategyReact},
			{ID: "branch_a", Type: NodeTypeCognitive, Strategy: StrategyChainOfThought, DependsOn: []string{"start"}, BudgetMax: &oneThousand},
			{ID: "branch_b", Type: NodeTypeCognitive, Strategy: StrategyDebate, DependsOn: []string{"start"}},
			{ID: "join", Type: NodeTypeCognitive, Strategy: StrategyReflection, DependsOn: []string{"branch_a", "branch_b"}},
		},
		Edges: []TemplateEdge{{From: "start", To: "join"}},
	}

	plan, err := CompileTemplate(tpl)
	if err != nil {
		t.Fatalf("CompileTemplate: %v", err)
	}

	if plan.Nodes["branch_a"].BudgetMax != 1000 {
		t.Fatalf("expected branch_a budget override to apply")
	}
	if plan.Nodes["branch_b"].BudgetMax != 4000 {
		t.Fatalf("expected branch_b to inherit default budget")
	}

	children := plan.Adjacency["start"]
	if len(children) != 3 {
		t.Fatalf("expected start to have 3 outgoing edges (branch_a, branch_b, join), got %v", children)
	}

	if len(plan.Order) != 4 {
		t.Fatalf("expected topological order to include 4 nodes")
	}
}

func TestCompileTemplateCycleDetected(t *testing.T) {
	tpl := &Template{
		Name: "cycle",
		Nodes: []TemplateNode{
			{ID: "a", Type: NodeTypeSimple, DependsOn: []string{"c"}},
			{ID: "b", Type: NodeTypeSimple, DependsOn: []string{"a"}},
			{ID: "c", Type: NodeTypeSimple, DependsOn: []string{"b"}},
		},
	}

	_, err := CompileTemplate(tpl)
	if err == nil {
		t.Fatalf("expected compile to fail due to cycle")
	}
}
