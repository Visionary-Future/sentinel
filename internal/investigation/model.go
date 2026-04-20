package investigation

import "time"

// Status tracks an investigation's lifecycle.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusReused    Status = "reused" // result copied from a previous investigation
)

// Feedback represents human evaluation of an investigation result.
type Feedback string

const (
	FeedbackNone      Feedback = ""
	FeedbackCorrect   Feedback = "correct"
	FeedbackIncorrect Feedback = "incorrect"
)

// Investigation represents one AI-driven alert analysis session.
type Investigation struct {
	ID          string    `db:"id"`
	AlertID     string    `db:"alert_id"`
	RunbookID   *string   `db:"runbook_id"`
	Status      Status    `db:"status"`
	RootCause   string    `db:"root_cause"`
	Resolution  string    `db:"resolution"`
	Summary     string    `db:"summary"`
	Steps       []Step    `db:"-"` // serialised as JSONB
	LLMProvider string    `db:"llm_provider"`
	LLMModel    string    `db:"llm_model"`
	TokenUsage  int        `db:"token_usage"`
	Confidence  int        `db:"confidence"`  // 0-100, LLM self-assessed confidence
	Feedback    Feedback   `db:"feedback"`
	HumanCause  string     `db:"human_cause"` // human-corrected root cause
	ReusedFrom  string     `db:"reused_from"` // ID of investigation this was copied from
	StartedAt   *time.Time `db:"started_at"`
	CompletedAt *time.Time `db:"completed_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// Step is a single executed investigation step with its results.
type Step struct {
	Index       int         `json:"Index"`
	Description string      `json:"Description"`
	ToolCalls   []ToolCall  `json:"ToolCalls"`
	Analysis    string      `json:"Analysis"`
	StartedAt   time.Time   `json:"StartedAt"`
	CompletedAt time.Time   `json:"CompletedAt"`
}

// ToolCall records a single tool invocation and its result.
type ToolCall struct {
	ID     string         `json:"ID"`
	Name   string         `json:"Name"`
	Input  map[string]any `json:"Input"`
	Result string         `json:"Result"`
	Error  string         `json:"Error,omitempty"`
}
