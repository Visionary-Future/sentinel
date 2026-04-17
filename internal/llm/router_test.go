package llm

import (
	"context"
	"testing"
)

// routerMockProvider is a minimal Provider used in router tests to avoid
// cross-test state from the stateful mockProvider in fallback_test.go.
type routerMockProvider struct {
	name  string
	model string
}

func (r *routerMockProvider) Name() string  { return r.name }
func (r *routerMockProvider) Model() string { return r.model }
func (r *routerMockProvider) Chat(_ context.Context, _ string, _ []Message, _ []Tool) (*Response, error) {
	return &Response{Content: r.name, StopReason: StopReasonEndTurn}, nil
}

func newRouterProvider(name, model string) *routerMockProvider {
	return &routerMockProvider{name: name, model: model}
}

func TestSeverityRouter_RouteKnownSeverity(t *testing.T) {
	tests := []struct {
		severity string
		wantName string
	}{
		{severity: "critical", wantName: "opus"},
		{severity: "warning", wantName: "sonnet"},
		{severity: "info", wantName: "haiku"},
	}

	fallback := newRouterProvider("fallback", "fallback-model")
	routes := map[string]Provider{
		"critical": newRouterProvider("opus", "opus-model"),
		"warning":  newRouterProvider("sonnet", "sonnet-model"),
		"info":     newRouterProvider("haiku", "haiku-model"),
	}
	router := NewSeverityRouter(fallback, routes)

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			p := router.Route(tt.severity)
			if p == nil {
				t.Fatal("Route() returned nil")
			}
			if p.Name() != tt.wantName {
				t.Errorf("Route(%q).Name() = %q, want %q", tt.severity, p.Name(), tt.wantName)
			}
		})
	}
}

func TestSeverityRouter_RouteUnknownSeverityReturnsFallback(t *testing.T) {
	fallback := newRouterProvider("fallback", "fallback-model")
	routes := map[string]Provider{
		"critical": newRouterProvider("opus", "opus-model"),
	}
	router := NewSeverityRouter(fallback, routes)

	unknownSeverities := []string{"unknown", "", "CRITICAL", "high", "low"}

	for _, sev := range unknownSeverities {
		t.Run(sev, func(t *testing.T) {
			p := router.Route(sev)
			if p == nil {
				t.Fatal("Route() returned nil")
			}
			if p.Name() != "fallback" {
				t.Errorf("Route(%q).Name() = %q, want %q", sev, p.Name(), "fallback")
			}
		})
	}
}

func TestSeverityRouter_NilRouteMapUseFallback(t *testing.T) {
	fallback := newRouterProvider("fallback", "fallback-model")
	router := NewSeverityRouter(fallback, nil)

	p := router.Route("critical")
	if p.Name() != "fallback" {
		t.Errorf("Route(\"critical\").Name() = %q, want %q", p.Name(), "fallback")
	}
}

func TestSeverityRouter_NameDelegatesToFallback(t *testing.T) {
	fallback := newRouterProvider("my-fallback", "fallback-model")
	router := NewSeverityRouter(fallback, nil)

	if router.Name() != "my-fallback" {
		t.Errorf("Name() = %q, want %q", router.Name(), "my-fallback")
	}
}

func TestSeverityRouter_ModelDelegatesToFallback(t *testing.T) {
	fallback := newRouterProvider("fallback", "gpt-4-turbo")
	router := NewSeverityRouter(fallback, nil)

	if router.Model() != "gpt-4-turbo" {
		t.Errorf("Model() = %q, want %q", router.Model(), "gpt-4-turbo")
	}
}

func TestSeverityRouter_ChatDelegatesToFallback(t *testing.T) {
	fallback := newRouterProvider("fallback", "fallback-model")
	routes := map[string]Provider{
		"critical": newRouterProvider("opus", "opus-model"),
	}
	router := NewSeverityRouter(fallback, routes)

	msgs := []Message{{Role: RoleUser, Content: "investigate this"}}
	resp, err := router.Chat(context.Background(), "system", msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// routerMockProvider.Chat returns the provider's name as content.
	if resp.Content != "fallback" {
		t.Errorf("Chat() content = %q, want %q", resp.Content, "fallback")
	}
}
