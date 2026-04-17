package source

// outlook.go provides an IMAP-based email alert source that works with any
// IMAP-compliant mail server, including:
//   - 163企业邮箱  (imap.163.com:993)
//   - QQ企业邮箱   (imap.exmail.qq.com:993)
//   - QQ个人邮箱   (imap.qq.com:993)
//   - 阿里企业邮箱  (imap.mxhichina.com:993)
//   - Outlook/365  (outlook.office365.com:993)
//   - Gmail        (imap.gmail.com:993)

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/config"
)

// EmailSource polls a mailbox via IMAP and emits alert events.
type EmailSource struct {
	cfg config.EmailSourceConfig
	log *slog.Logger
}

// NewOutlook creates an IMAP email alert source (name kept for API compatibility).
func NewOutlook(cfg config.EmailSourceConfig, log *slog.Logger) (*EmailSource, error) {
	if cfg.IMAPHost == "" {
		return nil, fmt.Errorf("email: imap_host is required")
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("email: username is required")
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("email: password is required")
	}
	if cfg.IMAPPort == 0 {
		cfg.IMAPPort = 993
	}
	if cfg.Folder == "" {
		cfg.Folder = "INBOX"
	}
	if !cfg.TLS {
		// Default to TLS=true unless explicitly set to false.
		// mapstructure zero value is false, so we rely on yaml default.
		// If port is 993, assume TLS.
		cfg.TLS = cfg.IMAPPort == 993
	}
	return &EmailSource{cfg: cfg, log: log.With("source", "email")}, nil
}

func (e *EmailSource) Name() string { return "email" }

// Start polls the mailbox at the configured interval until ctx is cancelled.
func (e *EmailSource) Start(ctx context.Context, onAlert func(*alert.Event)) error {
	interval := 30 * time.Second
	if e.cfg.PollInterval != "" {
		if d, err := time.ParseDuration(e.cfg.PollInterval); err == nil && d > 0 {
			interval = d
		}
	}

	e.log.Info("starting IMAP email poller",
		"host", e.cfg.IMAPHost,
		"port", e.cfg.IMAPPort,
		"user", e.cfg.Username,
		"folder", e.cfg.Folder,
		"interval", interval,
	)

	// Poll immediately, then on each tick.
	e.poll(ctx, onAlert)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			e.poll(ctx, onAlert)
		}
	}
}

func (e *EmailSource) poll(ctx context.Context, onAlert func(*alert.Event)) {
	addr := fmt.Sprintf("%s:%d", e.cfg.IMAPHost, e.cfg.IMAPPort)

	var (
		c   *client.Client
		err error
	)

	if e.cfg.TLS {
		c, err = client.DialTLS(addr, &tls.Config{ServerName: e.cfg.IMAPHost})
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		e.log.Error("IMAP connect failed", "addr", addr, "error", err)
		return
	}
	defer c.Logout() //nolint:errcheck

	if err := c.Login(e.cfg.Username, e.cfg.Password); err != nil {
		e.log.Error("IMAP login failed", "user", e.cfg.Username, "error", err)
		return
	}

	_, err = c.Select(e.cfg.Folder, false)
	if err != nil {
		e.log.Error("IMAP select folder failed", "folder", e.cfg.Folder, "error", err)
		return
	}

	// Search for unseen messages.
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	ids, err := c.Search(criteria)
	if err != nil {
		e.log.Error("IMAP search failed", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	// Fetch envelope (headers) + body text preview.
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		section.FetchItem(),
	}

	messages := make(chan *imap.Message, len(ids))
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	var toMark []uint32
	for msg := range messages {
		if e.shouldProcess(msg) {
			evt := e.toAlertEvent(msg, section)
			e.log.Info("email alert received",
				"subject", msg.Envelope.Subject,
				"from", senderAddr(msg),
			)
			onAlert(evt)
		}
		toMark = append(toMark, msg.SeqNum)
	}

	if err := <-done; err != nil {
		e.log.Warn("IMAP fetch error", "error", err)
	}

	// Mark all fetched messages as read (seen), regardless of filter match.
	if len(toMark) > 0 {
		markSet := new(imap.SeqSet)
		markSet.AddNum(toMark...)
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		if err := c.Store(markSet, item, flags, nil); err != nil {
			e.log.Warn("IMAP mark-read failed", "error", err)
		}
	}
}

// shouldProcess checks whether a message matches configured filters.
func (e *EmailSource) shouldProcess(msg *imap.Message) bool {
	if msg.Envelope == nil {
		return false
	}

	filters := e.cfg.Filters

	// Sender filter.
	if len(filters.Senders) > 0 {
		from := strings.ToLower(senderAddr(msg))
		matched := false
		for _, s := range filters.Senders {
			if strings.Contains(from, strings.ToLower(s)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Subject / keyword filter.
	if len(filters.Subjects) == 0 && len(filters.Keywords) == 0 {
		return true
	}

	subjectLower := strings.ToLower(msg.Envelope.Subject)
	for _, kw := range filters.Subjects {
		if strings.Contains(subjectLower, strings.ToLower(kw)) {
			return true
		}
	}
	for _, kw := range filters.Keywords {
		if strings.Contains(subjectLower, strings.ToLower(kw)) {
			return true
		}
	}

	return false
}

// toAlertEvent converts an IMAP message to an alert.Event.
func (e *EmailSource) toAlertEvent(msg *imap.Message, section *imap.BodySectionName) *alert.Event {
	subject := msg.Envelope.Subject
	if subject == "" {
		subject = "Email Alert"
	}

	_, severity := extractTitleAndSeverity(subject)

	// Extract plain-text body preview.
	bodyText := ""
	if r := msg.GetBody(section); r != nil {
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		bodyText = stripEmailHeaders(string(buf[:n]))
	}

	type rawEmail struct {
		Subject  string `json:"subject"`
		From     string `json:"from"`
		Date     string `json:"date"`
		Folder   string `json:"folder"`
		BodyPrev string `json:"body_preview"`
	}
	raw, _ := json.Marshal(rawEmail{
		Subject:  subject,
		From:     senderAddr(msg),
		Date:     msg.Envelope.Date.Format(time.RFC3339),
		Folder:   e.cfg.Folder,
		BodyPrev: truncate(bodyText, 500),
	})

	return &alert.Event{
		Source:      alert.SourceOutlook,
		Severity:    severity,
		Title:       truncate(subject, 200),
		Description: truncate(bodyText, 2000),
		Labels: map[string]string{
			"sender": senderAddr(msg),
			"folder": e.cfg.Folder,
		},
		RawPayload: raw,
	}
}

func senderAddr(msg *imap.Message) string {
	if msg.Envelope == nil || len(msg.Envelope.From) == 0 {
		return ""
	}
	addr := msg.Envelope.From[0]
	if addr.MailboxName != "" && addr.HostName != "" {
		return addr.MailboxName + "@" + addr.HostName
	}
	return addr.PersonalName
}

// stripEmailHeaders and truncate are in helpers.go.
