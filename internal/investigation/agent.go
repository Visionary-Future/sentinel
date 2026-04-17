package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/runbook"
)

const maxIterations = 20

// Agent runs the AI investigation loop for a single alert.
type Agent struct {
	provider llm.Provider
	tools    *tool.Registry
	log      *slog.Logger
}

func NewAgent(provider llm.Provider, tools *tool.Registry, log *slog.Logger) *Agent {
	return &Agent{provider: provider, tools: tools, log: log}
}

// Run executes the investigation and returns the updated Investigation record.
func (a *Agent) Run(ctx context.Context, inv *Investigation, evt *alert.Event, rb *runbook.Runbook) (*Investigation, error) {
	system := buildSystemPrompt(evt, rb)
	messages := []llm.Message{{
		Role:    llm.RoleUser,
		Content: buildInitialPrompt(evt, rb),
	}}

	var steps []Step
	totalTokens := 0

	for i := 0; i < maxIterations; i++ {
		a.log.Info("agent iteration", "investigation_id", inv.ID, "iteration", i+1)

		resp, err := a.provider.Chat(ctx, system, messages, a.tools.Tools())
		if err != nil {
			return nil, fmt.Errorf("llm call iteration %d: %w", i+1, err)
		}
		totalTokens += resp.TokensUsed

		// Append the assistant's response to the conversation
		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// If no tool calls, the LLM is done
		if resp.StopReason == llm.StopReasonEndTurn || len(resp.ToolCalls) == 0 {
			inv.Summary = resp.Content
			inv.RootCause, inv.Resolution = extractRootCauseAndResolution(resp.Content)
			break
		}

		// Execute each tool call and record results
		step := Step{
			Index:       len(steps) + 1,
			Description: describeToolCalls(resp.ToolCalls),
			StartedAt:   time.Now().UTC(),
		}

		for _, tc := range resp.ToolCalls {
			callResult, toolErr := a.tools.Execute(ctx, tc.Name, tc.Input)

			var inputMap map[string]any
			_ = json.Unmarshal(tc.Input, &inputMap)

			recorded := ToolCall{
				ID:     tc.ID,
				Name:   tc.Name,
				Input:  inputMap,
				Result: callResult,
			}
			if toolErr != nil {
				recorded.Error = toolErr.Error()
				callResult = "Error: " + toolErr.Error()
			}
			step.ToolCalls = append(step.ToolCalls, recorded)

			// Feed each result back to the LLM
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Content:    callResult,
			})
		}

		step.CompletedAt = time.Now().UTC()
		steps = append(steps, step)
	}

	inv.Steps = steps
	inv.TokenUsage = totalTokens
	inv.LLMProvider = a.provider.Name()
	inv.LLMModel = a.provider.Model()

	return inv, nil
}

// buildSystemPrompt creates the system instruction for the LLM.
func buildSystemPrompt(evt *alert.Event, rb *runbook.Runbook) string {
	var b strings.Builder
	b.WriteString("You are an expert Site Reliability Engineer (SRE) conducting an automated alert investigation.\n\n")
	b.WriteString("Your job is to:\n")
	b.WriteString("1. Follow the runbook steps to investigate the alert systematically\n")
	b.WriteString("2. Use the available tools to gather real data\n")
	b.WriteString("3. Analyse each result before moving to the next step\n")
	b.WriteString("4. Conclude with a clear root cause and recommended resolution\n\n")
	b.WriteString("Be concise and data-driven. When you have enough information, stop calling tools and provide your final analysis.\n")
	b.WriteString("Structure your final response as:\n")
	b.WriteString("## Root Cause\n[explanation]\n\n## Resolution\n[recommended actions]\n\n## Summary\n[brief summary]")
	return b.String()
}

// buildInitialPrompt constructs the user-facing investigation prompt.
func buildInitialPrompt(evt *alert.Event, rb *runbook.Runbook) string {
	var b strings.Builder

	b.WriteString("## Alert\n")
	b.WriteString(fmt.Sprintf("- **Title**: %s\n", evt.Title))
	b.WriteString(fmt.Sprintf("- **Severity**: %s\n", evt.Severity))
	b.WriteString(fmt.Sprintf("- **Service**: %s\n", evt.Service))
	b.WriteString(fmt.Sprintf("- **Source**: %s\n", evt.Source))
	if evt.Description != "" {
		b.WriteString(fmt.Sprintf("- **Description**: %s\n", evt.Description))
	}
	for k, v := range evt.Labels {
		b.WriteString(fmt.Sprintf("- **%s**: %s\n", k, v))
	}

	if rb != nil && len(rb.Steps) > 0 {
		b.WriteString("\n## Runbook: ")
		b.WriteString(rb.Name)
		b.WriteString("\nFollow these investigation steps in order:\n")
		for i, step := range rb.Steps {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
	} else {
		b.WriteString("\n## Instructions\n")
		b.WriteString("No runbook matched. Perform a general investigation:\n")
		b.WriteString("1. Check recent logs for errors\n")
		b.WriteString("2. Check key metrics (latency, error rate, throughput)\n")
		b.WriteString("3. Search for similar historical alerts\n")
		b.WriteString("4. Identify root cause and recommend resolution\n")
	}

	b.WriteString("\nBegin the investigation now.")
	return b.String()
}

// describeToolCalls produces a human-readable step description from tool calls.
func describeToolCalls(tcs []llm.ToolCall) string {
	names := make([]string, len(tcs))
	for i, tc := range tcs {
		names[i] = tc.Name
	}
	return strings.Join(names, ", ")
}

// extractRootCauseAndResolution parses the structured final report from the LLM.
func extractRootCauseAndResolution(content string) (rootCause, resolution string) {
	sections := map[string]*string{
		"## root cause": &rootCause,
		"## resolution": &resolution,
	}

	lower := strings.ToLower(content)
	lines := strings.Split(content, "\n")

	var current *string
	var buf strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if ptr, ok := sections[strings.ToLower(trimmed)]; ok {
			if current != nil {
				*current = strings.TrimSpace(buf.String())
				buf.Reset()
			}
			current = ptr
			continue
		}
		if current != nil && strings.HasPrefix(trimmed, "## ") {
			*current = strings.TrimSpace(buf.String())
			buf.Reset()
			current = nil
		}
		if current != nil {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	if current != nil {
		*current = strings.TrimSpace(buf.String())
	}

	_ = lower // used implicitly via sections map key matching
	return rootCause, resolution
}

// newStepID returns a short unique ID for a tool call.
func newStepID() string {
	return uuid.New().String()[:8]
}
