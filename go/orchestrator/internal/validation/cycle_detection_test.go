package validation

import (
	"testing"
)

func TestDetectCyclicDependencies_NoCycle(t *testing.T) {
	// Linear chain: A -> B -> C
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{}},
		{ID: "B", Dependencies: []string{"A"}},
		{ID: "C", Dependencies: []string{"B"}},
	}

	result := DetectCyclicDependencies(subtasks)

	if result.HasCycle {
		t.Errorf("Expected no cycle, but found cycle: %v", result.CyclePath)
	}

	if len(result.SortedOrder) != 3 {
		t.Errorf("Expected 3 items in sorted order, got %d", len(result.SortedOrder))
	}

	// Verify topological order: A must come before B, B must come before C
	aIdx, bIdx, cIdx := -1, -1, -1
	for i, id := range result.SortedOrder {
		switch id {
		case "A":
			aIdx = i
		case "B":
			bIdx = i
		case "C":
			cIdx = i
		}
	}
	if aIdx > bIdx || bIdx > cIdx {
		t.Errorf("Invalid topological order: A@%d, B@%d, C@%d", aIdx, bIdx, cIdx)
	}
}

func TestDetectCyclicDependencies_SimpleCycle(t *testing.T) {
	// Cycle: A -> B -> C -> A
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{"C"}},
		{ID: "B", Dependencies: []string{"A"}},
		{ID: "C", Dependencies: []string{"B"}},
	}

	result := DetectCyclicDependencies(subtasks)

	if !result.HasCycle {
		t.Error("Expected cycle, but none detected")
	}

	if len(result.CyclePath) == 0 {
		t.Error("Expected non-empty cycle path")
	}

	if result.ErrorMessage == "" {
		t.Error("Expected error message")
	}
}

func TestDetectCyclicDependencies_SelfDependency(t *testing.T) {
	// Self-dependency should be skipped (not a cycle in our definition)
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{"A"}}, // Self-reference
		{ID: "B", Dependencies: []string{"A"}},
	}

	result := DetectCyclicDependencies(subtasks)

	// Self-dependencies are filtered out, so this should not be a cycle
	if result.HasCycle {
		t.Error("Self-dependency should be skipped, not treated as cycle")
	}
}

func TestDetectCyclicDependencies_DiamondDependency(t *testing.T) {
	// Diamond: D depends on both B and C, both depend on A
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{}},
		{ID: "B", Dependencies: []string{"A"}},
		{ID: "C", Dependencies: []string{"A"}},
		{ID: "D", Dependencies: []string{"B", "C"}},
	}

	result := DetectCyclicDependencies(subtasks)

	if result.HasCycle {
		t.Errorf("Diamond dependency should not be a cycle: %v", result.CyclePath)
	}

	// Verify topological order: A before B,C; B,C before D
	order := make(map[string]int)
	for i, id := range result.SortedOrder {
		order[id] = i
	}

	if order["A"] > order["B"] || order["A"] > order["C"] {
		t.Error("A should come before B and C")
	}
	if order["B"] > order["D"] || order["C"] > order["D"] {
		t.Error("B and C should come before D")
	}
}

func TestDetectCyclicDependencies_TwoCycle(t *testing.T) {
	// Simple two-node cycle: A <-> B
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{"B"}},
		{ID: "B", Dependencies: []string{"A"}},
	}

	result := DetectCyclicDependencies(subtasks)

	if !result.HasCycle {
		t.Error("Expected cycle between A and B")
	}
}

func TestDetectCyclicDependencies_Empty(t *testing.T) {
	result := DetectCyclicDependencies([]SubtaskInfo{})

	if result.HasCycle {
		t.Error("Empty input should not have cycle")
	}

	if len(result.SortedOrder) != 0 {
		t.Error("Empty input should have empty sorted order")
	}
}

func TestDetectCyclicDependencies_SingleTask(t *testing.T) {
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{}},
	}

	result := DetectCyclicDependencies(subtasks)

	if result.HasCycle {
		t.Error("Single task should not have cycle")
	}

	if len(result.SortedOrder) != 1 || result.SortedOrder[0] != "A" {
		t.Errorf("Expected [A], got %v", result.SortedOrder)
	}
}

func TestDetectCyclicDependencies_UnknownDependency(t *testing.T) {
	// B depends on unknown task "X"
	subtasks := []SubtaskInfo{
		{ID: "A", Dependencies: []string{}},
		{ID: "B", Dependencies: []string{"X"}}, // X doesn't exist
	}

	result := DetectCyclicDependencies(subtasks)

	// Unknown dependencies are skipped, not treated as error
	if result.HasCycle {
		t.Error("Unknown dependency should be skipped, not cause cycle")
	}
}

func TestValidateDAGDependencies(t *testing.T) {
	// Test convenience function
	noCycle := []SubtaskInfo{
		{ID: "A", Dependencies: []string{}},
		{ID: "B", Dependencies: []string{"A"}},
	}

	err := ValidateDAGDependencies(noCycle)
	if err != nil {
		t.Errorf("Expected no error for non-cyclic graph, got: %v", err)
	}

	hasCycle := []SubtaskInfo{
		{ID: "A", Dependencies: []string{"B"}},
		{ID: "B", Dependencies: []string{"A"}},
	}

	err = ValidateDAGDependencies(hasCycle)
	if err == nil {
		t.Error("Expected error for cyclic graph")
	}
}
