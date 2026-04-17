package investigation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Repository handles Investigation persistence.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new investigation record in pending state.
func (r *Repository) Create(ctx context.Context, inv *Investigation) (*Investigation, error) {
	if inv.ID == "" {
		inv.ID = uuid.New().String()
	}

	stepsJSON, err := json.Marshal(inv.Steps)
	if err != nil {
		return nil, fmt.Errorf("marshal steps: %w", err)
	}

	const q = `
		INSERT INTO investigations
			(id, alert_id, runbook_id, status, steps, llm_provider, llm_model)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`

	saved := *inv
	err = r.db.QueryRowContext(ctx, q,
		inv.ID, inv.AlertID, inv.RunbookID, inv.Status,
		stepsJSON, inv.LLMProvider, inv.LLMModel,
	).Scan(&saved.ID, &saved.CreatedAt, &saved.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create investigation: %w", err)
	}

	return &saved, nil
}

// UpdateStatus transitions an investigation to a new status and persists steps.
func (r *Repository) UpdateStatus(ctx context.Context, id string, status Status, inv *Investigation) error {
	stepsJSON, err := json.Marshal(inv.Steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}

	now := time.Now().UTC()
	var startedAt, completedAt *time.Time

	switch status {
	case StatusRunning:
		startedAt = &now
	case StatusCompleted, StatusFailed:
		completedAt = &now
	}

	const q = `
		UPDATE investigations SET
			status       = $2,
			steps        = $3,
			root_cause   = $4,
			resolution   = $5,
			summary      = $6,
			token_usage  = $7,
			started_at   = COALESCE(started_at, $8),
			completed_at = $9
		WHERE id = $1`

	_, err = r.db.ExecContext(ctx, q,
		id, status, stepsJSON,
		inv.RootCause, inv.Resolution, inv.Summary,
		inv.TokenUsage, startedAt, completedAt,
	)
	return err
}

// FindByID retrieves a single investigation.
func (r *Repository) FindByID(ctx context.Context, id string) (*Investigation, error) {
	const q = `
		SELECT id, alert_id, runbook_id, status, root_cause, resolution, summary,
		       steps, llm_provider, llm_model, token_usage,
		       started_at, completed_at, created_at, updated_at
		FROM investigations WHERE id = $1`

	return r.scan(r.db.QueryRowContext(ctx, q, id))
}

func (r *Repository) scan(row *sql.Row) (*Investigation, error) {
	var inv Investigation
	var stepsJSON []byte

	err := row.Scan(
		&inv.ID, &inv.AlertID, &inv.RunbookID, &inv.Status,
		&inv.RootCause, &inv.Resolution, &inv.Summary,
		&stepsJSON, &inv.LLMProvider, &inv.LLMModel, &inv.TokenUsage,
		&inv.StartedAt, &inv.CompletedAt, &inv.CreatedAt, &inv.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan investigation: %w", err)
	}

	if err := json.Unmarshal(stepsJSON, &inv.Steps); err != nil {
		return nil, fmt.Errorf("unmarshal steps: %w", err)
	}

	return &inv, nil
}

// ListParams controls pagination for List.
type ListParams struct {
	Limit  int
	Offset int
	Status string
}

// List returns investigations ordered by creation time, newest first.
func (r *Repository) List(ctx context.Context, p ListParams) ([]*Investigation, int, error) {
	if p.Limit == 0 {
		p.Limit = 20
	}

	var total int
	if p.Status != "" {
		_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM investigations WHERE status = $1`, p.Status).Scan(&total)
	} else {
		_ = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM investigations`).Scan(&total)
	}

	q := `
		SELECT id, alert_id, runbook_id, status, root_cause, resolution, summary,
		       steps, llm_provider, llm_model, token_usage,
		       started_at, completed_at, created_at, updated_at
		FROM investigations
		ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	args := []any{p.Limit, p.Offset}

	if p.Status != "" {
		q = `
			SELECT id, alert_id, runbook_id, status, root_cause, resolution, summary,
			       steps, llm_provider, llm_model, token_usage,
			       started_at, completed_at, created_at, updated_at
			FROM investigations WHERE status = $1
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []any{p.Status, p.Limit, p.Offset}
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list investigations: %w", err)
	}
	defer rows.Close()

	var invs []*Investigation
	for rows.Next() {
		var inv Investigation
		var stepsJSON []byte
		if err := rows.Scan(
			&inv.ID, &inv.AlertID, &inv.RunbookID, &inv.Status,
			&inv.RootCause, &inv.Resolution, &inv.Summary,
			&stepsJSON, &inv.LLMProvider, &inv.LLMModel, &inv.TokenUsage,
			&inv.StartedAt, &inv.CompletedAt, &inv.CreatedAt, &inv.UpdatedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(stepsJSON, &inv.Steps)
		invs = append(invs, &inv)
	}
	return invs, total, rows.Err()
}

var ErrNotFound = fmt.Errorf("investigation not found")
