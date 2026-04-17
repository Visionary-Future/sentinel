package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sentinelai/sentinel/internal/api/handler"
	"github.com/sentinelai/sentinel/internal/api/middleware"
	"github.com/sentinelai/sentinel/internal/config"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	http   *http.Server
	log    *slog.Logger
}

// New creates a configured Gin-based HTTP server.
func New(cfg config.ServerConfig, deps *handler.Deps, log *slog.Logger) *Server {
	if cfg.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recovery(log))

	registerRoutes(r, deps)

	return &Server{
		http: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Port),
			Handler:      r,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		log: log,
	}
}

func registerRoutes(r *gin.Engine, deps *handler.Deps) {
	r.GET("/healthz", handler.Health)

	v1 := r.Group("/api/v1")
	{
		v1.POST("/alerts/webhook", deps.Alert.Webhook)
		v1.POST("/alerts/alertmanager", deps.Alert.Alertmanager)
		v1.POST("/alerts/grafana", deps.Alert.GrafanaWebhook)
		v1.GET("/alerts", deps.Alert.List)
		v1.GET("/alerts/:id", deps.Alert.Get)

		v1.POST("/runbooks", deps.Runbook.Create)
		v1.GET("/runbooks", deps.Runbook.List)
		v1.GET("/runbooks/:id", deps.Runbook.Get)
		v1.DELETE("/runbooks/:id", deps.Runbook.Delete)

		v1.GET("/investigations", deps.Investigation.List)
		v1.GET("/investigations/:id", deps.Investigation.Get)
	}
}

// Start begins listening. It returns when the server shuts down.
func (s *Server) Start() error {
	s.log.Info("starting HTTP server", "addr", s.http.Addr)
	if err := s.http.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully drains connections with a timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down HTTP server")
	return s.http.Shutdown(ctx)
}
