package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
)

// TimelineHandler builds human-readable timelines from Temporal history and optionally persists to DB.
type TimelineHandler struct {
	tclient  client.Client
	dbClient *db.Client
	logger   *zap.Logger
}

// Helper types for timeline building
type act struct {
	Type      string
	ID        string
	Scheduled time.Time
	Started   time.Time
}

type timer struct {
	ID      string
	Started time.Time
	Timeout time.Duration
}

type child struct {
	Type    string
	ID      string
	RunID   string
	Started time.Time
}

func NewTimelineHandler(tc client.Client, dbc *db.Client, logger *zap.Logger) *TimelineHandler {
	return &TimelineHandler{tclient: tc, dbClient: dbc, logger: logger}
}

func (h *TimelineHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/timeline", h.handleBuildTimeline)
}

// handleBuildTimeline: GET /timeline?workflow_id=&run_id=&mode=summary|full&include_payloads=false&persist=true
func (h *TimelineHandler) handleBuildTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	wf := q.Get("workflow_id")
	if wf == "" {
		http.Error(w, `{"error":"workflow_id required"}`, http.StatusBadRequest)
		return
	}
	runID := q.Get("run_id")
	mode := q.Get("mode")
	if mode == "" {
		mode = "summary"
	}
	includePayloads := strings.EqualFold(q.Get("include_payloads"), "true")
	persist := strings.EqualFold(q.Get("persist"), "true")

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	events, stats, err := h.buildTimeline(ctx, wf, runID, mode, includePayloads)
	if err != nil {
		h.logger.Error("build timeline failed", zap.String("workflow_id", wf), zap.Error(err))
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	if persist && h.dbClient != nil {
		// Persist asynchronously to avoid blocking request path
		go func(evts []db.EventLog) {
			ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
			defer c()
			for i := range evts {
				// Attach a monotonic seq if missing (use index)
				if evts[i].Seq == 0 {
					evts[i].Seq = uint64(i + 1)
				}
				_ = h.dbClient.SaveEventLog(ctx, &evts[i])
			}
		}(events)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "accepted",
			"workflow_id": wf,
			"count":       len(events),
		})
		return
	}

	// Return timeline directly
	payload := map[string]any{
		"workflow_id": wf,
		"events":      events,
		"stats":       stats,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

type timelineStats struct {
	Total int    `json:"total"`
	Mode  string `json:"mode"`
}

// buildTimeline maps Temporal history to human-readable events. Mode: summary|full
func (h *TimelineHandler) buildTimeline(ctx context.Context, workflowID, runID, mode string, includePayloads bool) ([]db.EventLog, timelineStats, error) {
	it := h.tclient.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	if it == nil {
		return nil, timelineStats{}, fmt.Errorf("history iterator is nil")
	}

	acts := map[int64]*act{}
	timers := map[int64]*timer{}
	childs := map[int64]*child{}

	var out []db.EventLog

	add := func(t, msg string, ts time.Time, seq uint64) {
		out = append(out, db.EventLog{
			WorkflowID: workflowID,
			Type:       t,
			Message:    msg,
			Timestamp:  ts,
			Seq:        seq,
		})
	}

	for it.HasNext() {
		e, err := it.Next()
		if err != nil {
			return nil, timelineStats{}, err
		}
		ts := e.GetEventTime().AsTime()
		switch e.EventType {
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED:
			add("WF_STARTED", "Workflow started", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED:
			add("WF_COMPLETED", "Workflow completed", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED:
			msg := "Workflow failed"
			if a := e.GetWorkflowExecutionFailedEventAttributes(); a != nil && a.GetFailure() != nil {
				msg = fmt.Sprintf("Workflow failed: %s", summarizeFailure(a.GetFailure(), includePayloads))
			}
			add("WF_FAILED", msg, ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_TIMED_OUT:
			add("WF_TIMEOUT", "Workflow timed out", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_TERMINATED:
			add("WF_TERMINATED", "Workflow terminated", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CANCELED:
			add("WF_CANCELLED", "Workflow cancelled", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CONTINUED_AS_NEW:
			add("WF_CONTINUED_AS_NEW", "Workflow continued as new", ts, uint64(e.GetEventId()))

		// Activities
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			if a := e.GetActivityTaskScheduledEventAttributes(); a != nil {
				acts[e.GetEventId()] = &act{Type: a.GetActivityType().GetName(), ID: a.GetActivityId(), Scheduled: ts}
				if mode == "full" {
					add("ACT_SCHEDULED", fmt.Sprintf("Activity %s(id=%s) scheduled", a.GetActivityType().GetName(), a.GetActivityId()), ts, uint64(e.GetEventId()))
				}
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED:
			if a := e.GetActivityTaskStartedEventAttributes(); a != nil {
				if st := acts[a.GetScheduledEventId()]; st != nil {
					st.Started = ts
				}
				if mode == "full" {
					add("ACT_STARTED", fmt.Sprintf("Activity started (scheduled_id=%d)", a.GetScheduledEventId()), ts, uint64(e.GetEventId()))
				}
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			if a := e.GetActivityTaskCompletedEventAttributes(); a != nil {
				st := acts[a.GetScheduledEventId()]
				dur := durationFromTo(st, ts)
				name, id := activityNameID(st)
				add("ACT_COMPLETED", fmt.Sprintf("Activity %s(id=%s) completed in %s", name, id, dur), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_FAILED:
			if a := e.GetActivityTaskFailedEventAttributes(); a != nil {
				st := acts[a.GetScheduledEventId()]
				dur := durationFromTo(st, ts)
				name, id := activityNameID(st)
				add("ACT_FAILED", fmt.Sprintf("Activity %s(id=%s) failed in %s: %s", name, id, dur, summarizeFailure(a.GetFailure(), includePayloads)), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT:
			if a := e.GetActivityTaskTimedOutEventAttributes(); a != nil {
				st := acts[a.GetScheduledEventId()]
				dur := durationFromTo(st, ts)
				name, id := activityNameID(st)
				add("ACT_TIMEOUT", fmt.Sprintf("Activity %s(id=%s) timed out in %s", name, id, dur), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCEL_REQUESTED:
			if a := e.GetActivityTaskCancelRequestedEventAttributes(); a != nil {
				add("ACT_CANCEL_REQUESTED", fmt.Sprintf("Activity cancel requested (scheduled_id=%d)", a.GetScheduledEventId()), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCELED:
			if a := e.GetActivityTaskCanceledEventAttributes(); a != nil {
				st := acts[a.GetScheduledEventId()]
				name, id := activityNameID(st)
				add("ACT_CANCELLED", fmt.Sprintf("Activity %s(id=%s) cancelled", name, id), ts, uint64(e.GetEventId()))
			}

		// Timers
		case enumspb.EVENT_TYPE_TIMER_STARTED:
			if a := e.GetTimerStartedEventAttributes(); a != nil {
				timers[e.GetEventId()] = &timer{ID: a.GetTimerId(), Started: ts, Timeout: a.GetStartToFireTimeout().AsDuration()}
				if mode == "full" {
					add("TIMER_STARTED", fmt.Sprintf("Timer %s started for %s", a.GetTimerId(), a.GetStartToFireTimeout().AsDuration()), ts, uint64(e.GetEventId()))
				}
			}
		case enumspb.EVENT_TYPE_TIMER_FIRED:
			if a := e.GetTimerFiredEventAttributes(); a != nil {
				t := timers[a.GetStartedEventId()]
				fired := fmt.Sprintf("Timer %s fired", valueOr(t, func(t *timer) string { return t.ID }))
				add("TIMER_FIRED", fired, ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_TIMER_CANCELED:
			if a := e.GetTimerCanceledEventAttributes(); a != nil {
				add("TIMER_CANCELLED", fmt.Sprintf("Timer cancel (started_id=%d)", a.GetStartedEventId()), ts, uint64(e.GetEventId()))
			}

		// Signals
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED:
			if a := e.GetWorkflowExecutionSignaledEventAttributes(); a != nil {
				add("SIG_RECEIVED", fmt.Sprintf("Signal received: %s", a.GetSignalName()), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_SIGNAL_EXTERNAL_WORKFLOW_EXECUTION_INITIATED:
			if a := e.GetSignalExternalWorkflowExecutionInitiatedEventAttributes(); a != nil {
				add("SIG_SENT", fmt.Sprintf("Signal sent: %s -> %s", a.GetSignalName(), a.GetWorkflowExecution().GetWorkflowId()), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_EXTERNAL_WORKFLOW_EXECUTION_SIGNALED:
			add("SIG_SENT_CONFIRMED", "External signal acknowledged", ts, uint64(e.GetEventId()))
		case enumspb.EVENT_TYPE_SIGNAL_EXTERNAL_WORKFLOW_EXECUTION_FAILED:
			add("SIG_SENT_FAILED", "External signal failed", ts, uint64(e.GetEventId()))

		// Child workflows
		case enumspb.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED:
			if a := e.GetStartChildWorkflowExecutionInitiatedEventAttributes(); a != nil {
				childs[e.GetEventId()] = &child{Type: a.GetWorkflowType().GetName(), ID: a.GetWorkflowId()}
				if mode == "full" {
					add("CHILD_INITIATED", fmt.Sprintf("Child %s(id=%s) initiated", a.GetWorkflowType().GetName(), a.GetWorkflowId()), ts, uint64(e.GetEventId()))
				}
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_STARTED:
			if a := e.GetChildWorkflowExecutionStartedEventAttributes(); a != nil {
				if c := childs[a.GetInitiatedEventId()]; c != nil {
					c.Started = ts
					c.RunID = a.GetWorkflowExecution().GetRunId()
				}
				add("CHILD_STARTED", fmt.Sprintf("Child %s started (id=%s)", safeChildType(childs, a.GetInitiatedEventId()), safeChildID(childs, a.GetInitiatedEventId())), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_COMPLETED:
			if a := e.GetChildWorkflowExecutionCompletedEventAttributes(); a != nil {
				c := childs[a.GetInitiatedEventId()]
				dur := childDuration(c, ts)
				add("CHILD_COMPLETED", fmt.Sprintf("Child %s(id=%s) completed in %s", ctype(c), cid(c), dur), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_FAILED:
			if a := e.GetChildWorkflowExecutionFailedEventAttributes(); a != nil {
				c := childs[a.GetInitiatedEventId()]
				dur := childDuration(c, ts)
				add("CHILD_FAILED", fmt.Sprintf("Child %s(id=%s) failed in %s", ctype(c), cid(c), dur), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_TIMED_OUT:
			if a := e.GetChildWorkflowExecutionTimedOutEventAttributes(); a != nil {
				c := childs[a.GetInitiatedEventId()]
				dur := childDuration(c, ts)
				add("CHILD_TIMEOUT", fmt.Sprintf("Child %s(id=%s) timed out in %s", ctype(c), cid(c), dur), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_CANCELED:
			if a := e.GetChildWorkflowExecutionCanceledEventAttributes(); a != nil {
				c := childs[a.GetInitiatedEventId()]
				add("CHILD_CANCELLED", fmt.Sprintf("Child %s(id=%s) cancelled", ctype(c), cid(c)), ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_TERMINATED:
			if a := e.GetChildWorkflowExecutionTerminatedEventAttributes(); a != nil {
				c := childs[a.GetInitiatedEventId()]
				add("CHILD_TERMINATED", fmt.Sprintf("Child %s(id=%s) terminated", ctype(c), cid(c)), ts, uint64(e.GetEventId()))
			}

		// Attributes / Markers (full)
		case enumspb.EVENT_TYPE_UPSERT_WORKFLOW_SEARCH_ATTRIBUTES:
			if mode == "full" {
				add("ATTR_UPSERT", "Search attributes upserted", ts, uint64(e.GetEventId()))
			}
		case enumspb.EVENT_TYPE_MARKER_RECORDED:
			if mode == "full" {
				add("MARKER_RECORDED", fmt.Sprintf("Marker recorded: %s", markerName(e)), ts, uint64(e.GetEventId()))
			}
		default:
			if mode == "full" {
				add("EVENT", fmt.Sprintf("%s", e.EventType.String()), ts, uint64(e.GetEventId()))
			}
		}
	}

	// Ensure order by timestamp
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })

	return out, timelineStats{Total: len(out), Mode: mode}, nil
}

func durationFromTo(a *act, end time.Time) time.Duration {
	if a == nil {
		return 0
	}
	if !a.Started.IsZero() {
		return end.Sub(a.Started)
	}
	if !a.Scheduled.IsZero() {
		return end.Sub(a.Scheduled)
	}
	return 0
}

func activityNameID(a *act) (string, string) {
	if a == nil {
		return "?", "?"
	}
	return a.Type, a.ID
}

func summarizeFailure(f *failurepb.Failure, includePayloads bool) string {
	if f == nil {
		return "unknown"
	}
	reason := f.GetMessage()
	if !includePayloads {
		runes := []rune(reason)
		if len(runes) > 200 {
			reason = string(runes[:200]) + "..."
		}
	}
	return reason
}

func valueOr[T any](v *T, f func(*T) string) string {
	if v == nil {
		return ""
	}
	return f(v)
}

func childDuration(c *child, end time.Time) time.Duration {
	if c == nil || c.Started.IsZero() {
		return 0
	}
	return end.Sub(c.Started)
}

func ctype(c *child) string {
	if c == nil {
		return "?"
	}
	return c.Type
}
func cid(c *child) string {
	if c == nil {
		return "?"
	}
	return c.ID
}

func safeChildType(childs map[int64]*child, id int64) string {
	if c := childs[id]; c != nil {
		return c.Type
	}
	return "?"
}

func safeChildID(childs map[int64]*child, id int64) string {
	if c := childs[id]; c != nil {
		return c.ID
	}
	return "?"
}

func markerName(e *historypb.HistoryEvent) string {
	if a := e.GetMarkerRecordedEventAttributes(); a != nil {
		return a.GetMarkerName()
	}
	return strconv.FormatInt(int64(e.GetEventId()), 10)
}
