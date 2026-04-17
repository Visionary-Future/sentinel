package investigation

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
)

// ---- extractConfidence -------------------------------------------------------

func TestExtractConfidence(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name: "standard structured response",
			content: `## Root Cause
High memory pressure on the pod.

## Confidence
85

## Resolution
Increase memory limit.`,
			want: 85,
		},
		{
			name: "confidence is 0",
			content: `## Root Cause
Unknown.

## Confidence
0

## Resolution
Investigate further.`,
			want: 0,
		},
		{
			name: "confidence is 100",
			content: `## Confidence
100`,
			want: 100,
		},
		{
			name:    "no confidence section",
			content: `## Root Cause\nSome cause.`,
			want:    0,
		},
		{
			name:    "empty string",
			content: "",
			want:    0,
		},
		{
			name: "confidence section present but non-numeric",
			content: `## Confidence
very high`,
			want: 0,
		},
		{
			name: "confidence value out of range (>100) is not returned",
			content: `## Confidence
150`,
			want: 0,
		},
		{
			name: "confidence value negative is not returned",
			content: `## Confidence
-5`,
			want: 0,
		},
		{
			name: "confidence header is case-insensitive",
			content: `## confidence
72`,
			want: 72,
		},
		{
			name: "leading whitespace around value",
			content: `## Confidence
   42   `,
			want: 42,
		},
		{
			name: "next section header stops parsing",
			content: `## Confidence
## Resolution
ignored`,
			want: 0,
		},
		{
			name: "multiple sections, confidence in middle",
			content: `## Root Cause
DB connection pool exhausted.

## Confidence
91

## Resolution
Increase pool size.

## Summary
Brief summary.`,
			want: 91,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractConfidence(tc.content)
			if got != tc.want {
				t.Errorf("extractConfidence() = %d, want %d\ncontent:\n%s", got, tc.want, tc.content)
			}
		})
	}
}

// ---- truncateResult ---------------------------------------------------------

func TestTruncateResult(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		maxLen      int
		expectTrunc bool
		wantPrefix  string
	}{
		{
			name:        "under limit — returned unchanged",
			input:       "hello",
			maxLen:      10,
			expectTrunc: false,
			wantPrefix:  "hello",
		},
		{
			name:        "exactly at limit — returned unchanged",
			input:       "hello",
			maxLen:      5,
			expectTrunc: false,
			wantPrefix:  "hello",
		},
		{
			name:        "over limit — truncated with notice",
			input:       "hello world",
			maxLen:      5,
			expectTrunc: true,
			wantPrefix:  "hello",
		},
		{
			name:        "empty string — returned unchanged",
			input:       "",
			maxLen:      10,
			expectTrunc: false,
			wantPrefix:  "",
		},
		{
			name:        "limit 0 — always truncated (empty prefix)",
			input:       "any text",
			maxLen:      0,
			expectTrunc: true,
			wantPrefix:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateResult(tc.input, tc.maxLen)

			if tc.expectTrunc {
				if !strings.Contains(got, "[truncated") {
					t.Errorf("expected truncation notice in result, got: %q", got)
				}
				if !strings.HasPrefix(got, tc.wantPrefix) {
					t.Errorf("expected prefix %q, got: %q", tc.wantPrefix, got)
				}
				// The returned string includes the truncation suffix so it will be
				// longer than maxLen — that's expected behaviour.
			} else {
				if got != tc.input {
					t.Errorf("expected unchanged string %q, got %q", tc.input, got)
				}
			}
		})
	}
}

func TestTruncateResult_NoticeContainsMaxLen(t *testing.T) {
	result := truncateResult(strings.Repeat("x", 100), 50)
	if !strings.Contains(result, "50") {
		t.Errorf("expected truncation notice to mention maxLen 50, got: %q", result)
	}
}

func TestTruncateResult_PrefixIsExactlyMaxLen(t *testing.T) {
	input := strings.Repeat("a", 200)
	maxLen := 80
	got := truncateResult(input, maxLen)
	// The first maxLen characters must be the original content
	if got[:maxLen] != input[:maxLen] {
		t.Errorf("first %d chars of result do not match input", maxLen)
	}
}

// ---- extractRootCauseAndResolution ------------------------------------------

func TestExtractRootCauseAndResolution(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantRC      string
		wantResolve string
	}{
		{
			name: "both sections present",
			content: `## Root Cause
High CPU caused by memory leak in service X.

## Confidence
80

## Resolution
Restart the service and patch the leak.

## Summary
Brief.`,
			wantRC:      "High CPU caused by memory leak in service X.",
			wantResolve: "Restart the service and patch the leak.",
		},
		{
			name:        "no sections",
			content:     "plain text without any markdown headers",
			wantRC:      "",
			wantResolve: "",
		},
		{
			name: "only root cause",
			content: `## Root Cause
The database ran out of connections.`,
			wantRC:      "The database ran out of connections.",
			wantResolve: "",
		},
		{
			name: "only resolution",
			content: `## Resolution
Scale up the deployment.`,
			wantRC:      "",
			wantResolve: "Scale up the deployment.",
		},
		{
			name:        "empty string",
			content:     "",
			wantRC:      "",
			wantResolve: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRC, gotResolve := extractRootCauseAndResolution(tc.content)
			if gotRC != tc.wantRC {
				t.Errorf("rootCause = %q, want %q", gotRC, tc.wantRC)
			}
			if gotResolve != tc.wantResolve {
				t.Errorf("resolution = %q, want %q", gotResolve, tc.wantResolve)
			}
		})
	}
}

// ---- extractConclusion ------------------------------------------------------

func TestExtractConclusion(t *testing.T) {
	tests := []struct {
		name       string
		toolCalls  []llm.ToolCall
		wantNil    bool
		wantRC     string
		wantConf   int
	}{
		{
			name:      "no tool calls",
			toolCalls: nil,
			wantNil:   true,
		},
		{
			name: "no conclude tool",
			toolCalls: []llm.ToolCall{
				{ID: "1", Name: "query_logs", Input: json.RawMessage(`{}`)},
			},
			wantNil: true,
		},
		{
			name: "conclude tool present",
			toolCalls: []llm.ToolCall{
				{ID: "1", Name: "query_logs", Input: json.RawMessage(`{}`)},
				{ID: "2", Name: tool.ConcludeToolName, Input: json.RawMessage(`{
					"root_cause": "OOM kill",
					"resolution": "increase memory",
					"summary": "pod killed",
					"confidence": 85
				}`)},
			},
			wantNil:  false,
			wantRC:   "OOM kill",
			wantConf: 85,
		},
		{
			name: "conclude tool with invalid JSON",
			toolCalls: []llm.ToolCall{
				{ID: "1", Name: tool.ConcludeToolName, Input: json.RawMessage(`not json`)},
			},
			wantNil: true,
		},
		{
			name: "conclude tool clamps negative confidence",
			toolCalls: []llm.ToolCall{
				{ID: "1", Name: tool.ConcludeToolName, Input: json.RawMessage(`{
					"root_cause": "test",
					"resolution": "fix",
					"summary": "s",
					"confidence": -10
				}`)},
			},
			wantNil:  false,
			wantRC:   "test",
			wantConf: 0,
		},
		{
			name: "conclude tool clamps >100 confidence",
			toolCalls: []llm.ToolCall{
				{ID: "1", Name: tool.ConcludeToolName, Input: json.RawMessage(`{
					"root_cause": "test",
					"resolution": "fix",
					"summary": "s",
					"confidence": 200
				}`)},
			},
			wantNil:  false,
			wantRC:   "test",
			wantConf: 100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractConclusion(tc.toolCalls)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil conclusion")
			}
			if got.RootCause != tc.wantRC {
				t.Errorf("root_cause = %q, want %q", got.RootCause, tc.wantRC)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("confidence = %d, want %d", got.Confidence, tc.wantConf)
			}
		})
	}
}

// ---- executeToolsParallel ---------------------------------------------------

func TestExecuteToolsParallel_SingleTool(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(llm.Tool{Name: "echo", Description: "test"}, func(_ context.Context, input json.RawMessage) (string, error) {
		return "echoed", nil
	})

	agent := NewAgent(&mockProvider{
		name: "test", model: "test",
		response: &llm.Response{StopReason: llm.StopReasonEndTurn},
	}, registry, slog.Default())

	results := agent.executeToolsParallel(context.Background(), []llm.ToolCall{
		{ID: "tc-1", Name: "echo", Input: json.RawMessage(`{}`)},
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].recorded.Result != "echoed" {
		t.Errorf("result = %q, want %q", results[0].recorded.Result, "echoed")
	}
	if results[0].message.ToolCallID != "tc-1" {
		t.Errorf("message tool_call_id = %q, want %q", results[0].message.ToolCallID, "tc-1")
	}
}

func TestExecuteToolsParallel_MultipleTool_RunsConcurrently(t *testing.T) {
	var running atomic.Int32

	registry := tool.NewRegistry()
	registry.Register(llm.Tool{Name: "slow", Description: "test"}, func(_ context.Context, input json.RawMessage) (string, error) {
		running.Add(1)
		time.Sleep(50 * time.Millisecond)
		running.Add(-1)
		return "done", nil
	})

	agent := NewAgent(&mockProvider{
		name: "test", model: "test",
		response: &llm.Response{StopReason: llm.StopReasonEndTurn},
	}, registry, slog.Default())

	tcs := []llm.ToolCall{
		{ID: "1", Name: "slow", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "slow", Input: json.RawMessage(`{}`)},
		{ID: "3", Name: "slow", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := agent.executeToolsParallel(context.Background(), tcs)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Should complete in ~50ms (parallel), not ~150ms (sequential)
	if elapsed > 120*time.Millisecond {
		t.Errorf("took %v, expected < 120ms for parallel execution", elapsed)
	}

	// Verify ordering is preserved
	for i, r := range results {
		wantID := tcs[i].ID
		if r.recorded.ID != wantID {
			t.Errorf("result[%d].ID = %q, want %q", i, r.recorded.ID, wantID)
		}
	}
}

func TestExecuteToolsParallel_ToolError(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(llm.Tool{Name: "fail", Description: "test"}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", context.DeadlineExceeded
	})

	agent := NewAgent(&mockProvider{
		name: "test", model: "test",
		response: &llm.Response{StopReason: llm.StopReasonEndTurn},
	}, registry, slog.Default())

	results := agent.executeToolsParallel(context.Background(), []llm.ToolCall{
		{ID: "1", Name: "fail", Input: json.RawMessage(`{}`)},
	})

	if results[0].recorded.Error == "" {
		t.Error("expected error in recorded tool call")
	}
	if !strings.Contains(results[0].message.Content, "Error:") {
		t.Errorf("expected error prefix in message, got: %q", results[0].message.Content)
	}
}
