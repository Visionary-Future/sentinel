package notify

import (
	"context"
	"fmt"
	"strings"

	slackgo "github.com/slack-go/slack"

	"github.com/sentinelai/sentinel/internal/config"
)

// SlackChannel sends investigation reports via the Slack Web API (Bot Token).
// It is completely independent from the Slack alert source, which uses Socket Mode
// to ingest alerts. This channel only posts outbound messages.
//
// If reply_in_thread is true AND the originating alert came from Slack
// (Alert.SlackChannelID and Alert.SlackMessageTS are set), the report is posted
// as a thread reply to the original alert message. Otherwise it falls back to
// default_channel.
type SlackChannel struct {
	client         *slackgo.Client
	defaultChannel string
	replyInThread  bool
}

func NewSlack(cfg config.SlackNotifyConfig) *SlackChannel {
	return &SlackChannel{
		client:         slackgo.New(cfg.BotToken),
		defaultChannel: cfg.DefaultChannel,
		replyInThread:  cfg.ReplyInThread,
	}
}

func (s *SlackChannel) Name() string { return "slack" }

func (s *SlackChannel) Send(ctx context.Context, p *Payload) error {
	channel, threadTS := s.resolveTarget(p)
	if channel == "" {
		return fmt.Errorf("slack notify: no target channel configured")
	}

	blocks := buildSlackBlocks(p)
	opts := []slackgo.MsgOption{
		slackgo.MsgOptionBlocks(blocks...),
		slackgo.MsgOptionText(fallbackText(p), false), // plain-text fallback for notifications
	}
	if threadTS != "" {
		opts = append(opts, slackgo.MsgOptionTS(threadTS))
	}

	_, _, err := s.client.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		return fmt.Errorf("slack notify: post message: %w", err)
	}
	return nil
}

// resolveTarget determines which channel and thread to reply to.
// Priority: thread reply in originating channel > default_channel.
func (s *SlackChannel) resolveTarget(p *Payload) (channel, threadTS string) {
	if s.replyInThread && p.Alert.SlackChannelID != "" && p.Alert.SlackMessageTS != "" {
		return p.Alert.SlackChannelID, p.Alert.SlackMessageTS
	}
	return s.defaultChannel, ""
}

// buildSlackBlocks produces a Block Kit message for the investigation report.
func buildSlackBlocks(p *Payload) []slackgo.Block {
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

	var blocks []slackgo.Block

	// Header
	blocks = append(blocks, slackgo.NewHeaderBlock(
		slackgo.NewTextBlockObject(slackgo.PlainTextType,
			fmt.Sprintf("%s Investigation Report", emoji), false, false),
	))

	// Alert info
	alertText := fmt.Sprintf("*Alert:* %s\n*Severity:* %s", evt.Title, string(evt.Severity))
	if evt.Service != "" {
		alertText += fmt.Sprintf("\n*Service:* %s", evt.Service)
	}
	blocks = append(blocks, slackgo.NewSectionBlock(
		slackgo.NewTextBlockObject(slackgo.MarkdownType, alertText, false, false),
		nil, nil,
	))

	blocks = append(blocks, slackgo.NewDividerBlock())

	// Root cause
	if inv.RootCause != "" {
		blocks = append(blocks, slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType,
				fmt.Sprintf("*Root Cause*\n%s", inv.RootCause), false, false),
			nil, nil,
		))
	}

	// Resolution
	if inv.Resolution != "" {
		blocks = append(blocks, slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType,
				fmt.Sprintf("*Resolution*\n%s", inv.Resolution), false, false),
			nil, nil,
		))
	}

	// Summary
	if inv.Summary != "" {
		blocks = append(blocks, slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType,
				fmt.Sprintf("*Summary*\n%s", inv.Summary), false, false),
			nil, nil,
		))
	}

	blocks = append(blocks, slackgo.NewDividerBlock())

	// Footer: metadata
	footer := fmt.Sprintf("`%s` · %s/%s · %d tokens · %d steps · %s",
		inv.ID, inv.LLMProvider, inv.LLMModel, inv.TokenUsage, inv.StepCount,
		inv.CompletedAt.Format("2006-01-02 15:04:05"),
	)
	blocks = append(blocks, slackgo.NewContextBlock("",
		slackgo.NewTextBlockObject(slackgo.MarkdownType, footer, false, false),
	))

	return blocks
}

// fallbackText is the plain-text notification summary (shown in push notifications).
func fallbackText(p *Payload) string {
	return fmt.Sprintf("[%s] %s — investigation complete",
		strings.ToUpper(string(p.Alert.Severity)), p.Alert.Title)
}
