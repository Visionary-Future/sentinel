package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health returns a simple liveness probe response.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
