package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sentinelai/sentinel/internal/config"
)

// WeComChannel sends investigation reports to a WeCom group robot webhook.
type WeComChannel struct {
	webhookURL string
	client     *http.Client
}

func NewWeCom(cfg config.WeComNotifyConfig) *WeComChannel {
	return &WeComChannel{
		webhookURL: cfg.WebhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *WeComChannel) Name() string { return "wecom" }

func (w *WeComChannel) Send(ctx context.Context, p *Payload) error {
	body := buildWeComMarkdown(p)
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": body,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("wecom: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("wecom: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("wecom: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wecom: unexpected status %d", resp.StatusCode)
	}

	return nil
}

func buildWeComMarkdown(p *Payload) string {
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

	statusLabel := "✅ Resolved"
	if inv.RootCause != "" && strings.Contains(strings.ToLower(inv.RootCause), "unknown") {
		statusLabel = "⚠️ Needs Attention"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s Alert Investigation Report\n\n", emoji))
	sb.WriteString(fmt.Sprintf("**Alert:** %s\n", evt.Title))
	if evt.Service != "" {
		sb.WriteString(fmt.Sprintf("**Service:** %s\n", evt.Service))
	}
	sb.WriteString(fmt.Sprintf("**Severity:** %s\n", string(evt.Severity)))
	sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", statusLabel))

	if inv.RootCause != "" {
		sb.WriteString(fmt.Sprintf("**Root Cause:**\n>%s\n\n", inv.RootCause))
	}
	if inv.Resolution != "" {
		sb.WriteString(fmt.Sprintf("**Resolution:**\n>%s\n\n", inv.Resolution))
	}
	if inv.Summary != "" {
		sb.WriteString(fmt.Sprintf("**Summary:**\n%s\n\n", inv.Summary))
	}

	sb.WriteString(fmt.Sprintf("**Investigation ID:** `%s`\n", inv.ID))
	sb.WriteString(fmt.Sprintf("**LLM:** %s / %s | **Tokens:** %d\n",
		inv.LLMProvider, inv.LLMModel, inv.TokenUsage))
	sb.WriteString(fmt.Sprintf("**Steps:** %d | **Completed:** %s",
		inv.StepCount, inv.CompletedAt.Format("2006-01-02 15:04:05")))

	return sb.String()
}
