package runbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Repository handles Runbook persistence.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Save inserts or updates a runbook. Sets ID if empty.
func (r *Repository) Save(ctx context.Context, rb *Runbook) (*Runbook, error) {
	if rb.ID == "" {
		rb.ID = uuid.New().String()
	}

	triggersJSON, err := json.Marshal(rb.Triggers)
	if err != nil {
		return nil, fmt.Errorf("marshal triggers: %w", err)
	}

	const q = `
		INSERT INTO runbooks (id, name, description, content, triggers, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name        = EXCLUDED.name,
			description = EXCLUDED.description,
			content     = EXCLUDED.content,
			triggers    = EXCLUDED.triggers,
			enabled     = EXCLUDED.enabled
		RETURNING id, created_at, updated_at`

	saved := *rb
	err = r.db.QueryRowContext(ctx, q,
		rb.ID, rb.Name, rb.Description, rb.Content, triggersJSON, rb.Enabled,
	).Scan(&saved.ID, &saved.CreatedAt, &saved.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save runbook: %w", err)
	}

	return &saved, nil
}

// FindByID retrieves a runbook by its ID.
func (r *Repository) FindByID(ctx context.Context, id string) (*Runbook, error) {
	const q = `
		SELECT id, name, description, content, triggers, enabled, created_at, updated_at
		FROM runbooks WHERE id = $1`

	return r.scan(r.db.QueryRowContext(ctx, q, id))
}

// ListEnabled returns all enabled runbooks, used for alert matching.
func (r *Repository) ListEnabled(ctx context.Context) ([]*Runbook, error) {
	const q = `
		SELECT id, name, description, content, triggers, enabled, created_at, updated_at
		FROM runbooks WHERE enabled = true ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list runbooks: %w", err)
	}
	defer rows.Close()

	var runbooks []*Runbook
	for rows.Next() {
		rb, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		runbooks = append(runbooks, rb)
	}
	return runbooks, rows.Err()
}

// Delete soft-disables a runbook by ID.
func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE runbooks SET enabled = false WHERE id = $1`, id)
	return err
}

// scanner allows scan() to work on both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func (r *Repository) scan(s scanner) (*Runbook, error) {
	var rb Runbook
	var triggersJSON []byte

	err := s.Scan(
		&rb.ID, &rb.Name, &rb.Description, &rb.Content,
		&triggersJSON, &rb.Enabled, &rb.CreatedAt, &rb.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan runbook: %w", err)
	}

	if err := json.Unmarshal(triggersJSON, &rb.Triggers); err != nil {
		return nil, fmt.Errorf("unmarshal triggers: %w", err)
	}

	// Re-parse steps and escalation from raw Markdown
	parsed := Parse(rb.Content)
	rb.Steps = parsed.Steps
	rb.Escalation = parsed.Escalation

	return &rb, nil
}

var ErrNotFound = fmt.Errorf("runbook not found")
