package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Repository handles persistence of alert events.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Save inserts a new alert event. Returns the saved event with generated ID.
func (r *Repository) Save(ctx context.Context, e *Event) (*Event, error) {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now().UTC()
	}

	labelsJSON, err := json.Marshal(e.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}

	const q = `
		INSERT INTO alerts (
			id, source, severity, title, description, service,
			labels, raw_payload, fingerprint, status, correlation_id,
			slack_channel_id, slack_message_ts, slack_thread_ts, received_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
		)
		ON CONFLICT (fingerprint) DO NOTHING
		RETURNING id`

	var savedID string
	err = r.db.QueryRowContext(ctx, q,
		e.ID, e.Source, e.Severity, e.Title, e.Description, e.Service,
		labelsJSON, e.RawPayload, e.Fingerprint, e.Status, e.CorrelationID,
		e.SlackChannelID, e.SlackMessageTS, e.SlackThreadTS, e.ReceivedAt,
	).Scan(&savedID)

	if err == sql.ErrNoRows {
		// Duplicate fingerprint — alert already exists
		return nil, ErrDuplicate
	}
	if err != nil {
		return nil, fmt.Errorf("insert alert: %w", err)
	}

	saved := *e
	saved.ID = savedID
	return &saved, nil
}

// FindByID returns a single alert by its ID.
func (r *Repository) FindByID(ctx context.Context, id string) (*Event, error) {
	const q = `
		SELECT id, source, severity, title, description, service,
		       labels, raw_payload, fingerprint, status, correlation_id,
		       slack_channel_id, slack_message_ts, slack_thread_ts, received_at
		FROM alerts WHERE id = $1`

	return r.scan(r.db.QueryRowContext(ctx, q, id))
}

// FindByFingerprint looks up an alert by its dedup fingerprint.
func (r *Repository) FindByFingerprint(ctx context.Context, fp string) (*Event, error) {
	const q = `
		SELECT id, source, severity, title, description, service,
		       labels, raw_payload, fingerprint, status, correlation_id,
		       slack_channel_id, slack_message_ts, slack_thread_ts, received_at
		FROM alerts WHERE fingerprint = $1 LIMIT 1`

	return r.scan(r.db.QueryRowContext(ctx, q, fp))
}

func (r *Repository) scan(row *sql.Row) (*Event, error) {
	var e Event
	var labelsJSON []byte
	err := row.Scan(
		&e.ID, &e.Source, &e.Severity, &e.Title, &e.Description, &e.Service,
		&labelsJSON, &e.RawPayload, &e.Fingerprint, &e.Status, &e.CorrelationID,
		&e.SlackChannelID, &e.SlackMessageTS, &e.SlackThreadTS, &e.ReceivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan alert: %w", err)
	}

	if err := json.Unmarshal(labelsJSON, &e.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}

	return &e, nil
}

// UpdateEmbedding stores the pgvector embedding for an alert.
// The vector must be serialised as "[f1,f2,...,fn]" (PostgreSQL array literal).
func (r *Repository) UpdateEmbedding(ctx context.Context, alertID string, vec []float32) error {
	// Build the PostgreSQL vector literal: '[0.1,0.2,...]'
	sb := strings.Builder{}
	sb.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", v))
	}
	sb.WriteString("]")

	_, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET embedding = $1 WHERE id = $2`,
		sb.String(), alertID,
	)
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}
	return nil
}

// SimilarAlert is a lightweight projection used for history correlation.
type SimilarAlert struct {
	ID          string
	Title       string
	Service     string
	ReceivedAt  time.Time
	Similarity  float64
	RootCause   string
	Resolution  string
}

// FindSimilar returns up to limit completed investigations whose alert
// embeddings are closest (cosine similarity) to the given vector.
// Falls back to empty slice if no embeddings exist yet.
func (r *Repository) FindSimilar(ctx context.Context, vec []float32, limit int) ([]SimilarAlert, error) {
	if len(vec) == 0 || limit == 0 {
		return nil, nil
	}

	sb := strings.Builder{}
	sb.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", v))
	}
	sb.WriteString("]")
	vecLiteral := sb.String()

	const q = `
		SELECT a.id, a.title, a.service, a.received_at,
		       1 - (a.embedding <=> $1::vector) AS similarity,
		       COALESCE(i.root_cause, ''), COALESCE(i.resolution, '')
		FROM alerts a
		LEFT JOIN investigations i ON i.alert_id = a.id AND i.status = 'completed'
		WHERE a.embedding IS NOT NULL
		ORDER BY a.embedding <=> $1::vector
		LIMIT $2`

	rows, err := r.db.QueryContext(ctx, q, vecLiteral, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar: %w", err)
	}
	defer rows.Close()

	var results []SimilarAlert
	for rows.Next() {
		var s SimilarAlert
		if err := rows.Scan(&s.ID, &s.Title, &s.Service, &s.ReceivedAt,
			&s.Similarity, &s.RootCause, &s.Resolution); err != nil {
			continue
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// FindByServiceInWindow returns alerts for the same service within ±window of t.
func (r *Repository) FindByServiceInWindow(ctx context.Context, service string, t time.Time, window time.Duration) ([]*Event, error) {
	if service == "" {
		return nil, nil
	}
	const q = `
		SELECT id, source, severity, title, description, service,
		       labels, raw_payload, fingerprint, status, correlation_id,
		       slack_channel_id, slack_message_ts, slack_thread_ts, received_at
		FROM alerts
		WHERE service = $1
		  AND received_at BETWEEN $2 AND $3
		ORDER BY received_at DESC
		LIMIT 20`

	rows, err := r.db.QueryContext(ctx, q, service,
		t.Add(-window), t.Add(window))
	if err != nil {
		return nil, fmt.Errorf("find by service window: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		var labelsJSON []byte
		if err := rows.Scan(
			&e.ID, &e.Source, &e.Severity, &e.Title, &e.Description, &e.Service,
			&labelsJSON, &e.RawPayload, &e.Fingerprint, &e.Status, &e.CorrelationID,
			&e.SlackChannelID, &e.SlackMessageTS, &e.SlackThreadTS, &e.ReceivedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(labelsJSON, &e.Labels)
		events = append(events, &e)
	}
	return events, rows.Err()
}

// ListParams controls pagination and filtering for List.
type ListParams struct {
	Limit  int
	Offset int
	Status string
}

// List returns a paginated list of alerts ordered newest-first.
func (r *Repository) List(ctx context.Context, p ListParams) ([]*Event, int, error) {
	if p.Limit == 0 {
		p.Limit = 20
	}

	countQ := `SELECT COUNT(*) FROM alerts`
	listQ := `
		SELECT id, source, severity, title, description, service,
		       labels, raw_payload, fingerprint, status, correlation_id,
		       slack_channel_id, slack_message_ts, slack_thread_ts, received_at
		FROM alerts
		ORDER BY received_at DESC
		LIMIT $1 OFFSET $2`

	var args []any
	args = append(args, p.Limit, p.Offset)

	if p.Status != "" {
		countQ += " WHERE status = $1"
		listQ = `
			SELECT id, source, severity, title, description, service,
			       labels, raw_payload, fingerprint, status, correlation_id,
			       slack_channel_id, slack_message_ts, slack_thread_ts, received_at
			FROM alerts WHERE status = $1
			ORDER BY received_at DESC LIMIT $2 OFFSET $3`
		args = []any{p.Status, p.Limit, p.Offset}
	}

	var total int
	if p.Status != "" {
		_ = r.db.QueryRowContext(ctx, countQ, p.Status).Scan(&total)
	} else {
		_ = r.db.QueryRowContext(ctx, countQ).Scan(&total)
	}

	rows, err := r.db.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		var labelsJSON []byte
		if err := rows.Scan(
			&e.ID, &e.Source, &e.Severity, &e.Title, &e.Description, &e.Service,
			&labelsJSON, &e.RawPayload, &e.Fingerprint, &e.Status, &e.CorrelationID,
			&e.SlackChannelID, &e.SlackMessageTS, &e.SlackThreadTS, &e.ReceivedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(labelsJSON, &e.Labels)
		events = append(events, &e)
	}
	return events, total, rows.Err()
}

// Sentinel errors for callers to check against.
var (
	ErrNotFound  = fmt.Errorf("alert not found")
	ErrDuplicate = fmt.Errorf("duplicate alert fingerprint")
)
