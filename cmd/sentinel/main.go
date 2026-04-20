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
	engine := investigation.NewEngine(db, invRepo, runbookRepo, alertRepo, provider, sources, embedder, notifier, log,
		buildEngineConfig(cfg.Investigation),
	)

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

	// Alert correlation (optional): buffer alerts by service within a time window
	var correlator *investigation.Correlator
	if cfg.Investigation.CorrelationEnabled {
		window := time.Duration(cfg.Investigation.CorrelationWindowSec) * time.Second
		correlator = investigation.NewCorrelator(window, func(group *investigation.AlertGroup) {
			inv, err := engine.Investigate(ctx, group.PrimaryAlert)
			if err != nil && err != investigation.ErrDuplicateInvestigation {
				log.Error("investigation from correlator failed",
					"alert_id", group.PrimaryAlert.ID,
					"related_count", len(group.Related),
					"error", err)
				return
			}
			if inv != nil {
				log.Info("correlated investigation started",
					"investigation_id", inv.ID,
					"primary_alert", group.PrimaryAlert.ID,
					"related_count", len(group.Related))
			}
		})
		log.Info("alert correlation enabled", "window", window)
	}

	// Alert handler: normalise → save → embed → correlate/investigate
	onAlert := func(evt *alert.Event) {
		handleAlert(ctx, evt, alertRepo, embedder, engine, correlator, log)
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
		Investigation: handler.NewInvestigationHandler(invRepo, engine, log),
	}
	srv := api.New(cfg.Server, deps, log)

	go func() {
		if err := srv.Start(); err != nil {
			log.Error("http server error", "error", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "error", err)
	}

	// Stop correlator first so it flushes pending groups
	if correlator != nil {
		correlator.Stop()
	}

	// Wait for active investigations to finish (or timeout)
	if err := engine.Shutdown(shutdownCtx); err != nil {
		log.Error("investigation engine shutdown timeout", "error", err)
	}
	log.Info("shutdown complete")
}

// handleAlert normalises, persists, embeds, and triggers investigation for an alert.
func handleAlert(ctx context.Context, evt *alert.Event, repo *alert.Repository, embedder llm.Embedder, engine *investigation.Engine, correlator *investigation.Correlator, log *slog.Logger) {
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

	// If correlator is enabled, buffer the alert for grouping; otherwise investigate directly
	if correlator != nil {
		correlator.Add(saved)
		log.Debug("alert queued for correlation", "alert_id", saved.ID, "service", saved.Service)
		return
	}

	inv, err := engine.Investigate(ctx, saved)
	if err == investigation.ErrDuplicateInvestigation {
		log.Debug("duplicate investigation skipped", "alert_id", saved.ID)
		return
	}
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

	return notify.NewMultiChannel(log, channels...)
}

// buildLLMProvider constructs the LLM provider from config.
func buildLLMProvider(cfg config.LLMConfig, log *slog.Logger) llm.Provider {
	var providers []llm.Provider
	providersByName := make(map[string]llm.Provider)

	// Build provider for each configured entry
	for name, p := range cfg.Providers {
		if p.APIKey == "" {
			continue
		}
		var provider llm.Provider
		switch name {
		case "claude":
			provider = llm.NewClaude(p.APIKey, p.Model)
		case "tongyi":
			provider = llm.NewTongyi(p.APIKey, p.Model)
		default:
			baseURL := p.BaseURL
			if baseURL == "" {
				baseURL = "https://api.openai.com/v1"
			}
			provider = llm.NewOpenAICompat(p.APIKey, p.Model, baseURL)
		}
		providers = append(providers, provider)
		providersByName[name] = provider
		log.Info("LLM provider configured", "provider", name, "model", p.Model)
	}

	// Put the default provider first in the chain
	if cfg.DefaultProvider != "" {
		sorted := make([]llm.Provider, 0, len(providers))
		for _, p := range providers {
			if p.Name() == cfg.DefaultProvider {
				sorted = append([]llm.Provider{p}, sorted...)
			} else {
				sorted = append(sorted, p)
			}
		}
		providers = sorted
	}

	if len(providers) == 0 {
		log.Warn("no LLM provider configured, using noop provider")
		return &noopProvider{}
	}

	if len(providers) == 1 {
		return wrapLLMProvider(providers[0], cfg, providersByName, log)
	}

	log.Info("LLM fallback chain configured", "count", len(providers))
	return wrapLLMProvider(llm.NewFallbackProvider(log, providers...), cfg, providersByName, log)
}

// wrapLLMProvider applies optional CachingProvider and SeverityRouter layers.
func wrapLLMProvider(base llm.Provider, cfg config.LLMConfig, providersByName map[string]llm.Provider, log *slog.Logger) llm.Provider {
	var p llm.Provider = base

	// Wrap with prompt caching
	if cfg.PromptCaching {
		p = llm.NewCachingProvider(p)
		log.Info("LLM prompt caching enabled")
	}

	// Wrap with severity routing if configured
	if len(cfg.SeverityRouting) > 0 {
		routes := make(map[string]llm.Provider)
		for severity, providerName := range cfg.SeverityRouting {
			if prov, ok := providersByName[providerName]; ok {
				routes[severity] = prov
				log.Info("severity routing configured", "severity", severity, "provider", providerName)
			}
		}
		if len(routes) > 0 {
			p = llm.NewSeverityRouter(p, routes)
		}
	}

	return p
}

// buildEngineConfig maps config values to EngineConfig with defaults.
func buildEngineConfig(cfg config.InvestigationConfig) investigation.EngineConfig {
	ec := investigation.EngineConfig{}
	if cfg.TimeoutMinutes > 0 {
		ec.Timeout = time.Duration(cfg.TimeoutMinutes) * time.Minute
	}
	if cfg.MaxConcurrent > 0 {
		ec.MaxConcurrent = cfg.MaxConcurrent
	}
	if cfg.DedupWindowMin > 0 {
		ec.DedupWindow = time.Duration(cfg.DedupWindowMin) * time.Minute
	}
	ec.TokenBudgets = investigation.TokenBudgets{
		Critical: cfg.TokenBudgets.Critical,
		Warning:  cfg.TokenBudgets.Warning,
		Info:     cfg.TokenBudgets.Info,
	}
	return ec
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
