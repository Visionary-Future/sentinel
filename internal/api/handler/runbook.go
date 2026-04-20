package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	rb "github.com/sentinelai/sentinel/internal/runbook"
)

// RunbookHandler handles runbook CRUD endpoints.
type RunbookHandler struct {
	repo *rb.Repository
	log  *slog.Logger
}

func NewRunbookHandler(repo *rb.Repository, log *slog.Logger) *RunbookHandler {
	return &RunbookHandler{repo: repo, log: log}
}

// Create parses a Markdown runbook from the request body and persists it.
func (h *RunbookHandler) Create(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	parsed := rb.Parse(string(body))
	parsed.Enabled = true

	if parsed.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "runbook must have a # Title"})
		return
	}

	saved, err := h.repo.Save(c.Request.Context(), parsed)
	if err != nil {
		h.log.Error("create runbook", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, saved)
}

// List returns all enabled runbooks.
func (h *RunbookHandler) List(c *gin.Context) {
	runbooks, err := h.repo.ListEnabled(c.Request.Context())
	if err != nil {
		h.log.Error("list runbooks", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runbooks": runbooks})
}

// Get returns a single runbook by ID.
func (h *RunbookHandler) Get(c *gin.Context) {
	id := c.Param("id")
	runbook, err := h.repo.FindByID(c.Request.Context(), id)
	if err == rb.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		return
	}
	if err != nil {
		h.log.Error("get runbook", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, runbook)
}

// Update replaces a runbook's content by re-parsing the Markdown body.
func (h *RunbookHandler) Update(c *gin.Context) {
	id := c.Param("id")

	existing, err := h.repo.FindByID(c.Request.Context(), id)
	if err == rb.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		return
	}
	if err != nil {
		h.log.Error("find runbook", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	parsed := rb.Parse(string(body))
	parsed.ID = existing.ID
	parsed.Enabled = existing.Enabled

	if parsed.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "runbook must have a # Title"})
		return
	}

	saved, err := h.repo.Save(c.Request.Context(), parsed)
	if err != nil {
		h.log.Error("update runbook", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, saved)
}

// Delete disables a runbook by ID.
func (h *RunbookHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		h.log.Error("delete runbook", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
