package runbook

import "time"

// Runbook is a Markdown-authored investigation playbook.
type Runbook struct {
	ID          string     `db:"id"`
	Name        string     `db:"name"`
	Description string     `db:"description"`
	Content     string     `db:"content"` // raw Markdown
	Triggers    []Trigger  `db:"-"`       // parsed from content; also stored as JSONB
	Steps       []string   `db:"-"`       // parsed from content
	Escalation  Escalation `db:"-"`       // parsed from content
	Enabled     bool       `db:"enabled"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// Trigger defines when a runbook should be matched to an alert.
type Trigger struct {
	Field    string `json:"field"`    // e.g. "alert.title", "alert.severity", "alert.service"
	Operator string `json:"operator"` // contains | matches | in | equals
	Value    string `json:"value"`    // the match value or pipe-separated list for "in"
}

// Escalation holds on-call routing info from the runbook.
type Escalation struct {
	Team     string `json:"team"`
	Channel  string `json:"channel"` // e.g. "wecom://oncall-group"
	Timeout  string `json:"timeout"` // e.g. "30m"
}
