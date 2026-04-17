package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// Deps holds all handler dependencies, injected at startup.
type Deps struct {
	Alert         *AlertHandler
	Runbook       *RunbookHandler
	Investigation *InvestigationHandler
}

// queryInt reads an integer query parameter with a default fallback.
func queryInt(c *gin.Context, key string, def int) int {
	s := c.Query(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}
