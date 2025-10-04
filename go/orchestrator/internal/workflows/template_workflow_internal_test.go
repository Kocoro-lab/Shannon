package workflows

import (
	"reflect"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
)

func TestAggregateDependencyOutputs(t *testing.T) {
	rt := &templateRuntime{
		NodeResults: map[string]TemplateNodeResult{
			"a": {Result: "alpha", Success: true},
			"b": {Result: "beta", Success: true},
		},
	}

	node := templates.ExecutableNode{DependsOn: []string{"a", "b"}}

	got := aggregateDependencyOutputs(rt, node)
	want := "[a]\nalpha\n\n[b]\nbeta"
	if got != want {
		t.Fatalf("unexpected aggregation:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestParseHybridTasks(t *testing.T) {
	metadata := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":          "t1",
				"description": "task one",
				"depends_on":  []interface{}{"root"},
				"tools":       []interface{}{"web_search"},
			},
			map[string]interface{}{
				"id":    "t2",
				"query": "execute",
			},
		},
	}

	tasks, err := parseHybridTasks(metadata)
	if err != nil {
		t.Fatalf("parseHybridTasks returned error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "t1" || tasks[0].Description != "task one" {
		t.Fatalf("unexpected first task: %+v", tasks[0])
	}
	if got := tasks[0].Dependencies; !reflect.DeepEqual(got, []string{"root"}) {
		t.Fatalf("unexpected dependencies: %v", got)
	}
	if got := tasks[0].SuggestedTools; !reflect.DeepEqual(got, []string{"web_search"}) {
		t.Fatalf("unexpected tools: %v", got)
	}
	if tasks[1].ID != "t2" || tasks[1].Description != "execute" {
		t.Fatalf("unexpected second task: %+v", tasks[1])
	}
}
