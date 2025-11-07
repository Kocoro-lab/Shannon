package patterns

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/util"
	"go.temporal.io/sdk/workflow"
)

// TreeOfThoughtsConfig configures the tree-of-thoughts pattern
type TreeOfThoughtsConfig struct {
	MaxDepth          int     // Maximum tree depth
	BranchingFactor   int     // Number of branches per node (2-4)
	EvaluationMethod  string  // "scoring", "voting", "llm"
	PruningThreshold  float64 // Minimum score to continue branch (0-1)
	ExplorationBudget int     // Max total thoughts to explore
	BacktrackEnabled  bool    // Allow backtracking to promising branches
	ModelTier         string  // Model tier for thought generation
}

// ThoughtNode represents a node in the thought tree
type ThoughtNode struct {
	ID          string
	Thought     string
	Score       float64
	Depth       int
	ParentID    string
	Children    []*ThoughtNode
	TokensUsed  int
	IsTerminal  bool
	Explanation string
}

// TreeOfThoughtsResult contains the exploration result
type TreeOfThoughtsResult struct {
	BestPath        []*ThoughtNode
	BestSolution    string
	TotalThoughts   int
	TreeDepth       int
	TotalTokens     int
	ExplorationTree *ThoughtNode // Root of the tree
	Confidence      float64
}

// TreeOfThoughts implements systematic exploration of solution paths
// This pattern explores multiple reasoning branches and selects the most promising path
func TreeOfThoughts(
	ctx workflow.Context,
	query string,
	context map[string]interface{},
	sessionID string,
	history []string,
	config TreeOfThoughtsConfig,
	opts Options,
) (*TreeOfThoughtsResult, error) {

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Tree-of-Thoughts exploration",
		"query", query,
		"max_depth", config.MaxDepth,
		"branching_factor", config.BranchingFactor,
	)

	// Set defaults
	if config.MaxDepth == 0 {
		config.MaxDepth = 3
	}
	if config.BranchingFactor == 0 {
		config.BranchingFactor = 3
	}
	if config.BranchingFactor > 4 {
		config.BranchingFactor = 4 // Cap for manageability
	}
	if config.PruningThreshold == 0 {
		config.PruningThreshold = 0.3
	}
	if config.ExplorationBudget == 0 {
		config.ExplorationBudget = 15
	}
	if config.EvaluationMethod == "" {
		config.EvaluationMethod = "scoring"
	}
	if config.ModelTier == "" {
		config.ModelTier = opts.ModelTier
		if config.ModelTier == "" {
			config.ModelTier = "medium"
		}
	}

	result := &TreeOfThoughtsResult{
		BestPath: make([]*ThoughtNode, 0),
	}

	// Initialize root node
	root := &ThoughtNode{
		ID:          "root",
		Thought:     query,
		Score:       1.0,
		Depth:       0,
		Children:    make([]*ThoughtNode, 0),
		IsTerminal:  false,
		Explanation: "Initial problem statement",
	}
	result.ExplorationTree = root

	// Track exploration budget
	thoughtsExplored := 0
	tokenBudgetPerThought := 0
	if opts.BudgetAgentMax > 0 {
		tokenBudgetPerThought = opts.BudgetAgentMax / config.ExplorationBudget
	}

	// Exploration queue (for BFS-like exploration with scoring)
	queue := []*ThoughtNode{root}
	allNodes := []*ThoughtNode{root}

	// Main exploration loop
	for len(queue) > 0 && thoughtsExplored < config.ExplorationBudget {
		// Get most promising node (based on score)
		sort.Slice(queue, func(i, j int) bool {
			return queue[i].Score > queue[j].Score
		})

		current := queue[0]
		queue = queue[1:]

		// Check depth limit
		if current.Depth >= config.MaxDepth {
			current.IsTerminal = true
			continue
		}

		logger.Info("Exploring thought node",
			"id", current.ID,
			"depth", current.Depth,
			"score", current.Score,
		)

		// Generate branches from current node
		branches := generateBranches(
			ctx,
			current,
			query,
			config.BranchingFactor,
			context,
			sessionID,
			history,
			tokenBudgetPerThought,
			config.ModelTier,
			opts,
		)

		thoughtsExplored += len(branches)
		result.TotalThoughts = thoughtsExplored

		// Evaluate and prune branches
		for _, branch := range branches {
			// Calculate score
			branch.Score = evaluateThought(ctx, branch, query, config.EvaluationMethod, opts)

			// Prune low-scoring branches
			if branch.Score < config.PruningThreshold {
				logger.Info("Pruning low-scoring branch",
					"id", branch.ID,
					"score", branch.Score,
				)
				continue
			}

			// Add to tree
			current.Children = append(current.Children, branch)
			allNodes = append(allNodes, branch)
			result.TotalTokens += branch.TokensUsed

			// Check if terminal (solution found or dead end)
			if isTerminalThought(branch.Thought) {
				branch.IsTerminal = true
			} else {
				// Add to exploration queue
				queue = append(queue, branch)
			}
		}

		// Update tree depth
		if current.Depth+1 > result.TreeDepth {
			result.TreeDepth = current.Depth + 1
		}
	}

	// Find best path through tree
	result.BestPath = findBestPath(root)

	// Generate final solution from best path
	if len(result.BestPath) > 0 {
		result.BestSolution = synthesizeSolution(result.BestPath, query)
		result.Confidence = calculatePathConfidence(result.BestPath)
	} else {
		result.BestSolution = "No viable solution path found"
		result.Confidence = 0.0
	}

	// Backtrack if enabled and confidence is low
	if config.BacktrackEnabled && result.Confidence < 0.5 && len(queue) > 0 {
		logger.Info("Backtracking to explore alternative paths")

		// Explore top alternatives
		alternatives := queue[:min(3, len(queue))]
		for _, alt := range alternatives {
			altPath := getPathToNode(alt, allNodes)
			altConfidence := calculatePathConfidence(altPath)

			if altConfidence > result.Confidence {
				result.BestPath = altPath
				result.BestSolution = synthesizeSolution(altPath, query)
				result.Confidence = altConfidence
			}
		}
	}

	logger.Info("Tree-of-Thoughts completed",
		"total_thoughts", result.TotalThoughts,
		"tree_depth", result.TreeDepth,
		"best_path_length", len(result.BestPath),
		"confidence", result.Confidence,
		"tokens", result.TotalTokens,
	)

	return result, nil
}

// generateBranches creates child thoughts from current node
func generateBranches(
	ctx workflow.Context,
	parent *ThoughtNode,
	originalQuery string,
	branchingFactor int,
	context map[string]interface{},
	sessionID string,
	history []string,
	tokenBudget int,
	modelTier string,
	opts Options,
) []*ThoughtNode {

	logger := workflow.GetLogger(ctx)
	branches := make([]*ThoughtNode, 0, branchingFactor)

	// Build branching prompt
	branchPrompt := fmt.Sprintf(`Given this problem: %s

Current reasoning path: %s

Generate %d different next steps or approaches to explore. Each should be:
1. A distinct direction or method
2. Building on the current thought
3. Moving toward a solution

Format each as a clear, concise thought.`,
		originalQuery,
		parent.Thought,
		branchingFactor,
	)

	// Generate branches
	var branchResult activities.AgentExecutionResult
    if tokenBudget > 0 {
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if context != nil {
            if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        err := workflow.ExecuteActivity(ctx,
            constants.ExecuteAgentWithBudgetActivity,
            activities.BudgetedAgentInput{
				AgentInput: activities.AgentExecutionInput{
					Query:             branchPrompt,
					AgentID:           fmt.Sprintf("tot-generator-%s", parent.ID),
					Context:           context,
					Mode:              "exploration",
					SessionID:         sessionID,
					History:           history,
                        ParentWorkflowID: wid,
				},
				MaxTokens: tokenBudget,
				UserID:    opts.UserID,
				TaskID:    wid,
				ModelTier: modelTier,
			}).Get(ctx, &branchResult)
        if err != nil {
            logger.Warn("Failed to generate branches", "error", err)
            return branches
        }
    } else {
        // Determine parent workflow for streaming correlation
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if context != nil {
            if p, ok := context["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        err := workflow.ExecuteActivity(ctx,
            activities.ExecuteAgent,
            activities.AgentExecutionInput{
                Query:             branchPrompt,
                AgentID:           fmt.Sprintf("tot-generator-%s", parent.ID),
                Context:           context,
                Mode:              "exploration",
                SessionID:         sessionID,
                History:           history,
                ParentWorkflowID:  wid,
            }).Get(ctx, &branchResult)
        if err != nil {
            logger.Warn("Failed to generate branches", "error", err)
            return branches
        }
    }

	// Parse generated branches
	thoughts := parseBranches(branchResult.Response, branchingFactor)

	// Create thought nodes
	for i, thought := range thoughts {
		node := &ThoughtNode{
			ID:          fmt.Sprintf("%s-%d", parent.ID, i),
			Thought:     thought,
			Depth:       parent.Depth + 1,
			ParentID:    parent.ID,
			Children:    make([]*ThoughtNode, 0),
			TokensUsed:  branchResult.TokensUsed / len(thoughts), // Distribute tokens
			IsTerminal:  false,
			Explanation: fmt.Sprintf("Branch %d from %s", i+1, parent.ID),
		}
		branches = append(branches, node)
	}

	return branches
}

// evaluateThought scores a thought node
func evaluateThought(
	ctx workflow.Context,
	node *ThoughtNode,
	originalQuery string,
	method string,
	opts Options,
) float64 {

	// Simple heuristic scoring
	score := 0.5 // Base score

	// Check for solution indicators
	thought := strings.ToLower(node.Thought)
	if strings.Contains(thought, "therefore") ||
		strings.Contains(thought, "solution") ||
		strings.Contains(thought, "answer") {
		score += 0.2
	}

	// Check for logical progression
	if strings.Contains(thought, "because") ||
		strings.Contains(thought, "since") ||
		strings.Contains(thought, "thus") {
		score += 0.1
	}

	// Check for concrete steps
	if strings.Contains(thought, "step") ||
		strings.Contains(thought, "first") ||
		strings.Contains(thought, "next") {
		score += 0.1
	}

	// Penalize vague thoughts
	if strings.Contains(thought, "maybe") ||
		strings.Contains(thought, "perhaps") ||
		strings.Contains(thought, "might") {
		score -= 0.1
	}

	// Depth penalty (prefer shorter paths)
	score -= float64(node.Depth) * 0.05

	// Clamp between 0 and 1
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// isTerminalThought checks if a thought represents a solution or dead end
func isTerminalThought(thought string) bool {
	lower := strings.ToLower(thought)

	// Solution indicators
	solutionKeywords := []string{
		"the answer is",
		"therefore",
		"in conclusion",
		"final answer",
		"solution:",
		"result:",
	}

	for _, keyword := range solutionKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	// Dead end indicators
	deadEndKeywords := []string{
		"impossible",
		"cannot be solved",
		"no solution",
		"contradiction",
	}

	for _, keyword := range deadEndKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	return false
}

// findBestPath finds the highest scoring path through the tree
func findBestPath(root *ThoughtNode) []*ThoughtNode {
	if root == nil {
		return nil
	}

	bestPath := []*ThoughtNode{}
	bestScore := 0.0

	// DFS to find all paths
	var findPaths func(node *ThoughtNode, currentPath []*ThoughtNode, currentScore float64)
	findPaths = func(node *ThoughtNode, currentPath []*ThoughtNode, currentScore float64) {
		currentPath = append(currentPath, node)
		currentScore += node.Score

		// If terminal or leaf, check if best
		if node.IsTerminal || len(node.Children) == 0 {
			avgScore := currentScore / float64(len(currentPath))
			if avgScore > bestScore {
				bestScore = avgScore
				bestPath = make([]*ThoughtNode, len(currentPath))
				copy(bestPath, currentPath)
			}
			return
		}

		// Explore children
		for _, child := range node.Children {
			findPaths(child, currentPath, currentScore)
		}
	}

	findPaths(root, []*ThoughtNode{}, 0)
	return bestPath
}

// getPathToNode reconstructs path from root to given node
func getPathToNode(target *ThoughtNode, allNodes []*ThoughtNode) []*ThoughtNode {
	path := []*ThoughtNode{}
	current := target

	// Build path backwards
	for current != nil {
		path = append([]*ThoughtNode{current}, path...)

		// Find parent
		if current.ParentID == "" {
			break
		}

		parent := findNodeByID(current.ParentID, allNodes)
		current = parent
	}

	return path
}

// findNodeByID finds a node by ID in the list
func findNodeByID(id string, nodes []*ThoughtNode) *ThoughtNode {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	return nil
}

// synthesizeSolution creates final answer from best path
func synthesizeSolution(path []*ThoughtNode, query string) string {
	if len(path) == 0 {
		return "No solution found"
	}

	// Build solution narrative
	solution := fmt.Sprintf("Solution to: %s\n\n", query)
	solution += "Reasoning path:\n"

	for i, node := range path {
		if i == 0 {
			continue // Skip root
		}
		solution += fmt.Sprintf("%d. %s\n", i, node.Thought)
	}

	// Add final answer if present
	lastNode := path[len(path)-1]
	if lastNode.IsTerminal {
		solution += fmt.Sprintf("\nFinal Answer: %s", lastNode.Thought)
	}

	return solution
}

// calculatePathConfidence calculates confidence for a solution path
func calculatePathConfidence(path []*ThoughtNode) float64 {
	if len(path) == 0 {
		return 0.0
	}

	totalScore := 0.0
	for _, node := range path {
		totalScore += node.Score
	}

	// Average score with depth penalty
	avgScore := totalScore / float64(len(path))
	depthPenalty := 1.0 / (1.0 + float64(len(path))*0.1)

	return avgScore * depthPenalty
}

// parseBranches extracts individual thoughts from response
func parseBranches(response string, expectedCount int) []string {
	thoughts := []string{}

	// Look for numbered items
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for numbered points
		if strings.HasPrefix(line, "1.") ||
			strings.HasPrefix(line, "2.") ||
			strings.HasPrefix(line, "3.") ||
			strings.HasPrefix(line, "4.") ||
			strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "•") {
			// Remove prefix
			thought := strings.TrimLeft(line, "1234567890.-• ")
			if len(thought) > 10 {
				thoughts = append(thoughts, thought)
			}
		}
	}

	// If not enough structured thoughts, split by sentences
	if len(thoughts) < expectedCount {
		sentences := strings.Split(response, ". ")
		for _, sent := range sentences {
			if len(thoughts) >= expectedCount {
				break
			}
			if len(sent) > 20 && !util.ContainsString(thoughts, sent) {
				thoughts = append(thoughts, sent)
			}
		}
	}

	// Limit to expected count
	if len(thoughts) > expectedCount {
		thoughts = thoughts[:expectedCount]
	}

	return thoughts
}

// min returns minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
