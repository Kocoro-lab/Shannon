package channels

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type Registry struct {
	db *sqlx.DB
}

func NewRegistry(db *sqlx.DB) *Registry {
	return &Registry{db: db}
}

func (r *Registry) Create(ctx context.Context, ch *Channel) error {
	return r.db.QueryRowxContext(ctx, `
		INSERT INTO channels (user_id, type, name, credentials, config, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, ch.UserID, ch.Type, ch.Name, ch.Credentials, ch.Config, ch.Enabled,
	).Scan(&ch.ID, &ch.CreatedAt, &ch.UpdatedAt)
}

func (r *Registry) Get(ctx context.Context, id uuid.UUID) (*Channel, error) {
	var ch Channel
	err := r.db.GetContext(ctx, &ch, `SELECT * FROM channels WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetByUser returns a channel only if owned by the given user.
func (r *Registry) GetByUser(ctx context.Context, id, userID uuid.UUID) (*Channel, error) {
	var ch Channel
	err := r.db.GetContext(ctx, &ch, `SELECT * FROM channels WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *Registry) GetByUserAndType(ctx context.Context, userID uuid.UUID, channelType string) (*Channel, error) {
	var ch Channel
	err := r.db.GetContext(ctx, &ch, `SELECT * FROM channels WHERE user_id = $1 AND type = $2`, userID, channelType)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *Registry) ListByUser(ctx context.Context, userID uuid.UUID) ([]Channel, error) {
	var channels []Channel
	err := r.db.SelectContext(ctx, &channels, `
		SELECT * FROM channels WHERE user_id = $1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func (r *Registry) List(ctx context.Context) ([]Channel, error) {
	var channels []Channel
	err := r.db.SelectContext(ctx, &channels, `
		SELECT * FROM channels ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// UpdateByUser updates a channel only if owned by the given user.
func (r *Registry) UpdateByUser(ctx context.Context, id, userID uuid.UUID, req UpdateChannelRequest) error {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Credentials != nil {
		setClauses = append(setClauses, fmt.Sprintf("credentials = $%d", argIdx))
		args = append(args, *req.Credentials)
		argIdx++
	}
	if req.Config != nil {
		setClauses = append(setClauses, fmt.Sprintf("config = $%d", argIdx))
		args = append(args, *req.Config)
		argIdx++
	}
	if req.Enabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *req.Enabled)
		argIdx++
	}

	if len(args) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE channels SET %s WHERE id = $%d AND user_id = $%d",
		strings.Join(setClauses, ", "), argIdx, argIdx+1)
	args = append(args, id, userID)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("channel not found")
	}
	return nil
}

// DeleteByUser deletes a channel only if owned by the given user.
func (r *Registry) DeleteByUser(ctx context.Context, id, userID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("channel not found")
	}
	return nil
}
