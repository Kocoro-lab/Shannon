package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EventLog represents a persisted streaming event row.
type EventLog struct {
	ID         uuid.UUID `json:"id"`
	WorkflowID string    `json:"workflow_id"`
	Type       string    `json:"type"`
	AgentID    string    `json:"agent_id,omitempty"`
	Message    string    `json:"message,omitempty"`
	Payload    JSONB     `json:"payload,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Seq        uint64    `json:"seq,omitempty"`
	StreamID   string    `json:"stream_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SaveEventLog inserts a new event_logs row.
func (c *Client) SaveEventLog(ctx context.Context, e *EventLog) error {
	if e == nil {
		return nil
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	_, err := c.db.ExecContext(ctx, `
        INSERT INTO event_logs (
            id, workflow_id, type, agent_id, message, payload, timestamp, seq, stream_id, created_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
        ON CONFLICT (workflow_id, type, seq) WHERE seq IS NOT NULL DO NOTHING
    `, e.ID, e.WorkflowID, e.Type, nullIfEmpty(e.AgentID), e.Message, e.Payload, e.Timestamp, e.Seq, nullIfEmpty(e.StreamID), e.CreatedAt)
	return err
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
