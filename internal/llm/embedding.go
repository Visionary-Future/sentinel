package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Embedder generates text embeddings suitable for similarity search.
type Embedder interface {
	// Embed returns a float32 vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dims returns the vector dimension (e.g. 1536 for text-embedding-ada-002).
	Dims() int
}

// OpenAIEmbedder calls any OpenAI-compatible /v1/embeddings endpoint.
// Works with OpenAI, Tongyi (text-embedding-v2), and other compatible providers.
type OpenAIEmbedder struct {
	apiKey   string
	model    string
	baseURL  string
	dims     int
	client   *http.Client
}

// NewOpenAIEmbedder creates an embedder pointed at the given base URL.
// For OpenAI: baseURL = "https://api.openai.com/v1", model = "text-embedding-3-small", dims = 1536.
// For Tongyi: baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1", model = "text-embedding-v2", dims = 1536.
func NewOpenAIEmbedder(apiKey, model, baseURL string, dims int) *OpenAIEmbedder {
	if dims == 0 {
		dims = 1536
	}
	return &OpenAIEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		dims:    dims,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OpenAIEmbedder) Dims() int { return e.dims }

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": e.model,
		"input": text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed: empty embedding in response")
	}

	return result.Data[0].Embedding, nil
}
