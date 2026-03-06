package activities

import (
	"encoding/json"
	"testing"
)

func TestSwarmTaskSerialization(t *testing.T) {
	task := SwarmTask{
		ID:          "task-1",
		Description: "Research market trends",
		Status:      "pending",
		Owner:       "",
		CreatedBy:   "decompose",
		DependsOn:   []string{"task-0"},
		CreatedAt:   "2026-02-19T10:00:00Z",
	}

	// Marshal
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Unmarshal
	var decoded SwarmTask
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, task.ID)
	}
	if decoded.Description != task.Description {
		t.Errorf("Description mismatch: got %q, want %q", decoded.Description, task.Description)
	}
	if decoded.Status != task.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, task.Status)
	}
	if decoded.CreatedBy != task.CreatedBy {
		t.Errorf("CreatedBy mismatch: got %q, want %q", decoded.CreatedBy, task.CreatedBy)
	}
	if len(decoded.DependsOn) != 1 || decoded.DependsOn[0] != "task-0" {
		t.Errorf("DependsOn mismatch: got %v, want %v", decoded.DependsOn, task.DependsOn)
	}
	if decoded.CreatedAt != task.CreatedAt {
		t.Errorf("CreatedAt mismatch: got %q, want %q", decoded.CreatedAt, task.CreatedAt)
	}
}

func TestSwarmTaskSerialization_CompletedAt(t *testing.T) {
	// CompletedAt should be omitted when empty
	task := SwarmTask{
		ID:     "task-1",
		Status: "pending",
	}
	b, _ := json.Marshal(task)
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	if _, ok := m["completed_at"]; ok {
		t.Error("completed_at should be omitted when empty")
	}

	// CompletedAt should be present when set
	task.CompletedAt = "2026-02-19T12:00:00Z"
	b, _ = json.Marshal(task)
	json.Unmarshal(b, &m)
	if _, ok := m["completed_at"]; !ok {
		t.Error("completed_at should be present when set")
	}
}

func TestSwarmTaskStatusTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{
			name:    "valid: pending to in_progress",
			from:    "pending",
			to:      "in_progress",
			wantErr: false,
		},
		{
			name:    "valid: in_progress to completed",
			from:    "in_progress",
			to:      "completed",
			wantErr: false,
		},
		{
			name:    "valid: pending to completed (Lead can cancel/complete pending tasks directly)",
			from:    "pending",
			to:      "completed",
			wantErr: false,
		},
		{
			name:    "valid: completed to in_progress (reactivate: agent idle auto-completes, Lead reassigns)",
			from:    "completed",
			to:      "in_progress",
			wantErr: false,
		},
		{
			name:    "invalid: completed to pending (backwards)",
			from:    "completed",
			to:      "pending",
			wantErr: true,
		},
		{
			name:    "invalid: in_progress to pending (backwards)",
			from:    "in_progress",
			to:      "pending",
			wantErr: true,
		},
		{
			name:    "invalid: same status pending",
			from:    "pending",
			to:      "pending",
			wantErr: true,
		},
		{
			name:    "valid: same status in_progress (Lead can re-assign)",
			from:    "in_progress",
			to:      "in_progress",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStatusTransition(%q, %q) error = %v, wantErr = %v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"valid-session-123", false},
		{"abc_def", false},
		{"", true},
		{"../etc/passwd", true},
		{".hidden", true},
		{"has space", true},
		{"has/slash", true},
	}
	for _, tt := range tests {
		err := validateSessionID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateSessionID(%q) error=%v, wantErr=%v", tt.id, err, tt.wantErr)
		}
	}
}

func TestTaskListKey(t *testing.T) {
	key := taskListKey("wf-abc-123")
	expected := "wf:wf-abc-123:tasklist"
	if key != expected {
		t.Errorf("taskListKey() = %q, want %q", key, expected)
	}
}
