package llm

// TongyiProvider calls the Alibaba Tongyi Qwen API.
// DashScope exposes an OpenAI-compatible endpoint, so we reuse the OpenAI client.
// Endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1

import (
	"context"
)

const tongyiBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

// TongyiProvider wraps the OpenAI-compatible client pointed at DashScope.
type TongyiProvider struct {
	inner *OpenAICompatProvider
}

func NewTongyi(apiKey, model string) *TongyiProvider {
	return &TongyiProvider{
		inner: NewOpenAICompat(apiKey, model, tongyiBaseURL),
	}
}

func (t *TongyiProvider) Name() string  { return "tongyi" }
func (t *TongyiProvider) Model() string { return t.inner.Model() }

func (t *TongyiProvider) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	return t.inner.Chat(ctx, system, messages, tools)
}
