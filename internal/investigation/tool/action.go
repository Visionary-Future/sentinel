package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentinelai/sentinel/internal/llm"
)

const ProposeActionToolName = "propose_action"

// ProposeActionTool is the LLM tool definition for proposing a remediation action.
var ProposeActionTool = llm.Tool{
	Name:        ProposeActionToolName,
	Description: "Propose a remediation action for human review and approval. The action will not be executed until a human approves it.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"action_type": {
				"type": "string",
				"enum": ["restart_service", "scale_up", "rollback", "custom"],
				"description": "The type of remediation action to perform"
			},
			"command": {
				"type": "string",
				"description": "The exact command or operation to execute upon approval"
			},
			"reason": {
				"type": "string",
				"description": "Explanation of why this action is being proposed and how it addresses the root cause"
			},
			"risk_level": {
				"type": "string",
				"enum": ["low", "medium", "high"],
				"description": "The risk level of the proposed action"
			}
		},
		"required": ["action_type", "command", "reason", "risk_level"]
	}`),
}

// ActionProposal holds a single proposed remediation action and its lifecycle state.
type ActionProposal struct {
	ActionType string    `json:"action_type"`
	Command    string    `json:"command"`
	Reason     string    `json:"reason"`
	RiskLevel  string    `json:"risk_level"`
	Status     string    `json:"status"` // proposed, approved, rejected, executed
	ProposedAt time.Time `json:"proposed_at"`
}

// ActionStore persists action proposals for a given investigation.
type ActionStore interface {
	SaveProposal(ctx context.Context, invID string, proposal ActionProposal) error
}

// proposeActionInput is the raw JSON payload sent by the LLM.
type proposeActionInput struct {
	ActionType string `json:"action_type"`
	Command    string `json:"command"`
	Reason     string `json:"reason"`
	RiskLevel  string `json:"risk_level"`
}

var validActionTypes = map[string]bool{
	"restart_service": true,
	"scale_up":        true,
	"rollback":        true,
	"custom":          true,
}

var validRiskLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

// ProposeAction returns a Func that validates input, persists the proposal, and
// returns a human-readable confirmation message (with a high-risk warning when
// risk_level is "high").
func ProposeAction(store ActionStore, invID string) Func {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var in proposeActionInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}

		if in.ActionType == "" {
			return "", fmt.Errorf("action_type is required")
		}
		if !validActionTypes[in.ActionType] {
			return "", fmt.Errorf("invalid action_type %q: must be one of restart_service, scale_up, rollback, custom", in.ActionType)
		}
		if in.Command == "" {
			return "", fmt.Errorf("command is required")
		}
		if in.Reason == "" {
			return "", fmt.Errorf("reason is required")
		}
		if in.RiskLevel == "" {
			return "", fmt.Errorf("risk_level is required")
		}
		if !validRiskLevels[in.RiskLevel] {
			return "", fmt.Errorf("invalid risk_level %q: must be one of low, medium, high", in.RiskLevel)
		}

		proposal := ActionProposal{
			ActionType: in.ActionType,
			Command:    in.Command,
			Reason:     in.Reason,
			RiskLevel:  in.RiskLevel,
			Status:     "proposed",
			ProposedAt: time.Now(),
		}

		if err := store.SaveProposal(ctx, invID, proposal); err != nil {
			return "", fmt.Errorf("failed to save action proposal: %w", err)
		}

		msg := fmt.Sprintf("Action proposed: %s. Awaiting human approval.", in.Command)
		if in.RiskLevel == "high" {
			msg += " WARNING: This is a high-risk action. Please review carefully before approving."
		}
		return msg, nil
	}
}
