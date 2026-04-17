package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackSource listens to Slack channels via Socket Mode and emits alert events.
type SlackSource struct {
	cfg    config.SlackSourceConfig
	client *slack.Client
	socket *socketmode.Client
	log    *slog.Logger
}

// NewSlack creates a Slack alert source. It validates the config before returning.
func NewSlack(cfg config.SlackSourceConfig, log *slog.Logger) (*SlackSource, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	if cfg.AppToken == "" {
		return nil, fmt.Errorf("slack app_token is required (Socket Mode)")
	}

	client := slack.New(cfg.BotToken,
		slack.OptionAppLevelToken(cfg.AppToken),
	)
	socket := socketmode.New(client,
		socketmode.OptionLog(slog.NewLogLogger(log.Handler(), slog.LevelDebug)),
	)

	return &SlackSource{
		cfg:    cfg,
		client: client,
		socket: socket,
		log:    log.With("source", "slack"),
	}, nil
}

func (s *SlackSource) Name() string { return "slack" }

// Start connects via Socket Mode and forwards matching messages as alert events.
func (s *SlackSource) Start(ctx context.Context, onAlert func(*alert.Event)) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-s.socket.Events:
				if !ok {
					return
				}
				s.handleSocketEvent(evt, onAlert)
			}
		}
	}()

	s.log.Info("connecting to Slack via Socket Mode")
	return s.socket.RunContext(ctx)
}

func (s *SlackSource) handleSocketEvent(evt socketmode.Event, onAlert func(*alert.Event)) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		s.socket.Ack(*evt.Request)
		eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		s.handleAPIEvent(eventsAPI, onAlert)

	case socketmode.EventTypeHello:
		s.log.Info("socket mode connected")
	}
}

func (s *SlackSource) handleAPIEvent(evt slackevents.EventsAPIEvent, onAlert func(*alert.Event)) {
	if evt.Type != slackevents.CallbackEvent {
		return
	}

	msgEvt, ok := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}

	// Ignore bot_message sub-types we're not interested in, and edits/deletes
	if msgEvt.SubType != "" && msgEvt.SubType != "bot_message" {
		return
	}

	if !s.isWatchedChannel(msgEvt.Channel) {
		return
	}

	channelCfg := s.channelConfig(msgEvt.Channel)
	if !s.matchesFilters(msgEvt.Text, channelCfg) {
		return
	}

	alertEvt := s.toAlertEvent(msgEvt, channelCfg)
	s.log.Info("received alert from Slack",
		"channel", msgEvt.Channel,
		"title", alertEvt.Title,
	)
	onAlert(alertEvt)
}

func (s *SlackSource) isWatchedChannel(channelID string) bool {
	if len(s.cfg.Channels) == 0 {
		return true // watch all channels if none configured
	}
	for _, ch := range s.cfg.Channels {
		if ch.ID == channelID {
			return true
		}
	}
	return false
}

func (s *SlackSource) channelConfig(channelID string) *config.ChannelConfig {
	for i := range s.cfg.Channels {
		if s.cfg.Channels[i].ID == channelID {
			return &s.cfg.Channels[i]
		}
	}
	return nil
}

func (s *SlackSource) matchesFilters(text string, ch *config.ChannelConfig) bool {
	if ch == nil || len(ch.Filters.Keywords) == 0 {
		return true
	}
	lower := strings.ToLower(text)
	for _, kw := range ch.Filters.Keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func (s *SlackSource) toAlertEvent(msg *slackevents.MessageEvent, ch *config.ChannelConfig) *alert.Event {
	channelName := msg.Channel
	if ch != nil {
		channelName = ch.Name
	}

	title, severity := extractTitleAndSeverity(msg.Text)
	rawPayload, _ := json.Marshal(msg)

	return &alert.Event{
		Source:         alert.SourceSlack,
		Severity:       severity,
		Title:          title,
		Description:    msg.Text,
		Service:        "", // enriched later by the normalizer / catalog lookup
		Labels:         map[string]string{"slack_channel": channelName},
		RawPayload:     rawPayload,
		SlackChannelID: msg.Channel,
		SlackMessageTS: msg.TimeStamp,
		SlackThreadTS:  msg.ThreadTimeStamp,
	}
}

// extractTitleAndSeverity does a best-effort parse of the Slack message text.
// It looks for common patterns emitted by Prometheus AlertManager, Datadog, etc.
func extractTitleAndSeverity(text string) (title string, severity alert.Severity) {
	severity = alert.SeverityWarning

	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "critical") || strings.Contains(lower, "p0") || strings.Contains(lower, "🔴"):
		severity = alert.SeverityCritical
	case strings.Contains(lower, "warning") || strings.Contains(lower, "p1") || strings.Contains(lower, "🟡"):
		severity = alert.SeverityWarning
	case strings.Contains(lower, "info") || strings.Contains(lower, "🟢"):
		severity = alert.SeverityInfo
	}

	// Use the first non-empty line as the title (max 200 chars)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		return line, severity
	}

	return "Slack Alert", severity
}
