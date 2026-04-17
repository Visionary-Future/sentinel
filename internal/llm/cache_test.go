package llm

import (
	"context"
	"testing"
)

// capturingProvider records every Chat call for assertion in tests.
type capturingProvider struct {
	name  string
	model string
	calls []capturedCall
}

type capturedCall struct {
	system   string
	messages []Message
	tools    []Tool
}

func newCapturingProvider(name, model string) *capturingProvider {
	return &capturingProvider{name: name, model: model}
}

func (c *capturingProvider) Name() string  { return c.name }
func (c *capturingProvider) Model() string { return c.model }

func (c *capturingProvider) Chat(_ context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	c.calls = append(c.calls, capturedCall{
		system:   system,
		messages: messages,
		tools:    tools,
	})
	return &Response{Content: "ok", StopReason: StopReasonEndTurn}, nil
}

func (c *capturingProvider) lastCall() capturedCall {
	return c.calls[len(c.calls)-1]
}

func TestCachingProvider_FirstCallNoCacheControl(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-3")
	cp := NewCachingProvider(inner)

	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
	}

	_, err := cp.Chat(context.Background(), "system prompt", msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(inner.calls))
	}
	got := inner.lastCall().messages[0]
	if got.CacheControl != "" {
		t.Errorf("first call CacheControl = %q, want empty", got.CacheControl)
	}
}

func TestCachingProvider_SecondCallSetsCacheControl(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-3")
	cp := NewCachingProvider(inner)

	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
	}
	system := "my system prompt"

	// First call — no cache control.
	if _, err := cp.Chat(context.Background(), system, msgs, nil); err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Second call with same system prompt — should set cache control.
	if _, err := cp.Chat(context.Background(), system, msgs, nil); err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if len(inner.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(inner.calls))
	}
	got := inner.calls[1].messages[0]
	if got.CacheControl != "ephemeral" {
		t.Errorf("second call CacheControl = %q, want %q", got.CacheControl, "ephemeral")
	}
}

func TestCachingProvider_DifferentSystemPromptsTrackedSeparately(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-3")
	cp := NewCachingProvider(inner)

	msgs := []Message{{Role: RoleUser, Content: "hello"}}

	// Alternate between two distinct system prompts. Each should behave
	// independently: first occurrence → no cache, second occurrence → cached.
	prompts := []string{"prompt-A", "prompt-B", "prompt-A", "prompt-B"}
	wantCacheControls := []string{"", "", "ephemeral", "ephemeral"}

	for i, prompt := range prompts {
		if _, err := cp.Chat(context.Background(), prompt, msgs, nil); err != nil {
			t.Fatalf("call %d error: %v", i, err)
		}
		got := inner.calls[i].messages[0].CacheControl
		if got != wantCacheControls[i] {
			t.Errorf("call %d (prompt=%q): CacheControl = %q, want %q", i, prompt, got, wantCacheControls[i])
		}
	}
}

func TestCachingProvider_MessagesAreNotMutated(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-3")
	cp := NewCachingProvider(inner)

	original := []Message{
		{Role: RoleUser, Content: "original content", CacheControl: ""},
	}
	system := "immutable test prompt"

	// First call — prime the cache.
	if _, err := cp.Chat(context.Background(), system, original, nil); err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Snapshot original state before second call.
	originalCacheControl := original[0].CacheControl
	originalContent := original[0].Content

	// Second call — the provider should clone messages, not mutate original.
	if _, err := cp.Chat(context.Background(), system, original, nil); err != nil {
		t.Fatalf("second call error: %v", err)
	}

	// The original slice must be untouched.
	if original[0].CacheControl != originalCacheControl {
		t.Errorf("original message CacheControl mutated: got %q, want %q",
			original[0].CacheControl, originalCacheControl)
	}
	if original[0].Content != originalContent {
		t.Errorf("original message Content mutated: got %q, want %q",
			original[0].Content, originalContent)
	}

	// But the inner provider should have received the mutated copy.
	innerMsg := inner.calls[1].messages[0]
	if innerMsg.CacheControl != "ephemeral" {
		t.Errorf("inner provider did not receive cached copy: CacheControl = %q", innerMsg.CacheControl)
	}
}

func TestCachingProvider_NoMessagesDoesNotPanic(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-3")
	cp := NewCachingProvider(inner)

	system := "system"

	// Prime the seen map.
	if _, err := cp.Chat(context.Background(), system, nil, nil); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	// Second call with same system but no messages — should not panic.
	if _, err := cp.Chat(context.Background(), system, nil, nil); err != nil {
		t.Fatalf("second call error: %v", err)
	}
}

func TestCachingProvider_DelegatesNameAndModel(t *testing.T) {
	inner := newCapturingProvider("claude", "claude-sonnet-4-6")
	cp := NewCachingProvider(inner)

	if cp.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", cp.Name(), "claude")
	}
	if cp.Model() != "claude-sonnet-4-6" {
		t.Errorf("Model() = %q, want %q", cp.Model(), "claude-sonnet-4-6")
	}
}
