package source

import "strings"

// truncate cuts s to max bytes and appends "…" if trimmed.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// stripEmailHeaders removes MIME headers from raw email body text.
// It finds the blank line (CRLF or LF) that separates headers from body.
func stripEmailHeaders(raw string) string {
	if idx := strings.Index(raw, "\r\n\r\n"); idx != -1 {
		raw = raw[idx+4:]
	} else if idx := strings.Index(raw, "\n\n"); idx != -1 {
		raw = raw[idx+2:]
	}
	return strings.TrimSpace(raw)
}
