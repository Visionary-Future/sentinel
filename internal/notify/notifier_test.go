package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/config"
	"github.com/sentinelai/sentinel/internal/notify"
)

func testPayload() *notify.Payload {
	return &notify.Payload{
		Alert: &alert.Event{
			ID:       "alert-1",
			Title:    "order-service P99 latency > 2s",
			Severity: alert.SeverityCritical,
			Service:  "order-service",
		},
		Investigation: &notify.InvestigationReport{
			ID:          "inv-1",
			Status:      "completed",
			RootCause:   "Database connection pool exhausted due to missing index on orders.status",
			Resolution:  "Added index. Pool connections dropped from 95% to 20%.",
			Summary:     "Root cause identified and resolved.",
			LLMProvider: "claude",
			LLMModel:    "claude-sonnet-4-6",
			TokenUsage:  1234,
			StepCount:   5,
			CompletedAt: time.Now(),
		},
	}
}

func TestMultiChannel_Send_CallsAllChannels(t *testing.T) {
	called := make([]string, 0)

	ch1 := &mockChannel{name: "ch1", onSend: func() { called = append(called, "ch1") }}
	ch2 := &mockChannel{name: "ch2", onSend: func() { called = append(called, "ch2") }}

	mc := notify.NewMultiChannel(ch1, ch2)
	mc.Send(context.Background(), testPayload())

	if len(called) != 2 {
		t.Fatalf("expected 2 channels called, got %d", len(called))
	}
}

func TestWeComChannel_Send_PostsMarkdown(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := notify.NewWeCom(config.WeComNotifyConfig{
		Enabled:    true,
		WebhookURL: srv.URL,
	})

	if err := ch.Send(context.Background(), testPayload()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if received["msgtype"] != "markdown" {
		t.Errorf("expected msgtype=markdown, got %v", received["msgtype"])
	}

	md := received["markdown"].(map[string]any)["content"].(string)
	if !strings.Contains(md, "order-service P99 latency") {
		t.Errorf("markdown missing alert title: %s", md)
	}
	if !strings.Contains(md, "Database connection pool") {
		t.Errorf("markdown missing root cause: %s", md)
	}
}

func TestDingTalkChannel_Send_PostsMarkdown(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := notify.NewDingTalk(config.DingTalkNotifyConfig{
		Enabled:    true,
		WebhookURL: srv.URL,
		Secret:     "", // no signing for test
	})

	if err := ch.Send(context.Background(), testPayload()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if received["msgtype"] != "markdown" {
		t.Errorf("expected msgtype=markdown, got %v", received["msgtype"])
	}

	md := received["markdown"].(map[string]any)
	if !strings.Contains(md["title"].(string), "CRITICAL") {
		t.Errorf("title missing severity: %v", md["title"])
	}
	if !strings.Contains(md["text"].(string), "Database connection pool") {
		t.Errorf("text missing root cause: %v", md["text"])
	}
}

func TestDingTalkChannel_Send_WithSigning(t *testing.T) {
	var queryString string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryString = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := notify.NewDingTalk(config.DingTalkNotifyConfig{
		Enabled:    true,
		WebhookURL: srv.URL,
		Secret:     "test-secret",
	})

	if err := ch.Send(context.Background(), testPayload()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.Contains(queryString, "timestamp=") {
		t.Errorf("expected timestamp in query, got: %s", queryString)
	}
	if !strings.Contains(queryString, "sign=") {
		t.Errorf("expected sign in query, got: %s", queryString)
	}
}

// mockChannel is a test double for notify.Channel.
type mockChannel struct {
	name   string
	onSend func()
}

func (m *mockChannel) Name() string { return m.name }
func (m *mockChannel) Send(_ context.Context, _ *notify.Payload) error {
	if m.onSend != nil {
		m.onSend()
	}
	return nil
}
