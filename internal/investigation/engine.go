package investigation

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/datasource"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/notify"
	"github.com/sentinelai/sentinel/internal/runbook"
)

// Engine orchestrates alert → runbook matching → AI investigation → persistence → notification.
type Engine struct {
	invRepo   *Repository
	rbRepo    *runbook.Repository
	alertRepo *alert.Repository
	agent     *Agent
	notifier  *notify.MultiChannel
	log       *slog.Logger
}

func NewEngine(
	db *sql.DB,
	invRepo *Repository,
	rbRepo *runbook.Repository,
	alertRepo *alert.Repository,
	provider llm.Provider,
	sources *datasource.Registry,
	embedder llm.Embedder,
	notifier *notify.MultiChannel,
	log *slog.Logger,
) *Engine {
	registry := buildToolRegistry(db, alertRepo, sources, embedder)
	agent := NewAgent(provider, registry, log)

	return &Engine{
		invRepo:   invRepo,
		rbRepo:    rbRepo,
		alertRepo: alertRepo,
		agent:     agent,
		notifier:  notifier,
		log:       log,
	}
}

// Investigate triggers an investigation for the given alert event.
// It runs asynchronously — the caller receives the Investigation ID immediately.
func (e *Engine) Investigate(ctx context.Context, evt *alert.Event) (*Investigation, error) {
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

	go e.runAgent(evt, rb, inv)

	return inv, nil
}

func (e *Engine) runAgent(evt *alert.Event, rb *runbook.Runbook, inv *Investigation) {
	ctx := context.Background()

	if err := e.invRepo.UpdateStatus(ctx, inv.ID, StatusRunning, inv); err != nil {
		e.log.Error("set running status", "investigation_id", inv.ID, "error", err)
	}

	result, err := e.agent.Run(ctx, inv, evt, rb)
	if err != nil {
		e.log.Error("agent run failed", "investigation_id", inv.ID, "error", err)
		inv.Summary = fmt.Sprintf("Investigation failed: %s", err.Error())
		_ = e.invRepo.UpdateStatus(ctx, inv.ID, StatusFailed, inv)
		return
	}

	if err := e.invRepo.UpdateStatus(ctx, inv.ID, StatusCompleted, result); err != nil {
		e.log.Error("set completed status", "investigation_id", inv.ID, "error", err)
	}

	e.log.Info("investigation completed",
		"investigation_id", inv.ID,
		"tokens_used", result.TokenUsage,
		"steps", len(result.Steps),
	)

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

// buildToolRegistry wires up the default tool set.
func buildToolRegistry(db *sql.DB, alertRepo *alert.Repository, sources *datasource.Registry, embedder llm.Embedder) *tool.Registry {
	r := tool.NewRegistry()
	r.Register(tool.QueryLogsTool, tool.QueryLogs(sources))
	r.Register(tool.QueryMetricsTool, tool.QueryMetrics(sources))
	r.Register(tool.SearchHistoryTool, tool.SearchHistory(db, alertRepo, embedder))
	return r
}
