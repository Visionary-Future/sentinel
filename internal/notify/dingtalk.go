package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/config"
)

// DingTalkChannel sends investigation reports to a DingTalk group robot webhook.
// Supports optional HMAC-SHA256 timestamp signing required by some DingTalk robots.
type DingTalkChannel struct {
	webhookURL string
	secret     string
	client     *http.Client
}

func NewDingTalk(cfg config.DingTalkNotifyConfig) *DingTalkChannel {
	return &DingTalkChannel{
		webhookURL: cfg.WebhookURL,
		secret:     cfg.Secret,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *DingTalkChannel) Name() string { return "dingtalk" }

func (d *DingTalkChannel) Send(ctx context.Context, p *Payload) error {
	text := buildDingTalkMarkdown(p)
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("[%s] %s", strings.ToUpper(string(p.Alert.Severity)), p.Alert.Title),
			"text":  text,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal payload: %w", err)
	}

	endpoint := d.signedURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dingtalk: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk: unexpected status %d", resp.StatusCode)
	}

	return nil
}

// signedURL appends timestamp + sign query params when a secret is configured.
func (d *DingTalkChannel) signedURL() string {
	if d.secret == "" {
		return d.webhookURL
	}

	timestamp := time.Now().UnixMilli()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, d.secret)

	mac := hmac.New(sha256.New, []byte(d.secret))
	mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	separator := "&"
	if !strings.Contains(d.webhookURL, "?") {
		separator = "?"
	}
	return fmt.Sprintf("%s%stimestamp=%d&sign=%s",
		d.webhookURL, separator, timestamp, url.QueryEscape(sign))
}

func buildDingTalkMarkdown(p *Payload) string {
	inv := p.Investigation
	evt := p.Alert

	severityEmoji := map[string]string{
		"critical": "🔴",
		"warning":  "🟡",
		"info":     "🔵",
	}
	emoji := severityEmoji[strings.ToLower(string(evt.Severity))]
	if emoji == "" {
		emoji = "⚪"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s Alert Investigation Report\n\n", emoji))
	sb.WriteString(fmt.Sprintf("**Alert:** %s  \n", evt.Title))
	if evt.Service != "" {
		sb.WriteString(fmt.Sprintf("**Service:** %s  \n", evt.Service))
	}
	sb.WriteString(fmt.Sprintf("**Severity:** %s  \n\n", string(evt.Severity)))

	if inv.RootCause != "" {
		sb.WriteString(fmt.Sprintf("### Root Cause\n%s\n\n", inv.RootCause))
	}
	if inv.Resolution != "" {
		sb.WriteString(fmt.Sprintf("### Resolution\n%s\n\n", inv.Resolution))
	}
	if inv.Summary != "" {
		sb.WriteString(fmt.Sprintf("### Summary\n%s\n\n", inv.Summary))
	}

	sb.WriteString(fmt.Sprintf("---\nID: `%s` | %s/%s | %d tokens | %d steps",
		inv.ID, inv.LLMProvider, inv.LLMModel, inv.TokenUsage, inv.StepCount))

	return sb.String()
}
