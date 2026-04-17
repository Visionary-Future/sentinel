package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/alert/source"
	"github.com/sentinelai/sentinel/internal/api"
	"github.com/sentinelai/sentinel/internal/api/handler"
	"github.com/sentinelai/sentinel/internal/config"
	"github.com/sentinelai/sentinel/internal/datasource"
	datasourcealiyun "github.com/sentinelai/sentinel/internal/datasource/aliyun"
	"github.com/sentinelai/sentinel/internal/investigation"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/notify"
	"github.com/sentinelai/sentinel/internal/runbook"
	"github.com/sentinelai/sentinel/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/sentinel.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Database
	db, err := store.Open(cfg.Database)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := store.Migrate(db, "migrations"); err != nil {
		log.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	log.Info("database migrations applied")

	// Repositories
	alertRepo := alert.NewRepository(db)
	runbookRepo := runbook.NewRepository(db)
	invRepo := investigation.NewRepository(db)

	// Data sources
	sources := buildDataSourceRegistry(cfg.DataSources, log)

	// Embedder (optional — enables vector search in search_history)
	embedder := buildEmbedder(cfg.Embedding, log)

	// Notification channels
	notifier := buildNotifier(cfg.Notify, log)

	// LLM Provider
	provider := buildLLMProvider(cfg.LLM, log)

	// Investigation Engine
	engine := investigation.NewEngine(db, invRepo, runbookRepo, alertRepo, provider, sources, embedder, notifier, log)

	// Alert sources
	webhookSrc := source.NewWebhook(cfg.Alert.Webhook, log)
	sources2 := []source.Handler{webhookSrc}

	if cfg.Alert.Slack.Enabled {
		slackSrc, err := source.NewSlack(cfg.Alert.Slack, log)
		if err != nil {
			log.Error("failed to init Slack source", "error", err)
			os.Exit(1)
		}
		sources2 = append(sources2, slackSrc)
	}

	if cfg.Alert.Outlook.Enabled {
		outlookSrc, err := source.NewOutlook(cfg.Alert.Outlook, log)
		if err != nil {
			log.Error("failed to init Outlook source", "error", err)
			os.Exit(1)
		}
		sources2 = append(sources2, outlookSrc)
	}

	// Root context — cancelled on SIGTERM/SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Alert handler: normalise → save → embed → investigate
	onAlert := func(evt *alert.Event) {
		handleAlert(ctx, evt, alertRepo, embedder, engine, log)
	}

	// Start all alert sources
	for _, src := range sources2 {
		s := src
		go func() {
			if err := s.Start(ctx, onAlert); err != nil {
				log.Error("alert source error", "source", s.Name(), "error", err)
			}
		}()
	}

	// HTTP server
	deps := &handler.Deps{
		Alert:         handler.NewAlertHandler(alertRepo, webhookSrc, log),
		Runbook:       handler.NewRunbookHandler(runbookRepo, log),
		Investigation: handler.NewInvestigationHandler(invRepo, log),
	}
	srv := api.New(cfg.Server, deps, log)

	go func() {
		if err := srv.Start(); err != nil {
			log.Error("http server error", "error", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "error", err)
	}
}

// handleAlert normalises, persists, embeds, and triggers investigation for an alert.
func handleAlert(ctx context.Context, evt *alert.Event, repo *alert.Repository, embedder llm.Embedder, engine *investigation.Engine, log *slog.Logger) {
	normalised := alert.Normalize(evt)

	saved, err := repo.Save(ctx, normalised)
	if err == alert.ErrDuplicate {
		log.Debug("duplicate alert, skipping", "fingerprint", normalised.Fingerprint)
		return
	}
	if err != nil {
		log.Error("failed to save alert", "error", err, "title", normalised.Title)
		return
	}

	log.Info("alert saved",
		"id", saved.ID,
		"source", saved.Source,
		"severity", saved.Severity,
		"title", saved.Title,
	)

	// Async embedding — non-blocking, best-effort
	if embedder != nil {
		go func() {
			text := saved.Title + " " + saved.Description
			vec, err := embedder.Embed(context.Background(), text)
			if err != nil {
				log.Warn("embed alert failed", "alert_id", saved.ID, "error", err)
				return
			}
			if err := repo.UpdateEmbedding(context.Background(), saved.ID, vec); err != nil {
				log.Warn("store embedding failed", "alert_id", saved.ID, "error", err)
			}
		}()
	}

	inv, err := engine.Investigate(ctx, saved)
	if err != nil {
		log.Error("failed to start investigation", "alert_id", saved.ID, "error", err)
		return
	}
	log.Info("investigation started", "investigation_id", inv.ID, "alert_id", saved.ID)
}

// buildEmbedder constructs the embedder from config, returns nil if disabled.
func buildEmbedder(cfg config.EmbeddingConfig, log *slog.Logger) llm.Embedder {
	if !cfg.Enabled || cfg.APIKey == "" {
		log.Info("embedding disabled — vector search will use keyword fallback")
		return nil
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		switch cfg.Provider {
		case "tongyi":
			baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}

	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}

	log.Info("embedding enabled", "provider", cfg.Provider, "model", model)
	return llm.NewOpenAIEmbedder(cfg.APIKey, model, baseURL, cfg.Dims)
}

// buildDataSourceRegistry wires configured observability backends.
func buildDataSourceRegistry(cfg config.DataSourcesConfig, log *slog.Logger) *datasource.Registry {
	r := datasource.NewRegistry()

	if cfg.AliyunSLS.Enabled {
		sls, err := datasourcealiyun.NewSLS(cfg.AliyunSLS)
		if err != nil {
			log.Warn("failed to init Aliyun SLS", "error", err)
		} else {
			r.Register(sls)
			log.Info("data source registered", "source", "aliyun_sls")
		}
	}

	if cfg.AliyunCMS.Enabled {
		cms, err := datasourcealiyun.NewCMS(cfg.AliyunCMS)
		if err != nil {
			log.Warn("failed to init Aliyun CMS", "error", err)
		} else {
			r.Register(cms)
			log.Info("data source registered", "source", "aliyun_cms")
		}
	}

	if r.Len() == 0 {
		log.Info("no data sources configured — query_logs/query_metrics will return stubs")
	}

	return r
}

// buildNotifier constructs the MultiChannel notifier from config.
func buildNotifier(cfg config.NotifyConfig, log *slog.Logger) *notify.MultiChannel {
	var channels []notify.Channel

	if cfg.WeCom.Enabled && cfg.WeCom.WebhookURL != "" {
		channels = append(channels, notify.NewWeCom(cfg.WeCom))
		log.Info("notification channel enabled", "channel", "wecom")
	}
	if cfg.DingTalk.Enabled && cfg.DingTalk.WebhookURL != "" {
		channels = append(channels, notify.NewDingTalk(cfg.DingTalk))
		log.Info("notification channel enabled", "channel", "dingtalk")
	}
	if cfg.Slack.Enabled && cfg.Slack.BotToken != "" {
		channels = append(channels, notify.NewSlack(cfg.Slack))
		log.Info("notification channel enabled", "channel", "slack",
			"reply_in_thread", cfg.Slack.ReplyInThread)
	}

	if len(channels) == 0 {
		log.Info("no notification channels configured")
	}

	return notify.NewMultiChannel(channels...)
}

// buildLLMProvider constructs the LLM provider from config.
func buildLLMProvider(cfg config.LLMConfig, log *slog.Logger) llm.Provider {
	p, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok || p.APIKey == "" {
		log.Warn("no LLM provider configured, using noop provider",
			"default_provider", cfg.DefaultProvider)
		return &noopProvider{}
	}

	switch cfg.DefaultProvider {
	case "claude":
		log.Info("using LLM provider", "provider", "claude", "model", p.Model)
		return llm.NewClaude(p.APIKey, p.Model)
	case "tongyi":
		log.Info("using LLM provider", "provider", "tongyi", "model", p.Model)
		return llm.NewTongyi(p.APIKey, p.Model)
	default:
		log.Info("using LLM provider", "provider", "openai_compat", "model", p.Model)
		return llm.NewOpenAICompat(p.APIKey, p.Model, "https://api.openai.com/v1")
	}
}

// noopProvider is used when no LLM API key is configured.
type noopProvider struct{}

func (n *noopProvider) Name() string  { return "noop" }
func (n *noopProvider) Model() string { return "noop" }
func (n *noopProvider) Chat(_ context.Context, _ string, _ []llm.Message, _ []llm.Tool) (*llm.Response, error) {
	return &llm.Response{
		Content:    "## Root Cause\nNo LLM provider configured.\n\n## Resolution\nSet llm.providers.claude.api_key in config.\n\n## Summary\nNoop investigation — configure an LLM to enable AI analysis.",
		StopReason: llm.StopReasonEndTurn,
	}, nil
}
