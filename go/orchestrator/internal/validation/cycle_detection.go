// Package validation provides utilities for validating workflow configurations.
package validation

import (
	"fmt"
	"strings"
)

// SubtaskInfo represents the minimal information needed for cycle detection
type SubtaskInfo struct {
	ID           string
	Dependencies []string
}

// CycleDetectionResult contains the result of cycle detection
type CycleDetectionResult struct {
	HasCycle     bool
	CyclePath    []string // IDs involved in the cycle (if found)
	SortedOrder  []string // Topological order (if no cycle)
	ErrorMessage string
}

// DetectCyclicDependencies checks for circular dependencies in subtask dependencies
// using Kahn's algorithm (topological sort). Returns an error if a cycle is detected.
//
// Example cycle: A depends on B, B depends on C, C depends on A
// This would cause the DAG workflow to hang indefinitely.
func DetectCyclicDependencies(subtasks []SubtaskInfo) CycleDetectionResult {
	if len(subtasks) == 0 {
		return CycleDetectionResult{HasCycle: false, SortedOrder: []string{}}
	}

	// Build adjacency list and in-degree map
	inDegree := make(map[string]int)
	graph := make(map[string][]string) // task -> tasks that depend on it
	allNodes := make(map[string]bool)

	// Initialize all nodes
	for _, st := range subtasks {
		allNodes[st.ID] = true
		if _, exists := inDegree[st.ID]; !exists {
			inDegree[st.ID] = 0
		}
		if _, exists := graph[st.ID]; !exists {
			graph[st.ID] = []string{}
		}
	}

	// Build the graph: if A depends on B, then B -> A in graph
	for _, st := range subtasks {
		for _, dep := range st.Dependencies {
			// Skip self-dependencies and non-existent dependencies
			if dep == st.ID {
				continue
			}
			if !allNodes[dep] {
				// Dependency references unknown task - not a cycle, but could be an error
				continue
			}
			graph[dep] = append(graph[dep], st.ID)
			inDegree[st.ID]++
		}
	}

	// Kahn's algorithm: start with nodes that have no incoming edges
	queue := []string{}
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	sortedOrder := []string{}
	processedCount := 0

	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]

		sortedOrder = append(sortedOrder, current)
		processedCount++

		// Reduce in-degree for all dependents
		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If we processed all nodes, there's no cycle
	if processedCount == len(allNodes) {
		return CycleDetectionResult{
			HasCycle:    false,
			SortedOrder: sortedOrder,
		}
	}

	// Cycle detected - find the nodes involved
	cycleNodes := []string{}
	for node, degree := range inDegree {
		if degree > 0 {
			cycleNodes = append(cycleNodes, node)
		}
	}

	// Try to find the actual cycle path using DFS
	cyclePath := findCyclePath(graph, cycleNodes)

	return CycleDetectionResult{
		HasCycle:     true,
		CyclePath:    cyclePath,
		ErrorMessage: fmt.Sprintf("circular dependency detected involving tasks: %s", strings.Join(cyclePath, " -> ")),
	}
}

// findCyclePath attempts to find the actual cycle path using DFS
func findCyclePath(graph map[string][]string, cycleNodes []string) []string {
	if len(cycleNodes) == 0 {
		return []string{}
	}

	// Convert to set for quick lookup
	cycleSet := make(map[string]bool)
	for _, n := range cycleNodes {
		cycleSet[n] = true
	}

	// DFS from each cycle node to find the cycle
	visited := make(map[string]bool)
	path := []string{}

	var dfs func(node string, currentPath []string) []string
	dfs = func(node string, currentPath []string) []string {
		if visited[node] {
			// Found cycle - extract the cycle portion
			for i, n := range currentPath {
				if n == node {
					cycle := append(currentPath[i:], node)
					return cycle
				}
			}
			return nil
		}

		if !cycleSet[node] {
			return nil
		}

		visited[node] = true
		currentPath = append(currentPath, node)

		for _, next := range graph[node] {
			if cycleSet[next] {
				result := dfs(next, currentPath)
				if result != nil {
					return result
				}
			}
		}

		return nil
	}

	// Start DFS from each cycle node
	for _, start := range cycleNodes {
		visited = make(map[string]bool)
		result := dfs(start, path)
		if result != nil && len(result) > 1 {
			return result
		}
	}

	// Fallback: just return the cycle nodes
	return cycleNodes
}

// ValidateDAGDependencies is a convenience function that returns an error if cycles exist
func ValidateDAGDependencies(subtasks []SubtaskInfo) error {
	result := DetectCyclicDependencies(subtasks)
	if result.HasCycle {
		return fmt.Errorf("%s", result.ErrorMessage)
	}
	return nil
}
