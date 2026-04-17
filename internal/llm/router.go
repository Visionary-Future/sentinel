package llm

import (
	"context"
)

// SeverityRouter selects a Provider based on alert severity.
// It maps severity strings to specific providers, falling back to
// a default provider for unknown severities.
type SeverityRouter struct {
	routes   map[string]Provider // severity → provider
	fallback Provider
}

// NewSeverityRouter creates a router. routes maps severity strings
// (e.g. "critical", "warning", "info") to providers.
// fallback is used when no route matches.
func NewSeverityRouter(fallback Provider, routes map[string]Provider) *SeverityRouter {
	if routes == nil {
		routes = make(map[string]Provider)
	}
	return &SeverityRouter{routes: routes, fallback: fallback}
}

// Route returns the provider for the given severity.
func (r *SeverityRouter) Route(severity string) Provider {
	if p, ok := r.routes[severity]; ok {
		return p
	}
	return r.fallback
}

// Name returns the fallback provider name.
func (r *SeverityRouter) Name() string { return r.fallback.Name() }

// Model returns the fallback provider model.
func (r *SeverityRouter) Model() string { return r.fallback.Model() }

// Chat delegates to the fallback. For severity-based routing, callers
// should use Route() to get the right provider before calling Chat.
func (r *SeverityRouter) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	return r.fallback.Chat(ctx, system, messages, tools)
}
