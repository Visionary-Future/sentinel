package alert

import "time"

// Severity represents the urgency of an alert.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Source identifies where an alert originated.
type Source string

const (
	SourceSlack      Source = "slack"
	SourceOutlook    Source = "outlook"
	SourceWebhook    Source = "webhook"
	SourceAliyun     Source = "aliyun"
	SourceHuaweiCloud Source = "huaweicloud"
)

// Status tracks the lifecycle of an alert.
type Status string

const (
	StatusOpen     Status = "open"
	StatusAcked    Status = "acked"
	StatusResolved Status = "resolved"
)

// Event is the normalised internal representation of an incoming alert.
// All alert sources produce an Event after parsing.
type Event struct {
	ID            string            `db:"id"`
	Source        Source            `db:"source"`
	Severity      Severity          `db:"severity"`
	Title         string            `db:"title"`
	Description   string            `db:"description"`
	Service       string            `db:"service"`
	Labels        map[string]string `db:"labels"`
	RawPayload    []byte            `db:"raw_payload"`
	Fingerprint   string            `db:"fingerprint"`
	Status        Status            `db:"status"`
	CorrelationID *string           `db:"correlation_id"`
	ReceivedAt    time.Time         `db:"received_at"`
	// Source-specific metadata
	SlackChannelID  string `db:"slack_channel_id"`
	SlackMessageTS  string `db:"slack_message_ts"`
	SlackThreadTS   string `db:"slack_thread_ts"`
}
