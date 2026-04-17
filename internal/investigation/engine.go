package investigation

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/datasource"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/notify"
	"github.com/sentinelai/sentinel/internal/runbook"
)

const (
	defaultInvestigationTimeout = 10 * time.Minute
	defaultMaxConcurrent        = 5
	defaultDedupWindow          = 5 * time.Minute
	defaultResultCacheTTL       = 24 * time.Hour
)

// TokenBudgets maps alert severity to token budget. Missing severities
// fall back to the default budget.
type TokenBudgets struct {
	Critical int `mapstructure:"critical"` // default: 200000
	Warning  int `mapstructure:"warning"`  // default: 100000
	Info     int `mapstructure:"info"`     // default: 50000
}

func (tb TokenBudgets) forSeverity(severity string) int {
	switch severity {
	case "critical":
		if tb.Critical > 0 {
			return tb.Critical
		}
		return 200_000
	case "warning":
		if tb.Warning > 0 {
			return tb.Warning
		}
		return 100_000
	case "info":
		if tb.Info > 0 {
			return tb.Info
		}
		return 50_000
	default:
		return defaultTokenBudget
	}
}

// ModelPricing holds per-token pricing for cost estimation.
type ModelPricing struct {
	InputPerMToken  float64 // cost per million input tokens
	OutputPerMToken float64 // cost per million output tokens
}

// EngineConfig holds tunable parameters for the investigation engine.
type EngineConfig struct {
	Timeout        time.Duration          // max duration per investigation
	MaxConcurrent  int                    // max parallel investigations
	DedupWindow    time.Duration          // ignore duplicate fingerprints within this window
	ResultCacheTTL time.Duration          // reuse results for same fingerprint within this window (0 = disabled)
	TokenBudgets   TokenBudgets           // per-severity token budgets
	Pricing        map[string]ModelPricing // provider name → pricing (for cost tracking)
	WebhookURL     string                 // investigation event webhook URL
}

func (c EngineConfig) withDefaults() EngineConfig {
	if c.Timeout == 0 {
		c.Timeout = defaultInvestigationTimeout
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = defaultMaxConcurrent
	}
	if c.DedupWindow == 0 {
		c.DedupWindow = defaultDedupWindow
	}
	if c.ResultCacheTTL == 0 {
		c.ResultCacheTTL = defaultResultCacheTTL
	}
	return c
}

// Engine orchestrates alert → runbook matching → AI investigation → persistence → notification.
type Engine struct {
	invRepo  InvestigationStore
	rbRepo   RunbookStore
	agent    *Agent
	router   *llm.SeverityRouter // optional severity-based model routing
	notifier *notify.MultiChannel
	hub      *SSEHub
	cfg      EngineConfig
	log      *slog.Logger

	// graceful shutdown
	wg sync.WaitGroup

	// concurrency control
	sem chan struct{}

	// cancellation: investigation ID → cancel func
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	// dedup: fingerprint → last investigation time
	dedupMu sync.Mutex
	dedup   map[string]time.Time
}

// Sentinel errors.
var (
	ErrDuplicateInvestigation = fmt.Errorf("duplicate investigation within dedup window")
	ErrInvestigationNotFound  = fmt.Errorf("investigation not found or not running")
)

func NewEngine(
	db *sql.DB,
	invRepo InvestigationStore,
	rbRepo RunbookStore,
	alertRepo *alert.Repository,
	provider llm.Provider,
	sources *datasource.Registry,
	embedder llm.Embedder,
	notifier *notify.MultiChannel,
	log *slog.Logger,
	opts ...EngineConfig,
) *Engine {
	var cfg EngineConfig
	if len(opts) > 0 {
		cfg = opts[0]
	}
	cfg = cfg.withDefaults()

	registry := buildToolRegistry(db, alertRepo, sources, embedder)
	agent := NewAgent(provider, registry, log)

	// Check if provider supports severity routing
	var router *llm.SeverityRouter
	if r, ok := provider.(*llm.SeverityRouter); ok {
		router = r
	}

	return &Engine{
		invRepo:  invRepo,
		rbRepo:   rbRepo,
		agent:    agent,
		router:   router,
		notifier: notifier,
		hub:      NewSSEHub(),
		cfg:      cfg,
		log:      log,
		sem:      make(chan struct{}, cfg.MaxConcurrent),
		cancels:  make(map[string]context.CancelFunc),
		dedup:    make(map[string]time.Time),
	}
}

// Hub returns the SSE hub for wiring into API handlers.
func (e *Engine) Hub() *SSEHub { return e.hub }

// Resume retries a failed investigation from its last successful step.
// It reconstructs conversation history from saved steps and continues.
func (e *Engine) Resume(ctx context.Context, invID string, evt *alert.Event) error {
	inv, err := e.invRepo.FindByID(ctx, invID)
	if err != nil {
		return fmt.Errorf("find investigation: %w", err)
	}
	if inv.Status != StatusFailed {
		return fmt.Errorf("can only resume failed investigations, current status: %s", inv.Status)
	}

	runbooks, err := e.rbRepo.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list runbooks: %w", err)
	}
	rb := runbook.Match(runbooks, evt)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runAgent(evt, rb, inv)
	}()

	return nil
}

// Shutdown waits for all active investigations to finish or ctx to expire.
func (e *Engine) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cancel aborts a running investigation by its ID.
func (e *Engine) Cancel(invID string) error {
	e.cancelMu.Lock()
	cancel, ok := e.cancels[invID]
	e.cancelMu.Unlock()

	if !ok {
		return ErrInvestigationNotFound
	}

	cancel()
	e.log.Info("investigation cancelled", "investigation_id", invID)
	return nil
}

// Investigate triggers an investigation for the given alert event.
// It runs asynchronously — the caller receives the Investigation ID immediately.
func (e *Engine) Investigate(ctx context.Context, evt *alert.Event) (*Investigation, error) {
	if e.isDuplicate(evt.Fingerprint) {
		e.log.Info("skipping duplicate investigation",
			"fingerprint", evt.Fingerprint, "alert", evt.ID)
		dedupSkipped.Inc()
		return nil, ErrDuplicateInvestigation
	}

	// Result caching: check for recent successful investigation with same fingerprint
	if cached := e.findCachedResult(ctx, evt); cached != nil {
		e.log.Info("reusing cached investigation result",
			"alert_id", evt.ID, "reused_from", cached.ID)
		investigationsTotal.WithLabelValues("reused").Inc()

		reused, err := e.invRepo.Create(ctx, &Investigation{
			AlertID:    evt.ID,
			Status:     StatusReused,
			RootCause:  cached.RootCause,
			Resolution: cached.Resolution,
			Summary:    cached.Summary,
			Confidence: cached.Confidence,
			ReusedFrom: cached.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("create reused investigation: %w", err)
		}
		return reused, nil
	}

	runbooks, err := e.rbRepo.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runbooks: %w", err)
	}

	rb := runbook.Match(runbooks, evt)
	var rbID *string
	if rb != nil {
		e.log.Info("matched runbook", "runbook", rb.Name, "alert", evt.ID)
		rbID = &rb.ID
	} else {
		e.log.Info("no runbook matched, using default investigation", "alert", evt.ID)
	}

	inv, err := e.invRepo.Create(ctx, &Investigation{
		AlertID:   evt.ID,
		RunbookID: rbID,
		Status:    StatusPending,
	})
	if err != nil {
		return nil, fmt.Errorf("create investigation: %w", err)
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runAgent(evt, rb, inv)
	}()

	return inv, nil
}

func (e *Engine) runAgent(evt *alert.Event, rb *runbook.Runbook, inv *Investigation) {
	// Acquire concurrency slot
	e.sem <- struct{}{}
	defer func() { <-e.sem }()

	activeInvestigations.Inc()
	startTime := time.Now()
	defer func() {
		activeInvestigations.Dec()
		investigationDuration.Observe(time.Since(startTime).Seconds())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Timeout)
	defer cancel()

	// Register cancel func for this investigation
	e.cancelMu.Lock()
	e.cancels[inv.ID] = cancel
	e.cancelMu.Unlock()
	defer func() {
		e.cancelMu.Lock()
		delete(e.cancels, inv.ID)
		e.cancelMu.Unlock()
	}()

	running := *inv
	if err := e.invRepo.UpdateStatus(ctx, inv.ID, StatusRunning, &running); err != nil {
		e.log.Error("set running status", "investigation_id", inv.ID, "error", err)
	}

	// If severity router is available, swap the agent's provider for this run
	agent := e.agent
	if e.router != nil {
		routed := e.router.Route(string(evt.Severity))
		if routed.Name() != agent.provider.Name() {
			e.log.Info("routing to severity-specific model",
				"severity", evt.Severity,
				"provider", routed.Name(),
				"model", routed.Model(),
			)
			agent = NewAgent(routed, agent.tools, e.log)
		}
	}

	// Set severity-based token budget
	agent.tokenBudget = e.cfg.TokenBudgets.forSeverity(string(evt.Severity))
	e.log.Info("token budget set", "severity", evt.Severity, "budget", agent.tokenBudget)

	// Step callback: persist intermediate results + SSE broadcast
	onStep := func(invID string, steps []Step) {
		partial := running
		partial.Steps = steps
		if err := e.invRepo.UpdateStatus(ctx, invID, StatusRunning, &partial); err != nil {
			e.log.Warn("persist intermediate steps", "investigation_id", invID, "error", err)
		}
		if len(steps) > 0 {
			e.hub.Broadcast(invID, steps[len(steps)-1], len(steps))
		}
	}

	result, err := agent.Run(ctx, inv, evt, rb, onStep)
	if err != nil {
		e.log.Error("agent run failed", "investigation_id", inv.ID, "error", err)
		investigationsTotal.WithLabelValues("failed").Inc()
		llmCallErrors.WithLabelValues(agent.provider.Name()).Inc()
		failed := *inv
		failed.Summary = fmt.Sprintf("Investigation failed: %s", err.Error())
		if updateErr := e.invRepo.UpdateStatus(ctx, inv.ID, StatusFailed, &failed); updateErr != nil {
			e.log.Error("persist failed status", "investigation_id", inv.ID, "error", updateErr)
		}
		return
	}

	investigationsTotal.WithLabelValues("completed").Inc()
	llmTokensTotal.WithLabelValues(result.LLMProvider, result.LLMModel).Add(float64(result.TokenUsage))

	// Cost tracking
	cost := e.estimateCost(result.LLMProvider, result.TokenUsage)
	investigationCost.WithLabelValues(result.LLMProvider, result.LLMModel).Add(cost)
	investigationCostHist.Observe(cost)

	// Quality scoring
	quality := ScoreInvestigation(result)
	investigationQuality.Observe(float64(quality.Total))

	if err := e.invRepo.UpdateStatus(ctx, inv.ID, StatusCompleted, result); err != nil {
		e.log.Error("set completed status", "investigation_id", inv.ID, "error", err)
	}

	e.log.Info("investigation completed",
		"investigation_id", inv.ID,
		"tokens_used", result.TokenUsage,
		"steps", len(result.Steps),
		"confidence", result.Confidence,
		"cost_usd", cost,
		"quality_score", quality.Total,
		"needs_review", quality.NeedsReview,
	)

	// Send webhook event
	if e.cfg.WebhookURL != "" {
		sender := NewWebhookSender(e.cfg.WebhookURL, e.log)
		go sender.Send(ctx, BuildEvent(result, EventInvestigationCompleted))
	}

	// Deliver notification (non-fatal on failure).
	if e.notifier != nil {
		e.notifier.Send(ctx, &notify.Payload{
			Alert: evt,
			Investigation: &notify.InvestigationReport{
				ID:          result.ID,
				Status:      string(result.Status),
				RootCause:   result.RootCause,
				Resolution:  result.Resolution,
				Summary:     result.Summary,
				LLMProvider: result.LLMProvider,
				LLMModel:    result.LLMModel,
				TokenUsage:  result.TokenUsage,
				StepCount:   len(result.Steps),
				CompletedAt: time.Now(),
			},
		})
	}
}

// estimateCost calculates estimated USD cost based on token usage and pricing config.
func (e *Engine) estimateCost(provider string, tokens int) float64 {
	pricing, ok := e.cfg.Pricing[provider]
	if !ok {
		return 0
	}
	// Rough estimate: assume ~60% input, ~40% output tokens
	inputTokens := float64(tokens) * 0.6
	outputTokens := float64(tokens) * 0.4
	return (inputTokens * pricing.InputPerMToken / 1_000_000) + (outputTokens * pricing.OutputPerMToken / 1_000_000)
}

// findCachedResult looks for a recent successful investigation with the same fingerprint.
func (e *Engine) findCachedResult(ctx context.Context, evt *alert.Event) *Investigation {
	if evt.Fingerprint == "" || e.cfg.ResultCacheTTL == 0 {
		return nil
	}

	// Use the store's FindByAlertFingerprint if available
	store, ok := e.invRepo.(interface {
		FindByAlertFingerprint(ctx context.Context, fingerprint string) (*Investigation, error)
	})
	if !ok {
		return nil
	}

	cached, err := store.FindByAlertFingerprint(ctx, evt.Fingerprint)
	if err != nil || cached == nil {
		return nil
	}

	// Only reuse completed investigations with positive feedback or no feedback
	if cached.Status != StatusCompleted {
		return nil
	}
	if cached.Feedback == FeedbackIncorrect {
		return nil
	}
	if cached.CompletedAt == nil {
		return nil
	}
	if time.Since(*cached.CompletedAt) > e.cfg.ResultCacheTTL {
		return nil
	}

	return cached
}

// isDuplicate returns true if an alert with the same fingerprint was investigated recently.
func (e *Engine) isDuplicate(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}

	e.dedupMu.Lock()
	defer e.dedupMu.Unlock()

	now := time.Now()

	// Lazy cleanup of expired entries
	for fp, ts := range e.dedup {
		if now.Sub(ts) > e.cfg.DedupWindow {
			delete(e.dedup, fp)
		}
	}

	if lastSeen, ok := e.dedup[fingerprint]; ok {
		if now.Sub(lastSeen) < e.cfg.DedupWindow {
			return true
		}
	}

	e.dedup[fingerprint] = now
	return false
}

// buildToolRegistry wires up the default tool set.
func buildToolRegistry(db *sql.DB, alertRepo *alert.Repository, sources *datasource.Registry, embedder llm.Embedder) *tool.Registry {
	r := tool.NewRegistry()
	r.Register(tool.QueryLogsTool, tool.QueryLogs(sources))
	r.Register(tool.QueryMetricsTool, tool.QueryMetrics(sources))
	r.Register(tool.SearchHistoryTool, tool.SearchHistory(db, alertRepo, embedder))
	r.Register(tool.CheckDeploymentsTool, tool.CheckDeployments(nil))
	r.Register(tool.SearchCodeTool, tool.SearchCode(nil))
	r.Register(tool.QueryTracesTool, tool.QueryTraces(nil))
	r.Register(tool.ConcludeTool, tool.ConcludeInvestigation())
	return r
}
