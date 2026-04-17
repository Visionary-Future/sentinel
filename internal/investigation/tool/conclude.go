package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sentinelai/sentinel/internal/llm"
)

// ConcludeToolName is the name of the terminal tool that signals
// the investigation is complete.
const ConcludeToolName = "conclude_investigation"

// ConclusionInput is the structured input the LLM provides when
// calling conclude_investigation.
type ConclusionInput struct {
	RootCause  string `json:"root_cause"`
	Resolution string `json:"resolution"`
	Summary    string `json:"summary"`
	Confidence int    `json:"confidence"` // 0-100
}

// ConcludeTool is the tool definition for conclude_investigation.
var ConcludeTool = llm.Tool{
	Name:        ConcludeToolName,
	Description: "Call this tool when you have completed your investigation and are ready to deliver your final conclusion. Provide the root cause, resolution, summary, and confidence level.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"root_cause": {
				"type": "string",
				"description": "The identified root cause of the alert"
			},
			"resolution": {
				"type": "string",
				"description": "Recommended actions to resolve the issue"
			},
			"summary": {
				"type": "string",
				"description": "Brief summary of the investigation findings"
			},
			"confidence": {
				"type": "integer",
				"minimum": 0,
				"maximum": 100,
				"description": "How confident you are in the root cause (0-100)"
			}
		},
		"required": ["root_cause", "resolution", "summary", "confidence"]
	}`),
}

// ConcludeInvestigation returns the handler for the conclude_investigation tool.
// It simply echoes back a confirmation — the real extraction happens in agent.go.
func ConcludeInvestigation() Func {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var c ConclusionInput
		if err := json.Unmarshal(input, &c); err != nil {
			return "", fmt.Errorf("parse conclusion: %w", err)
		}
		return fmt.Sprintf("Investigation concluded. Root cause: %s", c.RootCause), nil
	}
}
