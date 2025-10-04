package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryLoadDirectory(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: research_summary
version: v1
defaults:
  model_tier: medium
  budget_agent_max: 6000
nodes:
  - id: discover
    type: simple
    strategy: react
  - id: finalize
    type: cognitive
    strategy: reflection
    depends_on: [discover]
`
	if err := os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadDirectory(dir); err != nil {
		t.Fatalf("LoadDirectory: %v", err)
	}
	if err := reg.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	entry, ok := reg.Get(MakeKey("research_summary", "v1"))
	if !ok {
		t.Fatalf("expected template entry to be present")
	}
	if entry.Template.Name != "research_summary" {
		t.Fatalf("unexpected template name: %s", entry.Template.Name)
	}
	if entry.Template.Version != "v1" {
		t.Fatalf("unexpected template version: %s", entry.Template.Version)
	}
	if entry.ContentHash == "" {
		t.Fatalf("expected content hash to be populated")
	}

	summaries := reg.List()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Key != "research_summary@v1" {
		t.Fatalf("unexpected summary key: %s", summaries[0].Key)
	}
}

func TestRegistryDuplicateTemplate(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: duplicate
nodes:
  - id: n1
    type: simple
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write template a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write template b: %v", err)
	}

	reg := NewRegistry()
	err := reg.LoadDirectory(dir)
	if err == nil {
		t.Fatalf("expected duplicate error")
	}
	if !IsLoadError(err) {
		t.Fatalf("expected LoadError, got %T", err)
	}
}

func TestRegistryFindByName(t *testing.T) {
	dir := t.TempDir()
	yamlV1 := `name: sample
version: v1
nodes:
  - id: n1
    type: simple
`
	yamlV2 := `name: sample
version: v2
nodes:
  - id: n1
    type: simple
`
	if err := os.WriteFile(filepath.Join(dir, "v1.yaml"), []byte(yamlV1), 0o600); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "v2.yaml"), []byte(yamlV2), 0o600); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadDirectory(dir); err != nil {
		t.Fatalf("LoadDirectory: %v", err)
	}
	if err := reg.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if _, ok := reg.Find("sample", "v1"); !ok {
		t.Fatalf("expected to find sample@v1")
	}
	entry, ok := reg.Find("sample", "")
	if !ok {
		t.Fatalf("expected to find sample with latest version")
	}
	if entry.Template.Version != "v2" {
		t.Fatalf("expected latest version v2, got %s", entry.Template.Version)
	}
}

func TestRegistryTemplateInheritance(t *testing.T) {
	dir := t.TempDir()

	// Base template
	baseYAML := `name: base_research
version: v1
defaults:
  model_tier: medium
  budget_agent_max: 5000
nodes:
  - id: discover
    type: simple
    strategy: react
    budget_max: 1000
  - id: analyze
    type: cognitive
    strategy: chain_of_thought
    budget_max: 2000
    depends_on: [discover]
edges:
  - from: discover
    to: analyze
`

	// Derived template - extends base
	derivedYAML := `name: enterprise_research
version: v1
extends:
  - base_research
defaults:
  model_tier: large
  budget_agent_max: 8000
nodes:
  - id: discover
    budget_max: 1500
    metadata:
      depth: deep
  - id: finalize
    type: cognitive
    strategy: reflection
    depends_on: [analyze]
`

	if err := os.WriteFile(filepath.Join(dir, "base.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("write base template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "derived.yaml"), []byte(derivedYAML), 0o600); err != nil {
		t.Fatalf("write derived template: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadDirectory(dir); err != nil {
		t.Fatalf("LoadDirectory: %v", err)
	}
	if err := reg.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Verify base template loaded correctly
	baseEntry, ok := reg.Get(MakeKey("base_research", "v1"))
	if !ok {
		t.Fatalf("expected base template to be present")
	}
	if len(baseEntry.Template.Nodes) != 2 {
		t.Fatalf("base template should have 2 nodes, got %d", len(baseEntry.Template.Nodes))
	}

	// Verify derived template merged correctly
	derivedEntry, ok := reg.Get(MakeKey("enterprise_research", "v1"))
	if !ok {
		t.Fatalf("expected derived template to be present")
	}

	tpl := derivedEntry.Template

	// Check defaults override
	if tpl.Defaults.ModelTier != "large" {
		t.Fatalf("expected model_tier 'large', got %s", tpl.Defaults.ModelTier)
	}
	if tpl.Defaults.BudgetAgentMax != 8000 {
		t.Fatalf("expected budget_agent_max 8000, got %d", tpl.Defaults.BudgetAgentMax)
	}

	// Check nodes merged: should have 3 nodes (discover overridden, analyze inherited, finalize added)
	if len(tpl.Nodes) != 3 {
		t.Fatalf("expected 3 nodes after merge, got %d", len(tpl.Nodes))
	}

	// Check discover node was overridden
	var discoverNode *TemplateNode
	for i := range tpl.Nodes {
		if tpl.Nodes[i].ID == "discover" {
			discoverNode = &tpl.Nodes[i]
			break
		}
	}
	if discoverNode == nil {
		t.Fatalf("discover node not found")
	}
	if discoverNode.BudgetMax == nil || *discoverNode.BudgetMax != 1500 {
		t.Fatalf("expected discover budget_max 1500, got %v", discoverNode.BudgetMax)
	}
	if discoverNode.Metadata == nil || discoverNode.Metadata["depth"] != "deep" {
		t.Fatalf("expected discover metadata depth=deep, got %v", discoverNode.Metadata)
	}
	if discoverNode.Type != "simple" || discoverNode.Strategy != "react" {
		t.Fatalf("discover node should inherit type and strategy from base")
	}

	// Check analyze node was inherited
	var analyzeNode *TemplateNode
	for i := range tpl.Nodes {
		if tpl.Nodes[i].ID == "analyze" {
			analyzeNode = &tpl.Nodes[i]
			break
		}
	}
	if analyzeNode == nil {
		t.Fatalf("analyze node not found (should be inherited)")
	}
	if analyzeNode.Strategy != "chain_of_thought" {
		t.Fatalf("analyze node should have inherited strategy, got %s", analyzeNode.Strategy)
	}

	// Check finalize node was added
	var finalizeNode *TemplateNode
	for i := range tpl.Nodes {
		if tpl.Nodes[i].ID == "finalize" {
			finalizeNode = &tpl.Nodes[i]
			break
		}
	}
	if finalizeNode == nil {
		t.Fatalf("finalize node not found")
	}
	if finalizeNode.Strategy != "reflection" {
		t.Fatalf("expected finalize strategy 'reflection', got %s", finalizeNode.Strategy)
	}

	// Check extends field cleared after finalize
	if len(tpl.Extends) != 0 {
		t.Fatalf("expected extends to be cleared after finalize, got %v", tpl.Extends)
	}
}

func TestRegistryMultiLevelInheritance(t *testing.T) {
	dir := t.TempDir()

	// Base template
	baseYAML := `name: base
version: v1
defaults:
  model_tier: small
nodes:
  - id: step1
    type: simple
`

	// Middle template
	middleYAML := `name: middle
version: v1
extends:
  - base
defaults:
  model_tier: medium
nodes:
  - id: step2
    type: simple
`

	// Top template
	topYAML := `name: top
version: v1
extends:
  - middle
defaults:
  model_tier: large
nodes:
  - id: step3
    type: simple
`

	if err := os.WriteFile(filepath.Join(dir, "base.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "middle.yaml"), []byte(middleYAML), 0o600); err != nil {
		t.Fatalf("write middle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.yaml"), []byte(topYAML), 0o600); err != nil {
		t.Fatalf("write top: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadDirectory(dir); err != nil {
		t.Fatalf("LoadDirectory: %v", err)
	}
	if err := reg.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	topEntry, ok := reg.Get(MakeKey("top", "v1"))
	if !ok {
		t.Fatalf("top template not found")
	}

	// Should have all 3 steps
	if len(topEntry.Template.Nodes) != 3 {
		t.Fatalf("expected 3 nodes (step1+step2+step3), got %d", len(topEntry.Template.Nodes))
	}

	// Should have final tier override
	if topEntry.Template.Defaults.ModelTier != "large" {
		t.Fatalf("expected model_tier 'large', got %s", topEntry.Template.Defaults.ModelTier)
	}
}

func TestRegistryInheritanceCycleDetection(t *testing.T) {
	dir := t.TempDir()

	// Template A extends B
	aYAML := `name: template_a
version: v1
extends:
  - template_b
nodes:
  - id: n1
    type: simple
`

	// Template B extends A (cycle!)
	bYAML := `name: template_b
version: v1
extends:
  - template_a
nodes:
  - id: n2
    type: simple
`

	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(aYAML), 0o600); err != nil {
		t.Fatalf("write template a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(bYAML), 0o600); err != nil {
		t.Fatalf("write template b: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadDirectory(dir); err != nil {
		t.Fatalf("LoadDirectory: %v", err)
	}

	// Finalize should detect cycle
	err := reg.Finalize()
	if err == nil {
		t.Fatalf("expected cycle detection error")
	}
	if err.Error() != "template inheritance cycle detected for 'template_a@v1'" &&
		err.Error() != "template inheritance cycle detected for 'template_b@v1'" {
		t.Fatalf("unexpected error: %v", err)
	}
}
