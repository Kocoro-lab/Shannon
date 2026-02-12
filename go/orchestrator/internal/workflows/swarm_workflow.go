package workflows

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// isTransientError classifies tool errors as transient (rate limit, timeout) vs permanent.
// Transient errors warrant backoff and retry; permanent errors count toward abort threshold.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientPatterns := []string{"rate limit", "429", "timeout", "timed out", "temporary", "unavailable", "503", "502"}
	for _, p := range transientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// ── AgentLoop ──────────────────────────────────────────────────────────────────

// TeamMember describes a teammate visible to each agent for collaboration.
type TeamMember struct {
	AgentID string `json:"agent_id"`
	Task    string `json:"task"`
}

// AgentLoopInput is the input for a persistent agent loop.
type AgentLoopInput struct {
	AgentID               string                 `json:"agent_id"`
	WorkflowID            string                 `json:"workflow_id"`
	Task                  string                 `json:"task"`
	MaxIterations         int                    `json:"max_iterations"`
	SessionID             string                 `json:"session_id,omitempty"`
	Context               map[string]interface{} `json:"context,omitempty"`
	TeamRoster            []TeamMember           `json:"team_roster,omitempty"`
	WorkspaceMaxEntries   int                    `json:"workspace_max_entries,omitempty"`
	WorkspaceSnippetChars int                    `json:"workspace_snippet_chars,omitempty"`
	MaxMessagesPerAgent   int                    `json:"max_messages_per_agent,omitempty"`
}

// AgentLoopResult is the final result from a persistent agent.
type AgentLoopResult struct {
	AgentID      string `json:"agent_id"`
	Response     string `json:"response"`
	Iterations   int    `json:"iterations"`
	TokensUsed   int    `json:"tokens_used"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ModelUsed    string `json:"model_used"`
	Provider     string `json:"provider"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
}

// AgentLoop is a persistent agent workflow that runs a reason-act cycle.
// Each iteration: check mailbox → call LLM → execute action → loop.
func AgentLoop(ctx workflow.Context, input AgentLoopInput) (AgentLoopResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("AgentLoop started",
		"agent_id", input.AgentID,
		"task", input.Task,
		"max_iterations", input.MaxIterations,
	)

	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	// Short timeout for P2P activities
	p2pCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Emit agent started event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: input.WorkflowID,
		EventType:  activities.StreamEventAgentStarted,
		AgentID:    input.AgentID,
		Message:    activities.MsgAgentStarted(input.AgentID),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Convert team roster to activity-level type
	var teamRoster []activities.TeamMemberInfo
	for _, tm := range input.TeamRoster {
		teamRoster = append(teamRoster, activities.TeamMemberInfo{
			AgentID: tm.AgentID,
			Task:    tm.Task,
		})
	}

	// Apply guardrail defaults if not set
	wsMaxEntries := input.WorkspaceMaxEntries
	if wsMaxEntries <= 0 {
		wsMaxEntries = 5
	}
	wsSnippetChars := input.WorkspaceSnippetChars
	if wsSnippetChars <= 0 {
		wsSnippetChars = 800
	}
	maxMessages := input.MaxMessagesPerAgent
	if maxMessages <= 0 {
		maxMessages = 20
	}

	var history []activities.AgentLoopTurn
	var totalTokens, totalInput, totalOutput int
	var lastModel, lastProvider string
	var lastWorkspaceSeq uint64
	var messagesSent int                       // Track messages sent for per-agent cap enforcement
	var consecutiveToolErrors int              // Track consecutive tool failures to prevent infinite loops
	var consecutiveNonToolActions int          // Track iterations without tool calls for convergence detection
	var consecutiveTransientErrors int         // Track transient errors for escalating backoff

	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		logger.Info("AgentLoop iteration", "agent_id", input.AgentID, "iteration", iteration)

		// Step 1: Check mailbox for incoming messages
		var mailboxMsgs []activities.AgentMailboxMsg
		var fetchedMsgs []activities.AgentMessage
		fetchErr := workflow.ExecuteActivity(p2pCtx, constants.FetchAgentMessagesActivity, activities.FetchAgentMessagesInput{
			WorkflowID: input.WorkflowID,
			AgentID:    input.AgentID,
		}).Get(ctx, &fetchedMsgs)
		if fetchErr != nil {
			logger.Warn("AgentLoop mailbox fetch failed", "agent_id", input.AgentID, "error", fetchErr)
		}
		if fetchErr == nil && len(fetchedMsgs) > 0 {
			for _, m := range fetchedMsgs {
				mailboxMsgs = append(mailboxMsgs, activities.AgentMailboxMsg{
					From:    m.From,
					Type:    string(m.Type),
					Payload: m.Payload,
				})
			}
			logger.Info("AgentLoop received mailbox messages",
				"agent_id", input.AgentID,
				"count", len(mailboxMsgs),
			)
		}

		// Step 1b: Fetch shared workspace entries from ALL topics (findings from other agents)
		var wsSnippets []activities.WorkspaceSnippet
		var wsEntries []activities.WorkspaceEntry
		wsErr := workflow.ExecuteActivity(p2pCtx, constants.WorkspaceListAllActivity, activities.WorkspaceListAllInput{
			WorkflowID: input.WorkflowID,
			SinceSeq:   lastWorkspaceSeq,
			MaxEntries: wsMaxEntries,
		}).Get(ctx, &wsEntries)
		if wsErr != nil {
			logger.Warn("AgentLoop workspace fetch failed", "agent_id", input.AgentID, "error", wsErr)
		}
		if wsErr == nil && len(wsEntries) > 0 {
			for _, e := range wsEntries {
				author, _ := e.Entry["author"].(string)
				data, _ := e.Entry["data"].(string)
				// Truncate to control token usage (rune-safe to avoid splitting UTF-8)
				if runeData := []rune(data); len(runeData) > wsSnippetChars {
					data = string(runeData[:wsSnippetChars]) + "..."
				}
				wsSnippets = append(wsSnippets, activities.WorkspaceSnippet{
					Author: author,
					Data:   data,
					Seq:    e.Seq,
				})
				if e.Seq > lastWorkspaceSeq {
					lastWorkspaceSeq = e.Seq
				}
			}
			logger.Info("AgentLoop fetched workspace entries",
				"agent_id", input.AgentID,
				"count", len(wsSnippets),
			)
		}

		// Step 2: Call LLM to decide next action
		var stepResult activities.AgentLoopStepResult
		if err := workflow.ExecuteActivity(ctx, constants.AgentLoopStepActivity, activities.AgentLoopStepInput{
			AgentID:       input.AgentID,
			WorkflowID:    input.WorkflowID,
			Task:          input.Task,
			Iteration:     iteration,
			MaxIterations: input.MaxIterations,
			Messages:      mailboxMsgs,
			History:       history,
			Context:       input.Context,
			SessionID:     input.SessionID,
			TeamRoster:    teamRoster,
			WorkspaceData: wsSnippets,
		}).Get(ctx, &stepResult); err != nil {
			logger.Error("AgentLoop LLM step failed", "agent_id", input.AgentID, "error", err)
			return AgentLoopResult{
				AgentID: input.AgentID,
				Success: false,
				Error:   fmt.Sprintf("LLM step failed at iteration %d: %v", iteration, err),
			}, nil
		}

		// Track token usage
		totalTokens += stepResult.TokensUsed
		totalInput += stepResult.InputTokens
		totalOutput += stepResult.OutputTokens
		if stepResult.ModelUsed != "" {
			lastModel = stepResult.ModelUsed
		}
		if stepResult.Provider != "" {
			lastProvider = stepResult.Provider
		}

		// Force done on last iteration if LLM didn't choose it
		if iteration == input.MaxIterations-1 && stepResult.Action != "done" {
			logger.Warn("Forcing done on last iteration",
				"agent_id", input.AgentID,
				"original_action", stepResult.Action,
			)
			// Build summary from last 3 iterations only (avoid blowing synthesis context)
			var histSummary string
			startIdx := len(history) - 3
			if startIdx < 0 {
				startIdx = 0
			}
			for _, h := range history[startIdx:] {
				s := fmt.Sprintf("%v", h.Result)
				if s == "" || s == "<nil>" {
					continue
				}
				if len(s) > 2000 {
					s = s[:2000] + "..."
				}
				histSummary += fmt.Sprintf("[Iteration %d - %s]: %s\n", h.Iteration, h.Action, s)
			}
			if histSummary == "" {
				histSummary = fmt.Sprintf("Agent %s reached iteration limit. Last action was: %s", input.AgentID, stepResult.Action)
			} else {
				histSummary = fmt.Sprintf("Agent %s reached iteration limit (%d iterations). Partial findings from last 3 iterations:\n%s",
					input.AgentID, input.MaxIterations, histSummary)
			}
			stepResult.Action = "done"
			stepResult.Response = histSummary
		}

		// Step 3: Execute the action
		switch stepResult.Action {
		case "done":
			if len(stepResult.Response) > 2000 {
				logger.Warn("Agent returned oversized done response — should save to file instead",
					"agent_id", input.AgentID,
					"response_len", len(stepResult.Response),
				)
			}
			logger.Info("AgentLoop completed", "agent_id", input.AgentID, "iterations", iteration+1)

			// Emit agent completed event
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: input.WorkflowID,
				EventType:  activities.StreamEventAgentCompleted,
				AgentID:    input.AgentID,
				Message:    activities.MsgAgentCompleted(input.AgentID),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)

			return AgentLoopResult{
				AgentID:      input.AgentID,
				Response:     stepResult.Response,
				Iterations:   iteration + 1,
				TokensUsed:   totalTokens,
				InputTokens:  totalInput,
				OutputTokens: totalOutput,
				ModelUsed:    lastModel,
				Provider:     lastProvider,
				Success:      true,
			}, nil

		case "tool_call":
			consecutiveNonToolActions = 0 // Tool use = progress
			// Execute tool via standard agent execution (one-shot)
			var toolRes activities.AgentExecutionResult
			toolErr := workflow.ExecuteActivity(ctx, activities.ExecuteAgent, activities.AgentExecutionInput{
				Query:            fmt.Sprintf("Execute tool %s with params: %v", stepResult.Tool, stepResult.ToolParams),
				AgentID:          input.AgentID,
				SuggestedTools:   []string{stepResult.Tool},
				ToolParameters:   stepResult.ToolParams,
				SessionID:        input.SessionID,
				ParentWorkflowID: input.WorkflowID,
			}).Get(ctx, &toolRes)

			turnResult := interface{}(nil)
			if toolErr == nil {
				turnResult = toolRes.Response
				consecutiveToolErrors = 0
				consecutiveTransientErrors = 0
			} else {
				if isTransientError(toolErr) {
					consecutiveTransientErrors++
					backoff := time.Duration(consecutiveTransientErrors) * 5 * time.Second
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					logger.Warn("Transient tool error, backing off",
						"agent_id", input.AgentID,
						"tool", stepResult.Tool,
						"backoff", backoff,
						"attempt", consecutiveTransientErrors,
						"error", toolErr,
					)
					_ = workflow.Sleep(ctx, backoff)
					turnResult = fmt.Sprintf("transient error (will retry): %v", toolErr)
				} else {
					turnResult = fmt.Sprintf("tool error: %v", toolErr)
					consecutiveToolErrors++
				}
			}
			history = append(history, activities.AgentLoopTurn{
				Iteration: iteration,
				Action:    fmt.Sprintf("tool_call:%s", stepResult.Tool),
				Result:    turnResult,
			})

			// Bail out if agent is stuck in a loop of failing tool calls (permanent errors only)
			if consecutiveToolErrors >= 3 {
				logger.Warn("AgentLoop aborting: 3 consecutive permanent tool errors",
					"agent_id", input.AgentID,
					"last_tool", stepResult.Tool,
				)
				return AgentLoopResult{
					AgentID:      input.AgentID,
					Response:     fmt.Sprintf("Agent stopped after %d consecutive tool failures. Last attempted: %s", consecutiveToolErrors, stepResult.Tool),
					Iterations:   iteration + 1,
					TokensUsed:   totalTokens,
					InputTokens:  totalInput,
					OutputTokens: totalOutput,
					ModelUsed:    lastModel,
					Provider:     lastProvider,
					Success:      false,
					Error:        "consecutive tool errors",
				}, nil
			}

		case "send_message":
			consecutiveNonToolActions++
			if messagesSent >= maxMessages {
				logger.Warn("AgentLoop message cap reached, skipping send",
					"agent_id", input.AgentID, "cap", maxMessages, "to", stepResult.To)
				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    fmt.Sprintf("send_message:%s", stepResult.To),
					Result:    fmt.Sprintf("dropped: message cap reached (%d/%d)", messagesSent, maxMessages),
				})
			} else {
				// Send message to another agent via P2P mailbox
				_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
					WorkflowID: input.WorkflowID,
					From:       input.AgentID,
					To:         stepResult.To,
					Type:       activities.MessageType(stepResult.MessageType),
					Payload:    stepResult.Payload,
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
				messagesSent++

				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    fmt.Sprintf("send_message:%s", stepResult.To),
					Result:    "sent",
				})
			}

		case "publish_data":
			consecutiveNonToolActions++
			// Publish to workspace topic
			_ = workflow.ExecuteActivity(p2pCtx, constants.WorkspaceAppendActivity, activities.WorkspaceAppendInput{
				WorkflowID: input.WorkflowID,
				Topic:      stepResult.Topic,
				Entry: map[string]interface{}{
					"author": input.AgentID,
					"data":   stepResult.Data,
				},
				Timestamp: workflow.Now(ctx),
			}).Get(ctx, nil)

			history = append(history, activities.AgentLoopTurn{
				Iteration: iteration,
				Action:    fmt.Sprintf("publish_data:%s", stepResult.Topic),
				Result:    "published",
			})

		case "request_help":
			consecutiveNonToolActions++
			if messagesSent >= maxMessages {
				logger.Warn("AgentLoop message cap reached, skipping help request",
					"agent_id", input.AgentID, "cap", maxMessages)
				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    "request_help",
					Result:    fmt.Sprintf("dropped: message cap reached (%d/%d)", messagesSent, maxMessages),
				})
			} else {
				// Send help request to supervisor via mailbox
				payload := map[string]interface{}{
					"description": stepResult.HelpDescription,
					"skills":      stepResult.HelpSkills,
					"from":        input.AgentID,
				}
				_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
					WorkflowID: input.WorkflowID,
					From:       input.AgentID,
					To:         "supervisor",
					Type:       activities.MessageTypeRequest,
					Payload:    payload,
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
				messagesSent++

				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    "request_help",
					Result:    stepResult.HelpDescription,
				})
			}

		default:
			// Treat unrecognized actions (file_write, file_read, web_search, etc.)
			// as tool_call where the action name is the tool name. LLMs sometimes
			// return the tool name directly instead of wrapping in tool_call.
			toolName := stepResult.Action
			if toolName == "" {
				consecutiveNonToolActions++
				logger.Warn("Empty action from LLM", "agent_id", input.AgentID)
				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    "unknown:empty",
					Result:    "skipped",
				})
			} else {
				consecutiveNonToolActions = 0 // Implicit tool use = progress
				logger.Info("Treating action as tool_call", "action", toolName, "agent_id", input.AgentID)
				params := stepResult.ToolParams
				if len(params) == 0 {
					// Some actions put params in payload or data fields
					params = make(map[string]interface{})
					if stepResult.Data != "" {
						params["content"] = stepResult.Data
					}
					if stepResult.Topic != "" {
						params["path"] = stepResult.Topic
					}
				}
				var toolRes activities.AgentExecutionResult
				toolErr := workflow.ExecuteActivity(ctx, activities.ExecuteAgent, activities.AgentExecutionInput{
					Query:            fmt.Sprintf("Execute tool %s with params: %v", toolName, params),
					AgentID:          input.AgentID,
					SuggestedTools:   []string{toolName},
					ToolParameters:   params,
					SessionID:        input.SessionID,
					ParentWorkflowID: input.WorkflowID,
				}).Get(ctx, &toolRes)

				turnResult := interface{}(nil)
				if toolErr == nil {
					turnResult = toolRes.Response
					consecutiveToolErrors = 0
					consecutiveTransientErrors = 0
				} else {
					if isTransientError(toolErr) {
						consecutiveTransientErrors++
						backoff := time.Duration(consecutiveTransientErrors) * 5 * time.Second
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
						logger.Warn("Transient tool error, backing off",
							"agent_id", input.AgentID,
							"tool", toolName,
							"backoff", backoff,
							"attempt", consecutiveTransientErrors,
							"error", toolErr,
						)
						_ = workflow.Sleep(ctx, backoff)
						turnResult = fmt.Sprintf("transient error (will retry): %v", toolErr)
					} else {
						turnResult = fmt.Sprintf("tool error: %v", toolErr)
						consecutiveToolErrors++
						logger.Warn("Tool execution failed",
							"agent_id", input.AgentID,
							"tool", toolName,
							"consecutive_errors", consecutiveToolErrors,
							"error", toolErr,
						)
					}
				}
				history = append(history, activities.AgentLoopTurn{
					Iteration: iteration,
					Action:    fmt.Sprintf("tool_call:%s", toolName),
					Result:    turnResult,
				})

				// Bail out if agent is stuck in a loop of failing tool calls (permanent errors only)
				if consecutiveToolErrors >= 3 {
					logger.Warn("AgentLoop aborting: 3 consecutive permanent tool errors",
						"agent_id", input.AgentID,
						"last_tool", toolName,
					)
					return AgentLoopResult{
						AgentID:      input.AgentID,
						Response:     fmt.Sprintf("Agent stopped after %d consecutive tool failures. Last attempted: %s", consecutiveToolErrors, toolName),
						Iterations:   iteration + 1,
						TokensUsed:   totalTokens,
						InputTokens:  totalInput,
						OutputTokens: totalOutput,
						ModelUsed:    lastModel,
						Provider:     lastProvider,
						Success:      false,
						Error:        "consecutive tool errors",
					}, nil
				}
			}
		}

		// Convergence detection: if agent hasn't used tools for 3 consecutive iterations,
		// it's likely stuck in a reasoning loop without making progress (Claude Code pattern)
		if consecutiveNonToolActions >= 3 {
			logger.Warn("AgentLoop converged: no tool use for 3 consecutive iterations",
				"agent_id", input.AgentID,
				"iteration", iteration,
			)
			// Build partial findings from history
			var summary string
			startIdx := len(history) - 3
			if startIdx < 0 {
				startIdx = 0
			}
			for _, h := range history[startIdx:] {
				s := fmt.Sprintf("%v", h.Result)
				if s == "" || s == "<nil>" {
					continue
				}
				if len(s) > 2000 {
					s = s[:2000] + "..."
				}
				summary += fmt.Sprintf("[%s]: %s\n", h.Action, s)
			}
			if summary == "" {
				summary = fmt.Sprintf("Agent %s converged after %d iterations with no further tool use.", input.AgentID, iteration+1)
			}
			return AgentLoopResult{
				AgentID:      input.AgentID,
				Response:     summary,
				Iterations:   iteration + 1,
				TokensUsed:   totalTokens,
				InputTokens:  totalInput,
				OutputTokens: totalOutput,
				ModelUsed:    lastModel,
				Provider:     lastProvider,
				Success:      true,
			}, nil
		}

		// Emit progress event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: input.WorkflowID,
			EventType:  activities.StreamEventProgress,
			AgentID:    input.AgentID,
			Message:    activities.MsgAgentProgress(input.AgentID, iteration+1, input.MaxIterations, stepResult.Action),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
	}

	// Max iterations reached — return partial success so synthesis can use whatever was collected
	logger.Warn("AgentLoop max iterations reached", "agent_id", input.AgentID)
	var partialResponse string
	for _, h := range history {
		if s, ok := h.Result.(string); ok && s != "" {
			partialResponse += s + "\n"
		}
	}
	if partialResponse == "" {
		partialResponse = "Max iterations reached without completing task"
	}
	return AgentLoopResult{
		AgentID:      input.AgentID,
		Response:     partialResponse,
		Iterations:   input.MaxIterations,
		TokensUsed:   totalTokens,
		InputTokens:  totalInput,
		OutputTokens: totalOutput,
		ModelUsed:    lastModel,
		Provider:     lastProvider,
		Success:      true,
	}, nil
}

// ── SwarmWorkflow ──────────────────────────────────────────────────────────────

// SwarmWorkflow orchestrates persistent AgentLoop child workflows with
// inter-agent messaging and dynamic spawn capability.
func SwarmWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	if input.ParentWorkflowID != "" {
		workflowID = input.ParentWorkflowID
	}

	logger.Info("SwarmWorkflow started", "query", input.Query, "workflow_id", workflowID)

	// Activity options
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	p2pCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Emit workflow started
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmStarted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Load swarm config
	var cfg activities.WorkflowConfig
	if err := workflow.ExecuteActivity(ctx, activities.GetWorkflowConfig).Get(ctx, &cfg); err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		cfg.SwarmMaxAgents = 10
		cfg.SwarmMaxIterationsPerAgent = 25
		cfg.SwarmAgentTimeoutSeconds = 600
	}

	// Phase 1: Decompose task
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmPlanning(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	var decomp activities.DecompositionResult
	if input.PreplannedDecomposition != nil {
		decomp = *input.PreplannedDecomposition
	} else {
		decompInput := activities.DecompositionInput{
			Query:   input.Query,
			Context: input.Context,
		}
		if err := workflow.ExecuteActivity(ctx, constants.DecomposeTaskActivity, decompInput).Get(ctx, &decomp); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Decomposition failed: %v", err)}, err
		}
	}

	if len(decomp.Subtasks) == 0 {
		return TaskResult{Success: false, ErrorMessage: "No subtasks generated"}, nil
	}

	// Limit to max agents
	subtasks := decomp.Subtasks
	if len(subtasks) > cfg.SwarmMaxAgents {
		subtasks = subtasks[:cfg.SwarmMaxAgents]
	}

	logger.Info("SwarmWorkflow decomposed task",
		"subtask_count", len(subtasks),
		"max_agents", cfg.SwarmMaxAgents,
	)

	// Phase 2: Build team roster and spawn AgentLoops
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmSpawning(len(subtasks)),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Build team roster so each agent knows its teammates
	var roster []TeamMember
	for i, st := range subtasks {
		roster = append(roster, TeamMember{
			AgentID: agents.GetAgentName(workflowID, i),
			Task:    st.Description,
		})
	}

	type agentFuture struct {
		ID     string
		Future workflow.ChildWorkflowFuture
	}
	var agentFutures []agentFuture

	for i, st := range subtasks {
		agentName := agents.GetAgentName(workflowID, i)

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowExecutionTimeout: time.Duration(cfg.SwarmAgentTimeoutSeconds) * time.Second,
		})

		future := workflow.ExecuteChildWorkflow(childCtx, AgentLoop, AgentLoopInput{
			AgentID:               agentName,
			WorkflowID:            workflowID,
			Task:                  st.Description,
			MaxIterations:         cfg.SwarmMaxIterationsPerAgent,
			SessionID:             input.SessionID,
			Context:               input.Context,
			TeamRoster:            roster,
			WorkspaceMaxEntries:   cfg.SwarmWorkspaceMaxEntries,
			WorkspaceSnippetChars: cfg.SwarmWorkspaceSnippetChars,
			MaxMessagesPerAgent:   cfg.SwarmMaxMessagesPerAgent,
		})

		agentFutures = append(agentFutures, agentFuture{ID: agentName, Future: future})
		logger.Info("Spawned AgentLoop", "agent_id", agentName, "task", st.Description)
	}

	// Phase 3: Monitor — poll supervisor mailbox and wait for agents
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmMonitoring(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Track dynamic spawns to prevent infinite spawn loops
	dynamicSpawnCount := 0
	maxDynamicSpawns := cfg.SwarmMaxAgents - len(subtasks) // remaining capacity
	if maxDynamicSpawns < 0 {
		maxDynamicSpawns = 0
	}
	spawnedByAgent := make(map[string]bool) // track which agents have already spawned a helper (max 1 per agent)

	// Collect results using a completion channel
	results := make(map[string]AgentLoopResult)
	completionCh := workflow.NewChannel(ctx)

	// Start a goroutine for each agent to wait for its result
	for _, af := range agentFutures {
		agentID := af.ID
		fut := af.Future
		workflow.Go(ctx, func(gCtx workflow.Context) {
			var result AgentLoopResult
			if err := fut.Get(gCtx, &result); err != nil {
				result = AgentLoopResult{
					AgentID: agentID,
					Success: false,
					Error:   err.Error(),
				}
			}
			completionCh.Send(gCtx, result)
		})
	}

	// Wait for all agents to complete, checking supervisor mailbox periodically
	completedCount := 0
	totalExpected := len(agentFutures)

	for completedCount < totalExpected {
		sel := workflow.NewSelector(ctx)

		// Check for agent completions
		sel.AddReceive(completionCh, func(ch workflow.ReceiveChannel, more bool) {
			var result AgentLoopResult
			ch.Receive(ctx, &result)
			results[result.AgentID] = result
			completedCount++
			logger.Info("Agent completed",
				"agent_id", result.AgentID,
				"success", result.Success,
				"completed", completedCount,
				"total", totalExpected,
			)
		})

		// Periodically check supervisor mailbox for help/spawn requests
		timerFuture := workflow.NewTimer(ctx, 3*time.Second)
		sel.AddFuture(timerFuture, func(f workflow.Future) {
			_ = f.Get(ctx, nil) // Consume timer
			var supervisorMsgs []activities.AgentMessage
			fetchErr := workflow.ExecuteActivity(p2pCtx, constants.FetchAgentMessagesActivity, activities.FetchAgentMessagesInput{
				WorkflowID: workflowID,
				AgentID:    "supervisor",
			}).Get(ctx, &supervisorMsgs)

			if fetchErr == nil && len(supervisorMsgs) > 0 {
				for _, msg := range supervisorMsgs {
					logger.Info("Supervisor received message",
						"from", msg.From,
						"type", msg.Type,
					)

					if msg.Type == activities.MessageTypeRequest && dynamicSpawnCount < maxDynamicSpawns {
						// Each agent can only spawn one helper (prevents request_help loops)
						if spawnedByAgent[msg.From] {
							logger.Info("Ignoring duplicate spawn request", "from", msg.From)
							continue
						}
						desc, _ := msg.Payload["description"].(string)
						if desc == "" {
							desc = fmt.Sprintf("Help request from %s", msg.From)
						}
						spawnedByAgent[msg.From] = true
						newAgentName := agents.GetAgentName(workflowID, agents.IdxSwarmDynamicBase+dynamicSpawnCount)

						// Build updated roster including the new agent
						updatedRoster := make([]TeamMember, len(roster), len(roster)+1)
						copy(updatedRoster, roster)
						updatedRoster = append(updatedRoster, TeamMember{
							AgentID: newAgentName,
							Task:    desc,
						})

						// Strip force_swarm from context to prevent recursive swarm spawning
						spawnContext := make(map[string]interface{})
						for k, v := range input.Context {
							if k != "force_swarm" {
								spawnContext[k] = v
							}
						}

						spawnCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
							WorkflowExecutionTimeout: time.Duration(cfg.SwarmAgentTimeoutSeconds) * time.Second,
						})
						future := workflow.ExecuteChildWorkflow(spawnCtx, AgentLoop, AgentLoopInput{
							AgentID:               newAgentName,
							WorkflowID:            workflowID,
							Task:                  desc,
							MaxIterations:         cfg.SwarmMaxIterationsPerAgent,
							SessionID:             input.SessionID,
							Context:               spawnContext,
							TeamRoster:            updatedRoster,
							WorkspaceMaxEntries:   cfg.SwarmWorkspaceMaxEntries,
							WorkspaceSnippetChars: cfg.SwarmWorkspaceSnippetChars,
							MaxMessagesPerAgent:   cfg.SwarmMaxMessagesPerAgent,
						})
						dynamicSpawnCount++
						totalExpected++

						// Start goroutine to wait for this new agent
						workflow.Go(ctx, func(gCtx workflow.Context) {
							var result AgentLoopResult
							if err := future.Get(gCtx, &result); err != nil {
								result = AgentLoopResult{
									AgentID: newAgentName,
									Success: false,
									Error:   err.Error(),
								}
							}
							completionCh.Send(gCtx, result)
						})

						logger.Info("Dynamic spawn: new agent",
							"agent_id", newAgentName,
							"requested_by", msg.From,
							"task", desc,
						)

						// Notify requesting agent
						_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
							WorkflowID: workflowID,
							From:       "supervisor",
							To:         msg.From,
							Type:       activities.MessageTypeInfo,
							Payload: map[string]interface{}{
								"message":  fmt.Sprintf("Spawned agent %s to help with: %s", newAgentName, desc),
								"agent_id": newAgentName,
							},
							Timestamp: workflow.Now(ctx),
						}).Get(ctx, nil)
					}
				}
			}
		})

		sel.Select(ctx)
	}

	// Phase 4: Synthesize results
	logger.Info("SwarmWorkflow all agents completed", "result_count", len(results))

	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmSynthesizing(len(results)),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Convert AgentLoopResults to AgentExecutionResults for synthesis.
	// Sort by agent ID for deterministic Temporal replay.
	sortedIDs := make([]string, 0, len(results))
	for id := range results {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	var agentResults []activities.AgentExecutionResult
	var totalTokensUsed int
	for _, id := range sortedIDs {
		r := results[id]
		agentResults = append(agentResults, activities.AgentExecutionResult{
			AgentID:      r.AgentID,
			Response:     r.Response,
			TokensUsed:   r.TokensUsed,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			ModelUsed:    r.ModelUsed,
			Provider:     r.Provider,
			Success:      r.Success,
			Error:        r.Error,
		})
		totalTokensUsed += r.TokensUsed
	}

	// Pre-synthesis guard: check if any agents succeeded
	successCount := 0
	for _, r := range agentResults {
		if r.Success {
			successCount++
		}
	}
	if successCount == 0 {
		logger.Error("SwarmWorkflow all agents failed", "total_agents", len(agentResults))
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("All %d agents failed — no results to synthesize", len(agentResults)),
			TokensUsed:   totalTokensUsed,
			Metadata:     buildSwarmMetadata(results),
		}, nil
	}

	// Single result bypass
	if len(agentResults) == 1 && agentResults[0].Success {
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventWorkflowCompleted,
			AgentID:    "swarm-supervisor",
			Message:    activities.MsgSwarmCompleted(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		return TaskResult{
			Result:     agentResults[0].Response,
			Success:    true,
			TokensUsed: totalTokensUsed,
			Metadata:   buildSwarmMetadata(results),
		}, nil
	}

	// LLM synthesis
	var synth activities.SynthesisResult
	if err := workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{
		Query:            input.Query,
		AgentResults:     agentResults,
		Context:          input.Context,
		ParentWorkflowID: workflowID,
	}).Get(ctx, &synth); err != nil {
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Synthesis failed: %v", err),
			TokensUsed:   totalTokensUsed,
		}, err
	}

	totalTokensUsed += synth.TokensUsed

	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{
		Result:     synth.FinalResult,
		Success:    true,
		TokensUsed: totalTokensUsed,
		Metadata:   buildSwarmMetadata(results),
	}, nil
}

// buildSwarmMetadata builds metadata from swarm agent results.
// Iterates in sorted key order for deterministic output.
func buildSwarmMetadata(results map[string]AgentLoopResult) map[string]interface{} {
	sortedIDs := make([]string, 0, len(results))
	for id := range results {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	agentSummaries := make([]map[string]interface{}, 0, len(results))
	for _, id := range sortedIDs {
		r := results[id]
		agentSummaries = append(agentSummaries, map[string]interface{}{
			"agent_id":   r.AgentID,
			"iterations": r.Iterations,
			"tokens":     r.TokensUsed,
			"success":    r.Success,
			"model":      r.ModelUsed,
		})
	}

	meta := map[string]interface{}{
		"workflow_type": "swarm",
		"agents":        agentSummaries,
		"total_agents":  len(results),
	}

	// Serialize for metadata map
	if b, err := json.Marshal(meta); err == nil {
		var result map[string]interface{}
		if json.Unmarshal(b, &result) == nil {
			return result
		}
	}
	return meta
}
