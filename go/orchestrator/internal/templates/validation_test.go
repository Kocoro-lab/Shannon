package templates

import (
	"strings"
	"testing"
)

func TestValidateTemplateSuccess(t *testing.T) {
	tpl := &Template{
		Name:     "research_summary",
		Defaults: TemplateDefaults{ModelTier: "medium", BudgetAgentMax: 6000},
		Nodes: []TemplateNode{
			{ID: "discover", Type: NodeTypeSimple, Strategy: StrategyReact},
			{ID: "reason", Type: NodeTypeCognitive, Strategy: StrategyChainOfThought, DependsOn: []string{"discover"}},
			{ID: "finalize", Type: NodeTypeCognitive, Strategy: StrategyReflection, DependsOn: []string{"reason"}},
		},
		Edges: []TemplateEdge{
			{From: "discover", To: "reason"},
			{From: "reason", To: "finalize"},
		},
	}

	if err := ValidateTemplate(tpl); err != nil {
		t.Fatalf("expected template to validate, got %v", err)
	}
}

func TestValidateTemplateDetectsCycle(t *testing.T) {
	tpl := &Template{
		Name: "cycle",
		Nodes: []TemplateNode{
			{ID: "a", Type: NodeTypeSimple, DependsOn: []string{"c"}},
			{ID: "b", Type: NodeTypeSimple, DependsOn: []string{"a"}},
			{ID: "c", Type: NodeTypeSimple, DependsOn: []string{"b"}},
		},
	}

	err := ValidateTemplate(tpl)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	vErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	found := false
	for _, issue := range vErr.Issues {
		if issue.Code == "graph_cycle" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cycle detected issue, got %+v", vErr.Issues)
	}
}

func TestValidateTemplateUnknownStrategy(t *testing.T) {
	tpl := &Template{
		Name: "bad_strategy",
		Nodes: []TemplateNode{
			{ID: "n1", Type: NodeTypeCognitive, Strategy: "custom"},
		},
	}

	err := ValidateTemplate(tpl)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "unknown strategy 'custom'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTemplateBudgetExceedsDefault(t *testing.T) {
	over := 9000
	tpl := &Template{
		Name:     "budget",
		Defaults: TemplateDefaults{BudgetAgentMax: 8000},
		Nodes:    []TemplateNode{{ID: "n1", Type: NodeTypeSimple, BudgetMax: &over}},
	}

	err := ValidateTemplate(tpl)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "budget_max 9000 exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTemplateOnFailInvalidDegrade(t *testing.T) {
	tpl := &Template{
		Name: "bad_on_fail",
		Nodes: []TemplateNode{{
			ID:   "n1",
			Type: NodeTypeCognitive,
			OnFail: &TemplateNodeFailure{
				DegradeTo: "not_real",
			},
		}},
	}

	err := ValidateTemplate(tpl)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "on_fail.degrade_to 'not_real'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTemplateRejectsUnknownFields(t *testing.T) {
	yaml := `name: sample
defaults:
  model_tier: medium
nodes:
  - id: a
    type: simple
    extra: true
`
	_, err := LoadTemplate(strings.NewReader(yaml))
	if err == nil {
		t.Fatalf("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "field extra not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
