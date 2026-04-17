package investigation

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// testLogger returns a no-op slog.Logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestInvestigation creates a minimal Investigation for testing.
func newTestInvestigation() *Investigation {
	now := time.Now()
	return &Investigation{
		ID:          "inv-123",
		AlertID:     "alert-456",
		Status:      StatusCompleted,
		RootCause:   "database connection pool exhausted",
		Resolution:  "increase pool size",
		Summary:     "High connection wait times caused request failures",
		Confidence:  87,
		StartedAt:   &now,
		CompletedAt: &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// --- TestWebhookSend ---

func TestWebhookSend_DeliversPayload(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected Content-Type: %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		received = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewWebhookSender(srv.URL, testLogger())
	evt := BuildEvent(newTestInvestigation(), EventInvestigationCompleted)

	if err := sender.Send(context.Background(), evt); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var got WebhookEvent
	if err := json.Unmarshal(received, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got.Type != EventInvestigationCompleted {
		t.Errorf("type: got %q, want %q", got.Type, EventInvestigationCompleted)
	}
	if got.InvestigationID != "inv-123" {
		t.Errorf("investigation_id: got %q, want %q", got.InvestigationID, "inv-123")
	}
	if got.AlertID != "alert-456" {
		t.Errorf("alert_id: got %q, want %q", got.AlertID, "alert-456")
	}
	if got.Confidence != 87 {
		t.Errorf("confidence: got %d, want 87", got.Confidence)
	}
}

// --- TestWebhookSendRetry ---

func TestWebhookSend_RetriesOn5xx(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			// Fail the first two attempts.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use very short backoff delays so the test runs fast.
	origDelays := retryDelays
	retryDelays = [webhookMaxAttempts - 1]time.Duration{1 * time.Millisecond, 1 * time.Millisecond}
	defer func() { retryDelays = origDelays }()

	sender := NewWebhookSender(srv.URL, testLogger())
	evt := BuildEvent(newTestInvestigation(), EventInvestigationStarted)

	if err := sender.Send(context.Background(), evt); err != nil {
		t.Fatalf("Send returned error after retries: %v", err)
	}

	if got := callCount.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestWebhookSend_FailsAfterMaxAttempts(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	origDelays := retryDelays
	retryDelays = [webhookMaxAttempts - 1]time.Duration{1 * time.Millisecond, 1 * time.Millisecond}
	defer func() { retryDelays = origDelays }()

	sender := NewWebhookSender(srv.URL, testLogger())
	evt := BuildEvent(newTestInvestigation(), EventInvestigationFailed)

	if err := sender.Send(context.Background(), evt); err == nil {
		t.Fatal("expected error after exhausting all attempts, got nil")
	}

	if got := callCount.Load(); got != int32(webhookMaxAttempts) {
		t.Errorf("expected %d attempts, got %d", webhookMaxAttempts, got)
	}
}

// --- TestWebhookEmptyURL ---

func TestWebhookSend_EmptyURLReturnsNil(t *testing.T) {
	sender := NewWebhookSender("", testLogger())
	evt := BuildEvent(newTestInvestigation(), EventInvestigationCompleted)

	if err := sender.Send(context.Background(), evt); err != nil {
		t.Fatalf("expected nil for empty URL, got: %v", err)
	}
}

// --- TestBuildEvent ---

func TestBuildEvent_PopulatesAllFields(t *testing.T) {
	inv := newTestInvestigation()
	before := time.Now()
	evt := BuildEvent(inv, EventInvestigationCompleted)
	after := time.Now()

	if evt.Type != EventInvestigationCompleted {
		t.Errorf("Type: got %q, want %q", evt.Type, EventInvestigationCompleted)
	}
	if evt.InvestigationID != inv.ID {
		t.Errorf("InvestigationID: got %q, want %q", evt.InvestigationID, inv.ID)
	}
	if evt.AlertID != inv.AlertID {
		t.Errorf("AlertID: got %q, want %q", evt.AlertID, inv.AlertID)
	}
	if evt.Status != string(inv.Status) {
		t.Errorf("Status: got %q, want %q", evt.Status, inv.Status)
	}
	if evt.RootCause != inv.RootCause {
		t.Errorf("RootCause: got %q, want %q", evt.RootCause, inv.RootCause)
	}
	if evt.Resolution != inv.Resolution {
		t.Errorf("Resolution: got %q, want %q", evt.Resolution, inv.Resolution)
	}
	if evt.Summary != inv.Summary {
		t.Errorf("Summary: got %q, want %q", evt.Summary, inv.Summary)
	}
	if evt.Confidence != inv.Confidence {
		t.Errorf("Confidence: got %d, want %d", evt.Confidence, inv.Confidence)
	}
	if evt.Timestamp.Before(before) || evt.Timestamp.After(after) {
		t.Errorf("Timestamp %v is outside expected range [%v, %v]", evt.Timestamp, before, after)
	}
}

func TestBuildEvent_AllEventTypes(t *testing.T) {
	inv := newTestInvestigation()

	cases := []struct {
		eventType EventType
		status    Status
	}{
		{EventInvestigationStarted, StatusRunning},
		{EventInvestigationCompleted, StatusCompleted},
		{EventInvestigationFailed, StatusFailed},
	}

	for _, tc := range cases {
		inv.Status = tc.status
		evt := BuildEvent(inv, tc.eventType)
		if evt.Type != tc.eventType {
			t.Errorf("eventType %q: got %q", tc.eventType, evt.Type)
		}
		if evt.Status != string(tc.status) {
			t.Errorf("status %q: got %q", tc.status, evt.Status)
		}
	}
}

// --- TestWebhookContextCancellation ---

func TestWebhookSend_ContextCancellationStopsRetries(t *testing.T) {
	var callCount atomic.Int32

	// Server always returns 500 to force retries.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Use real backoff delays — we'll cancel the context before the second retry fires.
	origDelays := retryDelays
	retryDelays = [webhookMaxAttempts - 1]time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
	}
	defer func() { retryDelays = origDelays }()

	ctx, cancel := context.WithCancel(context.Background())

	sender := NewWebhookSender(srv.URL, testLogger())
	evt := BuildEvent(newTestInvestigation(), EventInvestigationFailed)

	// Cancel context shortly after the first attempt completes.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := sender.Send(ctx, evt)
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}

	// Only the first attempt should have gone through before cancellation.
	if got := callCount.Load(); got > 1 {
		t.Errorf("expected at most 1 server call before cancellation, got %d", got)
	}
}
