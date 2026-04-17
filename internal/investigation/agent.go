package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/runbook"
)

const (
	maxIterations      = 20
	defaultTokenBudget = 100_000
	maxToolResultLen   = 4000
	toolCallTimeout    = 30 * time.Second
	// Estimated chars-per-token ratio for context window management.
	// Conservative: 1 token ≈ 3 chars for English/Chinese mix.
	charsPerToken = 3
	// Start pruning when estimated token usage exceeds this fraction of budget.
	pruneThreshold = 0.7
)

// StepCallback is invoked after each investigation step completes.
// It receives the investigation ID and all steps accumulated so far.
type StepCallback func(invID string, steps []Step)

// Agent runs the AI investigation loop for a single alert.
type Agent struct {
	provider    llm.Provider
	tools       *tool.Registry
	tokenBudget int
	log         *slog.Logger
}

func NewAgent(provider llm.Provider, tools *tool.Registry, log *slog.Logger) *Agent {
	return &Agent{
		provider:    provider,
		tools:       tools,
		tokenBudget: defaultTokenBudget,
		log:         log,
	}
}

// Run executes the investigation and returns a new Investigation with results.
// The original inv is not mutated. onStep is called after each tool-execution
// step completes (may be nil).
func (a *Agent) Run(ctx context.Context, inv *Investigation, evt *alert.Event, rb *runbook.Runbook, onStep StepCallback) (*Investigation, error) {
	system := buildSystemPrompt(evt, rb)
	messages := []llm.Message{{
		Role:    llm.RoleUser,
		Content: buildInitialPrompt(evt, rb),
	}}

	// If resuming a failed investigation, reconstruct conversation from saved steps
	var steps []Step
	if len(inv.Steps) > 0 {
		steps = inv.Steps
		messages = append(messages, rebuildMessagesFromSteps(inv.Steps)...)
		a.log.Info("resuming investigation from saved steps",
			"investigation_id", inv.ID, "existing_steps", len(inv.Steps))
	}

	totalTokens := 0
	var summary, rootCause, resolution string
	confidence := 0

	for i := 0; i < maxIterations; i++ {
		a.log.Info("agent iteration", "investigation_id", inv.ID, "iteration", i+1)

		// Token budget check: force conclusion if exceeded
		if totalTokens > a.tokenBudget {
			a.log.Warn("token budget exceeded, forcing conclusion",
				"investigation_id", inv.ID,
				"tokens_used", totalTokens,
				"budget", a.tokenBudget,
			)
			messages = append(messages, llm.Message{
				Role:    llm.RoleUser,
				Content: "Token budget exceeded. Stop calling tools and provide your final analysis based on data collected so far.",
			})
			resp, err := a.provider.Chat(ctx, system, messages, nil)
			if err != nil {
				break
			}
			totalTokens += resp.TokensUsed
			summary = resp.Content
			rootCause, resolution = extractRootCauseAndResolution(resp.Content)
			confidence = extractConfidence(resp.Content)
			break
		}

		// Prune conversation history if approaching context window limits
		messages = pruneMessages(messages, a.tokenBudget)

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

		// If no tool calls, the LLM is done — fall back to markdown parsing
		if resp.StopReason == llm.StopReasonEndTurn || len(resp.ToolCalls) == 0 {
			summary = resp.Content
			rootCause, resolution = extractRootCauseAndResolution(resp.Content)
			confidence = extractConfidence(resp.Content)
			break
		}

		// Execute tool calls in parallel
		step := Step{
			Index:       len(steps) + 1,
			Description: describeToolCalls(resp.ToolCalls),
			StartedAt:   time.Now().UTC(),
		}

		toolResults := a.executeToolsParallel(ctx, resp.ToolCalls)

		step.ToolCalls = make([]ToolCall, len(resp.ToolCalls))
		for idx, tr := range toolResults {
			step.ToolCalls[idx] = tr.recorded
			messages = append(messages, tr.message)
		}

		step.CompletedAt = time.Now().UTC()
		steps = append(steps, step)

		// Check if the LLM called conclude_investigation (structured output)
		if conclusion := extractConclusion(resp.ToolCalls); conclusion != nil {
			rootCause = conclusion.RootCause
			resolution = conclusion.Resolution
			summary = conclusion.Summary
			confidence = conclusion.Confidence
			break
		}

		// Notify caller of intermediate progress
		if onStep != nil {
			onStep(inv.ID, steps)
		}
	}

	result := *inv
	result.Steps = steps
	result.TokenUsage = totalTokens
	result.LLMProvider = a.provider.Name()
	result.LLMModel = a.provider.Model()
	result.Summary = summary
	result.RootCause = rootCause
	result.Resolution = resolution
	result.Confidence = confidence

	return &result, nil
}

// toolExecResult holds the output of a single parallel tool execution.
type toolExecResult struct {
	recorded ToolCall
	message  llm.Message
}

// executeToolsParallel runs all tool calls concurrently, each with its own
// timeout. Results are returned in the same order as the input tool calls.
func (a *Agent) executeToolsParallel(ctx context.Context, tcs []llm.ToolCall) []toolExecResult {
	results := make([]toolExecResult, len(tcs))

	if len(tcs) == 1 {
		// Fast path: single tool call, no goroutine overhead
		results[0] = a.executeSingleTool(ctx, tcs[0])
		return results
	}

	var wg sync.WaitGroup
	for idx, tc := range tcs {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			results[i] = a.executeSingleTool(ctx, tc)
		}(idx, tc)
	}
	wg.Wait()
	return results
}

// executeSingleTool executes one tool call with a per-tool timeout.
func (a *Agent) executeSingleTool(ctx context.Context, tc llm.ToolCall) toolExecResult {
	toolCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	defer cancel()

	callResult, toolErr := a.tools.Execute(toolCtx, tc.Name, tc.Input)
	callResult = truncateResult(callResult, maxToolResultLen)

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

	return toolExecResult{
		recorded: recorded,
		message: llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Content:    callResult,
		},
	}
}

// truncateResult caps a tool result string, appending a truncation notice.
func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n\n... [truncated, showing first " + fmt.Sprintf("%d", maxLen) + " chars]"
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
	b.WriteString("Be concise and data-driven. When you have enough information, call the `conclude_investigation` tool with your findings.\n")
	b.WriteString("The conclude_investigation tool accepts: root_cause, resolution, summary, and confidence (0-100).\n")
	b.WriteString("Always use conclude_investigation to deliver your final analysis — do not write a free-text conclusion.")
	return b.String()
}

// buildInitialPrompt constructs the user-facing investigation prompt.
func buildInitialPrompt(evt *alert.Event, rb *runbook.Runbook) string {
	var b strings.Builder

	b.WriteString("## Alert\n")
	fmt.Fprintf(&b, "- **Title**: %s\n", evt.Title)
	fmt.Fprintf(&b, "- **Severity**: %s\n", evt.Severity)
	fmt.Fprintf(&b, "- **Service**: %s\n", evt.Service)
	fmt.Fprintf(&b, "- **Source**: %s\n", evt.Source)
	if evt.Description != "" {
		fmt.Fprintf(&b, "- **Description**: %s\n", evt.Description)
	}
	for k, v := range evt.Labels {
		fmt.Fprintf(&b, "- **%s**: %s\n", k, v)
	}

	if rb != nil && len(rb.Steps) > 0 {
		b.WriteString("\n## Runbook: ")
		b.WriteString(rb.Name)
		b.WriteString("\nFollow these investigation steps in order:\n")
		for i, step := range rb.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, step)
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

// rebuildMessagesFromSteps reconstructs LLM conversation history from saved
// investigation steps. This enables resuming a failed investigation.
func rebuildMessagesFromSteps(steps []Step) []llm.Message {
	var messages []llm.Message
	for _, step := range steps {
		// Reconstruct assistant message with tool calls
		tcs := make([]llm.ToolCall, len(step.ToolCalls))
		for i, tc := range step.ToolCalls {
			inputJSON, _ := json.Marshal(tc.Input)
			tcs[i] = llm.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: inputJSON,
			}
		}
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   step.Description,
			ToolCalls: tcs,
		})

		// Reconstruct tool result messages
		for _, tc := range step.ToolCalls {
			content := tc.Result
			if tc.Error != "" {
				content = "Error: " + tc.Error
			}
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Content:    content,
			})
		}

		// Add analysis as assistant message if present
		if step.Analysis != "" {
			messages = append(messages, llm.Message{
				Role:    llm.RoleAssistant,
				Content: step.Analysis,
			})
		}
	}
	return messages
}

// pruneMessages compresses conversation history when it gets too large.
// It replaces old tool result messages with short summaries to free up
// context window space while preserving the investigation flow.
func pruneMessages(messages []llm.Message, tokenBudget int) []llm.Message {
	totalChars := 0
	for _, m := range messages {
		totalChars += len(m.Content)
	}

	estimatedTokens := totalChars / charsPerToken
	threshold := int(float64(tokenBudget) * pruneThreshold)

	if estimatedTokens <= threshold {
		return messages
	}

	// Build pruned copy: keep first message (initial prompt) and last 4
	// messages intact; compress tool results in the middle.
	pruned := make([]llm.Message, 0, len(messages))
	keepTailCount := 4
	tailStart := len(messages) - keepTailCount
	if tailStart < 1 {
		tailStart = 1
	}

	for i, m := range messages {
		if i == 0 || i >= tailStart {
			// Keep first and recent messages unchanged
			pruned = append(pruned, m)
			continue
		}

		if m.Role == llm.RoleTool && len(m.Content) > 200 {
			// Compress old tool results to a short summary
			summary := m.Content[:100] + "\n... [pruned for context management]"
			pruned = append(pruned, llm.Message{
				Role:       m.Role,
				ToolCallID: m.ToolCallID,
				ToolName:   m.ToolName,
				Content:    summary,
			})
		} else {
			pruned = append(pruned, m)
		}
	}

	return pruned
}

// extractConclusion checks if any tool call is a conclude_investigation call
// and returns the structured conclusion data. Returns nil if not found.
func extractConclusion(tcs []llm.ToolCall) *tool.ConclusionInput {
	for _, tc := range tcs {
		if tc.Name != tool.ConcludeToolName {
			continue
		}
		var c tool.ConclusionInput
		if err := json.Unmarshal(tc.Input, &c); err != nil {
			continue
		}
		if c.Confidence < 0 {
			c.Confidence = 0
		}
		if c.Confidence > 100 {
			c.Confidence = 100
		}
		return &c
	}
	return nil
}

// extractConfidence parses the "## Confidence" section for a 0-100 value.
func extractConfidence(content string) int {
	lines := strings.Split(content, "\n")
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## Confidence") {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "## ") {
				break
			}
			var val int
			if _, err := fmt.Sscanf(trimmed, "%d", &val); err == nil && val >= 0 && val <= 100 {
				return val
			}
		}
	}
	return 0
}

// extractRootCauseAndResolution parses the structured final report from the LLM.
func extractRootCauseAndResolution(content string) (rootCause, resolution string) {
	sections := map[string]*string{
		"## root cause": &rootCause,
		"## resolution": &resolution,
	}

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

	return rootCause, resolution
}
