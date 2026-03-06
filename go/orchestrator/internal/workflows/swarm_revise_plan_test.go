package workflows

import (
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

func TestRevisePlanDependsOnParsing(t *testing.T) {
	newTask := map[string]interface{}{
		"id":          "T6",
		"description": "Cross-analysis of all findings",
		"depends_on":  []interface{}{"T1", "T2", "T3"},
	}

	task := activities.SwarmTask{
		ID:          newTask["id"].(string),
		Description: newTask["description"].(string),
		CreatedBy:   "lead",
	}
	if deps, ok := newTask["depends_on"].([]interface{}); ok {
		for _, d := range deps {
			task.DependsOn = append(task.DependsOn, d.(string))
		}
	}

	if len(task.DependsOn) != 3 {
		t.Errorf("expected 3 depends_on, got %d", len(task.DependsOn))
	}
	expected := []string{"T1", "T2", "T3"}
	for i, dep := range task.DependsOn {
		if dep != expected[i] {
			t.Errorf("depends_on[%d] = %q, want %q", i, dep, expected[i])
		}
	}
}

func TestRevisePlanNoDependsOn(t *testing.T) {
	newTask := map[string]interface{}{
		"id":          "T7",
		"description": "Simple standalone task",
	}

	task := activities.SwarmTask{
		ID:          newTask["id"].(string),
		Description: newTask["description"].(string),
		CreatedBy:   "lead",
	}
	if deps, ok := newTask["depends_on"].([]interface{}); ok {
		for _, d := range deps {
			task.DependsOn = append(task.DependsOn, d.(string))
		}
	}

	if len(task.DependsOn) != 0 {
		t.Errorf("expected 0 depends_on for task without deps, got %d", len(task.DependsOn))
	}
}
