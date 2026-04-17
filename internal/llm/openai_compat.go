package llm

// OpenAICompatProvider implements Provider for any OpenAI-compatible API.
// Used by: OpenAI, Tongyi (DashScope), DeepSeek, and self-hosted models.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAICompatProvider calls any OpenAI-compatible /chat/completions endpoint.
type OpenAICompatProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenAICompat(apiKey, model, baseURL string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OpenAICompatProvider) Name() string  { return "openai_compat" }
func (o *OpenAICompatProvider) Model() string { return o.model }

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string          `json:"type"` // always "function"
	Function openAIFunction  `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (o *OpenAICompatProvider) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	// Prepend system message
	oaiMsgs := []openAIMessage{{Role: "system", Content: system}}
	for _, m := range messages {
		oaiMsgs = append(oaiMsgs, toOpenAIMessage(m))
	}

	req := openAIRequest{
		Model:    o.model,
		Messages: oaiMsgs,
		Tools:    toOpenAITools(tools),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, respBody)
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return fromOpenAIResponse(&oaiResp), nil
}

func toOpenAIMessage(m Message) openAIMessage {
	oai := openAIMessage{Role: m.Role, Content: m.Content}
	if m.Role == RoleTool {
		oai.Role = "tool"
		oai.ToolCallID = m.ToolCallID
	}
	for _, tc := range m.ToolCalls {
		args, _ := json.Marshal(tc.Input)
		oai.ToolCalls = append(oai.ToolCalls, openAIToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: tc.Name, Arguments: string(args)},
		})
	}
	return oai
}

func toOpenAITools(tools []Tool) []openAITool {
	result := make([]openAITool, len(tools))
	for i, t := range tools {
		result[i] = openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return result
}

func fromOpenAIResponse(r *openAIResponse) *Response {
	if len(r.Choices) == 0 {
		return &Response{StopReason: StopReasonEndTurn}
	}

	choice := r.Choices[0]
	resp := &Response{
		Content:    choice.Message.Content,
		StopReason: StopReasonEndTurn,
		TokensUsed: r.Usage.TotalTokens,
	}

	for _, tc := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopReasonToolUse
	}

	return resp
}
