package investigation

import (
	"context"

	"github.com/sentinelai/sentinel/internal/runbook"
)

// InvestigationStore abstracts investigation persistence for testability.
type InvestigationStore interface {
	Create(ctx context.Context, inv *Investigation) (*Investigation, error)
	UpdateStatus(ctx context.Context, id string, status Status, inv *Investigation) error
	FindByID(ctx context.Context, id string) (*Investigation, error)
}

// InvestigationCacheStore extends InvestigationStore with fingerprint-based
// lookups for result caching. Implementations that support this should also
// implement this interface.
type InvestigationCacheStore interface {
	InvestigationStore
	FindByAlertFingerprint(ctx context.Context, fingerprint string) (*Investigation, error)
}

// InvestigationFeedbackStore extends InvestigationStore with feedback updates.
type InvestigationFeedbackStore interface {
	InvestigationStore
	UpdateFeedback(ctx context.Context, id string, feedback Feedback, humanCause string) error
}

// RunbookStore abstracts runbook retrieval for testability.
type RunbookStore interface {
	ListEnabled(ctx context.Context) ([]*runbook.Runbook, error)
}
