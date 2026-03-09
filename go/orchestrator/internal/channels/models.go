package channels

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Channel struct {
	ID          uuid.UUID       `db:"id" json:"id"`
	UserID      *uuid.UUID      `db:"user_id" json:"user_id,omitempty"`
	Type        string          `db:"type" json:"type"`
	Name        string          `db:"name" json:"name"`
	Credentials json.RawMessage `db:"credentials" json:"-"`
	Config      json.RawMessage `db:"config" json:"config,omitempty"`
	Enabled     bool            `db:"enabled" json:"enabled"`
	CreatedAt   time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updated_at"`
}

type CreateChannelRequest struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Credentials json.RawMessage `json:"credentials"`
	Config      json.RawMessage `json:"config,omitempty"`
}

type UpdateChannelRequest struct {
	Name        *string          `json:"name,omitempty"`
	Credentials *json.RawMessage `json:"credentials,omitempty"`
	Config      *json.RawMessage `json:"config,omitempty"`
	Enabled     *bool            `json:"enabled,omitempty"`
}
