package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/alert/source"
)

// AlertHandler handles alert-related HTTP endpoints.
type AlertHandler struct {
	repo    *alert.Repository
	webhook *source.WebhookSource
	log     *slog.Logger
}

func NewAlertHandler(repo *alert.Repository, webhook *source.WebhookSource, log *slog.Logger) *AlertHandler {
	return &AlertHandler{repo: repo, webhook: webhook, log: log}
}

// Webhook receives a generic JSON alert payload and enqueues it for processing.
func (h *AlertHandler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20)) // 1 MB limit
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	if err := h.webhook.ParseAndEnqueue(body); err != nil {
		h.log.Warn("webhook parse error", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
}

// List returns a paginated list of alert events.
func (h *AlertHandler) List(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	offset := queryInt(c, "offset", 0)

	events, total, err := h.repo.List(c.Request.Context(), alert.ListParams{
		Limit:  limit,
		Offset: offset,
		Status: c.Query("status"),
	})
	if err != nil {
		h.log.Error("list alerts", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"alerts": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Alertmanager receives Prometheus Alertmanager v4 webhook payloads.
func (h *AlertHandler) Alertmanager(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	events, err := source.ParseAlertmanager(body)
	if err != nil {
		h.log.Warn("alertmanager parse error", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, evt := range events {
		h.webhook.Enqueue(evt)
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "count": len(events)})
}

// GrafanaWebhook receives Grafana unified alerting webhook payloads.
func (h *AlertHandler) GrafanaWebhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	events, err := source.ParseGrafana(body)
	if err != nil {
		h.log.Warn("grafana parse error", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, evt := range events {
		h.webhook.Enqueue(evt)
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "count": len(events)})
}

// Get returns a single alert by ID.
func (h *AlertHandler) Get(c *gin.Context) {
	id := c.Param("id")
	evt, err := h.repo.FindByID(c.Request.Context(), id)
	if err == alert.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert not found"})
		return
	}
	if err != nil {
		h.log.Error("get alert", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, evt)
}
