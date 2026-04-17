package alert

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// Normalize validates and fills in defaults on an Event before it is persisted.
// It also computes the dedup fingerprint if not already set.
func Normalize(e *Event) *Event {
	out := *e // immutable: work on a copy

	out.Title = strings.TrimSpace(out.Title)
	out.Description = strings.TrimSpace(out.Description)
	out.Service = strings.TrimSpace(strings.ToLower(out.Service))

	if out.Severity == "" {
		out.Severity = SeverityWarning
	}
	if out.Status == "" {
		out.Status = StatusOpen
	}
	if out.Labels == nil {
		out.Labels = make(map[string]string)
	}
	if out.Fingerprint == "" {
		out.Fingerprint = fingerprint(&out)
	}

	return &out
}

// fingerprint produces a stable SHA-256 hash for deduplication.
// It is based on source, service, and title (lowercased).
func fingerprint(e *Event) string {
	// Sorted label keys for deterministic hashing
	labelParts := make([]string, 0, len(e.Labels))
	for k, v := range e.Labels {
		labelParts = append(labelParts, k+"="+v)
	}
	sort.Strings(labelParts)

	input := strings.Join([]string{
		string(e.Source),
		e.Service,
		strings.ToLower(e.Title),
		strings.Join(labelParts, ","),
	}, "|")

	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum[:8]) // 16-char hex prefix is plenty for dedup
}
