package source

import (
	"context"

	"github.com/sentinelai/sentinel/internal/alert"
)

// Handler is implemented by every alert source.
// Start blocks until ctx is cancelled or a fatal error occurs.
type Handler interface {
	// Start begins receiving alerts and calls onAlert for each one.
	Start(ctx context.Context, onAlert func(*alert.Event)) error
	// Name returns the human-readable source identifier.
	Name() string
}
