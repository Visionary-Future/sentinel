package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// CachingProvider wraps a Provider and marks system prompts for caching.
// It tracks unique system prompts and sets cache_control headers for
// providers that support it (e.g., Claude's prompt caching).
type CachingProvider struct {
	inner Provider
	mu    sync.RWMutex
	seen  map[string]bool // hash → already seen
}

func NewCachingProvider(inner Provider) *CachingProvider {
	return &CachingProvider{
		inner: inner,
		seen:  make(map[string]bool),
	}
}

func (c *CachingProvider) Name() string  { return c.inner.Name() }
func (c *CachingProvider) Model() string { return c.inner.Model() }

func (c *CachingProvider) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	// Mark the system prompt as cacheable if we've seen it before.
	// First call sends it fresh; subsequent calls benefit from cache.
	hash := hashString(system)

	c.mu.Lock()
	wasSeen := c.seen[hash]
	c.seen[hash] = true
	c.mu.Unlock()

	// If we've seen this system prompt before, signal caching by
	// setting CacheControl on the first message. The actual caching
	// mechanism depends on the provider implementation — Claude uses
	// cache_control: {"type": "ephemeral"} on content blocks.
	if wasSeen && len(messages) > 0 {
		// Clone messages to avoid mutation
		cached := make([]Message, len(messages))
		copy(cached, messages)
		cached[0] = Message{
			Role:         cached[0].Role,
			Content:      cached[0].Content,
			ToolCalls:    cached[0].ToolCalls,
			ToolCallID:   cached[0].ToolCallID,
			ToolName:     cached[0].ToolName,
			CacheControl: "ephemeral",
		}
		return c.inner.Chat(ctx, system, cached, tools)
	}

	return c.inner.Chat(ctx, system, messages, tools)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
