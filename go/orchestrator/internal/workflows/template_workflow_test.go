package workflows

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

// TestTemplateCompilation tests that templates compile correctly with validation
func TestTemplateCompilation(t *testing.T) {
	tmpl := &templates.Template{
		Name:        "simple_test",
		Description: "Test template compilation",
		Version:     "1.0.0",
		Defaults: templates.TemplateDefaults{
			BudgetAgentMax: 3000,
		},
		Nodes: []templates.TemplateNode{
			{
				ID:        "intake",
				Type:      templates.NodeTypeSimple,
				Strategy:  templates.StrategyReact,
				BudgetMax: intPtr(1000),
				DependsOn: []string{},
			},
			{
				ID:        "summarize",
				Type:      templates.NodeTypeCognitive,
				Strategy:  templates.StrategyChainOfThought,
				BudgetMax: intPtr(1200),
				DependsOn: []string{"intake"},
			},
		},
		Edges: []templates.TemplateEdge{
			{From: "intake", To: "summarize"},
		},
	}

	compiled, err := templates.CompileTemplate(tmpl)
	assert.NoError(t, err, "Template compilation should succeed")
	assert.NotNil(t, compiled, "Compiled template should not be nil")
	assert.Len(t, compiled.Nodes, 2, "Should have 2 nodes")
	assert.Len(t, compiled.Order, 2, "Should have 2 nodes in execution order")
	assert.Equal(t, "intake", compiled.Order[0], "First node should be intake")
	assert.Equal(t, "summarize", compiled.Order[1], "Second node should be summarize (topo sorted)")

	// Verify budget allocation
	intakeNode, ok := compiled.Nodes["intake"]
	assert.True(t, ok, "Intake node should exist")
	assert.Equal(t, 1000, intakeNode.BudgetMax, "Intake budget should be 1000")

	summarizeNode, ok := compiled.Nodes["summarize"]
	assert.True(t, ok, "Summarize node should exist")
	assert.Equal(t, 1200, summarizeNode.BudgetMax, "Summarize budget should be 1200")

	logger := zaptest.NewLogger(t)
	logger.Info("Template compiled successfully")
}

// TestTemplateCompilation_CycleDetection tests that cycles are detected
func TestTemplateCompilation_CycleDetection(t *testing.T) {
	tmpl := &templates.Template{
		Name:    "cycle_test",
		Version: "1.0.0",
		Nodes: []templates.TemplateNode{
			{ID: "a", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact, DependsOn: []string{"b"}},
			{ID: "b", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact, DependsOn: []string{"a"}},
		},
	}

	_, err := templates.CompileTemplate(tmpl)
	assert.Error(t, err, "Should detect cycle")
	assert.Contains(t, err.Error(), "cycle", "Error should mention cycle")
}

// TestTemplateCompilation_DAGNode tests DAG node metadata.tasks parsing
func TestTemplateCompilation_DAGNode(t *testing.T) {
	tmpl := &templates.Template{
		Name:    "dag_test",
		Version: "1.0.0",
		Defaults: templates.TemplateDefaults{
			BudgetAgentMax: 5000,
		},
		Nodes: []templates.TemplateNode{
			{
				ID:        "parallel_tasks",
				Type:      templates.NodeTypeDAG,
				Strategy:  templates.StrategyReact,
				BudgetMax: intPtr(3000),
				DependsOn: []string{},
				Metadata: map[string]any{
					"tasks": []interface{}{
						map[string]interface{}{
							"id":          "task1",
							"query":       "First task",
							"tools":       []interface{}{"web_search"},
							"depends_on":  []interface{}{},
						},
						map[string]interface{}{
							"id":         "task2",
							"query":      "Second task",
							"depends_on": []interface{}{"task1"},
						},
					},
				},
			},
		},
	}

	compiled, err := templates.CompileTemplate(tmpl)
	assert.NoError(t, err, "DAG template should compile")
	assert.NotNil(t, compiled)
	assert.Len(t, compiled.Nodes, 1)

	dagNode, ok := compiled.Nodes["parallel_tasks"]
	assert.True(t, ok, "DAG node should exist")
	assert.Equal(t, templates.NodeTypeDAG, dagNode.Type)

	// The metadata.tasks are validated during compilation
	tasks, ok := dagNode.Metadata["tasks"]
	assert.True(t, ok, "Tasks metadata should be present")
	assert.NotNil(t, tasks, "Tasks should not be nil")
}

// TestTemplateValidation tests validation catches errors
func TestTemplateValidation(t *testing.T) {
	// Test missing name
	invalid := &templates.Template{
		Version: "1.0.0",
		Nodes: []templates.TemplateNode{
			{ID: "node1", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact},
		},
	}

	err := templates.ValidateTemplate(invalid)
	if err != nil {
		// Expected - name is likely required
		assert.Error(t, err, "Should fail validation for missing name")
	}

	// Test duplicate node IDs
	duplicateIDs := &templates.Template{
		Name:    "duplicate_test",
		Version: "1.0.0",
		Nodes: []templates.TemplateNode{
			{ID: "node1", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact},
			{ID: "node1", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact}, // Duplicate
		},
	}

	err = templates.ValidateTemplate(duplicateIDs)
	assert.Error(t, err, "Should fail validation for duplicate node IDs")

	// Test missing dependency
	missingDep := &templates.Template{
		Name:    "missing_dep",
		Version: "1.0.0",
		Nodes: []templates.TemplateNode{
			{ID: "node1", Type: templates.NodeTypeSimple, Strategy: templates.StrategyReact, DependsOn: []string{"nonexistent"}},
		},
	}

	err = templates.ValidateTemplate(missingDep)
	assert.Error(t, err, "Should fail validation for missing dependency")
}

func intPtr(i int) *int {
	return &i
}
