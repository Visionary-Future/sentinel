package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sentinelai/sentinel/internal/investigation"
)

// InvestigationHandler handles investigation HTTP endpoints.
type InvestigationHandler struct {
	repo *investigation.Repository
	log  *slog.Logger
}

func NewInvestigationHandler(repo *investigation.Repository, log *slog.Logger) *InvestigationHandler {
	return &InvestigationHandler{repo: repo, log: log}
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
