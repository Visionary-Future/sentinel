package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sentinelai/sentinel/internal/investigation/tool"
)

// mockActionStore captures SaveProposal calls for assertion.
type mockActionStore struct {
	savedInvID   string
	savedProposal tool.ActionProposal
	saveErr       error
	callCount     int
}

func (m *mockActionStore) SaveProposal(_ context.Context, invID string, proposal tool.ActionProposal) error {
	m.callCount++
	m.savedInvID = invID
	m.savedProposal = proposal
	return m.saveErr
}

func TestProposeAction_SavesProposalCorrectly(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-123")

	input, _ := json.Marshal(map[string]any{
		"action_type": "restart_service",
		"command":     "kubectl rollout restart deployment/order-service",
		"reason":      "Service is stuck in CrashLoopBackOff",
		"risk_level":  "medium",
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.callCount != 1 {
		t.Errorf("expected SaveProposal called once, got %d", store.callCount)
	}
	if store.savedInvID != "inv-123" {
		t.Errorf("expected invID=inv-123, got=%s", store.savedInvID)
	}

	p := store.savedProposal
	if p.ActionType != "restart_service" {
		t.Errorf("expected action_type=restart_service, got=%s", p.ActionType)
	}
	if p.Command != "kubectl rollout restart deployment/order-service" {
		t.Errorf("unexpected command: %s", p.Command)
	}
	if p.Reason != "Service is stuck in CrashLoopBackOff" {
		t.Errorf("unexpected reason: %s", p.Reason)
	}
	if p.RiskLevel != "medium" {
		t.Errorf("expected risk_level=medium, got=%s", p.RiskLevel)
	}
	if p.Status != "proposed" {
		t.Errorf("expected status=proposed, got=%s", p.Status)
	}
	if p.ProposedAt.IsZero() {
		t.Error("expected ProposedAt to be set")
	}

	if !strings.Contains(result, "kubectl rollout restart deployment/order-service") {
		t.Errorf("expected result to contain command, got: %s", result)
	}
	if !strings.Contains(result, "Awaiting human approval") {
		t.Errorf("expected result to contain approval message, got: %s", result)
	}
}

func TestProposeAction_HighRiskWarning(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-456")

	input, _ := json.Marshal(map[string]any{
		"action_type": "rollback",
		"command":     "kubectl rollout undo deployment/payment-service",
		"reason":      "Latest deploy introduced critical regression",
		"risk_level":  "high",
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "WARNING") {
		t.Errorf("expected high-risk warning in result, got: %s", result)
	}
	if !strings.Contains(result, "high-risk") {
		t.Errorf("expected 'high-risk' in warning message, got: %s", result)
	}
}

func TestProposeAction_LowRiskNoWarning(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-789")

	input, _ := json.Marshal(map[string]any{
		"action_type": "scale_up",
		"command":     "kubectl scale deployment/api-service --replicas=5",
		"reason":      "High traffic causing slow response times",
		"risk_level":  "low",
	})

	result, err := fn(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, "WARNING") {
		t.Errorf("expected no warning for low-risk action, got: %s", result)
	}
}

func TestProposeAction_InvalidJSON(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-000")

	_, err := fn(context.Background(), json.RawMessage(`{invalid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if store.callCount != 0 {
		t.Errorf("expected no SaveProposal calls, got %d", store.callCount)
	}
}

func TestProposeAction_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		wantErr string
	}{
		{
			name:    "missing action_type",
			payload: map[string]any{"command": "echo hi", "reason": "test", "risk_level": "low"},
			wantErr: "action_type is required",
		},
		{
			name:    "missing command",
			payload: map[string]any{"action_type": "custom", "reason": "test", "risk_level": "low"},
			wantErr: "command is required",
		},
		{
			name:    "missing reason",
			payload: map[string]any{"action_type": "custom", "command": "echo hi", "risk_level": "low"},
			wantErr: "reason is required",
		},
		{
			name:    "missing risk_level",
			payload: map[string]any{"action_type": "custom", "command": "echo hi", "reason": "test"},
			wantErr: "risk_level is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockActionStore{}
			fn := tool.ProposeAction(store, "inv-err")
			input, _ := json.Marshal(tc.payload)

			_, err := fn(context.Background(), input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErr, err)
			}
			if store.callCount != 0 {
				t.Errorf("expected no SaveProposal calls, got %d", store.callCount)
			}
		})
	}
}

func TestProposeAction_InvalidActionType(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-bad")

	input, _ := json.Marshal(map[string]any{
		"action_type": "delete_database",
		"command":     "drop table users",
		"reason":      "test",
		"risk_level":  "low",
	})

	_, err := fn(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for invalid action_type, got nil")
	}
	if !strings.Contains(err.Error(), "invalid action_type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestProposeAction_InvalidRiskLevel(t *testing.T) {
	store := &mockActionStore{}
	fn := tool.ProposeAction(store, "inv-bad2")

	input, _ := json.Marshal(map[string]any{
		"action_type": "custom",
		"command":     "echo hi",
		"reason":      "test",
		"risk_level":  "extreme",
	})

	_, err := fn(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for invalid risk_level, got nil")
	}
	if !strings.Contains(err.Error(), "invalid risk_level") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestProposeAction_StoreError(t *testing.T) {
	store := &mockActionStore{saveErr: errStoreUnavailable}
	fn := tool.ProposeAction(store, "inv-err2")

	input, _ := json.Marshal(map[string]any{
		"action_type": "restart_service",
		"command":     "systemctl restart nginx",
		"reason":      "nginx not responding",
		"risk_level":  "low",
	})

	_, err := fn(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when store fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to save action proposal") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// sentinel error for store failure tests.
type storeError string

func (e storeError) Error() string { return string(e) }

const errStoreUnavailable storeError = "store unavailable"
