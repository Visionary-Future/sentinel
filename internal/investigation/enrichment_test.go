package investigation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/runbook"
)

// ---------------------------------------------------------------------------
// Test helpers / fakes
// ---------------------------------------------------------------------------

// fixedEnricher always returns a fixed string.
type fixedEnricher struct {
	text string
}

func (f *fixedEnricher) Enrich(_ context.Context, _ *alert.Event) (string, error) {
	return f.text, nil
}

// errorEnricher always returns an error.
type errorEnricher struct {
	msg string
}

func (e *errorEnricher) Enrich(_ context.Context, _ *alert.Event) (string, error) {
	return "", errors.New(e.msg)
}

// fakeDeploymentSource returns a configurable list or error.
type fakeDeploymentSource struct {
	deployments []Deployment
	err         error
}

func (f *fakeDeploymentSource) RecentDeployments(_ context.Context, _ string, _ time.Duration) ([]Deployment, error) {
	return f.deployments, f.err
}

// fakeServiceCatalog returns a configurable ServiceInfo or error.
type fakeServiceCatalog struct {
	info *ServiceInfo
	err  error
}

func (f *fakeServiceCatalog) GetService(_ context.Context, _ string) (*ServiceInfo, error) {
	return f.info, f.err
}

// makeEnrichEvent creates a minimal alert.Event for use in tests.
func makeEnrichEvent(service string) *alert.Event {
	return &alert.Event{
		ID:       "evt-1",
		Title:    "High error rate",
		Severity: alert.SeverityCritical,
		Service:  service,
		Source:   alert.SourceWebhook,
	}
}

// ---------------------------------------------------------------------------
// EnrichmentPipeline tests
// ---------------------------------------------------------------------------

func TestEnrichPipelineRunsAllEnrichersAndConcatenates(t *testing.T) {
	t.Parallel()

	pipe := NewEnrichmentPipeline(
		&fixedEnricher{text: "## Section A\n- item 1"},
		&fixedEnricher{text: "## Section B\n- item 2"},
	)

	result := pipe.Enrich(context.Background(), makeEnrichEvent("svc-a"))

	if !strings.Contains(result, "## Section A") {
		t.Errorf("expected Section A in result, got: %q", result)
	}
	if !strings.Contains(result, "## Section B") {
		t.Errorf("expected Section B in result, got: %q", result)
	}
	// Two sections should be separated by a blank line.
	if !strings.Contains(result, "\n\n") {
		t.Errorf("expected double newline separator between sections, got: %q", result)
	}
}

func TestEnrichPipelineSkipsErroringEnricherGracefully(t *testing.T) {
	t.Parallel()

	pipe := NewEnrichmentPipeline(
		&fixedEnricher{text: "## Good Section\n- data"},
		&errorEnricher{msg: "network timeout"},
		&fixedEnricher{text: "## Another Good Section\n- more data"},
	)

	result := pipe.Enrich(context.Background(), makeEnrichEvent("svc-b"))

	if !strings.Contains(result, "## Good Section") {
		t.Errorf("expected Good Section in result, got: %q", result)
	}
	if !strings.Contains(result, "## Another Good Section") {
		t.Errorf("expected Another Good Section in result, got: %q", result)
	}
	if strings.Contains(result, "network timeout") {
		t.Errorf("error message should not appear in result, got: %q", result)
	}
}

func TestEnrichPipelineEmptyPipelineReturnsEmptyString(t *testing.T) {
	t.Parallel()

	pipe := NewEnrichmentPipeline()
	result := pipe.Enrich(context.Background(), makeEnrichEvent("svc-c"))

	if result != "" {
		t.Errorf("expected empty string from empty pipeline, got: %q", result)
	}
}

func TestEnrichPipelineAllEnrichersErrorReturnsEmptyString(t *testing.T) {
	t.Parallel()

	pipe := NewEnrichmentPipeline(
		&errorEnricher{msg: "err 1"},
		&errorEnricher{msg: "err 2"},
	)

	result := pipe.Enrich(context.Background(), makeEnrichEvent("svc-d"))

	if result != "" {
		t.Errorf("expected empty string when all enrichers fail, got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// BuildEnrichedPrompt tests
// ---------------------------------------------------------------------------

func TestBuildEnrichedPromptIncludesEnrichmentText(t *testing.T) {
	t.Parallel()

	evt := makeEnrichEvent("payment-service")
	enrichment := "## Recent Deployments\n- v1.2.3 by alice (success)"

	result := BuildEnrichedPrompt(evt, nil, enrichment)

	if !strings.Contains(result, "## Alert") {
		t.Errorf("expected base alert section in prompt, got: %q", result)
	}
	if !strings.Contains(result, "## Enrichment Context") {
		t.Errorf("expected Enrichment Context header in prompt, got: %q", result)
	}
	if !strings.Contains(result, enrichment) {
		t.Errorf("expected enrichment body in prompt, got: %q", result)
	}
}

func TestBuildEnrichedPromptWithEmptyEnrichmentOmitsSection(t *testing.T) {
	t.Parallel()

	evt := makeEnrichEvent("auth-service")
	result := BuildEnrichedPrompt(evt, nil, "")

	if strings.Contains(result, "## Enrichment Context") {
		t.Errorf("expected no Enrichment Context section when enrichment is empty, got: %q", result)
	}
}

func TestBuildEnrichedPromptWithRunbookIncludesRunbookSteps(t *testing.T) {
	t.Parallel()

	evt := makeEnrichEvent("order-service")
	rb := &runbook.Runbook{
		Name:  "Order Service Playbook",
		Steps: []string{"Check error logs", "Check DB connections"},
	}
	enrichment := "## Service Info\n- Team: platform"

	result := BuildEnrichedPrompt(evt, rb, enrichment)

	if !strings.Contains(result, "Order Service Playbook") {
		t.Errorf("expected runbook name in prompt, got: %q", result)
	}
	if !strings.Contains(result, "Check error logs") {
		t.Errorf("expected runbook step in prompt, got: %q", result)
	}
	if !strings.Contains(result, "## Enrichment Context") {
		t.Errorf("expected enrichment context in prompt, got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// RecentDeploymentsEnricher tests
// ---------------------------------------------------------------------------

func TestRecentDeploymentsEnricherWithStubReturnsNoDeployments(t *testing.T) {
	t.Parallel()

	enricher := NewRecentDeploymentsEnricher(&StubDeploymentSource{}, 2*time.Hour)
	text, err := enricher.Enrich(context.Background(), makeEnrichEvent("checkout"))

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(text, "## Recent Deployments") {
		t.Errorf("expected Recent Deployments header, got: %q", text)
	}
	if !strings.Contains(text, "No recent deployments found") {
		t.Errorf("expected empty-state message, got: %q", text)
	}
}

func TestRecentDeploymentsEnricherWithRealDeployments(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	src := &fakeDeploymentSource{
		deployments: []Deployment{
			{Version: "v2.1.0", Author: "bob", DeployedAt: ts, Status: "success"},
			{Version: "v2.0.9", Author: "alice", DeployedAt: ts.Add(-30 * time.Minute), Status: "success"},
		},
	}

	enricher := NewRecentDeploymentsEnricher(src, 2*time.Hour)
	text, err := enricher.Enrich(context.Background(), makeEnrichEvent("inventory"))

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(text, "v2.1.0") {
		t.Errorf("expected version v2.1.0 in output, got: %q", text)
	}
	if !strings.Contains(text, "bob") {
		t.Errorf("expected author bob in output, got: %q", text)
	}
	if !strings.Contains(text, "v2.0.9") {
		t.Errorf("expected version v2.0.9 in output, got: %q", text)
	}
}

func TestRecentDeploymentsEnricherPropagatesSourceError(t *testing.T) {
	t.Parallel()

	src := &fakeDeploymentSource{err: errors.New("source unavailable")}
	enricher := NewRecentDeploymentsEnricher(src, time.Hour)
	_, err := enricher.Enrich(context.Background(), makeEnrichEvent("shipping"))

	if err == nil {
		t.Fatal("expected error from enricher, got nil")
	}
	if !strings.Contains(err.Error(), "RecentDeploymentsEnricher") {
		t.Errorf("expected enricher name in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ServiceInfoEnricher tests
// ---------------------------------------------------------------------------

func TestServiceInfoEnricherWithStubReturnsUnknownFields(t *testing.T) {
	t.Parallel()

	enricher := NewServiceInfoEnricher(&StubServiceCatalog{})
	text, err := enricher.Enrich(context.Background(), makeEnrichEvent("search"))

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(text, "## Service Info") {
		t.Errorf("expected Service Info header, got: %q", text)
	}
	if !strings.Contains(text, "Team: unknown") {
		t.Errorf("expected Team: unknown for empty stub, got: %q", text)
	}
	if !strings.Contains(text, "On-call: unknown") {
		t.Errorf("expected On-call: unknown for empty stub, got: %q", text)
	}
}

func TestServiceInfoEnricherWithPopulatedCatalog(t *testing.T) {
	t.Parallel()

	catalog := &fakeServiceCatalog{
		info: &ServiceInfo{
			Team:         "platform",
			OnCall:       "alice@example.com",
			Dependencies: []string{"postgres", "redis"},
		},
	}

	enricher := NewServiceInfoEnricher(catalog)
	text, err := enricher.Enrich(context.Background(), makeEnrichEvent("api-gateway"))

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(text, "Team: platform") {
		t.Errorf("expected Team: platform, got: %q", text)
	}
	if !strings.Contains(text, "On-call: alice@example.com") {
		t.Errorf("expected on-call address, got: %q", text)
	}
	if !strings.Contains(text, "postgres") {
		t.Errorf("expected dependency postgres, got: %q", text)
	}
	if !strings.Contains(text, "redis") {
		t.Errorf("expected dependency redis, got: %q", text)
	}
}

func TestServiceInfoEnricherPropagatesCatalogError(t *testing.T) {
	t.Parallel()

	catalog := &fakeServiceCatalog{err: errors.New("catalog offline")}
	enricher := NewServiceInfoEnricher(catalog)
	_, err := enricher.Enrich(context.Background(), makeEnrichEvent("reporting"))

	if err == nil {
		t.Fatal("expected error from enricher, got nil")
	}
	if !strings.Contains(err.Error(), "ServiceInfoEnricher") {
		t.Errorf("expected enricher name in error, got: %v", err)
	}
}
