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

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"NVDA", "nvda"},
		{"BRK.A", "brk_a"},
		{"BRK.B", "brk_b"},
		{"S&P500", "s_p500"},
		{"http://example.com", "http___example_com"},
		{"simple-id", "simple-id"},
		{"with_underscore", "with_underscore"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := sanitizeID(tt.input); got != tt.want {
				t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetermineNodeQuery(t *testing.T) {
	tests := []struct {
		name        string
		defaultQ    string
		metadata    map[string]interface{}
		nodeOutputs map[string]string
		nodeContext map[string]interface{}
		want        string
	}{
		{
			name:        "use default when no metadata",
			defaultQ:    "default query",
			metadata:    nil,
			nodeOutputs: nil,
			nodeContext: nil,
			want:        "default query",
		},
		{
			name:     "prefer prompt_template over query",
			defaultQ: "default",
			metadata: map[string]interface{}{
				"prompt_template": "template query",
				"query":           "metadata query",
			},
			nodeOutputs: nil,
			nodeContext: nil,
			want:        "template query",
		},
		{
			name:     "substitute node results",
			defaultQ: "default",
			metadata: map[string]interface{}{
				"prompt_template": "News data: {fetch_news_results}\nAlerts: {detect_alerts_results}",
			},
			nodeOutputs: map[string]string{
				"fetch_news":    "news output here",
				"detect_alerts": "alerts output here",
			},
			nodeContext: nil,
			want:        "News data: news output here\nAlerts: alerts output here",
		},
		{
			name:     "substitute context fields",
			defaultQ: "default",
			metadata: map[string]interface{}{
				"prompt_template": "Threshold: {alert_threshold}, Include: {include_filings}",
			},
			nodeOutputs: nil,
			nodeContext: map[string]interface{}{
				"alert_threshold": 0.3,
				"include_filings": true,
			},
			want: "Threshold: 0.3, Include: true",
		},
		{
			name:     "combined substitution",
			defaultQ: "default",
			metadata: map[string]interface{}{
				"prompt_template": "Data: {fetch_news_results}, Threshold: {alert_threshold}",
			},
			nodeOutputs: map[string]string{
				"fetch_news": "news data",
			},
			nodeContext: map[string]interface{}{
				"alert_threshold": 0.5,
			},
			want: "Data: news data, Threshold: 0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineNodeQuery(tt.defaultQ, tt.metadata, tt.nodeOutputs, tt.nodeContext)
			if got != tt.want {
				t.Errorf("determineNodeQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandParallelByTasks(t *testing.T) {
	tests := []struct {
		name        string
		nodeContext map[string]interface{}
		metadata    map[string]interface{}
		parallelBy  string
		wantLen     int
		wantIDs     []string
	}{
		{
			name: "expand tickers array",
			nodeContext: map[string]interface{}{
				"tickers":         []interface{}{"NVDA", "AAPL", "MSFT"},
				"include_filings": true,
			},
			metadata: map[string]interface{}{
				"prompt_template": "Analyze {ticker} stock. Include filings: {include_filings}",
			},
			parallelBy: "tickers",
			wantLen:    3,
			wantIDs:    []string{"tickers_0_nvda", "tickers_1_aapl", "tickers_2_msft"},
		},
		{
			name: "expand string slice with special chars",
			nodeContext: map[string]interface{}{
				"urls": []string{"http://a.com", "http://b.com"},
			},
			metadata: map[string]interface{}{
				"prompt_template": "Fetch {item}",
			},
			parallelBy: "urls",
			wantLen:    2,
			wantIDs:    []string{"urls_0_http___a_com", "urls_1_http___b_com"},
		},
		{
			name: "missing parallel_by field",
			nodeContext: map[string]interface{}{
				"other": "value",
			},
			metadata:   map[string]interface{}{},
			parallelBy: "tickers",
			wantLen:    0,
			wantIDs:    nil,
		},
		{
			name: "empty array",
			nodeContext: map[string]interface{}{
				"tickers": []interface{}{},
			},
			metadata:   map[string]interface{}{},
			parallelBy: "tickers",
			wantLen:    0,
			wantIDs:    nil,
		},
		{
			name: "default prompt template",
			nodeContext: map[string]interface{}{
				"items": []interface{}{"one", "two"},
			},
			metadata:   map[string]interface{}{},
			parallelBy: "items",
			wantLen:    2,
			wantIDs:    []string{"items_0_one", "items_1_two"},
		},
		{
			name: "special characters in tickers",
			nodeContext: map[string]interface{}{
				"tickers": []interface{}{"BRK.A", "BRK.B", "S&P500"},
			},
			metadata:   map[string]interface{}{},
			parallelBy: "tickers",
			wantLen:    3,
			wantIDs:    []string{"tickers_0_brk_a", "tickers_1_brk_b", "tickers_2_s_p500"},
		},
		{
			name:        "nil nodeContext",
			nodeContext: nil,
			metadata:    map[string]interface{}{},
			parallelBy:  "tickers",
			wantLen:     0,
			wantIDs:     nil,
		},
		{
			name: "empty parallelBy",
			nodeContext: map[string]interface{}{
				"tickers": []interface{}{"NVDA"},
			},
			metadata:   map[string]interface{}{},
			parallelBy: "",
			wantLen:    0,
			wantIDs:    nil,
		},
		{
			name: "duplicate items get unique IDs",
			nodeContext: map[string]interface{}{
				"items": []interface{}{"foo", "foo", "bar"},
			},
			metadata:   map[string]interface{}{},
			parallelBy: "items",
			wantLen:    3,
			wantIDs:    []string{"items_0_foo", "items_1_foo", "items_2_bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks := expandParallelByTasks(tt.nodeContext, tt.metadata, tt.parallelBy)
			if len(tasks) != tt.wantLen {
				t.Errorf("expandParallelByTasks() got %d tasks, want %d", len(tasks), tt.wantLen)
			}
			if tt.wantIDs != nil {
				for i, task := range tasks {
					if task.ID != tt.wantIDs[i] {
						t.Errorf("task[%d].ID = %q, want %q", i, task.ID, tt.wantIDs[i])
					}
				}
			}
		})
	}
}

func TestExpandParallelByTasks_ContextSubstitution(t *testing.T) {
	nodeContext := map[string]interface{}{
		"tickers":         []interface{}{"NVDA"},
		"include_filings": true,
		"include_twitter": false,
		"alert_threshold": 0.3,
	}
	metadata := map[string]interface{}{
		"prompt_template": "Analyze {ticker}. Filings: {include_filings}. Twitter: {include_twitter}. Threshold: {alert_threshold}",
	}

	tasks := expandParallelByTasks(nodeContext, metadata, "tickers")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	desc := tasks[0].Description
	if desc != "Analyze NVDA. Filings: true. Twitter: false. Threshold: 0.3" {
		t.Errorf("unexpected description: %s", desc)
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
