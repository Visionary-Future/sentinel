package llm

import (
	"context"
	"encoding/json"
)

// Role constants for conversation messages.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// StopReason signals why the LLM stopped generating.
const (
	StopReasonEndTurn  = "end_turn"
	StopReasonToolUse  = "tool_use"
	StopReasonMaxTokens = "max_tokens"
)

// Message is a single turn in the conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role=tool responses
	ToolName   string     `json:"tool_name,omitempty"`
}

// Tool describes a function the LLM may call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"` // JSON Schema object
}

// ToolCall is one function invocation requested by the LLM.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Response is what a Provider returns after each LLM call.
type Response struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	StopReason string     `json:"stop_reason"`
	TokensUsed int        `json:"tokens_used"`
}

// Provider is the interface every LLM backend must implement.
type Provider interface {
	// Chat sends a conversation to the LLM and returns its response.
	Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error)
	// Name returns the provider identifier (e.g. "claude", "tongyi").
	Name() string
	// Model returns the model identifier being used.
	Model() string
}
