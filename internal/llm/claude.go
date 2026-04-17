package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"

// ClaudeProvider calls the Anthropic Messages API.
type ClaudeProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewClaude(apiKey, model string) *ClaudeProvider {
	return &ClaudeProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *ClaudeProvider) Name() string  { return "claude" }
func (c *ClaudeProvider) Model() string { return c.model }

// claudeRequest mirrors the Anthropic Messages API request body.
type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
	Tools     []claudeTool    `json:"tools,omitempty"`
}

type claudeMessage struct {
	Role    string        `json:"role"`
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	Content    string          `json:"content,omitempty"`
}

type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type claudeResponse struct {
	ID         string         `json:"id"`
	StopReason string         `json:"stop_reason"`
	Content    []claudeBlock  `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *ClaudeProvider) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	req := claudeRequest{
		Model:     c.model,
		MaxTokens: 4096,
		System:    system,
		Messages:  toClaudeMessages(messages),
		Tools:     toClaudeTools(tools),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call claude api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude api error %d: %s", resp.StatusCode, respBody)
	}

	var cr claudeResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return fromClaudeResponse(&cr), nil
}

func toClaudeMessages(msgs []Message) []claudeMessage {
	result := make([]claudeMessage, 0, len(msgs))
	for _, m := range msgs {
		cm := claudeMessage{Role: m.Role}

		switch m.Role {
		case RoleTool:
			cm.Role = "user"
			cm.Content = []claudeBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}}
		case RoleAssistant:
			var blocks []claudeBlock
			if m.Content != "" {
				blocks = append(blocks, claudeBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, claudeBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			cm.Content = blocks
		default:
			cm.Content = []claudeBlock{{Type: "text", Text: m.Content}}
		}
		result = append(result, cm)
	}
	return result
}

func toClaudeTools(tools []Tool) []claudeTool {
	result := make([]claudeTool, len(tools))
	for i, t := range tools {
		result[i] = claudeTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return result
}

func fromClaudeResponse(cr *claudeResponse) *Response {
	resp := &Response{
		StopReason: cr.StopReason,
		TokensUsed: cr.Usage.InputTokens + cr.Usage.OutputTokens,
	}

	for _, block := range cr.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}

	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopReasonToolUse
	}

	return resp
}
