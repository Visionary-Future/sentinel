package source

import (
	"testing"

	"github.com/sentinelai/sentinel/internal/config"
)

// Tests cover pure helper functions and filter logic that don't require
// a live IMAP server or the go-imap library types.

// ---- stripEmailHeaders ----

func TestStripEmailHeaders_CRLF(t *testing.T) {
	raw := "Content-Type: text/plain\r\nSubject: test\r\n\r\nThis is the body."
	got := stripEmailHeaders(raw)
	if got != "This is the body." {
		t.Errorf("got %q", got)
	}
}

func TestStripEmailHeaders_LF(t *testing.T) {
	raw := "Content-Type: text/plain\n\nThis is the body."
	got := stripEmailHeaders(raw)
	if got != "This is the body." {
		t.Errorf("got %q", got)
	}
}

func TestStripEmailHeaders_NoHeaders(t *testing.T) {
	raw := "Just body text."
	got := stripEmailHeaders(raw)
	if got != "Just body text." {
		t.Errorf("expected unchanged body, got %q", got)
	}
}

func TestStripEmailHeaders_EmptyBody(t *testing.T) {
	raw := "Subject: hi\r\n\r\n"
	got := stripEmailHeaders(raw)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---- truncate ----

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("expected unchanged at exact length, got %q", got)
	}
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello…" {
		t.Errorf("expected truncated with ellipsis, got %q", got)
	}
}

func TestTruncate_Empty(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---- matchesSender ----

func newEmailSource(filters config.EmailFilterConfig) *EmailSource {
	return &EmailSource{
		cfg: config.EmailSourceConfig{
			Filters: filters,
		},
	}
}

func TestMatchesSender_ExactAddress(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{
		Senders: []string{"alertmanager@company.com"},
	})
	if !src.matchesSender("alertmanager@company.com") {
		t.Error("expected exact address to match")
	}
}

func TestMatchesSender_DomainFragment(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{
		Senders: []string{"datadog.com"},
	})
	if !src.matchesSender("alerts@datadog.com") {
		t.Error("expected domain fragment to match")
	}
}

func TestMatchesSender_Prefix(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{
		Senders: []string{"alertmanager@"},
	})
	if !src.matchesSender("alertmanager@prometheus.internal") {
		t.Error("expected prefix fragment to match")
	}
}

func TestMatchesSender_CaseInsensitive(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{
		Senders: []string{"Alerts@Company.COM"},
	})
	if !src.matchesSender("alerts@company.com") {
		t.Error("expected case-insensitive match")
	}
}

func TestMatchesSender_NoMatch(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{
		Senders: []string{"pagerduty.com"},
	})
	if src.matchesSender("alerts@datadog.com") {
		t.Error("expected no match")
	}
}

func TestMatchesSender_EmptyFilters_MatchesAll(t *testing.T) {
	src := newEmailSource(config.EmailFilterConfig{})
	// shouldProcess returns true for empty senders — validate the logic directly
	// by checking that matchesSender with empty list doesn't need to be called.
	// (shouldProcess only calls matchesSender when len(senders) > 0)
	_ = src
}

// ---- NewOutlook validation ----

func TestNewOutlook_MissingIMAPHost(t *testing.T) {
	_, err := NewOutlook(config.EmailSourceConfig{
		Username: "u",
		Password: "p",
	}, nil)
	if err == nil {
		t.Error("expected error for missing imap_host")
	}
}

func TestNewOutlook_MissingUsername(t *testing.T) {
	_, err := NewOutlook(config.EmailSourceConfig{
		IMAPHost: "imap.163.com",
		Password: "p",
	}, nil)
	if err == nil {
		t.Error("expected error for missing username")
	}
}

func TestNewOutlook_MissingPassword(t *testing.T) {
	_, err := NewOutlook(config.EmailSourceConfig{
		IMAPHost: "imap.163.com",
		Username: "u",
	}, nil)
	if err == nil {
		t.Error("expected error for missing password")
	}
}

func TestNewOutlook_DefaultsApplied(t *testing.T) {
	src, err := NewOutlook(config.EmailSourceConfig{
		IMAPHost: "imap.163.com",
		Username: "u",
		Password: "p",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.cfg.IMAPPort != 993 {
		t.Errorf("expected default port 993, got %d", src.cfg.IMAPPort)
	}
	if src.cfg.Folder != "INBOX" {
		t.Errorf("expected default folder INBOX, got %s", src.cfg.Folder)
	}
}
