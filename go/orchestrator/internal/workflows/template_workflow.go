package workflows

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
)

// TemplateWorkflowInput carries data required to execute a compiled template.
type TemplateWorkflowInput struct {
	Task         TaskInput
	TemplateKey  string
	TemplateHash string
}

// TemplateWorkflow executes a pre-defined template plan deterministically.
func TemplateWorkflow(ctx workflow.Context, input TemplateWorkflowInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting TemplateWorkflow",
		"template_key", input.TemplateKey,
		"session_id", input.Task.SessionID,
	)

	entry, ok := TemplateRegistry().Get(input.TemplateKey)
	if !ok {
		logger.Error("Template key not found in registry", "template_key", input.TemplateKey)
		return TaskResult{Success: false, ErrorMessage: "template not found"}, fmt.Errorf("template key %s not found", input.TemplateKey)
	}

	if input.TemplateHash != "" && entry.ContentHash != input.TemplateHash {
		logger.Error("Template content hash mismatch",
			"expected", input.TemplateHash,
			"actual", entry.ContentHash,
		)
		return TaskResult{Success: false, ErrorMessage: "template hash mismatch"}, fmt.Errorf("template hash mismatch")
	}

	plan, err := templates.CompileTemplate(entry.Template)
	if err != nil {
		logger.Error("Failed to compile template", "template", entry.Key, "error", err)
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
	}
	plan.Checksum = entry.ContentHash

	taskInput := input.Task
	if plan.Defaults.RequireApproval != nil {
		taskInput.RequireApproval = *plan.Defaults.RequireApproval
	}
	if taskInput.Context == nil {
		taskInput.Context = map[string]interface{}{}
	}
	taskInput.Context["template_resolved"] = entry.Key
	taskInput.Context["template_content_hash"] = entry.ContentHash

	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	runtime := &templateRuntime{
		Task:         taskInput,
		Plan:         plan,
		Thresholds:   loadPatternThresholds(taskInput.Context),
		NodeResults:  make(map[string]TemplateNodeResult, len(plan.Nodes)),
		NodeOutputs:  make(map[string]string, len(plan.Nodes)),
		AgentResults: make([]activities.AgentExecutionResult, 0, len(plan.Nodes)),
	}

	for _, nodeID := range plan.Order {
		node, ok := plan.Nodes[nodeID]
		if !ok {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("node %s missing", nodeID)}, fmt.Errorf("node %s missing", nodeID)
		}
		result, err := executeTemplateNode(activityCtx, runtime, node)
		if err != nil {
			logger.Error("Template node execution failed",
				"node", node.ID,
				"error", err,
			)
			return TaskResult{Success: false, ErrorMessage: err.Error(), Metadata: runtime.summaryMetadata()}, err
		}
		runtime.RecordNodeResult(node.ID, result)
	}

	finalResult := runtime.FinalResult()
	metadata := runtime.summaryMetadata()

	if runtime.Task.SessionID != "" {
		if err := updateTemplateSession(ctx, runtime.Task, runtime.Plan, finalResult, runtime.TotalTokens, runtime.AgentResults); err != nil {
			logger.Warn("Failed to update session for template workflow", "error", err)
		}
	}

	// Emit WORKFLOW_COMPLETED before returning
	workflowID := runtime.Task.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
    _ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
        WorkflowID: workflowID,
        EventType:  activities.StreamEventWorkflowCompleted,
        AgentID:    "template",
        Message:    "All done",
        Timestamp:  workflow.Now(ctx),
    }).Get(ctx, nil)

	return TaskResult{
		Result:     finalResult,
		Success:    true,
		TokensUsed: runtime.TotalTokens,
		Metadata:   metadata,
	}, nil
}

// TemplateNodeResult captures per-node execution details.
type TemplateNodeResult struct {
	Result   string
	Success  bool
	Tokens   int
	Metadata map[string]interface{}
}

type templateRuntime struct {
	Task         TaskInput
	Plan         *templates.ExecutablePlan
	Thresholds   map[patterns.PatternType]int
	NodeResults  map[string]TemplateNodeResult
	NodeOutputs  map[string]string
	AgentResults []activities.AgentExecutionResult
	TotalTokens  int
}

func (rt *templateRuntime) RecordNodeResult(nodeID string, result TemplateNodeResult) {
	rt.NodeResults[nodeID] = result
	rt.NodeOutputs[nodeID] = result.Result
	rt.TotalTokens += result.Tokens
}

func (rt *templateRuntime) FinalResult() string {
	if len(rt.Plan.Order) == 0 {
		return ""
	}
	lastID := rt.Plan.Order[len(rt.Plan.Order)-1]
	if res, ok := rt.NodeResults[lastID]; ok {
		return res.Result
	}
	return ""
}

func (rt *templateRuntime) summaryMetadata() map[string]interface{} {
	nodeSummary := make(map[string]interface{}, len(rt.NodeResults))
	for id, res := range rt.NodeResults {
		nodeSummary[id] = map[string]interface{}{
			"result":   res.Result,
			"success":  res.Success,
			"tokens":   res.Tokens,
			"metadata": res.Metadata,
		}
	}
	return map[string]interface{}{
		"template": map[string]interface{}{
			"name":     rt.Plan.TemplateName,
			"version":  rt.Plan.TemplateVersion,
			"checksum": rt.Plan.Checksum,
			"nodes":    nodeSummary,
		},
	}
}

func executeTemplateNode(ctx workflow.Context, rt *templateRuntime, node templates.ExecutableNode) (TemplateNodeResult, error) {
	nodeContext := mergeContext(rt.Task.Context, node.Metadata)
	nodeContext["template_node_id"] = node.ID
	nodeContext["template_node_type"] = string(node.Type)
	nodeContext["template_results"] = cloneStringMap(rt.NodeOutputs)

	history := convertHistoryForAgent(rt.Task.History)
	query := determineNodeQuery(rt.Task.Query, node.Metadata)

	switch node.Type {
	case templates.NodeTypeSimple:
		return executeSimpleTemplateNode(ctx, rt, node, nodeContext, history, query)
	case templates.NodeTypeCognitive:
		return executeCognitiveTemplateNode(ctx, rt, node, nodeContext, history, query)
	case templates.NodeTypeDAG:
		return executeDAGTemplateNode(ctx, rt, node, nodeContext, history, query)
	case templates.NodeTypeSupervisor:
		return executeSupervisorTemplateNode(ctx, rt, node, nodeContext, history, query)
	default:
		return TemplateNodeResult{}, fmt.Errorf("unsupported node type: %s", node.Type)
	}
}

func executeSimpleTemplateNode(ctx workflow.Context, rt *templateRuntime, node templates.ExecutableNode, nodeContext map[string]interface{}, history []string, query string) (TemplateNodeResult, error) {
	nodeContext["template_node_strategy"] = string(node.Strategy)
	input := activities.ExecuteSimpleTaskInput{
		Query:            query,
		UserID:           rt.Task.UserID,
		SessionID:        rt.Task.SessionID,
		Context:          nodeContext,
		SessionCtx:       rt.Task.SessionCtx,
		History:          history,
		SuggestedTools:   append([]string(nil), node.ToolsAllowlist...),
		ParentWorkflowID: rt.Task.ParentWorkflowID,
	}

	var result activities.ExecuteSimpleTaskResult
	if err := workflow.ExecuteActivity(ctx, activities.ExecuteSimpleTask, input).Get(ctx, &result); err != nil {
		return TemplateNodeResult{}, err
	}
	if !result.Success {
		return TemplateNodeResult{}, fmt.Errorf("simple node %s failed: %s", node.ID, result.Error)
	}

	agentResult := activities.AgentExecutionResult{
		AgentID:        node.ID,
		Response:       result.Response,
		TokensUsed:     result.TokensUsed,
		ModelUsed:      result.ModelUsed,
		Success:        true,
		ToolExecutions: result.ToolExecutions,
	}
	rt.AgentResults = append(rt.AgentResults, agentResult)

	return TemplateNodeResult{
		Result:  result.Response,
		Success: true,
		Tokens:  result.TokensUsed,
		Metadata: map[string]interface{}{
			"model_used": result.ModelUsed,
			"type":       "simple",
		},
	}, nil
}

func executeCognitiveTemplateNode(ctx workflow.Context, rt *templateRuntime, node templates.ExecutableNode, nodeContext map[string]interface{}, history []string, query string) (TemplateNodeResult, error) {
	registry := patterns.GetRegistry()

	originalStrategy := patterns.PatternType(node.Strategy)
	appliedStrategy := originalStrategy
	if node.BudgetMax > 0 {
		if next, degraded := patterns.DegradeByBudget(originalStrategy, node.BudgetMax, rt.Thresholds); degraded {
			appliedStrategy = next
			workflow.GetLogger(ctx).Info("Pattern degraded due to budget",
				"node", node.ID,
				"from", string(originalStrategy),
				"to", string(appliedStrategy),
				"budget", node.BudgetMax,
			)
			ometrics.PatternDegraded.WithLabelValues(string(originalStrategy), string(appliedStrategy), rt.Plan.TemplateName, node.ID).Inc()
			nodeContext["template_strategy_degraded_from"] = string(originalStrategy)
			nodeContext["template_strategy_degraded_to"] = string(appliedStrategy)
		}
	}

	nodeContext["template_node_strategy"] = string(appliedStrategy)

	pattern, ok := registry.Get(appliedStrategy)
	if !ok {
		return TemplateNodeResult{}, fmt.Errorf("pattern %s not registered", appliedStrategy)
	}

	modelTier := determineModelTierForTemplate(rt.Plan.Defaults.ModelTier, node.Metadata)

	config := make(map[string]interface{}, len(node.Metadata)+2)
	for k, v := range node.Metadata {
		config[k] = v
	}
	if appliedStrategy != originalStrategy {
		config["degraded_from"] = string(originalStrategy)
		config["degraded_to"] = string(appliedStrategy)
	}

	patternInput := patterns.PatternInput{
		Query:     query,
		Context:   nodeContext,
		History:   history,
		SessionID: rt.Task.SessionID,
		UserID:    rt.Task.UserID,
		Config:    config,
		BudgetMax: node.BudgetMax,
		ModelTier: modelTier,
	}

	patternResult, err := pattern.Execute(ctx, patternInput)
	if err != nil {
		return TemplateNodeResult{}, err
	}

	if len(patternResult.AgentResults) > 0 {
		rt.AgentResults = append(rt.AgentResults, patternResult.AgentResults...)
	}

	metadata := map[string]interface{}{}
	for k, v := range patternResult.Metadata {
		metadata[k] = v
	}
	metadata["type"] = "cognitive"
	metadata["strategy"] = string(appliedStrategy)
	if appliedStrategy != originalStrategy {
		metadata["degraded_from"] = string(originalStrategy)
	}

	return TemplateNodeResult{
		Result:   patternResult.Result,
		Success:  true,
		Tokens:   patternResult.TokensUsed,
		Metadata: metadata,
	}, nil
}

func executeDAGTemplateNode(ctx workflow.Context, rt *templateRuntime, node templates.ExecutableNode, nodeContext map[string]interface{}, history []string, query string) (TemplateNodeResult, error) {
	nodeContext["template_node_mode"] = "dag"

	tasks, err := parseHybridTasks(node.Metadata)
	if err != nil {
		return TemplateNodeResult{}, err
	}

	if len(tasks) == 0 {
		aggregated := aggregateDependencyOutputs(rt, node)
		meta := map[string]interface{}{
			"type": "dag",
			"mode": "aggregate",
		}
		if len(node.DependsOn) > 0 {
			meta["dependencies"] = append([]string(nil), node.DependsOn...)
		}
		return TemplateNodeResult{
			Result:   aggregated,
			Success:  true,
			Tokens:   0,
			Metadata: meta,
		}, nil
	}

	budgetPerAgent := node.BudgetMax
	if value, ok := toInt(node.Metadata["budget_per_agent"]); ok && value > 0 {
		budgetPerAgent = value
	} else if budgetPerAgent == 0 {
		budgetPerAgent = rt.Plan.Defaults.BudgetAgentMax
	}

	maxConcurrency := len(tasks)
	if value, ok := toInt(node.Metadata["max_concurrency"]); ok && value > 0 {
		maxConcurrency = value
	}

	config := execution.HybridConfig{
		MaxConcurrency:           maxConcurrency,
		EmitEvents:               boolValue(node.Metadata["emit_events"]),
		Context:                  nodeContext,
		PassDependencyResults:    boolValue(node.Metadata["pass_dependency_results"]),
		ClearDependentToolParams: boolValue(node.Metadata["clear_dependent_tool_params"]),
	}

	if secs, ok := toInt(node.Metadata["dependency_wait_seconds"]); ok && secs > 0 {
		config.DependencyWaitTimeout = time.Duration(secs) * time.Second
	}

	modelTier := determineModelTierForTemplate(rt.Plan.Defaults.ModelTier, node.Metadata)

	for i := range tasks {
		if strings.TrimSpace(tasks[i].Description) == "" {
			tasks[i].Description = query
		}
		if len(tasks[i].SuggestedTools) == 0 && len(node.ToolsAllowlist) > 0 {
			tasks[i].SuggestedTools = append([]string(nil), node.ToolsAllowlist...)
		}
	}

	result, err := execution.ExecuteHybrid(ctx, tasks, rt.Task.SessionID, history, config, budgetPerAgent, rt.Task.UserID, modelTier)
	if err != nil {
		return TemplateNodeResult{}, err
	}
	if result == nil {
		return TemplateNodeResult{}, fmt.Errorf("hybrid execution returned no result")
	}

	mergedResult, metadata := summariseHybridResult(rt, node, result)
	return TemplateNodeResult{
		Result:   mergedResult,
		Success:  true,
		Tokens:   result.TotalTokens,
		Metadata: metadata,
	}, nil
}

func executeSupervisorTemplateNode(ctx workflow.Context, rt *templateRuntime, node templates.ExecutableNode, nodeContext map[string]interface{}, _ []string, query string) (TemplateNodeResult, error) {
	nodeContext["template_node_mode"] = "supervisor"

	childInput := TaskInput{
		Query:              query,
		UserID:             rt.Task.UserID,
		TenantID:           rt.Task.TenantID,
		SessionID:          rt.Task.SessionID,
		Context:            nodeContext,
		Mode:               rt.Task.Mode,
		History:            rt.Task.History,
		SessionCtx:         rt.Task.SessionCtx,
		RequireApproval:    rt.Task.RequireApproval,
		ApprovalTimeout:    rt.Task.ApprovalTimeout,
		BypassSingleResult: rt.Task.BypassSingleResult,
		ParentWorkflowID:   workflow.GetInfo(ctx).WorkflowExecution.ID,
		SuggestedTools:     append([]string(nil), node.ToolsAllowlist...),
	}

	if tools := stringSlice(node.Metadata["tools_allowlist"]); len(tools) > 0 {
		childInput.SuggestedTools = tools
	}
	if params := mapStringInterface(node.Metadata["tool_parameters"]); params != nil {
		childInput.ToolParameters = params
	}
	if mode := stringValue(node.Metadata["mode"]); mode != "" {
		childInput.Mode = mode
	}
	if approvalRequired, ok := metadataBool(node.Metadata["require_approval"]); ok {
		childInput.RequireApproval = approvalRequired
	}
	if timeout, ok := toInt(node.Metadata["approval_timeout_seconds"]); ok && timeout >= 0 {
		childInput.ApprovalTimeout = timeout
	}

	var childResult TaskResult
	if err := workflow.ExecuteChildWorkflow(ctx, SupervisorWorkflow, childInput).Get(ctx, &childResult); err != nil {
		return TemplateNodeResult{}, err
	}
	if !childResult.Success {
		return TemplateNodeResult{}, fmt.Errorf("supervisor node %s failed: %s", node.ID, childResult.ErrorMessage)
	}

	metadata := map[string]interface{}{
		"type": "supervisor",
	}
	for k, v := range childResult.Metadata {
		metadata[k] = v
	}

	return TemplateNodeResult{
		Result:   childResult.Result,
		Success:  true,
		Tokens:   childResult.TokensUsed,
		Metadata: metadata,
	}, nil
}

func determineModelTierForTemplate(defaultTier string, metadata map[string]interface{}) string {
	if metadata != nil {
		if v, ok := metadata["model_tier"].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	tier := strings.TrimSpace(defaultTier)
	if tier == "" {
		tier = "medium"
	}
	return tier
}

func determineNodeQuery(defaultQuery string, metadata map[string]interface{}) string {
	if metadata != nil {
		if v, ok := metadata["query"].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return defaultQuery
}

func mergeContext(base map[string]interface{}, overlay map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func loadPatternThresholds(context map[string]interface{}) map[patterns.PatternType]int {
	thresholds := patterns.DegradationThresholds()
	if context == nil {
		return thresholds
	}

	raw, ok := context["pattern_degradation_thresholds"]
	if !ok {
		return thresholds
	}

	data, ok := raw.(map[string]interface{})
	if !ok {
		return thresholds
	}

	for key, value := range data {
		pt := patterns.PatternType(strings.TrimSpace(key))
		if pt == "" {
			continue
		}
		if _, exists := thresholds[pt]; !exists {
			continue
		}
		if iv, ok := toInt(value); ok && iv >= 0 {
			thresholds[pt] = iv
		}
	}
	return thresholds
}

func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case string:
		if val == "" {
			return 0, false
		}
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func parseHybridTasks(metadata map[string]interface{}) ([]execution.HybridTask, error) {
	if metadata == nil {
		return nil, nil
	}
	raw, ok := metadata["tasks"]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata.tasks must be an array")
	}
	tasks := make([]execution.HybridTask, 0, len(list))
	for i, item := range list {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("metadata.tasks[%d] must be an object", i)
		}
		id := stringValue(entry["id"])
		if id == "" {
			return nil, fmt.Errorf("metadata.tasks[%d].id is required", i)
		}
		description := firstNonEmptyString(entry["description"], entry["query"], entry["prompt"])
		tasks = append(tasks, execution.HybridTask{
			ID:             id,
			Description:    description,
			SuggestedTools: stringSlice(preferValue(entry["tools"], entry["tools_allowlist"])),
			ToolParameters: mapStringInterface(entry["tool_parameters"]),
			PersonaID:      stringValue(entry["persona_id"]),
			Role:           stringValue(entry["role"]),
			Dependencies:   stringSlice(entry["depends_on"]),
		})
	}
	return tasks, nil
}

func firstNonEmptyString(values ...interface{}) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func preferValue(primary, fallback interface{}) interface{} {
	if primary != nil {
		return primary
	}
	return fallback
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func stringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch raw := v.(type) {
	case []string:
		out := make([]string, len(raw))
		for i, s := range raw {
			out[i] = strings.TrimSpace(s)
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := stringValue(v); s != "" {
			return []string{s}
		}
	}
	return nil
}

func mapStringInterface(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		clone := make(map[string]interface{}, len(m))
		for k, val := range m {
			clone[k] = val
		}
		return clone
	}
	return nil
}

func boolValue(v interface{}) bool {
	if b, ok := metadataBool(v); ok {
		return b
	}
	return false
}

func metadataBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(val))
		if trimmed == "" {
			return false, false
		}
		if trimmed == "true" || trimmed == "1" || trimmed == "yes" {
			return true, true
		}
		if trimmed == "false" || trimmed == "0" || trimmed == "no" {
			return false, true
		}
	}
	return false, false
}

func aggregateDependencyOutputs(rt *templateRuntime, node templates.ExecutableNode) string {
	if len(node.DependsOn) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, dep := range node.DependsOn {
		if dep == "" {
			continue
		}
		if res, ok := rt.NodeResults[dep]; ok {
			if builder.Len() > 0 {
				builder.WriteString("\n\n")
			}
			builder.WriteString("[")
			builder.WriteString(dep)
			builder.WriteString("]\n")
			builder.WriteString(res.Result)
		}
	}
	return builder.String()
}

func summariseHybridResult(rt *templateRuntime, node templates.ExecutableNode, result *execution.HybridResult) (string, map[string]interface{}) {
	if result == nil {
		return "", map[string]interface{}{"type": "dag", "mode": "hybrid"}
	}
	ids := make([]string, 0, len(result.Results))
	for id := range result.Results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var builder strings.Builder
	for _, id := range ids {
		r := result.Results[id]
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("[")
		builder.WriteString(id)
		builder.WriteString("]\n")
		builder.WriteString(r.Response)
		rt.AgentResults = append(rt.AgentResults, r)
	}
	meta := make(map[string]interface{}, len(result.Metadata)+4)
	for k, v := range result.Metadata {
		meta[k] = v
	}
	meta["type"] = "dag"
	meta["mode"] = "hybrid"
	meta["tasks"] = ids
	if len(node.DependsOn) > 0 {
		meta["dependencies"] = append([]string(nil), node.DependsOn...)
	}
	return builder.String(), meta
}

func updateTemplateSession(ctx workflow.Context, task TaskInput, plan *templates.ExecutablePlan, result string, tokens int, agentResults []activities.AgentExecutionResult) error {
	// Create activity context with proper timeout
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	usages := make([]activities.AgentUsage, 0, len(agentResults))
	for _, ar := range agentResults {
		usages = append(usages, activities.AgentUsage{
			Model:        ar.ModelUsed,
			Tokens:       ar.TokensUsed,
			InputTokens:  ar.InputTokens,
			OutputTokens: ar.OutputTokens,
		})
	}

	var updateRes activities.SessionUpdateResult
	if err := workflow.ExecuteActivity(activityCtx, constants.UpdateSessionResultActivity, activities.SessionUpdateInput{
		SessionID:  task.SessionID,
		Result:     result,
		TokensUsed: tokens,
		AgentsUsed: len(agentResults),
		AgentUsage: usages,
	}).Get(ctx, &updateRes); err != nil {
		return err
	}

	recordTemplateToVectorStore(ctx, task, plan, result)
	return nil
}

func recordTemplateToVectorStore(ctx workflow.Context, task TaskInput, plan *templates.ExecutablePlan, answer string) {
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	detachedCtx = workflow.WithActivityOptions(detachedCtx, activityOpts)
	metadata := map[string]interface{}{
		"workflow":          "template_v1",
		"template_name":     plan.TemplateName,
		"template_version":  plan.TemplateVersion,
		"template_checksum": plan.Checksum,
		"tenant_id":         task.TenantID,
	}
	workflow.ExecuteActivity(detachedCtx,
		activities.RecordQuery,
		activities.RecordQueryInput{
			SessionID: task.SessionID,
			UserID:    task.UserID,
			TenantID:  task.TenantID,
			Query:     task.Query,
			Answer:    answer,
			Model:     determineModelTierForTemplate(plan.Defaults.ModelTier, nil),
			Metadata:  metadata,
			RedactPII: true,
		})
}
