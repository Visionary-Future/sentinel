package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sentinelai/sentinel/internal/investigation"
)

// InvestigationHandler handles investigation HTTP endpoints.
type InvestigationHandler struct {
	repo   *investigation.Repository
	engine *investigation.Engine
	log    *slog.Logger
}

func NewInvestigationHandler(repo *investigation.Repository, engine *investigation.Engine, log *slog.Logger) *InvestigationHandler {
	return &InvestigationHandler{repo: repo, engine: engine, log: log}
}

// List returns a paginated list of investigations.
func (h *InvestigationHandler) List(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	offset := queryInt(c, "offset", 0)

	invs, total, err := h.repo.List(c.Request.Context(), investigation.ListParams{
		Limit:  limit,
		Offset: offset,
		Status: c.Query("status"),
	})
	if err != nil {
		h.log.Error("list investigations", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"investigations": invs,
		"total":          total,
		"limit":          limit,
		"offset":         offset,
	})
}

// Get returns a single investigation by ID.
func (h *InvestigationHandler) Get(c *gin.Context) {
	id := c.Param("id")
	inv, err := h.repo.FindByID(c.Request.Context(), id)
	if err == investigation.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "investigation not found"})
		return
	}
	if err != nil {
		h.log.Error("get investigation", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, inv)
}

// Cancel aborts a running investigation.
func (h *InvestigationHandler) Cancel(c *gin.Context) {
	id := c.Param("id")
	if err := h.engine.Cancel(id); err != nil {
		if err == investigation.ErrInvestigationNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "investigation not running or not found"})
			return
		}
		h.log.Error("cancel investigation", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "investigation cancelled"})
}

// Feedback records human evaluation of an investigation.
func (h *InvestigationHandler) Feedback(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Feedback   string `json:"feedback" binding:"required,oneof=correct incorrect"`
		HumanCause string `json:"human_cause"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.repo.UpdateFeedback(c.Request.Context(), id,
		investigation.Feedback(req.Feedback), req.HumanCause); err != nil {
		h.log.Error("update feedback", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "feedback recorded"})
}

// Stream serves SSE events for a running investigation.
func (h *InvestigationHandler) Stream(c *gin.Context) {
	id := c.Param("id")
	hub := h.engine.Hub()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	ch := hub.Subscribe(id)
	defer hub.Unsubscribe(id, ch)

	c.Stream(func(w io.Writer) bool {
		select {
		case evt, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent("step", evt)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
